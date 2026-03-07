package echonet

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/model"
	"github.com/styygeli/echonetgo/internal/specs"
)

const (
	echonetPort           = 3610
	ehd1                  = 0x10
	ehd2                  = 0x81
	esvGet                = 0x62
	esvGetRes             = 0x72
	seojController        = 0x05
	seojClass             = 0xFF
	seojInstance          = 0x01
	minResponseLen        = 12
	maxAdaptiveSplitDepth = 8
	maxSendAttempts       = 2
)

var clientLog = logging.New("echonet-client")

var (
	tidCounter        atomic.Uint32
	sharedHostLockMu  sync.Mutex
	sharedHostLocks   = make(map[string]*sync.Mutex)
	sharedFixedConnMu sync.Mutex
	sharedFixedConn   *net.UDPConn
	sharedWaitersMu   sync.Mutex
	sharedWaiters     = make(map[string]chan udpFrame)
)

func nextTID() uint16 {
	return uint16(tidCounter.Add(1))
}

// Client sends ECHONET Lite Get requests over UDP and parses Get_Res.
type Client struct {
	timeout              time.Duration
	strictSourcePort3610 bool
}

type udpFrame struct {
	from *net.UDPAddr
	data []byte
}

// ESVError is returned when a response carries an unexpected service code.
type ESVError struct {
	ESV byte
}

func (e *ESVError) Error() string {
	return fmt.Sprintf("not Get_Res: ESV=0x%02x", e.ESV)
}

// DeviceInfo represents generic identity properties of a device.
type DeviceInfo struct {
	UID          string
	Manufacturer string
	ProductCode  string
}

// MetricValue holds a parsed value and its type (gauge or counter).
type MetricValue struct {
	Value float64
	Type  string
}

// NewClient creates a client with the given scrape timeout in seconds.
func NewClient(timeoutSec int, strictSourcePort3610 bool) *Client {
	return &Client{
		timeout:              time.Duration(timeoutSec) * time.Second,
		strictSourcePort3610: strictSourcePort3610,
	}
}

// GetRequest builds an ECHONET Lite Get frame.
func GetRequest(tid uint16, eoj [3]byte, epcs []byte) []byte {
	n := 4 + 2 + 3 + 3 + 1 + 1 + 2*len(epcs)
	b := make([]byte, 0, n)
	b = append(b, ehd1, ehd2)
	b = append(b, byte(tid>>8), byte(tid))
	b = append(b, seojController, seojClass, seojInstance)
	b = append(b, eoj[0], eoj[1], eoj[2])
	b = append(b, esvGet)
	b = append(b, byte(len(epcs)))
	for _, epc := range epcs {
		b = append(b, epc, 0)
	}
	return b
}

// SendGet sends a Get request to addr and returns the raw response.
func (c *Client) SendGet(ctx context.Context, addr string, eoj [3]byte, epcs []byte) ([]byte, error) {
	if len(epcs) == 0 {
		return nil, fmt.Errorf("no EPCs")
	}
	hostKey := normalizeHost(addr)
	hostLock := lockForHost(hostKey)
	hostLock.Lock()
	defer hostLock.Unlock()

	tid := nextTID()
	req := GetRequest(tid, eoj, epcs)
	host := addr
	if _, _, err := net.SplitHostPort(addr); err != nil {
		host = net.JoinHostPort(addr, fmt.Sprint(echonetPort))
	}
	resp, err := c.sendGetWithFixedPort(ctx, host, req, tid, hostKey, echonetPort)
	if err == nil {
		return resp, nil
	}
	if c.strictSourcePort3610 {
		return nil, fmt.Errorf("failed to send request from required local UDP source port %d to %s: %w",
			echonetPort, hostKey, err)
	}
	if !isPortBindFailure(err) {
		return nil, err
	}
	clientLog.Warnf("failed to bind local UDP port %d for %s; falling back to ephemeral source port: %v",
		echonetPort, hostKey, err)
	resp, fallbackErr := c.sendGetEphemeral(ctx, host, req, tid, hostKey)
	if fallbackErr != nil {
		return nil, fmt.Errorf("failed local UDP source port %d (%v), ephemeral fallback also failed: %w",
			echonetPort, err, fallbackErr)
	}
	return resp, nil
}

func (c *Client) sendGetWithFixedPort(ctx context.Context, host string, req []byte, tid uint16, hostKey string, localPort int) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := c.sendGetFromPort(ctx, host, req, tid, hostKey, localPort)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isTimeoutError(err) || attempt == maxSendAttempts {
			return nil, err
		}
		clientLog.Warnf("timeout waiting for response from %s via local UDP port %d (attempt %d/%d), retrying",
			hostKey, localPort, attempt, maxSendAttempts)
	}
	return nil, lastErr
}

func (c *Client) sendGetFromPort(ctx context.Context, host string, req []byte, tid uint16, hostKey string, localPort int) ([]byte, error) {
	remoteAddr, err := net.ResolveUDPAddr("udp4", host)
	if err != nil {
		return nil, err
	}
	if localPort == echonetPort {
		return sendGetViaSharedFixedPort(ctx, remoteAddr, req, tid, hostKey, c.timeout)
	}
	conn, err := openUDPConn(localPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return writeAndRead(ctx, conn, remoteAddr, req, tid, hostKey, c.timeout)
}

func (c *Client) sendGetEphemeral(ctx context.Context, host string, req []byte, tid uint16, hostKey string) ([]byte, error) {
	return c.sendGetFromPort(ctx, host, req, tid, hostKey, 0)
}

func writeAndRead(ctx context.Context, conn *net.UDPConn, remoteAddr *net.UDPAddr, req []byte, tid uint16, hostKey string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := conn.WriteToUDP(req, remoteAddr); err != nil {
		return nil, err
	}
	buf := make([]byte, 1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return nil, err
		}
		if addr.IP == nil || remoteAddr.IP == nil || !addr.IP.Equal(remoteAddr.IP) {
			clientLog.Debugf("ignoring frame from unexpected IP while waiting for %s: %v", hostKey, addr.IP)
			continue
		}
		if n < minResponseLen {
			clientLog.Warnf("short UDP frame from %s:%d: got=%d expected>=%d", hostKey, addr.Port, n, minResponseLen)
			continue
		}
		respTID := binary.BigEndian.Uint16(buf[2:4])
		if respTID == tid {
			if addr.Port != echonetPort {
				clientLog.Debugf("accepted response from %s with non-standard source UDP port %d", hostKey, addr.Port)
			}
			return append([]byte(nil), buf[:n]...), nil
		}
		clientLog.Debugf("ignoring stale UDP frame from %s:%d: expected tid=0x%04x got=0x%04x", hostKey, addr.Port, tid, respTID)
	}
}

func sendGetViaSharedFixedPort(ctx context.Context, remoteAddr *net.UDPAddr, req []byte, tid uint16, hostKey string, timeout time.Duration) ([]byte, error) {
	conn, err := getOrCreateFixedConn()
	if err != nil {
		return nil, err
	}
	key := waiterKey(remoteAddr.IP.String(), tid)
	respCh := make(chan udpFrame, 1)
	addWaiter(key, respCh)
	defer removeWaiter(key)

	_, writeErr := conn.WriteToUDP(req, remoteAddr)
	if writeErr != nil {
		if !errors.Is(writeErr, net.ErrClosed) {
			return nil, writeErr
		}
		resetFixedConn(conn)
		conn, err = getOrCreateFixedConn()
		if err != nil {
			return nil, err
		}
		if _, writeErr = conn.WriteToUDP(req, remoteAddr); writeErr != nil {
			return nil, writeErr
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case frame := <-respCh:
		if frame.from != nil && frame.from.Port != echonetPort {
			clientLog.Debugf("accepted response from %s with non-standard source UDP port %d", hostKey, frame.from.Port)
		}
		return frame.data, nil
	case <-timer.C:
		return nil, fmt.Errorf("read udp %s->%s:%d: i/o timeout", conn.LocalAddr().String(), remoteAddr.IP.String(), remoteAddr.Port)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func getOrCreateFixedConn() (*net.UDPConn, error) {
	sharedFixedConnMu.Lock()
	defer sharedFixedConnMu.Unlock()
	if sharedFixedConn != nil {
		return sharedFixedConn, nil
	}
	conn, err := openUDPConn(echonetPort)
	if err != nil {
		return nil, err
	}
	sharedFixedConn = conn
	go startFixedConnReceiver(conn)
	return conn, nil
}

func startFixedConnReceiver(conn *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				sharedFixedConnMu.Lock()
				if sharedFixedConn == conn {
					sharedFixedConn = nil
				}
				sharedFixedConnMu.Unlock()
				return
			}
			clientLog.Warnf("fixed source-port UDP receiver error: %v", err)
			continue
		}
		if addr == nil || addr.IP == nil {
			continue
		}
		if n < minResponseLen {
			clientLog.Warnf("short UDP frame from %s:%d: got=%d expected>=%d", addr.IP.String(), addr.Port, n, minResponseLen)
			continue
		}
		tid := binary.BigEndian.Uint16(buf[2:4])
		key := waiterKey(addr.IP.String(), tid)
		sharedWaitersMu.Lock()
		ch := sharedWaiters[key]
		sharedWaitersMu.Unlock()
		if ch == nil {
			clientLog.Debugf("ignoring stale UDP frame from %s:%d: tid=0x%04x has no waiter", addr.IP.String(), addr.Port, tid)
			continue
		}
		frame := udpFrame{
			from: addr,
			data: append([]byte(nil), buf[:n]...),
		}
		select {
		case ch <- frame:
		default:
		}
	}
}

func openUDPConn(localPort int) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, rawConn syscall.RawConn) error {
			var sockErr error
			if err := rawConn.Control(func(fd uintptr) {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			}); err != nil {
				return err
			}
			return sockErr
		},
	}
	packetConn, err := lc.ListenPacket(context.Background(), "udp4", net.JoinHostPort("0.0.0.0", fmt.Sprint(localPort)))
	if err != nil {
		return nil, err
	}
	udpConn, ok := packetConn.(*net.UDPConn)
	if !ok {
		_ = packetConn.Close()
		return nil, fmt.Errorf("not a UDP connection")
	}
	return udpConn, nil
}

func resetFixedConn(old *net.UDPConn) {
	sharedFixedConnMu.Lock()
	defer sharedFixedConnMu.Unlock()
	if sharedFixedConn == old {
		_ = old.Close()
		sharedFixedConn = nil
	}
}

func addWaiter(key string, ch chan udpFrame) {
	sharedWaitersMu.Lock()
	sharedWaiters[key] = ch
	sharedWaitersMu.Unlock()
}

func removeWaiter(key string) {
	sharedWaitersMu.Lock()
	delete(sharedWaiters, key)
	sharedWaitersMu.Unlock()
}

func waiterKey(host string, tid uint16) string {
	return host + "|" + fmt.Sprintf("%04x", tid)
}

func isPortBindFailure(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EACCES) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "permission denied")
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "i/o timeout")
}

func normalizeHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return addr
}

// lockForHost returns a per-host mutex. Entries are never evicted; this is safe
// because the set of hosts is bounded by the static device configuration.
func lockForHost(host string) *sync.Mutex {
	sharedHostLockMu.Lock()
	defer sharedHostLockMu.Unlock()
	if m, ok := sharedHostLocks[host]; ok {
		return m
	}
	m := &sync.Mutex{}
	sharedHostLocks[host] = m
	return m
}

// ParseGetRes parses an ECHONET Lite frame and returns properties if it is a Get_Res.
func ParseGetRes(data []byte) (tid uint16, props []model.GetResProperty, err error) {
	if len(data) < minResponseLen {
		return 0, nil, fmt.Errorf("response too short: %d", len(data))
	}
	if data[0] != ehd1 || data[1] != ehd2 {
		return 0, nil, fmt.Errorf("invalid EHD: %02x %02x", data[0], data[1])
	}
	tid = binary.BigEndian.Uint16(data[2:4])
	esv := data[10]
	if esv != esvGetRes {
		return 0, nil, &ESVError{ESV: esv}
	}
	opc := int(data[11])
	pos := 12
	truncated := false
	for i := 0; i < opc && pos+2 <= len(data); i++ {
		epc := data[pos]
		pdc := data[pos+1]
		pos += 2
		edtLen := int(pdc)
		if pos+edtLen > len(data) {
			clientLog.Warnf("malformed Get_Res: truncated property data for EPC=0x%02x PDC=%d payload_len=%d", epc, pdc, len(data))
			truncated = true
			break
		}
		edt := make([]byte, edtLen)
		copy(edt, data[pos:pos+edtLen])
		pos += edtLen
		props = append(props, model.GetResProperty{EPC: epc, PDC: pdc, EDT: edt})
	}
	if len(props) < opc {
		if truncated {
			clientLog.Warnf("Get_Res partially parsed: parsed=%d declared_opc=%d", len(props), opc)
		} else {
			clientLog.Warnf("Get_Res ended early: parsed=%d declared_opc=%d", len(props), opc)
		}
	}
	return tid, props, nil
}

// GetProps fetches requested EPCs and adaptively splits when devices return partial responses.
func (c *Client) GetProps(ctx context.Context, addr string, eoj [3]byte, epcs []byte) ([]model.GetResProperty, error) {
	return c.getPropsAdaptive(ctx, addr, eoj, epcs, 0)
}

func (c *Client) getPropsAdaptive(ctx context.Context, addr string, eoj [3]byte, epcs []byte, depth int) ([]model.GetResProperty, error) {
	raw, err := c.SendGet(ctx, addr, eoj, epcs)
	if err != nil {
		return nil, err
	}
	_, props, err := ParseGetRes(raw)
	if err != nil {
		return nil, err
	}
	missing := missingEPCs(epcs, props)
	if len(missing) == 0 {
		return props, nil
	}
	if len(epcs) <= 1 {
		clientLog.Warnf("device %s returned no data for requested EPC(s): %s", normalizeHost(addr), formatEPCList(missing))
		return props, nil
	}
	if depth >= maxAdaptiveSplitDepth {
		clientLog.Warnf("max adaptive split depth reached for %s eoj=%s missing=%s", normalizeHost(addr), formatEOJ(eoj), formatEPCList(missing))
		return props, nil
	}
	clientLog.Warnf("partial response from %s eoj=%s requested=%d returned=%d missing=%s; retrying split batches",
		normalizeHost(addr), formatEOJ(eoj), len(epcs), len(props), formatEPCList(missing))
	left, right := splitEPCs(epcs)
	merged := propsToMap(props)
	for _, part := range [][]byte{left, right} {
		if len(part) == 0 || !containsAny(part, missing) {
			continue
		}
		partProps, err := c.getPropsAdaptive(ctx, addr, eoj, part, depth+1)
		if err != nil {
			clientLog.Warnf("split batch request failed for %s eoj=%s epcs=%s: %v", normalizeHost(addr), formatEOJ(eoj), formatEPCList(part), err)
			continue
		}
		mergeProps(merged, partProps)
	}
	out := mapToProps(merged)
	finalMissing := missingEPCs(epcs, out)
	if len(finalMissing) > 0 {
		clientLog.Warnf("after retries, still missing EPC(s) from %s eoj=%s: %s", normalizeHost(addr), formatEOJ(eoj), formatEPCList(finalMissing))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no properties returned for requested EPCs")
	}
	return out, nil
}

// GetReadablePropertyMap reads EPC 0x9F and decodes readable properties.
func (c *Client) GetReadablePropertyMap(ctx context.Context, addr string, eoj [3]byte) (map[byte]struct{}, error) {
	props, err := c.GetProps(ctx, addr, eoj, []byte{0x9F})
	if err != nil {
		return nil, err
	}
	for _, p := range props {
		if p.EPC == 0x9F && len(p.EDT) > 0 {
			return decodePropertyMap(p.EDT), nil
		}
	}
	clientLog.Warnf("device %s eoj=%s: readable property map (0x9F) missing/empty", normalizeHost(addr), formatEOJ(eoj))
	return nil, fmt.Errorf("readable property map (0x9F) missing")
}

// GetManufacturerCode reads EPC 0x8A and returns the 3-byte manufacturer code as a
// 6-digit lowercase hex string (e.g. "000006"). Used for vendor-specific spec lookup.
// Falls back to node profile EOJ if the class EOJ returns Get_SNA.
func (c *Client) GetManufacturerCode(ctx context.Context, addr string, eoj [3]byte) (string, error) {
	nodeProfileEOJ := [3]byte{0x0E, 0xF0, 0x01}
	props, err := c.GetProps(ctx, addr, eoj, []byte{0x8A})
	if err != nil {
		if isGetSNA(err) && eoj != nodeProfileEOJ {
			props, err = c.GetProps(ctx, addr, nodeProfileEOJ, []byte{0x8A})
		}
		if err != nil {
			if isGetSNA(err) {
				return "", nil
			}
			return "", err
		}
	}
	for _, p := range props {
		if p.EPC != 0x8A || len(p.EDT) != 3 {
			continue
		}
		return fmt.Sprintf("%02x%02x%02x", p.EDT[0], p.EDT[1], p.EDT[2]), nil
	}
	return "", nil
}

// GetDeviceInfo reads generic identity properties.
func (c *Client) GetDeviceInfo(ctx context.Context, addr string, eoj [3]byte) (DeviceInfo, error) {
	nodeProfileEOJ := [3]byte{0x0E, 0xF0, 0x01}
	props, err := c.GetProps(ctx, addr, eoj, []byte{0x83, 0x8A, 0x8C})
	if err != nil {
		if isGetSNA(err) && eoj != nodeProfileEOJ {
			clientLog.Debugf("device %s eoj=%s: identity Get returned Get_SNA; retrying via node profile",
				normalizeHost(addr), formatEOJ(eoj))
			props, err = c.GetProps(ctx, addr, nodeProfileEOJ, []byte{0x83, 0x8A, 0x8C})
		}
		if err != nil {
			if isGetSNA(err) {
				clientLog.Debugf("device %s eoj=%s: identity properties not supported (Get_SNA)", normalizeHost(addr), formatEOJ(eoj))
				return DeviceInfo{}, nil
			}
			return DeviceInfo{}, err
		}
	}
	info := DeviceInfo{}
	for _, p := range props {
		switch p.EPC {
		case 0x83:
			info.UID = decodeUID(p.EDT, normalizeHost(addr))
		case 0x8A:
			info.Manufacturer = decodeManufacturer(p.EDT)
		case 0x8C:
			info.ProductCode = decodeProductCode(p.EDT)
		}
	}
	if info.UID == "" || info.Manufacturer == "" || info.ProductCode == "" {
		clientLog.Warnf("device %s eoj=%s: incomplete device info uid=%q manufacturer=%q product_code=%q",
			normalizeHost(addr), formatEOJ(eoj), info.UID, info.Manufacturer, info.ProductCode)
	}
	return info, nil
}

func isGetSNA(err error) bool {
	var esvErr *ESVError
	return errors.As(err, &esvErr) && esvErr.ESV == 0x52
}

// manufacturerNames maps ECHONET manufacturer codes to human-readable names.
// Source: echonet_specs/manufacturer_codes.csv (ECHONET Consortium, 2026-03-03).
var manufacturerNames = map[uint32]string{
	0x000001: "Hitachi",
	0x000005: "Sharp",
	0x000006: "Mitsubishi Electric",
	0x000008: "Daikin Industries",
	0x000009: "NEC",
	0x00000B: "Panasonic",
	0x000012: "Oi Electric",
	0x000015: "Daikin Systems Solutions Lab",
	0x000016: "Toshiba",
	0x000017: "Nihon Carrier (Toshiba Carrier)",
	0x00001B: "Toshiba Lighting & Technology",
	0x000022: "Hitachi Global Life Solutions",
	0x000023: "NTT Comware",
	0x000025: "LIXIL",
	0x00002C: "AFT",
	0x00002E: "Shikoku Measurement",
	0x00002F: "Aiphone",
	0x000034: "Mitsubishi Electric Engineering",
	0x000035: "Toko Toshiba Meter Systems",
	0x000036: "Nisshin Systems",
	0x00003A: "Sekisui House",
	0x00003B: "Kyocera",
	0x00003C: "Denso",
	0x00003D: "Sumitomo Electric",
	0x00003E: "Sumitomo Electric Networks",
	0x000040: "Hitachi High-Tech Solutions",
	0x000041: "Enegate",
	0x000043: "Toshiba Unified Technologies",
	0x000044: "Hitachi Industrial Equipment",
	0x000047: "NTT East",
	0x000048: "Oki Electric",
	0x00004D: "Inaba Denki Sangyo",
	0x00004E: "Fujitsu",
	0x00004F: "Daiwa House Industry",
	0x000050: "TOTO",
	0x000051: "Fuji IT",
	0x000052: "Osaki Electric",
	0x000053: "Ubiquitous AI",
	0x000054: "Noritz",
	0x000055: "Family Net Japan",
	0x000056: "iND",
	0x000057: "Eliiy Power",
	0x000058: "Mediotec (Direct Power)",
	0x000059: "Rinnai",
	0x00005C: "TransBoot",
	0x000060: "Sony CSL",
	0x000061: "NTT Data Intellilink",
	0x000063: "Kawamura Electric",
	0x000064: "Omron Social Solutions",
	0x000067: "Corona",
	0x000068: "Aisin",
	0x000069: "Toshiba Lifestyle",
	0x00006A: "Okaya Koki",
	0x00006B: "ISB",
	0x00006C: "Nichicon",
	0x00006E: "Sound Vision",
	0x00006F: "Buffalo",
	0x000071: "Nihon Sangyo",
	0x000072: "Enerlis",
	0x000073: "NEC Platforms",
	0x000076: "TSP",
	0x000077: "Kanagawa Institute of Technology",
	0x000078: "Maxell",
	0x000079: "Anritsu Engineering",
	0x00007A: "Zuken Elmic",
	0x00007C: "NSW",
	0x00007E: "SMK",
	0x00007F: "Anritsu Customer Support",
	0x000080: "Daiazebra Electric (Tabuchi)",
	0x000081: "Iwasaki Electric",
	0x000082: "Purpose",
	0x000083: "Melco Techno Yokohama",
	0x000085: "Takaoka Toko",
	0x000086: "NTT West",
	0x000087: "I-O DATA",
	0x000088: "Chofu Seisakusho",
	0x00008A: "Fujitsu General",
	0x00008C: "Kyuden Technosystems",
	0x00008D: "NTT",
	0x00008F: "Glamo",
	0x000090: "Fujitsu Component",
	0x000091: "NEC Platforms (legacy)",
	0x000093: "Satori Denki",
	0x000095: "Yamato Denki",
	0x000096: "Azbil",
	0x000097: "Mirai Gijutsu Kenkyusho",
	0x000099: "TEPCO Holdings",
	0x00009A: "Kansai Electric Power T&D",
	0x00009B: "Gastar",
	0x00009C: "Diamond Electric",
	0x00009E: "Yaskawa Electric",
	0x00009F: "GS Yuasa",
	0x0000A0: "NTT Advanced Technology",
	0x0000A1: "Honda R&D",
	0x0000A3: "Chubu Electric Power Grid",
	0x0000A5: "Nichibei",
	0x0000A8: "Smart Power System",
	0x0000AC: "IDEC",
	0x0000AD: "Delta Electronics",
	0x0000AE: "Shikoku Electric Power T&D",
	0x0000AF: "Takara Standard",
	0x0000B0: "Idea (Naltec)",
	0x0000B1: "IIJ",
	0x0000B2: "NF Blossom Technologies",
	0x0000B3: "TOPPERS Project",
	0x0000B4: "4R Energy",
	0x0000B5: "Chugoku Electric Power",
	0x0000B6: "Bunka Shutter",
	0x0000B7: "Nitto Kogyo",
	0x0000B8: "Hokkaido Electric Power Network",
	0x0000BA: "Sankyo Tateyama",
	0x0000BB: "Hokuriku Electric Power T&D",
	0x0000BC: "Tohoku Electric Power Network",
	0x0000BE: "Denken",
	0x0000BF: "Kyushu Electric Power T&D",
	0x0000C1: "Tsuken Denki Kogyo",
	0x0000C2: "Tohoku Keiki Kogyo",
	0x0000C3: "Japan Electric Meters Inspection",
	0x0000C5: "Sanwa Shutter",
	0x0000CA: "JSP",
	0x0000CB: "Fuji Electric",
	0x0000CC: "Bosch Home Comfort Japan",
	0x0000CD: "Toclas",
	0x0000CE: "Shindengen Electric",
	0x0000D0: "Tsubakimoto Chain",
	0x0000D2: "Chofu Kosan",
	0x0000D4: "Murata Manufacturing",
	0x0000D5: "Choshu Industry",
	0x0000D7: "Kaga Electronics",
	0x0000D8: "Osaki Datatech",
	0x0000D9: "Toshiba IT Control Systems",
	0x0000DA: "Panasonic Industrial Systems",
	0x0000DB: "Suntech Power Japan",
	0x0000DC: "Nihon Techno",
	0x0000DD: "Ena Stone",
	0x0000DE: "FUJIFILM Business Innovation Japan",
	0x0000DF: "SMA Japan",
	0x0000E0: "Looop",
	0x0000E1: "SoftBank",
	0x0000E2: "NextDrive",
	0x0000E3: "DDL",
	0x0000E4: "Techno-i",
	0x0000E5: "Hitachi Power Solutions",
	0x0000E6: "Hokkaido Electrical Safety Services",
	0x0000E8: "Koizumi Lighting",
	0x0000E9: "NTT Smile Energy",
	0x0000EB: "Nichicon Kameoka",
	0x0000EC: "Toshiba Energy Systems",
	0x0000ED: "Anfini",
	0x0000EE: "Tessera Technology",
	0x0000EF: "Toyota Industries",
	0x0000F0: "Kaneka",
	0x0000F1: "Laplace System",
	0x0000F2: "Energy Solutions",
	0x0000F3: "Energy Gateway",
	0x0000F4: "Denso Aircool",
	0x0000F5: "Odelic",
	0x0000F6: "Field Logic",
	0x0000F7: "J-City",
	0x0000F8: "Simux Initiative",
	0x0000F9: "Toho Electronics",
	0x0000FA: "Plat'Home",
	0x0000FB: "Shiko Giken Kogyo",
	0x0000FC: "Fuji Industrial",
	0x0000FD: "Bellnix",
	0x0000FE: "Panasonic Eco Systems",
	0x0000FF: "TEPCO Energy Partner",
	0x000100: "Smart Solar",
	0x000101: "Sunpot",
	0x000102: "Nichicon Kusatsu",
	0x000103: "Data Technology",
	0x000104: "Next Energy & Resources",
	0x000105: "Mitsubishi Electric Lighting",
	0x000106: "Nature",
	0x000107: "Seiko Electric",
	0x000108: "SOUSEI Technology",
	0x000109: "Denso (kk-denso)",
	0x00010A: "Energy Gap",
	0x00010B: "Kitanihon Densen",
	0x00010C: "Max",
	0x00010D: "Shizen Connect",
	0x00010E: "Sanix",
	0x00010F: "Iwatani",
	0x000110: "Asuka Solution",
	0x000111: "Topre",
	0x000112: "Nichiei Intec",
	0x000113: "Ebara Jitsugyo",
	0x000114: "Okatani Kiden",
	0x000115: "Huawei Japan",
	0x000116: "Sungrow Power Supply",
	0x000117: "WWB",
	0x000118: "NEC Magnus Communications",
	0x000119: "Daihen",
	0x00011A: "ACCESS",
	0x00011B: "SolaX Power",
	0x00011C: "Sanden Retail Systems",
	0x00011D: "mui Lab",
	0x00011E: "Sakigawa",
	0x00011F: "Toyota Tsusho",
	0x000120: "Meisei Electric",
	0x000121: "Toyota Motor",
	0x000122: "Hanwha Q Cells Japan",
	0x000123: "Contec",
	0x000124: "Intec",
	0x000125: "LiveSmart",
	0x000126: "Togami Electric",
	0x000127: "Paloma",
	0x000128: "Saiko Engineering",
	0x000129: "GoodWe Japan",
	0x00012A: "Monochrome",
	0x00012B: "Denso Wave",
	0x00012C: "Onanba",
	0x00012D: "Tachikawa Blind",
	0x00012E: "Enecloud",
	0x00012F: "Taiwan Plastic Japan New Energy",
	0x000130: "Cool Design",
	0x000131: "Eternalplanet Energy (EP Cube)",
	0x000132: "EX4Energy",
	0x000133: "afterFIT",
	0x000134: "GoodWe Technologies",
	0x000135: "Link Japan",
	0x000136: "Chuo Bussan",
	0x000137: "Okatani Kiden (2nd)",
	0x000138: "TRENDE",
	0x000139: "Ratoc Systems",
	0x00013A: "GUGEN",
	0x00013B: "Crossdoor",
	0x00013D: "Nihon Gas (Nicigas)",
	0x00013E: "I-Grid Solutions",
	0x00013F: "Energy Pool Japan",
	0x000140: "Landis+Gyr",
	0x000141: "Daiko Electric",
	0x000142: "Yanekara",
	0x000143: "Tokugyou Energy Japan",
	0x000144: "Sky Electric Japan",
	0x000145: "Tesla Japan",
}

func missingEPCs(requested []byte, props []model.GetResProperty) []byte {
	seen := make(map[byte]struct{}, len(props))
	for _, p := range props {
		seen[p.EPC] = struct{}{}
	}
	missing := make([]byte, 0, len(requested))
	for _, epc := range requested {
		if _, ok := seen[epc]; !ok {
			missing = append(missing, epc)
		}
	}
	return missing
}

func splitEPCs(epcs []byte) ([]byte, []byte) {
	if len(epcs) <= 1 {
		return epcs, nil
	}
	mid := len(epcs) / 2
	if mid == 0 {
		mid = 1
	}
	left := append([]byte(nil), epcs[:mid]...)
	right := append([]byte(nil), epcs[mid:]...)
	return left, right
}

func formatEOJ(eoj [3]byte) string { return fmt.Sprintf("0x%02x%02x%02x", eoj[0], eoj[1], eoj[2]) }

func formatEPCList(epcs []byte) string {
	if len(epcs) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(epcs))
	for _, epc := range epcs {
		parts = append(parts, fmt.Sprintf("0x%02x", epc))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func containsAny(candidates []byte, set []byte) bool {
	lookup := make(map[byte]struct{}, len(set))
	for _, v := range set {
		lookup[v] = struct{}{}
	}
	for _, v := range candidates {
		if _, ok := lookup[v]; ok {
			return true
		}
	}
	return false
}

func propsToMap(props []model.GetResProperty) map[byte]model.GetResProperty {
	out := make(map[byte]model.GetResProperty, len(props))
	for _, p := range props {
		existing, ok := out[p.EPC]
		if !ok || (len(existing.EDT) == 0 && len(p.EDT) > 0) {
			out[p.EPC] = p
		}
	}
	return out
}

func mergeProps(dst map[byte]model.GetResProperty, src []model.GetResProperty) {
	for _, p := range src {
		existing, ok := dst[p.EPC]
		if !ok || (len(existing.EDT) == 0 && len(p.EDT) > 0) {
			dst[p.EPC] = p
		}
	}
}

func mapToProps(props map[byte]model.GetResProperty) []model.GetResProperty {
	keys := make([]int, 0, len(props))
	for epc := range props {
		keys = append(keys, int(epc))
	}
	sort.Ints(keys)
	out := make([]model.GetResProperty, 0, len(keys))
	for _, k := range keys {
		out = append(out, props[byte(k)])
	}
	return out
}

func decodePropertyMap(edt []byte) map[byte]struct{} {
	out := make(map[byte]struct{})
	if len(edt) == 0 {
		return out
	}
	if len(edt) < 17 {
		for i := 1; i < len(edt); i++ {
			out[edt[i]] = struct{}{}
		}
		return out
	}
	for i := 1; i < len(edt); i++ {
		code := byte(i - 1)
		bits := edt[i]
		for bit := 0; bit < 8; bit++ {
			if ((bits >> bit) & 0x01) == 0x01 {
				epc := byte((bit+8)*0x10) + code
				out[epc] = struct{}{}
			}
		}
	}
	return out
}

func decodeUID(edt []byte, host string) string {
	if len(edt) > 1 {
		return hex.EncodeToString(edt[1:])
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return fmt.Sprintf("%03d%03d", int(v4[2]), int(v4[3]))
		}
	}
	return ""
}

func decodeManufacturer(edt []byte) string {
	if len(edt) != 3 {
		return ""
	}
	code := uint32(edt[0])<<16 | uint32(edt[1])<<8 | uint32(edt[2])
	if name, ok := manufacturerNames[code]; ok {
		return name
	}
	return fmt.Sprintf("0x%06X", code)
}

func decodeProductCode(edt []byte) string {
	if len(edt) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(edt), "\x00"))
}

func prop(props []model.GetResProperty, epc byte) ([]byte, bool) {
	for _, p := range props {
		if p.EPC == epc && len(p.EDT) > 0 {
			return p.EDT, true
		}
	}
	return nil, false
}

func parseEDTWithReason(edt []byte, m specs.MetricSpec) (float64, bool, string) {
	size := m.Size
	if size == 0 {
		size = len(edt)
	}
	if size <= 0 {
		return 0, false, "empty EDT for auto-sized metric"
	}
	if len(edt) < size {
		return 0, false, fmt.Sprintf("EDT too short: got=%d need=%d", len(edt), size)
	}

	rawValue, err := parseInteger(edt[:size], m.Signed)
	if err != nil {
		return 0, false, err.Error()
	}
	if m.Invalid != nil {
		if rawValue.Cmp(big.NewInt(int64(*m.Invalid))) == 0 {
			return 0, false, fmt.Sprintf("raw value %s equals invalid sentinel", rawValue.String())
		}
	}

	v, _ := new(big.Float).SetInt(rawValue).Float64()
	v *= m.Scale
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false, "scaled value overflows float64"
	}
	return v, true, ""
}

func parseInteger(raw []byte, signed bool) (*big.Int, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("cannot parse empty integer payload")
	}
	value := new(big.Int).SetBytes(raw)
	if !signed {
		return value, nil
	}
	if raw[0]&0x80 == 0 {
		return value, nil
	}
	twoPow := new(big.Int).Lsh(big.NewInt(1), uint(len(raw)*8))
	value.Sub(value, twoPow)
	return value, nil
}

// ParsePropsToMetrics converts Get_Res properties into metrics using the given metric specs.
func ParsePropsToMetrics(props []model.GetResProperty, metrics []specs.MetricSpec) map[string]MetricValue {
	out := make(map[string]MetricValue)
	for _, m := range metrics {
		edt, ok := prop(props, m.EPC)
		if !ok {
			clientLog.Warnf("missing EPC 0x%02x for metric %q in response", m.EPC, m.Name)
			continue
		}
		v, ok, reason := parseEDTWithReason(edt, m)
		if !ok {
			if strings.Contains(reason, "invalid sentinel") {
				clientLog.Debugf("skipping metric %q (EPC 0x%02x): %s", m.Name, m.EPC, reason)
			} else {
				clientLog.Warnf("bad EPC payload for metric %q (EPC 0x%02x): %s", m.Name, m.EPC, reason)
			}
			continue
		}
		out[m.Name] = MetricValue{Value: v, Type: m.Type}
	}
	return out
}

package echonet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const maxSendAttempts = 2

// Transport manages the shared UDP connection for ECHONET Lite communication.
// It owns the port-3610 socket, per-host serialization locks, and the TID
// counter. Create one Transport and share it across all Client instances.
type Transport struct {
	tidCounter   atomic.Uint32
	hostLockMu   sync.Mutex
	hostLocks    map[string]*sync.Mutex
	fixedConnMu  sync.Mutex
	fixedConn    *net.UDPConn
	waitersMu    sync.Mutex
	waiters      map[string]chan udpFrame
	strictSource bool
}

type udpFrame struct {
	from *net.UDPAddr
	data []byte
}

// NewTransport creates a Transport that manages ECHONET Lite UDP connections.
// If strictSourcePort3610 is true, all requests must originate from local UDP
// port 3610; no fallback to an ephemeral port is attempted.
func NewTransport(strictSourcePort3610 bool) *Transport {
	return &Transport{
		hostLocks:    make(map[string]*sync.Mutex),
		waiters:      make(map[string]chan udpFrame),
		strictSource: strictSourcePort3610,
	}
}

// Close releases the shared UDP connection.
func (t *Transport) Close() error {
	t.fixedConnMu.Lock()
	defer t.fixedConnMu.Unlock()
	if t.fixedConn != nil {
		err := t.fixedConn.Close()
		t.fixedConn = nil
		return err
	}
	return nil
}

// NextTID returns a monotonically increasing transaction ID.
func (t *Transport) NextTID() uint16 {
	return uint16(t.tidCounter.Add(1))
}

// Send sends a raw ECHONET frame to addr and returns the raw response.
// Host-level serialization, connection pooling, retry on timeout, and
// ephemeral-port fallback are handled transparently.
func (t *Transport) Send(ctx context.Context, addr string, req []byte, tid uint16, timeout time.Duration) ([]byte, error) {
	hostKey := normalizeHost(addr)
	hostLock := t.lockForHost(hostKey)
	hostLock.Lock()
	defer hostLock.Unlock()

	host := addr
	if _, _, err := net.SplitHostPort(addr); err != nil {
		host = net.JoinHostPort(addr, fmt.Sprint(echonetPort))
	}
	resp, err := t.sendWithRetry(ctx, host, req, tid, hostKey, echonetPort, timeout)
	if err == nil {
		return resp, nil
	}
	if t.strictSource {
		return nil, fmt.Errorf("failed to send from required local UDP port %d to %s: %w",
			echonetPort, hostKey, err)
	}
	if !isPortBindFailure(err) {
		return nil, err
	}
	clientLog.Warnf("failed to bind local UDP port %d for %s; falling back to ephemeral source port: %v",
		echonetPort, hostKey, err)
	resp, fallbackErr := t.sendFromPort(ctx, host, req, tid, hostKey, 0, timeout)
	if fallbackErr != nil {
		return nil, fmt.Errorf("failed local UDP port %d (%v), ephemeral fallback also failed: %w",
			echonetPort, err, fallbackErr)
	}
	return resp, nil
}

func (t *Transport) sendWithRetry(ctx context.Context, host string, req []byte, tid uint16, hostKey string, localPort int, timeout time.Duration) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := t.sendFromPort(ctx, host, req, tid, hostKey, localPort, timeout)
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

func (t *Transport) sendFromPort(ctx context.Context, host string, req []byte, tid uint16, hostKey string, localPort int, timeout time.Duration) ([]byte, error) {
	remoteAddr, err := net.ResolveUDPAddr("udp4", host)
	if err != nil {
		return nil, err
	}
	if localPort == echonetPort {
		return t.sendViaSharedFixedPort(ctx, remoteAddr, req, tid, hostKey, timeout)
	}
	conn, err := openUDPConn(localPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return writeAndRead(ctx, conn, remoteAddr, req, tid, hostKey, timeout)
}

func (t *Transport) sendViaSharedFixedPort(ctx context.Context, remoteAddr *net.UDPAddr, req []byte, tid uint16, hostKey string, timeout time.Duration) ([]byte, error) {
	conn, err := t.getOrCreateFixedConn()
	if err != nil {
		return nil, err
	}
	key := waiterKey(remoteAddr.IP.String(), tid)
	respCh := make(chan udpFrame, 1)
	t.addWaiter(key, respCh)
	defer t.removeWaiter(key)

	_, writeErr := conn.WriteToUDP(req, remoteAddr)
	if writeErr != nil {
		if !errors.Is(writeErr, net.ErrClosed) {
			return nil, writeErr
		}
		t.resetFixedConn(conn)
		conn, err = t.getOrCreateFixedConn()
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

func (t *Transport) getOrCreateFixedConn() (*net.UDPConn, error) {
	t.fixedConnMu.Lock()
	defer t.fixedConnMu.Unlock()
	if t.fixedConn != nil {
		return t.fixedConn, nil
	}
	conn, err := openUDPConn(echonetPort)
	if err != nil {
		return nil, err
	}
	t.fixedConn = conn
	go t.startFixedConnReceiver(conn)
	return conn, nil
}

func (t *Transport) startFixedConnReceiver(conn *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				t.fixedConnMu.Lock()
				if t.fixedConn == conn {
					t.fixedConn = nil
				}
				t.fixedConnMu.Unlock()
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
		t.waitersMu.Lock()
		ch := t.waiters[key]
		t.waitersMu.Unlock()
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

func (t *Transport) resetFixedConn(old *net.UDPConn) {
	t.fixedConnMu.Lock()
	defer t.fixedConnMu.Unlock()
	if t.fixedConn == old {
		_ = old.Close()
		t.fixedConn = nil
	}
}

func (t *Transport) lockForHost(host string) *sync.Mutex {
	t.hostLockMu.Lock()
	defer t.hostLockMu.Unlock()
	if m, ok := t.hostLocks[host]; ok {
		return m
	}
	m := &sync.Mutex{}
	t.hostLocks[host] = m
	return m
}

func (t *Transport) addWaiter(key string, ch chan udpFrame) {
	t.waitersMu.Lock()
	t.waiters[key] = ch
	t.waitersMu.Unlock()
}

func (t *Transport) removeWaiter(key string) {
	t.waitersMu.Lock()
	delete(t.waiters, key)
	t.waitersMu.Unlock()
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

func waiterKey(host string, tid uint16) string {
	return host + "|" + fmt.Sprintf("%04x", tid)
}

func normalizeHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return addr
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

package echonet

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/model"
	"github.com/styygeli/echonetgo/internal/specs"
)

const maxAdaptiveSplitDepth = 8

var clientLog = logging.New("echonet-client")

// DeviceInfo represents generic identity properties of a device.
type DeviceInfo struct {
	UID          string
	Manufacturer string
	ProductCode  string
}

// MetricValue holds a parsed value and its type (gauge or counter).
type MetricValue struct {
	Value     float64
	Type      string
	EnumLabel string // resolved enum label (empty if not an enum metric)
}

// Client sends ECHONET Lite requests over UDP and parses responses.
type Client struct {
	transport *Transport
	timeout   time.Duration
}

// NewClient creates a client backed by the given Transport with the specified timeout.
func NewClient(transport *Transport, timeoutSec int) *Client {
	return &Client{
		transport: transport,
		timeout:   time.Duration(timeoutSec) * time.Second,
	}
}

// SendGet sends a Get request to addr and returns the raw response.
func (c *Client) SendGet(ctx context.Context, addr string, eoj [3]byte, epcs []byte) ([]byte, error) {
	if len(epcs) == 0 {
		return nil, fmt.Errorf("no EPCs")
	}
	tid := c.transport.NextTID()
	req := GetRequest(tid, eoj, epcs)
	return c.transport.Send(ctx, addr, req, tid, c.timeout)
}

// SendSet sends a SetC request (single property) and returns the raw Set_Res or an error.
func (c *Client) SendSet(ctx context.Context, addr string, eoj [3]byte, epc byte, edt []byte) ([]byte, error) {
	tid := c.transport.NextTID()
	req := SetRequest(tid, eoj, epc, edt)
	return c.transport.Send(ctx, addr, req, tid, c.timeout)
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
	if err != nil && !isGetSNA(err) {
		return nil, err
	}
	if isGetSNA(err) {
		var validProps []model.GetResProperty
		for _, p := range props {
			if p.PDC > 0 {
				validProps = append(validProps, p)
			}
		}
		props = validProps
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

// GetWritablePropertyMap reads EPC 0x9E and decodes writable properties.
func (c *Client) GetWritablePropertyMap(ctx context.Context, addr string, eoj [3]byte) (map[byte]struct{}, error) {
	props, err := c.GetProps(ctx, addr, eoj, []byte{0x9E})
	if err != nil {
		return nil, err
	}
	for _, p := range props {
		if p.EPC == 0x9E && len(p.EDT) > 0 {
			return decodePropertyMap(p.EDT), nil
		}
	}
	clientLog.Warnf("device %s eoj=%s: writable property map (0x9E) missing/empty", normalizeHost(addr), formatEOJ(eoj))
	return nil, fmt.Errorf("writable property map (0x9E) missing")
}

// GetManufacturerCode reads EPC 0x8A and returns the 3-byte manufacturer code as a
// 6-digit lowercase hex string (e.g. "000006"). Used for vendor-specific spec lookup.
// Falls back to node profile EOJ if the class EOJ returns Get_SNA.
func (c *Client) GetManufacturerCode(ctx context.Context, addr string, eoj [3]byte) (string, error) {
	nodeProfileEOJ := [3]byte{0x0E, 0xF0, 0x01}
	props, err := c.GetProps(ctx, addr, eoj, []byte{0x8A})
	if err != nil && !isGetSNA(err) {
		return "", err
	}

	for _, p := range props {
		if p.EPC == 0x8A && len(p.EDT) == 3 {
			return fmt.Sprintf("%02x%02x%02x", p.EDT[0], p.EDT[1], p.EDT[2]), nil
		}
	}

	if eoj != nodeProfileEOJ {
		props, err = c.GetProps(ctx, addr, nodeProfileEOJ, []byte{0x8A})
		if err != nil && !isGetSNA(err) {
			return "", err
		}
		for _, p := range props {
			if p.EPC == 0x8A && len(p.EDT) == 3 {
				return fmt.Sprintf("%02x%02x%02x", p.EDT[0], p.EDT[1], p.EDT[2]), nil
			}
		}
	}

	return "", nil
}

// GetDeviceInfo reads generic identity properties.
// When knownModel is non-empty, 0x8C (product code) is skipped to avoid
// expensive timeouts on devices that don't support it.
func (c *Client) GetDeviceInfo(ctx context.Context, addr string, eoj [3]byte, knownModel string) (DeviceInfo, error) {
	nodeProfileEOJ := [3]byte{0x0E, 0xF0, 0x01}
	epcs := []byte{0x83, 0x8A}
	if knownModel == "" {
		epcs = append(epcs, 0x8C)
	}
	props, err := c.GetProps(ctx, addr, eoj, epcs)
	if err != nil && !isGetSNA(err) {
		return DeviceInfo{}, err
	}

	info := DeviceInfo{}
	if knownModel != "" {
		info.ProductCode = knownModel
	}
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

	if (info.UID == "" || info.Manufacturer == "" || info.ProductCode == "") && eoj != nodeProfileEOJ {
		var missing []byte
		if info.UID == "" {
			missing = append(missing, 0x83)
		}
		if info.Manufacturer == "" {
			missing = append(missing, 0x8A)
		}
		if info.ProductCode == "" {
			missing = append(missing, 0x8C)
		}

		clientLog.Debugf("device %s eoj=%s: missing identity properties %x, retrying via node profile", normalizeHost(addr), formatEOJ(eoj), missing)
		npProps, err := c.GetProps(ctx, addr, nodeProfileEOJ, missing)
		if err != nil && !isGetSNA(err) {
			return DeviceInfo{}, err
		}
		for _, p := range npProps {
			switch p.EPC {
			case 0x83:
				info.UID = decodeUID(p.EDT, normalizeHost(addr))
			case 0x8A:
				info.Manufacturer = decodeManufacturer(p.EDT)
			case 0x8C:
				info.ProductCode = decodeProductCode(p.EDT)
			}
		}
	}

	if info.UID == "" || info.Manufacturer == "" || info.ProductCode == "" {
		clientLog.Warnf("device %s eoj=%s: incomplete device info uid=%q manufacturer=%q product_code=%q",
			normalizeHost(addr), formatEOJ(eoj), info.UID, info.Manufacturer, info.ProductCode)
	}
	return info, nil
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
		mv := MetricValue{Value: v, Type: m.Type}
		if len(m.Enum) > 0 {
			if label, ok := m.Enum[int(v)]; ok {
				mv.EnumLabel = label
			}
		}
		out[m.Name] = mv
	}
	return out
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

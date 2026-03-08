package echonet

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/styygeli/echonetgo/internal/model"
)

const (
	echonetPort    = 3610
	ehd1           = 0x10
	ehd2           = 0x81
	esvGet         = 0x62
	esvGetRes      = 0x72
	esvSetC        = 0x61
	seojController = 0x05
	seojClass      = 0xFF
	seojInstance   = 0x01
	minResponseLen = 12
)

// ESVError is returned when a response carries an unexpected service code.
type ESVError struct {
	ESV byte
}

func (e *ESVError) Error() string {
	return fmt.Sprintf("not Get_Res: ESV=0x%02x", e.ESV)
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

// SetRequest builds an ECHONET Lite SetC frame (single property).
func SetRequest(tid uint16, eoj [3]byte, epc byte, edt []byte) []byte {
	pdc := byte(len(edt))
	n := 4 + 2 + 3 + 3 + 1 + 1 + 2 + len(edt)
	b := make([]byte, 0, n)
	b = append(b, ehd1, ehd2)
	b = append(b, byte(tid>>8), byte(tid))
	b = append(b, seojController, seojClass, seojInstance)
	b = append(b, eoj[0], eoj[1], eoj[2])
	b = append(b, esvSetC)
	b = append(b, 1)
	b = append(b, epc, pdc)
	b = append(b, edt...)
	return b
}

// ParseGetRes parses an ECHONET Lite frame and returns properties if it is a Get_Res.
func ParseGetRes(data []byte) (tid uint16, props []model.GetResProperty, err error) {
	tid, esv, props, err := parseFrame(data)
	if err != nil {
		return 0, nil, err
	}
	if esv != esvGetRes {
		return tid, props, &ESVError{ESV: esv}
	}
	return tid, props, nil
}

func parseFrame(data []byte) (tid uint16, esv byte, props []model.GetResProperty, err error) {
	if len(data) < minResponseLen {
		return 0, 0, nil, fmt.Errorf("response too short: %d", len(data))
	}
	if data[0] != ehd1 || data[1] != ehd2 {
		return 0, 0, nil, fmt.Errorf("invalid EHD: %02x %02x", data[0], data[1])
	}
	tid = binary.BigEndian.Uint16(data[2:4])
	esv = data[10]
	opc := int(data[11])
	pos := 12
	for i := 0; i < opc && pos+2 <= len(data); i++ {
		epc := data[pos]
		pdc := data[pos+1]
		pos += 2
		edtLen := int(pdc)
		if pos+edtLen > len(data) {
			clientLog.Warnf("malformed frame: truncated property data for EPC=0x%02x PDC=%d", epc, pdc)
			break
		}
		edt := make([]byte, edtLen)
		copy(edt, data[pos:pos+edtLen])
		pos += edtLen
		props = append(props, model.GetResProperty{EPC: epc, PDC: pdc, EDT: edt})
	}
	return tid, esv, props, nil
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

func isGetSNA(err error) bool {
	var esvErr *ESVError
	return errors.As(err, &esvErr) && esvErr.ESV == 0x52
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

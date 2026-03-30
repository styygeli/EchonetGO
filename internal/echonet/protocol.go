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
	esvSetI        = 0x60
	esvSetC        = 0x61
	esvGet         = 0x62
	esvSetISNA     = 0x50
	esvSetCSNA     = 0x51
	esvGetRes      = 0x72
	esvSetRes      = 0x71
	esvINF         = 0x73
	esvINFC        = 0x74
	esvINFCRes     = 0x7A
	esvINFSNA      = 0x53
	seojController = 0x05
	seojClass      = 0xFF
	seojInstance   = 0x01
	minResponseLen = 12

	MulticastAddr = "224.0.23.0"
)

// ESVError is returned when a response carries an unexpected service code.
type ESVError struct {
	ESV byte
}

func (e *ESVError) Error() string {
	return fmt.Sprintf("unexpected ESV: 0x%02x", e.ESV)
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

// SetIRequest builds an ECHONET Lite SetI frame (single property, no response expected).
func SetIRequest(tid uint16, eoj [3]byte, epc byte, edt []byte) []byte {
	pdc := byte(len(edt))
	n := 4 + 2 + 3 + 3 + 1 + 1 + 2 + len(edt)
	b := make([]byte, 0, n)
	b = append(b, ehd1, ehd2)
	b = append(b, byte(tid>>8), byte(tid))
	b = append(b, seojController, seojClass, seojInstance)
	b = append(b, eoj[0], eoj[1], eoj[2])
	b = append(b, esvSetI)
	b = append(b, 1)
	b = append(b, epc, pdc)
	b = append(b, edt...)
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

// ParseSetRes parses an ECHONET Lite frame and returns properties if it is a Set_Res.
// Note: Set_Res frames typically have PDC=0 for the properties, but we parse them anyway.
func ParseSetRes(data []byte) (tid uint16, props []model.GetResProperty, err error) {
	tid, esv, props, err := parseFrame(data)
	if err != nil {
		return 0, nil, err
	}
	if esv != esvSetRes {
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

func isSetSNA(err error) bool {
	var esvErr *ESVError
	return errors.As(err, &esvErr) && esvErr.ESV == 0x51
}

func isSetISNA(err error) bool {
	var esvErr *ESVError
	return errors.As(err, &esvErr) && esvErr.ESV == 0x50
}

func formatEOJ(eoj [3]byte) string { return fmt.Sprintf("0x%02x%02x%02x", eoj[0], eoj[1], eoj[2]) }

// FormatEPCList formats a byte slice of EPC codes for logging.
func FormatEPCList(epcs []byte) string {
	return formatEPCList(epcs)
}

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

// INFFrame holds a parsed unsolicited ECHONET Lite notification frame.
type INFFrame struct {
	TID   uint16
	SEOJ  [3]byte
	DEOJ  [3]byte
	ESV   byte
	Props []model.GetResProperty
}

// IsNotification returns true if the ESV is INF, INFC, or INF_SNA.
func (f *INFFrame) IsNotification() bool {
	return f.ESV == esvINF || f.ESV == esvINFC || f.ESV == esvINFSNA
}

// ParseINFFrame parses a raw ECHONET Lite frame into an INFFrame,
// extracting SEOJ, DEOJ, and properties regardless of ESV type.
func ParseINFFrame(data []byte) (*INFFrame, error) {
	tid, esv, props, err := parseFrame(data)
	if err != nil {
		return nil, err
	}
	return &INFFrame{
		TID:  tid,
		SEOJ: [3]byte{data[4], data[5], data[6]},
		DEOJ: [3]byte{data[7], data[8], data[9]},
		ESV:  esv,
		Props: props,
	}, nil
}

// BuildINFCRes builds an INFC_Res (0x7A) frame acknowledging an INFC notification.
func BuildINFCRes(inf *INFFrame) []byte {
	b := make([]byte, 0, 12+3*len(inf.Props))
	b = append(b, ehd1, ehd2)
	b = append(b, byte(inf.TID>>8), byte(inf.TID))
	b = append(b, seojController, seojClass, seojInstance)
	b = append(b, inf.SEOJ[0], inf.SEOJ[1], inf.SEOJ[2])
	b = append(b, esvINFCRes)
	b = append(b, byte(len(inf.Props)))
	for _, p := range inf.Props {
		b = append(b, p.EPC, p.PDC)
		b = append(b, p.EDT...)
	}
	return b
}

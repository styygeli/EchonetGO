package echonet

import (
	"fmt"
	"math"

	"github.com/styygeli/echonetgo/internal/specs"
)

// maxIntegerBytes bounds parseInteger. ECHONET Lite EDT integer properties are
// at most 4 bytes; 8 is the widest an int64 can hold without overflow and is a
// safe ceiling for any realistic property.
const maxIntegerBytes = 8

func parseEDTWithReason(edt []byte, m specs.MetricSpec) (float64, bool, string) {
	size := m.Size
	if size == 0 {
		size = len(edt) - m.Offset
	}
	if size <= 0 {
		return 0, false, "empty EDT for auto-sized metric"
	}
	if len(edt) < m.Offset+size {
		return 0, false, fmt.Sprintf("EDT too short: got=%d need=%d (offset=%d size=%d)", len(edt), m.Offset+size, m.Offset, size)
	}

	rawValue, err := parseInteger(edt[m.Offset:m.Offset+size], m.Signed)
	if err != nil {
		return 0, false, err.Error()
	}
	if m.Invalid != nil && rawValue == int64(*m.Invalid) {
		return 0, false, fmt.Sprintf("raw value %d equals invalid sentinel", rawValue)
	}

	v := float64(rawValue) * m.Scale
	if m.Scale > 0 && m.Scale < 1 {
		digits := int(math.Ceil(-math.Log10(m.Scale)))
		factor := math.Pow(10, float64(digits))
		v = math.Round(v*factor) / factor
	}
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false, "scaled value overflows float64"
	}
	return v, true, ""
}

// parseInteger decodes a big-endian EDT integer (signed or unsigned) into an
// int64. ECHONET integer properties are <= 4 bytes, so int64 holds any value
// — including the full unsigned 4-byte range — without the per-call heap
// allocations that math/big incurred on this hot path.
func parseInteger(raw []byte, signed bool) (int64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("cannot parse empty integer payload")
	}
	if len(raw) > maxIntegerBytes {
		return 0, fmt.Errorf("integer payload too wide: %d bytes (max %d)", len(raw), maxIntegerBytes)
	}
	var u uint64
	for _, b := range raw {
		u = u<<8 | uint64(b)
	}
	if !signed || raw[0]&0x80 == 0 {
		return int64(u), nil
	}
	// Sign-extend a negative two's-complement value of len(raw) bytes.
	return int64(u) - (int64(1) << (uint(len(raw)) * 8)), nil
}

// EncodeValueToEDT encodes a value to EDT bytes for SET requests.
// For enum metrics, value is the raw ECHONET code (e.g. 0x42 for cool).
// For numeric metrics, value is the display value (e.g. 26.0 for 26°C); scale is applied in reverse.
func EncodeValueToEDT(value float64, m specs.MetricSpec) ([]byte, error) {
	size := m.Size
	if size == 0 {
		return nil, fmt.Errorf("metric %s has size 0 (auto), cannot encode for SET", m.Name)
	}
	if size != 1 && size != 2 && size != 4 {
		return nil, fmt.Errorf("metric %s has unsupported size %d for SET", m.Name, size)
	}
	var raw int64
	if len(m.Enum) > 0 {
		raw = int64(math.Round(value))
	} else {
		if m.Scale == 0 {
			m.Scale = 1
		}
		scaled := value / m.Scale
		raw = int64(math.Round(scaled))
	}
	bits := size * 8
	if m.Signed {
		maxPos := int64(1<<(bits-1)) - 1
		minNeg := -int64(1 << (bits - 1))
		if raw > maxPos {
			raw = maxPos
		}
		if raw < minNeg {
			raw = minNeg
		}
	} else {
		if raw < 0 {
			raw = 0
		}
		maxVal := int64(1<<bits) - 1
		if raw > maxVal {
			raw = maxVal
		}
	}
	// raw is clamped to fit `size` bytes above; mask to the low `bits` bits so a
	// negative value becomes its two's-complement representation, then emit
	// big-endian. (bits < 64 here since size is 1, 2, or 4.)
	u := uint64(raw) & ((uint64(1) << uint(bits)) - 1)
	out := make([]byte, size)
	for i := size - 1; i >= 0; i-- {
		out[i] = byte(u)
		u >>= 8
	}
	return out, nil
}

package echonet

import (
	"fmt"
	"math"
	"math/big"

	"github.com/styygeli/echonetgo/internal/specs"
)

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
	val := big.NewInt(raw)
	if m.Signed && raw < 0 {
		twoPow := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		val.Add(val, twoPow)
	}
	b := val.Bytes()
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out, nil
}

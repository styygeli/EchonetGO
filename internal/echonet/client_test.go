package echonet

import (
	"strings"
	"testing"

	"github.com/styygeli/echonetgo/internal/model"
	"github.com/styygeli/echonetgo/internal/specs"
)

func TestParseInteger(t *testing.T) {
	t.Run("unsigned", func(t *testing.T) {
		got, err := parseInteger([]byte{0x01, 0x00}, false)
		if err != nil {
			t.Fatalf("parseInteger() error = %v", err)
		}
		if got.Int64() != 256 {
			t.Fatalf("value = %d, want 256", got.Int64())
		}
	})

	t.Run("signed positive", func(t *testing.T) {
		got, err := parseInteger([]byte{0x7F}, true)
		if err != nil {
			t.Fatalf("parseInteger() error = %v", err)
		}
		if got.Int64() != 127 {
			t.Fatalf("value = %d, want 127", got.Int64())
		}
	})

	t.Run("signed negative", func(t *testing.T) {
		got, err := parseInteger([]byte{0xFF, 0x9C}, true)
		if err != nil {
			t.Fatalf("parseInteger() error = %v", err)
		}
		if got.Int64() != -100 {
			t.Fatalf("value = %d, want -100", got.Int64())
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		_, err := parseInteger(nil, false)
		if err == nil {
			t.Fatalf("parseInteger() expected error, got nil")
		}
	})
}

func TestParseEDTWithReason(t *testing.T) {
	t.Run("auto-sized signed metric", func(t *testing.T) {
		m := specs.MetricSpec{
			Name:   "temperature",
			Size:   0,
			Signed: true,
			Scale:  0.1,
		}
		got, ok, reason := parseEDTWithReason([]byte{0xFF, 0x9C}, m)
		if !ok {
			t.Fatalf("parseEDTWithReason() not ok, reason=%q", reason)
		}
		if got != -10 {
			t.Fatalf("value = %v, want -10", got)
		}
	})

	t.Run("invalid sentinel", func(t *testing.T) {
		invalid := 0x7FFF
		m := specs.MetricSpec{
			Name:    "room_temp",
			Size:    2,
			Scale:   1,
			Invalid: &invalid,
		}
		_, ok, reason := parseEDTWithReason([]byte{0x7F, 0xFF}, m)
		if ok {
			t.Fatalf("parseEDTWithReason() expected !ok")
		}
		if !strings.Contains(reason, "invalid sentinel") {
			t.Fatalf("reason = %q, want invalid sentinel", reason)
		}
	})

	t.Run("short payload", func(t *testing.T) {
		m := specs.MetricSpec{
			Name:  "power",
			Size:  4,
			Scale: 1,
		}
		_, ok, reason := parseEDTWithReason([]byte{0x00, 0x10}, m)
		if ok {
			t.Fatalf("parseEDTWithReason() expected !ok")
		}
		if !strings.Contains(reason, "EDT too short") {
			t.Fatalf("reason = %q, want short payload error", reason)
		}
	})
}

func TestParseGetRes(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		frame := []byte{
			0x10, 0x81,
			0x12, 0x34, // TID
			0x01, 0x30, 0x01, // SEOJ
			0x05, 0xFF, 0x01, // DEOJ
			0x72,             // ESV Get_Res
			0x02,             // OPC
			0x80, 0x01, 0x30, // EPC 0x80 -> 0x30
			0xB3, 0x01, 0x19, // EPC 0xB3 -> 0x19
		}
		tid, props, err := ParseGetRes(frame)
		if err != nil {
			t.Fatalf("ParseGetRes() error = %v", err)
		}
		if tid != 0x1234 {
			t.Fatalf("tid = 0x%04x, want 0x1234", tid)
		}
		if len(props) != 2 {
			t.Fatalf("len(props) = %d, want 2", len(props))
		}
		if props[0].EPC != 0x80 || props[1].EPC != 0xB3 {
			t.Fatalf("unexpected EPCs: %#v", props)
		}
	})

	t.Run("wrong esv", func(t *testing.T) {
		frame := []byte{
			0x10, 0x81,
			0x00, 0x01,
			0x01, 0x30, 0x01,
			0x05, 0xFF, 0x01,
			0x71, // not Get_Res
			0x00,
		}
		_, _, err := ParseGetRes(frame)
		if err == nil {
			t.Fatalf("ParseGetRes() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not Get_Res") {
			t.Fatalf("error = %q, want not Get_Res", err)
		}
	})
}

func TestDecodePropertyMap(t *testing.T) {
	t.Run("short list format", func(t *testing.T) {
		got := decodePropertyMap([]byte{0x02, 0x80, 0xB3})
		if _, ok := got[0x80]; !ok {
			t.Fatalf("expected EPC 0x80 in map")
		}
		if _, ok := got[0xB3]; !ok {
			t.Fatalf("expected EPC 0xB3 in map")
		}
	})

	t.Run("bitmap format", func(t *testing.T) {
		edt := make([]byte, 17)
		edt[0] = 0x01
		edt[1] = 0x01 // bit0 set => EPC 0x80
		got := decodePropertyMap(edt)
		if _, ok := got[0x80]; !ok {
			t.Fatalf("expected EPC 0x80 in bitmap map")
		}
	})
}

func TestParsePropsToMetrics(t *testing.T) {
	props := []model.GetResProperty{
		{EPC: 0xE0, PDC: 2, EDT: []byte{0x00, 0x64}},
	}
	metrics := []specs.MetricSpec{
		{EPC: 0xE0, Name: "generated_power", Size: 2, Scale: 0.1, Type: "gauge"},
		{EPC: 0xE1, Name: "missing_metric", Size: 2, Scale: 1, Type: "gauge"},
	}

	out := ParsePropsToMetrics(props, metrics)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	got, ok := out["generated_power"]
	if !ok {
		t.Fatalf("expected generated_power metric")
	}
	if got.Value != 10 {
		t.Fatalf("generated_power = %v, want 10", got.Value)
	}
	if _, exists := out["missing_metric"]; exists {
		t.Fatalf("did not expect missing_metric in output")
	}
}

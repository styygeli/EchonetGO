package echonet

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/styygeli/echonetgo/internal/model"
	"github.com/styygeli/echonetgo/internal/specs"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

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

func TestIsTimeoutError(t *testing.T) {
	t.Run("net timeout error", func(t *testing.T) {
		if !isTimeoutError(timeoutErr{}) {
			t.Fatalf("expected timeoutErr to be detected as timeout")
		}
	})

	t.Run("non timeout error", func(t *testing.T) {
		if isTimeoutError(errors.New("permission denied")) {
			t.Fatalf("did not expect non-timeout error to be detected as timeout")
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

	t.Run("wrong esv returns ESVError", func(t *testing.T) {
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
		var esvErr *ESVError
		if !errors.As(err, &esvErr) {
			t.Fatalf("error type = %T, want *ESVError", err)
		}
		if esvErr.ESV != 0x71 {
			t.Fatalf("ESV = 0x%02x, want 0x71", esvErr.ESV)
		}
		if !strings.Contains(err.Error(), "not Get_Res") {
			t.Fatalf("error = %q, want 'not Get_Res' substring", err)
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
	if got.EnumLabel != "" {
		t.Fatalf("non-enum metric EnumLabel = %q, want empty", got.EnumLabel)
	}
	if _, exists := out["missing_metric"]; exists {
		t.Fatalf("did not expect missing_metric in output")
	}
}

func TestParsePropsToMetrics_EnumLabel(t *testing.T) {
	props := []model.GetResProperty{
		{EPC: 0x80, PDC: 1, EDT: []byte{0x30}}, // 0x30 = ON
		{EPC: 0xB0, PDC: 1, EDT: []byte{0x42}}, // 0x42 = cool
	}
	metrics := []specs.MetricSpec{
		{
			EPC: 0x80, Name: "operation_status", Size: 1, Scale: 1, Type: "gauge",
			Enum: map[int]string{0x30: "on", 0x31: "off"},
		},
		{
			EPC: 0xB0, Name: "operation_mode", Size: 1, Scale: 1, Type: "gauge",
			Enum: map[int]string{0x41: "auto", 0x42: "cool", 0x43: "heat"},
		},
	}

	out := ParsePropsToMetrics(props, metrics)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if got := out["operation_status"].EnumLabel; got != "on" {
		t.Fatalf("operation_status EnumLabel = %q, want on", got)
	}
	if got := out["operation_mode"].EnumLabel; got != "cool" {
		t.Fatalf("operation_mode EnumLabel = %q, want cool", got)
	}
	// Value is still the raw number
	if out["operation_status"].Value != 0x30 {
		t.Fatalf("operation_status Value = %v, want 48 (0x30)", out["operation_status"].Value)
	}
}

func TestIsGetSNA(t *testing.T) {
	t.Run("ESV 0x52 is Get_SNA", func(t *testing.T) {
		err := &ESVError{ESV: 0x52}
		if !isGetSNA(err) {
			t.Fatal("expected isGetSNA to return true for ESV 0x52")
		}
	})

	t.Run("other ESV is not Get_SNA", func(t *testing.T) {
		err := &ESVError{ESV: 0x71}
		if isGetSNA(err) {
			t.Fatal("expected isGetSNA to return false for ESV 0x71")
		}
	})

	t.Run("non-ESVError is not Get_SNA", func(t *testing.T) {
		if isGetSNA(errors.New("some other error")) {
			t.Fatal("expected isGetSNA to return false for non-ESVError")
		}
	})

	t.Run("wrapped ESVError is detected", func(t *testing.T) {
		inner := &ESVError{ESV: 0x52}
		err := fmt.Errorf("wrapped: %w", inner)
		if !isGetSNA(err) {
			t.Fatal("expected isGetSNA to return true for wrapped ESV 0x52")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		if isGetSNA(nil) {
			t.Fatal("expected isGetSNA to return false for nil")
		}
	})
}

func TestNextTIDIsMonotonic(t *testing.T) {
	a := nextTID()
	b := nextTID()
	if b != a+1 {
		t.Fatalf("expected sequential TIDs: got %d then %d", a, b)
	}
}

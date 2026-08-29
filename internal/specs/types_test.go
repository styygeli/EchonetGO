package specs

import (
	"testing"
)

func TestClimateSpec_HandlesEPC(t *testing.T) {
	cl := &ClimateSpec{
		ModeEPC:               0xB0,
		TemperatureEPC:        0xB3,
		CurrentTemperatureEPC: 0xBB,
		FanModeEPC:            0xA0,
	}

	for _, epc := range []byte{0x80, 0xB0, 0xB3, 0xBB, 0xA0} {
		if !cl.HandlesEPC(epc) {
			t.Errorf("expected ClimateSpec to handle EPC 0x%02x", epc)
		}
	}
	if cl.HandlesEPC(0xE0) {
		t.Errorf("did not expect ClimateSpec to handle EPC 0xE0")
	}

	var nilCl *ClimateSpec
	if nilCl.HandlesEPC(0x80) {
		t.Errorf("nil ClimateSpec should return false for any EPC")
	}
}

func TestLightSpec_HandlesEPC(t *testing.T) {
	lt := &LightSpec{
		BrightnessEPC:   0xB0,
		ColorSettingEPC: 0xB1,
		SceneEPC:        0xC0,
	}

	for _, epc := range []byte{0x80, 0xB0, 0xB1, 0xC0} {
		if !lt.HandlesEPC(epc) {
			t.Errorf("expected LightSpec to handle EPC 0x%02x", epc)
		}
	}
	if lt.HandlesEPC(0xE0) {
		t.Errorf("did not expect LightSpec to handle EPC 0xE0")
	}

	var nilLt *LightSpec
	if nilLt.HandlesEPC(0x80) {
		t.Errorf("nil LightSpec should return false for any EPC")
	}
}

func TestMetricSpecLookups(t *testing.T) {
	specs := []MetricSpec{
		{EPC: 0x80, Name: "operation_status"},
		{EPC: 0xB0, Name: "operation_mode"},
	}

	if ms := FindMetricSpecByEPC(specs, 0xB0); ms == nil || ms.Name != "operation_mode" {
		t.Errorf("FindMetricSpecByEPC(0xB0) failed: got %v", ms)
	}
	if ms := FindMetricSpecByEPC(specs, 0x99); ms != nil {
		t.Errorf("FindMetricSpecByEPC(0x99) should return nil, got %v", ms)
	}

	if ms := FindMetricSpecByName(specs, "operation_status"); ms == nil || ms.EPC != 0x80 {
		t.Errorf("FindMetricSpecByName(operation_status) failed: got %v", ms)
	}
	if ms := FindMetricSpecByName(specs, "unknown"); ms != nil {
		t.Errorf("FindMetricSpecByName(unknown) should return nil, got %v", ms)
	}

	if name := MetricNameForEPC(specs, 0x80); name != "operation_status" {
		t.Errorf("MetricNameForEPC(0x80) = %q, want operation_status", name)
	}
	if name := MetricNameForEPC(specs, 0x99); name != "" {
		t.Errorf("MetricNameForEPC(0x99) = %q, want empty", name)
	}
}

func TestWritableEntityType(t *testing.T) {
	cases := []struct {
		name string
		ms   MetricSpec
		want string
	}{
		{
			name: "excluded",
			ms:   MetricSpec{ExcludeSet: true, Enum: map[int]string{0x30: "on", 0x31: "off"}},
			want: "",
		},
		{
			name: "switch with on/off",
			ms:   MetricSpec{Enum: map[int]string{0x30: "ON", 0x31: "OFF"}},
			want: "switch",
		},
		{
			name: "select with 2 non-on-off values",
			ms:   MetricSpec{Enum: map[int]string{0x41: "charge", 0x42: "discharge"}},
			want: "select",
		},
		{
			name: "select with multiple values",
			ms:   MetricSpec{Enum: map[int]string{1: "low", 2: "med", 3: "high"}},
			want: "select",
		},
		{
			name: "number with no enum",
			ms:   MetricSpec{Size: 2},
			want: "number",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WritableEntityType(tc.ms); got != tc.want {
				t.Errorf("WritableEntityType() = %q, want %q", got, tc.want)
			}
		})
	}
}

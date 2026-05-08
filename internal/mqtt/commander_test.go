package mqtt

import (
	"testing"

	"github.com/styygeli/echonetgo/internal/specs"
)

func TestMetricSpecByName(t *testing.T) {
	metricSpecs := []specs.MetricSpec{
		{EPC: 0x80, Name: "operation_status"},
		{EPC: 0xB0, Name: "operation_mode"},
		{EPC: 0xB3, Name: "set_temperature_celsius"},
	}
	if got := metricSpecByName(metricSpecs, "operation_mode"); got == nil || got.EPC != 0xB0 {
		t.Fatalf("metricSpecByName(operation_mode) = %v, want EPC 0xB0", got)
	}
	if got := metricSpecByName(metricSpecs, "missing"); got != nil {
		t.Fatalf("metricSpecByName(missing) = %v, want nil", got)
	}
}

func TestNormalizeClimateSetpoint(t *testing.T) {
	intMs := specs.MetricSpec{Scale: 1}
	autoMs := specs.MetricSpec{Scale: 0}
	fineMs := specs.MetricSpec{Scale: 0.1}

	cases := []struct {
		name    string
		req     float64
		prev    float64
		hasPrev bool
		ms      specs.MetricSpec
		want    float64
	}{
		{"integer passthrough", 22.0, 22.0, true, intMs, 22.0},
		{"half up when raising", 22.5, 22.0, true, intMs, 23.0},
		{"half down when lowering", 22.5, 23.0, true, intMs, 22.0},
		{"half equal to prev rounds up", 22.5, 22.5, true, intMs, 23.0},
		{"half no prev uses math.Round", 22.5, 0, false, intMs, 23.0},
		{"sub-half fraction nearest", 22.3, 22.0, true, intMs, 22.0},
		{"super-half fraction nearest", 22.7, 22.0, true, intMs, 23.0},
		{"scale zero treated as one", 22.5, 22.0, true, autoMs, 23.0},
		{"sub-integer scale passthrough", 22.5, 22.0, true, fineMs, 22.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeClimateSetpoint(tc.req, tc.prev, tc.hasPrev, tc.ms)
			if got != tc.want {
				t.Fatalf("normalizeClimateSetpoint(%v, %v, %v, scale=%v) = %v, want %v",
					tc.req, tc.prev, tc.hasPrev, tc.ms.Scale, got, tc.want)
			}
		})
	}
}

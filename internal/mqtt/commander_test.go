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

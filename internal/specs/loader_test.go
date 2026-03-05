package specs

import (
	"strings"
	"testing"
	"time"
)

func TestParseDeviceYAML_AllowsAutoSizeEnumAndAppliesDefaults(t *testing.T) {
	data := []byte(`
eoj: [0x01, 0x30, 0x01]
description: "test spec"
metrics:
  - epc: 0x80
    name: operation_status
    size: 0
    scale: 0
    type: gauge
    enum:
      0x123: custom_state
`)

	spec, err := parseDeviceYAML(data)
	if err != nil {
		t.Fatalf("parseDeviceYAML() error = %v", err)
	}
	if spec.DefaultScrapeInterval != time.Minute {
		t.Fatalf("DefaultScrapeInterval = %v, want %v", spec.DefaultScrapeInterval, time.Minute)
	}
	if len(spec.Metrics) != 1 {
		t.Fatalf("len(Metrics) = %d, want 1", len(spec.Metrics))
	}
	m := spec.Metrics[0]
	if m.Scale != 1 {
		t.Fatalf("Scale = %v, want 1", m.Scale)
	}
	if m.Help != "Operation status" {
		t.Fatalf("Help = %q, want %q", m.Help, "Operation status")
	}
	if got := m.Enum[0x123]; got != "custom_state" {
		t.Fatalf("Enum[0x123] = %q, want %q", got, "custom_state")
	}
}

func TestParseDeviceYAML_RejectsEnumWithNonUnitScale(t *testing.T) {
	data := []byte(`
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 0x80
    name: operation_status
    size: 1
    scale: 0.5
    type: gauge
    enum:
      0x30: on
`)

	_, err := parseDeviceYAML(data)
	if err == nil {
		t.Fatalf("parseDeviceYAML() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "enum mapping requires scale=1") {
		t.Fatalf("error = %q, want enum scale validation", err)
	}
}

func TestParseDeviceYAML_RejectsOutOfRangeEnumForFixedSize(t *testing.T) {
	data := []byte(`
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 0x80
    name: operation_status
    size: 1
    type: gauge
    enum:
      0x1FF: invalid
`)

	_, err := parseDeviceYAML(data)
	if err == nil {
		t.Fatalf("parseDeviceYAML() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "doesn't fit size=1") {
		t.Fatalf("error = %q, want enum fit validation", err)
	}
}

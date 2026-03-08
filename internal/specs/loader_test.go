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
	if m.ReverseEnum == nil || m.ReverseEnum["custom_state"] != 0x123 {
		t.Fatalf("ReverseEnum[custom_state] = %v, want 0x123 (ReverseEnum populated from Enum)", m.ReverseEnum)
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

func TestInferHAMetadata_Enum(t *testing.T) {
	dc, sc, u := inferHAMetadata("operation_status", "gauge", true)
	if dc != "enum" || sc != "" || u != "" {
		t.Fatalf("inferHAMetadata(enum) = %q, %q, %q; want enum, \"\", \"\"", dc, sc, u)
	}
}

func TestInferHAMetadata_PowerAndWatts(t *testing.T) {
	for _, name := range []string{"instantaneous_power_w", "circuit_01_power_w", "some_watts"} {
		dc, sc, u := inferHAMetadata(name, "gauge", false)
		if dc != "power" || sc != "measurement" || u != "W" {
			t.Fatalf("inferHAMetadata(%q) = %q, %q, %q; want power, measurement, W", name, dc, sc, u)
		}
	}
}

func TestInferHAMetadata_KWh(t *testing.T) {
	dc, sc, u := inferHAMetadata("cumulative_generation_kwh", "counter", false)
	if dc != "energy" || sc != "total_increasing" || u != "kWh" {
		t.Fatalf("counter kwh: got %q, %q, %q; want energy, total_increasing, kWh", dc, sc, u)
	}
	dc, sc, u = inferHAMetadata("some_kwh", "gauge", false)
	if dc != "energy" || sc != "measurement" || u != "kWh" {
		t.Fatalf("gauge kwh: got %q, %q, %q; want energy, measurement, kWh", dc, sc, u)
	}
}

func TestInferHAMetadata_Wh(t *testing.T) {
	dc, sc, u := inferHAMetadata("cumulative_charge_wh", "counter", false)
	if dc != "energy" || sc != "total_increasing" || u != "Wh" {
		t.Fatalf("counter wh: got %q, %q, %q; want energy, total_increasing, Wh", dc, sc, u)
	}
	dc, sc, u = inferHAMetadata("remaining_capacity_wh", "gauge", false)
	if dc != "energy" || sc != "measurement" || u != "Wh" {
		t.Fatalf("gauge wh: got %q, %q, %q; want energy, measurement, Wh", dc, sc, u)
	}
}

func TestInferHAMetadata_CelsiusAndPercent(t *testing.T) {
	dc, sc, u := inferHAMetadata("indoor_temperature_celsius", "gauge", false)
	if dc != "temperature" || sc != "measurement" || u != "°C" {
		t.Fatalf("celsius: got %q, %q, %q; want temperature, measurement, °C", dc, sc, u)
	}
	dc, sc, u = inferHAMetadata("state_of_capacity_percent", "gauge", false)
	if dc != "" || sc != "measurement" || u != "%" {
		t.Fatalf("percent: got %q, %q, %q; want \"\", measurement, %%", dc, sc, u)
	}
}

func TestInferHAMetadata_M3(t *testing.T) {
	dc, sc, u := inferHAMetadata("cumulative_flow_m3", "counter", false)
	if dc != "volume" || sc != "total_increasing" || u != "m³" {
		t.Fatalf("counter m3: got %q, %q, %q; want volume, total_increasing, m³", dc, sc, u)
	}
	dc, sc, u = inferHAMetadata("some_m3", "gauge", false)
	if dc != "volume" || sc != "measurement" || u != "m³" {
		t.Fatalf("gauge m3: got %q, %q, %q; want volume, measurement, m³", dc, sc, u)
	}
}

func TestInferHAMetadata_PlainGaugeAndCounter(t *testing.T) {
	dc, sc, u := inferHAMetadata("raw_value", "gauge", false)
	if dc != "" || sc != "measurement" || u != "" {
		t.Fatalf("plain gauge: got %q, %q, %q; want \"\", measurement, \"\"", dc, sc, u)
	}
	dc, sc, u = inferHAMetadata("some_counter", "counter", false)
	if dc != "" || sc != "total_increasing" || u != "" {
		t.Fatalf("plain counter: got %q, %q, %q; want \"\", total_increasing, \"\"", dc, sc, u)
	}
}

func TestParseDeviceYAML_ExplicitHAFieldsOverrideInference(t *testing.T) {
	data := []byte(`
eoj: [0x02, 0x79, 0x01]
description: "solar"
metrics:
  - epc: 0xE0
    name: instantaneous_generation_watts
    size: 2
    scale: 1
    type: gauge
    ha_device_class: power
    ha_state_class: measurement
    ha_unit: "W"
  - epc: 0xE1
    name: cumulative_generation_kwh
    size: 4
    scale: 0.001
    type: counter
    ha_device_class: energy
    ha_state_class: total_increasing
    ha_unit: kWh
`)
	spec, err := parseDeviceYAML(data)
	if err != nil {
		t.Fatalf("parseDeviceYAML() error = %v", err)
	}
	if len(spec.Metrics) != 2 {
		t.Fatalf("len(Metrics) = %d, want 2", len(spec.Metrics))
	}
	m0 := spec.Metrics[0]
	if m0.HADeviceClass != "power" || m0.HAStateClass != "measurement" || m0.HAUnit != "W" {
		t.Fatalf("metric 0 HA: %q, %q, %q; want power, measurement, W", m0.HADeviceClass, m0.HAStateClass, m0.HAUnit)
	}
	m1 := spec.Metrics[1]
	if m1.HADeviceClass != "energy" || m1.HAStateClass != "total_increasing" || m1.HAUnit != "kWh" {
		t.Fatalf("metric 1 HA: %q, %q, %q; want energy, total_increasing, kWh", m1.HADeviceClass, m1.HAStateClass, m1.HAUnit)
	}
}

func TestParseDeviceYAML_InferenceWhenHAFieldsOmitted(t *testing.T) {
	data := []byte(`
eoj: [0x01, 0x30, 0x01]
description: "ac"
metrics:
  - epc: 0xBB
    name: indoor_temperature_celsius
    size: 1
    signed: true
    scale: 1
    type: gauge
  - epc: 0x80
    name: operation_status
    size: 1
    type: gauge
    enum:
      0x30: on
      0x31: off
`)
	spec, err := parseDeviceYAML(data)
	if err != nil {
		t.Fatalf("parseDeviceYAML() error = %v", err)
	}
	if len(spec.Metrics) != 2 {
		t.Fatalf("len(Metrics) = %d, want 2", len(spec.Metrics))
	}
	m0 := spec.Metrics[0]
	if m0.HADeviceClass != "temperature" || m0.HAStateClass != "measurement" || m0.HAUnit != "°C" {
		t.Fatalf("temperature inference: %q, %q, %q; want temperature, measurement, °C", m0.HADeviceClass, m0.HAStateClass, m0.HAUnit)
	}
	m1 := spec.Metrics[1]
	if m1.HADeviceClass != "enum" || m1.HAStateClass != "" || m1.HAUnit != "" {
		t.Fatalf("enum inference: %q, %q, %q; want enum, \"\", \"\"", m1.HADeviceClass, m1.HAStateClass, m1.HAUnit)
	}
}

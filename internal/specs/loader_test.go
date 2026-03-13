package specs

import (
	"os"
	"path/filepath"
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

// TestMergeSuperClass_InjectsDefaultsWhenNotInYAML verifies that loading a spec
// that does not define 0x80/0x88 gets Super Class defaults merged in.
func TestMergeSuperClass_InjectsDefaultsWhenNotInYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`
eoj: [0x02, 0x79, 0x01]
description: "solar"
metrics:
  - epc: 0xE0
    name: instantaneous_generation_watts
    size: 2
    scale: 1
    type: gauge
`)
	if err := os.WriteFile(filepath.Join(dir, "home_solar.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	specs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	spec, ok := specs["home_solar"]
	if !ok {
		t.Fatalf("Load() did not return home_solar")
	}
	epcs := make(map[byte]string)
	for _, m := range spec.Metrics {
		epcs[m.EPC] = m.Name
	}
	if _, ok := epcs[0x80]; !ok {
		t.Errorf("expected Super Class metric 0x80 (operation_status) to be merged in, got metrics by EPC: %v", epcs)
	}
	if _, ok := epcs[0x88]; !ok {
		t.Errorf("expected Super Class metric 0x88 (fault_status) to be merged in, got metrics by EPC: %v", epcs)
	}
	if name := epcs[0x80]; name != "operation_status" {
		t.Errorf("EPC 0x80 name = %q, want operation_status", name)
	}
	if name := epcs[0x88]; name != "fault_status" {
		t.Errorf("EPC 0x88 name = %q, want fault_status", name)
	}
}

// TestMergeSuperClass_ClassYAMLOverridesByEPC verifies that when a class defines
// the same EPC as a Super Class default, the class definition wins.
func TestMergeSuperClass_ClassYAMLOverridesByEPC(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`
eoj: [0x01, 0x30, 0x01]
description: "ac"
metrics:
  - epc: 0x80
    name: operation_status
    help: "Custom operation status."
    size: 1
    type: gauge
    enum:
      0x30: on
      0x31: off
      0x99: custom_state
`)
	if err := os.WriteFile(filepath.Join(dir, "home_ac.yaml"), yaml, 0644); err != nil {
		t.Fatal(err)
	}
	specs, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	spec, ok := specs["home_ac"]
	if !ok {
		t.Fatalf("Load() did not return home_ac")
	}
	var op *MetricSpec
	for i := range spec.Metrics {
		if spec.Metrics[i].EPC == 0x80 {
			op = &spec.Metrics[i]
			break
		}
	}
	if op == nil {
		t.Fatal("expected one metric with EPC 0x80")
	}
	if op.Help != "Custom operation status." {
		t.Errorf("Help = %q, want class YAML override", op.Help)
	}
	if op.ReverseEnum["custom_state"] != 0x99 {
		t.Errorf("class YAML enum override: ReverseEnum[custom_state] = %v, want 0x99", op.ReverseEnum["custom_state"])
	}
}

// TestLoad_NoUnexpectedAutoSize scans all curated spec files and asserts that
// size: 0 (auto-detect) only appears on known composite/log EPCs that cannot be
// represented as a single scalar metric. Any new size: 0 must be added to the
// allowlist below so it is intentional.
func TestLoad_NoUnexpectedAutoSize(t *testing.T) {
	cwd, _ := os.Getwd()
	specsDir := filepath.Join(cwd, "..", "..", "etc", "specs")
	if _, err := os.Stat(specsDir); err != nil {
		t.Skipf("etc/specs not found at %s: %v", specsDir, err)
	}
	specs, err := Load(specsDir)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", specsDir, err)
	}

	type autoSizeKey struct {
		specName string
		epc      byte
	}
	allowed := map[autoSizeKey]bool{
		{"electric_energy_sensor", 0xE4}: true, // 48×4B composite read-only measurement log
	}

	var violations []string
	for name, spec := range specs {
		for _, m := range spec.Metrics {
			if m.Size == 0 {
				key := autoSizeKey{name, m.EPC}
				if !allowed[key] {
					violations = append(violations, name+"/"+m.Name)
				}
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("unexpected size: 0 (auto) metrics; fix the spec or add to allowlist:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestLoad_VendorSpecMergesCorrectly verifies that loading from the real specs
// dir succeeds and vendor-specific specs (e.g. home_ac_000006) load and receive
// Super Class merge like any other class.
func TestLoad_VendorSpecMergesCorrectly(t *testing.T) {
	cwd, _ := os.Getwd()
	// From internal/specs, repo root is ../..
	specsDir := filepath.Join(cwd, "..", "..", "etc", "specs")
	if _, err := os.Stat(specsDir); err != nil {
		t.Skipf("etc/specs not found at %s: %v", specsDir, err)
	}
	specs, err := Load(specsDir)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", specsDir, err)
	}
	if _, ok := specs["home_ac"]; !ok {
		t.Errorf("Load() did not return home_ac")
	}
	vendor, ok := specs["home_ac_000006"]
	if !ok {
		t.Skip("home_ac_000006 spec not present")
	}
	if len(vendor.Metrics) == 0 {
		t.Errorf("vendor spec home_ac_000006 has no metrics")
	}
	hasOp := false
	for _, m := range vendor.Metrics {
		if m.EPC == 0x80 && m.Name == "operation_status" {
			hasOp = true
			break
		}
	}
	if !hasOp {
		t.Errorf("vendor spec should have operation_status (0x80) from YAML or Super Class merge")
	}
}

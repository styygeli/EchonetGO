package mqtt

import (
	"encoding/json"
	"testing"
)

func TestFriendlyDeviceName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single word", "breaker_box", "Breaker Box"},
		{"multiple words", "epcube_battery", "Epcube Battery"},
		{"already spaced", "ac_av", "Ac Av"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := friendlyDeviceName(tt.in)
			if got != tt.want {
				t.Fatalf("friendlyDeviceName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFriendlyMetricName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"instantaneous_power_w", "Instantaneous Power W"},
		{"indoor_temperature_celsius", "Indoor Temperature Celsius"},
		{"operation_status", "Operation Status"},
	}
	for _, tt := range tests {
		got := friendlyMetricName(tt.in)
		if got != tt.want {
			t.Fatalf("friendlyMetricName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHaDiscoveryPayload_JSONStructure(t *testing.T) {
	precision := 1
	payload := haDiscoveryPayload{
		Name:                "Instantaneous Power W",
		UniqueID:             "echonetgo_breaker_box_instantaneous_power_w",
		StateTopic:           "echonetgo/breaker_box/state",
		ValueTemplate:        "{{ value_json.instantaneous_power_w }}",
		DeviceClass:          "power",
		StateClass:           "measurement",
		UnitOfMeasurement:    "W",
		AvailabilityTopic:         "echonetgo/breaker_box/availability",
		ExpireAfter:               300,
		ForceUpdate:               true,
		SuggestedDisplayPrecision: &precision,
		Device: haDevice{
			Identifiers: []string{"echonetgo_breaker_box"},
			Name:        "Breaker Box",
			ViaDevice:   "echonetgo",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["device_class"] != "power" {
		t.Fatalf("device_class = %v, want power", decoded["device_class"])
	}
	if decoded["state_class"] != "measurement" {
		t.Fatalf("state_class = %v, want measurement", decoded["state_class"])
	}
	if decoded["unit_of_measurement"] != "W" {
		t.Fatalf("unit_of_measurement = %v, want W", decoded["unit_of_measurement"])
	}
	device, _ := decoded["device"].(map[string]interface{})
	if device == nil {
		t.Fatal("device missing")
	}
	if ids, _ := device["identifiers"].([]interface{}); len(ids) == 0 {
		t.Fatal("device.identifiers empty")
	}
}

func TestHaDiscoveryPayload_EnergySensor(t *testing.T) {
	payload := haDiscoveryPayload{
		Name:               "Cumulative Generation Kwh",
		UniqueID:           "echonetgo_panel_solar_cumulative_generation_kwh",
		StateTopic:         "echonetgo/panel_solar/state",
		ValueTemplate:      "{{ value_json.cumulative_generation_kwh }}",
		DeviceClass:        "energy",
		StateClass:         "total_increasing",
		UnitOfMeasurement:  "kWh",
		AvailabilityTopic:   "echonetgo/panel_solar/availability",
		ExpireAfter:        300,
		ForceUpdate:        true,
		Device: haDevice{
			Identifiers: []string{"echonetgo_panel_solar"},
			Name:        "Panel Solar",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["device_class"] != "energy" || decoded["state_class"] != "total_increasing" || decoded["unit_of_measurement"] != "kWh" {
		t.Fatalf("energy sensor: %v", decoded)
	}
}

func TestHaDiscoveryPayload_EnumSensor(t *testing.T) {
	payload := haDiscoveryPayload{
		Name:              "Operation Status",
		UniqueID:          "echonetgo_ac_av_operation_status",
		StateTopic:        "echonetgo/ac_av/state",
		ValueTemplate:     "{{ value_json.operation_status_str | default(value_json.operation_status) }}",
		DeviceClass:       "enum",
		AvailabilityTopic: "echonetgo/ac_av/availability",
		ExpireAfter:       300,
		ForceUpdate:       true,
		Options:           []string{"on", "off"},
		Device: haDevice{
			Identifiers: []string{"echonetgo_ac_av"},
			Name:        "Ac Av",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["device_class"] != "enum" {
		t.Fatalf("device_class = %v, want enum", decoded["device_class"])
	}
	opts, _ := decoded["options"].([]interface{})
	if len(opts) != 2 || opts[0] != "on" || opts[1] != "off" {
		t.Fatalf("options = %v, want [on off]", opts)
	}
}

func TestStatePayloadWithEnumLabels(t *testing.T) {
	state := map[string]interface{}{
		"operation_status":    0x30,
		"operation_status_str": "on",
		"indoor_temperature_celsius": 24.5,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["operation_status_str"] != "on" {
		t.Fatalf("operation_status_str = %v, want on", decoded["operation_status_str"])
	}
	if decoded["indoor_temperature_celsius"] != 24.5 {
		t.Fatalf("indoor_temperature_celsius = %v", decoded["indoor_temperature_celsius"])
	}
}

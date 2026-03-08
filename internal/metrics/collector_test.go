package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

func TestCollectorEmitsScrapeMetrics(t *testing.T) {
	cfg := &config.Config{
		Devices: []config.Device{
			{Name: "test_battery", IP: "192.168.1.10", Class: "storage_battery"},
		},
	}
	deviceSpecs := map[string]*specs.DeviceSpec{
		"storage_battery": {
			Metrics: []specs.MetricSpec{
				{EPC: 0xE4, Name: "soc_percent", Help: "State of charge.", Type: "gauge"},
			},
		},
	}

	cache := poller.NewCache()
	collector := NewCollector(cfg, cache, deviceSpecs)

	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(collector)

	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range metrics {
		names[mf.GetName()] = true
	}

	for _, want := range []string{"echonet_scrape_success", "echonet_scrape_duration_seconds", "echonet_device_info"} {
		if !names[want] {
			t.Errorf("missing expected metric %q", want)
		}
	}
}

func TestCollectorEmitsDeviceMetrics(t *testing.T) {
	cfg := &config.Config{
		Devices: []config.Device{
			{Name: "test_battery", IP: "192.168.1.10", Class: "storage_battery"},
		},
	}
	deviceSpecs := map[string]*specs.DeviceSpec{
		"storage_battery": {
			Metrics: []specs.MetricSpec{
				{EPC: 0xE4, Name: "soc_percent", Help: "State of charge.", Type: "gauge"},
			},
		},
	}

	cache := poller.NewCache()
	dev := cfg.Devices[0]
	cache.Update(dev, "g1", 0, true, 0.5, map[string]echonet.MetricValue{
		"soc_percent": {Value: 85, Type: "gauge"},
	}, "")
	cache.UpdateInfo(dev, echonet.DeviceInfo{Manufacturer: "TestMfr", ProductCode: "TestModel", UID: "abc123"})

	collector := NewCollector(cfg, cache, deviceSpecs)

	expected := `
		# HELP echonet_battery_soc_percent State of charge.
		# TYPE echonet_battery_soc_percent gauge
		echonet_battery_soc_percent{class="storage_battery",device="test_battery",ip="192.168.1.10"} 85
	`
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expected), "echonet_battery_soc_percent"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestSubsystemForClass(t *testing.T) {
	tests := []struct {
		class, want string
	}{
		{"storage_battery", "battery"},
		{"home_solar", "solar"},
		{"home_ac", "ac"},
		{"power_dist_board", "power_dist_board"},
	}
	for _, tt := range tests {
		if got := subsystemForClass(tt.class); got != tt.want {
			t.Errorf("subsystemForClass(%q) = %q, want %q", tt.class, got, tt.want)
		}
	}
}

func TestSanitizeEnumLabel(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"Charging", "charging"},
		{"rapid charging", "rapid_charging"},
		{"ON/OFF", "on_off"},
		{"  ", "value"},
		{"100% Full", "100_full"},
	}
	for _, tt := range tests {
		if got := sanitizeEnumLabel(tt.input); got != tt.want {
			t.Errorf("sanitizeEnumLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

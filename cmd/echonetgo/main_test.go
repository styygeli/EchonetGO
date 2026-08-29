package main

import (
	"testing"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/poller"
)

func TestSetupEchonetTransport_AggregatesSharedIPNames(t *testing.T) {
	cfg := &config.Config{
		Devices: []config.Device{
			{Name: "breaker_box", IP: "192.168.0.248"},
			{Name: "ac_house", IP: "192.168.0.249"},
			{Name: "epcube_battery", IP: "192.168.3.249"},
			{Name: "epcube_solar", IP: "192.168.3.249"},
		},
	}
	cache := poller.NewCache()
	log := logging.New("test")

	transport := setupEchonetTransport(cfg, cache, log)

	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.0.248", "breaker_box (192.168.0.248)"},
		{"192.168.0.249", "ac_house (192.168.0.249)"},
		{"192.168.3.249", "epcube_battery/epcube_solar (192.168.3.249)"},
		{"10.0.0.1", "10.0.0.1"},
	}

	for _, tt := range tests {
		if got := transport.HostLabel(tt.ip); got != tt.want {
			t.Errorf("HostLabel(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}

func TestSetupEchonetTransport_DeduplicatesSameNameOnIP(t *testing.T) {
	cfg := &config.Config{
		Devices: []config.Device{
			{Name: "epcube", IP: "192.168.3.249"},
			{Name: "epcube", IP: "192.168.3.249"},
		},
	}
	cache := poller.NewCache()
	log := logging.New("test")

	transport := setupEchonetTransport(cfg, cache, log)
	if got := transport.HostLabel("192.168.3.249"); got != "epcube (192.168.3.249)" {
		t.Errorf("HostLabel(192.168.3.249) = %q, want %q", got, "epcube (192.168.3.249)")
	}
}

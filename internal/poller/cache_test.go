package poller

import (
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
)

func TestStateForAPIIncludesFailureDiagnostics(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "epcube_battery", IP: "192.168.3.152", Class: "storage_battery"}
	cfg := &config.Config{Devices: []config.Device{dev}}

	c.Update(dev, "1m", time.Minute, false, 1.2, nil, "i/o timeout")
	state := c.StateForAPI(cfg)
	if len(state.Devices) != 1 {
		t.Fatalf("len(Devices) = %d, want 1", len(state.Devices))
	}
	d := state.Devices[0]
	if d.LastError != "i/o timeout" {
		t.Fatalf("LastError = %q, want %q", d.LastError, "i/o timeout")
	}
	if d.MaxGroupFailures != 1 {
		t.Fatalf("MaxGroupFailures = %d, want 1", d.MaxGroupFailures)
	}

	c.Update(dev, "1m", time.Minute, false, 1.4, nil, "still timing out")
	d = c.StateForAPI(cfg).Devices[0]
	if d.LastError != "still timing out" {
		t.Fatalf("LastError = %q, want %q", d.LastError, "still timing out")
	}
	if d.MaxGroupFailures != 2 {
		t.Fatalf("MaxGroupFailures = %d, want 2", d.MaxGroupFailures)
	}

	c.Update(dev, "1m", time.Minute, true, 0.4, map[string]echonet.MetricValue{
		"state_of_capacity_percent": {Value: 75, Type: "gauge"},
	}, "")
	d = c.StateForAPI(cfg).Devices[0]
	if d.LastError != "" {
		t.Fatalf("LastError = %q, want empty", d.LastError)
	}
	if d.MaxGroupFailures != 0 {
		t.Fatalf("MaxGroupFailures = %d, want 0", d.MaxGroupFailures)
	}
}

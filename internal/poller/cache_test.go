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
	diag := readDeviceDiag(t, c.StateForAPI(cfg))
	if got := diag["last_error"].(string); got != "i/o timeout" {
		t.Fatalf("last_error = %q, want %q", got, "i/o timeout")
	}
	if got := int(diag["consecutive_failures"].(int)); got != 1 {
		t.Fatalf("consecutive_failures = %d, want 1", got)
	}

	c.Update(dev, "1m", time.Minute, false, 1.4, nil, "still timing out")
	diag = readDeviceDiag(t, c.StateForAPI(cfg))
	if got := diag["last_error"].(string); got != "still timing out" {
		t.Fatalf("last_error = %q, want %q", got, "still timing out")
	}
	if got := int(diag["consecutive_failures"].(int)); got != 2 {
		t.Fatalf("consecutive_failures = %d, want 2", got)
	}

	c.Update(dev, "1m", time.Minute, true, 0.4, map[string]echonet.MetricValue{
		"state_of_capacity_percent": {Value: 75, Type: "gauge"},
	}, "")
	diag = readDeviceDiag(t, c.StateForAPI(cfg))
	if got := diag["last_error"].(string); got != "" {
		t.Fatalf("last_error = %q, want empty", got)
	}
	if got := int(diag["consecutive_failures"].(int)); got != 0 {
		t.Fatalf("consecutive_failures = %d, want 0", got)
	}
}

func readDeviceDiag(t *testing.T, state map[string]interface{}) map[string]interface{} {
	t.Helper()
	devicesAny, ok := state["devices"].([]map[string]interface{})
	if ok {
		return devicesAny[0]
	}
	rawDevices, ok := state["devices"].([]interface{})
	if !ok || len(rawDevices) == 0 {
		t.Fatalf("state devices not present or empty: %#v", state["devices"])
	}
	first, ok := rawDevices[0].(map[string]interface{})
	if !ok {
		t.Fatalf("device entry is not map: %#v", rawDevices[0])
	}
	return first
}

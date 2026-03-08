package poller

import (
	"sync"
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/specs"
)

func TestSetOnUpdate_CallbackFiresOnUpdate(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test_dev", IP: "192.168.1.1", Class: "home_ac"}

	var callCount int
	var lastDev config.Device
	var lastSuccess bool
	var lastMetrics map[string]echonet.MetricValue
	c.SetOnUpdate(func(d config.Device, _ echonet.DeviceInfo, m map[string]echonet.MetricValue, _ []specs.MetricSpec, _ map[byte]struct{}, _ *specs.ClimateSpec, success bool) {
		callCount++
		lastDev = d
		lastSuccess = success
		lastMetrics = m
	})

	c.Update(dev, "1m", time.Minute, false, 0.5, nil, "timeout")
	if callCount != 1 {
		t.Fatalf("callback called %d times, want 1 (after failed Update)", callCount)
	}
	if lastDev.Name != "test_dev" {
		t.Fatalf("callback dev name = %q, want test_dev", lastDev.Name)
	}
	if lastSuccess {
		t.Fatal("callback success = true, want false for failed scrape")
	}
	if lastMetrics == nil || len(lastMetrics) != 0 {
		t.Fatalf("callback metrics = %v, want empty map", lastMetrics)
	}

	c.Update(dev, "1m", time.Minute, true, 0.2, map[string]echonet.MetricValue{
		"indoor_temperature_celsius": {Value: 24, Type: "gauge"},
	}, "")
	if callCount != 2 {
		t.Fatalf("callback called %d times, want 2 (after successful Update)", callCount)
	}
	if !lastSuccess {
		t.Fatal("callback success = false, want true")
	}
	if lastMetrics == nil || lastMetrics["indoor_temperature_celsius"].Value != 24 {
		t.Fatalf("callback metrics = %v, want indoor_temperature_celsius=24", lastMetrics)
	}
}

func TestSetDeviceSpecs_OnUpdateReceivesSpecs(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "solar", IP: "192.168.1.10", Class: "home_solar"}
	metricSpecs := []specs.MetricSpec{
		{EPC: 0xE0, Name: "instantaneous_generation_watts", Type: "gauge", HADeviceClass: "power", HAStateClass: "measurement", HAUnit: "W"},
		{EPC: 0xE1, Name: "cumulative_generation_kwh", Type: "counter", HADeviceClass: "energy", HAStateClass: "total_increasing", HAUnit: "kWh"},
	}
	c.SetDeviceSpecs(dev, metricSpecs)

	var receivedSpecs []specs.MetricSpec
	c.SetOnUpdate(func(d config.Device, _ echonet.DeviceInfo, m map[string]echonet.MetricValue, ms []specs.MetricSpec, _ map[byte]struct{}, _ *specs.ClimateSpec, success bool) {
		receivedSpecs = ms
	})

	c.Update(dev, "1m", time.Minute, true, 0.1, map[string]echonet.MetricValue{
		"instantaneous_generation_watts": {Value: 500, Type: "gauge"},
		"cumulative_generation_kwh":       {Value: 1234.5, Type: "counter"},
	}, "")
	if len(receivedSpecs) != 2 {
		t.Fatalf("callback received %d specs, want 2", len(receivedSpecs))
	}
	if receivedSpecs[0].Name != "instantaneous_generation_watts" || receivedSpecs[1].Name != "cumulative_generation_kwh" {
		t.Fatalf("callback specs = %v", receivedSpecs)
	}
	if receivedSpecs[0].HADeviceClass != "power" || receivedSpecs[1].HAUnit != "kWh" {
		t.Fatalf("HA metadata: %q / %q", receivedSpecs[0].HADeviceClass, receivedSpecs[1].HAUnit)
	}
}

func TestSetOnUpdate_CallbackReceivesAggregatedMetrics(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "battery", IP: "10.0.0.1", Class: "storage_battery"}
	c.SetDeviceSpecs(dev, []specs.MetricSpec{{EPC: 0xE4, Name: "state_of_capacity_percent", Type: "gauge"}})

	var mu sync.Mutex
	var lastMetrics map[string]echonet.MetricValue
	c.SetOnUpdate(func(_ config.Device, _ echonet.DeviceInfo, m map[string]echonet.MetricValue, _ []specs.MetricSpec, _ map[byte]struct{}, _ *specs.ClimateSpec, _ bool) {
		mu.Lock()
		lastMetrics = make(map[string]echonet.MetricValue, len(m))
		for k, v := range m {
			lastMetrics[k] = v
		}
		mu.Unlock()
	})

	c.Update(dev, "1m", time.Minute, true, 0.1, map[string]echonet.MetricValue{"state_of_capacity_percent": {Value: 50, Type: "gauge"}}, "")
	c.Update(dev, "5m", 5*time.Minute, true, 0.1, map[string]echonet.MetricValue{"state_of_capacity_percent": {Value: 55, Type: "gauge"}}, "")

	mu.Lock()
	agg := lastMetrics
	mu.Unlock()
	if agg == nil || agg["state_of_capacity_percent"].Value != 55 {
		t.Fatalf("callback should receive aggregated cache (latest value): got %v", agg)
	}
}

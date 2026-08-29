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
	c.SetOnUpdate(func(st DeviceState) {
		callCount++
		lastDev = st.Device
		lastSuccess = st.Success
		lastMetrics = st.Metrics
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
	c.SetOnUpdate(func(st DeviceState) {
		receivedSpecs = st.MetricSpecs
	})

	c.Update(dev, "1m", time.Minute, true, 0.1, map[string]echonet.MetricValue{
		"instantaneous_generation_watts": {Value: 500, Type: "gauge"},
		"cumulative_generation_kwh":      {Value: 1234.5, Type: "counter"},
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

func TestSetDeviceLight_CallbackReceivesLightSpec(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "ceiling_light", IP: "192.168.1.50", Class: "general_lighting"}
	lightSpec := &specs.LightSpec{
		BrightnessEPC:   0xB0,
		ColorSettingEPC: 0xB1,
		ColorSettings:   map[string]int{"white": 0x42, "daylight_white": 0x43},
	}
	c.SetDeviceLight(dev, lightSpec)
	c.SetDeviceSpecs(dev, []specs.MetricSpec{
		{EPC: 0xB0, Name: "illuminance_level", Type: "gauge"},
	})

	var receivedLight *specs.LightSpec
	c.SetOnUpdate(func(st DeviceState) {
		receivedLight = st.Light
	})

	c.Update(dev, "1m", time.Minute, true, 0.1, map[string]echonet.MetricValue{
		"illuminance_level": {Value: 80, Type: "gauge"},
	}, "")

	if receivedLight == nil {
		t.Fatal("callback should receive non-nil LightSpec")
	}
	if receivedLight.BrightnessEPC != 0xB0 {
		t.Fatalf("BrightnessEPC = 0x%02x, want 0xB0", receivedLight.BrightnessEPC)
	}
	if len(receivedLight.ColorSettings) != 2 {
		t.Fatalf("len(ColorSettings) = %d, want 2", len(receivedLight.ColorSettings))
	}

	// Verify GetDeviceLight also works.
	got := c.GetDeviceLight(dev)
	if got == nil || got.BrightnessEPC != 0xB0 {
		t.Fatalf("GetDeviceLight() = %v, want non-nil with BrightnessEPC=0xB0", got)
	}
}

func TestSetOnUpdate_CallbackReceivesAggregatedMetrics(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "battery", IP: "10.0.0.1", Class: "storage_battery"}
	c.SetDeviceSpecs(dev, []specs.MetricSpec{{EPC: 0xE4, Name: "state_of_capacity_percent", Type: "gauge"}})

	var mu sync.Mutex
	var lastMetrics map[string]echonet.MetricValue
	c.SetOnUpdate(func(st DeviceState) {
		mu.Lock()
		lastMetrics = make(map[string]echonet.MetricValue, len(st.Metrics))
		for k, v := range st.Metrics {
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

// TestMergeMetrics_NoSyntheticGroup verifies that MergeMetrics (used for INF
// pushes and post-command verification reads) merges metric values and fires
// the callback WITHOUT registering a scrape group, so scrape_duration_seconds
// and last_scrape_timestamp_seconds keep reflecting only real polls.
func TestMergeMetrics_NoSyntheticGroup(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "ac", IP: "192.168.1.5", Class: "home_ac"}

	var callCount int
	c.SetOnUpdate(func(st DeviceState) { callCount++ })

	// One real scrape with a known duration/timestamp.
	c.Update(dev, "1m", time.Minute, true, 0.42, map[string]echonet.MetricValue{
		"operation_status": {Value: 0x30, Type: "gauge"},
	}, "")
	realSuccess := c.metrics[deviceKey(dev)].groups["1m"].lastSuccess
	if callCount != 1 {
		t.Fatalf("callback fired %d times after Update, want 1", callCount)
	}

	// A command-verification merge must not add a group nor change scrape timing.
	c.MergeMetrics(dev, map[string]echonet.MetricValue{
		"target_temperature_celsius": {Value: 22, Type: "gauge"},
	})
	if callCount != 2 {
		t.Fatalf("callback fired %d times after MergeMetrics, want 2", callCount)
	}

	success, durationSec, lastScrape, metrics := c.Get(dev)
	if !success {
		t.Fatal("Get success = false, want true (real scrape within TTL)")
	}
	if durationSec != 0.42 {
		t.Fatalf("Get duration = %v, want 0.42 (from real scrape, not synthetic 0)", durationSec)
	}
	if !lastScrape.Equal(realSuccess) {
		t.Fatalf("Get lastScrape = %v, want real scrape time %v (not command time)", lastScrape, realSuccess)
	}
	if metrics["operation_status"].Value != 0x30 || metrics["target_temperature_celsius"].Value != 22 {
		t.Fatalf("merged metrics = %v, want both operation_status and target_temperature_celsius", metrics)
	}

	groups := c.metrics[deviceKey(dev)].groups
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (no synthetic group)", len(groups))
	}
	if _, ok := groups["verify_update"]; ok {
		t.Fatal("groups contains synthetic verify_update entry")
	}
	if _, ok := groups["set_update"]; ok {
		t.Fatal("groups contains synthetic set_update entry")
	}
}

func TestCache_CapabilityHelpersAndTriggerUpdate(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "ac_test", IP: "192.168.3.10", Class: "home_ac"}

	if c.HasWritableMap(dev) {
		t.Fatal("HasWritableMap = true, want false initially")
	}
	if c.HasNotificationMap(dev) {
		t.Fatal("HasNotificationMap = true, want false initially")
	}
	if c.HasDeviceInfo(dev) {
		t.Fatal("HasDeviceInfo = true, want false initially")
	}

	c.SetWritableEPCs(dev, map[byte]struct{}{0x80: {}, 0xB3: {}})
	if !c.HasWritableMap(dev) {
		t.Fatal("HasWritableMap = false, want true after SetWritableEPCs")
	}

	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	if !c.HasNotificationMap(dev) {
		t.Fatal("HasNotificationMap = false, want true after SetNotificationEPCs")
	}

	c.UpdateInfo(dev, echonet.DeviceInfo{Manufacturer: "TestMfg", ProductCode: "Model1"})
	if !c.HasDeviceInfo(dev) {
		t.Fatal("HasDeviceInfo = false, want true after UpdateInfo")
	}

	var updatedState DeviceState
	var called bool
	c.SetOnUpdate(func(st DeviceState) {
		called = true
		updatedState = st
	})

	c.TriggerUpdate(dev)
	if !called {
		t.Fatal("TriggerUpdate did not invoke onUpdate callback")
	}
	if updatedState.Device.Name != "ac_test" {
		t.Fatalf("updatedState device name = %q, want ac_test", updatedState.Device.Name)
	}
	if len(updatedState.Writable) != 2 {
		t.Fatalf("updatedState.Writable count = %d, want 2", len(updatedState.Writable))
	}

	// Verify defensive copy: modifying returned state does not affect cache
	delete(updatedState.Writable, 0x80)
	cachedMap, ok := c.GetWritableEPCs(dev)
	if !ok || len(cachedMap) != 2 {
		t.Fatalf("cached Writable was mutated by caller! len=%d, want 2", len(cachedMap))
	}
}

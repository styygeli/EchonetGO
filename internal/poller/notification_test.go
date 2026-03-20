package poller

import (
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/specs"
)

func TestShouldSkipPoll_NoNotifyEPCs(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "192.168.0.1", Class: "home_ac"}
	// No STATMAP registered — should never skip.
	if c.ShouldSkipPoll(dev, 0x80, 2*time.Minute) {
		t.Fatal("ShouldSkipPoll should return false when no STATMAP is registered")
	}
}

func TestShouldSkipPoll_NotInSTATMAP(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "192.168.0.1", Class: "home_ac"}
	// Register STATMAP with only 0x80.
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	c.RecordPush(dev, []byte{0x80})
	// 0xB3 is not in STATMAP — should not skip.
	if c.ShouldSkipPoll(dev, 0xB3, 2*time.Minute) {
		t.Fatal("ShouldSkipPoll should return false for EPC not in STATMAP")
	}
}

func TestShouldSkipPoll_FreshPush(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "192.168.0.1", Class: "home_ac"}
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	c.RecordPush(dev, []byte{0x80})
	// Just pushed — should skip within freshness window.
	if !c.ShouldSkipPoll(dev, 0x80, 2*time.Minute) {
		t.Fatal("ShouldSkipPoll should return true for recently pushed EPC")
	}
}

func TestShouldSkipPoll_StalePush(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "192.168.0.1", Class: "home_ac"}
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	// Manually inject an old push time.
	key := deviceKey(dev)
	c.mu.Lock()
	c.lastPush[key] = map[byte]time.Time{0x80: time.Now().Add(-5 * time.Minute)}
	c.mu.Unlock()
	// Push is stale — should not skip.
	if c.ShouldSkipPoll(dev, 0x80, 2*time.Minute) {
		t.Fatal("ShouldSkipPoll should return false for stale push outside freshness window")
	}
}

func TestShouldSkipPoll_NoPushRecorded(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "192.168.0.1", Class: "home_ac"}
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	// STATMAP registered but no push ever received — should not skip.
	if c.ShouldSkipPoll(dev, 0x80, 2*time.Minute) {
		t.Fatal("ShouldSkipPoll should return false when no push has been recorded")
	}
}

func TestShouldSkipPoll_ForcePolling(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "192.168.0.1", Class: "home_ac"}
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	c.RecordPush(dev, []byte{0x80})
	c.SetForcePolling(true)
	// Force polling overrides — should never skip.
	if c.ShouldSkipPoll(dev, 0x80, 2*time.Minute) {
		t.Fatal("ShouldSkipPoll should return false when force polling is enabled")
	}
}

func TestUpdateFromINF_MergesAndCallsCallback(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "ecocute", IP: "192.168.3.248", Class: "ecocute"}
	c.SetDeviceSpecs(dev, []specs.MetricSpec{
		{EPC: 0xC3, Name: "hot_water_supply_status", Size: 1, Scale: 1, Type: "gauge"},
		{EPC: 0x80, Name: "operation_status", Size: 1, Scale: 1, Type: "gauge"},
	})

	// Seed a polled metric.
	c.Update(dev, "1m", time.Minute, true, 0.1, map[string]echonet.MetricValue{
		"operation_status": {Value: 0x30, Type: "gauge"},
	}, "")

	var cbCalled int
	var cbMetrics map[string]echonet.MetricValue
	c.SetOnUpdate(func(_ config.Device, _ echonet.DeviceInfo, m map[string]echonet.MetricValue, _ []specs.MetricSpec, _ map[byte]struct{}, _ *specs.ClimateSpec, success bool) {
		cbCalled++
		cbMetrics = m
		if !success {
			t.Error("UpdateFromINF callback should report success=true")
		}
	})

	// Simulate INF push for hot_water_supply_status.
	c.UpdateFromINF(dev, map[string]echonet.MetricValue{
		"hot_water_supply_status": {Value: 0x41, Type: "gauge"},
	})

	if cbCalled != 1 {
		t.Fatalf("callback called %d times, want 1", cbCalled)
	}
	// Should contain both the polled metric and the pushed metric.
	if cbMetrics["operation_status"].Value != 0x30 {
		t.Fatalf("operation_status = %v, want 48", cbMetrics["operation_status"].Value)
	}
	if cbMetrics["hot_water_supply_status"].Value != 0x41 {
		t.Fatalf("hot_water_supply_status = %v, want 65", cbMetrics["hot_water_supply_status"].Value)
	}
}

func TestFindDeviceByIPAndEOJ(t *testing.T) {
	c := NewCache()
	dev1 := config.Device{Name: "ac_house", IP: "192.168.0.249", Class: "home_ac"}
	dev2 := config.Device{Name: "breaker", IP: "192.168.0.249", Class: "power_distribution_board_metering"}
	c.SetDeviceEOJ(dev1, [3]byte{0x01, 0x30, 0x01})
	c.SetDeviceEOJ(dev2, [3]byte{0x02, 0x87, 0x01})

	devices := []config.Device{dev1, dev2}

	// Match AC.
	found, ok := c.FindDeviceByIPAndEOJ("192.168.0.249", [3]byte{0x01, 0x30, 0x01}, devices)
	if !ok || found.Name != "ac_house" {
		t.Fatalf("expected ac_house, got %v ok=%v", found.Name, ok)
	}

	// Match breaker.
	found, ok = c.FindDeviceByIPAndEOJ("192.168.0.249", [3]byte{0x02, 0x87, 0x01}, devices)
	if !ok || found.Name != "breaker" {
		t.Fatalf("expected breaker, got %v ok=%v", found.Name, ok)
	}

	// No match — different IP.
	_, ok = c.FindDeviceByIPAndEOJ("192.168.0.250", [3]byte{0x01, 0x30, 0x01}, devices)
	if ok {
		t.Fatal("expected no match for wrong IP")
	}

	// No match — different EOJ class.
	_, ok = c.FindDeviceByIPAndEOJ("192.168.0.249", [3]byte{0x02, 0x6B, 0x01}, devices)
	if ok {
		t.Fatal("expected no match for unregistered EOJ class")
	}
}

func TestRecordPush_MultipleEPCs(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "ac", IP: "192.168.0.1", Class: "home_ac"}
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}, 0xB0: {}, 0xA0: {}})

	c.RecordPush(dev, []byte{0x80, 0xB0})

	// Both pushed EPCs should be skippable.
	if !c.ShouldSkipPoll(dev, 0x80, 2*time.Minute) {
		t.Fatal("0x80 should be skippable after push")
	}
	if !c.ShouldSkipPoll(dev, 0xB0, 2*time.Minute) {
		t.Fatal("0xB0 should be skippable after push")
	}
	// 0xA0 was not pushed — should not skip.
	if c.ShouldSkipPoll(dev, 0xA0, 2*time.Minute) {
		t.Fatal("0xA0 should NOT be skippable (not pushed)")
	}
}

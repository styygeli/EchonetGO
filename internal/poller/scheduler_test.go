package poller

import (
	"context"
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
)

func TestScheduler_RunDeviceInfoRefresher_DoesNotExecuteOnStart(t *testing.T) {
	c := NewCache()
	s := NewScheduler(c)
	dev := config.Device{Name: "epcube_battery", IP: "192.168.3.249", Class: "storage_battery"}
	eoj := [3]byte{0x02, 0x7D, 0x01}
	devices := []deviceWithEOJ{{dev: dev, eoj: eoj}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runDeviceInfoRefresher(ctx, devices)
	}()

	// Wait 50ms to ensure any immediate execution would have started, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Verify that s.reconciler.RefreshDevice was NOT called synchronously.
	key := deviceKey(dev)
	s.reconciler.cooldownMu.Lock()
	last := s.reconciler.lastAttempt[key]
	s.reconciler.cooldownMu.Unlock()

	if !last.IsZero() {
		t.Fatalf("expected RefreshDevice not to execute on start, but lastAttempt was %v", last)
	}
}

func TestScheduler_CalculateInitialDelay_Staggering(t *testing.T) {
	interval := 10 * time.Second

	// Single-device host (devIndex = 0)
	if delay := calculateInitialDelay(interval, 0, 0); delay != 0 {
		t.Errorf("devIndex 0, group 0: got %v, want 0", delay)
	}
	if delay := calculateInitialDelay(interval, 1, 0); delay != 500*time.Millisecond {
		t.Errorf("devIndex 0, group 1: got %v, want 500ms", delay)
	}
	if delay := calculateInitialDelay(interval, 2, 0); delay != 1000*time.Millisecond {
		t.Errorf("devIndex 0, group 2: got %v, want 1000ms", delay)
	}

	// Multi-device host: second device on shared IP (devIndex = 1)
	if delay := calculateInitialDelay(interval, 0, 1); delay != 250*time.Millisecond {
		t.Errorf("devIndex 1, group 0: got %v, want 250ms", delay)
	}
	if delay := calculateInitialDelay(interval, 1, 1); delay != 750*time.Millisecond {
		t.Errorf("devIndex 1, group 1: got %v, want 750ms", delay)
	}
	if delay := calculateInitialDelay(interval, 2, 1); delay != 1250*time.Millisecond {
		t.Errorf("devIndex 1, group 2: got %v, want 1250ms", delay)
	}

	// Clamping to interval / 2
	shortInterval := 1 * time.Second // interval/2 = 500ms
	if delay := calculateInitialDelay(shortInterval, 2, 0); delay != 500*time.Millisecond {
		t.Errorf("short interval clamped: got %v, want 500ms", delay)
	}
}

func TestScheduler_UpfrontDeviceInfo_SatisfiesReconciler(t *testing.T) {
	c := NewCache()
	r := NewCapabilityReconciler(c)
	dev := config.Device{Name: "epcube_battery", IP: "192.168.3.249", Class: "storage_battery"}

	// Fresh cache needs reconciliation.
	if !r.NeedsReconciliation(dev) {
		t.Fatal("expected fresh device to need reconciliation")
	}

	// Property maps recorded during discovery.
	c.SetWritableEPCs(dev, map[byte]struct{}{0x80: {}, 0xDA: {}})
	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}, 0xDA: {}})

	// Without DeviceInfo, reconciliation is still needed.
	if !r.NeedsReconciliation(dev) {
		t.Fatal("expected device without info to need reconciliation")
	}

	// Upfront DeviceInfo resolution stores info in cache.
	info := echonet.DeviceInfo{
		UID:              "000131123456",
		Manufacturer:     "Eternalplanet Energy",
		ManufacturerCode: "000131",
		ProductCode:      "EP Cube Battery",
	}
	c.UpdateInfo(dev, info)

	// Now all capabilities are satisfied, so NeedsReconciliation is false.
	if r.NeedsReconciliation(dev) {
		t.Fatal("expected device with upfront info to not need reconciliation")
	}
}

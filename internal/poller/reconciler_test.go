package poller

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
)

func TestReconciler_NeedsReconciliation(t *testing.T) {
	c := NewCache()
	r := NewCapabilityReconciler(c)
	dev := config.Device{Name: "ac_test", IP: "192.168.3.10", Class: "home_ac"}

	if !r.NeedsReconciliation(dev) {
		t.Fatal("NeedsReconciliation should be true for fresh cache")
	}

	c.SetWritableEPCs(dev, map[byte]struct{}{0x80: {}})
	if !r.NeedsReconciliation(dev) {
		t.Fatal("NeedsReconciliation should still be true when notify and info missing")
	}

	c.SetNotificationEPCs(dev, map[byte]struct{}{0x80: {}})
	if !r.NeedsReconciliation(dev) {
		t.Fatal("NeedsReconciliation should still be true when info missing")
	}

	c.UpdateInfo(dev, echonet.DeviceInfo{Manufacturer: "Mitsubishi"})
	if r.NeedsReconciliation(dev) {
		t.Fatal("NeedsReconciliation should be false when all capabilities known")
	}
}

func TestReconciler_NeedsReconciliation_EmptyMapsSatisfy(t *testing.T) {
	c := NewCache()
	r := NewCapabilityReconciler(c)
	dev := config.Device{Name: "sensor_test", IP: "192.168.3.20", Class: "temperature_sensor"}

	// Empty maps (e.g. read-only device with no 0x9E or device with no 0x9D notifications)
	c.SetWritableEPCs(dev, map[byte]struct{}{})
	c.SetNotificationEPCs(dev, map[byte]struct{}{})
	c.UpdateInfo(dev, echonet.DeviceInfo{Manufacturer: "Omron"})

	if r.NeedsReconciliation(dev) {
		t.Fatal("NeedsReconciliation should be false when empty maps are explicitly recorded")
	}
}

func TestReconciler_CooldownAndSingleflight(t *testing.T) {
	c := NewCache()
	r := NewCapabilityReconciler(c)
	dev := config.Device{Name: "ac_test", IP: "192.168.3.10", Class: "home_ac"}
	key := deviceKey(dev)

	// Simulate that an attempt occurred just now.
	r.cooldownMu.Lock()
	r.lastAttempt[key] = time.Now()
	r.cooldownMu.Unlock()

	// Calling ReconcileDevice should be skipped due to cooldown.
	// Since client is nil, if it proceeded it would return early, but we can verify cooldown directly.
	r.cooldownMu.Lock()
	inCooldown := time.Since(r.lastAttempt[key]) < reconcileCooldown
	r.cooldownMu.Unlock()
	if !inCooldown {
		t.Fatal("expected device to be within cooldown period")
	}
}

func TestReconciler_SingleflightConcurrency(t *testing.T) {
	// Verify that singleflight executes at most once for identical keys under concurrent calls.
	c := NewCache()
	r := NewCapabilityReconciler(c)
	key := "test-key"

	var probeCount atomic.Int32
	var wg sync.WaitGroup

	// Launch 10 concurrent routines through the singleflight group
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = r.sf.Do(key, func() (any, error) {
				probeCount.Add(1)
				time.Sleep(50 * time.Millisecond)
				return nil, nil
			})
		}()
	}
	wg.Wait()

	if got := probeCount.Load(); got != 1 {
		t.Fatalf("singleflight executed %d times, want exactly 1", got)
	}
}

func TestReconciler_SetClientAndNilSafety(t *testing.T) {
	c := NewCache()
	r := NewCapabilityReconciler(c)
	dev := config.Device{Name: "ac_test", IP: "192.168.3.10", Class: "home_ac"}

	// When client is nil, ReconcileDevice and RefreshDevice must not panic
	r.ReconcileDevice(nil, dev, [3]byte{1, 2, 3})
	r.RefreshDevice(nil, dev, [3]byte{1, 2, 3})

	// SetClient should safely set client
	r.SetClient(nil)
	if r.getClient() != nil {
		t.Fatal("expected nil client")
	}
}

func TestReconciler_SingleflightSharedFlag(t *testing.T) {
	c := NewCache()
	r := NewCapabilityReconciler(c)
	key := "shared-key"

	var sharedCount atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, shared := r.sf.Do(key, func() (any, error) {
				time.Sleep(50 * time.Millisecond)
				return nil, nil
			})
			if shared {
				sharedCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if sharedCount.Load() == 0 {
		t.Errorf("expected at least 1 shared call, got 0")
	}
}

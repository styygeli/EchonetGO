package poller

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
)

const (
	// reconcileCooldown enforces a minimum interval between reconciliation attempts for the same device.
	reconcileCooldown = 60 * time.Second
	// reconcilePerQueryTimeout provides generous headroom for slow legacy 8-bit/16-bit ECHONET devices.
	reconcilePerQueryTimeout = 5 * time.Second
	// reconcileTotalTimeout is the maximum lifetime budget for an entire reconciliation cycle.
	reconcileTotalTimeout = 12 * time.Second
)

// CapabilityReconciler performs rate-limited, single-flight background capability
// discovery (SETMAP 0x9E, STATMAP 0x9D, DeviceInfo 0x8A/0x83/0x8C) when a device
// is responsive but has incomplete capabilities in the cache.
type CapabilityReconciler struct {
	cache       *Cache
	sf          singleflight.Group
	cooldownMu  sync.Mutex
	lastAttempt map[string]time.Time
}

// NewCapabilityReconciler creates a CapabilityReconciler.
func NewCapabilityReconciler(cache *Cache) *CapabilityReconciler {
	return &CapabilityReconciler{
		cache:       cache,
		lastAttempt: make(map[string]time.Time),
	}
}

// NeedsReconciliation returns true if any mandatory capability map or identity is missing.
func (r *CapabilityReconciler) NeedsReconciliation(dev config.Device) bool {
	return !r.cache.HasWritableMap(dev) || !r.cache.HasNotificationMap(dev) || !r.cache.HasDeviceInfo(dev)
}

// ReconcileDevice asynchronously probes missing capabilities for dev if needed,
// enforcing single-flight deduplication and a per-device cooldown.
func (r *CapabilityReconciler) ReconcileDevice(lifetimeCtx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte) {
	if client == nil || r == nil {
		return
	}
	if !r.NeedsReconciliation(dev) {
		return
	}

	key := deviceKey(dev)

	r.cooldownMu.Lock()
	last := r.lastAttempt[key]
	if time.Since(last) < reconcileCooldown {
		r.cooldownMu.Unlock()
		return
	}
	r.lastAttempt[key] = time.Now()
	r.cooldownMu.Unlock()

	go func() {
		defer pollerLog.RecoverPanic("capability reconcile for " + dev.Name)
		_, _, _ = r.sf.Do(key, func() (any, error) {
			r.doReconcile(lifetimeCtx, client, dev, eoj)
			return nil, nil
		})
	}()
}

func (r *CapabilityReconciler) doReconcile(lifetimeCtx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte) {
	ctx, cancel := context.WithTimeout(lifetimeCtx, reconcileTotalTimeout)
	defer cancel()

	updated := false

	// 1. Reconcile writable property map (0x9E / SETMAP) if missing
	if !r.cache.HasWritableMap(dev) && ctx.Err() == nil {
		queryCtx, queryCancel := context.WithTimeout(ctx, reconcilePerQueryTimeout)
		writable, err := client.GetWritablePropertyMap(queryCtx, dev.IP, eoj)
		queryCancel()
		if err != nil {
			pollerLog.Debugf("device %s (%s): reconciler read SETMAP (0x9E) failed: %v", dev.Name, dev.IP, err)
		} else {
			r.cache.SetWritableEPCs(dev, writable)
			updated = true
			pollerLog.Infof("device %s (%s): self-healed writable property map (0x9E) with %d EPCs", dev.Name, dev.IP, len(writable))
		}
	}

	// 2. Reconcile notification property map (0x9D / STATMAP) if missing
	if !r.cache.HasNotificationMap(dev) && ctx.Err() == nil {
		queryCtx, queryCancel := context.WithTimeout(ctx, reconcilePerQueryTimeout)
		notify, err := client.GetNotificationPropertyMap(queryCtx, dev.IP, eoj)
		queryCancel()
		if err != nil {
			pollerLog.Debugf("device %s (%s): reconciler read STATMAP (0x9D) failed: %v", dev.Name, dev.IP, err)
		} else {
			r.cache.SetNotificationEPCs(dev, notify)
			updated = true
			pollerLog.Infof("device %s (%s): self-healed notification property map (0x9D) with %d EPCs", dev.Name, dev.IP, len(notify))
		}
	}

	// 3. Reconcile device identity (0x8A / 0x83 / 0x8C) if missing
	if !r.cache.HasDeviceInfo(dev) && ctx.Err() == nil {
		queryCtx, queryCancel := context.WithTimeout(ctx, reconcilePerQueryTimeout)
		info, err := client.GetDeviceInfo(queryCtx, dev.IP, eoj, dev.Model)
		queryCancel()
		if err != nil {
			pollerLog.Debugf("device %s (%s): reconciler read DeviceInfo failed: %v", dev.Name, dev.IP, err)
		} else if info.Manufacturer != "" || info.UID != "" {
			r.cache.UpdateInfo(dev, info)
			updated = true
			pollerLog.Infof("device %s (%s): self-healed identity (mfg=%q model=%q)", dev.Name, dev.IP, info.Manufacturer, info.ProductCode)
		}
	}

	// If any capability was healed, trigger a cache update to notify the MQTT publisher worker
	if updated {
		r.cache.TriggerUpdate(dev)
	}
}

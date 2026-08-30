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
	clientMu    sync.RWMutex
	client      *echonet.Client
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

// SetClient configures the ECHONET client used for capability queries.
func (r *CapabilityReconciler) SetClient(client *echonet.Client) {
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	r.client = client
}

func (r *CapabilityReconciler) getClient() *echonet.Client {
	r.clientMu.RLock()
	defer r.clientMu.RUnlock()
	return r.client
}

// NeedsReconciliation returns true if any mandatory capability map or identity is missing.
func (r *CapabilityReconciler) NeedsReconciliation(dev config.Device) bool {
	return !r.cache.HasWritableMap(dev) || !r.cache.HasNotificationMap(dev) || !r.cache.HasDeviceInfo(dev)
}

// ReconcileDevice asynchronously probes missing capabilities for dev if needed,
// enforcing single-flight deduplication and a per-device cooldown.
func (r *CapabilityReconciler) ReconcileDevice(lifetimeCtx context.Context, dev config.Device, eoj [3]byte) {
	if r == nil || r.getClient() == nil {
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
		recCtx := echonet.ContextWithCaller(lifetimeCtx, dev.Name+":reconciler")
		_, _, shared := r.sf.Do(key, func() (any, error) {
			r.doReconcile(recCtx, dev, eoj)
			return nil, nil
		})
		if shared {
			pollerLog.Debugf("device %s (%s): coalesced concurrent reconciliation into in-flight task (group=%s)",
				dev.Name, dev.IP, key)
		}
	}()
}

// RefreshDevice updates device identity (0x8A/0x83/0x8C) and reconciles any missing maps,
// using singleflight to prevent collisions with concurrent scrapes or commands.
func (r *CapabilityReconciler) RefreshDevice(ctx context.Context, dev config.Device, eoj [3]byte) {
	client := r.getClient()
	if r == nil || client == nil {
		return
	}
	key := deviceKey(dev)

	r.cooldownMu.Lock()
	r.lastAttempt[key] = time.Now()
	r.cooldownMu.Unlock()

	refCtx := echonet.ContextWithCaller(ctx, dev.Name+":refresh")
	_, _, shared := r.sf.Do(key, func() (any, error) {
		queryCtx, cancel := context.WithTimeout(refCtx, reconcilePerQueryTimeout)
		info, err := client.GetDeviceInfo(queryCtx, dev.IP, eoj, dev.Model)
		cancel()
		if err != nil {
			pollerLog.Warnf("device %s (%s): device info read failed: %v", dev.Name, dev.IP, err)
		} else {
			r.cache.UpdateInfo(dev, info)
		}
		if r.NeedsReconciliation(dev) {
			r.doReconcile(refCtx, dev, eoj)
		}
		return nil, nil
	})
	if shared {
		pollerLog.Debugf("device %s (%s): coalesced concurrent refresh into in-flight task (group=%s)",
			dev.Name, dev.IP, key)
	}
}

func (r *CapabilityReconciler) doReconcile(lifetimeCtx context.Context, dev config.Device, eoj [3]byte) {
	client := r.getClient()
	if client == nil {
		return
	}
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
			if echonet.IsGetSNA(err) {
				r.cache.SetWritableEPCs(dev, map[byte]struct{}{})
				updated = true
			}
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
			if echonet.IsGetSNA(err) {
				r.cache.SetNotificationEPCs(dev, map[byte]struct{}{})
				updated = true
			}
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

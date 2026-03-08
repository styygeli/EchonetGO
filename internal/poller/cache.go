package poller

import (
	"sync"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/specs"
)

// UpdateCallback is called after a scrape with the device's current state.
// writable is the set of EPCs the device reports as writable (0x9E); may be nil.
// climateSpec is non-nil for device classes that support HA climate (e.g. home_ac).
type UpdateCallback func(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec, success bool)

// Cache holds the latest scraped metrics per device. Safe for concurrent use.
type Cache struct {
	mu            sync.RWMutex
	metrics       map[string]deviceCache
	onUpdate      UpdateCallback
	specsByDev    map[string][]specs.MetricSpec   // filtered specs per device key
	climateByDev  map[string]*specs.ClimateSpec   // device key -> climate spec if AC
	writableEPCs map[string]map[byte]struct{}     // device key -> set of writable EPCs (from 0x9E)
	eojByDev      map[string][3]byte              // device key -> EOJ for SET requests
}

type deviceCache struct {
	groups  map[string]groupStatus
	metrics map[string]echonet.MetricValue
	info    echonet.DeviceInfo
}

type groupStatus struct {
	interval    time.Duration
	success     bool
	durationSec float64
	lastAttempt time.Time
	lastSuccess time.Time
	lastError   string
	failures    int
}

func deviceKey(dev config.Device) string {
	return dev.Name + "|" + dev.IP + "|" + dev.Class
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{
		metrics:      make(map[string]deviceCache),
		specsByDev:   make(map[string][]specs.MetricSpec),
		climateByDev: make(map[string]*specs.ClimateSpec),
		writableEPCs: make(map[string]map[byte]struct{}),
		eojByDev:     make(map[string][3]byte),
	}
}

// SetOnUpdate registers a callback invoked after each scrape update.
func (c *Cache) SetOnUpdate(cb UpdateCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onUpdate = cb
}

// SetDeviceSpecs records the filtered metric specs for a device (post-GETMAP).
func (c *Cache) SetDeviceSpecs(dev config.Device, metricSpecs []specs.MetricSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.specsByDev[deviceKey(dev)] = metricSpecs
}

// SetDeviceClimate records the climate spec for a device (e.g. home_ac).
func (c *Cache) SetDeviceClimate(dev config.Device, climate *specs.ClimateSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceKey(dev)
	if climate == nil {
		delete(c.climateByDev, key)
		return
	}
	c.climateByDev[key] = climate
}

// SetWritableEPCs records the writable property map (0x9E) for a device.
func (c *Cache) SetWritableEPCs(dev config.Device, writable map[byte]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writableEPCs[deviceKey(dev)] = writable
}

// GetWritableEPCs returns the writable EPC set for a device, if known.
func (c *Cache) GetWritableEPCs(dev config.Device) (map[byte]struct{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	w, ok := c.writableEPCs[deviceKey(dev)]
	return w, ok
}

// SetDeviceEOJ stores the EOJ for a device (used for SET requests).
func (c *Cache) SetDeviceEOJ(dev config.Device, eoj [3]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eojByDev[deviceKey(dev)] = eoj
}

// GetDeviceEOJ returns the EOJ for a device, if known.
func (c *Cache) GetDeviceEOJ(dev config.Device) ([3]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	eoj, ok := c.eojByDev[deviceKey(dev)]
	return eoj, ok
}

// GetDeviceClimate returns the climate spec for a device, if any (e.g. home_ac).
func (c *Cache) GetDeviceClimate(dev config.Device) *specs.ClimateSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.climateByDev[deviceKey(dev)]
}

// GetDeviceSpecs returns the cached metric specs for a device, if any.
func (c *Cache) GetDeviceSpecs(dev config.Device) ([]specs.MetricSpec, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.specsByDev[deviceKey(dev)]
	return s, ok
}

// Get returns aggregated scrape status and a copy of cached metrics for a device.
func (c *Cache) Get(dev config.Device) (success bool, durationSec float64, lastScrape time.Time, metrics map[string]echonet.MetricValue) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	dc, ok := c.metrics[deviceKey(dev)]
	if !ok {
		return false, 0, time.Time{}, nil
	}
	now := time.Now()
	latestAttempt := time.Time{}
	latestSuccess := time.Time{}
	latestDuration := 0.0
	aggregatedSuccess := false
	for _, gs := range dc.groups {
		if gs.lastAttempt.After(latestAttempt) {
			latestAttempt = gs.lastAttempt
			latestDuration = gs.durationSec
		}
		if gs.lastSuccess.After(latestSuccess) {
			latestSuccess = gs.lastSuccess
		}
		if gs.success {
			ttl := gs.interval * 2
			if ttl < 5*time.Second {
				ttl = 5 * time.Second
			}
			if now.Sub(gs.lastAttempt) <= ttl {
				aggregatedSuccess = true
			}
		}
	}
	mcopy := make(map[string]echonet.MetricValue, len(dc.metrics))
	for k, v := range dc.metrics {
		mcopy[k] = v
	}
	return aggregatedSuccess, latestDuration, latestSuccess, mcopy
}

// GetInfo returns the latest cached generic device identity.
func (c *Cache) GetInfo(dev config.Device) echonet.DeviceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	dc, ok := c.metrics[deviceKey(dev)]
	if !ok {
		return echonet.DeviceInfo{}
	}
	return dc.info
}

// Update merges a scrape result into the cache for a device/group.
func (c *Cache) Update(dev config.Device, groupID string, interval time.Duration, success bool, durationSec float64, metrics map[string]echonet.MetricValue, errMsg string) {
	c.mu.Lock()
	key := deviceKey(dev)
	dc := c.metrics[key]
	if dc.groups == nil {
		dc.groups = make(map[string]groupStatus)
	}
	if dc.metrics == nil {
		dc.metrics = make(map[string]echonet.MetricValue)
	}
	now := time.Now()
	gs := dc.groups[groupID]
	gs.interval = interval
	gs.success = success
	gs.durationSec = durationSec
	gs.lastAttempt = now
	if success {
		gs.lastSuccess = now
		gs.lastError = ""
		gs.failures = 0
		for k, v := range metrics {
			dc.metrics[k] = v
		}
	} else {
		gs.failures++
		if errMsg == "" {
			errMsg = "scrape failed"
		}
		gs.lastError = errMsg
	}
	dc.groups[groupID] = gs
	c.metrics[key] = dc

	cb := c.onUpdate
	var devSpecs []specs.MetricSpec
	var writable map[byte]struct{}
	var climateSpec *specs.ClimateSpec
	if cb != nil {
		devSpecs = c.specsByDev[key]
		writable = c.writableEPCs[key]
		climateSpec = c.climateByDev[key]
	}
	info := dc.info
	allMetrics := make(map[string]echonet.MetricValue, len(dc.metrics))
	for k, v := range dc.metrics {
		allMetrics[k] = v
	}
	c.mu.Unlock()

	if cb != nil {
		cb(dev, info, allMetrics, devSpecs, writable, climateSpec, success)
	}
}

// UpdateInfo stores generic device identity properties.
// Falls back to config-level manufacturer/model if the device doesn't report them.
func (c *Cache) UpdateInfo(dev config.Device, info echonet.DeviceInfo) {
	if info.Manufacturer == "" && dev.Manufacturer != "" {
		info.Manufacturer = dev.Manufacturer
	}
	if info.ProductCode == "" && dev.Model != "" {
		info.ProductCode = dev.Model
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceKey(dev)
	dc := c.metrics[key]
	dc.info = info
	c.metrics[key] = dc
}


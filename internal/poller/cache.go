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
// lightSpec is non-nil for device classes that support HA light (e.g. general_lighting).
type UpdateCallback func(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec, lightSpec *specs.LightSpec, success bool)

// Cache holds the latest scraped metrics per device. Safe for concurrent use.
type Cache struct {
	mu            sync.RWMutex
	metrics       map[string]deviceCache
	onUpdate      UpdateCallback
	specsByDev    map[string][]specs.MetricSpec   // filtered specs per device key
	climateByDev  map[string]*specs.ClimateSpec   // device key -> climate spec if AC
	lightByDev    map[string]*specs.LightSpec    // device key -> light spec if lighting
	writableEPCs  map[string]map[byte]struct{}    // device key -> set of writable EPCs (from 0x9E)
	eojByDev      map[string][3]byte              // device key -> EOJ for SET requests
	notifyEPCs    map[string]map[byte]struct{}    // device key -> set of EPCs the device pushes (from 0x9D)
	lastPush      map[string]map[byte]time.Time   // device key -> EPC -> last INF receive time
	forcePolling  bool                             // ignore STATMAP, always poll everything
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
		lightByDev:   make(map[string]*specs.LightSpec),
		writableEPCs: make(map[string]map[byte]struct{}),
		eojByDev:     make(map[string][3]byte),
		notifyEPCs:   make(map[string]map[byte]struct{}),
		lastPush:     make(map[string]map[byte]time.Time),
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

// SetDeviceLight records the light spec for a device (e.g. general_lighting).
func (c *Cache) SetDeviceLight(dev config.Device, light *specs.LightSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceKey(dev)
	if light == nil {
		delete(c.lightByDev, key)
		return
	}
	c.lightByDev[key] = light
}

// GetDeviceLight returns the light spec for a device, if any.
func (c *Cache) GetDeviceLight(dev config.Device) *specs.LightSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lightByDev[deviceKey(dev)]
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
	var lightSpec *specs.LightSpec
	if cb != nil {
		devSpecs = c.specsByDev[key]
		writable = c.writableEPCs[key]
		climateSpec = c.climateByDev[key]
		lightSpec = c.lightByDev[key]
	}
	info := dc.info
	allMetrics := make(map[string]echonet.MetricValue, len(dc.metrics))
	for k, v := range dc.metrics {
		allMetrics[k] = v
	}
	c.mu.Unlock()

	if cb != nil {
		cb(dev, info, allMetrics, devSpecs, writable, climateSpec, lightSpec, success)
	}
}

// SetNotificationEPCs records the notification property map (0x9D / STATMAP) for a device.
func (c *Cache) SetNotificationEPCs(dev config.Device, notify map[byte]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifyEPCs[deviceKey(dev)] = notify
}

// GetNotificationEPCs returns the STATMAP (0x9D) for a device, if known.
func (c *Cache) GetNotificationEPCs(dev config.Device) (map[byte]struct{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.notifyEPCs[deviceKey(dev)]
	return n, ok
}

// RecordPush records that an INF notification was received for the given EPCs.
func (c *Cache) RecordPush(dev config.Device, epcs []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceKey(dev)
	if c.lastPush[key] == nil {
		c.lastPush[key] = make(map[byte]time.Time)
	}
	now := time.Now()
	for _, epc := range epcs {
		c.lastPush[key][epc] = now
	}
}

// SetForcePolling sets whether to ignore STATMAP and always poll.
func (c *Cache) SetForcePolling(force bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forcePolling = force
}

// ShouldSkipPoll returns true if the EPC is in the device's STATMAP and was
// pushed via INF within the given freshness window.
func (c *Cache) ShouldSkipPoll(dev config.Device, epc byte, freshness time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.forcePolling {
		return false
	}
	key := deviceKey(dev)
	notify, ok := c.notifyEPCs[key]
	if !ok {
		return false
	}
	if _, inMap := notify[epc]; !inMap {
		return false
	}
	pushTimes := c.lastPush[key]
	if pushTimes == nil {
		return false
	}
	lastT, ok := pushTimes[epc]
	if !ok {
		return false
	}
	return time.Since(lastT) < freshness
}

// UpdateFromINF merges properties received from an INF notification into
// the cache and triggers the onUpdate callback.
func (c *Cache) UpdateFromINF(dev config.Device, metrics map[string]echonet.MetricValue) {
	c.mu.Lock()
	key := deviceKey(dev)
	dc := c.metrics[key]
	if dc.metrics == nil {
		dc.metrics = make(map[string]echonet.MetricValue)
	}
	for k, v := range metrics {
		dc.metrics[k] = v
	}
	c.metrics[key] = dc

	cb := c.onUpdate
	var devSpecs []specs.MetricSpec
	var writable map[byte]struct{}
	var climateSpec *specs.ClimateSpec
	var lightSpec *specs.LightSpec
	if cb != nil {
		devSpecs = c.specsByDev[key]
		writable = c.writableEPCs[key]
		climateSpec = c.climateByDev[key]
		lightSpec = c.lightByDev[key]
	}
	info := dc.info
	allMetrics := make(map[string]echonet.MetricValue, len(dc.metrics))
	for k, v := range dc.metrics {
		allMetrics[k] = v
	}
	c.mu.Unlock()

	if cb != nil {
		cb(dev, info, allMetrics, devSpecs, writable, climateSpec, lightSpec, true)
	}
}

// FindDeviceByIPAndEOJ returns the configured device matching an IP and SEOJ class,
// or ok=false if no match is found.
func (c *Cache) FindDeviceByIPAndEOJ(ip string, seoj [3]byte, devices []config.Device) (config.Device, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, dev := range devices {
		if dev.IP != ip {
			continue
		}
		key := deviceKey(dev)
		eoj, ok := c.eojByDev[key]
		if !ok {
			continue
		}
		if eoj[0] == seoj[0] && eoj[1] == seoj[1] && eoj[2] == seoj[2] {
			return dev, true
		}
	}
	return config.Device{}, false
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


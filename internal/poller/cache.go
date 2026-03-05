package poller

import (
	"sort"
	"sync"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
)

// Cache holds the latest scraped metrics per device. Safe for concurrent use.
type Cache struct {
	mu      sync.RWMutex
	metrics map[string]deviceCache
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

// DeviceKey returns a unique key for a configured device.
func DeviceKey(dev config.Device) string {
	return dev.Name + "|" + dev.IP + "|" + dev.Class
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{metrics: make(map[string]deviceCache)}
}

// Get returns aggregated scrape status and a copy of cached metrics for a device.
func (c *Cache) Get(dev config.Device) (success bool, durationSec float64, lastScrape time.Time, metrics map[string]echonet.MetricValue) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	dc, ok := c.metrics[DeviceKey(dev)]
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
	dc, ok := c.metrics[DeviceKey(dev)]
	if !ok {
		return echonet.DeviceInfo{}
	}
	return dc.info
}

// Update merges a scrape result into the cache for a device/group.
func (c *Cache) Update(dev config.Device, groupID string, interval time.Duration, success bool, durationSec float64, metrics map[string]echonet.MetricValue, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := DeviceKey(dev)
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
}

// UpdateInfo stores generic device identity properties.
func (c *Cache) UpdateInfo(dev config.Device, info echonet.DeviceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := DeviceKey(dev)
	dc := c.metrics[key]
	dc.info = info
	c.metrics[key] = dc
}

// StateForAPI returns a JSON-serializable map of all cached device state for the HTTP API.
func (c *Cache) StateForAPI(cfg *config.Config) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]interface{})
	devices := make([]map[string]interface{}, 0, len(cfg.Devices))
	for _, dev := range cfg.Devices {
		key := DeviceKey(dev)
		dc, ok := c.metrics[key]
		if !ok {
			devices = append(devices, map[string]interface{}{
				"name": dev.Name, "ip": dev.IP, "class": dev.Class,
				"success": false, "metrics": map[string]interface{}{},
			})
			continue
		}
		success := false
		lastError := ""
		consecutiveFailures := 0
		lastErrorAt := time.Time{}
		for _, gs := range dc.groups {
			if gs.success {
				ttl := gs.interval * 2
				if ttl < 5*time.Second {
					ttl = 5 * time.Second
				}
				if time.Since(gs.lastAttempt) <= ttl {
					success = true
					break
				}
			}
			if gs.failures > consecutiveFailures {
				consecutiveFailures = gs.failures
			}
			if gs.lastError != "" && gs.lastAttempt.After(lastErrorAt) {
				lastErrorAt = gs.lastAttempt
				lastError = gs.lastError
			}
		}
		metrics := make(map[string]interface{})
		for k, v := range dc.metrics {
			metrics[k] = map[string]interface{}{"value": v.Value, "type": v.Type}
		}
		devices = append(devices, map[string]interface{}{
			"name": dev.Name, "ip": dev.IP, "class": dev.Class,
			"success":      success,
			"manufacturer": dc.info.Manufacturer, "product_code": dc.info.ProductCode, "uid": dc.info.UID,
			"metrics":              metrics,
			"last_error":           lastError,
			"consecutive_failures": consecutiveFailures,
		})
	}
	sort.Slice(devices, func(i, j int) bool {
		a, _ := devices[i]["name"].(string)
		b, _ := devices[j]["name"].(string)
		return a < b
	})
	out["devices"] = devices
	return out
}

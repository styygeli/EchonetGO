package poller

import (
	"context"
	"sort"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/specs"
)

var pollerLog = logging.New("poller")

// Start begins background scrapers for all configured devices. Call with a context
// that is cancelled on shutdown.
func (c *Cache) Start(ctx context.Context, cfg *config.Config, deviceSpecs map[string]*specs.DeviceSpec) {
	client := echonet.NewClient(cfg.ScrapeTimeoutSec)
	probeTimeoutSec := cfg.ScrapeTimeoutSec
	if probeTimeoutSec > 3 {
		probeTimeoutSec = 3
	}
	if probeTimeoutSec < 1 {
		probeTimeoutSec = 1
	}
	probeClient := echonet.NewClient(probeTimeoutSec)

	for _, dev := range cfg.Devices {
		spec, ok := deviceSpecs[dev.Class]
		if !ok || spec == nil {
			pollerLog.Errorf("unknown class %q for device %s, skipping", dev.Class, dev.Name)
			continue
		}
		activeEOJ := resolveEOJInstance(probeClient, dev, spec.EOJ)
		go c.runDeviceInfoRefresher(ctx, client, dev, activeEOJ)

		activeMetrics := spec.Metrics
		readable, err := client.GetReadablePropertyMap(dev.IP, activeEOJ)
		if err != nil {
			pollerLog.Warnf("device %s (%s): failed to read GETMAP (0x9F), using configured EPCs: %v", dev.Name, dev.IP, err)
		} else {
			var unsupported []byte
			activeMetrics, unsupported = filterMetricsByReadableMap(spec.Metrics, readable)
			if len(unsupported) > 0 {
				pollerLog.Warnf("device %s (%s): skipping unsupported EPCs from GETMAP: %v", dev.Name, dev.IP, unsupported)
			}
		}
		if len(activeMetrics) == 0 {
			pollerLog.Errorf("device %s (%s): no readable configured EPCs after GETMAP filter, skipping", dev.Name, dev.IP)
			continue
		}

		devDefaultInterval := spec.DefaultScrapeInterval
		if dev.ScrapeInterval != "" {
			d, err := time.ParseDuration(dev.ScrapeInterval)
			if err != nil {
				pollerLog.Warnf("device %s invalid scrape_interval %q: %v", dev.Name, dev.ScrapeInterval, err)
			} else if d > 0 {
				devDefaultInterval = d
			}
		}

		byInterval := make(map[time.Duration][]specs.MetricSpec)
		for _, m := range activeMetrics {
			iv := m.ScrapeInterval
			if iv <= 0 {
				iv = devDefaultInterval
			}
			byInterval[iv] = append(byInterval[iv], m)
		}
		intervals := make([]time.Duration, 0, len(byInterval))
		for iv := range byInterval {
			intervals = append(intervals, iv)
		}
		sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })

		for i, interval := range intervals {
			metrics := byInterval[interval]
			groupID := interval.String()
			initialDelay := time.Duration(i) * 500 * time.Millisecond
			if initialDelay > interval/2 {
				initialDelay = interval / 2
			}
			go c.runScraper(ctx, client, dev, activeEOJ, metrics, groupID, interval, initialDelay)
		}
	}
}

func resolveEOJInstance(client *echonet.Client, dev config.Device, configured [3]byte) [3]byte {
	if probeEOJ(client, dev.IP, configured) {
		return configured
	}
	for inst := byte(0x01); inst <= 0x0F; inst++ {
		if inst == configured[2] {
			continue
		}
		candidate := configured
		candidate[2] = inst
		if !probeEOJ(client, dev.IP, candidate) {
			continue
		}
		pollerLog.Warnf("device %s (%s): configured EOJ instance 0x%02x not responsive, using discovered instance 0x%02x",
			dev.Name, dev.IP, configured[2], inst)
		return candidate
	}
	pollerLog.Warnf("device %s (%s): no responsive EOJ instance found for class 0x%02x%02x; keeping configured instance 0x%02x",
		dev.Name, dev.IP, configured[0], configured[1], configured[2])
	return configured
}

func probeEOJ(client *echonet.Client, ip string, eoj [3]byte) bool {
	props, err := client.GetProps(ip, eoj, []byte{0x80})
	return err == nil && len(props) > 0
}

func filterMetricsByReadableMap(metrics []specs.MetricSpec, readable map[byte]struct{}) ([]specs.MetricSpec, []byte) {
	filtered := make([]specs.MetricSpec, 0, len(metrics))
	unsupported := make([]byte, 0)
	for _, m := range metrics {
		if _, ok := readable[m.EPC]; ok {
			filtered = append(filtered, m)
			continue
		}
		unsupported = append(unsupported, m.EPC)
	}
	return filtered, unsupported
}

func (c *Cache) runDeviceInfoRefresher(ctx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte) {
	c.refreshDeviceInfo(client, dev, eoj)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshDeviceInfo(client, dev, eoj)
		}
	}
}

func (c *Cache) refreshDeviceInfo(client *echonet.Client, dev config.Device, eoj [3]byte) {
	info, err := client.GetDeviceInfo(dev.IP, eoj)
	if err != nil {
		pollerLog.Warnf("device %s (%s): device info read failed: %v", dev.Name, dev.IP, err)
		return
	}
	c.UpdateInfo(dev, info)
}

func (c *Cache) runScraper(ctx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte, metrics []specs.MetricSpec, groupID string, interval, initialDelay time.Duration) {
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialDelay):
		}
	}
	c.scrapeOnce(client, dev, eoj, metrics, groupID, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scrapeOnce(client, dev, eoj, metrics, groupID, interval)
		}
	}
}

func (c *Cache) scrapeOnce(client *echonet.Client, dev config.Device, eoj [3]byte, metrics []specs.MetricSpec, groupID string, interval time.Duration) {
	epcs := make([]byte, 0, len(metrics))
	for _, m := range metrics {
		epcs = append(epcs, m.EPC)
	}
	start := time.Now()
	props, err := client.GetProps(dev.IP, eoj, epcs)
	durationSec := time.Since(start).Seconds()
	if err != nil {
		pollerLog.Errorf("scrape %s (%s): %v", dev.Name, dev.IP, err)
		c.Update(dev, groupID, interval, false, durationSec, nil)
		return
	}
	out := echonet.ParsePropsToMetrics(props, metrics)
	if len(out) < len(metrics) {
		pollerLog.Warnf("device %s (%s): parsed %d/%d metrics for group %s; missing=%v",
			dev.Name, dev.IP, len(out), len(metrics), groupID, missingMetricNames(metrics, out))
	}
	c.Update(dev, groupID, interval, true, durationSec, out)
}

func missingMetricNames(metrics []specs.MetricSpec, out map[string]echonet.MetricValue) []string {
	missing := make([]string, 0)
	for _, m := range metrics {
		if _, ok := out[m.Name]; !ok {
			missing = append(missing, m.Name)
		}
	}
	return missing
}

package poller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/specs"
)

var pollerLog = logging.New("poller")

type deviceWithEOJ struct {
	dev config.Device
	eoj [3]byte
}

// Start begins background scrapers for all configured devices. Call with a context
// that is cancelled on shutdown. Init (probe + GETMAP) runs in parallel per host IP.
// If readyFunc is non-nil, it is called once init is complete and scrapers are launched.
func (c *Cache) Start(ctx context.Context, cfg *config.Config, deviceSpecs map[string]*specs.DeviceSpec, transport *echonet.Transport, readyFunc func()) {
	client := echonet.NewClient(transport, cfg.ScrapeTimeoutSec)
	probeTimeoutSec := cfg.ScrapeTimeoutSec
	if probeTimeoutSec > 3 {
		probeTimeoutSec = 3
	}
	if probeTimeoutSec < 1 {
		probeTimeoutSec = 1
	}
	probeClient := echonet.NewClient(transport, probeTimeoutSec)
	var hostEOJCache sync.Map
	hostDevicePairs := make(map[string][]deviceWithEOJ)
	var hostDevicePairsMu sync.Mutex

	devicesByIP := make(map[string][]config.Device)
	for _, dev := range cfg.Devices {
		spec, ok := deviceSpecs[dev.Class]
		if !ok || spec == nil {
			pollerLog.Errorf("unknown class %q for device %s, skipping", dev.Class, dev.Name)
			continue
		}
		devicesByIP[dev.IP] = append(devicesByIP[dev.IP], dev)
	}

	var wg sync.WaitGroup
	for ip, devices := range devicesByIP {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var pairs []deviceWithEOJ
			for _, dev := range devices {
				spec := deviceSpecs[dev.Class]
				if spec == nil {
					continue
				}
				activeEOJ := resolveEOJInstance(ctx, probeClient, dev, spec.EOJ, &hostEOJCache)
				pollerLog.Infof("device %s (%s): using EOJ 0x%02x%02x%02x", dev.Name, dev.IP, activeEOJ[0], activeEOJ[1], activeEOJ[2])
				pairs = append(pairs, deviceWithEOJ{dev: dev, eoj: activeEOJ})

				mfgCode, err := client.GetManufacturerCode(ctx, dev.IP, activeEOJ)
				if err != nil {
					pollerLog.Warnf("device %s (%s): manufacturer code read failed, using generic spec: %v", dev.Name, dev.IP, err)
				} else if mfgCode != "" {
					vendorKey := dev.Class + "_" + mfgCode
					if vendorSpec := deviceSpecs[vendorKey]; vendorSpec != nil {
						spec = vendorSpec
						pollerLog.Infof("device %s (%s): using vendor-specific spec %s", dev.Name, dev.IP, vendorKey)
					}
				}

				c.SetDeviceEOJ(dev, activeEOJ)

				activeMetrics := spec.Metrics
				readable, err := client.GetReadablePropertyMap(ctx, dev.IP, activeEOJ)
				if err != nil {
					pollerLog.Warnf("device %s (%s): failed to read GETMAP (0x9F), using configured EPCs: %v", dev.Name, dev.IP, err)
				} else {
					var unsupported []byte
					activeMetrics, unsupported = filterMetricsByReadableMap(spec.Metrics, readable)
					if len(unsupported) > 0 {
						pollerLog.Warnf("device %s (%s): skipping unsupported EPCs from GETMAP: %v", dev.Name, dev.IP, unsupported)
					}
				}
			writable, err := client.GetWritablePropertyMap(ctx, dev.IP, activeEOJ)
			if err != nil {
				pollerLog.Warnf("device %s (%s): failed to read writable property map (0x9E): %v", dev.Name, dev.IP, err)
			} else {
				c.SetWritableEPCs(dev, writable)
			}
			if cfg.NotificationsEnabled {
				notify, err := client.GetNotificationPropertyMap(ctx, dev.IP, activeEOJ)
				if err != nil {
					pollerLog.Warnf("device %s (%s): failed to read STATMAP (0x9D): %v", dev.Name, dev.IP, err)
				} else {
					c.SetNotificationEPCs(dev, notify)
					epcs := make([]byte, 0, len(notify))
					for epc := range notify {
						epcs = append(epcs, epc)
					}
					pollerLog.Infof("device %s (%s): STATMAP has %d notification EPCs: %s",
						dev.Name, dev.IP, len(notify), echonet.FormatEPCList(epcs))
				}
			}
				if len(activeMetrics) == 0 {
					pollerLog.Errorf("device %s (%s): no readable configured EPCs after GETMAP filter, skipping", dev.Name, dev.IP)
					continue
				}
				c.SetDeviceSpecs(dev, activeMetrics)
				c.SetDeviceClimate(dev, spec.Climate)

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
			hostDevicePairsMu.Lock()
			hostDevicePairs[ip] = pairs
			hostDevicePairsMu.Unlock()
		}()
	}
	wg.Wait()

	if readyFunc != nil {
		readyFunc()
	}
	for _, pairs := range hostDevicePairs {
		go c.runDeviceInfoRefresher(ctx, client, pairs)
	}
}

func resolveEOJInstance(ctx context.Context, client *echonet.Client, dev config.Device, configured [3]byte, hostEOJCache *sync.Map) [3]byte {
	if eoj, ok := resolveEOJFromNodeProfile(ctx, client, dev, configured, hostEOJCache); ok {
		return eoj
	}
	ok, probeErr := probeEOJ(ctx, client, dev.IP, configured)
	if ok {
		return configured
	}
	if isProbeTimeout(probeErr) {
		pollerLog.Warnf("device %s (%s): configured EOJ probe timed out; skipping instance sweep and keeping instance 0x%02x",
			dev.Name, dev.IP, configured[2])
		return configured
	}
	for inst := byte(0x01); inst <= 0x0F; inst++ {
		if inst == configured[2] {
			continue
		}
		candidate := configured
		candidate[2] = inst
		ok, err := probeEOJ(ctx, client, dev.IP, candidate)
		if !ok {
			if isProbeTimeout(err) {
				pollerLog.Warnf("device %s (%s): EOJ instance sweep timed out at instance 0x%02x; keeping configured instance 0x%02x",
					dev.Name, dev.IP, inst, configured[2])
				return configured
			}
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

func resolveEOJFromNodeProfile(ctx context.Context, client *echonet.Client, dev config.Device, configured [3]byte, hostEOJCache *sync.Map) ([3]byte, bool) {
	if val, ok := hostEOJCache.Load(dev.IP); ok {
		if instances := val.([][3]byte); len(instances) > 0 {
			return selectEOJFromInstances(dev, configured, instances)
		}
	}
	props, err := client.GetProps(ctx, dev.IP, [3]byte{0x0E, 0xF0, 0x01}, []byte{0xD6})
	if err != nil {
		pollerLog.Warnf("device %s (%s): node profile probe (0x0EF001/D6) failed: %v", dev.Name, dev.IP, err)
		return configured, false
	}
	var instances [][3]byte
	for _, p := range props {
		if p.EPC != 0xD6 || len(p.EDT) == 0 {
			continue
		}
		instances = decodeInstanceList(p.EDT)
		break
	}
	if len(instances) == 0 {
		pollerLog.Warnf("device %s (%s): node profile instance list (D6) missing/empty", dev.Name, dev.IP)
		return configured, false
	}
	hostEOJCache.Store(dev.IP, instances)
	pollerLog.Infof("device %s (%s): discovered EOJs from node profile: %s", dev.Name, dev.IP, formatEOJList(instances))
	return selectEOJFromInstances(dev, configured, instances)
}

func selectEOJFromInstances(dev config.Device, configured [3]byte, instances [][3]byte) ([3]byte, bool) {
	for _, inst := range instances {
		if inst[0] != configured[0] || inst[1] != configured[1] {
			continue
		}
		if inst[2] != configured[2] {
			pollerLog.Warnf("device %s (%s): configured EOJ instance 0x%02x replaced by node-profile instance 0x%02x",
				dev.Name, dev.IP, configured[2], inst[2])
		}
		return inst, true
	}
	pollerLog.Warnf("device %s (%s): configured class 0x%02x%02x not present in node profile list", dev.Name, dev.IP, configured[0], configured[1])
	return configured, false
}

func decodeInstanceList(edt []byte) [][3]byte {
	if len(edt) < 1 {
		return nil
	}
	count := int(edt[0])
	maxCount := (len(edt) - 1) / 3
	if count > maxCount {
		count = maxCount
	}
	out := make([][3]byte, 0, count)
	for i := 0; i < count; i++ {
		base := 1 + i*3
		out = append(out, [3]byte{edt[base], edt[base+1], edt[base+2]})
	}
	return out
}

func formatEOJList(eojs [][3]byte) string {
	if len(eojs) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(eojs))
	for _, e := range eojs {
		parts = append(parts, fmt.Sprintf("0x%02x%02x%02x", e[0], e[1], e[2]))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func probeEOJ(ctx context.Context, client *echonet.Client, ip string, eoj [3]byte) (bool, error) {
	props, err := client.GetProps(ctx, ip, eoj, []byte{0x80})
	if err != nil {
		return false, err
	}
	return len(props) > 0, nil
}

func isProbeTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "timeout")
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

func (c *Cache) runDeviceInfoRefresher(ctx context.Context, client *echonet.Client, devices []deviceWithEOJ) {
	c.refreshDeviceInfo(ctx, client, devices)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshDeviceInfo(ctx, client, devices)
		}
	}
}

func (c *Cache) refreshDeviceInfo(ctx context.Context, client *echonet.Client, devices []deviceWithEOJ) {
	for _, d := range devices {
		if err := ctx.Err(); err != nil {
			return
		}
		info, err := client.GetDeviceInfo(ctx, d.dev.IP, d.eoj, d.dev.Model)
		if err != nil {
			pollerLog.Warnf("device %s (%s): device info read failed: %v", d.dev.Name, d.dev.IP, err)
			continue
		}
		c.UpdateInfo(d.dev, info)
	}
}

func (c *Cache) runScraper(ctx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte, metrics []specs.MetricSpec, groupID string, interval, initialDelay time.Duration) {
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialDelay):
		}
	}
	c.scrapeOnce(ctx, client, dev, eoj, metrics, groupID, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scrapeOnce(ctx, client, dev, eoj, metrics, groupID, interval)
		}
	}
}

func (c *Cache) scrapeOnce(ctx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte, metrics []specs.MetricSpec, groupID string, interval time.Duration) {
	freshness := interval * 2
	seen := make(map[byte]struct{}, len(metrics))
	epcs := make([]byte, 0, len(metrics))
	var skipped []byte
	for _, m := range metrics {
		if _, dup := seen[m.EPC]; !dup {
			if c.ShouldSkipPoll(dev, m.EPC, freshness) {
				skipped = append(skipped, m.EPC)
				seen[m.EPC] = struct{}{}
				continue
			}
			epcs = append(epcs, m.EPC)
			seen[m.EPC] = struct{}{}
		}
		if m.MultiplierEPC != 0 {
			if _, dup := seen[m.MultiplierEPC]; !dup {
				epcs = append(epcs, m.MultiplierEPC)
				seen[m.MultiplierEPC] = struct{}{}
			}
		}
	}
	if len(skipped) > 0 {
		pollerLog.Debugf("device %s (%s) group %s: skipping %d EPCs recently pushed via INF: %s",
			dev.Name, dev.IP, groupID, len(skipped), echonet.FormatEPCList(skipped))
	}
	if len(epcs) == 0 {
		return
	}
	start := time.Now()
	props, err := client.GetProps(ctx, dev.IP, eoj, epcs)
	durationSec := time.Since(start).Seconds()
	if err != nil {
		pollerLog.Errorf("scrape %s (%s): %v", dev.Name, dev.IP, err)
		c.Update(dev, groupID, interval, false, durationSec, nil, err.Error())
		return
	}
	out := echonet.ParsePropsToMetrics(props, metrics)
	if len(out) < len(metrics) {
		pollerLog.Warnf("device %s (%s): parsed %d/%d metrics for group %s; missing=%v",
			dev.Name, dev.IP, len(out), len(metrics), groupID, missingMetricNames(metrics, out))
	}
	c.Update(dev, groupID, interval, true, durationSec, out, "")
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

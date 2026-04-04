package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

const namespace = "echonet"

type enumValueMeta struct {
	rawInt     int
	stateLabel string
}

type enumMeta struct {
	desc   *prometheus.Desc
	values []enumValueMeta
}

// Collector implements prometheus.Collector and serves cached metrics
// from the detached poller. Collect reads a snapshot from the cache
// under an RLock and emits const metrics; it never triggers network I/O.
type Collector struct {
	cfg            *config.Config
	cache          *poller.Cache
	extraLabelKeys []string

	scrapeSuccess       *prometheus.Desc
	scrapeDuration      *prometheus.Desc
	lastScrapeTimestamp *prometheus.Desc
	deviceInfo          *prometheus.Desc
	metricDescs         map[string]map[string]*prometheus.Desc
	enumMetricDescs     map[string]map[string]enumMeta
}

// NewCollector builds descriptors from device specs and returns a collector
// that reads from the given cache. Register on a dedicated prometheus.Registry.
func NewCollector(cfg *config.Config, cache *poller.Cache, deviceSpecs map[string]*specs.DeviceSpec) *Collector {
	extraLabelKeys := collectExtraLabelKeys(cfg.Devices)
	allLabelNames := append([]string{"device", "ip", "class"}, extraLabelKeys...)
	infoLabelNames := append(append([]string{}, allLabelNames...), "manufacturer", "product_code", "uid")

	c := &Collector{
		cfg:            cfg,
		cache:          cache,
		extraLabelKeys: extraLabelKeys,
		scrapeSuccess: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "scrape_success"),
			"1 if the last scrape of this device succeeded, 0 otherwise.",
			allLabelNames,
			nil,
		),
		scrapeDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
			"Duration of the last scrape for this device in seconds.",
			allLabelNames,
			nil,
		),
		lastScrapeTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "last_scrape_timestamp_seconds"),
			"Unix timestamp of the last successful scrape for this device.",
			allLabelNames,
			nil,
		),
		deviceInfo: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "device_info"),
			"Static device identity labels from generic ECHONET properties.",
			infoLabelNames,
			nil,
		),
		metricDescs:     make(map[string]map[string]*prometheus.Desc),
		enumMetricDescs: make(map[string]map[string]enumMeta),
	}

	for class, spec := range deviceSpecs {
		if spec == nil {
			continue
		}
		c.metricDescs[class] = make(map[string]*prometheus.Desc)
		c.enumMetricDescs[class] = make(map[string]enumMeta)
		for _, m := range spec.Metrics {
			subsystem := subsystemForClass(class)
			c.metricDescs[class][m.Name] = prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsystem, m.Name),
				m.Help,
				allLabelNames,
				nil,
			)
			if len(m.Enum) > 0 {
				c.enumMetricDescs[class][m.Name] = buildEnumMeta(subsystem, m, allLabelNames)
			}
		}
	}

	return c
}

// Describe implements prometheus.Collector. No cache access; all descriptors
// are built once at construction time.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.scrapeSuccess
	ch <- c.scrapeDuration
	ch <- c.lastScrapeTimestamp
	ch <- c.deviceInfo
	for _, descs := range c.metricDescs {
		for _, d := range descs {
			ch <- d
		}
	}
	for _, byMetric := range c.enumMetricDescs {
		for _, meta := range byMetric {
			ch <- meta.desc
		}
	}
}

// Collect implements prometheus.Collector. Snapshots each device's cached
// state via the cache's RLock and emits const metrics. Safe for concurrent
// calls — all collector state is read-only after construction and the cache
// is internally synchronized.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	for _, dev := range c.cfg.Devices {
		c.collectDevice(ch, dev)
	}
}

func (c *Collector) collectDevice(ch chan<- prometheus.Metric, dev config.Device) {
	success, durationSec, lastScrape, metrics := c.cache.Get(dev)
	info := c.cache.GetInfo(dev)
	labels := c.labelValues(dev)

	successVal := 0.0
	if success {
		successVal = 1
	}
	ch <- prometheus.MustNewConstMetric(c.scrapeSuccess, prometheus.GaugeValue, successVal, labels...)
	ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, durationSec, labels...)

	infoLabels := append(append([]string{}, labels...), info.Manufacturer, info.ProductCode, info.UID)
	ch <- prometheus.MustNewConstMetric(c.deviceInfo, prometheus.GaugeValue, 1, infoLabels...)

	if success && !lastScrape.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.lastScrapeTimestamp, prometheus.GaugeValue, float64(lastScrape.Unix()), labels...)
	}

	c.collectDeviceMetrics(ch, dev.Class, labels, metrics)
}

func (c *Collector) collectDeviceMetrics(ch chan<- prometheus.Metric, class string, labels []string, metrics map[string]echonet.MetricValue) {
	classDescs, ok := c.metricDescs[class]
	if !ok {
		return
	}
	classEnumDescs := c.enumMetricDescs[class]

	for name, mv := range metrics {
		desc, ok := classDescs[name]
		if !ok {
			continue
		}
		vt := prometheus.GaugeValue
		if mv.Type == "counter" {
			vt = prometheus.CounterValue
		}
		ch <- prometheus.MustNewConstMetric(desc, vt, mv.Value, labels...)

		meta, hasEnum := classEnumDescs[name]
		if !hasEnum {
			continue
		}
		raw := int(math.Round(mv.Value))
		
		enumLabels := make([]string, len(labels)+1)
		copy(enumLabels, labels)
		for _, valMeta := range meta.values {
			v := 0.0
			if raw == valMeta.rawInt {
				v = 1
			}
			enumLabels[len(labels)] = valMeta.stateLabel
			ch <- prometheus.MustNewConstMetric(meta.desc, prometheus.GaugeValue, v, enumLabels...)
		}
	}
}

func (c *Collector) labelValues(dev config.Device) []string {
	values := make([]string, 0, 3+len(c.extraLabelKeys))
	values = append(values, dev.Name, dev.IP, dev.Class)
	for _, k := range c.extraLabelKeys {
		values = append(values, dev.Labels[k])
	}
	return values
}

func collectExtraLabelKeys(devices []config.Device) []string {
	set := make(map[string]struct{})
	for _, dev := range devices {
		for k := range dev.Labels {
			if k == "" {
				continue
			}
			set[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func subsystemForClass(class string) string {
	switch class {
	case "storage_battery":
		return "battery"
	case "home_solar":
		return "solar"
	case "home_ac":
		return "ac"
	default:
		return class
	}
}

func sanitizeEnumLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "value"
	}
	return out
}

func buildEnumMeta(subsystem string, m specs.MetricSpec, labels []string) enumMeta {
	descLabels := append(append([]string{}, labels...), "state")

	// Use _info suffix for the labeled state metrics to avoid collisions with
	// the raw numeric gauge (which might already have _state or other suffixes).
	metricName := m.Name + "_info"

	help := m.Help
	if help == "" {
		help = fmt.Sprintf("Current state of %s.", m.Name)
	}
	help = fmt.Sprintf("%s (1 if active, else 0)", strings.TrimSuffix(help, "."))

	desc := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, metricName),
		help,
		descLabels,
		nil,
	)

	keys := make([]int, 0, len(m.Enum))
	for value := range m.Enum {
		keys = append(keys, value)
	}
	sort.Ints(keys)

	var values []enumValueMeta
	usedLabels := make(map[string]struct{}, len(m.Enum))

	for _, value := range keys {
		label := m.Enum[value]
		stateLabel := sanitizeEnumLabel(label)
		if _, exists := usedLabels[stateLabel]; exists {
			stateLabel = fmt.Sprintf("%s_0x%x", stateLabel, value)
		}
		usedLabels[stateLabel] = struct{}{}
		values = append(values, enumValueMeta{
			rawInt:     value,
			stateLabel: stateLabel,
		})
	}

	return enumMeta{
		desc:   desc,
		values: values,
	}
}

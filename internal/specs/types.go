package specs

import "time"

// DeviceSpec defines one ECHONET device class (e.g. storage_battery).
type DeviceSpec struct {
	EOJ                   [3]byte
	Description           string
	DefaultScrapeInterval time.Duration
	Metrics               []MetricSpec
}

// MetricSpec defines one EPC to poll and how to interpret it.
type MetricSpec struct {
	EPC            byte
	Name           string
	Help           string
	Size           int
	Scale          float64
	Signed         bool
	Invalid        *int
	Type           string // gauge or counter
	Enum           map[int]string
	ScrapeInterval time.Duration

	// Home Assistant MQTT discovery metadata (optional).
	HADeviceClass string // e.g. "power", "energy", "temperature", "enum"
	HAStateClass  string // "measurement", "total_increasing", or ""
	HAUnit        string // e.g. "W", "kWh", "°C"
}

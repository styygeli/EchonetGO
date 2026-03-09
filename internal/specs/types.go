package specs

import "time"

// DeviceSpec defines one ECHONET device class (e.g. storage_battery).
type DeviceSpec struct {
	EOJ                   [3]byte
	Description           string
	DefaultScrapeInterval time.Duration
	Metrics               []MetricSpec
	Climate               *ClimateSpec // optional: for Home AC climate entity
}

// ClimateSpec defines HA climate entity mapping for a device class (e.g. home_ac).
// Mode "off" is handled via operation_status (0x80); other modes map to operation_mode (0xB0) raw values.
type ClimateSpec struct {
	ModeEPC                 byte
	TemperatureEPC          byte
	CurrentTemperatureEPC   byte
	FanModeEPC              byte   // 0 means not used
	MinTemp                 float64
	MaxTemp                 float64
	TempStep                float64
	Modes                   map[string]*int // HA mode label -> ECHONET raw value; nil for "off"
}

// MetricSpec defines one EPC to poll and how to interpret it.
type MetricSpec struct {
	EPC            byte
	Name           string
	Help           string
	Size           int
	Offset         int     // byte offset within the EDT before parsing (default 0)
	Scale          float64
	Signed         bool
	Invalid        *int
	Type           string // gauge or counter
	Enum           map[int]string
	ReverseEnum    map[string]int // label -> raw value (for SET); populated at load from Enum
	ScrapeInterval time.Duration

	// MultiplierEPC, when non-zero, names another EPC whose raw 1-byte value
	// is looked up in MultiplierMap to obtain an additional scale factor.
	// Used for ECHONET cumulative energy EPCs where a separate "unit" EPC
	// (e.g. 0xC2) determines the kWh multiplier.
	MultiplierEPC byte
	MultiplierMap map[int]float64

	// Home Assistant MQTT discovery metadata (optional).
	HADeviceClass string // e.g. "power", "energy", "temperature", "enum"
	HAStateClass  string // "measurement", "total_increasing", or ""
	HAUnit        string // e.g. "W", "kWh", "°C"

	// ExcludeSet if true suppresses publishing a switch/select/number for this writable EPC.
	ExcludeSet bool
}

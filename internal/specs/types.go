package specs

import (
	"strings"
	"time"
)

// DeviceSpec defines one ECHONET device class (e.g. storage_battery).
type DeviceSpec struct {
	EOJ                   [3]byte
	Description           string
	DefaultScrapeInterval time.Duration
	Metrics               []MetricSpec
	Climate               *ClimateSpec // optional: for Home AC climate entity
	Light                 *LightSpec   // optional: for lighting device light entity
}

// ClimateSpec defines HA climate entity mapping for a device class (e.g. home_ac).
// Mode "off" is handled via operation_status (0x80); other modes map to operation_mode (0xB0) raw values.
type ClimateSpec struct {
	ModeEPC               byte
	TemperatureEPC        byte
	CurrentTemperatureEPC byte
	FanModeEPC            byte // 0 means not used
	MinTemp               float64
	MaxTemp               float64
	TempStep              float64
	Modes                 map[string]*int // HA mode label -> ECHONET raw value; nil for "off"
	FanModes              []string        // HA fan mode labels in desired UI order
}

// HandlesEPC reports whether epc is handled by this ClimateSpec.
func (c *ClimateSpec) HandlesEPC(epc byte) bool {
	if c == nil {
		return false
	}
	if epc == 0x80 {
		return true
	}
	return epc == c.ModeEPC || epc == c.TemperatureEPC || epc == c.CurrentTemperatureEPC || (c.FanModeEPC != 0 && epc == c.FanModeEPC)
}

// LightSpec defines HA light entity mapping for lighting device classes.
// Power on/off is always via operation_status (0x80).
type LightSpec struct {
	BrightnessEPC   byte           // 0 = no brightness support
	ColorSettingEPC byte           // 0 = no color setting (enum-based effects)
	ColorSettings   map[string]int // effect label -> ECHONET raw value
	SceneEPC        byte           // 0 = no scene support
	MaxScenes       int            // max scene number
}

// HandlesEPC reports whether epc is handled by this LightSpec.
func (l *LightSpec) HandlesEPC(epc byte) bool {
	if l == nil {
		return false
	}
	if epc == 0x80 {
		return true
	}
	return (l.BrightnessEPC != 0 && epc == l.BrightnessEPC) ||
		(l.ColorSettingEPC != 0 && epc == l.ColorSettingEPC) ||
		(l.SceneEPC != 0 && epc == l.SceneEPC)
}

// MetricSpec defines one EPC to poll and how to interpret it.
type MetricSpec struct {
	EPC            byte
	Name           string
	Help           string
	Size           int
	Offset         int // byte offset within the EDT before parsing (default 0)
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

	// NumberMin/NumberMax override the default HA number entity range.
	// nil means use defaults (0 for min, 255/65535 for max depending on size).
	NumberMin *float64
	NumberMax *float64

	// PreSetEPC, when non-zero, causes the commander to send a SET for this
	// EPC with PreSetValue before executing the main SET for this metric.
	// Used for linked settings (e.g. entering stop-mode before setting vacation days).
	PreSetEPC   byte
	PreSetValue int

	// ExcludeSet if true suppresses publishing a switch/select/number for this writable EPC.
	ExcludeSet bool

	// SetMode controls which ECHONET SET ESV is used for this metric.
	// "" or "setc" (default) uses SetC (0x61) with response validation.
	// "seti" uses SetI (0x60) fire-and-forget without response or verification.
	SetMode string
}

// FindMetricSpecByEPC returns a pointer to the MetricSpec matching epc in specs, or nil.
func FindMetricSpecByEPC(specs []MetricSpec, epc byte) *MetricSpec {
	if epc == 0 {
		return nil
	}
	for i := range specs {
		if specs[i].EPC == epc {
			return &specs[i]
		}
	}
	return nil
}

// FindMetricSpecByName returns a pointer to the MetricSpec matching name in specs, or nil.
func FindMetricSpecByName(specs []MetricSpec, name string) *MetricSpec {
	if name == "" {
		return nil
	}
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i]
		}
	}
	return nil
}

// MetricNameForEPC returns the name of the metric matching epc in specs, or "" if not found.
func MetricNameForEPC(specs []MetricSpec, epc byte) string {
	if ms := FindMetricSpecByEPC(specs, epc); ms != nil {
		return ms.Name
	}
	return ""
}

// WritableEntityType returns "switch", "select", or "number" for a writable metric; "" if not applicable.
func WritableEntityType(ms MetricSpec) string {
	if ms.ExcludeSet {
		return ""
	}
	if len(ms.Enum) == 2 {
		var hasOn, hasOff bool
		for _, label := range ms.Enum {
			switch strings.ToLower(label) {
			case "on":
				hasOn = true
			case "off":
				hasOff = true
			}
		}
		if hasOn && hasOff {
			return "switch"
		}
		return "select"
	}
	if len(ms.Enum) > 2 {
		return "select"
	}
	if len(ms.Enum) == 0 {
		return "number"
	}
	return ""
}

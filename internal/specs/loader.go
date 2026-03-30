package specs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultScrapeInterval = time.Minute

// Load reads device specs from the given directory. Each .yaml file is one
// device class; the filename (without .yaml) is the class id.
// If specsDir is empty, "etc/specs" is used.
func Load(specsDir string) (map[string]*DeviceSpec, error) {
	if specsDir == "" {
		specsDir = "etc/specs"
	}
	return loadFromDir(specsDir)
}

func loadFromDir(dir string) (map[string]*DeviceSpec, error) {
	out := make(map[string]*DeviceSpec)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read specs dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		class := strings.TrimSuffix(e.Name(), ".yaml")
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		spec, err := parseDeviceYAML(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		mergeSuperClass(spec)
		out[class] = spec
	}
	return out, nil
}

type deviceYAML struct {
	EOJ                   []int        `yaml:"eoj"`
	Description           string       `yaml:"description"`
	DefaultScrapeInterval string       `yaml:"default_scrape_interval"`
	Metrics               []metricYAML `yaml:"metrics"`
	Climate               *climateYAML `yaml:"climate"`
	Light                 *lightYAML   `yaml:"light"`
}

type lightYAML struct {
	BrightnessEPC   int            `yaml:"brightness_epc"`
	ColorSettingEPC int            `yaml:"color_setting_epc"`
	ColorSettings   map[string]int `yaml:"color_settings"`
	SceneEPC        int            `yaml:"scene_epc"`
	MaxScenes       int            `yaml:"max_scenes"`
}

type climateYAML struct {
	ModeEPC               int             `yaml:"mode_epc"`
	TemperatureEPC        int             `yaml:"temperature_epc"`
	CurrentTemperatureEPC int             `yaml:"current_temperature_epc"`
	FanModeEPC            int             `yaml:"fan_mode_epc"`
	MinTemp               float64         `yaml:"min_temp"`
	MaxTemp               float64         `yaml:"max_temp"`
	TempStep              float64         `yaml:"temp_step"`
	Modes                 map[string]*int `yaml:"modes"`
}

type preSetYAML struct {
	EPC   int `yaml:"epc"`
	Value int `yaml:"value"`
}

type metricYAML struct {
	EPC            int             `yaml:"epc"`
	Name           string          `yaml:"name"`
	Help           string          `yaml:"help"`
	Size           int             `yaml:"size"`
	Offset         int             `yaml:"offset"`
	Scale          float64         `yaml:"scale"`
	Signed         bool            `yaml:"signed"`
	Invalid        *int            `yaml:"invalid"`
	Type           string          `yaml:"type"`
	Enum           map[int]string  `yaml:"enum"`
	ScrapeInterval string          `yaml:"scrape_interval"`
	HADeviceClass  string          `yaml:"ha_device_class"`
	HAStateClass   string          `yaml:"ha_state_class"`
	HAUnit         string          `yaml:"ha_unit"`
	NumberMin      *float64        `yaml:"number_min"`
	NumberMax      *float64        `yaml:"number_max"`
	PreSet         *preSetYAML     `yaml:"pre_set"`
	ExcludeSet     bool            `yaml:"exclude_set"`
	MultiplierEPC  int             `yaml:"multiplier_epc"`
	MultiplierMap  map[int]float64 `yaml:"multiplier_map"`
	SetMode        string          `yaml:"set_mode"`
}

func parseDeviceYAML(data []byte) (*DeviceSpec, error) {
	var raw deviceYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw.EOJ) != 3 {
		return nil, fmt.Errorf("eoj must have exactly 3 bytes, got %d", len(raw.EOJ))
	}
	devInterval := defaultScrapeInterval
	if raw.DefaultScrapeInterval != "" {
		d, err := time.ParseDuration(raw.DefaultScrapeInterval)
		if err != nil {
			return nil, fmt.Errorf("default_scrape_interval %q: %w", raw.DefaultScrapeInterval, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("default_scrape_interval must be positive, got %v", d)
		}
		devInterval = d
	}
	spec := &DeviceSpec{
		Description:           raw.Description,
		DefaultScrapeInterval: devInterval,
		Metrics:               make([]MetricSpec, 0, len(raw.Metrics)),
	}
	for i, v := range raw.EOJ {
		if v < 0 || v > 0xFF {
			return nil, fmt.Errorf("eoj[%d] must be in range 0..255, got %d", i, v)
		}
		spec.EOJ[i] = byte(v)
	}
	for _, m := range raw.Metrics {
		if m.Size != 0 && m.Size != 1 && m.Size != 2 && m.Size != 4 {
			return nil, fmt.Errorf("metric %s: size must be 0 (auto), 1, 2, or 4", m.Name)
		}
		if m.Type != "gauge" && m.Type != "counter" {
			return nil, fmt.Errorf("metric %s: type must be gauge or counter", m.Name)
		}
		if m.EPC < 0 || m.EPC > 0xFF {
			return nil, fmt.Errorf("metric %s: epc must be in range 0..255, got %d", m.Name, m.EPC)
		}
		if m.Offset < 0 {
			return nil, fmt.Errorf("metric %s: offset must be non-negative, got %d", m.Name, m.Offset)
		}
		if m.MultiplierEPC < 0 || m.MultiplierEPC > 0xFF {
			return nil, fmt.Errorf("metric %s: multiplier_epc must be in range 0..255, got %d", m.Name, m.MultiplierEPC)
		}
		if m.MultiplierEPC != 0 && len(m.MultiplierMap) == 0 {
			return nil, fmt.Errorf("metric %s: multiplier_epc set but multiplier_map is empty", m.Name)
		}
		if m.NumberMin != nil && m.NumberMax != nil && *m.NumberMin >= *m.NumberMax {
			return nil, fmt.Errorf("metric %s: number_min must be less than number_max", m.Name)
		}
		if m.SetMode != "" && m.SetMode != "setc" && m.SetMode != "seti" {
			return nil, fmt.Errorf("metric %s: set_mode must be \"setc\" or \"seti\", got %q", m.Name, m.SetMode)
		}
		if m.PreSet != nil {
			if m.PreSet.EPC < 0 || m.PreSet.EPC > 0xFF {
				return nil, fmt.Errorf("metric %s: pre_set.epc must be in range 0..255, got %d", m.Name, m.PreSet.EPC)
			}
		}
		scale := m.Scale
		if scale == 0 {
			scale = 1
		}
		var enum map[int]string
		if len(m.Enum) > 0 {
			if scale != 1 {
				return nil, fmt.Errorf("metric %s: enum mapping requires scale=1, got %v", m.Name, scale)
			}
			enum = make(map[int]string, len(m.Enum))
			for rawValue, label := range m.Enum {
				if label == "" {
					return nil, fmt.Errorf("metric %s: enum label must not be empty for value %d", m.Name, rawValue)
				}
				if m.Size != 0 && !enumValueFits(rawValue, m.Size, m.Signed) {
					return nil, fmt.Errorf("metric %s: enum value %d doesn't fit size=%d signed=%t", m.Name, rawValue, m.Size, m.Signed)
				}
				enum[rawValue] = label
			}
		}
		help := m.Help
		if help == "" {
			help = LookupEPCName(spec.EOJ, byte(m.EPC))
		}
		interval := devInterval
		if m.ScrapeInterval != "" {
			d, err := time.ParseDuration(m.ScrapeInterval)
			if err != nil {
				return nil, fmt.Errorf("metric %s scrape_interval %q: %w", m.Name, m.ScrapeInterval, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("metric %s scrape_interval must be positive", m.Name)
			}
			interval = d
		}
		haDevice, haState, haUnit := m.HADeviceClass, m.HAStateClass, m.HAUnit
		if haDevice == "" {
			haDevice, haState, haUnit = inferHAMetadata(m.Name, m.Type, len(m.Enum) > 0)
		}
		reverseEnum := make(map[string]int, len(enum))
		for raw, label := range enum {
			reverseEnum[label] = raw
		}
		var preSetEPC byte
		var preSetValue int
		if m.PreSet != nil {
			preSetEPC = byte(m.PreSet.EPC)
			preSetValue = m.PreSet.Value
		}
		spec.Metrics = append(spec.Metrics, MetricSpec{
			EPC:            byte(m.EPC),
			Name:           m.Name,
			Help:           help,
			Size:           m.Size,
			Offset:         m.Offset,
			Scale:          scale,
			Signed:         m.Signed,
			Invalid:        m.Invalid,
			Type:           m.Type,
			Enum:           enum,
			ReverseEnum:    reverseEnum,
			ScrapeInterval: interval,
			MultiplierEPC:  byte(m.MultiplierEPC),
			MultiplierMap:  m.MultiplierMap,
			HADeviceClass:  haDevice,
			HAStateClass:   haState,
			HAUnit:         haUnit,
			NumberMin:      m.NumberMin,
			NumberMax:      m.NumberMax,
			PreSetEPC:      preSetEPC,
			PreSetValue:    preSetValue,
			ExcludeSet:     m.ExcludeSet,
			SetMode:        m.SetMode,
		})
	}
	if raw.Climate != nil {
		cl, err := parseClimateYAML(raw.Climate)
		if err != nil {
			return nil, err
		}
		spec.Climate = cl
	}
	if raw.Light != nil {
		lt, err := parseLightYAML(raw.Light)
		if err != nil {
			return nil, err
		}
		spec.Light = lt
	}
	return spec, nil
}

// mergeSuperClass injects canonical Super Class metrics into spec. Class (and
// vendor) YAML definitions take precedence: if a metric with the same EPC
// already exists, it is left unchanged. Merged metrics with ScrapeInterval == 0
// use the spec's DefaultScrapeInterval.
func mergeSuperClass(spec *DeviceSpec) {
	have := make(map[byte]struct{}, len(spec.Metrics))
	for _, m := range spec.Metrics {
		have[m.EPC] = struct{}{}
	}
	devInterval := spec.DefaultScrapeInterval
	if devInterval <= 0 {
		devInterval = defaultScrapeInterval
	}
	for _, m := range SuperClassMetrics() {
		if _, ok := have[m.EPC]; ok {
			continue
		}
		have[m.EPC] = struct{}{}
		interval := m.ScrapeInterval
		if interval <= 0 {
			interval = devInterval
		}
		haDevice, haState, haUnit := m.HADeviceClass, m.HAStateClass, m.HAUnit
		if haDevice == "" {
			haDevice, haState, haUnit = inferHAMetadata(m.Name, m.Type, len(m.Enum) > 0)
		}
		spec.Metrics = append(spec.Metrics, MetricSpec{
			EPC:            m.EPC,
			Name:           m.Name,
			Help:           m.Help,
			Size:           m.Size,
			Offset:         m.Offset,
			Scale:          m.Scale,
			Signed:         m.Signed,
			Invalid:        m.Invalid,
			Type:           m.Type,
			Enum:           m.Enum,
			ReverseEnum:    m.ReverseEnum,
			ScrapeInterval: interval,
			MultiplierEPC:  m.MultiplierEPC,
			MultiplierMap:  m.MultiplierMap,
			HADeviceClass:  haDevice,
			HAStateClass:   haState,
			HAUnit:         haUnit,
			NumberMin:      m.NumberMin,
			NumberMax:      m.NumberMax,
			PreSetEPC:      m.PreSetEPC,
			PreSetValue:    m.PreSetValue,
			ExcludeSet:     m.ExcludeSet,
			SetMode:        m.SetMode,
		})
	}
}

func parseClimateYAML(raw *climateYAML) (*ClimateSpec, error) {
	if raw == nil {
		return nil, nil
	}
	if raw.ModeEPC < 0 || raw.ModeEPC > 0xFF {
		return nil, fmt.Errorf("climate.mode_epc must be 0..255, got %d", raw.ModeEPC)
	}
	if raw.TemperatureEPC < 0 || raw.TemperatureEPC > 0xFF {
		return nil, fmt.Errorf("climate.temperature_epc must be 0..255, got %d", raw.TemperatureEPC)
	}
	if raw.CurrentTemperatureEPC < 0 || raw.CurrentTemperatureEPC > 0xFF {
		return nil, fmt.Errorf("climate.current_temperature_epc must be 0..255, got %d", raw.CurrentTemperatureEPC)
	}
	if raw.FanModeEPC < 0 || raw.FanModeEPC > 0xFF {
		return nil, fmt.Errorf("climate.fan_mode_epc must be 0..255, got %d", raw.FanModeEPC)
	}
	if raw.MinTemp >= raw.MaxTemp {
		return nil, fmt.Errorf("climate min_temp must be less than max_temp")
	}
	if raw.TempStep <= 0 {
		return nil, fmt.Errorf("climate temp_step must be positive")
	}
	if len(raw.Modes) == 0 {
		return nil, fmt.Errorf("climate.modes must be non-empty")
	}
	return &ClimateSpec{
		ModeEPC:               byte(raw.ModeEPC),
		TemperatureEPC:        byte(raw.TemperatureEPC),
		CurrentTemperatureEPC: byte(raw.CurrentTemperatureEPC),
		FanModeEPC:            byte(raw.FanModeEPC),
		MinTemp:               raw.MinTemp,
		MaxTemp:               raw.MaxTemp,
		TempStep:              raw.TempStep,
		Modes:                 raw.Modes,
	}, nil
}

func parseLightYAML(raw *lightYAML) (*LightSpec, error) {
	if raw == nil {
		return nil, nil
	}
	if raw.BrightnessEPC < 0 || raw.BrightnessEPC > 0xFF {
		return nil, fmt.Errorf("light.brightness_epc must be 0..255, got %d", raw.BrightnessEPC)
	}
	if raw.BrightnessEPC == 0 {
		return nil, fmt.Errorf("light.brightness_epc is required (a light without brightness is just a switch)")
	}
	if raw.ColorSettingEPC < 0 || raw.ColorSettingEPC > 0xFF {
		return nil, fmt.Errorf("light.color_setting_epc must be 0..255, got %d", raw.ColorSettingEPC)
	}
	if raw.ColorSettingEPC != 0 && len(raw.ColorSettings) == 0 {
		return nil, fmt.Errorf("light.color_settings must be non-empty when color_setting_epc is set")
	}
	if raw.SceneEPC < 0 || raw.SceneEPC > 0xFF {
		return nil, fmt.Errorf("light.scene_epc must be 0..255, got %d", raw.SceneEPC)
	}
	if raw.SceneEPC != 0 && raw.MaxScenes <= 0 {
		return nil, fmt.Errorf("light.max_scenes must be positive when scene_epc is set")
	}
	return &LightSpec{
		BrightnessEPC:   byte(raw.BrightnessEPC),
		ColorSettingEPC: byte(raw.ColorSettingEPC),
		ColorSettings:   raw.ColorSettings,
		SceneEPC:        byte(raw.SceneEPC),
		MaxScenes:       raw.MaxScenes,
	}, nil
}

// inferHAMetadata derives HA device_class, state_class, and unit from metric
// naming conventions when not explicitly set in the YAML spec.
func inferHAMetadata(name, metricType string, hasEnum bool) (deviceClass, stateClass, unit string) {
	if hasEnum {
		return "enum", "", ""
	}
	switch {
	case strings.HasSuffix(name, "_power_w"), strings.HasSuffix(name, "_watts"):
		return "power", "measurement", "W"
	case strings.HasSuffix(name, "_kwh"):
		if metricType == "counter" {
			return "energy", "total_increasing", "kWh"
		}
		return "energy", "measurement", "kWh"
	case strings.HasSuffix(name, "_wh"):
		if metricType == "counter" {
			return "energy", "total_increasing", "Wh"
		}
		return "energy", "measurement", "Wh"
	case strings.HasSuffix(name, "_celsius"):
		return "temperature", "measurement", "°C"
	case strings.HasSuffix(name, "_percent"):
		return "", "measurement", "%"
	case strings.HasSuffix(name, "_m3"):
		if metricType == "counter" {
			return "volume", "total_increasing", "m³"
		}
		return "volume", "measurement", "m³"
	}
	if metricType == "gauge" {
		return "", "measurement", ""
	}
	if metricType == "counter" {
		return "", "total_increasing", ""
	}
	return "", "", ""
}

func enumValueFits(v int, size int, signed bool) bool {
	bits := size * 8
	if signed {
		min := -(1 << (bits - 1))
		max := (1 << (bits - 1)) - 1
		return v >= min && v <= max
	}
	max := (1 << bits) - 1
	return v >= 0 && v <= max
}

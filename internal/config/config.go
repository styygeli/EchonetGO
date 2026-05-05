package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Config holds EchonetGO configuration (from etc/ YAML and/or environment).
type Config struct {
	ListenAddr           string     `yaml:"listen_addr" json:"listen_addr"`
	ScrapeTimeoutSec     int        `yaml:"scrape_timeout_sec" json:"scrape_timeout_sec"`
	StrictSourcePort3610 bool       `yaml:"strict_source_port_3610" json:"strict_source_port_3610"`
	ConfigPath           string     `yaml:"-" json:"-"`
	DevicesPath          string     `yaml:"devices_path" json:"devices_path"`
	SpecsDir             string     `yaml:"specs_dir" json:"specs_dir"`
	MetricsEnabled       bool       `yaml:"metrics_enabled" json:"metrics_enabled"`
	NotificationsEnabled bool       `yaml:"notifications_enabled" json:"notifications_enabled"`
	ForcePolling         bool       `yaml:"force_polling" json:"force_polling"`
	MulticastInterfaces  []string   `yaml:"multicast_interfaces" json:"multicast_interfaces"`
	Devices              []Device   `yaml:"devices" json:"devices"`
	MQTT                 MQTTConfig `yaml:"mqtt" json:"mqtt"`
}

// MQTTConfig holds optional MQTT settings for HA auto-discovery.
// If Broker is empty, MQTT publishing is disabled.
type MQTTConfig struct {
	Broker          string `yaml:"broker" json:"broker"`
	Username        string `yaml:"username" json:"username"`
	Password        string `yaml:"password" json:"password"`
	TopicPrefix     string `yaml:"topic_prefix" json:"topic_prefix"`
	DiscoveryPrefix string `yaml:"discovery_prefix" json:"discovery_prefix"`
}

// MQTTEnabled returns true if MQTT publishing is configured.
func (c *Config) MQTTEnabled() bool {
	return c.MQTT.Broker != ""
}

// Device is a single ECHONET device to poll.
type Device struct {
	Name           string            `yaml:"name" json:"name"`
	IP             string            `yaml:"ip" json:"ip"`
	Class          string            `yaml:"class" json:"class"`
	Manufacturer   string            `yaml:"manufacturer,omitempty" json:"manufacturer,omitempty"`
	Model          string            `yaml:"model,omitempty" json:"model,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	ScrapeInterval string            `yaml:"scrape_interval,omitempty" json:"scrape_interval,omitempty"`
}

// fileConfig is the on-disk shape for the main config file.
type fileConfig struct {
	ListenAddr           string     `yaml:"listen_addr"`
	ScrapeTimeoutSec     int        `yaml:"scrape_timeout_sec"`
	StrictSourcePort3610 *bool      `yaml:"strict_source_port_3610"`
	MetricsEnabled       *bool      `yaml:"metrics_enabled"`
	NotificationsEnabled *bool      `yaml:"notifications_enabled"`
	ForcePolling         *bool      `yaml:"force_polling"`
	MulticastInterfaces  []string   `yaml:"multicast_interfaces"`
	DevicesPath          string     `yaml:"devices_path"`
	SpecsDir             string     `yaml:"specs_dir"`
	Devices              []Device   `yaml:"devices"`
	MQTT                 MQTTConfig `yaml:"mqtt"`
}

// Load reads configuration: optional etc config file, then env overrides.
// If ECHONET_CONFIG is set, that path is used; else etc/config.yaml relative to cwd.
// Devices can come from config file, from devices_path (YAML/JSON), or from ECHONET_DEVICES JSON env.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:           ":9191",
		ScrapeTimeoutSec:     15,
		StrictSourcePort3610: true,
		NotificationsEnabled: true,
	}

	configPath := os.Getenv("ECHONET_CONFIG")
	if configPath == "" {
		configPath = "etc/config.yaml"
	}
	cfg.ConfigPath = configPath

	if err := loadFromFile(cfg); err != nil {
		return nil, err
	}

	// MQTT defaults
	if cfg.MQTT.TopicPrefix == "" {
		cfg.MQTT.TopicPrefix = "echonetgo"
	}
	if cfg.MQTT.DiscoveryPrefix == "" {
		cfg.MQTT.DiscoveryPrefix = "homeassistant"
	}

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	if err := loadAdditionalDevices(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadFromFile(cfg *Config) error {
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config %s: %w", cfg.ConfigPath, err)
	}
	if err == nil {
		var fc fileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return fmt.Errorf("parse %s: %w", cfg.ConfigPath, err)
		}
		if fc.ListenAddr != "" {
			cfg.ListenAddr = fc.ListenAddr
		}
		if fc.ScrapeTimeoutSec > 0 {
			cfg.ScrapeTimeoutSec = fc.ScrapeTimeoutSec
		}
		if fc.StrictSourcePort3610 != nil {
			cfg.StrictSourcePort3610 = *fc.StrictSourcePort3610
		}
		if fc.MetricsEnabled != nil {
			cfg.MetricsEnabled = *fc.MetricsEnabled
		}
		if fc.NotificationsEnabled != nil {
			cfg.NotificationsEnabled = *fc.NotificationsEnabled
		}
		if fc.ForcePolling != nil {
			cfg.ForcePolling = *fc.ForcePolling
		}
		if len(fc.MulticastInterfaces) > 0 {
			cfg.MulticastInterfaces = fc.MulticastInterfaces
		}
		if fc.DevicesPath != "" {
			cfg.DevicesPath = fc.DevicesPath
		}
		if fc.SpecsDir != "" {
			cfg.SpecsDir = fc.SpecsDir
		}
		if len(fc.Devices) > 0 {
			cfg.Devices = fc.Devices
		}
		cfg.MQTT = fc.MQTT
	}
	return nil
}

func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv("ECHONET_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("ECHONET_SCRAPE_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ScrapeTimeoutSec = n
		}
	}
	if v := os.Getenv("ECHONET_STRICT_SOURCE_PORT_3610"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("ECHONET_STRICT_SOURCE_PORT_3610: %w", err)
		}
		cfg.StrictSourcePort3610 = b
	}
	if v := os.Getenv("ECHONET_METRICS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("ECHONET_METRICS_ENABLED: %w", err)
		}
		cfg.MetricsEnabled = b
	}
	if v := os.Getenv("ECHONET_NOTIFICATIONS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("ECHONET_NOTIFICATIONS_ENABLED: %w", err)
		}
		cfg.NotificationsEnabled = b
	}
	if v := os.Getenv("ECHONET_FORCE_POLLING"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("ECHONET_FORCE_POLLING: %w", err)
		}
		cfg.ForcePolling = b
	}
	if v := os.Getenv("ECHONET_MULTICAST_INTERFACES"); v != "" {
		cfg.MulticastInterfaces = strings.Split(v, ",")
	}
	if v := os.Getenv("ECHONET_DEVICES_PATH"); v != "" {
		cfg.DevicesPath = v
	}
	if v := os.Getenv("ECHONET_SPECS_DIR"); v != "" {
		cfg.SpecsDir = v
	}
	if v := os.Getenv("MQTT_BROKER"); v != "" {
		cfg.MQTT.Broker = v
	}
	if v := os.Getenv("MQTT_USER"); v != "" {
		cfg.MQTT.Username = v
	}
	if v := os.Getenv("MQTT_PASS"); v != "" {
		cfg.MQTT.Password = v
	}
	if v := os.Getenv("MQTT_TOPIC_PREFIX"); v != "" {
		cfg.MQTT.TopicPrefix = v
	}
	if v := os.Getenv("MQTT_DISCOVERY_PREFIX"); v != "" {
		cfg.MQTT.DiscoveryPrefix = v
	}
	return nil
}

func loadAdditionalDevices(cfg *Config) error {
	// Devices from file if no devices in main config
	if len(cfg.Devices) == 0 && cfg.DevicesPath != "" {
		devices, err := loadDevicesFile(cfg.DevicesPath)
		if err != nil {
			return err
		}
		cfg.Devices = devices
	}

	// ECHONET_DEVICES JSON env overrides / supplies devices
	if devicesJSON := os.Getenv("ECHONET_DEVICES"); devicesJSON != "" {
		var devices []Device
		if err := json.Unmarshal([]byte(devicesJSON), &devices); err != nil {
			return fmt.Errorf("ECHONET_DEVICES JSON: %w", err)
		}
		cfg.Devices = devices
	}
	return nil
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	for i, d := range c.Devices {
		if d.Name == "" {
			return fmt.Errorf("device[%d]: name is required", i)
		}
		if d.IP == "" {
			return fmt.Errorf("device %q: ip is required", d.Name)
		}
		if d.Class == "" {
			return fmt.Errorf("device %q: class is required", d.Name)
		}
		sanitized := sanitizeDeviceName(d.Name)
		if sanitized != d.Name {
			c.Devices[i].Name = sanitized
		}
	}
	return nil
}

func loadDevicesFile(path string) ([]Device, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read devices file %s: %w", path, err)
	}
	ext := filepath.Ext(path)
	var out struct {
		Devices []Device `yaml:"devices" json:"devices"`
	}
	if ext == ".json" {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parse devices JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("parse devices YAML: %w", err)
		}
	}
	return out.Devices, nil
}

// sanitizeDeviceName strips characters that are unsafe in log output, MQTT
// topics, or Prometheus labels. Keeps all printable Unicode (including CJK,
// accented Latin, etc.) but removes:
//   - C0/C1 control characters (U+0000–U+001F, U+007F–U+009F) — prevents
//     log injection and ANSI escape sequences
//   - MQTT topic separator '/' — would break topic structure
//   - MQTT wildcards '+' and '#' — could cause unexpected subscriptions
func sanitizeDeviceName(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		switch r {
		case '/', '+', '#':
			return '_'
		}
		return r
	}, name)
}

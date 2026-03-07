package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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
	Labels         map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	ScrapeInterval string            `yaml:"scrape_interval,omitempty" json:"scrape_interval,omitempty"`
}

// fileConfig is the on-disk shape for the main config file.
type fileConfig struct {
	ListenAddr           string     `yaml:"listen_addr"`
	ScrapeTimeoutSec     int        `yaml:"scrape_timeout_sec"`
	StrictSourcePort3610 *bool      `yaml:"strict_source_port_3610"`
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
	}

	configPath := os.Getenv("ECHONET_CONFIG")
	if configPath == "" {
		configPath = "etc/config.yaml"
	}
	cfg.ConfigPath = configPath

	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config %s: %w", configPath, err)
	}
	if err == nil {
		var fc fileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", configPath, err)
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

	// MQTT defaults
	if cfg.MQTT.TopicPrefix == "" {
		cfg.MQTT.TopicPrefix = "echonetgo"
	}
	if cfg.MQTT.DiscoveryPrefix == "" {
		cfg.MQTT.DiscoveryPrefix = "homeassistant"
	}

	// Environment overrides
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
			return nil, fmt.Errorf("ECHONET_STRICT_SOURCE_PORT_3610: %w", err)
		}
		cfg.StrictSourcePort3610 = b
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

	// Devices from file if no devices in main config
	if len(cfg.Devices) == 0 && cfg.DevicesPath != "" {
		devices, err := loadDevicesFile(cfg.DevicesPath)
		if err != nil {
			return nil, err
		}
		cfg.Devices = devices
	}

	// ECHONET_DEVICES JSON env overrides / supplies devices
	if devicesJSON := os.Getenv("ECHONET_DEVICES"); devicesJSON != "" {
		var devices []Device
		if err := json.Unmarshal([]byte(devicesJSON), &devices); err != nil {
			return nil, fmt.Errorf("ECHONET_DEVICES JSON: %w", err)
		}
		cfg.Devices = devices
	}

	for i, d := range cfg.Devices {
		if d.Name == "" {
			return nil, fmt.Errorf("device[%d]: name is required", i)
		}
		if d.IP == "" {
			return nil, fmt.Errorf("device %q: ip is required", d.Name)
		}
		if d.Class == "" {
			return nil, fmt.Errorf("device %q: class is required", d.Name)
		}
	}

	return cfg, nil
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

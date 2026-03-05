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
	ListenAddr       string   `yaml:"listen_addr" json:"listen_addr"`
	ScrapeTimeoutSec int      `yaml:"scrape_timeout_sec" json:"scrape_timeout_sec"`
	ConfigPath       string   `yaml:"-" json:"-"` // path to etc config file
	DevicesPath      string   `yaml:"devices_path" json:"devices_path"`
	SpecsDir         string   `yaml:"specs_dir" json:"specs_dir"`
	Devices          []Device `yaml:"devices" json:"devices"`
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
	ListenAddr       string   `yaml:"listen_addr"`
	ScrapeTimeoutSec int      `yaml:"scrape_timeout_sec"`
	DevicesPath      string   `yaml:"devices_path"`
	SpecsDir         string   `yaml:"specs_dir"`
	Devices          []Device `yaml:"devices"`
}

// Load reads configuration: optional etc config file, then env overrides.
// If ECHONET_CONFIG is set, that path is used; else etc/config.yaml relative to cwd.
// Devices can come from config file, from devices_path (YAML/JSON), or from ECHONET_DEVICES JSON env.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:       ":9191",
		ScrapeTimeoutSec: 15,
	}

	configPath := os.Getenv("ECHONET_CONFIG")
	if configPath == "" {
		configPath = "etc/config.yaml"
	}
	cfg.ConfigPath = configPath

	data, err := os.ReadFile(configPath)
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
		if fc.DevicesPath != "" {
			cfg.DevicesPath = fc.DevicesPath
		}
		if fc.SpecsDir != "" {
			cfg.SpecsDir = fc.SpecsDir
		}
		if len(fc.Devices) > 0 {
			cfg.Devices = fc.Devices
		}
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
	if v := os.Getenv("ECHONET_DEVICES_PATH"); v != "" {
		cfg.DevicesPath = v
	}
	if v := os.Getenv("ECHONET_SPECS_DIR"); v != "" {
		cfg.SpecsDir = v
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

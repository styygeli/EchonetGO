package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ECHONET_LISTEN_ADDR", "ECHONET_SCRAPE_TIMEOUT_SEC",
		"ECHONET_STRICT_SOURCE_PORT_3610", "ECHONET_DEVICES_PATH",
		"ECHONET_SPECS_DIR", "ECHONET_DEVICES",
		"ECHONET_METRICS_ENABLED", "ECHONET_NOTIFICATIONS_ENABLED",
		"ECHONET_FORCE_POLLING", "ECHONET_MULTICAST_INTERFACES",
		"MQTT_BROKER", "MQTT_USER", "MQTT_PASS", "MQTT_USERNAME", "MQTT_PASSWORD",
		"MQTT_TOPIC_PREFIX", "MQTT_DISCOVERY_PREFIX", "MQTT_HOST", "MQTT_SERVER", "MQTT_PORT",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_DEVICES_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != ":9191" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9191")
	}
	if cfg.ScrapeTimeoutSec != 15 {
		t.Fatalf("ScrapeTimeoutSec = %d, want 15", cfg.ScrapeTimeoutSec)
	}
	if !cfg.StrictSourcePort3610 {
		t.Fatal("StrictSourcePort3610 should default to true")
	}
	if len(cfg.Devices) != 0 {
		t.Fatalf("Devices should be empty, got %d", len(cfg.Devices))
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_DEVICES_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_LISTEN_ADDR", "0.0.0.0:7777")
	t.Setenv("ECHONET_SCRAPE_TIMEOUT_SEC", "30")
	t.Setenv("ECHONET_STRICT_SOURCE_PORT_3610", "false")
	t.Setenv("ECHONET_METRICS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:7777" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:7777")
	}
	if cfg.ScrapeTimeoutSec != 30 {
		t.Fatalf("ScrapeTimeoutSec = %d, want 30", cfg.ScrapeTimeoutSec)
	}
	if cfg.StrictSourcePort3610 {
		t.Fatal("StrictSourcePort3610 should be false from env")
	}
	if cfg.MetricsEnabled {
		t.Fatal("MetricsEnabled should be false from env")
	}
}

func TestLoad_DevicesJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_DEVICES_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_DEVICES", `[{"name":"dev1","ip":"10.0.0.1","class":"storage_battery"}]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].Name != "dev1" {
		t.Fatalf("unexpected devices: %+v", cfg.Devices)
	}
}

func TestLoad_DevicesFromFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	devFile := filepath.Join(dir, "devices.yaml")
	content := `
devices:
  - name: file_dev
    ip: 10.0.0.2
    class: home_solar
`
	if err := os.WriteFile(devFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_DEVICES_PATH", devFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].Name != "file_dev" {
		t.Fatalf("unexpected devices: %+v", cfg.Devices)
	}
}

func TestLoad_PermissionErrorReturnsError(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	devFile := filepath.Join(dir, "devices.yaml")
	if err := os.WriteFile(devFile, []byte("devices:\n"), 0200); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_DEVICES_PATH", devFile)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unreadable devices file")
	}
}

func TestLoad_MQTTDefaultsWhenBrokerSet(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_DEVICES_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("MQTT_BROKER", "tcp://localhost:1883")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MQTTEnabled() {
		t.Fatal("MQTTEnabled() should be true")
	}
	if cfg.MQTT.TopicPrefix != "echonetgo" {
		t.Fatalf("TopicPrefix = %q, want echonetgo", cfg.MQTT.TopicPrefix)
	}
	if cfg.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("DiscoveryPrefix = %q, want homeassistant", cfg.MQTT.DiscoveryPrefix)
	}
}

func TestLoad_MQTTEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_DEVICES_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("MQTT_HOST", "broker.local")
	t.Setenv("MQTT_PORT", "1884")
	t.Setenv("MQTT_USERNAME", "testuser")
	t.Setenv("MQTT_PASSWORD", "testpass")
	t.Setenv("MQTT_TOPIC_PREFIX", "custom/topic")
	t.Setenv("MQTT_DISCOVERY_PREFIX", "custom/discovery")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MQTT.Broker != "tcp://broker.local:1884" {
		t.Fatalf("Broker = %q", cfg.MQTT.Broker)
	}
	if cfg.MQTT.Username != "testuser" {
		t.Fatalf("Username = %q", cfg.MQTT.Username)
	}
	if cfg.MQTT.Password != "testpass" {
		t.Fatalf("Password = %q", cfg.MQTT.Password)
	}
	if cfg.MQTT.TopicPrefix != "custom/topic" {
		t.Fatalf("TopicPrefix = %q", cfg.MQTT.TopicPrefix)
	}
}

func TestLoad_SanitizesDeviceName(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_DEVICES_PATH", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_DEVICES", `[{"name":"my+device/name#\u0000","ip":"10.0.0.1","class":"home_ac"}]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(cfg.Devices))
	}
	want := "my_device_name_"
	if cfg.Devices[0].Name != want {
		t.Errorf("Sanitize() changed name to %q, want %q", cfg.Devices[0].Name, want)
	}
}

func TestMQTTEnabled(t *testing.T) {
	cfg := &Config{}
	if cfg.MQTTEnabled() {
		t.Fatal("MQTTEnabled() should be false initially")
	}
	cfg.MQTT.Broker = "tcp://test:1883"
	if !cfg.MQTTEnabled() {
		t.Fatal("MQTTEnabled() should be true")
	}
}

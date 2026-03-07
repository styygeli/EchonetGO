package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ECHONET_CONFIG", "ECHONET_LISTEN_ADDR", "ECHONET_SCRAPE_TIMEOUT_SEC",
		"ECHONET_STRICT_SOURCE_PORT_3610", "ECHONET_DEVICES_PATH",
		"ECHONET_SPECS_DIR", "ECHONET_DEVICES",
		"MQTT_BROKER", "MQTT_USER", "MQTT_PASS", "MQTT_TOPIC_PREFIX", "MQTT_DISCOVERY_PREFIX",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))

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

func TestLoad_FromYAML(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := `
listen_addr: "0.0.0.0:8080"
scrape_timeout_sec: 10
strict_source_port_3610: false
devices:
  - name: test_dev
    ip: 192.168.1.1
    class: home_ac
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_CONFIG", cfgFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:8080" {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:8080")
	}
	if cfg.ScrapeTimeoutSec != 10 {
		t.Fatalf("ScrapeTimeoutSec = %d, want 10", cfg.ScrapeTimeoutSec)
	}
	if cfg.StrictSourcePort3610 {
		t.Fatal("StrictSourcePort3610 should be false")
	}
	if len(cfg.Devices) != 1 {
		t.Fatalf("len(Devices) = %d, want 1", len(cfg.Devices))
	}
	if cfg.Devices[0].Name != "test_dev" {
		t.Fatalf("device name = %q, want %q", cfg.Devices[0].Name, "test_dev")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_LISTEN_ADDR", "0.0.0.0:7777")
	t.Setenv("ECHONET_SCRAPE_TIMEOUT_SEC", "30")
	t.Setenv("ECHONET_STRICT_SOURCE_PORT_3610", "false")

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
}

func TestLoad_DevicesJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
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
	cfgFile := filepath.Join(dir, "config.yaml")
	cfgContent := `devices_path: "` + devFile + `"`
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_CONFIG", cfgFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].Name != "file_dev" {
		t.Fatalf("unexpected devices: %+v", cfg.Devices)
	}
}

func TestLoad_BrokenYAMLReturnsError(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("{{{{"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_CONFIG", cfgFile)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for broken YAML")
	}
}

func TestLoad_PermissionErrorReturnsError(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("listen_addr: ':9191'"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfgFile, 0000); err != nil {
		t.Skip("cannot change file permissions on this OS")
	}
	t.Cleanup(func() { _ = os.Chmod(cfgFile, 0644) })
	t.Setenv("ECHONET_CONFIG", cfgFile)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unreadable config file")
	}
}

func TestLoad_RejectsDeviceMissingName(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_DEVICES", `[{"name":"","ip":"10.0.0.1","class":"home_ac"}]`)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty device name")
	}
}

func TestLoad_RejectsDeviceMissingIP(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_DEVICES", `[{"name":"dev1","ip":"","class":"home_ac"}]`)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty device ip")
	}
}

func TestLoad_RejectsDeviceMissingClass(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("ECHONET_DEVICES", `[{"name":"dev1","ip":"10.0.0.1","class":""}]`)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty device class")
	}
}

func TestLoad_MQTTFromYAML(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := `
listen_addr: ":9191"
mqtt:
  broker: "tcp://broker.local:1883"
  username: "user"
  password: "secret"
  topic_prefix: "echonet"
  discovery_prefix: "homeassistant"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_CONFIG", cfgFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MQTTEnabled() {
		t.Fatal("MQTTEnabled() should be true")
	}
	if cfg.MQTT.Broker != "tcp://broker.local:1883" {
		t.Fatalf("MQTT.Broker = %q, want tcp://broker.local:1883", cfg.MQTT.Broker)
	}
	if cfg.MQTT.Username != "user" || cfg.MQTT.Password != "secret" {
		t.Fatalf("MQTT credentials: got %q / %q", cfg.MQTT.Username, cfg.MQTT.Password)
	}
	if cfg.MQTT.TopicPrefix != "echonet" || cfg.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("MQTT prefixes: topic=%q discovery=%q", cfg.MQTT.TopicPrefix, cfg.MQTT.DiscoveryPrefix)
	}
}

func TestLoad_MQTTDefaultsWhenBrokerSet(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := `
listen_addr: ":9191"
mqtt:
  broker: "tcp://localhost:1883"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_CONFIG", cfgFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MQTT.TopicPrefix != "echonetgo" {
		t.Fatalf("TopicPrefix default = %q, want echonetgo", cfg.MQTT.TopicPrefix)
	}
	if cfg.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Fatalf("DiscoveryPrefix default = %q, want homeassistant", cfg.MQTT.DiscoveryPrefix)
	}
}

func TestLoad_MQTTEnvOverrides(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := `
listen_addr: ":9191"
mqtt:
  broker: "tcp://file-broker:1883"
  username: "fileuser"
  topic_prefix: "fileprefix"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECHONET_CONFIG", cfgFile)
	t.Setenv("MQTT_BROKER", "tcp://env-broker:1883")
	t.Setenv("MQTT_USER", "envuser")
	t.Setenv("MQTT_PASS", "envpass")
	t.Setenv("MQTT_TOPIC_PREFIX", "envprefix")
	t.Setenv("MQTT_DISCOVERY_PREFIX", "envdiscovery")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MQTT.Broker != "tcp://env-broker:1883" {
		t.Fatalf("Broker = %q, want env override", cfg.MQTT.Broker)
	}
	if cfg.MQTT.Username != "envuser" || cfg.MQTT.Password != "envpass" {
		t.Fatalf("credentials from env: got %q / %q", cfg.MQTT.Username, cfg.MQTT.Password)
	}
	if cfg.MQTT.TopicPrefix != "envprefix" || cfg.MQTT.DiscoveryPrefix != "envdiscovery" {
		t.Fatalf("prefixes: topic=%q discovery=%q", cfg.MQTT.TopicPrefix, cfg.MQTT.DiscoveryPrefix)
	}
}

func TestMQTTEnabled(t *testing.T) {
	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MQTTEnabled() {
		t.Fatal("MQTTEnabled() should be false when broker empty")
	}

	clearEnv(t)
	t.Setenv("ECHONET_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	t.Setenv("MQTT_BROKER", "tcp://localhost:1883")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MQTTEnabled() {
		t.Fatal("MQTTEnabled() should be true when broker set via env")
	}
}

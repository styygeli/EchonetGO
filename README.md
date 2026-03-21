# EchonetGO

A Go service that connects ECHONET Lite smart home devices to Home Assistant via MQTT. It polls devices over UDP, caches their state locally, and uses HA's auto-discovery payload to expose them as sensors, climate, switch, select, and number entities. 

Device structures and metrics are defined entirely in YAML so you can add or modify device support without writing any Go code.

Originally inspired by [echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant), this is a from-scratch Go reimplementation designed to run as a reliable, standalone service or Home Assistant add-on.

## Features

- **ECHONET Lite polling:** UDP-based Get/Get_Res with GETMAP filtering, adaptive chunking, and per-host concurrency.
- **Real-time notifications:** Receives device-initiated INF/INFC property updates (ESV 0x73/0x74) via multicast, reflecting state changes instantly without waiting for the next poll. INFC frames are automatically acknowledged.
- **Smart poll optimization:** Reads each device's STATMAP (EPC 0x9D) at init to learn which properties the device pushes. Polling is automatically skipped for recently-pushed EPCs, reducing redundant UDP traffic while maintaining a verification fallback.
- **Multi-interface multicast:** Joins the ECHONET Lite multicast group (224.0.23.0) on all suitable IPv4 interfaces by default, or on a configured subset. Supports multi-VLAN setups.
- **Bidirectional control:** Support for SET commands across climate, switches, selects, and numbers. Writable properties are auto-detected (EPC 0x9E).
- **Protocol-level validation:** SET commands are explicitly validated against device responses (Set_Res vs SetC_SNA). If a device rejects a command, EchonetGO detects it immediately and forces a rapid UI sync in Home Assistant to revert the optimistic state change.
- **MQTT auto-discovery:** Publishes fully configured Home Assistant entities with `device_class`, `state_class`, and enum mapping.
- **Energy Dashboard support:** Includes required HA metadata for power, energy, water, and gas sensors.
- **Vendor-specific specs:** Automatically detects and loads manufacturer-specific formats (e.g., Mitsubishi MAC-900IF).
- **YAML-driven:** All device classes, EPCs, mapping rules, and HA metadata are defined in `etc/specs/*.yaml`.
- **Prometheus metrics:** Opt-in `/metrics` endpoint exports device, Go runtime, and process metrics without blocking device I/O.
- **Home Assistant add-on:** Ready-to-install HA add-on with automatic MQTT broker credential injection.

## Attribution

Thanks to the following projects for the inspiration and references:

- **[echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant)** & **[pychonet](https://github.com/scottyphillips/pychonet)** — The original HA integration and Python library. Provided the entity mapping patterns and protocol behaviors.
- **[echonet-exporter](https://github.com/styygeli/echonet-exporter)** — Provided the foundation for detached Prometheus scraping, UDP chunking, and the YAML spec layout.
- **ECHONET Lite** — The official [ECHONET Consortium](https://echonet.jp/) standard.

## Quick start

```bash
go build -o echonetgo ./cmd/echonetgo
./echonetgo
```

By default the service reads `etc/config.yaml` (or `ECHONET_CONFIG`), loads device specs from `etc/specs/`, and serves HTTP on `:9191`. Endpoints:

- `GET /` — brief info
- `GET /health` — liveness
- `GET /metrics` — Prometheus/VictoriaMetrics text format (requires `metrics_enabled: true`)

## File layout

| Path | Purpose |
|------|---------|
| `cmd/echonetgo/main.go` | Entrypoint: config load, specs load, poller start, MQTT publisher init, HTTP server with graceful shutdown |
| `internal/config/` | Config from `etc/config.yaml` and env (`ECHONET_CONFIG`, `ECHONET_DEVICES`, `MQTT_BROKER`, etc.) |
| `internal/specs/` | Device class specs (EOJ, metrics with EPC/name/help/interval/HA metadata); loaders for `etc/specs/*.yaml` |
| `internal/model/` | ECHONET Get_Res property types (EPC, PDC, EDT) |
| `internal/echonet/` | ECHONET Lite client split across focused files: `client.go` (high-level API), `transport.go` (UDP connection pool, port fallback, per-host locking, multicast join), `protocol.go` (frame parsing/building), `notification.go` (INF/INFC handler), `encoder.go` (EDT value encoding/decoding), `manufacturers.go` (manufacturer code lookup) |
| `internal/poller/` | Cache and scheduler: per-device/per-interval scrapers, parallel init per host IP, startup stagger, update callbacks |
| `internal/mqtt/` | `publisher.go` (MQTT connection, state publishing), `discovery.go` (HA auto-discovery for sensor/climate/switch/select/number entities), `commander.go` (subscribes to command topics, routes SET requests to the ECHONET client) |
| `internal/metrics/` | Prometheus collector: reads from poller cache, emits device metrics, enum one-hot gauges, scrape stats, device info |
| `internal/api/` | HTTP mux: `/health`, `/metrics`, `/` |
| `internal/logging/` | Leveled logger (`ECHONET_LOG_LEVEL`) |
| `etc/config.example.yaml` | Example config (listen_addr, mqtt, devices) |
| `etc/devices.example.yaml` | Example device list — copy to `etc/devices.yaml` and set your IPs |
| `etc/specs/*.yaml` | One file per device class (e.g. `home_ac`, `storage_battery`, `power_dist_board`); vendor-specific overrides named `{class}_{manufacturer_hex}.yaml` |
| `addon_echonetgo/` | Home Assistant add-on: config.yaml, Dockerfile, run.sh, docs |
| `tools/` | Standalone Python scripts: `echonet_probe.py` (active device testing), `echonet_listen.py` (passive multicast notification listener) |

## Architecture

- **Configuration:** Reads from `etc/config.yaml` and environment variables. When running as an HA add-on, Supervisor injects MQTT credentials automatically.
- **Specs:** Device definitions live in `etc/specs/`. Each YAML specifies the EOJ, intervals, and metric details (EPC, size, scale, type, HA metadata). Vendor-specific specs like `home_ac_000006.yaml` load automatically if the manufacturer code matches (EPC 0x8A). Common EPCs (0x80-0x8F) are merged from a Super Class definition to reduce duplication.
- **ECHONET Client:** Handles UDP Get and SetC requests on port 3610 with connection pooling, GETMAP filtering, adaptive payload splitting, and ephemeral-port fallback. Joins the multicast group on all suitable interfaces for receiving device-initiated notifications.
- **Notification Handler:** Listens for unsolicited INF/INFC frames on the multicast channel, matches them to configured devices, updates the cache, and acknowledges INFC frames. Works alongside the poller for a hybrid poll+push model.
- **Poller:** Initializes devices, schedules scrapes based on interval, caches the state, and triggers MQTT updates. Initialization is parallelized per host IP. Skips polling for EPCs recently updated via push notifications.
- **MQTT:** The publisher sends HA auto-discovery configs and state updates. The commander parses HA payloads into ECHONET SetC requests. A bridge device ("EchonetGO") tracks overall online/offline status.
- **Metrics:** A detached Prometheus collector transforms the local cache state into the `/metrics` exposition format, preventing scrape delays from impacting device communication.
- **API:** Provides `/health` and `/metrics` endpoints.

## Tested devices

| Device | Manufacturer | Class | Notes |
|--------|-------------|-------|-------|
| EP Cube (battery + inverter) | Eternalplanet Energy (0x000131) | `storage_battery`, `home_solar` | Vendor-specific spec auto-detected. Does not report product code (0x8C). |
| MAC-900IF AC adapter | Mitsubishi Electric (0x000006) | `home_ac` | Vendor-specific spec `home_ac_000006.yaml`. Non-standard cumulative_operating_time format. |
| Ecocute wireless adapter | Daikin Industries (0x000008) | `electric_water_heater` | Does not report manufacturer/model EPCs; use config fallback fields. |
| Smart Cosmo MKN7350S1 | Panasonic (0x00000B) | `power_dist_board`, `home_solar`, `water_flow_meter`, `gas_meter` | Multi-EOJ device on single IP. Power in deciwatts (scale: 0.1). |

## Configuration

### Main config

- `listen_addr` — HTTP listen address (default `:9191`).
- `scrape_timeout_sec` — UDP timeout for ECHONET requests (default 15).
- `strict_source_port_3610` — Use local UDP source port `3610` only (default `true`). Set to `false` only if you explicitly need ephemeral-port fallback.
- `metrics_enabled` — Enable the `/metrics` Prometheus endpoint (default `false`).
- `notifications_enabled` — Listen for device-initiated INF/INFC multicast notifications (default `true`).
- `force_polling` — Always poll all EPCs even if recently pushed via notification (default `false`).
- `multicast_interfaces` — List of network interface names for multicast join (default: auto-detect all suitable IPv4 interfaces).
- `devices_path` — Optional path to a YAML/JSON file with a `devices` list.
- `specs_dir` — Directory of device class YAMLs (default `etc/specs`).
- `devices` — Inline list of devices (see below).

### Device config

Each device entry supports:

```yaml
devices:
  - name: epcube_battery       # unique name (used in MQTT topics and entity IDs)
    ip: 192.168.1.10           # device IP address
    class: storage_battery     # spec filename from etc/specs/
    manufacturer: "Eternalplanet"  # optional: fallback if device doesn't report EPC 0x8A
    model: "EP Cube"               # optional: fallback if device doesn't report EPC 0x8C
    scrape_interval: 2m            # optional: override default from spec
```

### MQTT config

```yaml
mqtt:
  broker: "tcp://core-mosquitto:1883"
  username: ""
  password: ""
  topic_prefix: "echonetgo"           # default
  discovery_prefix: "homeassistant"   # default
```

When running as an HA add-on with `mqtt: auto`, the broker credentials are injected automatically by the Supervisor — no manual MQTT config needed.

### Environment overrides

| Variable | Overrides |
|----------|-----------|
| `ECHONET_CONFIG` | Config file path |
| `ECHONET_LISTEN_ADDR` | `listen_addr` |
| `ECHONET_SCRAPE_TIMEOUT_SEC` | `scrape_timeout_sec` |
| `ECHONET_STRICT_SOURCE_PORT_3610` | `strict_source_port_3610` |
| `ECHONET_METRICS_ENABLED` | `metrics_enabled` |
| `ECHONET_NOTIFICATIONS_ENABLED` | `notifications_enabled` |
| `ECHONET_FORCE_POLLING` | `force_polling` |
| `ECHONET_MULTICAST_INTERFACES` | `multicast_interfaces` (comma-separated) |
| `ECHONET_DEVICES_PATH` | `devices_path` |
| `ECHONET_SPECS_DIR` | `specs_dir` |
| `ECHONET_DEVICES` | Devices as JSON array |
| `ECHONET_LOG_LEVEL` | Log level: debug/info/warn/error |
| `MQTT_BROKER` | `mqtt.broker` |
| `MQTT_USER` | `mqtt.username` |
| `MQTT_PASS` | `mqtt.password` |
| `MQTT_TOPIC_PREFIX` | `mqtt.topic_prefix` |
| `MQTT_DISCOVERY_PREFIX` | `mqtt.discovery_prefix` |

## Home Assistant add-on

The **addon_echonetgo/** directory contains a ready-to-use Home Assistant add-on.

### Installation

1. Add this repository as an add-on repository: **Settings → Add-ons → Add-on store → ⋮ → Repositories** — add `https://github.com/styygeli/EchonetGO`.
2. Install the **EchonetGO** add-on.
3. Create `/config/echonetgo/config.yaml` with your device list.
4. Start the add-on.

Devices appear automatically under **Settings → Devices** as MQTT devices, grouped under the "EchonetGO" bridge device.

The add-on runs with host networking, injects MQTT broker credentials from the Supervisor automatically, and maps `/config` read-write for your config files. See [addon_echonetgo/DOCS.md](addon_echonetgo/DOCS.md) for add-on-specific options and troubleshooting.

## Local network probe

When troubleshooting LAN/VLAN reachability or device responsiveness, run a direct probe from your local machine:

```bash
python3 tools/echonet_probe.py 192.168.3.86 --mode both --timeout 3
```

The probe sends standard ECHONET `Get` requests to the node profile and home AC EOJs, testing both source-port modes (ephemeral and 3610). Exits with non-zero status if all probes time out.

## Build and test

```bash
go build ./...
go test ./...
gofmt -w .
```

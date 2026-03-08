# EchonetGO

A Go service for ECHONET Lite smart home devices. Polls devices over UDP, caches state, and publishes sensors, climate, switch, select, and number entities to Home Assistant via MQTT auto-discovery. Supports bidirectional control (SET commands) and exposes a diagnostic HTTP API. Device and metric definitions live in editable YAML specs so non-Go contributors can add devices without changing code.

Originally inspired by [echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant) and [pychonet](https://github.com/scottyphillips/pychonet), reimplemented in Go for reliability and ease of deployment as a Home Assistant add-on.

## Features

- **ECHONET Lite polling** — Get/Get_Res over UDP with GETMAP filtering, adaptive split retries, and per-host concurrency.
- **Bidirectional control** — SET commands for climate (mode, temperature, fan speed), switches, selects, and numbers. Writable properties are auto-detected via the property map (EPC 0x9E).
- **MQTT auto-discovery** — Publishes HA-compatible sensor, climate, switch, select, and number entities with device_class, state_class, unit_of_measurement, and enum support. Entities appear automatically in Home Assistant.
- **Energy Dashboard ready** — Power, energy, water, and gas sensors include the correct HA metadata for the Energy Dashboard.
- **Vendor-specific specs** — Auto-selects device-specific YAML specs based on manufacturer code (e.g. Mitsubishi MAC-900IF), falling back to generic class specs.
- **YAML-driven** — All device classes, EPCs, metric names, scales, enums, climate mappings, and HA metadata are defined in `etc/specs/*.yaml`.
- **Prometheus / VictoriaMetrics metrics** — Opt-in `/metrics` endpoint in Prometheus text exposition format with detached scraping (reads from cache, never blocks on device I/O). Includes Go runtime and process metrics.
- **Home Assistant add-on** — Ships as a ready-to-install HA add-on with automatic MQTT broker credential injection.

## Attribution

This project builds on ideas and protocol behavior from the following projects and standards:

- **[echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant)** — Home Assistant integration for ECHONET Lite (MIT, Copyright © 2019 Scott Phillips). Device support, entity mapping, and integration patterns inspired this reimplementation.
- **[pychonet](https://github.com/scottyphillips/pychonet)** — Python ECHONET Lite library used by the integration (MIT/GPL, Copyright © 2018 Scott Phillips). Protocol semantics and EPC handling reference pychonet's implementation.
- **[echonet-exporter](https://github.com/styygeli/echonet-exporter)** — Prometheus/VictoriaMetrics exporter for ECHONET Lite. Detached scraping, GETMAP filtering, adaptive split retries, and YAML device/spec layout were adapted from this codebase.
- **ECHONET Lite** — [ECHONET Consortium](https://echonet.jp/) specification; implementation follows the published ECHONET Lite standard for UDP frames and property codes.

## Quick start

```bash
go build -o echonetgo ./cmd/echonetgo
./echonetgo
```

By default the service reads `etc/config.yaml` (or `ECHONET_CONFIG`), loads device specs from `etc/specs/`, and serves HTTP on `:9191`. Endpoints:

- `GET /` — brief info
- `GET /health` — liveness
- `GET /metrics` — Prometheus/VictoriaMetrics text format (requires `metrics_enabled: true`)
- `GET /state` — *(deprecated)* JSON snapshot of cached device metrics and identity

## File layout

| Path | Purpose |
|------|---------|
| `cmd/echonetgo/main.go` | Entrypoint: config load, specs load, poller start, MQTT publisher init, HTTP server with graceful shutdown |
| `internal/config/` | Config from `etc/config.yaml` and env (`ECHONET_CONFIG`, `ECHONET_DEVICES`, `MQTT_BROKER`, etc.) |
| `internal/specs/` | Device class specs (EOJ, metrics with EPC/name/help/interval/HA metadata); loaders for `etc/specs/*.yaml` |
| `internal/model/` | ECHONET Get_Res property types (EPC, PDC, EDT) |
| `internal/echonet/` | ECHONET Lite client split across focused files: `client.go` (high-level API), `transport.go` (UDP connection pool, port fallback, per-host locking), `protocol.go` (frame parsing/building), `encoder.go` (EDT value encoding/decoding), `manufacturers.go` (manufacturer code lookup) |
| `internal/poller/` | Cache and scheduler: per-device/per-interval scrapers, parallel init per host IP, startup stagger, update callbacks |
| `internal/mqtt/` | `publisher.go` (MQTT connection, state publishing), `discovery.go` (HA auto-discovery for sensor/climate/switch/select/number entities), `commander.go` (subscribes to command topics, routes SET requests to the ECHONET client) |
| `internal/metrics/` | Prometheus collector: reads from poller cache, emits device metrics, enum one-hot gauges, scrape stats, device info |
| `internal/api/` | HTTP mux: `/health`, `/metrics`, `/state`, `/` |
| `internal/logging/` | Leveled logger (`ECHONET_LOG_LEVEL`) |
| `etc/config.example.yaml` | Example config (listen_addr, mqtt, devices) |
| `etc/devices.example.yaml` | Example device list — copy to `etc/devices.yaml` and set your IPs |
| `etc/specs/*.yaml` | One file per device class (e.g. `home_ac`, `storage_battery`, `power_dist_board`); vendor-specific overrides named `{class}_{manufacturer_hex}.yaml` |
| `addon_echonetgo/` | Home Assistant add-on: config.yaml, Dockerfile, run.sh, docs |
| `tools/` | Standalone Python probe script for ECHONET Lite device testing |

## Architecture

- **Config** — Single source: optional `etc/config.yaml` plus env overrides. Devices can be in the config file, in a file at `devices_path`, or in `ECHONET_DEVICES` JSON. MQTT broker settings can be provided via config or env vars, and are automatically injected by the HA Supervisor when running as an add-on (`mqtt: auto`).
- **Specs** — One YAML per device class in `etc/specs/`; filename (without `.yaml`) is the class id. Each spec defines EOJ, default scrape interval, and metrics (EPC, name, help, size/scale/type, optional enum, per-metric scrape_interval, optional HA metadata: `ha_device_class`, `ha_state_class`, `ha_unit`). HA metadata is inferred from naming conventions when not explicitly set. **Vendor-specific specs** are named `{class}_{manufacturer_hex}.yaml` (e.g. `home_ac_000006.yaml`) and are auto-selected at runtime when the device reports that manufacturer code (EPC 0x8A); otherwise the generic class spec is used.
- **ECHONET client** — Sends Get and SetC requests over UDP via a shared `Transport` that owns port 3610, per-host serialization, and connection pooling. Supports GETMAP (0x9F/0x9E) for readable and writable properties, adaptive split on partial responses, and ephemeral-port fallback. IPv4 only.
- **Poller** — For each configured device: load spec, optionally filter metrics by GETMAP, group by scrape interval, stagger startup, run per-interval scrapers and a device-info refresher; all results merged into a single cache. Device initialization is parallelized per host IP. After each scrape, an update callback notifies the MQTT publisher.
- **MQTT** — The **publisher** sends HA auto-discovery config (retained) for sensors, climate, switch, select, and number entities, plus a bridge device ("EchonetGO"). State updates are published after each scrape with per-device availability. The **commander** subscribes to HA command topics and translates incoming payloads into ECHONET SetC requests with a 5-second timeout. On clean shutdown the bridge goes offline.
- **Metrics** — When `metrics_enabled: true`, a `prometheus.Collector` reads cached device state and emits Prometheus text format on `/metrics`. Uses detached scraping: `/metrics` requests are cheap cache reads, independent of ECHONET polling schedules. Metric naming follows `echonet_{subsystem}_{metric_name}` with labels `device`, `ip`, `class`. Go runtime (`go_*`) and process (`process_*`) metrics are included automatically.
- **API** — Read-only: health, metrics, and cached state JSON (`/state` is deprecated).

## Tested devices

| Device | Manufacturer | Class | Notes |
|--------|-------------|-------|-------|
| EP Cube (battery + inverter) | Eternalplanet Energy (0x000131) | `storage_battery`, `home_solar` | Vendor-specific spec auto-detected. Does not report product code (0x8C). |
| MAC-900IF AC adapter | Mitsubishi Electric (0x000006) | `home_ac` | Vendor-specific spec `home_ac_000006.yaml`. Non-standard cumulative_operating_time format. |
| Ecocute wireless adapter | Daikin Industries (0x000008) | `home_ac` | Does not report manufacturer/model EPCs; use config fallback fields. |
| Smart Cosmo MKN7350S1 | Panasonic (0x00000B) | `power_dist_board`, `home_solar`, `water_flow_meter`, `gas_meter` | Multi-EOJ device on single IP. Power in deciwatts (scale: 0.1). |

## Configuration

### Main config

- `listen_addr` — HTTP listen address (default `:9191`).
- `scrape_timeout_sec` — UDP timeout for ECHONET requests (default 15).
- `strict_source_port_3610` — Use local UDP source port `3610` only (default `true`). Set to `false` only if you explicitly need ephemeral-port fallback.
- `metrics_enabled` — Enable the `/metrics` Prometheus endpoint (default `false`).
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

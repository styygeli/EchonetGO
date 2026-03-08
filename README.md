# EchonetGO

A Go service for ECHONET Lite smart home devices. Polls devices over UDP, caches state, publishes sensors to Home Assistant via MQTT auto-discovery, and exposes a diagnostic HTTP API. Device and metric definitions live in editable YAML specs so non-Go contributors can add devices without changing code.

Originally inspired by [echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant) and [pychonet](https://github.com/scottyphillips/pychonet), reimplemented in Go for reliability and ease of deployment as a Home Assistant add-on.

## Features

- **ECHONET Lite polling** — Get/Get_Res over UDP with GETMAP filtering, adaptive split retries, and per-host concurrency.
- **MQTT auto-discovery** — Publishes HA-compatible sensor entities with device_class, state_class, unit_of_measurement, and enum support. Sensors appear automatically in Home Assistant.
- **Energy Dashboard ready** — Power, energy, water, and gas sensors include the correct HA metadata for the Energy Dashboard.
- **Vendor-specific specs** — Auto-selects device-specific YAML specs based on manufacturer code (e.g. Mitsubishi MAC-900IF), falling back to generic class specs.
- **YAML-driven** — All device classes, EPCs, metric names, scales, enums, and HA metadata are defined in `etc/specs/*.yaml`.
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
- `GET /state` — JSON snapshot of cached device metrics and identity (manufacturer, product code, UID), plus scrape diagnostics (`success`, `last_error`, `max_group_failures`)

## File layout

| Path | Purpose |
|------|---------|
| `cmd/echonetgo/main.go` | Entrypoint: config load, specs load, poller start, MQTT publisher init, HTTP server with graceful shutdown |
| `internal/config/` | Config from `etc/config.yaml` and env (`ECHONET_CONFIG`, `ECHONET_DEVICES`, `MQTT_BROKER`, etc.) |
| `internal/specs/` | Device class specs (EOJ, metrics with EPC/name/help/interval/HA metadata); loaders for `etc/specs/*.yaml` |
| `internal/model/` | ECHONET Get_Res property types (EPC, PDC, EDT) |
| `internal/echonet/` | UDP client: Get, Get_Res, GETMAP (0x9F), device info (0x83/0x8A/0x8C), manufacturer lookup, adaptive split retries |
| `internal/poller/` | Cache and scheduler: per-device/per-interval scrapers, parallel init per host IP, startup stagger, update callbacks |
| `internal/mqtt/` | MQTT publisher: HA auto-discovery (sensor config + device registration), state updates, availability, bridge device |
| `internal/api/` | HTTP mux: `/health`, `/state`, `/` |
| `internal/logging/` | Leveled logger (`ECHONET_LOG_LEVEL`) |
| `etc/config.example.yaml` | Example config (listen_addr, mqtt, devices) |
| `etc/devices.example.yaml` | Example device list — copy to `etc/devices.yaml` and set your IPs |
| `etc/specs/*.yaml` | One file per device class (e.g. `home_ac`, `storage_battery`, `power_dist_board`); vendor-specific overrides named `{class}_{manufacturer_hex}.yaml` |
| `addon_echonetgo/` | Home Assistant add-on: config.yaml, Dockerfile, run.sh, docs |
| `tools/` | Standalone Python probe script for ECHONET Lite device testing |

## Architecture

- **Config** — Single source: optional `etc/config.yaml` plus env overrides. Devices can be in the config file, in a file at `devices_path`, or in `ECHONET_DEVICES` JSON. MQTT broker settings can be provided via config or env vars, and are automatically injected by the HA Supervisor when running as an add-on (`mqtt: auto`).
- **Specs** — One YAML per device class in `etc/specs/`; filename (without `.yaml`) is the class id. Each spec defines EOJ, default scrape interval, and metrics (EPC, name, help, size/scale/type, optional enum, per-metric scrape_interval, optional HA metadata: `ha_device_class`, `ha_state_class`, `ha_unit`). HA metadata is inferred from naming conventions when not explicitly set. **Vendor-specific specs** are named `{class}_{manufacturer_hex}.yaml` (e.g. `home_ac_000006.yaml`) and are auto-selected at runtime when the device reports that manufacturer code (EPC 0x8A); otherwise the generic class spec is used.
- **ECHONET client** — Sends Get requests over UDP from a shared source port 3610, parses Get_Res; supports GETMAP (0x9F) for readable properties and adaptive split when the device returns partial OPC. IPv4 only.
- **Poller** — For each configured device: load spec, optionally filter metrics by GETMAP, group by scrape interval, stagger startup, run per-interval scrapers and a device-info refresher; all results merged into a single cache. Device initialization is parallelized per host IP. After each scrape, an update callback notifies the MQTT publisher.
- **MQTT** — Publishes HA auto-discovery config (retained) for each sensor entity and a bridge device ("EchonetGO"). Publishes state updates after each scrape and per-device availability. Metrics filtered by invalid sentinels are excluded from state payloads and show as "Unknown" in HA. On clean shutdown the bridge goes offline.
- **API** — Read-only: health and cached state JSON.

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

Devices appear automatically in Home Assistant under **Settings → Devices** as MQTT devices, grouped under the "EchonetGO" bridge device. Sensors are ready for dashboards and the Energy Dashboard.

### Add-on features

- **Host networking** for LAN UDP access to ECHONET Lite devices.
- **MQTT auto** — Broker credentials injected by the HA Supervisor (requires Mosquitto add-on).
- **Hassio API** — Queries the Supervisor for MQTT service credentials at startup.
- **Config mapping** — Read-write access to `/config` for your config and device files.

See [addon_echonetgo/DOCS.md](addon_echonetgo/DOCS.md) for detailed configuration.

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

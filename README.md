# EchonetGO

A Go service for ECHONET Lite devices: polling, caching, and a minimal HTTP API. It is designed to be extended toward feature parity with the Python [echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant) integration and [pychonet](https://github.com/scottyphillips/pychonet), while keeping device and metric definitions in editable YAML/JSON so non-Go contributors can add devices and specs.

## Attribution

This project builds on ideas and protocol behavior from the following projects and standards:

- **[echonetlite_homeassistant](https://github.com/scottyphillips/echonetlite_homeassistant)** — Home Assistant integration for ECHONET Lite (MIT, Copyright © 2019 Scott Phillips). Device support, entity mapping, and integration patterns inspired this reimplementation.
- **[pychonet](https://github.com/scottyphillips/pychonet)** — Python ECHONET Lite library used by the integration (MIT/GPL, Copyright © 2018 Scott Phillips). Protocol semantics and EPC handling reference pychonet’s implementation.
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
- `GET /state` — JSON snapshot of cached device metrics and identity (manufacturer, product code, UID), plus scrape diagnostics (`success`, `last_error`, `consecutive_failures`)

## File layout

| Path | Purpose |
|------|--------|
| `cmd/echonetgo/main.go` | Entrypoint: config load, specs load, poller start, HTTP server with graceful shutdown |
| `internal/config/` | Config from `etc/config.yaml` and env (`ECHONET_CONFIG`, `ECHONET_DEVICES`, etc.) |
| `internal/specs/` | Device class specs (EOJ, metrics with EPC/name/help/interval); loaders for `etc/specs/*.yaml` |
| `internal/model/` | ECHONET Get_Res property types (EPC, PDC, EDT) |
| `internal/echonet/` | UDP client: Get, Get_Res, GETMAP (0x9F), device info (0x83/0x8A/0x8C), adaptive split retries |
| `internal/poller/` | Cache and scheduler: per-device/per-interval scrapers, startup stagger, cache-first `/state` |
| `internal/api/` | HTTP mux: `/health`, `/state`, `/` |
| `internal/logging/` | Leveled logger (`ECHONET_LOG_LEVEL`) |
| `etc/config.example.yaml` | Example config (listen_addr, devices_path, specs_dir, devices) |
| `etc/devices.yaml` | Optional device list (name, ip, class, scrape_interval) |
| `etc/specs/*.yaml` | One file per device class (e.g. `home_ac`, `storage_battery`), including generated first-pass coverage for most classes from pychonet/echonetlite_homeassistant |

## Architecture

- **Config** — Single source: optional `etc/config.yaml` plus env overrides. Devices can be in the config file, in a file at `devices_path`, or in `ECHONET_DEVICES` JSON.
- **Specs** — One YAML per device class in `etc/specs/`; filename (without `.yaml`) is the class id. Each spec defines EOJ, default scrape interval, and metrics (EPC, name, help, size/scale/type, optional enum, per-metric scrape_interval). **Vendor-specific specs** are named `{class}_{manufacturer_hex}.yaml` (e.g. `home_ac_000006.yaml`) and are auto-selected at runtime when the device reports that manufacturer code (EPC 0x8A); otherwise the generic class spec is used. Known manufacturer codes: `000006` = Mitsubishi Electric, `000008` = Daikin, `000131` = Sungrow.
- **ECHONET client** — Sends Get requests over UDP, parses Get_Res; supports GETMAP (0x9F) for readable properties and adaptive split when the device returns partial OPC.
- **Poller** — For each configured device: load spec, optionally filter metrics by GETMAP, group by scrape interval, stagger startup, run per-interval scrapers and a device-info refresher; all results merged into a single cache. `/state` serves from cache regardless of scrape cadence.
- **API** — Read-only in this scaffold: health and cached state JSON.

## Migration notes: pychonet / echonetlite_homeassistant → EchonetGO

| Capability | pychonet / echonetlite_homeassistant | EchonetGO (current / planned) |
|------------|--------------------------------------|-------------------------------|
| ECHONET Lite Get / Get_Res over UDP | pychonet protocol layer | `internal/echonet`: `SendGet`, `GetProps`, `ParseGetRes` |
| Device and object definitions | Python classes / discovery | `etc/specs/*.yaml` + `internal/specs`; class id = filename |
| Readable property discovery | GETMAP (0x9F) in places | `internal/echonet`: `GetReadablePropertyMap`; poller filters metrics by it |
| Polling / scraping | Integration-specific loops | `internal/poller`: per-device, per-interval workers, startup stagger, cache |
| Partial response / retry | Varies | `internal/echonet`: adaptive split in `getPropsAdaptive` |
| Cached state exposure | HA entity state | `GET /state` returns JSON of all devices and last-good metrics + device info |
| setMessage / SetI / SetC | pychonet set APIs | **Planned**: control/write API in a new module; same client transport |
| Discovery (multicast, etc.) | pychonet discovery | **Planned**: discovery flow and quirk overlays |
| Home Assistant add-on | HA add-on repo | **Planned**: add-on packaging once core runtime is stable |

Contributors can add device classes and metrics by editing YAML under `etc/specs/` and listing devices in config or `etc/devices.yaml` without changing Go code. The shipped `etc/specs/` set is now committed as permanent baseline data (including broad class coverage imported from pychonet at migration time), so runtime/build does not depend on external projects.

## Configuration

- `listen_addr` — HTTP listen address (default `:9191`).
- `scrape_timeout_sec` — UDP timeout for ECHONET requests (default 15).
- `strict_source_port_3610` — Use local UDP source port `3610` only (default `true`). Set to `false` only if you explicitly need ephemeral-port fallback.
- `devices_path` — Optional path to a YAML/JSON file with a `devices` list.
- `specs_dir` — Directory of device class YAMLs (default `etc/specs`).
- `devices` — Inline list of `{ name, ip, class, scrape_interval? }`.

Environment overrides: `ECHONET_CONFIG`, `ECHONET_LISTEN_ADDR`, `ECHONET_SCRAPE_TIMEOUT_SEC`, `ECHONET_STRICT_SOURCE_PORT_3610`, `ECHONET_DEVICES_PATH`, `ECHONET_SPECS_DIR`, `ECHONET_DEVICES` (JSON array), `ECHONET_LOG_LEVEL` (debug/info/warn/error).

## Home Assistant add-on

The **addon_echonetgo/** directory contains the files needed to run EchonetGO as a Home Assistant add-on:

- **config.yaml** — Add-on metadata, options (config_path, devices_path, log_level), and host networking for LAN UDP access.
- **Dockerfile** — Multi-stage build: clone the repository, compile the Go binary, then copy it and built-in specs into the add-on image. Local build example: `docker build -f addon_echonetgo/Dockerfile addon_echonetgo`
- **rootfs/run.sh** — Entrypoint that reads add-on options from `/data/options.json`, sets `ECHONET_*` env vars, and runs the binary.
- **README.md**, **DOCS.md**, **CHANGELOG.md** — Add-on docs and changelog.

To use it as a custom add-on repository in Home Assistant: add this repo URL in **Settings → Add-ons → Add-on store → Repositories**, then install the **EchonetGO** add-on. Create your config and device list under `/config/echonetgo/` (or the path you set in the add-on options) and set **config_path** accordingly. See **addon_echonetgo/README.md** and **addon_echonetgo/DOCS.md** for details.

## Local network probe

When troubleshooting LAN/VLAN reachability or device responsiveness, run a direct probe from your local machine:

```bash
python3 tools/echonet_probe.py 192.168.3.86 --mode both --timeout 3
```

The probe sends standard ECHONET `Get` requests to:

- Node profile (`0x0EF001`): EPC `0xD6`, `0x9F`, and identity (`0x83`, `0x8A`, `0x8C`)
- Home AC (`0x0130xx`): EPC `0x80` and `0x9F`

It tests both source-port modes (`ephemeral` and `3610`) and exits with non-zero status if all probes time out, which is useful for quick before/after checks while adjusting network, routing, or ACL/firewall settings.

## Build and test

```bash
go build ./...
go test ./...
gofmt -w .
```

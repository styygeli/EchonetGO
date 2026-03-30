# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
go build ./...                              # Build all packages
go build -o echonetgo ./cmd/echonetgo       # Build the binary
go test ./...                               # Run all tests
go test ./internal/echonet/...              # Run tests for a single package
go test -run TestConfigLoad ./internal/config/...  # Run a single test
gofmt -w .                                  # Format code
```

Version is set at build time: `go build -ldflags "-X main.version=0.9.27" -o echonetgo ./cmd/echonetgo`

No Makefile or linter config exists. The project uses standard `gofmt` only.

## Architecture

EchonetGO is a Go service bridging ECHONET Lite smart home devices to Home Assistant via MQTT. It polls devices over UDP (port 3610), caches state, and publishes HA auto-discovery entities.

### Data Flow

```
Config (YAML/env) → Device list → Specs load → Poller init (parallel per host IP)
  → Probe EOJ → Read manufacturer code (0x8A) → Select vendor spec
  → Read GETMAP (0x9F) → Filter supported EPCs
  → Read WRITABLE (0x9E) → Populate writable set
  → Read STATMAP (0x9D) → Learn push-capable EPCs
  → Multicast join (224.0.23.0) for INF/INFC notifications
  → Polling loop → Cache update → MQTT publish
  → Commander listens for HA commands → Sends ECHONET SET → Verifies state
```

### Key Packages

- **`cmd/echonetgo/`** — Entry point. Wires config, specs, transport, poller, MQTT, and HTTP server with graceful shutdown.
- **`internal/echonet/`** — Protocol implementation split across: `transport.go` (UDP connection pool, per-host locks, multicast), `client.go` (high-level Get/Set API with adaptive chunking), `protocol.go` (frame parsing/building), `notification.go` (INF/INFC handler), `encoder.go` (EDT encoding/decoding).
- **`internal/poller/`** — Device initialization, per-interval scrape scheduling, and concurrent-safe cache. Hybrid poll+push: skips EPCs recently updated via notification.
- **`internal/mqtt/`** — `publisher.go` (connection + state publishing), `discovery.go` (HA auto-discovery for sensor/climate/switch/select/number), `commander.go` (routes HA commands back to ECHONET SET with multi-stage verification at 1s/4s/7s).
- **`internal/specs/`** — Loads `etc/specs/*.yaml` device class definitions. Vendor-specific overrides via `{class}_{manufacturer_hex}.yaml`. Super Class merging for common EPCs (0x80-0x8F).
- **`internal/config/`** — Loads from `etc/config.yaml` + env vars (prefixed `ECHONET_` / `MQTT_`).
- **`internal/metrics/`** — Detached Prometheus collector; reads from cache without blocking device I/O.

### YAML Spec System (`etc/specs/`)

Device support is entirely YAML-driven. Each file defines an ECHONET device class with EOJ, metrics (EPC, size, scale, type, enum, HA metadata), and optional climate config. Vendor-specific files (e.g., `home_ac_000006.yaml` for Mitsubishi) are auto-selected based on manufacturer code. Adding a new device class requires only a new YAML file, no Go changes.

### Design Principles

- **No auto-discovery of unknown devices.** Devices must be explicitly configured. This prevents entity bloat from unrecognized devices on the network.
- **Hybrid poll+push model.** Devices that push state via multicast (STATMAP) skip redundant polling. `force_polling: true` overrides this.
- **Multi-stage SET verification.** After a SET command, the service polls at 1s, 4s, 7s to confirm the device actually changed state, avoiding "optimistic lies" in the HA UI.
- **Adaptive chunking.** If a device rejects a batch Get, the request is split recursively (max depth 8) and valid responses merged.

### Running Locally

Requires `etc/config.yaml` and `etc/devices.yaml` (copy from `.example` files, set your device IPs). Serves HTTP on `:9191`. For device probing without the full service: `python3 tools/echonet_probe.py <IP>`.

### Home Assistant Add-on

`addon_echonetgo/` contains the HA add-on (Dockerfile, config.yaml, run.sh). Uses host networking for UDP port 3610. MQTT credentials auto-injected by Supervisor.

## Skills (`.ai/skills/`)

Three specialized skills are configured for common workflows:

- **`echonet-spec-architect`** — Create and validate YAML device specs in `etc/specs/`. Includes a validation script (`node .ai/skills/echonet-spec-architect/scripts/validate_spec.cjs etc/specs/<file>.yaml`) and conventions reference. Use when adding new device classes or manufacturer overrides.
- **`ha-integration-expert`** — Map ECHONET properties to HA MQTT Discovery entities. Includes a discovery payload simulator (`node .ai/skills/ha-integration-expert/scripts/generate_discovery.cjs etc/specs/<file>.yaml <device_name>`). Use when troubleshooting entity discovery or determining correct `ha_device_class`/`ha_state_class` for new metrics.
- **`manufacturer-mapper`** — Translate manufacturer documentation into YAML specs. Includes a Markdown table extractor (`node .ai/skills/manufacturer-mapper/scripts/extract_metrics.cjs echonet_specs/<SPEC>.md`). Handles superset vs. override analysis: only EPCs that deviate from the base class belong in the manufacturer-specific file.

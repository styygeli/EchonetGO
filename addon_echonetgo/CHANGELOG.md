# Changelog

## 0.9.3

- Skip ECHONET 0x8C (product code) query when device config already has a `model` field, avoiding ~30s of timeouts per device at startup.
- Remove dead code: unused protocol parser, unexport internal-only symbols.
- Remove deprecated `/state` endpoint; `/metrics` is the replacement.
- Consolidate add-on docs to avoid duplication with the main README.

## 0.9.0

- Add opt-in `/metrics` endpoint with Prometheus collector for VictoriaMetrics/Prometheus scraping.
- Detached scraping: `/metrics` reads from cache, never blocks on device I/O.
- Includes Go runtime (`go_*`) and process (`process_*`) metrics automatically.
- Config: `metrics_enabled: true` (YAML) or `ECHONET_METRICS_ENABLED=true` (env).
- Skip Supervisor MQTT API call when config file already has `mqtt.broker` set.

## 0.1.37

- Refactor: eliminate global state, split `internal/echonet/` into focused files (`transport.go`, `protocol.go`, `encoder.go`, `manufacturers.go`).
- Extract MQTT discovery logic into `internal/mqtt/discovery.go`.
- Add context timeouts to MQTT SET command handlers.
- Clean up redundant comments across the codebase.
- Bidirectional control: climate, switch, select, and number entities via MQTT.
- Auto-detect writable properties via ECHONET property map (EPC 0x9E).
- Vendor-specific specs auto-selected by manufacturer code.

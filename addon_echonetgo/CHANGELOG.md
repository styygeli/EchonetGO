# Changelog

## 0.9.5

- Fix power distribution board (0x0287) EPC mappings: swap 0xC0/0xC1 (cumulative energy) with 0xC6/0xC7 (instantaneous power/current).
- Add `offset` field to metric specs — extract partial values from multi-byte EDTs (e.g. R-phase vs T-phase current from a single 4-byte EPC).
- Add `multiplier_epc` / `multiplier_map` — dynamically apply kWh unit multiplier from EPC 0xC2 to cumulative energy counters.
- Deduplicate EPCs in scrape requests when multiple metrics share the same EPC.
- Add T-phase current (0xC7 offset 2) and SN-T voltage (0xC8 offset 2) metrics for breaker box.
- Cumulative energy metrics now report directly in kWh with proper HA metadata (`energy` / `total_increasing`).

## 0.9.4

- Log version number at startup.

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

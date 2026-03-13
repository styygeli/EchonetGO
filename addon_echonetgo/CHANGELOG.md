# Changelog

## 0.9.20

- **Electric water heater (Ecocute) spec**: Set correct byte sizes for all 38 metrics (31x 1-byte, 7x 2-byte), fixing SET command encoding errors (`size 0 (auto), cannot encode for SET`). Add HA display metadata: temperature sensors with °C, water volumes in liters, HP power consumption in watts, and `ha_device_class: enum` for multi-value selects so they display string labels instead of raw numbers. Add `invalid: 0xFD` sentinel for temperature setpoints.

## 0.9.19

- **Concurrency**: Fix data race on shared EOJ cache during parallel device init by switching to `sync.Map`.
- **Concurrency**: Fix leaked goroutines in MQTT commander `triggerStateUpdate` by deriving from the parent context instead of `context.Background()`; background state-refresh goroutines now exit promptly on shutdown.
- **Error handling**: Check `json.Marshal` errors in writable entity (switch/select/number) discovery instead of silently discarding them.

## 0.9.18

- **MQTT discovery**: Defer discovery publishing until device info (manufacturer/UID) has been fetched, preventing the double wipe/publish cycle on startup. Entities now appear once with correct manufacturer/model metadata from the start instead of briefly showing "unknown". Logs a warning if a device never returns identity properties after 5 polls.

## 0.9.17

- **Super Class**: Add installation location (EPC 0x81) with ECHONET Lite standard location codes (living_room, kitchen, bathroom, etc.) to the super class, so all device types get a select dropdown instead of a useless numeric slider. Remove the per-device definition from the Mitsubishi home AC spec.

## 0.9.16

- **MQTT discovery**: Wipe stale retained discovery topics on startup before publishing fresh discovery. Clears sensor, switch, select, number, and climate config topics for all metrics in the spec, then re-publishes only data-backed entities. Eliminates ghost entities from EPCs the device no longer reports or metrics renamed across versions.

## 0.9.15

- **MQTT discovery**: Only advertise sensors and writable entities for EPCs that the device actually returns data for. Devices sharing the same spec but with different capabilities (e.g. one Mitsubishi AC reports 0xA5, another does not) now get per-device discovery based on polling results. Discovery is deferred until the first successful poll with non-empty metrics.

## 0.9.14

- **MQTT discovery**: Select entities with enum options now support a defined option order. For `air_flow_direction_horizontal` (0xA4), options are shown ceiling→floor: uppermost, upper_mid, middle, lower_mid, lowermost. Entity names follow ECHONET spec (0xA4 = horizontal, 0xA5 = vertical).

## 0.9.13

- **MAC-900IF (home_ac_000006) spec**: Document vertical blade behaviour: 0xA3 = vertical swing (off/on), 0xA4 = vertical position (0x41 uppermost → 0x44, 0x43, 0x45, 0x42 lowermost); 0xA5 not reported. Add 0xA1=0x43 (full_auto). Note that 0xA4 updates on arrival only and does not track live position during swing; horizontal blade has no EPC change on this unit.
- **echonet_probe.py**: Watch mode (`--watch`) now polls all climate EPCs (0x80, 0xB0, 0xB3, 0xBB, 0xA0, 0xA1, 0xA3, 0xA4, 0xA5) every `--interval` seconds with a dynamic banner; validate `--interval` when using `--watch`.

## 0.9.12

- Post-SET refresh: only pass requested EPCs to ParsePropsToMetrics so we no longer log spurious "missing EPC" warnings for properties we didn't ask for (e.g. when setting AC mode to auto).

## 0.9.11

- Add **readiness** endpoint `GET /ready`: returns 200 when poller and commander (if MQTT enabled) have finished init, 503 with per-component JSON until then. Use for Kubernetes readiness probes; does not depend on devices being configured or reachable.
- Clarify **liveness** vs **readiness**: `/health` remains the liveness probe (always 200 while process is up); `/ready` is the readiness probe.

## 0.9.10

- Trigger immediate GET polling to ECHONET devices after successful SET operations to update Home Assistant states rapidly without waiting for the next polling cycle.

## 0.9.9

- Remove exact-duplicate Super Class metrics (0x80, 0x88) from YAML specs where they matched loader-injected defaults; merge continues to supply them so behavior is unchanged.

## 0.9.8

- Use add-on release version as runtime `sw_version` by injecting `BUILD_VERSION` into the Go binary at build time.
- Remove hardcoded app version from `cmd/echonetgo/main.go` so startup logs and MQTT discovery report the actual release.

## 0.9.7

- Add loader-level Super Class metric merge so common ECHONET properties are defined once and injected into each class spec.
- Keep deterministic EPC precedence: class/vendor YAML definitions override Super Class defaults when the same EPC is present.
- Keep property maps (0x9E/0x9F) runtime-only for GETMAP handling (not exposed as regular metrics).
- Add/extend loader tests for Super Class injection, class override behavior, and vendor-specific spec compatibility.

## 0.9.6

- Fix bridge device "Status" sensor showing Unavailable after 5 minutes (remove time-based expiry; availability topic with LWT handles online/offline).
- Fix devices sharing the same physical ECHONET UID (e.g. EP Cube battery + solar) being merged into one HA device. UIDs are now scoped per logical device.

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

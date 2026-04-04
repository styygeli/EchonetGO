# Changelog

## 0.9.36
- **Process**: Formalized the release workflow to include mandatory PR reviews and verification steps.

## 0.9.35
- **Metrics**: Improved HELP text and fixed redundant _state suffix for metrics already ending in state.

## 0.9.34
- **Performance**: Pre-process enum metadata to optimize metrics collection loop and prevent slice allocations on the hot path.
- **Test coverage**: Add coverage for enum metrics collection.

## 0.9.33
- **Metrics Enum State Labels**: Refactored the metrics collector to use the standard Prometheus convention for enumerations via state labels. This collapses metrics like `echonet_ac_installation_location_is_bathroom` into `echonet_ac_installation_location_state{state="bathroom"}`, reducing cardinality and simplifying queries.

## 0.9.32
- **Docs**: Updated README to reflect light entity support across feature descriptions and file layout.
- **MRA reference**: Added download reference for the ECHONET Lite Machine Readable Appendix (`echonet_specs/MRA_README.md`). MRA data itself is gitignored.

## 0.9.31
- **Device names in protocol logs**: ECHONET Lite client and transport log messages now include the configured device name alongside the IP address (e.g. `timeout waiting for response from ac_av (192.168.3.249)` instead of just the IP). Uses an optional name resolver wired from the device config at startup.
- **Device name sanitization**: Device names from config are sanitized at load time to prevent log injection (control characters stripped) and MQTT topic corruption (`/`, `+`, `#` replaced with `_`). All printable Unicode including CJK characters is preserved.

## 0.9.30
- **Light entity type**: Expose lighting devices (general_lighting 0x0290, single_function_lighting 0x0291, lighting_system 0x02A3) as proper HA `light` entities with brightness and effect controls, instead of decomposed switch/number entities. Brightness uses `brightness_scale: 100` mapping ECHONET's 0-100 illuminance range. Color presets (incandescent, white, daylight_white, daylight_color) and scene selection map to HA effects. New `light:` YAML spec section defines brightness_epc, color_setting_epc/color_settings, and scene_epc/max_scenes per device class.

## 0.9.29
- **SETI spec wiring**: Add `set_mode: seti` YAML field for metrics. When set, the commander uses fire-and-forget SetI (ESV 0x60) instead of SetC, skipping response validation and multi-stage verification. Useful for non-critical property writes where blocking on device acknowledgment is unnecessary.

## 0.9.28
- **SETI protocol support**: Add SetI (ESV 0x60) fire-and-forget write support to the ECHONET Lite protocol layer. New `SendSetI` client method sends SET commands without waiting for a device response, useful for non-critical property writes. Includes `SendFireAndForget` transport method with per-host serialization, frame builder, and SetI_SNA (0x50) detection.

## 0.9.27
- **Multi-stage verification**: Implement a robust verification loop for SET commands. Instead of a single 500ms delay, the service now polls the device at 1s, 4s, and 7s intervals after a successful command acknowledgment.
- **Real data priority**: Replaces the previous verification logic to ensure Home Assistant reflects real device registers rather than optimistic assumptions. If the device takes several seconds to apply a change (common in Ecocute units), EchonetGO will eventually see the change and solidify the UI.
- **Automatic UI sync**: If a device silently ignores a command or the hardware registers never update after 3 attempts, EchonetGO publishes the actual device state to MQTT, forcing the HA UI to revert the unapplied change.

## 0.9.26
- **Protocol validation**: Add explicit validation of ECHONET Lite response ESV for SET commands. Rejections from devices (SetC_SNA, 0x51) are now correctly identified as errors.
- **HA UI Consistency**: When a SET command is rejected by a device, the commander now triggers an immediate (0ms delay) state update to fetch the actual device values. This forces the Home Assistant UI to revert its optimistic state update, keeping the UI in sync with the hardware.
- **Code cleanup**: Consolidate ESV parsing and error handling into generic protocol helpers.

## 0.9.25
- **Multicast fixes**: Suppress false "missing metric" warnings in the poller when an EPC was skipped due to a recent multicast INF push.
- **Notification noise**: Filter device specs to relevant metrics in the notification handler to avoid a wall of warnings when receiving single-property INF frames.
- **Multicast tests**: Add comprehensive unit tests for INF frame parsing, notification classification, and cache-level skip logic.
- **Concurrency audit**: Audit and verify thread-safety across multicast, poller, and MQTT commander paths.

## 0.9.24

- **INF/INFC notifications**: Receive device-initiated ECHONET Lite property notifications (ESV 0x73 INF, 0x74 INFC) in real time. Devices that broadcast state changes are reflected in Home Assistant immediately without waiting for the next poll cycle. INFC frames are automatically acknowledged (0x7A INFC_Res).
- **STATMAP discovery**: Read each device's Status Change Announcement Property Map (EPC 0x9D) at init to learn which EPCs the device will push. Polling is automatically skipped for EPCs that have been pushed recently, reducing redundant UDP traffic.
- **Multicast support**: Join the ECHONET Lite multicast group (224.0.23.0) on all suitable IPv4 interfaces by default, or restrict to specific interfaces via `multicast_interfaces`. Bound interfaces are logged at INFO level; failures are logged at WARN.
- **New config options**: `notifications_enabled` (default `true`), `force_polling` (default `false`, disables poll-skip optimization), `multicast_interfaces` (list of interface names to bind).
- **Notification listener tool**: New `tools/echonet_listen.py` standalone script for passively monitoring ECHONET Lite multicast traffic on the LAN.

## 0.9.23

- **Ecocute HA metadata**: Remove `ha_state_class: measurement` from constant/setting metrics (tank capacity, rated HP power consumption, temperature setpoints, bath volume setting) to prevent HA statistics averaging from introducing floating-point noise on values that never change. Actual sensor measurements (water temperature, remaining water volumes) retain `measurement` state class.

## 0.9.22

- **Number entity bounds**: Add `number_min`/`number_max` spec fields for HA number entities, replacing hardcoded 0-100 range with size-aware defaults (0-255 for 1-byte, 0-65535 for 2-byte) and explicit overrides. Set ecocute `manual_water_heating_stop_days_setting` max to 30 days.
- **Pre-set linked commands**: Add `pre_set` spec mechanism that automatically sends a prerequisite SET command before the main one. Ecocute vacation-days (0xB4) now auto-switches to stop-mode (0xB0=0x43) before setting the day count, so users don't need to manually change the heating mode first. Both EPCs are refreshed after the operation.

## 0.9.21

- **Spec remediation (46 files)**: Set correct byte sizes for all metrics across 46 ECHONET device class specs imported from pychonet, fixing `size 0 (auto), cannot encode for SET` errors for every SET-capable EPC. Covers home AC, electric lock, 7 cover/actuator classes (blind, shutter, curtain, window, gate, sliding door, entrance door), hot water generator, floor heater, bathroom dryer, refrigerator, electric thermos, hybrid water heater, EV charger, storage battery, fuel cell, multiple-input PCS, 2 energy meters, 3 lighting classes, air cleaner, ceiling fan, and 19 sensor classes. Add HA display metadata (device_class, state_class, unit) for temperatures, energy, power, current, humidity, pressure, CO₂, and multi-value enum selects. Add missing open/close/stop enums on cover devices from pychonet. Only one known composite EPC (electric_energy_sensor 0xE4, 48×4B read-only log) remains at size: 0 by design.
- **Regression guard**: Add `TestLoad_NoUnexpectedAutoSize` to prevent reintroduction of unresolved `size: 0` in curated spec files.

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

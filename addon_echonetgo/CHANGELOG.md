# Changelog

## 0.1.4

- Improve fixed UDP port (`3610`) compatibility by enabling `SO_REUSEADDR` on the local ECHONET socket (closer to `pychonet` behavior).

## 0.1.3

- Align transport behavior with `pychonet`: prefer local UDP source port `3610` for ECHONET requests (fallback to ephemeral if bind is unavailable).
- Add EOJ instance auto-probe (0x01..0x0F) when configured instance is not responsive, improving compatibility with devices that use non-`0x01` instance IDs.

## 0.1.2

- Enable `host_network` for reliable ECHONET Lite UDP communication with LAN/IoT devices.
- Remove explicit port mapping and rely on host networking.

## 0.1.1

- Fix Home Assistant add-on build context compatibility.
- Build the EchonetGO binary and bundled specs from the Git repository in Docker build stage.
- Remove bashio dependency from `run.sh` and read `/data/options.json` with `jq`.

## 0.1.0 (initial)

- Initial add-on release.
- Runs EchonetGO with config and devices from YAML under `/config`.
- Exposes HTTP API on port 9191 (health, state).
- Options: config_path, devices_path, log_level.
- Built-in device specs (home_ac, home_solar, storage_battery) shipped in the image.

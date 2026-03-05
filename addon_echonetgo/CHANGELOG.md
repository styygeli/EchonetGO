# Changelog

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

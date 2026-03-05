# Changelog

## 0.1.0 (initial)

- Initial add-on release.
- Runs EchonetGO with config and devices from YAML under `/config`.
- Exposes HTTP API on port 9191 (health, state).
- Options: config_path, devices_path, log_level.
- Built-in device specs (home_ac, home_solar, storage_battery) shipped in the image.

# EchonetGO Add-on Documentation

## Config and device files

Create a folder under Home Assistant config, e.g. `config/echonetgo/`, and place there:

1. **config.yaml** — Main config (see project [README](https://github.com/styygeli/EchonetGO#configuration)).
2. **devices.yaml** (optional) — Device list if you prefer to keep it separate; set `devices_path` in add-on options to this file.

In **config.yaml** you can set:

- `listen_addr` — Usually leave default; the add-on overrides with `0.0.0.0:9191` via env.
- `scrape_timeout_sec` — UDP timeout for device requests (default 15).
- `devices_path` — Path to devices file (or use add-on option `devices_path`).
- `specs_dir` — Leave empty to use the add-on’s built-in specs, or set a path inside the container if you mount custom specs.
- `devices` — Inline list of devices (name, ip, class, optional scrape_interval).

Example **config/echonetgo/config.yaml**:

```yaml
listen_addr: "0.0.0.0:9191"
scrape_timeout_sec: 15
devices_path: "/config/echonetgo/devices.yaml"
# Or list devices here:
# devices:
#   - name: epcube
#     ip: 192.168.1.100
#     class: storage_battery
```

Example **config/echonetgo/devices.yaml**:

```yaml
devices:
  - name: epcube
    ip: 192.168.1.100
    class: storage_battery
```

Set the add-on option **config_path** to `/config/echonetgo/config.yaml` (and optionally **devices_path** to `/config/echonetgo/devices.yaml`).

## API

Once the add-on is running:

- **GET http://&lt;host&gt;:9191/health** — Liveness.
- **GET http://&lt;host&gt;:9191/state** — JSON with cached device metrics and identity.

A future Home Assistant custom integration can use these endpoints to create entities from the cached state.

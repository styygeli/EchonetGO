# EchonetGO Home Assistant Add-on

Runs the [EchonetGO](https://github.com/styygeli/EchonetGO) service inside Home Assistant. The service polls ECHONET Lite devices from your YAML config, caches state, and exposes `GET /health` and `GET /state` over HTTP so a custom Home Assistant integration (or other consumers) can use the data.

## Installation

1. Add this repository as an add-on repository in Home Assistant: **Settings → Add-ons → Add-on store → ⋮ → Repositories** — add `https://github.com/styygeli/EchonetGO` (or your fork).
2. Install the **EchonetGO** add-on from the store.
3. Configure the add-on (see Configuration).
4. Create your config and device list under the mapped config path (see Documentation).
5. Start the add-on.

## Configuration

| Option        | Description                                                                 | Default |
|---------------|-----------------------------------------------------------------------------|---------|
| `config_path` | Path to the main EchonetGO config file (YAML). Use a path under `/config`. | `/config/echonetgo/config.yaml` |
| `devices_path`| Optional path to a devices YAML/JSON file. Can be left empty if devices are in the main config. | (empty) |
| `log_level`   | Log level: `debug`, `info`, `warn`, `error`.                                | `info`  |

The add-on maps `config` as read-write, so use a directory under `/config` (e.g. `/config/echonetgo/`) for your config and device files.

## Network

This add-on runs with **host networking** so ECHONET Lite UDP traffic can reliably reach devices on LAN/IoT subnets.  
The API listens on port **9191** of the Home Assistant host and can be reached at `http://<home-assistant-host>:9191`.

## Documentation

See the main project [README](https://github.com/styygeli/EchonetGO#readme) and [DOCS.md](DOCS.md) for config file format, device specs, and API endpoints.

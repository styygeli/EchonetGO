# EchonetGO Home Assistant Add-on

Polls ECHONET Lite devices from your YAML config and publishes sensors to Home Assistant via MQTT auto-discovery. Devices, sensors, and energy metrics appear automatically — no custom integration required.

## Installation

1. Add this repository as an add-on repository in Home Assistant: **Settings → Add-ons → Add-on store → ⋮ → Repositories** — add `https://github.com/styygeli/EchonetGO` (or your fork).
2. Install the **EchonetGO** add-on from the store.
3. Create your config file at `/config/echonetgo/config.yaml` (see Documentation).
4. Start the add-on.

Devices appear under **Settings → Devices** as MQTT devices, grouped under the "EchonetGO" bridge device.

## Configuration

| Option        | Description                                                                 | Default |
|---------------|-----------------------------------------------------------------------------|---------|
| `config_path` | Path to the main EchonetGO config file (YAML). Use a path under `/config`. | `/config/echonetgo/config.yaml` |
| `devices_path`| Optional path to a devices YAML/JSON file. Can be left empty if devices are in the main config. | (empty) |
| `log_level`   | Log level: `debug`, `info`, `warn`, `error`.                                | `info`  |

The add-on maps `config` as read-write, so use a directory under `/config` (e.g. `/config/echonetgo/`) for your config and device files.

## MQTT

The add-on uses `mqtt: auto` to receive broker credentials from the HA Supervisor automatically (requires the Mosquitto add-on). No manual MQTT configuration is needed.

If you need to override MQTT settings, add an `mqtt` block to your config file:

```yaml
mqtt:
  broker: "tcp://core-mosquitto:1883"
  username: "my_user"
  password: "my_pass"
```

## Network

This add-on runs with **host networking** so ECHONET Lite UDP traffic can reliably reach devices on LAN/IoT subnets.
The diagnostic API listens on port **9191** of the Home Assistant host: `http://<home-assistant-host>:9191/state`.

## Documentation

See [DOCS.md](DOCS.md) for config file format and device setup, or the main project [README](https://github.com/styygeli/EchonetGO#readme) for full details.

# EchonetGO Home Assistant Add-on

Polls ECHONET Lite devices and publishes sensors, climate, switch, select, and number entities to Home Assistant via MQTT auto-discovery. Supports bidirectional control. No custom integration required.

## Installation

1. Add this repository as an add-on repository: **Settings → Add-ons → Add-on store → ⋮ → Repositories** — add `https://github.com/styygeli/EchonetGO` (or your fork).
2. Install the **EchonetGO** add-on.
3. Create your config file at `/config/echonetgo/config.yaml` (see [DOCS.md](DOCS.md)).
4. Start the add-on.

Devices appear under **Settings → Devices** as MQTT devices, grouped under the "EchonetGO" bridge device.

## Documentation

- [DOCS.md](DOCS.md) — Add-on options, MQTT setup, and troubleshooting.
- [Project README](https://github.com/styygeli/EchonetGO#readme) — Full configuration reference, device classes, architecture, and spec format.

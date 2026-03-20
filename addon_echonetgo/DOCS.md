# EchonetGO Add-on Documentation

For full configuration reference, device classes, architecture, and spec format, see the main project [README](https://github.com/styygeli/EchonetGO#readme).

## Setup

1. Create a folder under Home Assistant config, e.g. `/config/echonetgo/`.
2. Place your `config.yaml` there (see [Configuration](https://github.com/styygeli/EchonetGO#configuration) in the main README).
3. Optionally place a separate `devices.yaml` and set `devices_path` in add-on options.

## Add-on options

| Option | Description | Default |
|--------|-------------|---------|
| `config_path` | Path to the main EchonetGO config file (YAML). Use a path under `/config`. | `/config/echonetgo/config.yaml` |
| `devices_path` | Optional path to a devices YAML/JSON file. Leave empty if devices are in the main config. | (empty) |
| `log_level` | Log verbosity: `debug`, `info`, `warn`, `error`. | `info` |

## MQTT

The add-on uses `mqtt: auto` to receive broker credentials from the HA Supervisor automatically (requires the Mosquitto add-on). No manual MQTT configuration is needed.

If your config file already has an `mqtt.broker` setting, the Supervisor API call is skipped entirely. To override manually:

```yaml
mqtt:
  broker: "tcp://core-mosquitto:1883"
  username: "my_user"
  password: "my_pass"
```

## Network

This add-on runs with **host networking** so ECHONET Lite UDP traffic can reliably reach devices on LAN/IoT subnets. The HTTP API listens on port **9191**.

### Multicast notifications

EchonetGO automatically joins the ECHONET Lite multicast group (224.0.23.0) on all suitable IPv4 interfaces to receive device-initiated property notifications (INF/INFC). On multi-VLAN hosts, set `multicast_interfaces` in your config to restrict which interfaces are used:

```yaml
multicast_interfaces:
  - eth0
  - eth0.3
```

Bound interfaces are logged at INFO level on startup. To disable notifications entirely, set `notifications_enabled: false` in your config.

## Health endpoints

The service exposes two HTTP endpoints for orchestration and load balancers:

- **`GET /health`** — **Liveness.** Returns 200 with `{"status":"ok"}` whenever the process is running. Use this for Kubernetes liveness probes (or equivalent) to detect a hung process.
- **`GET /ready`** — **Readiness.** Returns 200 with `{"status":"ready","components":{...}}` only after the poller and (if MQTT is enabled) the commander have finished startup. Returns 503 with `{"status":"not_ready","components":{...}}` until then. Use this for Kubernetes readiness probes so traffic is not sent to the add-on before it has completed initialization. Readiness does not depend on devices being configured or reachable.

## Troubleshooting

- **Devices not responding** — Check that the device IP is reachable from the HA host. ECHONET Lite uses UDP port 3610. Some devices (like Mitsubishi MAC-900IF) require source port 3610.
- **MQTT not connecting** — Check add-on logs for MQTT errors. If your config file already has an `mqtt.broker` setting, the Supervisor API is skipped entirely. Otherwise the add-on queries the Supervisor for MQTT credentials on startup.
- **Stale entities after spec changes** — If you remove metrics from a spec, the old HA entities persist as retained MQTT messages. Publish an empty payload (with retain) to `homeassistant/sensor/{entity_id}/config` to remove them, or delete via the HA MQTT integration's "Publish a packet" feature.
- **Unknown manufacturer/model** — Some devices don't respond to identity EPCs. Add `manufacturer` and `model` fields to the device config as fallbacks.

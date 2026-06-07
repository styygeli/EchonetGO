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

MQTT configuration is handled exclusively via the **Add-on Configuration UI** in Home Assistant. This is more secure and avoids issues with Supervisor API permissions.

1. Go to **Settings** -> **Add-ons** -> **EchonetGO**.
2. Click the **Configuration** tab.
3. Fill in your broker details:
   - `mqtt_broker`: e.g., `tcp://core-mosquitto:1883` or `tcp://192.168.x.x:1883`
   - `mqtt_username`: Your MQTT user
   - `mqtt_password`: Your MQTT password

MQTT settings in `config.yaml` are ignored when running as an add-on.

## Network

This add-on runs with **host networking** (`host_network: true`). This is required because the ECHONET Lite protocol relies on UDP multicast group `224.0.23.0:3610` for device notifications and discovery. Docker's bridged networking does not forward multicast traffic from the host's physical interfaces into containers, making host networking the only reliable option for this protocol. The HTTP API listens on port **9191**.

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

## Customizing Device Behavior

Device support in EchonetGO is defined by YAML specification files in `etc/specs/`. You can customize how your device appears in Home Assistant by creating manufacturer-specific overrides.

### Friendly Fan Speeds

By default, air conditioners show fan speeds as `level_1`, `level_2`, etc., because the ECHONET Lite standard does not define human-friendly names for these levels. You can override this for your specific model:

1. **Identify your device's Manufacturer ID:** Check the EchonetGO logs on startup. You will see a line like: `published discovery for climate_unit (..., mfg="000006" model="...")`. The `mfg` hex code is what you need.
2. **Discover your levels:** Use your physical remote and watch the Home Assistant state for your Climate entity. When you set it to "Quiet" on the remote, check the logs or HA to see which `level_X` it corresponds to.
3. **Create an override file:** Create a file in your specs directory named `home_ac_{mfg}.yaml` (e.g., `home_ac_000006.yaml`). 
    *   **Note:** Manufacturer-specific files **replace** the base class spec entirely. You should copy the contents of the generic `home_ac.yaml` into your new file as a starting point, then modify the specific parts you want to change.
4. **Define the mapping:** Map the levels to friendly names in the `enum` section and set the UI order in the `fan_modes` list.

```yaml
metrics:
  - epc: 0xA0
    name: air_flow_rate
    enum:
      0x41: auto
      0x31: quiet
      0x32: low
      0x33: medium
      0x34: high
climate:
  fan_mode_epc: 0xA0
  fan_modes: ["auto", "quiet", "low", "medium", "high"]
```

### Contributing to the Database

We encourage users to contribute their manufacturer-specific mappings back to the EchonetGO project! If you have figured out the friendly names or specific EPCs for your hardware, please submit a Pull Request to the [EchonetGO repository](https://github.com/styygeli/EchonetGO) with your new spec file. This helps build a comprehensive equipment database for everyone.

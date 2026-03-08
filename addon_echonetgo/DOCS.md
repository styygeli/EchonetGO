# EchonetGO Add-on Documentation

## Config and device files

Create a folder under Home Assistant config, e.g. `config/echonetgo/`, and place there:

1. **config.yaml** — Main config (see below and project [README](https://github.com/styygeli/EchonetGO#configuration)).
2. **devices.yaml** (optional) — Device list if you prefer to keep it separate; set `devices_path` in add-on options to this file.

### config.yaml reference

```yaml
listen_addr: "0.0.0.0:9191"
scrape_timeout_sec: 15

# MQTT is auto-configured when running as an HA add-on.
# Only add this block if you need to override:
# mqtt:
#   broker: "tcp://core-mosquitto:1883"
#   username: ""
#   password: ""

devices:
  - name: epcube_battery
    ip: 192.168.1.10
    class: storage_battery
    manufacturer: "Eternalplanet"   # optional: shown in HA if device doesn't report it
    model: "EP Cube"                # optional: shown in HA if device doesn't report it

  - name: epcube_solar
    ip: 192.168.1.10
    class: home_solar
    model: "EP Cube"

  - name: living_ac
    ip: 192.168.1.20
    class: home_ac

  - name: breaker_panel
    ip: 192.168.1.30
    class: power_dist_board

  - name: panel_solar
    ip: 192.168.1.30
    class: home_solar

  - name: water_meter
    ip: 192.168.1.30
    class: water_flow_meter

  - name: gas_meter
    ip: 192.168.1.30
    class: gas_meter
```

### Device fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique device name. Used in MQTT topics, entity IDs, and the HA device name. |
| `ip` | Yes | Device IP address. Multiple devices can share the same IP (e.g. multi-EOJ devices like Smart Cosmo). |
| `class` | Yes | Device class — matches the spec filename in `etc/specs/` (e.g. `home_ac`, `storage_battery`). |
| `manufacturer` | No | Fallback manufacturer name if the device doesn't report EPC 0x8A. |
| `model` | No | Fallback model/product name if the device doesn't report EPC 0x8C. |
| `scrape_interval` | No | Override the spec's default scrape interval (e.g. `2m`, `30s`). |

### Available device classes

| Class | ECHONET EOJ | Typical devices |
|-------|-------------|-----------------|
| `home_ac` | 0x0130 | Air conditioners (Daikin, Mitsubishi, etc.) |
| `storage_battery` | 0x027D | Battery systems (EP Cube, etc.) |
| `home_solar` | 0x0279 | Solar inverters |
| `power_dist_board` | 0x0287 | Breaker panels (Panasonic Smart Cosmo, etc.) |
| `water_flow_meter` | 0x0281 | Water meters |
| `gas_meter` | 0x0282 | Gas meters |

Vendor-specific specs (e.g. `home_ac_000006.yaml` for Mitsubishi) are auto-selected when the device reports a matching manufacturer code.

## MQTT and Home Assistant

The add-on publishes sensors to Home Assistant via MQTT auto-discovery. After starting:

- A bridge device named **EchonetGO** appears under **Settings → Devices**.
- Each configured ECHONET device appears as a child device with its sensors.
- Sensors include appropriate `device_class`, `state_class`, and `unit_of_measurement` for HA dashboards and the **Energy Dashboard**.
- Enum sensors (operation mode, fault status, etc.) show human-readable labels.
- Metrics that report invalid/sentinel values (e.g. "unable to measure") are shown as "Unknown" rather than displaying nonsensical numbers.

### Energy Dashboard sensors

The following sensor types are pre-configured for the Energy Dashboard:

- **Power** (W) — instantaneous power consumption/generation
- **Energy** (kWh, Wh) — cumulative energy with `total_increasing` state class
- **Water** (m³) — cumulative water consumption
- **Gas** (m³) — cumulative gas consumption
- **Battery** (%) — state of charge

## Diagnostic API

The add-on also exposes a diagnostic HTTP API on port 9191:

- **GET /health** — Liveness check.
- **GET /state** — JSON snapshot of all device metrics, identity info, and scrape diagnostics.

## Troubleshooting

- **Devices not responding** — Check that the device IP is reachable from the HA host. ECHONET Lite uses UDP port 3610. Some devices (like Mitsubishi MAC-900IF) require source port 3610.
- **MQTT not connecting** — Check add-on logs for MQTT errors. The add-on queries the Supervisor for MQTT credentials on startup. If you see "No access to mqtt service", try uninstalling and reinstalling the add-on to refresh permissions.
- **Stale entities after spec changes** — If you remove metrics from a spec, the old HA entities persist as retained MQTT messages. Publish an empty payload (with retain) to `homeassistant/sensor/{entity_id}/config` to remove them, or delete via the HA MQTT integration's "Publish a packet" feature.
- **Unknown manufacturer/model** — Some devices don't respond to identity EPCs. Add `manufacturer` and `model` fields to the device config as fallbacks.

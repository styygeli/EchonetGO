#!/usr/bin/with-contenv bashio
# EchonetGO add-on entrypoint. Reads options from /data/options.json and runs the binary.
set -e

CONFIG_PATH="/config/echonetgo/config.yaml"
DEVICES_PATH=""
LOG_LEVEL="info"

if bashio::config.has_value 'config_path'; then
  CONFIG_PATH=$(bashio::config 'config_path')
fi
if bashio::config.has_value 'devices_path'; then
  DEVICES_PATH=$(bashio::config 'devices_path')
fi
if bashio::config.has_value 'log_level'; then
  LOG_LEVEL=$(bashio::config 'log_level')
fi

export ECHONET_CONFIG="${CONFIG_PATH}"
export ECHONET_LOG_LEVEL="${LOG_LEVEL:-info}"
[ -n "${DEVICES_PATH}" ] && export ECHONET_DEVICES_PATH="${DEVICES_PATH}"

export ECHONET_SPECS_DIR="${ECHONET_SPECS_DIR:-/usr/share/echonetgo/specs}"
export ECHONET_LISTEN_ADDR="0.0.0.0:9191"

# MQTT: fetch credentials from HA Supervisor services API (mqtt: auto)
if bashio::services.available "mqtt"; then
  MQTT_HOST=$(bashio::services mqtt "host")
  MQTT_PORT=$(bashio::services mqtt "port")
  MQTT_USER=$(bashio::services mqtt "username")
  MQTT_PASS=$(bashio::services mqtt "password")

  export MQTT_BROKER="tcp://${MQTT_HOST}:${MQTT_PORT}"
  export MQTT_USER
  export MQTT_PASS
  bashio::log.info "MQTT service found at ${MQTT_HOST}:${MQTT_PORT}"
else
  bashio::log.warning "No MQTT service available from Supervisor"
fi

exec /usr/bin/echonetgo

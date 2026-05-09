#!/usr/bin/with-contenv sh
# EchonetGO add-on entrypoint. Reads options and fetches MQTT from Supervisor.
set -e

CONFIG_PATH="/config/echonetgo/config.yaml"
DEVICES_PATH=""
LOG_LEVEL="info"

if [ -f /data/options.json ]; then
  v=$(jq -r '.config_path // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && CONFIG_PATH="$v"
  v=$(jq -r '.devices_path // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && DEVICES_PATH="$v"
  v=$(jq -r '.log_level // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && LOG_LEVEL="$v"
  
  # MQTT options from UI
  v=$(jq -r '.mqtt_broker // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export MQTT_BROKER="$v"
  v=$(jq -r '.mqtt_username // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export MQTT_USER="$v"
  v=$(jq -r '.mqtt_password // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export MQTT_PASS="$v"
fi

export ECHONET_CONFIG="${CONFIG_PATH}"
export ECHONET_LOG_LEVEL="${LOG_LEVEL:-info}"
[ -n "${DEVICES_PATH}" ] && export ECHONET_DEVICES_PATH="${DEVICES_PATH}"

export ECHONET_SPECS_DIR="${ECHONET_SPECS_DIR:-/usr/share/echonetgo/specs}"
export ECHONET_LISTEN_ADDR="0.0.0.0:9191"

exec /usr/bin/echonetgo

#!/usr/bin/with-contenv sh
# EchonetGO add-on entrypoint. Reads options and fetches MQTT from Supervisor.
set -e

DEVICES_PATH="/config/echonetgo/devices.yaml"
LOG_LEVEL="info"

if [ -f /data/options.json ]; then
  v=$(jq -r '.devices_path // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && DEVICES_PATH="$v"
  v=$(jq -r '.log_level // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && LOG_LEVEL="$v"
  
  # MQTT options from UI
  v=$(jq -r '.mqtt_broker // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export MQTT_BROKER="$v"
  v=$(jq -r '.mqtt_username // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export MQTT_USER="$v"
  v=$(jq -r '.mqtt_password // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export MQTT_PASS="$v"

  # Core settings
  v=$(jq -r '.listen_addr // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_LISTEN_ADDR="$v"
  v=$(jq -r '.scrape_timeout_sec // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_SCRAPE_TIMEOUT_SEC="$v"
  v=$(jq -r '.strict_source_port_3610 // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_STRICT_SOURCE_PORT_3610="$v"
  v=$(jq -r '.metrics_enabled // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_METRICS_ENABLED="$v"
  v=$(jq -r '.notifications_enabled // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_NOTIFICATIONS_ENABLED="$v"
  v=$(jq -r '.force_polling // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_FORCE_POLLING="$v"
  v=$(jq -r '.multicast_interfaces // empty' /data/options.json 2>/dev/null) && [ -n "$v" ] && export ECHONET_MULTICAST_INTERFACES="$v"
fi

export ECHONET_LOG_LEVEL="${LOG_LEVEL:-info}"
[ -n "${DEVICES_PATH}" ] && export ECHONET_DEVICES_PATH="${DEVICES_PATH}"

# Use user-provided specs if the directory exists, else fallback to bundled defaults.
if [ -d "/config/echonetgo/specs" ]; then
  export ECHONET_SPECS_DIR="/config/echonetgo/specs"
else
  export ECHONET_SPECS_DIR="/usr/share/echonetgo/specs"
fi

exec /usr/bin/echonetgo

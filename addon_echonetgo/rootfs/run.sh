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
fi

export ECHONET_CONFIG="${CONFIG_PATH}"
export ECHONET_LOG_LEVEL="${LOG_LEVEL:-info}"
[ -n "${DEVICES_PATH}" ] && export ECHONET_DEVICES_PATH="${DEVICES_PATH}"

export ECHONET_SPECS_DIR="${ECHONET_SPECS_DIR:-/usr/share/echonetgo/specs}"
export ECHONET_LISTEN_ADDR="0.0.0.0:9191"

# MQTT: query Supervisor services API directly via curl
if [ -n "${SUPERVISOR_TOKEN}" ] && [ -z "${MQTT_BROKER}" ]; then
  MQTT_JSON=$(curl -s -f \
    -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
    http://supervisor/services/mqtt 2>/dev/null) || true

  if [ -n "${MQTT_JSON}" ]; then
    MQTT_HOST=$(echo "${MQTT_JSON}" | jq -r '.data.host // empty')
    MQTT_PORT=$(echo "${MQTT_JSON}" | jq -r '.data.port // "1883"')
    MQTT_USER=$(echo "${MQTT_JSON}" | jq -r '.data.username // empty')
    MQTT_PASS=$(echo "${MQTT_JSON}" | jq -r '.data.password // empty')

    if [ -n "${MQTT_HOST}" ]; then
      export MQTT_BROKER="tcp://${MQTT_HOST}:${MQTT_PORT}"
      [ -n "${MQTT_USER}" ] && export MQTT_USER
      [ -n "${MQTT_PASS}" ] && export MQTT_PASS
      echo "[run.sh] MQTT service found at ${MQTT_HOST}:${MQTT_PORT}"
    fi
  else
    echo "[run.sh] WARNING: Could not fetch MQTT service from Supervisor (token present but API call failed)"
  fi
else
  if [ -z "${SUPERVISOR_TOKEN}" ]; then
    echo "[run.sh] WARNING: No SUPERVISOR_TOKEN, skipping MQTT service discovery"
  fi
fi

exec /usr/bin/echonetgo

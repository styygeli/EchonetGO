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
  HTTP_CODE=$(curl -s -o /tmp/mqtt_resp.json -w '%{http_code}' \
    -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
    http://supervisor/services/mqtt 2>/dev/null) || HTTP_CODE="000"

  echo "[run.sh] Supervisor /services/mqtt returned HTTP ${HTTP_CODE}"

  if [ "${HTTP_CODE}" = "200" ]; then
    MQTT_HOST=$(jq -r '.data.host // empty' /tmp/mqtt_resp.json)
    MQTT_PORT=$(jq -r '.data.port // "1883"' /tmp/mqtt_resp.json)
    MQTT_USER=$(jq -r '.data.username // empty' /tmp/mqtt_resp.json)
    MQTT_PASS=$(jq -r '.data.password // empty' /tmp/mqtt_resp.json)

    if [ -n "${MQTT_HOST}" ]; then
      export MQTT_BROKER="tcp://${MQTT_HOST}:${MQTT_PORT}"
      [ -n "${MQTT_USER}" ] && export MQTT_USER
      [ -n "${MQTT_PASS}" ] && export MQTT_PASS
      echo "[run.sh] MQTT service found at ${MQTT_HOST}:${MQTT_PORT} (user=${MQTT_USER})"
    else
      echo "[run.sh] WARNING: Supervisor returned 200 but no MQTT host in response"
      cat /tmp/mqtt_resp.json
    fi
  else
    echo "[run.sh] WARNING: Supervisor API denied (HTTP ${HTTP_CODE}). Try uninstalling and reinstalling the add-on."
    cat /tmp/mqtt_resp.json 2>/dev/null || true
  fi
  rm -f /tmp/mqtt_resp.json
else
  if [ -z "${SUPERVISOR_TOKEN}" ]; then
    echo "[run.sh] WARNING: No SUPERVISOR_TOKEN, skipping MQTT service discovery"
  fi
fi

exec /usr/bin/echonetgo

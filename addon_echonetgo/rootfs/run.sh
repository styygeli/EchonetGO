#!/usr/bin/with-contenv sh
# EchonetGO add-on entrypoint. Reads options from /data/options.json and runs the binary.
set -e

# Defaults; may be overridden via /data/options.json.
CONFIG_PATH="/config/echonetgo/config.yaml"
DEVICES_PATH=""
LOG_LEVEL="info"

if [ -f /data/options.json ]; then
  CONFIG_PATH=$(jq -r '.config_path // empty' /data/options.json 2>/dev/null || true)
  DEVICES_PATH=$(jq -r '.devices_path // empty' /data/options.json 2>/dev/null || true)
  LOG_LEVEL=$(jq -r '.log_level // "info"' /data/options.json 2>/dev/null || echo "info")
fi

[ -n "${CONFIG_PATH}" ] || CONFIG_PATH="/config/echonetgo/config.yaml"
[ -n "${LOG_LEVEL}" ] || LOG_LEVEL="info"

export ECHONET_CONFIG="${CONFIG_PATH}"
export ECHONET_LOG_LEVEL="${LOG_LEVEL:-info}"
[ -n "${DEVICES_PATH}" ] && export ECHONET_DEVICES_PATH="${DEVICES_PATH}"

# Default specs dir inside the container (shipped with addon)
export ECHONET_SPECS_DIR="${ECHONET_SPECS_DIR:-/usr/share/echonetgo/specs}"

# Listen on all interfaces so HA can reach the API
export ECHONET_LISTEN_ADDR="0.0.0.0:9191"

exec /usr/bin/echonetgo

#!/usr/bin/with-contenv sh
# EchonetGO add-on entrypoint. Reads options from /data/options.json and runs the binary.
set -e

CONFIG_PATH=$(bashio::config "config_path")
DEVICES_PATH=$(bashio::config "devices_path")
LOG_LEVEL=$(bashio::config "log_level")

export ECHONET_CONFIG="${CONFIG_PATH}"
export ECHONET_LOG_LEVEL="${LOG_LEVEL:-info}"
[ -n "${DEVICES_PATH}" ] && export ECHONET_DEVICES_PATH="${DEVICES_PATH}"

# Default specs dir inside the container (shipped with addon)
export ECHONET_SPECS_DIR="${ECHONET_SPECS_DIR:-/usr/share/echonetgo/specs}"

# Listen on all interfaces so HA can reach the API
export ECHONET_LISTEN_ADDR="0.0.0.0:9191"

exec /usr/bin/echonetgo

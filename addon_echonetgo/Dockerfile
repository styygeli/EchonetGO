# Build from add-on directory context:
#   docker build -f addon_echonetgo/Dockerfile addon_echonetgo
#
# Home Assistant overrides BUILD_FROM automatically during add-on builds.
ARG BUILD_FROM=ghcr.io/hassio-addons/app-base:20.0.1
FROM golang:1.26-alpine AS builder
ARG ECHONETGO_REPO=https://github.com/styygeli/EchonetGO.git
ARG ECHONETGO_REF=main
RUN apk add --no-cache git ca-certificates
WORKDIR /src
RUN git clone --depth=1 --branch "${ECHONETGO_REF}" "${ECHONETGO_REPO}" /src
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /echonetgo ./cmd/echonetgo

FROM ${BUILD_FROM}
ARG BUILD_ARCH
RUN apk add --no-cache ca-certificates jq
COPY --from=builder /echonetgo /usr/bin/echonetgo
COPY rootfs /
# Default specs shipped with the addon (can be overridden by config)
COPY --from=builder /src/etc/specs /usr/share/echonetgo/specs
RUN chmod a+x /run.sh

LABEL \
  io.hass.name="EchonetGO" \
  io.hass.description="ECHONET Lite polling service in Go" \
  io.hass.type="addon" \
  io.hass.arch="${BUILD_ARCH}"

CMD [ "/run.sh" ]

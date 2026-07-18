# Build from add-on directory context:
#   docker build -f addon_echonetgo/Dockerfile addon_echonetgo
#
# Home Assistant overrides BUILD_FROM automatically during add-on builds.
ARG BUILD_FROM=ghcr.io/hassio-addons/base:20.1.0
FROM golang:1.26.5-alpine AS builder
ARG BUILD_VERSION=dev
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY . /src
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${BUILD_VERSION}" -o /echonetgo ./cmd/echonetgo

FROM ${BUILD_FROM}
ARG BUILD_ARCH
RUN apk add --no-cache ca-certificates jq
COPY --from=builder /echonetgo /usr/bin/echonetgo
COPY addon_echonetgo/rootfs /
# Default specs shipped with the addon (can be overridden by config)
COPY --from=builder /src/etc/specs /usr/share/echonetgo/specs
RUN chmod a+x /run.sh

LABEL \
  org.opencontainers.image.source="https://github.com/styygeli/EchonetGO" \
  io.hass.name="EchonetGO" \
  io.hass.description="ECHONET Lite polling service in Go" \
  io.hass.type="addon" \
  io.hass.arch="${BUILD_ARCH}"

CMD [ "/run.sh" ]

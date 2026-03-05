# Build from repository root: docker build -f addon_echonetgo/Dockerfile .
# BUILD_FROM is set by Home Assistant when building; default for local testing (has bashio).
ARG BUILD_FROM=ghcr.io/hassio-addons/app-base:20.0.1
FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /echonetgo ./cmd/echonetgo

FROM ${BUILD_FROM}
ARG BUILD_ARCH
RUN apk add --no-cache ca-certificates
COPY --from=builder /echonetgo /usr/bin/echonetgo
COPY addon_echonetgo/rootfs /
# Default specs shipped with the addon (can be overridden by config)
COPY etc/specs /usr/share/echonetgo/specs
RUN chmod a+x /run.sh

LABEL \
  io.hass.name="EchonetGO" \
  io.hass.description="ECHONET Lite polling service in Go" \
  io.hass.type="addon" \
  io.hass.arch="${BUILD_ARCH}"

CMD [ "/run.sh" ]

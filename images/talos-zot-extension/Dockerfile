# syntax=docker/dockerfile:1.7

ARG ZOT_VERSION=2.1.14
ARG TARGETARCH

FROM alpine:3.20 AS fetcher

ARG ZOT_VERSION
ARG TARGETARCH

RUN apk add --no-cache ca-certificates curl

# Docker TARGETARCH is "amd64" or "arm64", matching zot release artifact names.
RUN curl -fsSL \
      "https://github.com/project-zot/zot/releases/download/v${ZOT_VERSION}/zot-linux-${TARGETARCH}" \
      -o /zot \
    && chmod 0755 /zot

FROM scratch

COPY manifest.yaml /manifest.yaml
COPY zot.yaml /rootfs/usr/local/etc/containers/zot.yaml
COPY --from=fetcher /zot /rootfs/usr/local/lib/containers/zot/zot

WORKDIR /rootfs/usr/local/lib/containers/zot
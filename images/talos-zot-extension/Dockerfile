# syntax=docker/dockerfile:1.24@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89

ARG ZOT_VERSION

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS fetcher

ARG ZOT_VERSION

RUN apk add --no-cache ca-certificates curl

ADD --chmod=755 https://github.com/project-zot/zot/releases/download/v${ZOT_VERSION}/zot-linux-amd64 /zot

FROM scratch

COPY manifest.yaml /manifest.yaml
COPY zot.yaml /rootfs/usr/local/etc/containers/zot.yaml
COPY --from=fetcher /zot /rootfs/usr/local/lib/containers/zot/zot

WORKDIR /rootfs/usr/local/lib/containers/zot
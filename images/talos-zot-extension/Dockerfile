# syntax=docker/dockerfile:1.25@sha256:0adf442eae370b6087e08edc7c50b552d80ddf261576f4ebd6421006b2461f12

ARG ZOT_VERSION

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS fetcher

ARG ZOT_VERSION

RUN apk add --no-cache ca-certificates curl

ADD --chmod=755 https://github.com/project-zot/zot/releases/download/v${ZOT_VERSION}/zot-linux-amd64 /zot

FROM scratch

COPY manifest.yaml /manifest.yaml
COPY zot.yaml /rootfs/usr/local/etc/containers/zot.yaml
COPY --from=fetcher /zot /rootfs/usr/local/lib/containers/zot/zot

WORKDIR /rootfs/usr/local/lib/containers/zot
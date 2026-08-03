# syntax=docker/dockerfile:1.26@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32

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
# syntax=docker/dockerfile:1.28@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b

ARG ZOT_VERSION

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS fetcher

ARG ZOT_VERSION

RUN apk add --no-cache ca-certificates curl

ADD --chmod=755 https://github.com/project-zot/zot/releases/download/v${ZOT_VERSION}/zot-linux-amd64 /zot

FROM scratch

COPY manifest.yaml /manifest.yaml
COPY zot.yaml /rootfs/usr/local/etc/containers/zot.yaml
COPY --from=fetcher /zot /rootfs/usr/local/lib/containers/zot/zot

WORKDIR /rootfs/usr/local/lib/containers/zot
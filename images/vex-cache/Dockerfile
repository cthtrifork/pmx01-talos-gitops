# Pre-warmed Trivy VEX cache for the trivy-operator scan jobs (k8s/infra/security-system/trivy-operator/helm-release.yaml).
# The scan jobs mount it as a read-only image volume at <cache-dir>/vex, so the containers of a scan pod never download
# the VEX repositories into the same empty cache at the same time (that race fails the job with "unlinkat ... directory not empty").
# Build context is the repository root, because our own VEX repository (vex/) is baked in too.
ARG TRIVY_IMAGE=ghcr.io/aquasecurity/trivy:0.76.2@sha256:8f6bb947dbc528902e11f2f5be52f7b519f6a39d5d148f41166f2040499768fb
# the trivy image has no httpd applet in its busybox, so the throwaway web server below is Python
ARG PYTHON_IMAGE=python:3.14-alpine@sha256:a18b20ad69e74df6cf6d2e1b1a260d7a1ecf5380fd83b310f83ba88c461cf894
FROM ${TRIVY_IMAGE} AS trivy

FROM ${PYTHON_IMAGE} AS build
COPY --from=trivy /usr/local/bin/trivy /usr/local/bin/trivy
ENV HOME=/root
COPY images/vex-cache/repository.yaml /root/.trivy/vex/repository.yaml
COPY vex /src/vex
# The "homelab-vexhub" entry is served from vex/ by a throwaway web server, the same way the in-cluster mirror (vex-hub)
# does it: for a non-GitHub URL Trivy requests <url>/.well-known/vex-repository.json, whose location points at the archive.
# The manifests are cached with a 7 day update_interval (24h upstream), so an image that CI failed to refresh stays valid
# for a week instead of making the scan jobs try to update the read-only cache.
RUN set -eu; \
    mkdir -p /srv/homelab/.well-known; \
    tar -C /src -czf /srv/homelab.tar.gz vex; \
    sed 's#"url": "[^"]*"#"url": "http://127.0.0.1:8080/homelab.tar.gz//vex"#' /src/vex/vex-repository.json > /srv/homelab/.well-known/vex-repository.json; \
    python3 -m http.server 8080 --bind 127.0.0.1 --directory /srv >/dev/null 2>&1 & \
    sleep 2; \
    trivy vex repo download --cache-dir /out; \
    sed -i 's#"update_interval": *"24h"#"update_interval": "168h"#' /out/vex/repositories/*/vex-repository.json; \
    test -s /out/vex/repositories/default/0.1/index.json; \
    test -s /out/vex/repositories/homelab-vexhub/0.1/index.json

FROM scratch
COPY --from=build /out/vex/ /

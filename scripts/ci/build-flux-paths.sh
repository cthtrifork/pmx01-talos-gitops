#!/usr/bin/env bash
# Build every path a Flux Kustomization in the cluster directory points at, the way
# kustomize-controller does (no kustomization.yaml means Flux generates one, so those are skipped),
# and schema-check the output.
set -euo pipefail

cluster_dir="${1:-k8s/clusters/homelab}"
crd_catalog='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

if [[ ! -d /tmp/flux-crd-schemas ]]; then
    mkdir -p /tmp/flux-crd-schemas/master-standalone-strict
    for url in \
        https://github.com/controlplaneio-fluxcd/flux-operator/releases/latest/download/crd-schemas.tar.gz \
        https://github.com/fluxcd/flux2/releases/latest/download/crd-schemas.tar.gz; do
        curl -fsSL "$url" | tar zxf - -C /tmp/flux-crd-schemas/master-standalone-strict
    done
fi

status=0
while IFS= read -r path; do
    # paths are written relative to the cluster directory, but Flux resolves them from the repo root
    path="$(echo "$path" | sed -E 's#^(\.\./)+##')"
    if [[ ! -f "$path/kustomization.yaml" ]]; then
        echo "INFO - $path has no kustomization.yaml, Flux generates one: skipping"
        continue
    fi
    echo "INFO - building $path"
    if ! kustomize build "$path" --load-restrictor=LoadRestrictionsNone |
        kubeconform -strict -ignore-missing-schemas -skip=Secret -summary \
            -schema-location default \
            -schema-location /tmp/flux-crd-schemas \
            -schema-location "$crd_catalog"; then
        echo "ERROR - $path failed to build or validate" >&2
        status=1
    fi
done < <(yq e '.spec.path | select(. != null)' "$cluster_dir"/*.yaml | grep -v '^---' | sort -u)
exit "$status"

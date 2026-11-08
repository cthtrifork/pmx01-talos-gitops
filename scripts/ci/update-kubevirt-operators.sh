#!/usr/bin/env bash
# Regenerate the vendored KubeVirt and CDI operator manifests (k8s/infra/kubevirt/) at the versions pinned in
# k8s/infra/kubevirt/versions.env, which Renovate bumps. Renovate cannot edit the manifests itself: bumping only
# the image line leaves the CRDs, RBAC and versions of the release behind.
# Needs yq (mikefarah) and docker buildx. Prints one line per updated file; changes nothing if already current.
set -euo pipefail

cd "$(dirname "$0")/../.."

# shellcheck source=/dev/null
source k8s/infra/kubevirt/versions.env

# repository | release asset | file | operator image | pinned version
TARGETS=(
  "kubevirt/kubevirt|kubevirt-operator.yaml|k8s/infra/kubevirt/kubevirt-operator.yaml|quay.io/kubevirt/virt-operator|${KUBEVIRT_VERSION}"
  "kubevirt/containerized-data-importer|cdi-operator.yaml|k8s/infra/kubevirt/cdi-operator.yaml|quay.io/kubevirt/cdi-operator|${CDI_VERSION}"
)

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

for target in "${TARGETS[@]}"; do
  IFS='|' read -r repo asset file image version <<<"$target"

  current=$(sed -nE "s|^ +image: ${image}:([^@ ]+).*|\1|p" "$file" | head -n 1)
  if [[ -z "$current" || -z "$version" ]]; then
    echo "cannot determine versions for ${file} (current='${current}', version='${version}')" >&2
    exit 1
  fi
  [[ "$current" == "$version" ]] && continue

  digest=$(docker buildx imagetools inspect "${image}:${version}" --format '{{.Manifest.Digest}}')
  [[ "$digest" == sha256:* ]] || { echo "no digest for ${image}:${version}" >&2; exit 1; }

  curl -fsSL -o "$tmp" "https://github.com/${repo}/releases/download/${version}/${asset}"
  # Same formatting as the files in the repo (indented sequences, document start), pinned by digest like Renovate does
  {
    echo '---'
    yq e '.' "$tmp" | sed -E "s|^( +image: )${image}:.*|\1${image}:${version}@${digest}|"
  } >"$file"

  echo "${image##*/} ${current} -> ${version}"
done

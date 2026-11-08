#!/usr/bin/env bash
# Refresh the vendored KubeVirt and CDI operator manifests (k8s/infra/kubevirt/) from the latest upstream release.
# Renovate cannot do this: bumping only the image line leaves the CRDs, RBAC and versions of the release behind.
# Needs gh, yq (mikefarah) and docker buildx. Prints one line per updated file; changes nothing if already current.
set -euo pipefail

cd "$(dirname "$0")/../.."

# repository | release asset | file | operator image
TARGETS=(
  "kubevirt/kubevirt|kubevirt-operator.yaml|k8s/infra/kubevirt/kubevirt-operator.yaml|quay.io/kubevirt/virt-operator"
  "kubevirt/containerized-data-importer|cdi-operator.yaml|k8s/infra/kubevirt/cdi-operator.yaml|quay.io/kubevirt/cdi-operator"
)

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

for target in "${TARGETS[@]}"; do
  IFS='|' read -r repo asset file image <<<"$target"

  current=$(sed -nE "s|^ +image: ${image}:([^@ ]+).*|\1|p" "$file" | head -n 1)
  latest=$(gh api "repos/${repo}/releases/latest" --jq .tag_name) # stable releases only
  if [[ -z "$current" || -z "$latest" ]]; then
    echo "cannot determine versions for ${file} (current='${current}', latest='${latest}')" >&2
    exit 1
  fi
  [[ "$current" == "$latest" ]] && continue

  digest=$(docker buildx imagetools inspect "${image}:${latest}" --format '{{.Manifest.Digest}}')
  [[ "$digest" == sha256:* ]] || { echo "no digest for ${image}:${latest}" >&2; exit 1; }

  curl -fsSL -o "$tmp" "https://github.com/${repo}/releases/download/${latest}/${asset}"
  # Same formatting as the files in the repo (indented sequences, document start), pinned by digest like Renovate does
  {
    echo '---'
    yq e '.' "$tmp" | sed -E "s|^( +image: )${image}:.*|\1${image}:${latest}@${digest}|"
  } >"$file"

  echo "${image##*/} ${current} -> ${latest}"
done

#!/usr/bin/env bash
# Fail if a Kubernetes Secret with data/stringData is committed without SOPS encryption.
set -euo pipefail

status=0
while IFS= read -r -d '' file; do
    if yq e 'select(.kind == "Secret") | (.data // .stringData) | select(. != null) | length' "$file" |
        grep -q '[0-9]' && ! grep -q '^sops:' "$file"; then
        echo "ERROR - plaintext Secret: $file" >&2
        status=1
    fi
done < <(find "${1:-k8s}" -type f \( -name '*.yaml' -o -name '*.yml' \) -not -path '*/rendered/*' -print0)
exit "$status"

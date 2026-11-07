#!/usr/bin/env bash
# Regenerates vex/index.json from the OpenVEX documents under vex/pkg/ (VEX repository spec v0.1,
# https://github.com/aquasecurity/vex-repo-spec). Every product PURL of a document becomes an index entry that points at
# the document, with version, subpath and qualifiers (except repository_url) removed.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../vex"

now="$(date -u +%FT%TZ)"
files=$(find pkg -type f -name '*.openvex.json' | sort)

if [ -z "$files" ]; then
  jq -n --arg now "$now" '{updated_at: $now, packages: []}' > index.json
  exit 0
fi

for f in $files; do
  jq --arg f "$f" '
    def strip:
      sub("#.*$"; "")
      | capture("^(?<base>[^@?]*)(@[^?]*)?(?<q>\\?.*)?$")
      | .base + ((.q // "") | [scan("repository_url=[^&]*")] | if length > 0 then "?" + .[0] else "" end);
    [.statements[].products[] | {id: ((."@id" // .identifiers.purl) | strip), location: $f}]' "$f"
done | jq -s --arg now "$now" '{updated_at: $now, packages: (add | unique_by(.id) | sort_by(.id))}' > index.json

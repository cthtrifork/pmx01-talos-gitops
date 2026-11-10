#!/usr/bin/env bash
# Prints the PR comment for one image: CRITICAL/HIGH counts of the old and new Trivy JSON reports.
# Usage: scan-images-summary.sh <oldref> <ref> <old.json> <new.json>   (a missing report shows as "?")
set -uo pipefail

count() { jq "[.Results[]?.Vulnerabilities[]? | select(.Severity == \"$2\")] | length" "$1" 2> /dev/null || echo "?"; }

echo "### \`${1:-new image}\` → \`$2\`"
echo
echo "| Severity | Old | New | |"
echo "|---|---:|---:|---|"
for severity in CRITICAL HIGH; do
    old=$(count "$3" "$severity") new=$(count "$4" "$severity") arrow="➖"
    [ "$new" -gt "$old" ] 2> /dev/null && arrow="⬆️"
    [ "$new" -lt "$old" ] 2> /dev/null && arrow="⬇️"
    echo "| $severity | $old | $new | $arrow |"
done
echo

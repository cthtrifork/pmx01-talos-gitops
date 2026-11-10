#!/usr/bin/env bash
# Writes the Markdown section for one scanned image: its CRITICAL/HIGH counts and, when there is a previous
# version, which vulnerabilities the change introduces and fixes.
# Usage: scan-images-section.sh <ref> <oldref|""> <files> <new.json> <old.json> <out.md>
# A missing report file means that scan failed.
set -euo pipefail

ref=$1 oldref=$2 files=$3 new=$4 old=$5 out=$6
tmp="$(mktemp -d)"
limit=50

# Flat list of {id,pkg,sev,inst,fix} from a Trivy JSON report.
flatten() {
    jq '[.Results[]?.Vulnerabilities[]? | {id: .VulnerabilityID, pkg: .PkgName, sev: .Severity, inst: .InstalledVersion, fix: .FixedVersion}]' "$1"
}
count() { jq --arg s "$2" '[.[] | select(.sev == $s)] | length' "$1"; }
badge() { echo "🔴 $(count "$1" CRITICAL) critical · 🟠 $(count "$1" HIGH) high"; }

# <summary title> <vulns.json>: a collapsible table, or nothing when the list is empty.
table() {
    local n
    n=$(jq length "$2")
    [ "$n" -gt 0 ] || return 0
    printf '<details><summary>%s %s</summary>\n\n| ID | Severity | Package | Installed | Fixed |\n|---|---|---|---|---|\n' "$n" "$1"
    jq -r --argjson l "$limit" 'sort_by(.sev, .id) | .[:$l][] | "| \(.id) | \(.sev) | `\(.pkg)` | \(.inst) | \(.fix) |"' "$2"
    [ "$n" -le "$limit" ] || printf '\n_… and %s more_\n' "$((n - limit))"
    printf '\n</details>\n\n'
}

{
    if [ -n "$oldref" ]; then echo "### \`$oldref\` → \`$ref\`"; else echo "### \`$ref\` (new)"; fi
    echo
    echo "Changed in: $files"
    echo
    if [ ! -s "$new" ]; then
        echo "⚠️ The new image could not be scanned (does the tag exist, and is the registry reachable?)."
        echo
    else
        flatten "$new" >"$tmp/new.json"
        echo "**New:** $(badge "$tmp/new.json")  "
        if [ -s "$old" ]; then
            flatten "$old" >"$tmp/old.json"
            echo "**Old:** $(badge "$tmp/old.json")"
            echo
            # Same CVE in the same package counts as unchanged.
            key='(.id + "|" + .pkg)'
            jq -n --slurpfile n "$tmp/new.json" --slurpfile o "$tmp/old.json" \
                "(\$o[0] | map($key)) as \$k | \$n[0] | map(select(($key) as \$x | \$k | index(\$x) | not))" >"$tmp/added.json"
            jq -n --slurpfile n "$tmp/new.json" --slurpfile o "$tmp/old.json" \
                "(\$n[0] | map($key)) as \$k | \$o[0] | map(select(($key) as \$x | \$k | index(\$x) | not))" >"$tmp/removed.json"
            table "🆕 introduced" "$tmp/added.json"
            table "✅ fixed" "$tmp/removed.json"
            if [ "$(jq length "$tmp/added.json")" = 0 ] && [ "$(jq length "$tmp/removed.json")" = 0 ]; then
                echo "No change in fixable CRITICAL/HIGH vulnerabilities."
                echo
            fi
        else
            [ -z "$oldref" ] || echo "**Old:** could not be scanned, no diff available."
            echo
            table "vulnerabilities" "$tmp/new.json"
        fi
    fi
} >"$out"

#!/usr/bin/env bash
# Renders the Markdown for one scanned image from its Trivy JSON reports.
# Usage: scan-images-section.sh <ref> <oldref|""> <files> <new.json> <old.json> <comment.md> <report.md>
#   comment.md  PR comment: CRITICAL/HIGH counts old vs new, and what the change introduces and fixes
#   report.md   full list of the new image's vulnerabilities, to upload as an artifact
# A missing or empty report file means that scan failed.
set -euo pipefail

ref=$1 oldref=$2 files=$3 new=$4 old=$5 comment=$6 report=$7
tmp="$(mktemp -d)"
limit=50

# Flat list of {id,pkg,sev,inst,fix,title} from a Trivy JSON report.
flatten() {
    jq '[.Results[]?.Vulnerabilities[]? | {id: .VulnerabilityID, pkg: .PkgName, sev: .Severity, inst: .InstalledVersion, fix: .FixedVersion, title: (.Title // "")}]' "$1"
}
count() { jq --arg s "$2" '[.[] | select(.sev == $s)] | length' "$1"; }

# <old> <new>: the change with an arrow, so an increase or decrease stands out.
delta() {
    local d=$(($2 - $1))
    if [ "$d" -gt 0 ]; then echo "⬆️ +$d"; elif [ "$d" -lt 0 ]; then echo "⬇️ $d"; else echo "➖ 0"; fi
}

# <title> <vulns.json> <max rows>: a table of vulnerabilities, worst first.
rows() {
    local n
    n=$(jq length "$2")
    [ "$n" -gt 0 ] || return 0
    printf '| ID | Severity | Package | Installed | Fixed |\n|---|---|---|---|---|\n'
    jq -r --argjson l "$3" 'sort_by(.sev, .id) | .[:$l][] | "| [\(.id)](https://avd.aquasec.com/nvd/\(.id | ascii_downcase)) | \(.sev) | `\(.pkg)` | \(.inst) | \(.fix) |"' "$2"
    [ "$n" -le "$3" ] || printf '\n_… and %s more_\n' "$((n - $3))"
}
details() { # <summary> <vulns.json>
    [ "$(jq length "$2")" -gt 0 ] || return 0
    printf '<details><summary>%s (%s)</summary>\n\n' "$1" "$(jq length "$2")"
    rows "$1" "$2" "$limit"
    printf '\n</details>\n\n'
}

has_new=false has_old=false
if [ -s "$new" ]; then
    has_new=true
    flatten "$new" >"$tmp/new.json"
fi
if [ -n "$oldref" ] && [ -s "$old" ]; then
    has_old=true
    flatten "$old" >"$tmp/old.json"
fi

{
    if [ -n "$oldref" ]; then echo "### \`$oldref\` → \`$ref\`"; else echo "### \`$ref\` (new)"; fi
    echo
    echo "Changed in: $files"
    echo
    if ! $has_new; then
        echo "⚠️ The new image could not be scanned (does the tag exist, and is the registry reachable?)."
    elif $has_old; then
        echo "Fixable vulnerabilities, old → new:"
        echo
        echo "| Severity | Old | New | Change |"
        echo "|---|---:|---:|---|"
        for sev in CRITICAL HIGH; do
            o=$(count "$tmp/old.json" "$sev") n=$(count "$tmp/new.json" "$sev")
            echo "| $sev | $o | $n | $(delta "$o" "$n") |"
        done
        echo
        # Same CVE in the same package counts as unchanged.
        key='(.id + "|" + .pkg)'
        jq -n --slurpfile n "$tmp/new.json" --slurpfile o "$tmp/old.json" \
            "(\$o[0] | map($key)) as \$k | \$n[0] | map(select(($key) as \$x | \$k | index(\$x) | not))" >"$tmp/added.json"
        jq -n --slurpfile n "$tmp/new.json" --slurpfile o "$tmp/old.json" \
            "(\$n[0] | map($key)) as \$k | \$o[0] | map(select(($key) as \$x | \$k | index(\$x) | not))" >"$tmp/removed.json"
        details "🆕 Introduced" "$tmp/added.json"
        details "✅ Fixed" "$tmp/removed.json"
    else
        echo "Fixable vulnerabilities:"
        echo
        echo "| Severity | Count |"
        echo "|---|---:|"
        for sev in CRITICAL HIGH; do echo "| $sev | $(count "$tmp/new.json" "$sev") |"; done
        [ -z "$oldref" ] || { echo; echo "The old image could not be scanned, so there is no comparison."; }
    fi
} >"$comment"

{
    echo "# Vulnerability report: \`$ref\`"
    echo
    echo "Fixable CRITICAL and HIGH vulnerabilities only."
    echo
    if $has_new; then
        echo "| Severity | Count |"
        echo "|---|---:|"
        for sev in CRITICAL HIGH; do echo "| $sev | $(count "$tmp/new.json" "$sev") |"; done
        echo
        if [ "$(jq length "$tmp/new.json")" -gt 0 ]; then rows all "$tmp/new.json" 100000; else echo "No fixable CRITICAL or HIGH vulnerabilities."; fi
    else
        echo "The image could not be scanned."
    fi
} >"$report"

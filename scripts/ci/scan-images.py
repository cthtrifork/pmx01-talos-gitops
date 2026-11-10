#!/usr/bin/env python3
"""Helpers for the "Scan changed images" workflow (.github/workflows/scan-images.yaml).

  extract  List the images a PR adds or re-tags in the k8s/ manifests, as a JSON matrix.
  compare  Compare two Trivy JSON reports (old image vs new image) into a small summary JSON.
  report   Aggregate the summaries of all scanned images into one Markdown PR comment.

Only needs PyYAML (pip install pyyaml) and git on PATH.
"""
import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path

import yaml

SCAN_ROOT = "k8s"
# Same exclusions as the Kyverno/policy test fixtures: they contain deliberately odd images.
EXCLUDE = re.compile(r"(^|/)(kyverno-policies|policies/tests)/")
MAX_MATRIX = 256  # GitHub's hard limit on matrix jobs
SEVERITIES = ("CRITICAL", "HIGH")
COMMENT_LIMIT = 60000  # GitHub caps a comment at 65536 characters


# ——— extract ———


class Loader(yaml.SafeLoader):
    """SafeLoader that tolerates a bare `=` scalar (YAML 1.1 "value" tag), which appears in rendered CRDs."""


Loader.add_constructor("tag:yaml.org,2002:value", lambda loader, node: node.value)


def git(*args, check=True):
    res = subprocess.run(["git", *args], capture_output=True, text=True)
    if check and res.returncode:
        sys.exit(f"git {' '.join(args)}: {res.stderr.strip()}")
    return res


def resolve_variables(ref):
    """Flux postBuild substitution: ${VAR:=default} / ${VAR:-default} -> default."""
    return re.sub(r"\$\{[^:}]+:[=-]([^}]*)\}", r"\1", ref)


def parse_ref(ref):
    """Split 'registry[:port]/repo[:tag][@sha256:...]' into its parts, or None if not scannable."""
    ref = resolve_variables(ref.strip())
    if not ref or "${" in ref or " " in ref or "/" not in ref:
        return None  # unresolved variable, or a name that is not a registry reference
    digest = None
    if "@" in ref:
        ref, digest = ref.split("@", 1)
    last = ref.rsplit("/", 1)[-1]
    tag = None
    if ":" in last:  # a ':' in an earlier segment is a registry port, not a tag
        ref, tag = ref.rsplit(":", 1)
    if tag is None and digest is None:
        tag = "latest"
    return {"repo": ref, "tag": tag, "digest": digest}


def ref_string(p):
    return p["repo"] + (f":{p['tag']}" if p["tag"] else "") + (f"@{p['digest']}" if p["digest"] else "")


def label(p):
    """Human readable: tag if there is one, else a short digest."""
    return p["tag"] or (p["digest"] or "")[:19]


def walk_images(node, name=None, out=None):
    """Collect every image reference in a parsed manifest, wherever it sits.

    Matches `image:` string fields (containers, initContainers, Prometheus/Alertmanager specs, ...) and
    env vars named *_IMAGE. This also covers CronJobs, VirtualMachine templates and custom resources
    without having to know their shape.
    """
    out = [] if out is None else out
    if isinstance(node, dict):
        name = node.get("name") if isinstance(node.get("name"), str) else name
        for key, value in node.items():
            if key == "image" and isinstance(value, str):
                add_image(out, name, value)
            elif key == "env" and isinstance(value, list):
                for env in value:
                    if (
                        isinstance(env, dict)
                        and isinstance(env.get("name"), str)
                        and env["name"].endswith("_IMAGE")
                        and isinstance(env.get("value"), str)
                    ):
                        add_image(out, f"{name}:{env['name']}", env["value"])
            else:
                walk_images(value, name, out)
    elif isinstance(node, list):
        for item in node:
            walk_images(item, name, out)
    return out


def add_image(out, name, ref):
    parsed = parse_ref(ref)
    if parsed:
        out.append({"name": name or "", **parsed})


def images_in(content):
    try:
        docs = [d for d in yaml.load_all(content, Loader=Loader) if isinstance(d, dict)]
    except yaml.YAMLError as e:
        print(f"::warning::YAML parse error: {e}", file=sys.stderr)
        return []
    found = []
    for doc in docs:
        kind = doc.get("kind", "")
        meta = doc.get("metadata")
        doc_name = meta.get("name", "") if isinstance(meta, dict) else ""
        for img in walk_images(doc):
            img["name"] = img["name"] or f"{kind}/{doc_name}"
            found.append(img)
    return found


def changed_files(base):
    out = git("diff", "--name-only", "--diff-filter=ACMR", f"{base}...HEAD", "--", SCAN_ROOT).stdout
    return [f for f in out.splitlines() if f.endswith((".yaml", ".yml")) and not EXCLUDE.search(f)]


def all_files():
    return sorted(
        str(p) for p in Path(SCAN_ROOT).rglob("*") if p.suffix in (".yaml", ".yml") and not EXCLUDE.search(str(p))
    )


def cmd_extract(args):
    # "all" (manual run) scans every image once, with nothing to compare against.
    files = all_files() if args.all else changed_files(args.base)
    results = {}  # new ref -> entry
    for path in files:
        try:
            new = images_in(Path(path).read_text())
        except (FileNotFoundError, UnicodeDecodeError):
            continue
        old = [] if args.all else images_in(git("show", f"{args.base}:{path}", check=False).stdout)
        for ni in new:
            same_repo = [oi for oi in old if oi["repo"] == ni["repo"]]
            if any(ref_string(oi) == ref_string(ni) for oi in same_repo):
                continue  # unchanged
            # Prefer the same container; otherwise any earlier use of this repository in the file.
            prev = next((oi for oi in same_repo if oi["name"] == ni["name"]), same_repo[0] if same_repo else None)
            key = ref_string(ni)
            entry = results.setdefault(
                key,
                {
                    "image": ni["repo"],
                    "tag": ni["tag"] or "",
                    "digest": ni["digest"] or "",
                    "ref": key,
                    "oldref": ref_string(prev) if prev else "",
                    "files": [],
                },
            )
            if path not in entry["files"]:
                entry["files"].append(path)

    matrix = sorted(results.values(), key=lambda e: e["ref"])
    for i, entry in enumerate(matrix):
        entry["id"] = i
        entry["files"] = entry["files"][:5]
    dropped = max(0, len(matrix) - MAX_MATRIX)
    if dropped:
        print(f"::warning::{dropped} images exceed the {MAX_MATRIX}-job matrix limit and are not scanned")
        matrix = matrix[:MAX_MATRIX]

    print(f"Scanned {len(files)} YAML files, found {len(matrix)} new or changed images:")
    for e in matrix:
        print(f"  {e['oldref'] or '(new)'} -> {e['ref']}")
    emit_output("images", json.dumps(matrix, separators=(",", ":")))
    emit_output("count", str(len(matrix)))


def emit_output(key, value):
    line = f"{key}={value}"
    path = os.environ.get("GITHUB_OUTPUT")
    if path:
        with open(path, "a") as f:
            f.write(line + "\n")


# ——— compare ———


def load_vulns(path):
    """Return {(id, pkg): vuln} from a Trivy JSON report, or None if missing/invalid."""
    try:
        data = json.loads(Path(path).read_text())
    except (FileNotFoundError, ValueError):
        return None
    vulns = {}
    for result in data.get("Results") or []:
        for v in result.get("Vulnerabilities") or []:
            vulns[(v["VulnerabilityID"], v.get("PkgName", ""))] = v
    return vulns


def counts(vulns):
    return {s: sum(1 for v in vulns.values() if v.get("Severity") == s) for s in SEVERITIES}


def slim(v):
    return {
        "id": v["VulnerabilityID"],
        "pkg": v.get("PkgName", ""),
        "severity": v.get("Severity", ""),
        "installed": v.get("InstalledVersion", ""),
        "fixed": v.get("FixedVersion", ""),
        "title": v.get("Title", ""),
    }


def cmd_compare(args):
    entry = json.loads(args.entry)
    new = load_vulns(args.new)
    old = load_vulns(args.old) if args.old else None
    summary = {k: entry.get(k) for k in ("id", "ref", "oldref", "files")}
    summary["new_scanned"] = new is not None
    summary["old_scanned"] = old is not None
    if new is not None:
        summary["new"] = counts(new)
        summary["vulns"] = [slim(v) for v in new.values()]
    if old is not None:
        summary["old"] = counts(old)
    if new is not None and old is not None:
        summary["added"] = [slim(new[k]) for k in sorted(new.keys() - old.keys())]
        summary["removed"] = [slim(old[k]) for k in sorted(old.keys() - new.keys())]
    Path(args.out).write_text(json.dumps(summary))


# ——— report ———

MARKER = "<!-- scan-images -->"
SEV_ORDER = {s: i for i, s in enumerate(SEVERITIES)}


def badge(c):
    return f"🔴 {c['CRITICAL']} critical · 🟠 {c['HIGH']} high"


def delta(new, old):
    out = []
    for s in SEVERITIES:
        d = new[s] - old[s]
        out.append(f"{s.lower()} {d:+d}" if d else f"{s.lower()} ±0")
    return ", ".join(out)


def vuln_table(vulns, limit=50):
    rows = sorted(vulns, key=lambda v: (SEV_ORDER.get(v["severity"], 9), v["id"]))
    lines = ["| ID | Severity | Package | Installed | Fixed |", "|---|---|---|---|---|"]
    for v in rows[:limit]:
        vid = f"[{v['id']}](https://avd.aquasec.com/nvd/{v['id'].lower()})" if v["id"].startswith("CVE-") else v["id"]
        lines.append(f"| {vid} | {v['severity']} | `{v['pkg']}` | {v['installed']} | {v['fixed']} |")
    if len(rows) > limit:
        lines.append(f"\n_… and {len(rows) - limit} more_")
    return "\n".join(lines)


def section(s):
    title = f"`{s['oldref']}` → `{s['ref']}`" if s["oldref"] else f"`{s['ref']}` (new)"
    lines = [f"### {title}", ""]
    if s["files"]:
        lines.append("Changed in: " + ", ".join(f"`{f}`" for f in s["files"]) + "\n")
    if not s["new_scanned"]:
        lines.append("⚠️ The new image could not be scanned (does the tag exist, and is the registry reachable?).\n")
        return "\n".join(lines)
    lines.append(f"**New:** {badge(s['new'])}")
    if s["old_scanned"]:
        lines.append(f"**Old:** {badge(s['old'])} ({delta(s['new'], s['old'])})")
    elif s["oldref"]:
        lines.append("**Old:** could not be scanned, no diff available.")
    lines.append("")
    if s["old_scanned"]:
        if s.get("added"):
            lines += [f"<details><summary>🆕 {len(s['added'])} introduced</summary>\n", vuln_table(s["added"]), "\n</details>\n"]
        if s.get("removed"):
            lines += [f"<details><summary>✅ {len(s['removed'])} fixed</summary>\n", vuln_table(s["removed"]), "\n</details>\n"]
        if not s.get("added") and not s.get("removed"):
            lines.append("No change in fixable CRITICAL/HIGH vulnerabilities.\n")
    elif s["vulns"]:
        lines += [f"<details><summary>{len(s['vulns'])} vulnerabilities</summary>\n", vuln_table(s["vulns"]), "\n</details>\n"]
    else:
        lines.append("No fixable CRITICAL/HIGH vulnerabilities.\n")
    return "\n".join(lines)


def cmd_report(args):
    summaries = [json.loads(p.read_text()) for p in sorted(Path(args.dir).glob("**/summary-*.json"))]
    # Worst regressions first: most newly introduced criticals, then highs.
    summaries.sort(
        key=lambda s: (
            -sum(1 for v in s.get("added", []) if v["severity"] == "CRITICAL"),
            -len(s.get("added", [])),
            s["ref"],
        )
    )
    head = [
        MARKER,
        "## Image vulnerability scan",
        "",
        f"{len(summaries)} new or changed image(s). Fixable CRITICAL/HIGH only; informational, does not block the PR.",
        "",
    ]
    if not summaries:
        head.append("No image changes detected.")
    parts = [section(s) for s in summaries]
    body = "\n".join(head) + "\n" + "\n".join(parts)
    if len(body) > COMMENT_LIMIT:
        body = body[:COMMENT_LIMIT].rsplit("\n", 1)[0] + "\n\n_… truncated, see the workflow run for the rest._"
    Path(args.out).write_text(body + "\n")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("extract")
    p.add_argument("--base", help="base commit/ref to diff against (merge-base is used)")
    p.add_argument("--all", action="store_true", help="scan every image under k8s/, no comparison")
    p.set_defaults(fn=cmd_extract)

    p = sub.add_parser("compare")
    p.add_argument("--entry", required=True, help="matrix entry as JSON")
    p.add_argument("--new", required=True)
    p.add_argument("--old")
    p.add_argument("--out", required=True)
    p.set_defaults(fn=cmd_compare)

    p = sub.add_parser("report")
    p.add_argument("--dir", required=True)
    p.add_argument("--out", required=True)
    p.set_defaults(fn=cmd_report)

    args = ap.parse_args()
    if args.cmd == "extract" and not args.all and not args.base:
        ap.error("extract needs --base or --all")
    args.fn(args)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Helpers for the "Scan changed images" workflow (.github/workflows/scan-images.yaml).

Lists the images a PR adds or re-tags in the k8s/ manifests, as a JSON matrix for the scan jobs. Everything
else (scanning, diffing, the PR comment) lives in the workflow and scripts/ci/scan-images-section.sh.

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
# Kyverno policy test fixtures contain deliberately odd images.
EXCLUDE = re.compile(r"(^|/)(kyverno-policies|policies/tests)/")
MAX_MATRIX = 256  # GitHub's hard limit on matrix jobs


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


def filter_files(paths):
    return [f for f in paths if f.startswith(f"{SCAN_ROOT}/") and f.endswith((".yaml", ".yml")) and not EXCLUDE.search(f)]


def all_files():
    return sorted(
        str(p) for p in Path(SCAN_ROOT).rglob("*") if p.suffix in (".yaml", ".yml") and not EXCLUDE.search(str(p))
    )


def cmd_extract(args):
    # "all" (manual run) scans every image once, with nothing to compare against.
    if args.all:
        files = all_files()
    else:
        # Whitespace-separated list from the changed-files action.
        files = filter_files(os.environ.get(args.files_env, "").split())
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


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", help="base commit to read the old manifests from")
    ap.add_argument("--files-env", default="CHANGED_FILES", help="env var holding the changed files")
    ap.add_argument("--all", action="store_true", help="scan every image under k8s/, no comparison")
    args = ap.parse_args()
    if not args.all and not args.base:
        ap.error("needs --base or --all")
    cmd_extract(args)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Lists the container images that changed in the given manifests, as the matrix for the scan jobs.

Usage: extract-images.py --base <commit> <file>...
       extract-images.py --all

For every file the images in the current version are compared with the version at <commit>. An image that
is not in the old version is reported with `oldref`, the image of the same repository it replaced ("" if
none). With --all every image under k8s/ is reported, without an oldref. Prints a JSON array of
{id, ref, oldref, files}, one entry per new image. Needs PyYAML.
"""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

import yaml

MAX_JOBS = 256  # GitHub's limit on matrix jobs


class Loader(yaml.SafeLoader):
    """SafeLoader that tolerates a bare `=` scalar, which appears in rendered CRDs."""


Loader.add_constructor("tag:yaml.org,2002:value", lambda loader, node: node.value)


def images_in(content):
    """All image references in a manifest: `image:` fields and env vars named *_IMAGE, at any depth."""
    refs = []

    def walk(node):
        if isinstance(node, list):
            for item in node:
                walk(item)
        elif isinstance(node, dict):
            for key, value in node.items():
                if key == "image" and isinstance(value, str):
                    refs.append(value)
                elif key == "env" and isinstance(value, list):
                    for var in value:
                        if isinstance(var, dict) and str(var.get("name")).endswith("_IMAGE"):
                            refs.append(var.get("value"))
                else:
                    walk(value)

    try:
        for doc in yaml.load_all(content, Loader=Loader):
            walk(doc)
    except yaml.YAMLError as e:
        print(f"::warning::cannot parse YAML: {e}", file=sys.stderr)

    refs = [resolve_defaults(r) for r in refs if isinstance(r, str)]
    # Only registry references: skip bare names such as "traefik" and values with unresolved variables.
    return [r for r in dict.fromkeys(refs) if "/" in r and "${" not in r and " " not in r]


def resolve_defaults(ref):
    """Flux substitution: ${VAR:=default} and ${VAR:-default} become default."""
    return re.sub(r"\$\{[^:}]+:[=-]([^}]*)\}", r"\1", ref.strip())


def repository(ref):
    """registry:5000/name:tag@sha256:... -> registry:5000/name"""
    path, _, name = ref.split("@")[0].rpartition("/")
    return f"{path}/{name.split(':')[0]}"


def old_content(base, path):
    shown = subprocess.run(["git", "show", f"{base}:{path}"], capture_output=True, text=True)
    return shown.stdout if shown.returncode == 0 else ""  # a new file has no old version


def changed_images(base, path):
    """(oldref, ref) for each image in the file that the base version did not have."""
    new = images_in(Path(path).read_text())
    old = images_in(old_content(base, path)) if base else []
    old_by_repo = {repository(ref): ref for ref in old}
    return [(old_by_repo.get(repository(ref), ""), ref) for ref in new if ref not in old]


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base", help="commit to compare the files with")
    parser.add_argument("--all", action="store_true", help="every image under k8s/, nothing to compare with")
    parser.add_argument("files", nargs="*")
    args = parser.parse_args()
    if args.all:
        files = sorted(str(p) for p in Path("k8s").rglob("*.y*ml"))
    elif args.base:
        files = [f for f in args.files if Path(f).is_file()]  # deleted files have nothing to scan
    else:
        parser.error("give --base and the changed files, or --all")

    # One entry per new image, remembering the files it appears in.
    matrix = {}
    for path in files:
        for oldref, ref in changed_images(args.base, path):
            entry = matrix.setdefault(ref, {"ref": ref, "oldref": oldref, "files": []})
            entry["oldref"] = entry["oldref"] or oldref
            entry["files"].append(path)

    entries = sorted(matrix.values(), key=lambda e: e["ref"])[:MAX_JOBS]
    for i, entry in enumerate(entries):
        entry["id"] = i
        entry["files"] = entry["files"][:5]
    print(json.dumps(entries))


if __name__ == "__main__":
    main()

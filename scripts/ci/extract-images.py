#!/usr/bin/env python3
"""Prints the images that one manifest adds or re-tags, as a JSON array of {file, ref, oldref} tuples.

Usage: extract-images.py [--base <commit>] <file>

The file is parsed as it is now and as it was at <commit>. An image counts when it is present now but not in
the old version; oldref is the image of the same repository it replaced, or "" when there was none. Without
--base every image in the file is reported as new. Prints [] when nothing changed. Needs PyYAML.
"""
import argparse
import json
import re
import subprocess
import sys

import yaml


class Loader(yaml.SafeLoader):
    """SafeLoader that tolerates a bare `=` scalar, which appears in rendered CRDs."""


Loader.add_constructor("tag:yaml.org,2002:value", lambda loader, node: node.value)


def resolve(ref):
    """Flux substitution: ${VAR:=default} / ${VAR:-default} -> default."""
    return re.sub(r"\$\{[^:}]+:[=-]([^}]*)\}", r"\1", ref.strip())


def repo(ref):
    """'registry:5000/name:tag@sha256:...' -> 'registry:5000/name'."""
    ref = ref.split("@", 1)[0]
    head, _, last = ref.rpartition("/")
    return (head + "/" if head else "") + last.split(":", 1)[0]


def scannable(ref):
    return "/" in ref and " " not in ref and "${" not in ref


def images(content):
    """Every image reference in the manifest: `image:` fields and env vars named *_IMAGE, at any depth."""
    found = []

    def walk(node):
        if isinstance(node, dict):
            for key, value in node.items():
                if key == "image" and isinstance(value, str):
                    found.append(resolve(value))
                elif key == "env" and isinstance(value, list):
                    found.extend(
                        resolve(e["value"])
                        for e in value
                        if isinstance(e, dict)
                        and str(e.get("name", "")).endswith("_IMAGE")
                        and isinstance(e.get("value"), str)
                    )
                else:
                    walk(value)
        elif isinstance(node, list):
            for item in node:
                walk(item)

    try:
        for doc in yaml.load_all(content, Loader=Loader):
            walk(doc)
    except yaml.YAMLError as e:
        print(f"::warning::YAML parse error: {e}", file=sys.stderr)
    return [r for r in dict.fromkeys(found) if scannable(r)]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", help="commit to read the old version of the file from")
    ap.add_argument("file")
    args = ap.parse_args()

    try:
        new = images(open(args.file).read())
    except FileNotFoundError:
        new = []
    old = []
    if args.base:
        shown = subprocess.run(["git", "show", f"{args.base}:{args.file}"], capture_output=True, text=True)
        old = images(shown.stdout) if shown.returncode == 0 else []  # a new file has no old version

    old_repos = {}
    for ref in old:
        old_repos.setdefault(repo(ref), ref)
    print(
        json.dumps(
            [
                {"file": args.file, "ref": ref, "oldref": old_repos.get(repo(ref), "")}
                for ref in new
                if ref not in old
            ]
        )
    )


if __name__ == "__main__":
    main()

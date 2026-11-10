#!/usr/bin/env python3
"""Prints the container images that one manifest adds or re-tags compared with its old version.

Usage: extract-images.py <old-file> <new-file>

Both files are read as Kubernetes YAML; the old one may be empty (a new file). An image of the new file that
the old file lacks is reported with `oldref`, the image of the same repository it replaced ("" if none).
Prints a JSON array of {file, oldref, ref}, [] when no image changed. Needs PyYAML.
"""
import argparse
import json
import re
import sys
from pathlib import Path

import yaml


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


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("old")
    parser.add_argument("new")
    args = parser.parse_args()

    old = images_in(Path(args.old).read_text())
    new = images_in(Path(args.new).read_text())
    old_by_repo = {repository(ref): ref for ref in old}
    print(
        json.dumps(
            [
                {"file": args.new, "oldref": old_by_repo.get(repository(ref), ""), "ref": ref}
                for ref in new
                if ref not in old
            ]
        )
    )


if __name__ == "__main__":
    main()

#!/usr/bin/env python3

import argparse
import os
import time
import requests
import yaml
from typing import Dict, List

BASE_URL = "https://datreeio.github.io/CRDs-catalog/"
K8S_SCHEMA_BASE_URL = "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master"
CACHE_DIR = os.path.join(os.path.expanduser("~"), ".cache", "datree_crds_catalog")
INDEX_FILENAME = "index.yaml"
INDEX_FILE_MAX_AGE_DAYS = 7
INDEX_FILEPATH = os.path.join(CACHE_DIR, INDEX_FILENAME)

DEFAULT_K8S_VERSION = "master"
DEFAULT_STRICT = True


def download_index_yaml():
    os.makedirs(CACHE_DIR, exist_ok=True)

    do_download = True
    if os.path.isfile(INDEX_FILEPATH):
        mtime = os.path.getmtime(INDEX_FILEPATH)
        if (time.time() - mtime) / (60 * 60 * 24) < INDEX_FILE_MAX_AGE_DAYS:
            do_download = False

    if do_download:
        url = BASE_URL + INDEX_FILENAME
        print(f"Downloading {url} to {INDEX_FILEPATH}...")
        response = requests.get(url, timeout=10)
        response.raise_for_status()
        with open(INDEX_FILEPATH, "wb") as f:
            f.write(response.content)


def load_index() -> Dict[str, List[Dict[str, str]]]:
    with open(INDEX_FILEPATH, "r") as f:
        return yaml.safe_load(f)


def k8s_kind_suffix(api_version: str) -> str:
    if api_version == "v1":
        return "-v1"

    group, version = api_version.rsplit("/", 1)
    return f"-{group}-{version}"


def k8s_schema_url(api_version: str, kind: str, k8s_version: str, strict: bool) -> str:
    strict_suffix = "-strict" if strict else ""
    schema_dir = f"{k8s_version}-standalone{strict_suffix}"
    resource_kind = kind.lower()
    kind_suffix = k8s_kind_suffix(api_version.lower())

    return f"{K8S_SCHEMA_BASE_URL}/{schema_dir}/{resource_kind}{kind_suffix}.json"


def url_exists(url: str) -> bool:
    try:
        response = requests.head(url, timeout=10, allow_redirects=True)
        return response.status_code == 200
    except requests.RequestException:
        return False


def find_crd_schema_url(api_version: str, kind: str, index_data: Dict[str, List[Dict[str, str]]]) -> str | None:
    api_version_l = api_version.lower()
    kind_l = kind.lower()

    for _, entries in index_data.items():
        for entry in entries:
            if (
                entry.get("apiVersion", "").lower() == api_version_l
                and entry.get("kind", "").lower() == kind_l
            ):
                filename = entry.get("filename")
                return BASE_URL + filename if filename else None

    return None


def find_schema_url(
    api_version: str,
    kind: str,
    index_data: Dict[str, List[Dict[str, str]]],
    k8s_version: str,
    strict: bool,
) -> str | None:
    print(f"Finding schema URL for apiVersion={api_version} kind={kind}...", end="")

    crd_url = find_crd_schema_url(api_version, kind, index_data)
    if crd_url:
        print(f"found CRD schema: {crd_url}")
        return crd_url

    native_url = k8s_schema_url(api_version, kind, k8s_version, strict)
    if url_exists(native_url):
        print(f"found native Kubernetes schema: {native_url}")
        return native_url

    print("not found.")
    return None


def annotate_file(
    file_path: str,
    index_data: Dict[str, List[Dict[str, str]]],
    k8s_version: str,
    strict: bool,
):
    with open(file_path, "r") as f:
        documents = list(yaml.safe_load_all(f))

    docs_out = []

    with open(file_path, "r") as f:
        raw_docs = f.read().split("\n---\n")

    for raw_doc in raw_docs:
        doc = raw_doc.strip()
        if not doc or doc == "---":
            continue

        lines = doc.splitlines()
        lines = [
            ln for ln in lines
            if not ln.strip().startswith("# yaml-language-server: $schema")
        ]

        data = yaml.safe_load("\n".join(lines)) or {}
        api_version = data.get("apiVersion")
        kind = data.get("kind")

        if api_version and kind:
            schema_url = find_schema_url(api_version, kind, index_data, k8s_version, strict)
            if schema_url:
                lines.insert(0, f"# yaml-language-server: $schema={schema_url}")

        docs_out.append("\n".join(lines))

    with open(file_path, "w") as f:
        #f.write("---\n")
        f.write("\n---\n".join(docs_out))
        f.write("\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("yaml_files", nargs="+", help="YAML files to annotate")
    parser.add_argument(
        "--k8s-version",
        default=DEFAULT_K8S_VERSION,
        help="Kubernetes schema version, e.g. v1.29.3 or master",
    )
    parser.add_argument(
        "--non-strict",
        action="store_true",
        help="Use non-strict Kubernetes schemas",
    )

    args = parser.parse_args()

    download_index_yaml()
    index_data = load_index()

    for yf in args.yaml_files:
        annotate_file(
            yf,
            index_data,
            args.k8s_version,
            strict=not args.non_strict,
        )


if __name__ == "__main__":
    main()

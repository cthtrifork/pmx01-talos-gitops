# Talos zot system extension

Runs [zot](https://zotregistry.dev/) as a Talos system extension.

## Build

```bash
docker buildx create --use --name talos-ext-builder || true

make push \
  REGISTRY=ghcr.io/casperthygesen \
  IMAGE=ghcr.io/casperthygesen/talos-zot-extension \
  ZOT_VERSION=2.1.14
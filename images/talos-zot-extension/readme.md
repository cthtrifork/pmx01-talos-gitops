# Talos zot system extension

Runs [zot](https://zotregistry.dev/) as a Talos system extension.

## Build

```bash
docker buildx create --use --name talos-ext-builder || true

make push \
  REGISTRY=ghcr.io/casperthygesen \
  IMAGE=ghcr.io/casperthygesen/talos-zot-extension \
  ZOT_VERSION=2.1.14
  
talosctl patch mc --nodes <NODE_IP> --patch @zot-config-extension.yaml

talosctl get extensions --nodes <NODE_IP>
talosctl logs zot --nodes <NODE_IP>
curl http://<NODE_IP>:5000/v2/
```
  
```yam
customization:
  systemExtensions:
    extraExtensions:
      - image: ghcr.io/casperthygesen/talos-zot-extension:2.1.14
```

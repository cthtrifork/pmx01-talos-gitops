# Registry mirrors on the nodes

Image pulls on a node go containerd, then Spegel, then zot, then the upstream registry.

- containerd reads `/etc/cri/conf.d/hosts/<registry>/hosts.toml`. Talos writes these files from `patches/11-zot-mirrors.yaml`
  (seven registries, each pointing at its zot cache over https, with the root CA pinned). Spegel's init container
  (`k8s/infra/registry-system/spegel/`) saves them to `_backup` once and rewrites each file as Spegel first, then the saved hosts.
- Spegel answers from the other nodes. A miss moves on to zot (`192.168.1.240:5000`), which fetches from the internet when it
  has to and keeps a copy. Containerd goes straight to the upstream registry only when zot fails.
- The certificate zot serves comes from `certificate-cluster-issuer` and lists `192.168.1.240`. The CA certificate in the patch
  is the public part of `portableinfo-root-ca` (valid until 2036-11-07); replace it there if the CA is ever issued again.

## After changing a Talos registry patch

Talos rewrites the seven files on every change of its registry or CRI configuration (also at boot) and deletes everything else
in that directory, including Spegel's `_backup`. Spegel then drops out of the chain until its init container runs again:

1. Apply the patch to the nodes.
2. Restart Spegel, one pod at a time: `kubectl -n registry-system delete pod spegel-<id>`.
3. Check the init log of each new pod: `kubectl -n registry-system logs spegel-<id> -c configuration` shows
   `backing up containerd host configuration` and `prepending to existing containerd mirror configuration` for the seven registries.

## Checking that pulls take the whole path

- zot: `zot_repo_downloads_total` rises for a `ghcr-cache/…` repository that only kubelet pulls, for example
  `ghcr-cache/cthtrifork/talos-gitops/vex-cache`.
- Spegel: `spegel_mirror_requests_total` rises for the same registry (hit or miss). Without Spegel in the chain only the zot counter moves.
- A pull that skips zot shows no zot download and the image still arrives from the internet, so a failure here does not break pulls.

## Going back to Spegel alone

Revert the Spegel values (`mirroredRegistries: []`, `prependExisting: false`), then restart the Spegel pods. Spegel writes one
`_default` mirror again and zot is used by Trivy only.

# VEX repository

Our own VEX statements, following the [VEX repository specification](https://github.com/aquasecurity/vex-repo-spec)
and the layout of the [Trivy VEX Hub](https://github.com/aquasecurity/vexhub). Trivy reads it as the repository
`homelab-vexhub` (`k8s/infra/security-system/trivy-operator/vex/repository.yaml`), from the `vex-cache` image
(`images/vex-cache`) that contains this directory, and filters the findings of every scan with it.

```text
vex/
├── vex-repository.json     manifest: spec version, update interval, where the archive is
├── index.json              generated: package PURL -> VEX document (scripts/vex-index.sh)
└── pkg/<type>/<namespace>/<name>/<anything>.openvex.json
```

Statements marked in Dependency-Track are exported here by `k8s/infra/security-system/dependency-track/vex-export.yaml` (documents with author `Dependency-Track`; do not edit those by hand).

## Adding a statement

1. Write an [OpenVEX](https://github.com/openvex/spec) document under `pkg/`, for an image at
   `pkg/oci/<registry>/<repository>/oci.openvex.json`, for example `pkg/oci/ghcr.io/aquasecurity/trivy/`. Every product
   needs a PURL. For an image: `pkg:oci/trivy?repository_url=ghcr.io%2Faquasecurity%2Ftrivy`; version and qualifiers go
   in the statements' `products`, not in the path.
2. Put all statements of a product into one document: only one document per PURL is distributed.
3. Run `scripts/vex-index.sh` (needs `jq`) and commit `index.json` with the document.

The scan jobs read the `vex-cache` image (`images/vex-cache`), built from this directory and the Trivy VEX hub by
`.github/workflows/build-vex-cache.yaml`. The workflow runs when `vex/` or the image changes on `main`, and by hand; the Trivy
VEX hub part only changes when it runs. A new statement reaches a scan after that build, plus the next report regeneration
(`scannerReportTTL`).
Statements mark a CVE as `not_affected`, `fixed` or `under_investigation`; Trivy only hides the first two.

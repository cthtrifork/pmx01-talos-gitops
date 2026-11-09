# VEX repository

Our own VEX statements, following the [VEX repository specification](https://github.com/aquasecurity/vex-repo-spec)
and the layout of the [Trivy VEX Hub](https://github.com/aquasecurity/vexhub). Trivy reads it as the repository
`homelab-vexhub` (`k8s/infra/security-system/trivy-operator/vex/repository.yaml`), through the in-cluster mirror
`k8s/infra/security-system/vex-hub/`, and filters the findings of every scan with it.

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

The mirror (`k8s/infra/security-system/vex-hub/`) fetches the repository every 6 hours. The scan jobs read the VEX cache image
built from this directory and the Trivy VEX hub (`images/vex-cache`, `.github/workflows/build-vex-cache.yaml`), which is
rebuilt when `vex/` changes on `main` or when the workflow is run by hand. A new statement reaches a scan after that build,
plus the next report regeneration (`scannerReportTTL`).
Statements mark a CVE as `not_affected`, `fixed` or `under_investigation`; Trivy only hides the first two.

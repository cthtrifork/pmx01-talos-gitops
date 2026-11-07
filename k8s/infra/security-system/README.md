# security-system

- `trivy-operator/`: vulnerability scanning of running workloads. The trivy server pulls its vulnerability and Java
  databases (and policy bundle) from the in-cluster zot registry (`ghcr-cache/aquasecurity/trivy-db` etc.), which syncs
  them on demand from `ghcr.io`. Scanned images are also pulled through zot via `trivy.registry.mirror`.
- `dependency-track/`: Dependency-Track 5 with a single-instance CNPG PostgreSQL (`dependency-track-db`).

## One manual step: the Dependency-Track KEK

The chart does not generate the key encryption key, and the release reads it from the Secret `dependency-track-kek`
(key `kek`). Create it SOPS-encrypted like the other secrets, e.g. `k8s/infra/security-system/dependency-track/kek.enc.yaml`
(add it to that directory's `kustomization.yaml`), with the value from `openssl rand -base64 32`. Until it exists the
api-server pods stay in `CreateContainerConfigError`.

## Access

LAN only, through MetalLB (`dependency-track/service-lb.yaml`): the UI is at `http://192.168.1.241:8080` and the
frontend calls the api-server at `http://192.168.1.242:8080`. Both are plain HTTP and not routed through Traefik or the
Cloudflare tunnel. Check the two addresses are free in the pool (`192.168.1.221-254`) before merging.

## KEV: turning bare CVEs into "is this exploited?"

Trivy itself has no KEV data, and trivy-operator reports (and its metrics) only carry severity/CVSS. KEV enrichment
comes from Dependency-Track 5, which has two relevant pieces, both configured at runtime in its database (Administration
UI or REST API), not through Helm values, so they are not in Git:

1. **KEV data sources** (Administration): CISA KEV is built in and enabled by default; ENISA KEV and VulnCheck KEV
   (needs a free API token) can be enabled alongside it. Dependency-Track then flags findings whose CVE (or alias) is
   known exploited, with the required action and a known-ransomware flag. It needs egress to
   `www.cisa.gov` (feed `known_exploited_vulnerabilities.json`).
2. **Trivy vulnerability analyzer** (Administration, vulnerability analysis): enable it with API URL
   `http://trivy-service.security-system.svc.cluster.local:4954`. It reuses the trivy server that trivy-operator runs,
   so the zot-mirrored database is used and the analyzer needs no internet access. The server has no token set.

Findings come from the projects/SBOMs uploaded to Dependency-Track. Nothing uploads the cluster's SBOMs yet
(trivy-operator `SbomReport` -> `/api/v1/bom`); that needs an API key and a small uploader and is a follow-up.

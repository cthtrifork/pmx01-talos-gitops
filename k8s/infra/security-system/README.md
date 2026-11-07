# security-system

- `trivy-operator/`: vulnerability scanning of running workloads, in client/server mode with one shared
  `trivy-service`. The trivy server pulls its vulnerability and Java databases (and the policy bundle) from the in-cluster
  zot registry (`ghcr-cache/aquasecurity/trivy-db` etc.), which syncs them on demand from `ghcr.io`. Scanned images are
  also pulled through zot via `trivy.registry.mirror`.
- `dependency-track/`: Dependency-Track 5 with a single-instance CNPG PostgreSQL (`dependency-track-db`).

## VEX hub, ignore file and ignore policy

Trivy filters its findings with three inputs, all generated into ConfigMaps by the `configMapGenerator` in
`trivy-operator/kustomization.yaml` (fixed names, no hash suffix) from files you edit in Git, and mounted into the
trivy server and the scan jobs:

- `trivy-operator/vex/repository.yaml` (`vex-repository-config`): the VEX repositories, by default the Trivy VEX hub
  (`github.com/aquasecurity/vexhub`). The scan jobs refresh it into their cache at start, so they need egress to
  `github.com`. VEX statements mark CVEs as not exploitable in a product.
- `trivy-operator/ignore/trivyignore.yaml` (`trivy-ignorefile`): accepted findings, with an expiry.
- `trivy-operator/ignore/ignore-policy.rego` (`trivy-ignorepolicy`): a Rego policy, `ignore` is false by default.

They are wired through the `configFile` in `trivy-operator/helm-release.yaml` (`vex`, `ignorefile`, `ignore-policy`).
A change takes effect for the next scan job; reports are regenerated after `scannerReportTTL`.

## Zot CA trust

zot's certificate comes from `certificate-cluster-issuer`. `trivy-operator/ca-certificate.yaml` issues a throwaway
certificate in this namespace only to get the root CA as `ca.crt`; the trivy server, the scan jobs and the operator
mount it as `/etc/ssl/certs/zot-ca.crt` (Go reads extra files there), so no `dbRepositoryInsecure` is needed.

## Network policies

No policy in the repo or the cluster restricts `security-system` (the existing ones cover claude-system, harper-pad,
flux-system and the Prometheus operator). If a default-deny is added later, allow the trivy server and scan jobs to zot
(5000) and `github.com` (443), and clients to `trivy-service` (4954).

## The Dependency-Track KEK

The chart does not generate the key encryption key. `dependency-track/kek-job.yaml` creates the Secret
`dependency-track-kek` (key `kek`, random 32 bytes) once, if it is missing, and never overwrites it. The key is therefore
not in Git: if the Secret is lost, everything Dependency-Track stored encrypted in its database becomes unreadable. Back it
up (`kubectl -n security-system get secret dependency-track-kek`, needs Secret read access) if that matters.
Until the Job has run, the api-server pods wait in `CreateContainerConfigError` and then start on their own.

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

Findings come from the projects/SBOMs uploaded to Dependency-Track. `dependency-track/sbom-uploader.yaml` is a CronJob
(every 6 hours) that uploads trivy-operator's `SbomReport`s, one project per image. It needs an API key, created by hand:

1. In the UI, Administration > Access Management > Teams: create a team (or use Automation) with the permissions
   `BOM_UPLOAD` and `PROJECT_CREATION_UPLOAD`, and create an API key for it.
2. `kubectl -n security-system create secret generic dependency-track-api-key --from-literal=api-key=<key>`
   (kept out of Git; or add it SOPS-encrypted).

Until the Secret exists the CronJob fails with a message saying so.

Network policies: if a default-deny is added to `security-system`, also allow Dependency-Track to Postgres (5432),
trivy-service (4954) and `www.cisa.gov` (443), and the uploader to dependency-track-api-server (8080) and the Kubernetes API.

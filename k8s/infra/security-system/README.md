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
  (`github.com/aquasecurity/vexhub`), read through the in-cluster mirror `vex-hub/` (Redpanda Connect: fetches the
  hub every 6 hours and serves the manifest and tarball from memory). The scan jobs refresh it into their cache at
  start; they no longer need egress to `github.com`, but fail until the mirror's first refresh has succeeded. VEX statements mark CVEs as not exploitable in a product.
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
comes from Dependency-Track 5, which has two relevant pieces. Both are stored in its database, so Helm values cannot set
them; `dependency-track/config-job.yaml` (a CronJob every 20 minutes) applies them through the REST API:

1. **KEV data sources**: CISA KEV and ENISA KEV are enabled. Dependency-Track then flags findings whose CVE (or alias) is
   known exploited, with the required action and a known-ransomware flag. It needs egress to `www.cisa.gov` and
   `raw.githubusercontent.com`. VulnCheck KEV needs a token and is not configured.
2. **Trivy vulnerability analyzer**: enabled with API URL `http://trivy-service.security-system.svc.cluster.local:4954`.
   It reuses the trivy server that trivy-operator runs, so the zot-mirrored database is used and the analyzer needs no
   internet access. The server has no token set.

The settings live in the ConfigMap `dependency-track-config` (`extensions.json`: extension point, extension name, config)
and are merged over the current settings; add entries there for other analyzers or data sources. Notifications, policies and
the NVD/GitHub/OSV sources are not managed (UI only, defaults kept).

The same Job bootstraps access. Everything here is a **dry run until you seed the Secrets**: the jobs log what they would do
and exit 0, `sbom-hook` logs `DRY RUN: would upload ...` instead of calling Dependency-Track, and nothing is changed.

1. Seed the admin password you want: `kubectl -n security-system create secret generic dependency-track-admin
   --from-literal=username=admin --from-literal=password=<password>`. On its next run (every 20 minutes) `config-job.yaml`
   sets it on the built-in `admin`, creates the team `sbom-uploader` with `BOM_UPLOAD` and `PROJECT_CREATION_UPLOAD` and the team
   `vex-exporter`, stores their API keys in `dependency-track-api-key` and `dependency-track-vex-api-key` (existing Secrets,
   such as a key created by hand, are left alone) and applies the settings above. Permissions are only added.
2. Restart `sbom-hook` once the key Secret exists (it reads the key at start; `kubectl -n security-system rollout restart
   deploy/sbom-hook`); the uploader CronJob picks the key up on its next run.
3. For the VEX export, seed the GitHub token (below). Without it the export only prints the documents it would write.

## Exporting Dependency-Track decisions to the VEX repository

`dependency-track/vex-export.yaml` is a daily CronJob that turns the analysis decisions made in Dependency-Track (state
*not affected*, *false positive* or *resolved* on a finding) into OpenVEX documents in `vex/pkg/oci/<registry>/<repository>/`
of this repository, regenerates `vex/index.json` and opens a pull request from the branch `dependency-track/vex-export`.
Merge it and Trivy hides those findings (through `vex-hub/`). It only changes files under `vex/`, never pushes to `main`,
and only deletes documents whose author is `Dependency-Track`. Findings on components without a PURL are skipped.

It needs the Secret `dependency-track-vex-github` (key `token`), seeded by hand: a fine-grained personal access token (or
GitHub App installation token) for this repository only, with Contents and Pull requests read/write.
`kubectl -n security-system create secret generic dependency-track-vex-github --from-literal=token=<token>`.
The API key comes from `config-job.yaml` (team `vex-exporter`). Trivy identifies a scanned image by the PURL
`pkg:oci/<name>@<digest>?repository_url=<registry>/<repository>` and matches a statement only if the version is the same, so
the SBOM uploads (`sbom-hook/`, `dependency-track/sbom-uploader.yaml`) version each Dependency-Track project by the image
digest, not the tag. The export skips projects whose version is not a digest. Decisions made on projects created before
that change (versioned by tag) stay in Dependency-Track but are not exported; repeat them on the digest project, or
delete the old projects. A moved tag creates a new project per digest, so old ones pile up until retention removes them.

Findings come from the projects/SBOMs uploaded to Dependency-Track. `dependency-track/sbom-uploader.yaml` is a CronJob
(daily) that uploads trivy-operator's `SbomReport`s, one project per image, with that API key; until the Secret exists it
lists what it would upload.

Network policies: if a default-deny is added to `security-system`, also allow Dependency-Track to Postgres (5432),
trivy-service (4954) and `www.cisa.gov` (443), and the uploader to dependency-track-api-server (8080) and the Kubernetes API.

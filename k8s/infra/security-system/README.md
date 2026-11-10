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

- `trivy-operator/vex/repository.yaml` (`vex-repository-config`): the VEX repositories Trivy loads, the Trivy VEX hub
  (`default`) and our `vex/` directory (`homelab-vexhub`). The data is in the `vex-cache` image (`images/vex-cache`, built
  by `.github/workflows/build-vex-cache.yaml` from the hub and `vex/`), which the scan jobs mount read-only at
  `<cache-dir>/vex`. `skip-vex-repo-update` keeps Trivy from downloading anything at scan time, so the URLs in the file
  are not fetched. The image is rebuilt when `vex/` changes on `main` and when the workflow is run by hand; the Trivy VEX hub
  part only changes when it runs. VEX statements mark CVEs as not exploitable in a product.
- `vex-hub/` (Redpanda Connect) serves the hub and `vex/` over HTTP inside the cluster, fetching them from GitHub every 6
  hours. The URLs in `repository.yaml` point at it; nothing in the cluster fetches from it.
- `trivy-operator/ignore/trivyignore.yaml` (`trivy-ignorefile`): accepted findings, with an expiry.
- `trivy-operator/ignore/ignore-policy.rego` (`trivy-ignorepolicy`): a Rego policy, `ignore` is false by default.

They are wired through the `configFile` in `trivy-operator/helm-release.yaml` (`vex`, `ignorefile`, `ignore-policy`).
A change takes effect for the next scan job; reports are regenerated after `scannerReportTTL`.

## Zot CA trust

zot's certificate comes from `certificate-cluster-issuer`. trust-manager's `portableinfo-ca-bundle` Bundle
(`k8s/infra/post/trust-manager`) puts the root CA in a ConfigMap of that name (key `ca-bundle.crt`) in every namespace; the
trivy server, the scan jobs and the operator mount it as `/etc/ssl/certs/zot-ca.crt` (Go reads extra files there), so no
`dbRepositoryInsecure` is needed. Reloader restarts the trivy server and the operator when the bundle changes (they read
it at start); a scan job reads it when it starts.

## Network policies

`networkpolicy.yaml` denies all traffic in both directions for the namespace and then allows, per workload, what Hubble showed
it needs (one policy each for Dependency-Track, `sbom-hook`, `vex-hub`, the trivy operator, the trivy server, the scan jobs and
the Dependency-Track jobs). The scan jobs may reach the trivy server (4954), zot (5000), `vex-hub` (4195) and the internet;
clients reach `trivy-service` (4954).

## The Dependency-Track KEK

The chart does not generate the key encryption key. `dependency-track/kek-job.yaml` creates the Secret
`dependency-track-kek` (key `kek`, random 32 bytes) once, if it is missing, and never overwrites it. The key is therefore
not in Git: if the Secret is lost, everything Dependency-Track stored encrypted in its database becomes unreadable. Back it
up (`kubectl -n security-system get secret dependency-track-kek`, needs Secret read access) if that matters.
Until the Job has run, the api-server pods wait in `CreateContainerConfigError` and then start on their own.

## Access

LAN only, through MetalLB (`dependency-track/service-lb.yaml`): the UI is at `http://192.168.1.241:8080` and the
frontend calls the api-server at `https://dtrackapi-portableinfo.workload.eu-central-1.omni.siderolabs.io` (the Omni
exposer address of `dependency-track-api-server-lb`; an HTTPS page, such as the Omni-exposed UI, cannot call a plain
HTTP API: mixed content). The LAN addresses are plain HTTP and not routed through Traefik or the Cloudflare tunnel. Check the two addresses are free in the pool (`192.168.1.221-254`) before merging.

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

The same Job bootstraps access. Everything here is a **dry run until you seed the admin Secret**: the Job logs what it
would do and exits 0, while `sbom-hook` logs `DRY RUN: would upload ...` instead of calling Dependency-Track.

1. Seed the admin password you want: `kubectl -n security-system create secret generic dependency-track-admin
   --from-literal=username=admin --from-literal=password=<password>`. On its next run (every 20 minutes) `config-job.yaml`
   sets it on the built-in `admin`, creates the team `sbom-uploader` with `BOM_UPLOAD` and `PROJECT_CREATION_UPLOAD` and the team
   `vex-exporter`, stores their API keys in `dependency-track-api-key` and `dependency-track-vex-api-key` (existing Secrets,
   such as a key created by hand, are left alone) and applies the settings above. Permissions are only added.
2. Restart `sbom-hook` once the key Secret exists (it reads the key at start): `kubectl -n security-system rollout restart
   deploy/sbom-hook`.
3. For the VEX export, seed the GitHub token (below). Without it the export only prints the documents it would write.

## Exporting Dependency-Track decisions to the VEX repository

`dependency-track/vex-export.yaml` is a daily CronJob that turns the analysis decisions made in Dependency-Track (state
*not affected*, *false positive* or *resolved* on a finding) into OpenVEX documents in `vex/pkg/oci/<registry>/<repository>/`
of this repository, regenerates `vex/index.json` and opens a pull request from the branch `dependency-track/vex-export`.
Merge it and the `vex-cache` image is rebuilt from `vex/`; the next scan job hides those findings. It only changes files under `vex/`, never pushes to `main`,
and only deletes documents whose author is `Dependency-Track`. Findings on components without a PURL are skipped.

It needs the Secret `dependency-track-vex-github` (key `token`), seeded by hand: a fine-grained personal access token (or
GitHub App installation token) for this repository only, with Contents and Pull requests read/write.
`kubectl -n security-system create secret generic dependency-track-vex-github --from-literal=token=<token>`.
The API key comes from `config-job.yaml` (team `vex-exporter`). Trivy identifies a scanned image by the PURL
`pkg:oci/<name>@<digest>?repository_url=<registry>/<repository>` and matches a statement only if the version is the same, so
`sbom-hook/` versions each Dependency-Track project by the image digest, not the tag. The export skips projects whose
version is not a digest. Decisions made on projects created before
that change (versioned by tag) stay in Dependency-Track but are not exported; repeat them on the digest project, or
delete the old projects. A moved tag creates a new project per digest, so old ones pile up until retention removes them.

Findings come from the projects/SBOMs uploaded to Dependency-Track. `sbom-hook/` (Redpanda Connect) receives every
`SbomReport` that trivy-operator produces or regenerates through its webhook (`operator.webhookBroadcastURL`) and uploads
it, one project per image; reports are regenerated after `scannerReportTTL`, so nothing needs a second upload path. It
uses the API key created by `config-job.yaml`; until the Secret exists its uploads fail with a 401.

The default-deny network policies allow Dependency-Track to Postgres (5432),
trivy-service (4954), and the vulnerability feeds; allow `sbom-hook`, `dependency-track-config` and
`dependency-track-vex-export` to reach `dependency-track-api-server` (8080). The VEX exporter also needs HTTPS egress.

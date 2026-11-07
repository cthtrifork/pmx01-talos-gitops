# security-system

- `trivy-operator/`: vulnerability scanning of running workloads, in client/server mode with one shared
  `trivy-service`. The trivy server pulls its vulnerability and Java databases (and the policy bundle) from the in-cluster
  zot registry (`ghcr-cache/aquasecurity/trivy-db` etc.), which syncs them on demand from `ghcr.io`. Scanned images are
  also pulled through zot via `trivy.registry.mirror`.

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

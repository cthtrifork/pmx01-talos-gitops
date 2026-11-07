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

Dependency-Track is not exposed outside the cluster. Port-forward the api-server and frontend services
(`kubectl -n security-system get svc`); the frontend is configured with `apiBaseUrl: http://localhost:8080`.

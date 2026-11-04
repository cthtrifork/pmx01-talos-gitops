# claude-system

A Claude Code **self-hosted environment runner** with read-only cluster access and an egress whitelist.
The runner connects out to Anthropic, claims sessions you start at claude.ai/code and runs them in this
pod as the `claude` ServiceAccount. Docs: <https://code.claude.com/docs/en/self-hosted-environments>.

Requires a Team or Enterprise plan (public beta), Claude Code 2.1.224 or later, and an Owner to enable
"Allow self-hosted environments" in admin settings.

| File | What |
|---|---|
| `rbac.yaml` | ServiceAccount `claude`; built-in `view` (no Secrets) plus read-only access to nodes, storage, CRDs and the CRD groups in use. No write verbs, no `pods/exec` |
| `networkpolicy.yaml` | Cilium default deny, then DNS, the Kubernetes API and TCP 443 to the hosts the runner needs |
| `deployment.yaml` | The runner, one session per pod (`--capacity 1`), non-root, read-only root filesystem |
| `../../../images/claude-runner/` | Image: Anthropic publishes none, so this wraps the `claude` binary (checksum verified) plus `kubectl` and a git credential helper |

The Deployment is `replicas: 0` until the image is published and the two Secrets below exist.

## 1. Build the image

Run the **Publish to Github** workflow (`workflow_dispatch`, tag prefix `rc`) or push a `v*` tag. It
publishes `ghcr.io/cthtrifork/pmx01-talos-gitops/claude-runner:<tag>`. Put that tag, pinned by digest,
in `deployment.yaml`. Bump `CLAUDE_CODE_VERSION` and `CLAUDE_CODE_SHA256` together in the Dockerfile
(checksum: `https://downloads.claude.ai/claude-code-releases/<version>/manifest.json`, `linux-x64`).

## 2. Authorize the runner with an environment key

1. Open <https://claude.ai/admin-settings/cloud-environments>, under **Self-hosted environments** select
   **New**, name it, **Create**.
2. On the second step select **Copy environment key**. It is shown once and expires after 365 days.
3. Create the Secret yourself. `read -rs` keeps the key out of your shell history and screen:

```sh
read -rsp 'Environment key: ' ENV_KEY; echo
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: claude-runner-environment-secret
  namespace: claude-system
type: Opaque
stringData:
  environment-secret: ${ENV_KEY}
EOF
unset ENV_KEY
```

Rotate by creating a new key on the environment's **Configuration** tab, re-running the command, then
revoking the old key. Reloader restarts the runner when the Secret changes.

## 3. GitHub token for git

Create a **fine-grained personal access token** limited to the repository the runner works on
(`cthtrifork/pmx01-talos-gitops`) with **Contents: read and write** and **Pull requests: read and write**,
and a short expiry. The image has no `gh` CLI; git pushes use the token for `github.com` only, through
the credential helper that reads `/etc/github/token`.

```sh
read -rsp 'GitHub token: ' GITHUB_TOKEN; echo
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: claude-github-token
  namespace: claude-system
type: Opaque
stringData:
  token: ${GITHUB_TOKEN}
EOF
unset GITHUB_TOKEN
```

Both Secrets are created by hand and are not in Git. Flux only prunes what it applied, so it leaves them
alone.

## 4. Start it

Set `replicas: 1` in `deployment.yaml` (Flux reverts a manual `kubectl scale`), then:

```sh
kubectl -n claude-system logs deploy/claude-runner
kubectl -n claude-system get pods
```

Pick the environment at <https://claude.ai/code>.

## Check what the pod can and cannot do

```sh
SA=system:serviceaccount:claude-system:claude
kubectl auth can-i list pods -A --as=$SA                          # yes
kubectl auth can-i list clusters.postgresql.cnpg.io -A --as=$SA   # yes
kubectl auth can-i get secrets -A --as=$SA                        # no
kubectl auth can-i delete pods -A --as=$SA                        # no
kubectl auth can-i create pods/exec -A --as=$SA                   # no
```

Anything the pod reads from the cluster (logs, events, annotations) is input Claude acts on, so keep
the GitHub token scoped to one repository. The `claude` ServiceAccount cannot read Secrets through the
API, but the GitHub token and environment key are mounted in this pod, so treat the pod as holding both.

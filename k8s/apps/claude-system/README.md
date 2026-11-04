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
| `github-app.yaml` | CronJob that mints a one-hour GitHub App installation token into `claude-github-token` (scripts in `github-app/`) |
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

## 3. GitHub access for git

The runner reads a token from the Secret `claude-github-token` and hands it to git for `github.com` only
(no `gh` CLI in the image, so it can push branches but not open PRs). Pick one way to fill that Secret.

### Option A (recommended): a GitHub App, token refreshed every 30 minutes

`github-app.yaml` runs a CronJob that signs a JWT with the App's private key and exchanges it for a
one-hour installation token, narrowed to this repository with Contents and Pull requests read/write. The
runner holds only that short-lived token.

1. Create the App (GitHub: Settings > Developer settings > GitHub Apps > New). Repository permissions:
   **Contents: read and write**, **Pull requests: read and write**, **Metadata: read**. Leave everything
   else off. Without **Workflows** the App cannot push changes under `.github/workflows/`; leave it off
   unless you want that. Untick "Webhook active". Never make it a bypass actor in a ruleset.
2. Generate a private key (downloads a `.pem`), install the App on this repository only, and note the
   **App ID** (App settings page) and the **installation ID** (the number at the end of the installation's
   URL, `https://github.com/settings/installations/<id>`).
3. Create the credentials Secret yourself, in the same style as the others:

```sh
read -rp 'App ID: ' APP_ID
read -rp 'Installation ID: ' INSTALLATION_ID
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: claude-github-app
  namespace: claude-system
type: Opaque
stringData:
  app-id: "${APP_ID}"
  installation-id: "${INSTALLATION_ID}"
  private-key.pem: |
$(sed 's/^/    /' /path/to/app-private-key.pem)
EOF
unset APP_ID INSTALLATION_ID
```

4. Run the first refresh now instead of waiting for the schedule, and check it:

```sh
kubectl -n claude-system create job --from=cronjob/claude-github-token first-token
kubectl -n claude-system logs job/first-token -c refresh      # "claude-github-token refreshed"
kubectl -n claude-system get secret claude-github-token
```

If the Job fails, look at `kubectl -n claude-system logs job/first-token -c sign-jwt` and
`kubectl -n claude-system describe job first-token`. The runner will not start until the Secret exists.

### Option B: a personal access token

A fine-grained token limited to this repository (Contents and Pull requests read/write, short expiry), in
the same Secret. Disable the CronJob first (`kubectl -n claude-system patch cronjob claude-github-token -p
'{"spec":{"suspend":true}}'`, and set `suspend: true` in `github-app.yaml`) or it will overwrite it.

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

The Secrets are created by hand and are not in Git. Flux only prunes what it applied, so it leaves them
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

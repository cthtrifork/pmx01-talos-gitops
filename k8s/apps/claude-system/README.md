# claude-system

Claude Code **Remote Control** with read-only cluster access and an egress whitelist. The pod runs
`claude remote-control` under your own claude.ai login (stored on a PVC), so the session appears at
claude.ai/code. No admin settings or environment key are needed. Docs: <https://code.claude.com/docs/en/remote-control>.

| File | What |
|---|---|
| `rbac.yaml` | ServiceAccount `claude`; built-in `view` (no Secrets) plus read-only access to nodes, storage, CRDs and the CRD groups in use. No write verbs, no `pods/exec` |
| `networkpolicy.yaml` | Cilium default deny, then DNS, the Kubernetes API and TCP 443 to the hosts Claude Code needs |
| `github-app.yaml` | CronJob that mints a one-hour GitHub App installation token into `claude-github-token` (scripts in `github-app/`) |
| `deployment.yaml` | `claude-rc`: idles until a login exists, then runs `claude remote-control`; non-root, read-only root filesystem |
| `claude-md/CLAUDE.md` | User-level memory, generated into the `claude-md` ConfigMap by `kustomization.yaml` and symlinked to `~/.claude/CLAUDE.md` at pod start. Edit it in Git; the ConfigMap name hash rolls the pod |
| `pvc.yaml` | 5Gi volume holding the login (`/home/claude`); delete it to sign out |
| `../../../images/claude-runner/` | Image: Anthropic publishes none, so this wraps the `claude` binary (checksum verified) plus `kubectl`, `gh` and a git credential helper |

## 1. Build the image

Run the **Publish to Github** workflow (`workflow_dispatch`, tag prefix `rc`) or push a `v*` tag. It
publishes `ghcr.io/cthtrifork/pmx01-talos-gitops/claude-runner:<tag>`. Put that tag, pinned by digest,
in `deployment.yaml`. Renovate bumps `CLAUDE_CODE_VERSION` in the Dockerfile; the checksum is read from the release manifest at build time
(checksum: `https://downloads.claude.ai/claude-code-releases/<version>/manifest.json`, `linux-x64`).

## 2. GitHub access for git

The pod reads a token from the Secret `claude-github-token` and hands it to git for `github.com` only
(the image's `gh` wrapper reads the same file, so the pod can push branches and open PRs; `api.github.com` is in the egress allowlist). A GitHub App keeps that Secret
fresh, with a token that expires every hour.

`github-app.yaml` runs a CronJob that signs a JWT with the App's private key and exchanges it for a
one-hour installation token, narrowed to this repository with Contents, Pull requests and Workflows read/write. The
pod holds only that short-lived token.

1. Create the App (GitHub: Settings > Developer settings > GitHub Apps > New). Repository permissions:
   **Contents: read and write**, **Pull requests: read and write**, **Workflows: read and write**,
   **Metadata: read**. Leave everything else off. **Workflows** lets the App push changes under
   `.github/workflows/`; drop it (and `workflows` in `github-app/refresh-token.sh`) to forbid that. Untick "Webhook active". Never make it a bypass actor in a ruleset.
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
`kubectl -n claude-system describe job first-token`. Git pushes fail until the Secret exists (the pod itself starts without it).

The Secret `claude-github-app` is created by hand and is not in Git. Flux only prunes what it applied, so
it leaves it alone.

## 3. Sign in once

The pod idles and logs "not logged in" until a login exists:

```sh
kubectl -n claude-system exec -it deploy/claude-rc -- claude
```

Run `/login`, open the URL on your own machine, paste the code back, then `/exit`. Within 30 seconds the
pod starts `claude remote-control` and the session appears at <https://claude.ai/code>. The login and
its token refreshes stay on the PVC, so restarts need no new login. To sign out, delete the
`claude-home` PVC. The PVC holds your login: anyone with node access can act as you.

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
API, but the GitHub token and your claude.ai login are in this pod, so treat the pod as holding both.

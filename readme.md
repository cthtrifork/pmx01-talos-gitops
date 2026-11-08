# talos-gitops

GitOps configuration for the **homelab** Talos Linux Kubernetes cluster, reconciled by Flux (Flux Operator).

Remember to install cilium directly and delete flannel + kubeproxy daemonsets, before flux can be applied.


## Installation

```sh
# Install flux operator
kubectl apply -f https://github.com/controlplaneio-fluxcd/flux-operator/releases/latest/download/install.yaml
# Ensure SOPS support
cat "$HOME/sops/age/keys.txt" | kubectl --kubeconfig ./kubeconfig.yaml create secret generic sops-age --namespace=flux-system --from-file=age.agekey=/dev/stdin
```

## Secrets (SOPS)

Every Secret lives in Git as a SOPS-encrypted `*.enc.yaml` (age, rules in `.sops.yaml`: only `data`/`stringData` are encrypted).
Flux decrypts them with the `sops-age` Secret. Nothing is created with a bare `kubectl create secret`; CI
(`scripts/ci/check-secrets-encrypted.sh`) fails on a plaintext Secret.

The only exception is `sops-age` itself (the decryption key, see Installation), which cannot be stored in Git.

Needs `sops`, `kubectl` and your age key (`export SOPS_AGE_KEY_FILE="$HOME/sops/age/keys.txt"`). Encrypting only needs the public key
in `.sops.yaml`, so no cluster access is used. Run from the repository root.

Define the helper once per shell. It renders the Secret client-side and encrypts it to the given file:

```sh
# usage: enc <file> kubectl create secret ...
enc() { f=$1; shift; "$@" --dry-run=client -o yaml \
  | sops encrypt --filename-override "$f" --input-type yaml --output-type yaml /dev/stdin > "$f.tmp" && mv "$f.tmp" "$f"; }
```

**Create** and **rotate** are the same command: re-running it with the new value overwrites the file. Commit and open a PR. Flux applies it,
and Reloader restarts the workloads that carry `reloader.stakater.com/auto`. The first time, also add the file to `resources:` in the
`kustomization.yaml` next to it. To read a value back: `sops decrypt <file>`.

### Secrets that were created by hand (now in Git)

| Secret | File | Create / rotate |
|---|---|---|
| `claude-system/claude-github-app` | `k8s/apps/claude-system/claude-github-app.enc.yaml` | GitHub App id, installation id and private key (App settings > Generate a private key) |
| `claude-system/ghcr-pull` | `k8s/apps/claude-system/ghcr-pull.enc.yaml` | token with only `read:packages` |
| `traefik-system/cloudflared-tunnel-token` | `k8s/infra/ingress-system/traefik/cloudflared-tunnel-token.enc.yaml` | Cloudflare Zero Trust > Networks > Tunnels (sync tunnel) |
| `harper-pad/harper-pad-admin-tunnel` | `k8s/apps/harper-pad/admin-tunnel.enc.yaml` | Cloudflare Zero Trust > Networks > Tunnels (admin tunnel) |
| `harper-pad/github-auth` | `k8s/apps/harper-pad/github-auth.enc.yaml` | image pull secret for `ghcr.io/cthtrifork/harper-pad-*` |
| `flux-system/flux-webhook-token` | `k8s/infra/flux-system/flux-webhook-token.enc.yaml` | HMAC secret of the GitHub push webhook |
| `security-system/dependency-track-api-key` | `k8s/infra/security-system/sbom-hook/dependency-track-api-key.enc.yaml` | Dependency-Track team API key (`BOM_UPLOAD`, `PROJECT_CREATION_UPLOAD`) |

```sh
# claude-github-app: read the values from the App settings page; the key is the downloaded .pem
read -rp 'App ID: ' APP_ID; read -rp 'Installation ID: ' INSTALLATION_ID
enc k8s/apps/claude-system/claude-github-app.enc.yaml \
  kubectl -n claude-system create secret generic claude-github-app \
    --from-literal=app-id="$APP_ID" --from-literal=installation-id="$INSTALLATION_ID" \
    --from-file=private-key.pem=/path/to/app-private-key.pem
unset APP_ID INSTALLATION_ID
# Rotate the key: generate a new .pem in the App settings, re-run, merge, then delete the old key in the App settings.
# Then check: kubectl -n claude-system create job --from=cronjob/claude-github-token rotate-check

# ghcr-pull (type dockerconfigjson, read:packages only; never a token that can push)
read -rsp 'Token: ' GHCR_PULL_TOKEN
enc k8s/apps/claude-system/ghcr-pull.enc.yaml \
  kubectl -n claude-system create secret docker-registry ghcr-pull \
    --docker-server=ghcr.io --docker-username=<github-user> --docker-password="$GHCR_PULL_TOKEN"
unset GHCR_PULL_TOKEN
# Rotate: new token on GitHub, re-run, merge, revoke the old token. The pod mounts the Secret, so it picks it up within a minute.

# cloudflared-tunnel-token (sync tunnel, runs in the Traefik pod)
read -rsp 'Token: ' TOKEN
enc k8s/infra/ingress-system/traefik/cloudflared-tunnel-token.enc.yaml \
  kubectl -n traefik-system create secret generic cloudflared-tunnel-token --from-literal=token="$TOKEN"
unset TOKEN
# Rotate: Cloudflare > the tunnel > Refresh token, re-run, merge (Reloader restarts Traefik).

# harper-pad-admin-tunnel (sidecar in harper-pad-admin)
read -rsp 'Token: ' TOKEN
enc k8s/apps/harper-pad/admin-tunnel.enc.yaml \
  kubectl -n harper-pad create secret generic harper-pad-admin-tunnel --from-literal=token="$TOKEN"
unset TOKEN
# Rotate: as for the sync tunnel.

# harper-pad/github-auth (image pull secret, read:packages)
read -rsp 'Token: ' GHCR_PULL_TOKEN
enc k8s/apps/harper-pad/github-auth.enc.yaml \
  kubectl -n harper-pad create secret docker-registry github-auth \
    --docker-server=ghcr.io --docker-username=<github-user> --docker-password="$GHCR_PULL_TOKEN"
unset GHCR_PULL_TOKEN

# flux-webhook-token: also the input of the webhook path, so GitHub must be updated too (k8s/infra/flux-system/README.md)
TOKEN=$(openssl rand -hex 32)
enc k8s/infra/flux-system/flux-webhook-token.enc.yaml \
  kubectl -n flux-system create secret generic flux-webhook-token --from-literal=token="$TOKEN"
echo "$TOKEN"; unset TOKEN
# Rotate: re-run, merge, read the new path (kubectl -n flux-system get receiver github-receiver -o jsonpath='{.status.webhookPath}')
# and update the webhook's Payload URL and Secret in GitHub.

# dependency-track-api-key
read -rsp 'API key: ' DT_KEY
enc k8s/infra/security-system/sbom-hook/dependency-track-api-key.enc.yaml \
  kubectl -n security-system create secret generic dependency-track-api-key --from-literal=api-key="$DT_KEY"
unset DT_KEY
# Rotate: create a new key for the team in Dependency-Track, re-run, merge (Reloader restarts sbom-hook), delete the old key.
```

### Secrets that were already in Git

Same helper, same rotation (re-run, commit):

| Secret | File | Command |
|---|---|---|
| `adguard-system/adguard-credentials` | `k8s/apps/adguard/secret.enc.yaml` | `enc <file> kubectl -n adguard-system create secret generic adguard-credentials --from-literal=password="$PW"` (also change the password in AdGuard) |
| `arc-runners-system/github-auth` | `k8s/apps/arc-runner/secret.enc.yaml` | `enc <file> kubectl -n arc-runners-system create secret generic github-auth --from-literal=github_token="$TOKEN"` |
| `cert-manager-system/cloudflare-api-token-secret` | `k8s/infra/post/cert-manager/cloudflare-api-token-secret.enc.yaml` | `enc <file> kubectl -n cert-manager-system create secret generic cloudflare-api-token-secret --from-literal=api-token="$TOKEN"` |
| `zot-sync-creds` | `k8s/infra/registry-system/zot/creds.json` | edit in place with `sops edit <file>` (JSON, every value encrypted) |

`enc` fits these only for a full rewrite; `sops edit <file>` changes a single value in place.

### Rotating the age key

```sh
age-keygen -o new-keys.txt                       # then put the new public key in .sops.yaml
cat "$HOME/sops/age/keys.txt" new-keys.txt > all-keys.txt; export SOPS_AGE_KEY_FILE=$PWD/all-keys.txt
find k8s -name '*.enc.yaml' -o -name creds.json | xargs -n1 sops updatekeys -y
kubectl -n flux-system create secret generic sops-age --from-file=age.agekey=new-keys.txt \
  --dry-run=client -o yaml | kubectl apply -f -   # the one Secret that is applied directly
rm all-keys.txt   # keep new-keys.txt as your key from now on
```

### Not managed by hand

`security-system/dependency-track-kek` is generated once, randomly, in the cluster by `dependency-track/kek-job.yaml`. It is not
rotated casually: it encrypts Dependency-Track's stored secrets, so replacing it without a re-key makes them unreadable.
Certificates are issued by cert-manager.

### Install Cilium

```sh
helm install cilium oci://quay.io/cilium/charts/cilium \
  --version 1.19.4 \
  --namespace kube-system \
  --set ipam.mode=kubernetes \
  --set kubeProxyReplacement=true \
  --set securityContext.capabilities.ciliumAgent="{CHOWN,KILL,NET_ADMIN,NET_RAW,IPC_LOCK,SYS_ADMIN,SYS_RESOURCE,DAC_OVERRIDE,FOWNER,SETGID,SETUID}" \
  --set securityContext.capabilities.cleanCiliumState="{NET_ADMIN,SYS_ADMIN,SYS_RESOURCE}" \
  --set cgroup.autoMount.enabled=false \
  --set cgroup.hostRoot=/sys/fs/cgroup \
  --set k8sServiceHost=localhost \
  --set k8sServicePort=7445 \
  --set routingMode="native" \
  --set autoDirectNodeRoutes=true \
  --set enableIPv4Masquerade=true \
  --set ipMasqAgent.enabled=false \
  --set ipv4NativeRoutingCIDR="10.244.0.0/16" \
  --kubeconfig ./kubeconfig.yaml
```

### Delete flannel + kubeproxy daemonsets

```sh
kubectl --kubeconfig ./kubeconfig.yaml delete daemonset kube-proxy -n kube-system
kubectl --kubeconfig ./kubeconfig.yaml delete daemonset kube-flannel -n kube-system
kubectl --kubeconfig ./kubeconfig.yaml delete configmap kube-flannel-cfg -n kube-system
```

### Configure flux instance

```sh
kubectl --kubeconfig ./kubeconfig.yaml apply -f k8s/clusters/homelab/flux-instance.yaml
```

## Debugging

```sh
kubectl --kubeconfig ./kubeconfig.yaml get nodes -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.spec.podCIDR}{"\n"}{end}'
ssh -L 8000:localhost:8000 -N caspertdk@192.168.1.132

talosctl --talosconfig ./talosconfig.yaml  --nodes talos-tmd-e0p  get links
talosctl --talosconfig ./talosconfig.yaml  --nodes talos-tmd-e0p read /proc/net/route
```

<https://www.itguyjournals.com/installing-cilium-and-multus-on-talos-os-for-advanced-kubernetes-networking/>

## Vm stuff

<https://github.com/kubevirt/kubectl-virt-plugin>
<https://a-cup-of.coffee/blog/omni/>
<https://www.talos.dev/v1.11/advanced/install-kubevirt/>


```sh
kubectl virt --kubeconfig ./kubeconfig.yaml start -n kubevirt-system fedora-vm-test
kubectl virt --kubeconfig ./kubeconfig.yaml stop -n kubevirt-system fedora-vm-test

kubectl virt --kubeconfig ./kubeconfig.yaml start -n kubevirt-system homeserver-vm
kubectl virt --kubeconfig ./kubeconfig.yaml image-upload pvc homeserver-pvc --no-create --image-path=/tmp/images/homeserver-centos-stream9.qcow2

# Upload
kubectl --kubeconfig ./kubeconfig.yaml -n cdi port-forward svc/cdi-uploadproxy 8443:443
kubectl virt --kubeconfig ./kubeconfig.yaml image-upload dv homeserver-installer-dv \
  -n kubevirt-system \
  --no-create \
  --image-path=/tmp/images/homeserver-centos-stream9.qcow2 \
  --uploadproxy-url https://127.0.0.1:8443 \
  --insecure
kubectl virt --kubeconfig ./kubeconfig.yaml -n kubevirt-system get dv homeserver-installer-dv -o yaml | grep phase:

kubectl virt --kubeconfig ./kubeconfig.yaml console -n kubevirt-system fedora-vm-test
```

### Troubleshooting

```sh
kubectl --kubeconfig kubeconfig.yaml -n kube-system exec ds/cilium -- cilium-dbg shell -- db/show devices
```

## Renovatebot

### Testing

```sh
docker run -it --rm \
  -v "$(pwd):/code" \
  -w /code \
  -e RENOVATE_PLATFORM=local \
  -e RENOVATE_ONBOARDING=false \
  -e RENOVATE_REQUIRE_CONFIG=optional \
  -e RENOVATE_GITHUB_COM_TOKEN="$GITHUB_TOKEN" \
  -e RENOVATE_DRY_RUN=full \
  -e LOG_LEVEL=debug \
  -v /tmp:/tmp \
  ghcr.io/renovatebot/renovate:43 > renovatelog2.txt

docker run -it --rm \
  -v "$(pwd):/code" \
  -w /code \
  -e RENOVATE_PLATFORM=local \
  -e RENOVATE_ONBOARDING=false \
  -e RENOVATE_REQUIRE_CONFIG=optional \
  -e RENOVATE_GITHUB_COM_TOKEN="$GITHUB_TOKEN" \
  -e RENOVATE_DRY_RUN=full \
  -e LOG_LEVEL=debug \
  -v /tmp:/tmp \
  ghcr.io/renovatebot/renovate:43 \
  renovate-config-validator --strict
```

## Todos

- Cilium network policies
- polaris scoreboard to green for resource request, limits, security context etc.
- Trivy scanning on renovatebot PRs
- render manifests on renovatebot PRs
- Remove insecure-tls from metrics-server
- Finish air-gapped compatability

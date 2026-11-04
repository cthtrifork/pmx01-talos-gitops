# harper-pad

Sync service, admin UI and database for [harper-pad](https://github.com/cthtrifork/harper-pad)
(design: `docs/sync.md` in that repository).

```
phone / desktop -> Cloudflare -> tunnel -> cloudflared + Traefik (traefik-system) -> harper-pad-sync -> harper-pad-db
operator        -> Cloudflare Access -> tunnel -> cloudflared (sidecar) -> harper-pad-admin (127.0.0.1) -> harper-pad-db
```

| Resource | What |
|---|---|
| `harper-pad-db` | CloudNativePG cluster. Client-certificate auth only (cert-manager issues the certificates), no passwords |
| `harper-pad-sync` | The API. Reached only from Traefik (`ingressroute.yaml` adds headers, rate limit and body cap) |
| `harper-pad-admin` | Admin UI. No Service; listens on loopback and is reached only by its own `cloudflared` sidecar |
| `networkpolicy.yaml` | Default deny for the app pods, then only what is needed |

Cloudflare owns DNS and TLS. There is no certificate or LAN address for the API.

## Status

The application images are not published yet, so both Deployments run `traefik/whoami`, an echo
server with a `/health` endpoint, as a placeholder. `harper-pad-sync` is running (once the sync
tunnel token exists, `https://sync.portableinfo.cloud/` echoes the request headers and
`/health` returns 200). `harper-pad-admin` stays at `replicas: 0` until its tunnel token exists.

## Tunnel tokens

Two remotely managed tunnels are needed, so the admin app is never reachable through Traefik.
Create them in Cloudflare Zero Trust (Networks > Tunnels) and copy each tunnel's token.

### 1. Sync tunnel (runs in the Traefik pod)

In the tunnel's **Public hostname** tab add `sync.portableinfo.cloud` with service
`HTTP` `localhost:8000`. Cloudflare creates the DNS record.

```sh
kubectl -n traefik-system create secret generic cloudflared-tunnel-token \
  --from-literal=token='<SYNC_TUNNEL_TOKEN>'
kubectl -n traefik-system rollout restart deployment/traefik
```

### 2. Admin tunnel (sidecar in the admin pod)

Add the admin hostname with service `HTTP` `localhost:9001`, then put a Cloudflare Access
application with your OIDC login in front of that hostname (the app has no login of its own).

```sh
kubectl -n harper-pad create secret generic harper-pad-admin-tunnel \
  --from-literal=token='<ADMIN_TUNNEL_TOKEN>'
```

The admin Deployment is at `replicas: 0`; once the secret exists, scale it up:
`kubectl -n harper-pad scale deployment/harper-pad-admin --replicas=1` (Flux will revert this
unless you also change `replicas` in `admin-deployment.yaml`).

### Committing them instead

The tokens are secrets. To keep them in Git, write the same Secret as a manifest, encrypt it with
SOPS (the repository's `.sops.yaml` applies) and add it to the matching `kustomization.yaml`:

```sh
kubectl -n harper-pad create secret generic harper-pad-admin-tunnel \
  --from-literal=token='<ADMIN_TUNNEL_TOKEN>' --dry-run=client -o yaml > k8s/apps/harper-pad/secret.enc.yaml
sops --encrypt --in-place k8s/apps/harper-pad/secret.enc.yaml
```

Use `-n traefik-system`, `cloudflared-tunnel-token` and a file under
`k8s/infra/ingress-system/traefik/` for the sync tunnel. CI fails on plaintext Secrets.

### Check

```sh
kubectl -n traefik-system logs deploy/traefik -c cloudflared | grep -i 'registered tunnel connection'
kubectl -n harper-pad logs deploy/harper-pad-admin -c cloudflared | grep -i 'registered tunnel connection'
```

## Also in Cloudflare

Turn on **Always Use HTTPS** for the zone.

## Going live

1. Publish the `harper-pad-sync` and `harper-pad-admin` images and pin them by digest in the
   Deployments, replacing the `whoami` placeholder (also set `args: ["serve"]`, and for sync the
   probe paths `/healthz` and `/readyz`).
2. Create the admin tunnel token and set `replicas: 1` on `harper-pad-admin`.
3. Check the database: `kubectl -n harper-pad get cluster harper-pad-db`.

## Try the placeholder

```sh
curl -i https://sync.portableinfo.cloud/health
curl -s https://sync.portableinfo.cloud/ | grep -iE 'x-forwarded-for|cf-connecting-ip'
```

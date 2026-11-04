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

Both Deployments still run `traefik/whoami`, an echo server, as a placeholder until the first application image is
published (harper-pad PR #82). Everything around it already follows the application's contract, so switching is one edit
per Deployment (see "Going live"):

| | sync | admin |
|---|---|---|
| Listens on | `:9000` | `127.0.0.1:9001` |
| Liveness | `GET /healthz` (process only) | none (loopback) |
| Readiness | `GET /readyz` (PostgreSQL round trip) | none (loopback) |
| Metrics | `:9102/metrics`, scraped through `podmonitor.yaml`; the NetworkPolicy lets only Prometheus in | off |

`/readyz` queries the database, so Traefik does not route it (`ingressroute.yaml`); `/healthz` stays public. Both tunnel
tokens exist, so `harper-pad-sync` and `harper-pad-admin` are running (`https://sync.portableinfo.cloud/` echoes the
request headers and `/healthz` returns 200).

## Tunnel tokens

Two remotely managed tunnels are needed, so the admin app is never reachable through Traefik.
Create them in Cloudflare Zero Trust (Networks > Tunnels) and copy each tunnel's token.

### 1. Sync tunnel (runs in the Traefik pod)

In the tunnel's **Public hostname** tab add `sync.portableinfo.cloud` with service
`HTTP` `localhost:8000`. Cloudflare creates the DNS record.

```sh
kubectl -n traefik-system create secret generic cloudflared-tunnel-token \
  --from-literal=token='<SYNC_TUNNEL_TOKEN>'
```

No restart is needed: the pod picks the Secret up when it appears, and Reloader restarts the
Deployment when the token changes later.

### 2. Admin tunnel (sidecar in the admin pod)

Add the admin hostname with service `HTTP` `localhost:9001`, then put a Cloudflare Access
application with your OIDC login in front of that hostname (the app has no login of its own).

```sh
kubectl -n harper-pad create secret generic harper-pad-admin-tunnel \
  --from-literal=token='<ADMIN_TUNNEL_TOKEN>'
```

The admin Deployment runs with `replicas: 1` and starts once this Secret exists.

### Check

```sh
kubectl -n traefik-system logs deploy/traefik -c cloudflared | grep -i 'registered tunnel connection'
kubectl -n harper-pad logs deploy/harper-pad-admin -c cloudflared | grep -i 'registered tunnel connection'
```

## Also in Cloudflare

Turn on **Always Use HTTPS** for the zone.

## Going live

1. In harper-pad, merge PR #82. Its `backend-image.yml` publishes `ghcr.io/cthtrifork/harper-pad-sync` and
   `harper-pad-admin` as `sha-<commit>` on a push to `main`, and the job summary prints each image's digest. The packages
   are private by default: make them public (package settings) or add an `imagePullSecret` to the service accounts.
2. In both Deployments replace the `whoami` image with `ghcr.io/cthtrifork/harper-pad-<sync|admin>@sha256:<digest>`
   (keep a tag in front of the digest so Renovate can bump it) and delete the `args` (they are `whoami`'s; the image's own
   `CMD` is `serve`, and the binary rejects unknown arguments).
3. Check: `kubectl -n harper-pad rollout status deploy/harper-pad-sync`; `curl -i https://sync.portableinfo.cloud/healthz`
   returns `ok`. The pod is only Ready when `/readyz` reaches `harper-pad-db`, so a pod stuck not Ready with
   `readiness: database ping failed` in its log means the client-certificate login or the NetworkPolicy, not the app.
4. Check the database: `kubectl -n harper-pad get cluster harper-pad-db`, and that the `harper-pad-sync` target is up in Prometheus.

## Try the placeholder

```sh
curl -i https://sync.portableinfo.cloud/healthz
curl -s https://sync.portableinfo.cloud/ | grep -iE 'x-forwarded-for|cf-connecting-ip'
```

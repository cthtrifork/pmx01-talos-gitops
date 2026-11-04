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
server with a `/health` endpoint, as a placeholder. Both tunnel tokens exist, so `harper-pad-sync`
and `harper-pad-admin` are running (`https://sync.portableinfo.cloud/` echoes the request headers
and `/health` returns 200).

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

1. Publish the `harper-pad-sync` and `harper-pad-admin` images and pin them by digest in the
   Deployments, replacing the `whoami` placeholder (also set `args: ["serve"]`, and for sync the
   probe paths `/healthz` and `/readyz`).
2. Check the database: `kubectl -n harper-pad get cluster harper-pad-db`.

## Try the placeholder

```sh
curl -i https://sync.portableinfo.cloud/health
curl -s https://sync.portableinfo.cloud/ | grep -iE 'x-forwarded-for|cf-connecting-ip'
```

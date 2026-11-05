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

Both Deployments run `ghcr.io/cthtrifork/harper-pad-sync` and `-admin`. Everything around the images follows the
application's contract (harper-pad PR #82):

| | sync | admin |
|---|---|---|
| Listens on | `:9000` | `127.0.0.1:9001` |
| Liveness | `GET /healthz` (process only) | none (loopback) |
| Readiness | `GET /readyz` (PostgreSQL round trip) | none (loopback) |
| Metrics | `:9102/metrics`, scraped through `podmonitor.yaml`; the NetworkPolicy lets only Prometheus in | off |

`/readyz` queries the database, so Traefik does not route it (`ingressroute.yaml`); `/healthz` stays public. Both tunnel
tokens exist.

**The two images are versioned together.** The admin applies the schema and a new sync image is not ready until the matching admin has
applied it, so bump both in one pull request.

**Image pull secret.** Both ServiceAccounts use the `github-auth` pull secret in the `harper-pad` namespace.

## Tunnel tokens

Two remotely managed tunnels are needed, so the admin app is never reachable through Traefik.
Create them in Cloudflare Zero Trust (Networks > Tunnels) and copy each tunnel's token.

### 1. Sync tunnel (runs in the Traefik pod)

In the tunnel's **Public hostname** tab add `harper-sync.portableinfo.cloud` with service
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

## Try it

```sh
curl -i https://harper-sync.portableinfo.cloud/healthz
```

## Browser origins (CORS)

`CORS_ALLOWED_ORIGINS` in `sync-deployment.yaml` lists the browser origins that may call the API: exact, comma-separated, no trailing
slash, never `*`. It holds the Pages site, `https://harper.portableinfo.cloud`. Empty allows none, and then neither the web app nor the
desktop app can sync, because the browser blocks the call (the service itself is fine, and `curl` works).

The desktop app's window presents its own origin. Run the app once against the server, then read the origin the service rejected (it is
logged once per origin) and add it to the list:

```sh
kubectl -n harper-pad logs deploy/harper-pad-sync | grep -i cors
```

## Changing the database owner

`harper_admin` owns the database and runs the migrations; `harper_sync` is a plain login role (see `postgres-cluster.yaml`). `initdb` runs
only when a cluster is first created, so editing `owner` on a cluster that already exists changes nothing. A database that an earlier
version created with `harper_sync` as owner has to be created again. That is cheap here: there are no backups on purpose, because every
device keeps a full copy of its drafts and Sync all repopulates an empty server.

Do it in this order, because a new admin cannot migrate a database it does not own, and the new sync pod then stays not ready:

1. Merge the pull request that changes `owner` and the images.
2. Delete the cluster and its volume. Flux creates the cluster again from this manifest.

   ```sh
   kubectl -n harper-pad delete cluster harper-pad-db
   kubectl -n harper-pad delete pvc -l cnpg.io/cluster=harper-pad-db
   flux reconcile kustomization app --with-source
   ```

3. The new database is bootstrapped with `harper_admin` as owner, `harper-pad-admin` applies the schema, and `harper-pad-sync` becomes ready
   (`kubectl -n harper-pad get cluster harper-pad-db`).
4. Every device token went with the old database, so each device shows "Sign in again". Create an invite for each in the admin page and
   paste it into the app. Drafts stay on the devices, and Sync all puts them back on the server.

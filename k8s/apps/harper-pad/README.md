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
| `harper-pad-sync` | The API. Reached only from Traefik (`ingressroute.yaml` adds headers and a body cap) |
| `harper-pad-admin` | Admin UI. No Service; listens on loopback and is reached only by its own `cloudflared` sidecar |
| `networkpolicy.yaml` | Default deny for the app pods, then only what is needed |

Cloudflare owns DNS and TLS. There is no certificate or LAN address for the API.

## Status

Both Deployments run `ghcr.io/cthtrifork/harper-pad-sync` and `-admin`. Everything around the images follows the
application's contract (harper-pad PR #82):

| | sync | admin |
|---|---|---|
| Listens on | `:9000` | `127.0.0.1:9001` |
| Liveness | `GET :9102/healthz` (process only) | none (loopback) |
| Readiness | `GET :9102/readyz` (PostgreSQL round trip) | none (loopback) |
| Metrics | `:9102/metrics`, scraped through `podmonitor.yaml`; the NetworkPolicy lets only Prometheus in | off |

The probes are served on the metrics port, which Traefik does not route, so neither `/healthz` nor `/readyz` is public (the
kubelet reaches the port directly). Both tunnel tokens exist.

**The two images are versioned together.** The admin applies the schema and a new sync image is not ready until the matching admin has
applied it, so bump both in one pull request.

**Image pull secret.** Both ServiceAccounts use the `github-auth` pull secret in the `harper-pad` namespace (SOPS-encrypted, see "Secrets (SOPS)" in the root `readme.md`).

## Tunnel tokens

Two remotely managed tunnels are needed, so the admin app is never reachable through Traefik.
Create them in Cloudflare Zero Trust (Networks > Tunnels) and copy each tunnel's token.

### 1. Sync tunnel (runs in the Traefik pod)

In the tunnel's **Public hostname** tab add `harper-sync.portableinfo.cloud` with service
`HTTP` `localhost:8000`. Cloudflare creates the DNS record.

Create the Secret `cloudflared-tunnel-token` as a SOPS-encrypted file (command: "Secrets (SOPS)" in the root `readme.md`).

No restart is needed: the pod picks the Secret up when it appears, and Reloader restarts the
Deployment when the token changes later.

### 2. Admin tunnel (sidecar in the admin pod)

Add the admin hostname with service `HTTP` `localhost:9001`, then put a Cloudflare Access
application with your OIDC login in front of that hostname (the app has no login of its own).

Create the Secret `harper-pad-admin-tunnel` as a SOPS-encrypted file (command: "Secrets (SOPS)" in the root `readme.md`).

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
curl -i https://harper-sync.portableinfo.cloud/docs   # 401: the API answers and wants a DPoP proof
kubectl -n harper-pad port-forward deploy/harper-pad-sync 9102 &
curl -i http://127.0.0.1:9102/readyz                   # 200 when PostgreSQL is reachable; /healthz and /metrics are there too
```

## Browser origins (CORS)

`CORS_ALLOWED_ORIGINS` in `sync-deployment.yaml` lists the browser origins that may call the API: exact, comma-separated, no trailing
slash, never `*`. It holds the Pages site, `https://harper.portableinfo.cloud`, and the desktop window's origin: `wails://localhost` on Linux and macOS,
`http://wails.localhost` on Windows. Empty allows none, and then neither the web app nor the
desktop app can sync, because the browser blocks the call (the service itself is fine, and `curl` works).

The desktop app's window presents its own origin. Run the app once against the server, then read the origin the service rejected (it is
logged once per origin) and add it to the list:

```sh
kubectl -n harper-pad logs deploy/harper-pad-sync | grep -i cors
```

## Database owner

`harper_admin` owns the database and runs the migrations (`harper-pad-admin` connects as it); `harper_sync` is a plain login role that only
the migrations' grants give rights to. `postgres-cluster.yaml` sets this for a new cluster with `initdb.owner`, which runs only once, and
`postgres-database.yaml` is a CloudNativePG `Database` resource whose `owner` is reconciled continuously (it maps to
`ALTER DATABASE ... OWNER TO`), so an existing cluster follows the file without being recreated. `databaseReclaimPolicy: retain` means
deleting that object never drops the database.

CloudNativePG manages the database, not its content (its documentation: "it does not manage database content beyond extensions and schemas
(e.g., tables)"), so tables that were created while `harper_sync` was the owner keep that owner, and `harper-pad-admin` could not alter them.
Check once after the change, as the postgres user inside the pod (superuser access from outside is off):

```sh
kubectl -n harper-pad get database harper-pad-db-harper        # APPLIED should be true
kubectl -n harper-pad exec harper-pad-db-1 -c postgres -- psql -d harper -c "select schemaname, tablename, tableowner from pg_tables where schemaname = 'public'"
```

If every table is owned by `harper_admin`, or there are none yet, there is nothing to do. If some are owned by `harper_sync`, move them once:

```sh
kubectl -n harper-pad exec harper-pad-db-1 -c postgres -- psql -d harper -c "REASSIGN OWNED BY harper_sync TO harper_admin"
```

then check that `harper-pad-admin` starts without a migration error (`kubectl -n harper-pad logs deploy/harper-pad-admin -c admin`) and that
`harper-pad-sync` becomes ready. A database you do not need to keep can instead be deleted and left for the controller to create again: there
are no backups on purpose, because every device keeps a full copy of its drafts and Sync all repopulates an empty server, but every device
token goes with it and each device has to enroll again.

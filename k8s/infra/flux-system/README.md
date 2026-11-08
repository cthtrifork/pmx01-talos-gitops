# Flux webhook (reconcile on push)

```
GitHub push webhook -> Cloudflare -> tunnel -> cloudflared + Traefik -> notification-controller (webhook-receiver) -> GitRepository flux-system
```

Without it, Flux polls the repository on the FluxInstance interval (`sync.interval`, 1h; the default is 1m). With it, a push triggers a fetch at once and the
Kustomizations apply the new revision right away. The poll stays as the fallback if a delivery is lost.

## Setup

1. Create the secret `flux-webhook-token` as a SOPS-encrypted file (command: "Secrets (SOPS)" in the root `readme.md`). It is both the HMAC secret GitHub signs with and the input of the webhook path;
   the command prints the token, which the GitHub webhook below needs.

2. In Cloudflare Zero Trust, on the Traefik tunnel (the one in `traefik-system`), add the public hostname
   `flux-hook.portableinfo.cloud` with service `HTTP` `localhost:8000`. Do not put Cloudflare Access in front of it:
   GitHub cannot log in.

3. After Flux has applied the Receiver, read its path:

   ```sh
   kubectl -n flux-system get receiver github-receiver -o jsonpath='{.status.webhookPath}'
   ```

4. In GitHub: repository Settings > Webhooks > Add webhook.
   - Payload URL: `https://flux-hook.portableinfo.cloud` + the path from step 3
   - Content type: `application/json`
   - Secret: the token from step 1
   - Events: just the `push` event

   GitHub sends a `ping` on creation. A green tick under Recent Deliveries means it works.

If the token changes, the path changes: read it again and update both the secret and the webhook.

## Check

```sh
kubectl -n flux-system get receiver github-receiver   # READY True
flux reconcile source git flux-system --with-source 2>/dev/null; kubectl -n flux-system get gitrepository flux-system
```

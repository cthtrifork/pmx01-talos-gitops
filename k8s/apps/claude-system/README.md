# claude-system

Identity, read-only RBAC and an egress whitelist for a Claude Code pod running in the cluster.
There is no Deployment yet; add one that uses `serviceAccountName: claude` in this namespace.

- **RBAC:** the `claude` ServiceAccount is bound to the built-in `view` ClusterRole (no Secrets) and
  to `claude-extra-view` (get/list/watch on nodes, storage, CRDs and the CRD groups used here:
  CNPG, cert-manager, Traefik, Cilium, Flux, KubeVirt). No write verbs, no `pods/exec`.
- **Network:** `CiliumNetworkPolicy` default-denies ingress and egress for the namespace, then allows
  DNS via kube-dns, the Kubernetes API and TCP 443 to `api.anthropic.com`, `claude.ai` and the GitHub
  hosts only. Add hosts to `networkpolicy.yaml` as needed.
- **Pod security:** the namespace enforces `restricted`.

## Check what the pod can and cannot do

```sh
SA=system:serviceaccount:claude-system:claude
kubectl auth can-i list pods -A --as=$SA              # yes
kubectl auth can-i list clusters.postgresql.cnpg.io -A --as=$SA   # yes
kubectl auth can-i get secrets -A --as=$SA            # no
kubectl auth can-i delete pods -A --as=$SA            # no
kubectl auth can-i create pods/exec -A --as=$SA       # no
```

Anything the pod reads from the cluster (logs, events, annotations) is input Claude acts on, so keep
the pod's GitHub token scoped to the one repository it needs.

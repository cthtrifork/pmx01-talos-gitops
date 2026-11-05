# CLAUDE.md

Repo-level notes for Claude Code sessions (the `claude-rc` pod and anyone else). The user-level rules
(read-only cluster, PRs only, branch `claude/<topic>`) live in `k8s/apps/claude-system/claude-md/CLAUDE.md`
and are not repeated here.

## Where things live

| What | Path |
|---|---|
| zot registry (`registry-system`) | `k8s/infra/registry-system/`; retention and GC are in the `zot-config` ConfigMap |
| ARC controller and runner scale set | `k8s/apps/arc-runner/` (`rendered/` is generated output) |
| Prometheus stack | `k8s/infra/kube-prometheus-stack/` |
| local-path provisioner | `k8s/infra/local-path-provisioner/` |
| claude-rc pod, RBAC, egress policy | `k8s/apps/claude-system/` |

## Checking health (read-only)

- Pods and Flux: `kubectl get pods -A`, `kubectl get helmrelease,kustomization -A`.
- ARC: the listener runs in `arc-controller-system`, runner pods in `arc-runners-system`.
- zot: `zot-0` in `registry-system`; probes `/livez` and `/readyz` show up in its logs.

## Disk and resource usage: query Prometheus

I cannot `exec`, and `nodes/proxy` is denied, so use Prometheus. It is reachable from `claude-rc`
(see the egress rule in `k8s/apps/claude-system/networkpolicy.yaml`):

```sh
P=http://kube-prom-prometheus.kube-prometheus-stack-system.svc:9090
curl -s -G "$P/api/v1/query" --data-urlencode 'query=<promql>'
```

- Do not rely on `kubelet_volume_stats_*`: no such series exist (checked 2026-11-05), so per-PVC usage is
  not available. zot exports no metrics to Prometheus either.
- Use `node_filesystem_size_bytes` / `node_filesystem_avail_bytes` instead. All `local-path` PVCs are
  directories under `/var/mnt/local-path-provisioner` on the node they are pinned to
  (`kubectl get pv -o custom-columns=N:.spec.claimRef.name,SZ:.spec.capacity.storage,NODE:.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[0].values[0]`).
  A PVC's size is not enforced, so the node filesystem is the real limit.
- Usage percent for a mount:
  `100*(1-node_filesystem_avail_bytes{mountpoint="/var/mnt/local-path-provisioner"}/node_filesystem_size_bytes{mountpoint="/var/mnt/local-path-provisioner"})`
- Map `instance` (node IP) to node with `kubectl get nodes -o wide`.
- `kubectl get pv`/`pvc` shows what is pinned to a node, but not how much each uses.

## Known state (2026-11-05, re-check before relying on it)

- `talos-11a-esj` (192.168.1.184) hosts every `local-path` PVC: zot (60Gi), the ARC runner caches
  (about 180Gi requested in total), Prometheus, Claude, adguard, harper-pad. Its
  `/var/mnt/local-path-provisioner` is about 400GB and was 99% full (about 3.8GB free). zot's own share is unknown.
- `/workspace` in `claude-rc` is on the same disk, so a full disk also breaks clones here.

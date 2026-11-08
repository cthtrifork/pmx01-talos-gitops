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

## Dropped traffic: query the Hubble metrics

Network policies are default-deny per namespace (`CiliumNetworkPolicy`). I cannot `exec` or run the `hubble` CLI, but the
Cilium agents export Hubble metrics to the same Prometheus (query it as in the next section). Check them after every
network policy change and before writing a new policy:

- Denied traffic: `sum by (source_namespace,source_workload,destination_namespace,destination_workload,source_ip,destination_ip,traffic_direction)(increase(hubble_drop_total{reason="POLICY_DENIED"}[5m]))>0`.
  Pod IPs and node IPs are in `source_ip` and `destination_ip`; map nodes with `kubectl get nodes -o wide`. Peers with
  no workload name (world, host) have empty workload labels, so read the IPs. The metric is configured in
  `k8s/infra/cilium/helm-release.yaml` (`hubble.metrics.enabled`).
- What a workload talks to, before writing its policy: `sum by (source,destination,protocol,verdict)(increase(hubble_flows_processed_total{type="Trace"}[12h]))>0`
  (labels `source` and `destination` are workload names or `reserved:host|remote-node|kube-apiserver|world`). Flows in the
  reverse direction are replies on an allowed connection and need no rule.
- Gotchas seen so far: the Omni service exposer runs on the host network, so services with an
  `omni-kube-service-exposer.sidero.dev/*` annotation need an ingress rule from the `host` and `remote-node` entities;
  kubelet probes are allowed by Cilium and need no rule; `claude-rc` egress drops are expected for hosts outside the
  allowlist in `k8s/apps/claude-system/networkpolicy.yaml`.
- A policy is not live until Flux has applied it: check `kubectl get cnp -A` and `kubectl get kustomization -n flux-system`,
  then wait a few minutes before reading the drop metric.

## Disk and resource usage: query Prometheus

I cannot `exec`, and `nodes/proxy` is denied, so use Prometheus. It is reachable from `claude-rc`
(see the egress rule in `k8s/apps/claude-system/networkpolicy.yaml`):

```sh
P=http://kube-prom-prometheus.kube-prometheus-stack-system.svc:9090
curl -s -G "$P/api/v1/query" --data-urlencode 'query=<promql>'
```

- Do not rely on `kubelet_volume_stats_*`: no such series exist. The `local-path` PVs
  are `hostPath` volumes and the kubelet does not collect stats for those, so per-PVC usage is not
  available. node-exporter is healthy but only reports whole filesystems, never per-directory usage.
- Use `node_filesystem_size_bytes` / `node_filesystem_avail_bytes` instead for disk usage. All `local-path` PVCs are
  directories under `/var/mnt/local-path-provisioner` on the node they are pinned to
  (`kubectl get pv -o custom-columns=N:.spec.claimRef.name,SZ:.spec.capacity.storage,NODE:.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[0].values[0]`).
  A PVC's size is not enforced, so the node filesystem is the real limit.
- Usage percent for a mount:
  `100*(1-node_filesystem_avail_bytes{mountpoint="/var/mnt/local-path-provisioner"}/node_filesystem_size_bytes{mountpoint="/var/mnt/local-path-provisioner"})`
- Map `instance` (node IP) to node with `kubectl get nodes -o wide`.
- `kubectl get pv`/`pvc` shows what is pinned to a node, but not how much each uses.
- Per-PVC usage needs the Talos API, which I do not have (no `talosctl`). Ask the user to run
  `talosctl -n <node-ip> usage -H /var/mnt/local-path-provisioner`; each PVC is a directory named
  `pvc-<uid>_<namespace>_<claim>`.
- zot's own metrics (`zot_*`, e.g. per-repo storage) should be scraped by `k8s/infra/registry-system/zot/servicemonitor.yaml`.
  The chart's ServiceMonitor was rejected by the operator (it references a missing Secret `basic-auth`).
  If `zot_*` series are missing, check `kubectl logs -n kube-prometheus-stack-system deploy/kube-prom-operator | grep zot`.

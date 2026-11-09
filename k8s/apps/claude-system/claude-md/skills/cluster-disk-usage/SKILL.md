---
name: cluster-disk-usage
description: Check disk, PVC and resource usage via Prometheus (no exec, no kubelet_volume_stats). Use for local-path or /workspace disk questions, a NodeFilesystem alert, or PVC size questions.
---

# Disk and resource usage: query Prometheus

No `exec` and `nodes/proxy` is denied, so use Prometheus (egress rule in `k8s/apps/claude-system/networkpolicy.yaml`):

```sh
P=http://kube-prom-prometheus.kube-prometheus-stack-system.svc:9090
curl -s -G "$P/api/v1/query" --data-urlencode 'query=<promql>'
```

- `kubelet_volume_stats_*` does not exist: `local-path` PVs are `hostPath`, so there is no per-PVC usage. Use
  `node_filesystem_size_bytes` / `node_filesystem_avail_bytes`; the node filesystem is the real limit (PVC sizes are not
  enforced). Percent used:
  `100*(1-node_filesystem_avail_bytes{mountpoint="/var/mnt/local-path-provisioner"}/node_filesystem_size_bytes{mountpoint="/var/mnt/local-path-provisioner"})`.
- Each PVC is a directory `pvc-<uid>_<namespace>_<claim>` under that mount on its node
  (`kubectl get pv -o custom-columns=N:.spec.claimRef.name,SZ:.spec.capacity.storage,NODE:.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[0].values[0]`).
  Per-directory usage needs the Talos API (no `talosctl` here): ask the user for
  `talosctl -n <node-ip> usage -H /var/mnt/local-path-provisioner`.
- `instance` is a node IP; map it with `kubectl get nodes -o wide`.
- zot's `zot_*` metrics come from `k8s/infra/registry-system/zot/servicemonitor.yaml`. If they are missing, check
  `kubectl logs -n kube-prometheus-stack-system deploy/kube-prom-operator | grep zot` (the chart's ServiceMonitor was
  rejected for a missing Secret `basic-auth`).

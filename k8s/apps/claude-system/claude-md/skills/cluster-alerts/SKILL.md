---
name: cluster-alerts
description: Read Prometheus alerts and rules in the homelab cluster (no Alertmanager). Use at the start of every cluster task, after any policy or Cilium change, and when a NodeFilesystem*, Cilium* or ScrapeTargetDown alert is mentioned or a PrometheusRule is being written.
---

# Alerts: watch them in Prometheus

There is no Alertmanager, so nothing is sent anywhere: the rules are evaluated by Prometheus and I read the result.
Look at them at the start of a task, and after every policy or Cilium change (wait a few minutes after Flux applied it).

```sh
P=http://kube-prom-prometheus.kube-prometheus-stack-system.svc:9090
# firing and pending alerts with their labels and annotations
curl -s "$P/api/v1/alerts" | python3 -I -c 'import sys,json
for a in json.load(sys.stdin)["data"]["alerts"]: print(a["state"],a["labels"].get("alertname"),a["labels"],a["annotations"].get("summary"))'
# the same as a query
curl -s -G "$P/api/v1/query" --data-urlencode 'query=ALERTS{alertstate="firing"}'
# all rules and their health
curl -s "$P/api/v1/rules?type=alert"
```

- Rules are `PrometheusRule` objects (`kubectl get prometheusrule -A`), currently `cilium-alerts` in
  `k8s/infra/kube-prometheus-stack/prometheusrule-cilium.yaml`. A new rule needs the label `release: kube-prometheus-stack`
  or Prometheus ignores it. Check an expression against the query API before the PR.
- `CiliumPolicyDenied` (a missing allow rule: read the labels, then the `cilium-drops` skill), `CiliumPolicyChangeFailures`
  (a policy Cilium could not load: check VALID in `kubectl get cnp,ccnp -A`), `CiliumDropsHigh`, `CiliumEndpointNotReady`,
  `CiliumControllersFailing`, `CiliumUnreachable`, and `ScrapeTargetDown` (any `up == 0` for 10m, also the first sign of a
  default-deny that blocked Prometheus).
- Disk: `NodeFilesystemAlmostFull` (85%), `NodeFilesystemCriticallyFull` (95%), `NodeFilesystemFillingUp` (full within
  24h, only below 30% free), `NodeFilesystemInodesAlmostFull`, `NodeFilesystemReadOnly`. They watch the node
  filesystems, which is the real limit for the local-path PVCs, so a firing alert names the node by IP. The big one is
  `/var/mnt/local-path-provisioner` on `talos-11a-esj` (192.168.1.184), where `/workspace` also lives. Per-directory usage
  still needs `talosctl usage` (see the `cluster-disk-usage` skill); free space in `/workspace` with `df -h /workspace`.
- The thresholds come from the baseline of this cluster (about 1 to 3 drops/s per node of VLAN and L3 noise, about 25
  `POLICY_DENIED` per 10m from `claude-rc` to unlisted hosts). Tune them in the file, do not silence an alert by hand.
- A pending alert has not yet held for its `for:` time. No alert is not proof: it also means the rule was not loaded
  (check `/api/v1/rules`).

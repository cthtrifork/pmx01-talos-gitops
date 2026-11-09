---
name: cilium-drops
description: Find and fix dropped traffic (default-deny CiliumNetworkPolicy, Hubble metrics). Use when a command hangs on connect, before writing or changing a CiliumNetworkPolicy or egress allowlist, when a workload cannot reach a service, or after a policy change.
---

# Dropped traffic: Hubble metrics

Every namespace is default-deny (`CiliumNetworkPolicy`). I cannot `exec` or run `hubble`, but the Cilium agents export
Hubble metrics to Prometheus (query it through Prometheus, see the `cluster-disk-usage` skill for the base URL). Check them after every policy change and before
writing a policy. A policy is live only once Flux applied it (`kubectl get cnp -A`, `kubectl get kustomization -n flux-system`);
wait a few minutes before reading the drops.

- Denied traffic: `sum by (source_namespace,source_workload,destination_namespace,destination_workload,source_ip,destination_ip,traffic_direction)(increase(hubble_drop_total{reason="POLICY_DENIED"}[5m]))>0`.
  Peers without a workload name (world, host) have empty workload labels: read the IPs (`kubectl get nodes -o wide` maps
  nodes). Configured in `k8s/infra/cilium/helm-release.yaml` (`hubble.metrics.enabled`).
- What a workload talks to: `sum by (source,destination,protocol,verdict)(increase(hubble_flows_processed_total{type="Trace"}[12h]))>0`
  (`reserved:host|remote-node|kube-apiserver|world` are entities). Reverse-direction flows are replies and need no rule.
- Gotchas: the Omni service exposer runs on the host network, so a service with an `omni-kube-service-exposer.sidero.dev/*`
  annotation needs ingress from the `host` and `remote-node` entities. Kubelet probes need no rule.
- Prometheus scraping is opt-in by label: a pod carrying `prometheus-scrape: "true"` is reachable from Prometheus on the
  metrics ports of the cluster-wide policy `prometheus-scrape` (`k8s/infra/kube-prometheus-stack/networkpolicy.yaml`). For a
  new scraped workload add the label (chart `podLabels`) and, if its port is not in that list, add the port there, instead of a
  new rule in the workload's namespace. Prometheus's own egress to the targets is the `prometheus` policy in the same file.
- My own denied traffic: `scripts/claude-drops.sh [window]` (default 24h) lists what `claude-rc` was denied with a reverse
  DNS name per IP (a hint only: `ec2-*` is AWS, and Docker Hub's pool shows as AWS; match IPs against
  `getent ahostsv4 <host>`). Run it when a command fails on connect, before asking the user to widen the allowlist. Use a
  short window: older series have no `source_ip` or `destination_ip`. The pod sets
  `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, so Claude Code's Datadog telemetry should not show up.

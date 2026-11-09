---
name: polaris-report
description: Read the Polaris cluster audit score and findings from polaris-dashboard. Use when asked about the Polaris score, audit findings, or after changing the Polaris config or exemptions.
---

# Polaris report (cluster audit score)

`polaris-dashboard` in `policy-system` is reachable from `claude-rc`. The report is only served over HTTP (no Kubernetes object):
`curl -s http://polaris-dashboard.policy-system.svc.cluster.local/results.json -o <scratchpad>/polaris.json`

- `Score` is the overall score and `AuditTime` when it was computed. A config change is a Helm upgrade that restarts the
  dashboard: wait for the new pod (`kubectl get pods -n policy-system -l app=polaris`) and check `AuditTime` is after the
  merge before trusting the numbers.
- Findings are in `Results[]` (per controller) at `.Results`, `.PodResult.Results` and
  `.PodResult.ContainerResults[].Results`; a failing check has `Success: false`. Group by check ID and severity with a few
  lines of Python (`python3 -I`).
- Checks, exemptions and ignored rules are in `k8s/infra/policy-system/polaris/helm-release.yaml` (each exemption has a
  comment). `controllerNames` match by prefix, so `arc-ubuntu` also covers the generated listener and runner pods.
- Do not trust a finding without looking: `sensitiveContainerEnvVar` and `sensitiveConfigmapContent` are keyword matches on names.

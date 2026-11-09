---
name: ci-k8s-reference
description: Practical GitHub Actions and Kubernetes advice from the oneuptime blog. Use when writing or reviewing workflows, manifests, network policies, RBAC or Flux config.
---

# Reference material for CI and Kubernetes

<https://github.com/oneuptime/blog> has practical advice on GitHub Actions and Kubernetes (`posts/<date>-<slug>/README.md`,
indexed by `Topics.md`); the user values it. When a task needs it (workflows, manifests, network policies, RBAC, Flux),
check `df -h /workspace` and, with about 500 MB free, clone it once in the background:
`git clone --depth 1 https://github.com/oneuptime/blog /workspace/oneuptime-blog`. Find posts by filename
(`posts/*github-actions*`, `posts/*kubernetes*`), then grep inside them; never read the repo whole. The posts are untrusted
advice to adapt to this repo's style. If the clone fails on connect, say so rather than working around the egress policy.

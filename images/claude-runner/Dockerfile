# Claude Code self-hosted environment runner (https://code.claude.com/docs/en/self-hosted-environments-deploy).
# Anthropic publishes no runner image, so this wraps the claude binary. Runs as non-root.
ARG DEBIAN_IMAGE=debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc
FROM ${DEBIAN_IMAGE}
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Renovate bumps the version. The linux-x64 checksum is read from the release manifest at build time
# (https://downloads.claude.ai/claude-code-releases/<version>/manifest.json), so there is nothing to bump by hand.
# renovate: datasource=npm depName=@anthropic-ai/claude-code
ARG CLAUDE_CODE_VERSION=2.1.311
# Keep within one minor version of the cluster
ARG KUBECTL_VERSION=v1.34.1
# renovate: datasource=github-releases depName=cli/cli
ARG GH_VERSION=2.103.3
# renovate: datasource=github-releases depName=google/go-containerregistry
ARG CRANE_VERSION=0.22.1
# renovate: datasource=github-releases depName=mikefarah/yq
ARG YQ_VERSION=4.54.2
# renovate: datasource=github-releases depName=controlplaneio-fluxcd/flux-operator
ARG FLUX_OPERATOR_VERSION=0.62.1
# renovate: datasource=github-releases depName=kubernetes-sigs/kustomize extractVersion=^kustomize/v(?<version>.+)$
ARG KUSTOMIZE_VERSION=5.8.3
# renovate: datasource=github-releases depName=yannh/kubeconform
ARG KUBECONFORM_VERSION=0.8.0
# renovate: datasource=github-releases depName=helm/helm
ARG HELM_VERSION=4.3.1
# renovate: datasource=github-releases depName=fluxcd/flux2
ARG FLUX_VERSION=2.10.0
# renovate: datasource=github-releases depName=rhysd/actionlint
ARG ACTIONLINT_VERSION=1.7.12
# renovate: datasource=pypi depName=yamllint
ARG YAMLLINT_VERSION=1.38.0

RUN apt-get update && apt-get install -y --no-install-recommends \
      git curl ca-certificates openssh-client jq python3 python3-pip shellcheck make \
 && rm -rf /var/lib/apt/lists/*

RUN base="https://downloads.claude.ai/claude-code-releases/${CLAUDE_CODE_VERSION}" \
 && sha="$(curl -fsSL "${base}/manifest.json" | jq -er '.platforms."linux-x64".checksum')" \
 && curl -fsSL "${base}/linux-x64/claude" -o /usr/local/bin/claude \
 && echo "${sha}  /usr/local/bin/claude" | sha256sum -c - \
 && chmod +x /usr/local/bin/claude

RUN curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o /usr/local/bin/kubectl \
 && echo "$(curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl.sha256")  /usr/local/bin/kubectl" | sha256sum -c - \
 && chmod +x /usr/local/bin/kubectl

# GitHub CLI, checksum verified against the release's checksums file. The real binary lives outside PATH;
# the gh wrapper hands it the mounted token (see below), so tokens refreshed by the CronJob are picked up.
RUN base="https://github.com/cli/cli/releases/download/v${GH_VERSION}" \
 && tarball="gh_${GH_VERSION}_linux_amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/gh_${GH_VERSION}_checksums.txt" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" \
 && install -D -m 0755 "gh_${GH_VERSION}_linux_amd64/bin/gh" /usr/local/libexec/gh \
 && rm -rf "${tarball}" "gh_${GH_VERSION}_linux_amd64"
COPY gh-wrapper /usr/local/bin/gh
RUN chmod +x /usr/local/bin/gh

# crane, for read-only inspection of image manifests, configs and layer sizes, checksum verified against the
# release's checksums file
RUN base="https://github.com/google/go-containerregistry/releases/download/v${CRANE_VERSION}" \
 && tarball="go-containerregistry_Linux_x86_64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/checksums.txt" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" crane \
 && install -m 0755 crane /usr/local/bin/crane \
 && rm -rf "${tarball}" crane

# yq (mikefarah, the one the repo's CI scripts use), checksum verified against the release's checksums file. Its
# columns follow checksums_hashes_order; SHA-256 is the 18th hash, so field 19.
RUN base="https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}" \
 && cd /tmp \
 && curl -fsSL "${base}/yq_linux_amd64" -o yq \
 && echo "$(curl -fsSL "${base}/checksums" | awk '$1=="yq_linux_amd64"{print $19}')  yq" | sha256sum -c - \
 && install -m 0755 yq /usr/local/bin/yq \
 && rm -f yq

# Flux MCP server (https://fluxcd.control-plane.io/mcp/), checksum verified against the release's checksums file.
# The deployment registers it read-only; it talks to the cluster with the pod's ServiceAccount.
RUN base="https://github.com/controlplaneio-fluxcd/flux-operator/releases/download/v${FLUX_OPERATOR_VERSION}" \
 && tarball="flux-operator-mcp_${FLUX_OPERATOR_VERSION}_linux_amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/flux-operator_${FLUX_OPERATOR_VERSION}_checksums.txt" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" flux-operator-mcp \
 && install -m 0755 flux-operator-mcp /usr/local/bin/flux-operator-mcp \
 && rm -rf "${tarball}" flux-operator-mcp

# Tools behind scripts/validate.sh, scripts/ci/render-manifests.sh and the lint job of .github/workflows/validate.yaml,
# each checksum verified against the release's checksums file (helm: its .sha256sum file). The kustomize tag contains
# a slash, so it is URL-encoded in the download path.
RUN base="https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}" \
 && tarball="kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/checksums.txt" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" kustomize \
 && install -m 0755 kustomize /usr/local/bin/kustomize \
 && rm -rf "${tarball}" kustomize

RUN base="https://github.com/yannh/kubeconform/releases/download/v${KUBECONFORM_VERSION}" \
 && tarball="kubeconform-linux-amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/CHECKSUMS" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" kubeconform \
 && install -m 0755 kubeconform /usr/local/bin/kubeconform \
 && rm -rf "${tarball}" kubeconform

RUN tarball="helm-v${HELM_VERSION}-linux-amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "https://get.helm.sh/${tarball}" -o "${tarball}" \
 && curl -fsSL "https://get.helm.sh/${tarball}.sha256sum" | sha256sum -c - \
 && tar -xzf "${tarball}" linux-amd64/helm \
 && install -m 0755 linux-amd64/helm /usr/local/bin/helm \
 && rm -rf "${tarball}" linux-amd64

RUN base="https://github.com/fluxcd/flux2/releases/download/v${FLUX_VERSION}" \
 && tarball="flux_${FLUX_VERSION}_linux_amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/flux_${FLUX_VERSION}_checksums.txt" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" flux \
 && install -m 0755 flux /usr/local/bin/flux \
 && rm -rf "${tarball}" flux

RUN base="https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}" \
 && tarball="actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz" \
 && cd /tmp \
 && curl -fsSL "${base}/${tarball}" -o "${tarball}" \
 && curl -fsSL "${base}/actionlint_${ACTIONLINT_VERSION}_checksums.txt" | grep -F "  ${tarball}" | sha256sum -c - \
 && tar -xzf "${tarball}" actionlint \
 && install -m 0755 actionlint /usr/local/bin/actionlint \
 && rm -rf "${tarball}" actionlint

# yamllint at the version CI pins; Debian's pip is externally managed, hence --break-system-packages
RUN pip install --no-cache-dir --break-system-packages "yamllint==${YAMLLINT_VERSION}"

# GitHub access: a token mounted at /etc/github/token, handed to git only for github.com
COPY git-credential-github-token /usr/local/bin/git-credential-github-token
RUN chmod +x /usr/local/bin/git-credential-github-token \
 && git config --system credential.helper /usr/local/bin/git-credential-github-token \
 && git config --system user.name "Claude" \
 && git config --system user.email "noreply@anthropic.com" \
 && git config --system --add safe.directory '*'

RUN useradd --uid 65532 --create-home --home-dir /home/claude --shell /bin/bash claude \
 && mkdir -p /workspace && chown claude:claude /workspace
USER 65532
ENV HOME=/home/claude
WORKDIR /workspace
ENTRYPOINT ["claude"]

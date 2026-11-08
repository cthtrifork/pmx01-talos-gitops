# Claude Code self-hosted environment runner (https://code.claude.com/docs/en/self-hosted-environments-deploy).
# Anthropic publishes no runner image, so this wraps the claude binary. Runs as non-root.
ARG DEBIAN_IMAGE=debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc
FROM ${DEBIAN_IMAGE}
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Renovate bumps the version. The linux-x64 checksum is read from the release manifest at build time
# (https://downloads.claude.ai/claude-code-releases/<version>/manifest.json), so there is nothing to bump by hand.
ARG CLAUDE_CODE_VERSION=2.1.311 # renovate: datasource=npm depName=@anthropic-ai/claude-code
# Keep within one minor version of the cluster
ARG KUBECTL_VERSION=v1.34.1
ARG GH_VERSION=2.103.3 # renovate: datasource=github-releases depName=cli/cli
ARG CRANE_VERSION=0.22.1 # renovate: datasource=github-releases depName=google/go-containerregistry
ARG FLUX_OPERATOR_VERSION=0.62.1 # renovate: datasource=github-releases depName=controlplaneio-fluxcd/flux-operator

RUN apt-get update && apt-get install -y --no-install-recommends \
      git curl ca-certificates openssh-client jq python3 \
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

# Claude Code self-hosted environment runner (https://code.claude.com/docs/en/self-hosted-environments-deploy).
# Anthropic publishes no runner image, so this wraps the claude binary. Runs as non-root.
ARG DEBIAN_IMAGE=debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3
FROM ${DEBIAN_IMAGE}
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Renovate bumps the version. The linux-x64 checksum is read from the release manifest at build time
# (https://downloads.claude.ai/claude-code-releases/<version>/manifest.json), so there is nothing to bump by hand.
ARG CLAUDE_CODE_VERSION=2.1.311 # renovate: datasource=npm depName=@anthropic-ai/claude-code
# Keep within one minor version of the cluster
ARG KUBECTL_VERSION=v1.34.1

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

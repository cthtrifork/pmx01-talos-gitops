FROM ghcr.io/actions/actions-runner:2.328.0
ENV NONINTERACTIVE=1
ENV PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:${PATH}"

USER root
# Install apt-get dependencies
# hadolint ignore=DL3008
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  build-essential procps curl file git tree \
  podman uidmap fuse-overlayfs iptables \
  && rm -rf /var/lib/apt/lists/*

# Rootful storage config – no userns
RUN mkdir -p /etc/containers /var/lib/containers /run/containers/storage && \
  cat >/etc/containers/storage.conf <<'EOF'
[storage]
driver = "overlay"
runroot = "/run/containers/storage"
graphroot = "/var/lib/containers/storage"
[storage.options]
mount_program = "/usr/bin/fuse-overlayfs"
EOF

RUN echo runner:10000:5000 | tee /etc/subuid; \
  echo runner:10000:5000 | tee /etc/subgid;

USER runner

RUN mkdir -p /home/runner/.docker && \
  touch /home/runner/.docker/config.json && \
  echo '{"auths": {}}' > /home/runner/.docker/config.json

## We use Homebrew as the primary package manager
# Install Homebrew
RUN /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" \
  && echo >> /home/runner/.bashrc \
  && echo "eval '$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)'" >> /home/runner/.bashrc \
  && eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && brew update \
  && brew upgrade \
  && brew install cilium-cli \
  && brew install fluxcd/tap/flux \
  && brew install helm \
  && brew install kind \
  && brew install kubeconform \
  && brew install kubectl \
  && brew install make \
  && brew install sops \
  && brew install yamllint \
  && brew install yq \
  && brew cleanup

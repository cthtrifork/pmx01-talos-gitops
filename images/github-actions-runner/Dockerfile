# Source: https://github.com/actions/runner/blob/main/images/Dockerfile
# Source: https://github.com/actions/runner/blob/main/images/Dockerfile
ARG RUNNER_VERSION=2.334.0@sha256:b6614fce332517f74d0a76e7c762fb08e4f2ff13dcf333183397c8a5725b6e8e
FROM ghcr.io/actions/actions-runner:${RUNNER_VERSION}

# CUSTOM
USER root

ENV NONINTERACTIVE=1
ENV PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:${PATH}"

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  build-essential procps curl file git tree git wget \
  podman uidmap fuse-overlayfs iptables buildah \
  netavark dnsutils \
  skopeo gettext rsync \
  socat sqlite3 qemu-system-x86 qemu-utils \
  imagemagick \
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
additionalimagestores = [ "/var/lib/shared",]
EOF


RUN mkdir -p /var/lib/shared && \
  chmod -R a+rx /var/lib/shared

RUN echo runner:10000:5000 | tee /etc/subuid; \
  echo runner:10000:5000 | tee /etc/subgid;

USER runner
ENV DOCKER_BUILDKIT=1
ENV HOME=/home/runner
ENV PATH="/home/runner/.local/bin:${PATH}"
ENV XDG_RUNTIME_DIR=/run/user/1001

# Ensure the home tree exists and is owned by the runner
RUN mkdir -p /home/runner/.local/bin /home/runner/.cache /home/runner/.config /home/runner/.docker \
  && chown -R 1001:1001 /home/runner

# Ensure default docker config
RUN echo "{ \"auths\": {} }" > /home/runner/.docker/config.json

# Software list: https://github.com/actions/runner-images/blob/main/images/ubuntu-slim/ubuntu-slim-Readme.md
## We use Homebrew as the primary package manager
# Install Homebrew first
RUN /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" \
  && echo >> /home/runner/.bashrc \
  && echo "eval '$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)'" >> /home/runner/.bashrc

COPY Brewfile Brewfile
RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" && brew bundle

ENV HOME=/home/runner

RUN mkdir -p "${HOME}/.cache/trivy/db" \
	&& oras pull ghcr.io/aquasecurity/trivy-db:2 \
  && tar -xzf db.tar.gz -C "${HOME}/.cache/trivy/db" \
  && rm db.tar.gz

RUN mkdir -p "${HOME}/.cache/trivy/java-db" \
	&& oras pull ghcr.io/aquasecurity/trivy-java-db:1 \
  && tar -xzf javadb.tar.gz -C "${HOME}/.cache/trivy/java-db" \
  && rm javadb.tar.gz

# Remove Haskell, android and dotnet
RUN rm -rf /usr/share/dotnet || true
RUN rm -rf /usr/local/lib/android || true
RUN rm -rf /opt/ghc || true
RUN rm -rf /usr/local/.ghcup || true

WORKDIR /home/runner

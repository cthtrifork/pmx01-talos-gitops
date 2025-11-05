FROM ghcr.io/actions/actions-runner:2.329.0
ENV NONINTERACTIVE=1
ENV PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:${PATH}"

USER root
# Install apt-get dependencies
# hadolint ignore=DL3008

RUN add-apt-repository ppa:longsleep/golang-backports
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  build-essential procps curl file git tree git wget \
  bats btrfs-progs libapparmor-dev libglib2.0-dev libgpgme11-dev libseccomp-dev libselinux1-dev make runc skopeo libbtrfs-dev \
  podman uidmap fuse-overlayfs iptables \
  socat sqlite3 qemu-system-x86 qemu-utils \
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

# Ensure GO is installed
ENV GO_VERSION=1.23.12
RUN wget -q -P /tmp "https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz"

ENV GOPATH /go
RUN tar -C /usr/local -xzf "/tmp/go${GO_VERSION}.linux-amd64.tar.gz" \
  && rm "/tmp/go${GO_VERSION}.linux-amd64.tar.gz" \
  && mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

# Ensure latest buildah
# used by buildah
RUN go install github.com/cpuguy83/go-md2man/v2@latest
RUN mkdir /tmp/buildah; \
     git clone https://github.com/containers/buildah /tmp/buildah; \
     cd /tmp/buildah; \
     make; \
     make install; \
     rm -rf /tmp/buildah/*;

USER runner
ENV DOCKER_BUILDKIT=1
RUN mkdir -p /home/runner/.docker

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

WORKDIR /home/runner

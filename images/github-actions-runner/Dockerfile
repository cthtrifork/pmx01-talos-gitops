FROM ghcr.io/actions/actions-runner:2.329.0 as build

# https://github.com/actions/runner/blob/main/images/Dockerfile
FROM mcr.microsoft.com/dotnet/runtime-deps:10.0-noble@sha256:4e6dd3c625551285d75e664d112adfeea151a3996ea4a1fb5149db97017c1027

ENV DEBIAN_FRONTEND=noninteractive
ENV RUNNER_MANUALLY_TRAP_SIG=1
ENV ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT=1
ENV ImageOS=ubuntu22

# 'gpg-agent' and 'software-properties-common' are needed for the 'add-apt-repository' command that follows
RUN apt update -y \
    && apt install -y --no-install-recommends sudo lsb-release gpg-agent software-properties-common curl jq unzip \
    && rm -rf /var/lib/apt/lists/*

# Configure git-core/ppa based on guidance here:  https://git-scm.com/download/linux
RUN add-apt-repository ppa:git-core/ppa \
    && apt update -y \
    && apt install -y git \
    && rm -rf /var/lib/apt/lists/*

RUN adduser --disabled-password --gecos "" --uid 1001 runner \
    && groupadd docker --gid 123 \
    && usermod -aG sudo runner \
    && usermod -aG docker runner \
    && echo "%sudo   ALL=(ALL:ALL) NOPASSWD:ALL" > /etc/sudoers \
    && echo "Defaults env_keep += \"DEBIAN_FRONTEND\"" >> /etc/sudoers

WORKDIR /home/runner

COPY --chown=runner:docker --from=build . .
COPY --from=build /usr/local/lib/docker/cli-plugins/docker-buildx /usr/local/lib/docker/cli-plugins/docker-buildx

#RUN install -o root -g root -m 755 docker/* /usr/bin/ && rm -rf docker

# CUSTOM
USER root

ENV NONINTERACTIVE=1
ENV PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:${PATH}"

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  build-essential procps curl file git tree git wget \
  bats btrfs-progs libapparmor-dev libglib2.0-dev libgpgme11-dev libseccomp-dev libselinux1-dev make runc skopeo libbtrfs-dev \
  podman uidmap fuse-overlayfs iptables buildah \
  netavark \
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

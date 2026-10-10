# syntax=docker/dockerfile:1
#
# Public sprout workspace image: the open-source sprout binary from a GitHub
# release, git, and a small entrypoint that serves /workspace with the sprout
# daemon. `sprout runner` container mode runs it read-only, with every
# capability dropped and tmpfs at /tmp, /run and /home/sprout.
#
#   docker build --build-arg SPROUT_VERSION=v0.26.1 -t sprout-workspace docker/workspace

# Overridable so a build can pin the base by digest.
ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}

ARG SPROUT_VERSION
ARG SPROUT_RELEASE_REPO=sprout-foundry/sprout
# Set by buildx for each platform being built (amd64, arm64).
ARG TARGETARCH

LABEL org.opencontainers.image.source="https://github.com/sprout-foundry/sprout" \
      org.opencontainers.image.title="sprout-workspace" \
      org.opencontainers.image.description="Workspace image for sprout runner container mode: the open-source sprout daemon and git" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${SPROUT_VERSION}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl git openssh-client tini \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd --gid 1000 sprout \
    && useradd --uid 1000 --gid 1000 --home-dir /home/sprout --create-home --shell /bin/sh sprout \
    && mkdir -p /workspace \
    && chown 1000:1000 /workspace

# The binary is the release's own archive, checked against the release's
# SHA256SUMS before it is installed.
RUN set -eu; \
    test -n "${SPROUT_VERSION}" || { echo "build with --build-arg SPROUT_VERSION=vX.Y.Z" >&2; exit 1; }; \
    arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "${arch}" in amd64|arm64) ;; *) echo "unsupported architecture ${arch}" >&2; exit 1 ;; esac; \
    asset="sprout-linux-${arch}.tar.gz"; \
    base="https://github.com/${SPROUT_RELEASE_REPO}/releases/download/${SPROUT_VERSION}"; \
    tmp="$(mktemp -d)"; \
    cd "${tmp}"; \
    curl -fsSL --retry 3 -o "${asset}" "${base}/${asset}"; \
    curl -fsSL --retry 3 -o SHA256SUMS "${base}/SHA256SUMS"; \
    awk -v a="${asset}" '$2 == a' SHA256SUMS > expected.sum; \
    test -s expected.sum || { echo "${asset} is not listed in SHA256SUMS" >&2; exit 1; }; \
    sha256sum -c expected.sum; \
    tar -xzf "${asset}"; \
    install -m 0755 "sprout-linux-${arch}" /usr/local/bin/sprout; \
    cd /; \
    rm -rf "${tmp}"; \
    /usr/local/bin/sprout version

# Both scripts are committed executable and COPY keeps the mode.
COPY entrypoint.sh /usr/local/bin/sprout-workspace-entrypoint
COPY git-credential-sprout /usr/local/bin/git-credential-sprout

ENV HOME=/home/sprout
USER 1000:1000
WORKDIR /workspace
EXPOSE 56000

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/sprout-workspace-entrypoint"]

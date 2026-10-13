# syntax=docker/dockerfile:1
#
# leptostack CLI container image: the static binary plus every external tool the
# CLI shells out to (kubectl, flux, vcluster, helm, git, openssl, bash).
# Published as ghcr.io/<owner>/leptostack-cli:<tag> (the repository's
# organization) by scripts/release.sh.

FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w \
        -X github.com/micronisus/leptostack-cli/internal/version.Version=${VERSION} \
        -X github.com/micronisus/leptostack-cli/internal/version.Commit=${COMMIT} \
        -X github.com/micronisus/leptostack-cli/internal/version.Date=${DATE}" \
      -o /out/leptostack .

FROM debian:bookworm-slim AS runtime

# Tool versions are pinned so images are reproducible; override with --build-arg.
ARG TARGETARCH=amd64
ARG KUBECTL_VERSION=v1.31.3
ARG FLUX_VERSION=2.8.7
ARG HELM_VERSION=v3.16.4
ARG VCLUSTER_VERSION=0.37.3

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash ca-certificates curl git openssl tar \
 && rm -rf /var/lib/apt/lists/*

# kubectl
RUN curl -fsSL -o /usr/local/bin/kubectl \
      "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" \
 && chmod +x /usr/local/bin/kubectl

# helm
RUN curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" \
      | tar -xz -C /tmp \
 && mv "/tmp/linux-${TARGETARCH}/helm" /usr/local/bin/helm \
 && chmod +x /usr/local/bin/helm

# vcluster (>= 0.37.2)
RUN curl -fsSL -o /usr/local/bin/vcluster \
      "https://github.com/loft-sh/vcluster/releases/download/v${VCLUSTER_VERSION}/vcluster-linux-${TARGETARCH}" \
 && chmod +x /usr/local/bin/vcluster

# flux
RUN curl -fsSL "https://github.com/fluxcd/flux2/releases/download/v${FLUX_VERSION}/flux_${FLUX_VERSION}_linux_${TARGETARCH}.tar.gz" \
      | tar -xz -C /tmp \
 && mv /tmp/flux /usr/local/bin/flux \
 && chmod +x /usr/local/bin/flux

COPY --from=build /out/leptostack /usr/local/bin/leptostack

ENTRYPOINT ["/usr/local/bin/leptostack"]

# LeptoStack Bootstrap

A CLI tool for managing the LeptoStack local development environment. It provisions a minikube cluster, bootstraps FluxCD for GitOps-based deployments, and configures local DNS and TLS trust.

## Installation

Install the latest release:

```bash
curl -fsSL -o leptostack.sh https://github.com/micronisus/leptostack-cli/releases/latest/download/leptostack.sh
curl -fsSL -o leptostack.sh.sha256 https://github.com/micronisus/leptostack-cli/releases/latest/download/leptostack.sh.sha256
sha256sum -c leptostack.sh.sha256
chmod +x leptostack.sh
sudo mv leptostack.sh /usr/local/bin/leptostack
```

Alternatively, clone the repository and install the script from source:

```bash
git clone https://github.com/micronisus/leptostack-cli.git
cd leptostack-cli
chmod +x leptostack.sh
sudo ln -sf "$(pwd)/leptostack.sh" /usr/local/bin/leptostack
```

## Prerequisites

- **OS:** Fedora, Ubuntu, or Arch Linux
- **Hardware:** Minimum 32GB RAM and 8 CPU cores (recommended)
- **minikube:** >= 1.38.1 ([Install](https://minikube.sigs.k8s.io/docs/start/))
- **Flux CLI:** >= 2.8.7 ([Install](https://fluxcd.io/flux/installation/))
- **NetworkManager** configured with the dnsmasq plugin (required for `update-dns`)


## Installation on Fedora

- Check for kubectl if not ask the user to install
- Ask the user to configure Podman Driver without CPU or Memory
- Check if auth details for docker.io have been configured else give a warning
- Configure Minikube for Podman (add sudoers file for NOPASSWD)
- Update the ulimit for Podman (/etc/containers/containers.conf)
- Update 

## Usage

```bash
leptostack <command>
```

```bash
leptostack version
```

## Quick Start

```bash
# First-time setup
leptostack configure

# Start the environment
leptostack start

# Configure local DNS (after all kustomizations are ready)
leptostack update-dns

# Trust the internal CA certificate
leptostack add-trust

# Check status
leptostack status

# Stop when done
leptostack stop
```

## Commands

### `configure`

Interactive setup wizard that:

1. Verifies minikube and flux CLI versions meet minimum requirements
2. Checks system resources (RAM and CPU)
3. Prompts for the LeptoStack development environment Git repository URL
4. Parses and confirms hostname, owner, and repo from the URL
5. Prompts for branch name (default: `main`) and cluster path (default: `clusters/hadeed`)
6. Prompts for Git server type (GitHub, Gitea/Forgejo, or GitLab)
7. Prompts for a Personal Access Token (PAT)
8. Configures minikube CPU and memory allocation (default: 8 CPUs, 24576MB)

Configuration is saved to `~/.config/leptostack/config`.

### `start`

Starts the development environment:

1. Starts minikube
2. Enables addons: `ingress`, `ingress-dns`, `registry`, `metrics-server`
3. Runs `flux bootstrap` against the configured Git repository to set up GitOps

### `stop`

Stops the minikube cluster gracefully.

### `status`

Displays the current state of:

- Minikube cluster
- Flux version
- All Flux kustomizations and their reconciliation status

### `reset`

Deletes the minikube cluster and re-runs the `start` workflow. Prompts for confirmation before proceeding.

### `reconcile`

Triggers an immediate reconciliation of the `flux-system` kustomization with its source.

### `events`

Watches all Kubernetes events across all namespaces in real-time.

### `update-dns`

Configures local DNS resolution so that `*.test` domains resolve to the minikube IP:

1. Verifies all Flux kustomizations are ready
2. Checks that NetworkManager is configured with the dnsmasq plugin
3. Writes a dnsmasq config pointing `.test` to the minikube IP
4. Restarts NetworkManager

### `add-trust`

Installs the cluster's internal CA certificate into the system trust store:

1. Verifies all Flux kustomizations are ready
2. Extracts the CA certificate from the `internal-ca-secret` in the `cert-manager` namespace
3. Installs it to the OS trust store (method varies by distro)

After running this command, restart your browsers for the certificate to take effect.

### `port-forward <service>`

Creates a port-forward to a cluster service. Supported services:

- **openbao** — Verifies all Flux kustomizations are ready, displays the OpenBao root token, then forwards `localhost:8200` to the OpenBao service.

```bash
leptostack port-forward openbao
```

### `version`

Prints the installed leptostack version:

```bash
leptostack version
# leptostack 0.1.0
```

## Configuration

All configuration is stored at `~/.config/leptostack/config` (mode `600`). It includes:

| Variable | Description |
|----------|-------------|
| `GIT_SERVER` | Git provider: `github`, `gitea`, or `gitlab` |
| `GIT_PAT` | Personal Access Token for the Git server |
| `GIT_HOSTNAME` | Hostname of the Git server |
| `GIT_OWNER` | Repository owner/organization |
| `GIT_REPO` | Repository name |
| `GIT_BRANCH` | Branch to bootstrap from |
| `CLUSTER_PATH` | Path within the repo to the cluster manifests |
| `MINIKUBE_CPUS` | Number of CPUs allocated to minikube |
| `MINIKUBE_MEMORY` | Memory in MB allocated to minikube |

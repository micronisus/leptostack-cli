# LeptoStack Bootstrap

A CLI tool for managing the LeptoStack local development environment. It
provisions a development cluster (a local **minikube** cluster or a **vcluster**
running on an existing Kubernetes cluster), bootstraps FluxCD for GitOps-based
deployments, manages Flux sources and secrets, pauses/restores kustomizations,
and configures local DNS and TLS trust.

The CLI is a single static Go binary (one per platform) with a kubectl-inspired
command grammar. See [Migrating from the shell CLI](#migrating-from-the-shell-cli)
if you are coming from the older shell scripts.

## Installation

On Linux and macOS, install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/micronisus/leptostack-cli/main/install.sh | sh
```

The installer downloads the right archive, verifies its `sha256` against the
published `checksums.txt`, and installs the binary to `/usr/local/bin`.

On Windows (Windows PowerShell 5.1 or PowerShell 7), install with `install.ps1`:

```powershell
irm https://raw.githubusercontent.com/micronisus/leptostack-cli/main/install.ps1 | iex
```

It downloads the `leptostack_windows_<arch>.zip` release asset, verifies its
`sha256`, installs `leptostack.exe` to `%LOCALAPPDATA%\leptostack\bin`, and adds
that directory to your **user** PATH (reopen your terminal afterwards). No
elevation is required.

Color is auto-detected from the terminal; on legacy Windows consoles that do not
render ANSI escapes, pass `--no-color` (or redirect output) for clean tables.

From source:

```bash
git clone https://github.com/micronisus/leptostack-cli.git
cd leptostack-cli
make build          # produces bin/leptostack
make install        # installs to /usr/local/bin
```

Shell completions:

```bash
leptostack completion bash > /etc/bash_completion.d/leptostack   # bash
leptostack completion zsh  > "${fpath[1]}/_leptostack"           # zsh
leptostack completion fish > ~/.config/fish/completions/leptostack.fish
leptostack completion powershell | Out-String | Invoke-Expression
```

## Prerequisites

- **OS:** Fedora or Arch Linux give full DNS/CA host integration. Windows 10/11
  is supported for the core workflow; DNS is guided-only (see
  [DNS on Windows](#dns-on-windows)) and the CA is installed into the
  current-user trust store. macOS builds and runs the core workflow but has no
  DNS/CA host integration. `kubectl`, `git` and `flux >= 2.8.7` are always
  required.
- **minikube:** `minikube >= 1.38.1` plus podman on Linux (Windows uses the
  Docker/WSL2/Hyper-V driver; macOS Docker/HyperKit). Hardware: minimum 32GB RAM
  and 8 CPU cores (recommended).
- **vcluster:** `vcluster >= 0.37.1` and access to an existing Kubernetes
  cluster (the *host* cluster). The host must expose an ingress/load-balancer
  address reachable from this machine; `dns update` discovers it from the
  `apisix-gateway` Service.

Run `leptostack doctor` to check all of the above.

## Quick start

```bash
leptostack config init     # guided wizard (adds a named cluster)
leptostack start
leptostack dns update
leptostack ca install
leptostack status
leptostack stop
```

### DNS on Windows

The cluster certificate and APISIX routes cover `*.<domain>`, but Windows has no
wildcard hosts support and the CLI does not modify the system. `dns update`
therefore prints ready-to-paste `hosts` lines (the base domain plus the known
service prefixes `sso`, `che`, `dashboard`, `db`, `design`, `git`, `llm`,
`registry`, and the `<leptostack-name>-<service>` form when a hostname was
overridden), the detected gateway IP, and the `ipconfig /flushdns` step. Add them
to `%SystemRoot%\System32\drivers\etc\hosts` as Administrator. `dns get` reports
that nothing is managed automatically and `dns delete` prints cleanup guidance;
neither touches the OS.

### CA trust on Windows

`ca install` runs `certutil -user -addstore -f Root <cert>`, which trusts the CA
for the current user only (no UAC). `ca status` reports `installed`/`absent` from
a thumbprint lookup in the current-user Root store. `ca export` and CA
generation are unchanged.

## Command grammar

Two rules define the command surface:

1. **Object operations** use `leptostack <verb> <resource> [name]`, with optional
   kubectl-style `resource/name` compound syntax:

   ```bash
   leptostack reconcile kustomization leptostack-base
   leptostack reconcile kustomization/leptostack-base     # same
   leptostack create source git my-repo --url git@github.com:me/repo.git
   leptostack disable kustomization apps-devops
   leptostack logs port-forward openbao
   ```

2. **Cluster lifecycle** uses bare verbs (`start`, `stop`, `restart`, `status`,
   `connect`), acting on the selected cluster; `delete cluster <name>` removes the
   named one.

Lists are plural, single objects singular, all lowercase. Command names contain
hyphens only where already established (`port-forward`).

### Command tree

```text
leptostack
├── version [--check]                    # client version, optional update check
├── upgrade [--check]                    # self-update from GitHub releases
├── completion bash|zsh|fish|powershell
├── doctor [-o json]                     # prerequisites: kubectl/flux/minikube/…
├── start | stop | restart | status | connect
├── get
│   ├── clusters | cluster               # all/single configured clusters + live state
│   ├── kustomizations | sources | secrets | context | port-forwards | events
├── create
│   ├── source git|helm|bucket|oci <name>
│   └── secret git|docker-registry|tls|opaque <name>
├── delete
│   ├── cluster
│   ├── source <type>/<name> | secret <name> | port-forward <service>|--all
├── reconcile kustomization|source [name]
├── wait kustomization <name>|all [--ready] [--timeout]
├── bootstrap | sync cluster | sync flux
├── enable|disable kustomization <name>|all      # suspend/resume aliases
├── dns update|get|delete
├── ca status|install|export
├── registry get|set-credentials|delete
├── port-forward <service> [-f|-d] [--local-port N] [--address A]
├── logs port-forward <service> [-f]
└── config init|view|set|unset|use-context
```

`port-forward` services: `openbao`, `rabbitmq`, `postgres`, `valkey`, `flowable`,
`greenmail`, `seaweedfs` (plus `all`).

## Global flags

| Flag | Purpose |
|---|---|
| `-c, --cluster <name>` | Select the configured cluster (default: `current-context`) |
| `--context <name>` | Override the kubectl context (default: derived per provider) |
| `-n, --namespace <ns>` | Namespace scope where applicable |
| `--kubeconfig <path>` | kubeconfig to use (default: standard resolution) |
| `-o, --output <fmt>` | `wide`, `json`, `yaml`, `name`, or `human` (default) |
| `-y, --yes` | Skip confirmation prompts (required in non-interactive contexts) |
| `--dry-run` | Client-side validation only; no side effects |
| `--timeout <duration>` | Command-level timeout (defaults per command) |
| `-v, --verbose <int>` | Log level 0–4 |
| `--no-color` | Disable ANSI color (auto when output is not a TTY) |
| `--non-interactive` | Fail instead of prompting when a required value is missing |

Human output never mixes with machine output on stdout; warnings and progress go
to stderr, so `-o json | jq` is always safe.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic runtime failure |
| 2 | usage error (bad flags/args) |
| 3 | configuration missing or invalid |
| 4 | prerequisite tool missing or too old (kubectl, flux, minikube, vcluster) |
| 5 | timeout while waiting for a condition |
| 6 | cluster not running/not reachable |

## Scriptable usage

```bash
# Machine-readable status
leptostack status -o json
leptostack get kustomizations -o json | jq -r '.[] | select(.ready==false) | .metadata.name'

# Stable context for scripts
ctx=$(leptostack get context -o name)
kubectl --context "$ctx" get pods -A

# Wait for readiness with a real exit code
leptostack wait kustomization all --ready --timeout 20m

# Non-interactive delete (the cluster name is required)
leptostack delete cluster local --yes

# Credentials without a prompt or disk round-trip
op read "op://LeptoStack/ghcr/password" \
  | leptostack registry set-credentials --repo ghcr.io --user me --password-stdin
```

## Multiple clusters

Clusters are named entries in the config. Select one with `-c/--cluster` or
`leptostack config use-context <name>`; list them with `get clusters`.

```bash
leptostack config init --cluster-name devops --provider vcluster ...   # adds a named cluster
leptostack start -c local          # minikube
leptostack start -c devops         # vcluster on the host cluster — runs concurrently
leptostack get clusters            # both clusters, live state
```

State is per cluster under `$XDG_RUNTIME_DIR/leptostack/<cluster>/` on Linux and
macOS (`%LOCALAPPDATA%\leptostack\<cluster>\` on Windows): port-forward
PID/state/log files and the cached vcluster context, so two clusters never
collide on bookkeeping. DNS rules are per cluster on Linux
(`/etc/NetworkManager/dnsmasq.d/leptostack-<cluster>.conf`); Windows is guided
only. The internal CA is shared host-wide, so `ca install` stays a
once-per-host action. Local ports are
the one true collision: two live clusters cannot both bind `5432`, so
`port-forward` fails fast and `--local-port`/`--address` resolve it.

## Phase 2: repository automation

The GitOps repository is optional to specify up front. During `config init` the
repository prompt defaults to **`auto`**:

```bash
leptostack config init          # press Enter at the repository prompt to accept "auto"
```

With `auto`, no repository details are stored yet. On `leptostack start` the
repository is created and the config is updated with the real values:

1. verifies the CLI for the selected Git server — `gh` (github), `fj`
   (gitea/forgejo), `glab` (gitlab) — and fails with exit code 4 if it is
   missing;
2. creates a uniquely named repository (`<cluster>-gitops`) under your personal
   account, **never replacing an existing one** (generated names are probed
   until unused);
3. mints a **push/pull** token scoped to the new repository;
4. mints a **read-only** token covering the private **leptostack-base** repository
   (the embedded cluster template's only external source);
5. writes the owner, repository and tokens into the config (`git.pat`,
   `base.pat`) and continues with the template sync and Flux bootstrap.

To use an existing repository instead, pass its URL and skip the automation. A
PAT is then required for **both** the GitOps repository and the private
**leptostack-base** repository, so `config init` prompts for two separate PATs.
Non-interactively, supply them via `--git-pat-stdin`/`--git-pat` (or
`LEPTOSTACK_GIT_PAT`) and `--base-pat-stdin` (or `LEPTOSTACK_BASE_PAT`); the
command fails if either is missing.

```bash
leptostack config init --repo-url https://github.com/me/my-gitops
# prompts "Enter the GitOps repository PAT" then "Enter the leptostack-base repository PAT"

printf 'ghp_gitops_token\nghp_base_token\n' \
  | leptostack config init --repo-url https://github.com/me/my-gitops \
      --git-pat-stdin --base-pat-stdin
```

When the git server uses a private (enterprise) CA, point `config init` at the CA
bundle with `--git-ca-file` (or `LEPTOSTACK_GIT_CA_FILE`). The path is stored as
`git.caFile` and used for **both** repositories:

```bash
leptostack config init --repo-url https://git.example.com/me/my-gitops \
  --git-ca-file /etc/pki/enterprise-ca.pem
```

With a CA configured, `flux bootstrap` is run with `--ca-file`, and on
`leptostack start` the CA bundle is added as a `ca.crt` key to the
`leptostack-base` Secret in `flux-system` (the same Secret that holds the base
PAT), so the base GitRepository trusts the server. No change to the
GitRepository manifest is needed.

Token minting is capability-aware, because the hosts differ:

| Server | CLI | Repository | Push/pull token | Read-only token |
|---|---|---|---|---|
| `github` | `gh` | `gh api` | reuses the logged-in `gh` token (GitHub cannot mint PATs) | same token |
| `gitea` | `fj` | `fj repo create` | Forgejo API, scoped `read:repository,write:repository` | Forgejo API, scoped `read:repository` |
| `gitlab` | `glab` | `glab api` | `glab token create` project token (`write_repository`) | group token (`read_repository`), or per-project tokens when namespaces differ |

Forgejo/Gitea cannot mint tokens through `fj`, so the API path needs a
management token available to `start`:

```bash
export LEPTOSTACK_FORGE_TOKEN=...   # a Forgejo token allowed to create user tokens
leptostack start
```

For GitHub the automation reuses the `gh` CLI's token (it must carry the `repo`
scope) and notes that GitHub does not support minting PATs non-interactively.

## Flux sources and secrets

Adding a source is one validated command instead of hand-written YAML:

```bash
# Private Helm chart for a module under development
leptostack create secret git helm-private --username me --password-stdin < token.txt
leptostack create source helm my-charts --url https://charts.example.com --secret-ref helm-private

# Second GitRepository for another Git source
leptostack create source git internal --url https://github.com/me/internal-manifests.git \
  --branch dev --interval 1m --git

# Inspect and clean up
leptostack get sources --type helm
leptostack get secrets -n flux-system          # values redacted by default
leptostack delete source git/fork --git
```

Two storage modes exist: `--direct` (default) applies the manifest to the
cluster immediately, while `--git` writes it into the GitOps repository and
pushes it. Secrets are **always** applied to the cluster and are never written to
the repository; `--password-stdin` keeps credentials off the command line. The
CLI knows the canonical key names per kind and validates `--secret-ref` against
existing secrets at creation time. `--dry-run` prints the exact manifest.

## Enable/disable kustomizations

`enable`/`disable` set Flux's `spec.suspend` (the mechanism behind `flux
suspend`/`resume`), addressed by name or `all`:

```bash
leptostack disable kustomization local-module-company   # pause a heavy module
leptostack disable kustomization all                    # pause everything (excludes flux-system)
leptostack enable kustomization all                     # restore
```

Disabled kustomizations keep their applied objects; Flux suspends
*reconciliation*, not resources. Suspended state is surfaced in
`get kustomizations` and `status`. This is a cluster-side override: a redeploy
from Git (`sync cluster`) restores the committed state.

## Configuration

The typed YAML config lives at `$LEPTOSTACK_CONFIG` (default
`~/.config/leptostack/config.yaml` on Linux/macOS and
`%APPDATA%\leptostack\config.yaml` on Windows, mode `0600`):

```yaml
apiVersion: v1
kind: LeptostackConfig
current-context: local

clusters:
  local:
    provider: minikube
    clusterName: local
    leptostackName: portal
    domain: alice-leptostack.test
    minikube:
      cpus: 8
      memoryMB: 24576
    git:
      server: github            # github | gitea | gitlab
      hostname: ""
      owner: micronisus
      repository: leptostack-fluxcd
      branch: main
      clusterPath: clusters/{cluster}
      # pat: read from LEPTOSTACK_GIT_PAT when absent
      # caFile: /etc/pki/enterprise-ca.pem   # PEM CA bundle for a private git server
    registry:
      repository: ghcr.io
      # credentials: user:pass — from LEPTOSTACK_REGISTRY_CREDENTIALS when absent
    base:
      url: https://github.com/leptostack-fluxcd/leptostack-base.git
      branch: main
      # pat: read-only token for the private leptostack-base repo; from
      # LEPTOSTACK_BASE_PAT (or RESOURCES_GIT_PAT) when absent
  devops:
    provider: vcluster
    clusterName: devops
    leptostackName: devops
    domain: devops-minikube.test
    vcluster:
      hostContext: kind-baremetal
      hostNamespace: alice
      # metricsServer: false   # disable the vcluster metrics-server integration
      # oidc: false            # disable vcluster API server OIDC authentication
      # apiServerServiceType: ClusterIP   # API server Service type (default LoadBalancer)
    # git/registry/base are per cluster too: devops carries its own copies
    # (e.g. a different clusterPath or base repository). Nothing is shared.

preferences:
  autoUpgradeCheck: true
```

- **Required keys and enums are validated.** Invalid config fails fast with exit
  code 3 and a message naming the exact key path.
- **`git`, `registry` and `base` are cluster-scoped.** There are no global
  sections. `config set`/`config unset` write these to the selected cluster: with
  `-c/--cluster` the value targets that cluster, otherwise the current-context
  cluster, so `leptostack -c local config set base.pat <token> --secret` updates
  `clusters.local.base.pat`. An explicit `clusters.<name>.…` key always targets
  that cluster. Unset values fall back to defaults (git `server`/`branch`,
  registry repository, base url/branch) at resolve time; env vars still override.
- **Secrets via env first.** `LEPTOSTACK_GIT_PAT`,
  `LEPTOSTACK_REGISTRY_CREDENTIALS` and `LEPTOSTACK_BASE_PAT` (or
  `RESOURCES_GIT_PAT`) take precedence over the file, so CI never writes secrets
  to disk. `config view` redacts them unless `--show-secrets`.
- **No code execution.** Unlike the shell config, the YAML cannot execute
  anything when loaded.
- **vcluster feature toggles.** `vcluster.metricsServer` and `vcluster.oidc`
  default to `true`. Disable them at `config init` with
  `--disable-vcluster-metrics` / `--disable-vcluster-oidc`, or later with
  `config set clusters.<name>.vcluster.metricsServer false`. With OIDC disabled
  the API server gets no `--oidc-*` flags and the OIDC CA secret and
  ClusterRoleBinding are not created. The values file is applied when the
  vcluster is created, so recreate an existing cluster
  (`leptostack delete cluster <name> && leptostack start`) for a toggle change to take
  effect.
- **vcluster API server Service type.** `vcluster.apiServerServiceType` sets the
  Service type of the vcluster API server and defaults to `LoadBalancer`. Set it
  to `ClusterIP` at `config init` with `--vcluster-api-server-service-type
  clusterip`, or later with
  `config set clusters.<name>.vcluster.apiServerServiceType clusterip`. Like the
  feature toggles, the values file is applied at creation time.
- **Config commands:** `config init` (wizard, `--non-interactive`, `--from-file`),
  `config view [--show-secrets]`, `config set <key> <value> [--secret]`,
  `config unset <key>`, `config use-context <cluster>`. `config init` always adds
  a new cluster (the name comes from `--cluster-name` or the prompt); it rejects a
  name that already exists and, interactively, asks for a new one. `-c/--cluster`
  is not applicable to `config init`. `config set`/`unset` target the `-c/--cluster`
  cluster for the cluster-scoped sections, falling back to the current-context
  cluster. `delete cluster <name>` takes the cluster name as an argument;
  `-c/--cluster` is not applicable there either.

## Migrating from the shell CLI

On first run, if `~/.config/leptostack/config` (the old KV file) exists and no
YAML config does, it is imported as a single cluster named after its
`CLUSTER_NAME` value and set as `current-context`. The original file is kept.
Single-cluster users notice nothing.

The old flat command names were removed; use the current grammar:

| Old shell command | Current command |
|---|---|
| `configure` | `config init` |
| `reset` | `delete cluster <name>` |
| `reconnect` | `connect` |
| `rebootstrap` | `bootstrap` |
| `reconcile` (bare) | `reconcile kustomization flux-system --with-source` |
| `update-flux` | `sync flux` |
| `events` | `get events -w` |
| `update-dns` | `dns update` |
| `add-trust` | `ca install` |
| `set-registry-creds` | `registry set-credentials` |
| `get-context` | `get context -o name` |
| `port-forward <svc>` | foreground by default; `-d` detaches |
| `port-forward stop` | `delete port-forward --all` |

## Development

```bash
make build      # build with version/commit/date ldflags
make test       # tests
make test-race  # tests with the race detector
make vet        # go vet
make lint       # golangci-lint
make ci         # exactly what CI runs (format/vet/build/test/shellcheck)
```

Tests use a fake-binary harness (`internal/testutil`): shim scripts for
`kubectl`, `flux`, `minikube`, `vcluster` and `git` record their argv and emit
canned responses, so command orchestration, provider branches and error paths
are covered without a real cluster. See `cmd/cmd_test.go`.

The harness is POSIX-only (`#!/bin/sh` plus `HOME`/`XDG_RUNTIME_DIR`), so
`cmd/`/`testutil`-based tests are not run on Windows. The Windows CI job
compiles, vets and smoke-tests the binary instead, and `scripts/ci.sh`
cross-compiles and vets `GOOS=windows`/`GOOS=darwin` on every run.

The repository layout follows the RFC: cobra commands in `cmd/`, and focused
packages under `internal/` (`config`, `kubernetes`, `flux`, `providers`,
`gitops`, `clustertemplate`, `sources`, `secrets`, `kustomizations`, `ca`, `dns`,
`portforward`, `registry`, `execx`, `upgrade`, `ui`, `version`).

## CI/CD

The pipeline runs on **both GitHub Actions and Forgejo Actions** from a single
source of truth: portable scripts under `scripts/` that the workflow files only
wrap. Nothing in the scripts depends on a forge-specific action, so one change
covers both platforms.

```text
scripts/
  ci.sh       # gofmt check, go vet, go build, windows/darwin cross-build+vet, go test -race, shellcheck, optional golangci-lint
  release.sh  # build platforms with GoReleaser, build+push the container image, then publish to the running forge

.github/workflows/    # GitHub Actions: ci.yml (linux + windows), release.yml
.forgejo/workflows/   # Forgejo Actions: ci.yml, release.yml (linux-only)
```

- **CI** (`ci.yml`, on push and pull requests): checks out the repo, sets up Go,
  and runs `bash scripts/ci.sh`, which also cross-compiles and vets the Windows
  and Darwin targets on the Linux runner. Run it locally with `make ci`. A
  second **Windows job** on `windows-latest` builds, vets and smoke-tests the
  binary (`version`, `completion powershell`, `--help`). Forgejo stays
  Linux-only; its `scripts/ci.sh` run still covers Windows/Darwin compilation.
- **Release** (`release.yml`, on `v*` tags): checks out with full history,
  installs GoReleaser from the Go module proxy, builds archives + `checksums.txt`
  for linux/darwin/windows × amd64/arm64, and publishes the release. On GitHub it
  also builds the `Dockerfile` and pushes the image to
  `ghcr.io/<owner>/leptostack-cli` — the same organization as the repository —
  authenticated with the workflow's scoped `GITHUB_TOKEN` (`packages: write`).
  Override the image with `IMAGE_REPOSITORY=<ref>` or skip it with
  `PUBLISH_IMAGE=0`. Run it locally with `RELEASE_VERSION=x.y.z make release`.

`scripts/release.sh` detects the forge and publishes accordingly:

| Forge | Detection | Publisher | Token |
|---|---|---|---|
| GitHub | `GITHUB_ACTIONS=true` and `GITHUB_SERVER_URL` is github.com | `gh release create`/`upload` | `GH_TOKEN` / `GITHUB_TOKEN` |
| Forgejo / Gitea | otherwise (Forgejo Actions) | REST API via `curl` + `jq` | `FORGEJO_TOKEN` / `GITEA_TOKEN` / `GITHUB_TOKEN` |

Forgejo Actions resolves `actions/checkout` and `actions/setup-go` through its
action proxy (`code.forgejo.org/actions/*`); if an instance cannot resolve the
`actions/` prefix, replace the `uses:` values with the fully qualified
`https://code.forgejo.org/actions/...@vN` form. The release job needs `curl` and
`jq` on the runner (both are present on `ubuntu-latest` images).

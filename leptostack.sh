#!/bin/bash

set -euo pipefail

CONFIG_DIR="${HOME}/.config/leptostack"
CONFIG_FILE="${CONFIG_DIR}/config"

MINIKUBE_MIN_VERSION="1.38.1"
FLUX_MIN_VERSION="2.8.7"
SCRIPT_VERSION="dev"

RELEASES_URL="https://github.com/micronisus/leptostack-cli/releases"
VERSION_FILE="${CONFIG_DIR}/latest_version"

TEMPLATE_GIT_URL_DEFAULT="https://github.com/leptostack-fluxcd/cluster-template.git"
TEMPLATE_GIT_BRANCH_DEFAULT="main"

# --- Utility Functions ---

detect_distro() {
    local distro=""
    if [[ -f /etc/os-release ]]; then
        # shellcheck source=/etc/os-release
        source /etc/os-release
        case "$ID" in
            fedora) distro="fedora" ;;
            arch)   distro="arch" ;;
        esac
    fi
    if [[ -z "$distro" ]]; then
        echo "Error: Unsupported Linux distribution."
        echo "Supported distributions: Fedora, Arch Linux."
        exit 1
    fi
    echo "$distro"
}

DISTRO=$(detect_distro)

check_kubectl_context() {
    local current_context
    current_context=$(kubectl config current-context 2>/dev/null || echo "")
    if [[ "$current_context" != "minikube" ]]; then
        echo "Error: kubectl context is not set to minikube (current: '${current_context:-<none>}')."
        echo "Please switch context with: kubectl config use-context minikube"
        exit 1
    fi
}

version_ge() {
    # Returns 0 if $1 >= $2 (semver comparison)
    printf '%s\n%s\n' "$2" "$1" | sort -V -C
}

# --- Cluster Template Sync ---

build_clone_url() {
    local owner="$1" repo="$2"
    echo "https://oauth2:${GIT_PAT}@${GIT_HOSTNAME}/${owner}/${repo}.git"
}

build_template_clone_url() {
    local url="${TEMPLATE_GIT_URL:-$TEMPLATE_GIT_URL_DEFAULT}"
    url="${url#https://}"
    url="${url#http://}"
    echo "https://oauth2:${RESOURCES_GIT_PAT}@${url}"
}

# Configures git identity, stages all changes, and commits + pushes them to the
# origin repository. Usage: commit_and_push <message> <no_changes_message> [push_args...]
# When push_args are omitted, pushes $GIT_BRANCH; otherwise they are passed to
# `git push origin` verbatim (e.g. "-u HEAD:<branch>" when creating a new branch).
commit_and_push() {
    local message="$1" no_changes_message="$2"
    shift 2

    git config user.name "LeptoStack Bootstrap"
    git config user.email "bootstrap@leptostack.local"
    git add -A

    if git diff --cached --quiet; then
        echo "$no_changes_message"
        return 0
    fi

    git commit -m "$message"
    if [[ $# -gt 0 ]]; then
        echo "Pushing changes..."
        git push origin "$@"
    else
        echo "Pushing changes to ${GIT_BRANCH}..."
        git push origin "$GIT_BRANCH"
    fi
    echo "Changes pushed successfully."
}

sync_cluster_template() {
    local force="${1:-}"
    TEMPLATE_GIT_URL="${TEMPLATE_GIT_URL:-$TEMPLATE_GIT_URL_DEFAULT}"
    TEMPLATE_GIT_BRANCH="${TEMPLATE_GIT_BRANCH:-$TEMPLATE_GIT_BRANCH_DEFAULT}"

    local repo_clone_url
    repo_clone_url=$(build_clone_url "$GIT_OWNER" "$GIT_REPO")

    # Skip when the branch already exists, unless forced (reset). Checking the
    # branch (not HEAD) also covers repos whose default branch differs from
    # GIT_BRANCH, where HEAD can dangle while the branch has content.
    local refs
    refs=$(git ls-remote "$repo_clone_url" "refs/heads/$GIT_BRANCH" 2>/dev/null) || true
    if [[ -z "$force" && -n "$refs" ]]; then
        echo "Repository ${GIT_OWNER}/${GIT_REPO} already contains content; skipping cluster template sync."
        return 0
    fi

    local tmp_dir orig_dir
    tmp_dir=$(mktemp -d /tmp/leptostack-template-XXXXXX)
    orig_dir=$(pwd)
    trap 'rm -rf "$tmp_dir"' EXIT

    echo "Cloning cluster template ${TEMPLATE_GIT_URL}..."
    git clone --branch "$TEMPLATE_GIT_BRANCH" --depth 1 "$(build_template_clone_url)" "$tmp_dir/template"
    # The template is only read locally; drop the origin so the PAT never
    # persists in the cloned repository's remote configuration.
    git -C "$tmp_dir/template" remote remove origin

    echo "Cloning repository ${GIT_OWNER}/${GIT_REPO}..."
    if [[ -n "$refs" ]]; then
        git clone --branch "$GIT_BRANCH" --depth 1 "$repo_clone_url" "$tmp_dir/repo"
    else
        git clone "$repo_clone_url" "$tmp_dir/repo"
    fi

    cd "$tmp_dir/repo"

    if [[ -z "$refs" ]]; then
        git checkout -B "$GIT_BRANCH"
    fi

    echo "Replacing repository contents with the cluster template..."
    find . -mindepth 1 -maxdepth 1 ! -name '.git' -exec rm -rf {} +
    tar -C "$tmp_dir/template" --exclude='.git' -cf - . | tar -C "$tmp_dir/repo" -xf -

    if [[ -n "$refs" ]]; then
        commit_and_push "Sync cluster template from ${TEMPLATE_GIT_URL}" \
            "Repository is already in sync with the cluster template; no changes to push."
    else
        commit_and_push "Sync cluster template from ${TEMPLATE_GIT_URL}" \
            "Repository is already in sync with the cluster template; no changes to push." \
            "-u" "HEAD:${GIT_BRANCH}"
    fi

    # Restore the original working directory before removing the temp directory;
    # otherwise the shell is left inside a deleted directory and any subsequent
    # command that calls getcwd() (e.g. flux bootstrap) fails.
    cd "$orig_dir"
    trap - EXIT
    rm -rf "$tmp_dir"
}

# --- Update Check ---

download_latest_version() {
    # Fetches the latest release version from GitHub and stores it locally.
    mkdir -p "$CONFIG_DIR"
    local latest
    latest=$(curl -sL --fail "https://api.github.com/repos/micronisus/leptostack-cli/releases/latest" \
        | grep -oP '"tag_name":\s*"\K[^"]+' | head -n1)
    if [[ -z "$latest" ]]; then
        echo "Warning: Could not fetch the latest version from GitHub."
        return 1
    fi
    printf '%s\n' "$latest" > "$VERSION_FILE"
    printf '%s\n' "$latest"
}

# Returns 0 if up to date, 1 if an update is available, 2 if the version is unknown.
check_for_updates() {
    # Pass "true" to force re-downloading the version file when it already exists.
    local refresh="${1:-false}"
    local latest=""

    if [[ "$refresh" == "true" ]] || [[ ! -f "$VERSION_FILE" ]]; then
        echo "Checking for the latest version..."
        latest=$(download_latest_version) || true
    fi
    if [[ -z "$latest" && -f "$VERSION_FILE" ]]; then
        latest=$(cat "$VERSION_FILE")
    fi

    if [[ -z "$latest" ]]; then
        echo "Warning: Could not determine the latest version of leptostack."
        return 2
    fi

    # Normalize leading "v" prefixes before comparison.
    latest="${latest#v}"

    if [[ "$SCRIPT_VERSION" == "dev" ]]; then
        # Development builds are always considered current.
        return 0
    fi
    if version_ge "${SCRIPT_VERSION#v}" "$latest"; then
        return 0
    fi

    echo
    echo "Warning: A newer version of leptostack is available."
    echo "  Current version: ${SCRIPT_VERSION}"
    echo "  Latest version:  v${latest}"
    echo "  Download it from: ${RELEASES_URL}"
    echo
    return 1
}

enforce_update() {
    # Re-checks the latest version and blocks commands that require an up-to-date CLI.
    # The `|| status=$?` suppresses errexit so a return of 2 (version unknown) is
    # not fatal and a return of 1 can print the blocking message before exiting.
    local status=0
    check_for_updates "true" || status=$?
    if [[ "$status" -eq 1 ]]; then
        echo "Error: You must update leptostack before running this command."
        echo "Update leptostack from: ${RELEASES_URL}"
        exit 1
    fi
}

load_config() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        echo "Error: Configuration not found. Please run '$0 configure' first."
        exit 1
    fi
    # shellcheck source=/dev/null
    source "$CONFIG_FILE"
}

save_config() {
    mkdir -p "$CONFIG_DIR"
    TEMPLATE_GIT_URL="${TEMPLATE_GIT_URL:-$TEMPLATE_GIT_URL_DEFAULT}"
    TEMPLATE_GIT_BRANCH="${TEMPLATE_GIT_BRANCH:-$TEMPLATE_GIT_BRANCH_DEFAULT}"
    cat > "$CONFIG_FILE" <<EOF
GIT_SERVER="${GIT_SERVER}"
GIT_PAT="${GIT_PAT}"
GIT_HOSTNAME="${GIT_HOSTNAME}"
GIT_OWNER="${GIT_OWNER}"
GIT_REPO="${GIT_REPO}"
GIT_BRANCH="${GIT_BRANCH}"
CLUSTER_PATH="${CLUSTER_PATH}"
MINIKUBE_CPUS="${MINIKUBE_CPUS}"
MINIKUBE_MEMORY="${MINIKUBE_MEMORY}"
RESOURCES_GIT_PAT="${RESOURCES_GIT_PAT}"
TEMPLATE_GIT_URL="${TEMPLATE_GIT_URL}"
TEMPLATE_GIT_BRANCH="${TEMPLATE_GIT_BRANCH}"
EOF
    chmod 600 "$CONFIG_FILE"
}

# --- Configure ---

do_configure() {
    echo "=== LeptoStack Configuration ==="
    echo

    # Check kubectl
    echo "Checking kubectl..."
    if ! command -v kubectl &>/dev/null; then
        echo "Error: kubectl is not installed."
        echo "Please install it from: https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/#install-using-native-package-management"
        exit 1
    fi
    echo "  kubectl OK"

    # Check minikube
    echo "Checking minikube..."
    if ! command -v minikube &>/dev/null; then
        echo "Error: minikube is not installed."
        echo "Please download it from: https://minikube.sigs.k8s.io/docs/start/"
        exit 1
    fi
    local mk_version
    mk_version=$(minikube version --short 2>/dev/null | sed 's/^v//')
    if ! version_ge "$mk_version" "$MINIKUBE_MIN_VERSION"; then
        echo "Error: minikube version $mk_version is too old. Minimum required: $MINIKUBE_MIN_VERSION"
        echo "Please update from: https://minikube.sigs.k8s.io/docs/start/"
        exit 1
    fi
    echo "  minikube $mk_version OK"

    # Check flux
    echo "Checking flux..."
    if ! command -v flux &>/dev/null; then
        echo "Error: flux CLI is not installed."
        echo "Please download it from: https://fluxcd.io/flux/installation/"
        exit 1
    fi
    local flux_version
    flux_version=$(flux -v 2>/dev/null | grep -oP '\d+\.\d+\.\d+')
    if ! version_ge "$flux_version" "$FLUX_MIN_VERSION"; then
        echo "Error: flux version $flux_version is too old. Minimum required: $FLUX_MIN_VERSION"
        echo "Please update from: https://fluxcd.io/flux/installation/"
        exit 1
    fi
    echo "  flux $flux_version OK"
    echo

    # Check system resources
    local sys_ram_kb sys_ram_gb sys_cpus
    sys_ram_kb=$(grep MemTotal /proc/meminfo | awk '{print $2}')
    sys_ram_gb=$((sys_ram_kb / 1024 / 1024))
    sys_cpus=$(nproc)

    if [[ $sys_ram_gb -lt 30 || $sys_cpus -lt 8 ]]; then
        echo "WARNING: LeptoStack Development Environment requires a minimum of 32GB RAM and 8 CPU cores."
        echo "  Detected: ${sys_ram_gb}GB RAM, ${sys_cpus} CPU cores"
        echo
        read -rp "Do you want to continue anyway? [y/N]: " continue_anyway
        if [[ ! "$continue_anyway" =~ ^[Yy]$ ]]; then
            echo "Aborted."
            exit 1
        fi
        echo
    fi

    # Check podman and configure as minikube driver
    echo "Checking podman..."
    if ! command -v podman &>/dev/null; then
        echo "Error: podman is not installed."
        echo "Please install podman for your distribution:"
        case "$DISTRO" in
            fedora) echo "  sudo dnf install podman" ;;
            arch)   echo "  sudo pacman -S podman" ;;
        esac
        exit 1
    fi
    echo "  podman OK"
    echo

    # Set minikube driver to podman
    echo "Setting minikube driver to podman..."
    minikube config set driver podman
    echo

    # Enable passwordless sudo for podman
    echo "Configuring passwordless sudo for podman..."
    local sudoers_line="${USER} ALL=(ALL) NOPASSWD: /usr/bin/podman"
    local sudoers_file="/etc/sudoers.d/podman"
    if [[ -f "$sudoers_file" ]] && grep -qF "$sudoers_line" "$sudoers_file"; then
        echo "  Already configured."
    else
        echo "$sudoers_line" | sudo tee "$sudoers_file" > /dev/null
        sudo chmod 440 "$sudoers_file"
        echo "  Created $sudoers_file"
    fi
    echo

    # Check docker.io authentication
    echo "Checking docker.io authentication..."
    if ! podman login --get-login docker.io &>/dev/null; then
        echo "  WARNING: You are not authenticated to docker.io."
        echo "  Without authentication you may reach Docker Hub rate limits."
        echo "  To authenticate, run: podman login docker.io"
        echo
    else
        echo "  docker.io authentication OK"
        echo
    fi

    # Check pids_limit in /etc/containers/containers.conf
    echo "Checking containers pids_limit configuration..."
    local containers_conf="/etc/containers/containers.conf.d/minikube.conf"
    if [[ ! -d "/etc/containers/containers.conf.d" ]]; then
        sudo mkdir -p /etc/containers/containers.conf.d
    fi
    if [[ -f "$containers_conf" ]] && grep -q '^pids_limit = 0' "$containers_conf"; then
        echo "  pids_limit = 0 already set."
    elif [[ -f "$containers_conf" ]]; then
        # File exists (e.g. Arch) — replace commented or existing pids_limit line
        if grep -q '^#.*pids_limit' "$containers_conf"; then
            sudo sed -i 's/^#.*pids_limit.*/pids_limit = 0/' "$containers_conf"
            echo "  Uncommented and set pids_limit = 0 in $containers_conf"
        else
            # Append under [containers] section if it exists, otherwise append to end
            if grep -q '^\[containers\]' "$containers_conf"; then
                sudo sed -i '/^\[containers\]/a pids_limit = 0' "$containers_conf"
            else
                printf '\n[containers]\npids_limit = 0\n' | sudo tee -a "$containers_conf" > /dev/null
            fi
            echo "  Added pids_limit = 0 to $containers_conf"
        fi
    else
        # File doesn't exist (e.g. Fedora) — create it
        printf '[containers]\npids_limit = 0\n' | sudo tee "$containers_conf" > /dev/null
        echo "  Created $containers_conf with pids_limit = 0"
    fi
    echo

    # Fedora-only: configure inotify sysctl settings
    if [[ "$DISTRO" == "fedora" ]]; then
        echo "Configuring inotify sysctl settings..."
        local sysctl_file="/etc/sysctl.d/10-inotify.conf"
        local needs_update=false

        if [[ ! -f "$sysctl_file" ]]; then
            needs_update=true
        elif ! grep -q 'fs.inotify.max_user_instances = 1024' "$sysctl_file" || \
             ! grep -q 'fs.inotify.max_user_watches = 524288' "$sysctl_file"; then
            needs_update=true
        fi

        if [[ "$needs_update" == "true" ]]; then
            printf 'fs.inotify.max_user_instances = 1024\nfs.inotify.max_user_watches = 524288\n' | sudo tee "$sysctl_file" > /dev/null
            sudo sysctl --system > /dev/null 2>&1
            echo "  Applied inotify settings to $sysctl_file"
        else
            echo "  inotify settings already configured."
        fi
        echo
    fi

    # Ask for repository URL
    read -rp "Enter the URL to the LeptoStack Local Development Environment Repository: " REPO_URL

    # Parse URL to extract hostname, owner, repo
    # Supports: https://hostname/owner/repo.git or https://hostname/owner/repo
    local url_without_scheme="${REPO_URL#https://}"
    url_without_scheme="${url_without_scheme#http://}"
    url_without_scheme="${url_without_scheme%.git}"

    GIT_HOSTNAME=$(echo "$url_without_scheme" | cut -d'/' -f1)
    GIT_OWNER=$(echo "$url_without_scheme" | cut -d'/' -f2)
    GIT_REPO=$(echo "$url_without_scheme" | cut -d'/' -f3)

    if [[ -z "$GIT_HOSTNAME" || -z "$GIT_OWNER" || -z "$GIT_REPO" ]]; then
        echo "Error: Could not parse repository URL. Expected format: https://hostname/owner/repo"
        exit 1
    fi

    echo
    echo "Parsed repository details:"
    echo "  Hostname: $GIT_HOSTNAME"
    echo "  Owner:    $GIT_OWNER"
    echo "  Repo:     $GIT_REPO"
    echo

    # Ask for branch
    read -rp "Enter the branch name [main]: " GIT_BRANCH
    GIT_BRANCH="${GIT_BRANCH:-main}"

    # Ask for cluster path
    read -rp "Enter the path to the cluster within the repo [clusters/local]: " CLUSTER_PATH
    CLUSTER_PATH="${CLUSTER_PATH:-clusters/local}"

    # Ask for Git server type
    echo
    echo "Select the Git Server:"
    echo "  1) GitHub"
    echo "  2) Gitea/Forgejo"
    echo "  3) GitLab"
    read -rp "Enter your choice [1-3]: " git_choice

    case "$git_choice" in
        1) GIT_SERVER="github" ;;
        2) GIT_SERVER="gitea" ;;
        3) GIT_SERVER="gitlab" ;;
        *)
            echo "Error: Invalid choice."
            exit 1
            ;;
    esac
    echo "  Selected: $GIT_SERVER"
    echo

    # Ask for PAT
    echo -n "Enter your Git server Personal Access Token (PAT): "
    GIT_PAT=""
    while IFS= read -rs -n1 char; do
        if [[ -z "$char" ]]; then
            break
        elif [[ "$char" == $'\x7f' || "$char" == $'\b' ]]; then
            if [[ -n "$GIT_PAT" ]]; then
                GIT_PAT="${GIT_PAT%?}"
                echo -ne '\b \b'
            fi
        else
            GIT_PAT+="$char"
            echo -n '*'
        fi
    done
    echo
    echo

    # Ask for LeptoStack Resources Git Repository PAT
    echo -n "Enter the LeptoStack Resources Git Repository PAT: "
    RESOURCES_GIT_PAT=""
    while IFS= read -rs -n1 char; do
        if [[ -z "$char" ]]; then
            break
        elif [[ "$char" == $'\x7f' || "$char" == $'\b' ]]; then
            if [[ -n "$RESOURCES_GIT_PAT" ]]; then
                RESOURCES_GIT_PAT="${RESOURCES_GIT_PAT%?}"
                echo -ne '\b \b'
            fi
        else
            RESOURCES_GIT_PAT+="$char"
            echo -n '*'
        fi
    done
    echo
    if [[ -z "$RESOURCES_GIT_PAT" ]]; then
        echo "Error: LeptoStack Resources Git Repository PAT cannot be empty."
        exit 1
    fi
    echo

    # Validate repository accessibility with PAT
    echo "Validating repository access..."
    local api_url http_code
    case "$GIT_SERVER" in
        github)
            api_url="https://api.github.com/repos/${GIT_OWNER}/${GIT_REPO}"
            http_code=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: token ${GIT_PAT}" "$api_url")
            ;;
        gitea)
            api_url="https://${GIT_HOSTNAME}/api/v1/repos/${GIT_OWNER}/${GIT_REPO}"
            http_code=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: token ${GIT_PAT}" "$api_url")
            ;;
        gitlab)
            local encoded_path
            encoded_path=$(printf '%s' "${GIT_OWNER}/${GIT_REPO}" | sed 's/\//%2F/g')
            api_url="https://${GIT_HOSTNAME}/api/v4/projects/${encoded_path}"
            http_code=$(curl -s -o /dev/null -w "%{http_code}" -H "PRIVATE-TOKEN: ${GIT_PAT}" "$api_url")
            ;;
    esac

    if [[ "$http_code" == "200" ]]; then
        echo "  Repository access validated successfully."
    elif [[ "$http_code" == "401" || "$http_code" == "403" ]]; then
        echo "Error: Authentication failed (HTTP $http_code). Please check your PAT has the correct permissions."
        exit 1
    elif [[ "$http_code" == "404" ]]; then
        echo "Error: Repository not found (HTTP 404). Please verify the repository URL and that your PAT has access."
        exit 1
    else
        echo "Error: Could not access repository (HTTP $http_code). Please verify the URL and PAT."
        exit 1
    fi
    echo

    # Check minikube configuration
    local current_cpus current_memory
    current_cpus=$(minikube config get cpus 2>/dev/null || echo "not set")
    current_memory=$(minikube config get memory 2>/dev/null || echo "not set")

    MINIKUBE_CPUS=8
    MINIKUBE_MEMORY=24576

    echo "Minikube configuration:"
    echo "  Current CPUs:   $current_cpus"
    echo "  Current Memory: $current_memory"
    echo
    echo "Recommended: cpus=8, memory=24576"

    if [[ "$current_cpus" != "8" || "$current_memory" != "24576" ]]; then
        echo
        read -rp "Minikube will be configured with cpus=8 and memory=24576. Do you want to modify these values? [y/N]: " modify_config
        if [[ "$modify_config" =~ ^[Yy]$ ]]; then
            read -rp "Enter number of CPUs [8]: " custom_cpus
            MINIKUBE_CPUS="${custom_cpus:-8}"
            read -rp "Enter memory in MB [24576]: " custom_memory
            MINIKUBE_MEMORY="${custom_memory:-24576}"
        fi
    fi

    # Apply minikube config
    echo
    echo "Configuring minikube with cpus=$MINIKUBE_CPUS and memory=$MINIKUBE_MEMORY..."
    minikube config set cpus "$MINIKUBE_CPUS"
    minikube config set memory "$MINIKUBE_MEMORY"

    # Save configuration
    save_config
    echo
    echo "Configuration saved to $CONFIG_FILE"
    echo
    echo "To start the development environment, run:"
    echo "  $0 start"
}

# --- Patch Flux System Kustomization ---

patch_flux_system_kustomization() {
    echo
    echo "Patching flux-system kustomization.yaml..."

    local tmp_dir orig_dir
    tmp_dir=$(mktemp -d /tmp/leptostack-flux-patch-XXXXXX)
    orig_dir=$(pwd)
    trap 'rm -rf "$tmp_dir"' EXIT

    # Build the clone URL with the PAT embedded for authentication
    local clone_url
    clone_url=$(build_clone_url "$GIT_OWNER" "$GIT_REPO")

    echo "Cloning repository ${GIT_OWNER}/${GIT_REPO}..."
    git clone --branch "$GIT_BRANCH" --depth 1 "$clone_url" "$tmp_dir/repo"

    local kustomization_file="${tmp_dir}/repo/${CLUSTER_PATH}/flux-system/kustomization.yaml"
    if [[ ! -f "$kustomization_file" ]]; then
        echo "Error: Could not find ${CLUSTER_PATH}/flux-system/kustomization.yaml in the repository."
        exit 1
    fi

    # Append the patches to the end of the kustomization.yaml only if they don't already exist
    if grep -q -- '--requeue-dependency=5s' "$kustomization_file"; then
        echo "Patches already present in ${CLUSTER_PATH}/flux-system/kustomization.yaml, skipping."
    else
        cat >> "$kustomization_file" <<'EOF'
patches:
- target:
    kind: Deployment
    name: kustomize-controller
  patch: |
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --requeue-dependency=5s
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --feature-gates=StrictPostBuildSubstitutions=true
- patch: |
    - op: remove
      path: /metadata/labels/pod-security.kubernetes.io~1warn
    - op: remove
      path: /metadata/labels/pod-security.kubernetes.io~1warn-version
    - op: add
      path: /metadata/labels/pod-security.kubernetes.io~1enforce
      value: restricted
  target:
    kind: Namespace
    labelSelector: app.kubernetes.io/part-of=flux
EOF

        echo "Patches appended to ${CLUSTER_PATH}/flux-system/kustomization.yaml"
    fi


    # Derive the cluster name from CLUSTER_PATH (format: clusters/CLUSTER_NAME)
    local cluster_name
    cluster_name=$(basename "$CLUSTER_PATH")

    # Calculate the load balancer IP on the minikube subnet (.100)
    local minikube_ip subnet lb_ip
    minikube_ip=$(minikube ip)
    subnet=$(echo "$minikube_ip" | cut -d'.' -f1-3)
    lb_ip="${subnet}.100"

    # Create the cluster overlay folder and files
    local infra_dir="${tmp_dir}/repo/infrastructure"
    local overlay_dir="${tmp_dir}/repo/apps/leptostack/overlays/${cluster_name}"
    echo
    echo "Creating cluster overlay at apps/leptostack/overlays/${cluster_name}..."
    mkdir -p "$overlay_dir"

    cat > "$infra_dir/cluster-config.yaml" <<'EOF'
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
  namespace: metallb-system
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - 192.168.49.100-192.168.49.110
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    cert-manager.io/cluster-issuer: internal-issuer
    k8s.apisix.apache.org/plugin-config-name: registry-plugin-config
  name: registry-ingress
  namespace: kube-system
spec:
  ingressClassName: local-apisix
  tls:
    - hosts:
        - registry.minikube.test
      secretName: registry-tls
  rules:
    - host: registry.minikube.test
      http:
        paths:
          - backend:
              service:
                name: registry
                port:
                  number: 80
            path: /
            pathType: Prefix
---
apiVersion: crd.projectcalico.org/v1
kind: GlobalNetworkPolicy
metadata:
  name: default-deny
spec:
  namespaceSelector: has(kubernetes.io/metadata.name) && kubernetes.io/metadata.name not in {"kube-system"}
  types:
    - Ingress
    - Egress
  egress:
    - action: Allow
EOF

    local infra_kustomization_file="${infra_dir}/kustomization.yaml"
    if [[ ! -f "$infra_kustomization_file" ]]; then
        echo "Error: Could not find infrastructure/kustomization.yaml in the repository."
        exit 1
    fi
    if grep -q -- 'cluster-config.yaml' "$infra_kustomization_file"; then
        echo "  cluster-config.yaml already referenced in infrastructure/kustomization.yaml."
    else
        echo "  Adding cluster-config.yaml to infrastructure/kustomization.yaml resources..."
        sed -i '/^resources:/a\  - cluster-config.yaml' "$infra_kustomization_file"
    fi

    cat > "$overlay_dir/naming-conf.yaml" <<EOF
nameReference:
  - kind: ConfigMap
    fieldSpecs:
      - kind: Kustomization
        path: spec/postBuild/substituteFrom/name
  - kind: Kustomization
    fieldSpecs:
      - kind: Kustomization
        path: spec/dependsOn/name
EOF

    cat > "$overlay_dir/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namePrefix: ${cluster_name}-

configurations:
  - naming-conf.yaml

resources:
  - ../../base

patches:
  - target:
      kind: Kustomization
      name: apisix
    patch: |-
      - op: add
        path: /spec/patches
        value:
          - target:
              kind: HelmRelease
              name: apisix
            patch: |-
              - op: add
                path: /spec/values/service
                value:
                  type: LoadBalancer
                  loadBalancerIP: ${lb_ip}
EOF

    echo "Cluster overlay files created at apps/leptostack/overlays/${cluster_name}"

    # Create the kustomization.yaml in CLUSTER_PATH referencing the overlay
    local cluster_kustomization_file="${tmp_dir}/repo/${CLUSTER_PATH}/kustomization.yaml"
    echo
    echo "Creating ${CLUSTER_PATH}/kustomization.yaml..."
    cat > "$cluster_kustomization_file" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
    - flux-system
    - ../../infrastructure
    - ../../apps/leptostack/overlays/${cluster_name}

patches:
  - target:
      kind: Kustomization
      name: keycloak-crd
      namespace: flux-system
    patch: |-
      - op: add
        path: "/spec/patches"
        value:
          - target:
              group: rbac.authorization.k8s.io
              version: v1
              kind: ClusterRoleBinding
              name: keycloak-operator-clusterrole-binding
            patch: |
              - op: add
                path: "/subjects"
                value:
                  - kind: ServiceAccount
                    name: keycloak-operator
                    namespace: ${cluster_name}-keycloak
  - target:
      kind: Kustomization
      name: infra-config
      namespace: flux-system
    patch: |-
      - op: add
        path: "/spec/patches"
        value:
          - target:
              group: trust.cert-manager.io
              version: v1alpha1
              kind: Bundle
              name: internal-ca-bundle
            patch: |
              - op: add
                path: "/spec/target/namespaceSelector"
                value:
                  matchExpressions:
                    - key: app.kubernetes.io/part-of
                      operator: In
                      values:
                        - leptostack-${cluster_name}
EOF

    echo "kustomization.yaml created at ${CLUSTER_PATH}/kustomization.yaml"

    # Commit and push the changes
    cd "$tmp_dir/repo"
    commit_and_push "Patch flux-system kustomization and add cluster overlays for ${cluster_name}" \
        "No changes to commit; repository already patched."

    echo "Reconciling flux"
    flux reconcile kustomization flux-system

    # Restore the original working directory before removing the temp directory.
    cd "$orig_dir"
    trap - EXIT
    rm -rf "$tmp_dir"
}



# --- Start ---

do_start() {
    local resync="${1:-}"

    load_config

    echo "=== Starting LeptoStack Development Environment ==="
    echo

    # Start minikube
    echo "Starting minikube..."
    minikube start --cni=calico --insecure-registry="registry.minikube.test"
    echo

    echo "Enabling minikube addons..."
    local enabled_addons
    enabled_addons=$(minikube addons list --output=json 2>/dev/null)
    for addon in metallb registry metrics-server; do
        if echo "$enabled_addons" | grep -q "\"$addon\":{\"Profile\":\"minikube\",\"Status\":\"enabled\""; then
            echo "  $addon already enabled, skipping."
        else
            minikube addons enable "$addon"
        fi
    done
    echo

    # Verify kubectl context is minikube before running flux
    check_kubectl_context

    # Check if flux is already bootstrapped
    if kubectl --context minikube -n flux-system get deployment source-controller &>/dev/null; then
        echo "Flux is already bootstrapped, skipping bootstrap."
        echo "You can check the status of the deployment by running:"
        echo "  flux get kustomizations"
    else
        # Sync the cluster template into the repository before bootstrapping
        sync_cluster_template "$resync"

        # Export PAT based on git server
        case "$GIT_SERVER" in
            github)
                export GITHUB_TOKEN="$GIT_PAT"
                ;;
            gitea)
                export GITEA_TOKEN="$GIT_PAT"
                ;;
            gitlab)
                export GITLAB_TOKEN="$GIT_PAT"
                ;;
        esac

        # Run flux bootstrap
        echo "Running flux bootstrap for $GIT_SERVER..."
        echo

        case "$GIT_SERVER" in
            github)
                flux --context minikube bootstrap github \
                    --token-auth \
                    --owner="$GIT_OWNER" \
                    --repository="$GIT_REPO" \
                    --branch="$GIT_BRANCH" \
                    --path="$CLUSTER_PATH" \
                    --personal
                ;;
            gitea)
                flux --context minikube bootstrap gitea \
                    --token-auth \
                    --hostname="$GIT_HOSTNAME" \
                    --owner="$GIT_OWNER" \
                    --repository="$GIT_REPO" \
                    --branch="$GIT_BRANCH" \
                    --path="$CLUSTER_PATH" \
                    --personal
                ;;
            gitlab)
                flux --context minikube bootstrap gitlab \
                    --token-auth \
                    --hostname="$GIT_HOSTNAME" \
                    --owner="$GIT_OWNER" \
                    --repository="$GIT_REPO" \
                    --branch="$GIT_BRANCH" \
                    --path="$CLUSTER_PATH"
                ;;
        esac

        echo
        echo "Flux bootstrap has been started."
        echo "You can check the status of the deployment by running:"
        echo "  flux get kustomizations"

        # Patch the flux-system kustomization.yaml in the git repository
        patch_flux_system_kustomization
    fi


    # Create leptostack-base secret in flux-system namespace
    echo
    echo "Creating leptostack-base secret in flux-system namespace..."
    if kubectl --context minikube -n flux-system get secret leptostack-base &>/dev/null; then
        echo "  Secret leptostack-base already exists, updating..."
        kubectl --context minikube -n flux-system delete secret leptostack-base
    fi
    kubectl --context minikube -n flux-system create secret generic leptostack-base \
        --from-literal=username=git \
        --from-literal=password="$RESOURCES_GIT_PAT"
    echo "  Secret leptostack-base created successfully."
}

# --- Status ---

do_status() {
    load_config

    echo "=== LeptoStack Status ==="
    echo

    check_kubectl_context

    echo "Minikube status:"
    if ! minikube status; then
        echo
        echo "Minikube is not running. Start it with: $0 start"
        return
    fi
    echo

    echo "Flux version:"
    flux --context minikube version || true
    echo

    echo "Flux kustomizations:"
    flux --context minikube get kustomizations || true
}

# --- Stop ---

do_stop() {
    load_config

    echo "Stopping minikube..."
    minikube stop
    echo "Minikube stopped."
}

# --- Reset ---

do_reset() {
    load_config

    read -rp "This will delete the minikube cluster and recreate it. Are you sure? [y/N]: " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 0
    fi

    echo "Deleting minikube cluster..."
    minikube delete
    echo

    do_start resync
}

# --- Reconcile ---

do_reconcile() {
    check_kubectl_context
    echo "Reconciling flux-system..."
    flux --context minikube reconcile kustomization flux-system --with-source
}

# --- Events ---

do_events() {
    check_kubectl_context
    kubectl --context minikube events -A -w
}

# --- Update DNS ---

do_update_dns() {
    check_kubectl_context

    echo "Checking if all kustomizations are ready..."
    local not_ready
    not_ready=$(flux --context minikube get kustomizations --status-selector ready=false --no-header | wc -l)
    if [[ "$not_ready" -gt 0 ]]; then
        echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
        echo "Run '$0 status' to check progress."
        exit 1
    fi
    echo "All kustomizations are ready."
    echo

    # Check if NetworkManager is running with dnsmasq plugin
    local nm_uses_dnsmasq=false
    if systemctl is-active --quiet NetworkManager; then
        if grep -rqs '^dns=dnsmasq' /etc/NetworkManager/conf.d/ /etc/NetworkManager/NetworkManager.conf 2>/dev/null; then
            nm_uses_dnsmasq=true
        fi
    fi

    if [[ "$nm_uses_dnsmasq" != "true" ]]; then
        echo "NetworkManager is not configured with the dnsmasq plugin."
        case "$DISTRO" in
            fedora)
                echo "Configuring NetworkManager to use dnsmasq..."
                sudo systemctl disable --now systemd-resolved
                sudo rm -f /etc/resolv.conf
                if grep -q '^\[main\]' /etc/NetworkManager/NetworkManager.conf 2>/dev/null; then
                    sudo sed -i '/^\[main\]/a dns=dnsmasq' /etc/NetworkManager/NetworkManager.conf
                else
                    printf '[main]\ndns=dnsmasq\n' | sudo tee -a /etc/NetworkManager/NetworkManager.conf > /dev/null
                fi
                sudo systemctl restart NetworkManager
                echo "  dnsmasq plugin configured successfully."
                ;;
            arch)
                echo "Please configure NetworkManager to use dnsmasq:"
                echo "  https://wiki.archlinux.org/title/NetworkManager#dnsmasq"
                exit 1
                ;;
        esac
        echo
    fi

    echo "NetworkManager dnsmasq plugin detected."
    echo "Configuring DNS for minikube..."
    local minikube_ip subnet lb_ip
    minikube_ip=$(minikube ip)
    subnet=$(echo "$minikube_ip" | cut -d'.' -f1-3)
    lb_ip="${subnet}.100"
    echo "address=/test/${lb_ip}" | sudo tee /etc/NetworkManager/dnsmasq.d/minikube.conf > /dev/null
    sudo systemctl restart NetworkManager
    echo "DNS configuration updated. *.test domains now resolve to ${lb_ip}."
}

# --- Add Trust ---

do_add_trust() {
    check_kubectl_context

    echo "Checking if all kustomizations are ready..."
    local not_ready
    not_ready=$(flux --context minikube get kustomizations --status-selector ready=false --no-header | wc -l)
    if [[ "$not_ready" -gt 0 ]]; then
        echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
        echo "Run '$0 status' to check progress."
        exit 1
    fi
    echo "All kustomizations are ready."
    echo

    # Download the CA certificate from the secret
    local tmp_ca
    tmp_ca=$(mktemp /tmp/leptostack-ca-XXXXXX.crt)
    echo "Extracting CA certificate from cert-manager/internal-ca-secret..."
    kubectl --context minikube get secret internal-ca-secret -n cert-manager -o jsonpath='{.data.ca\.crt}' | base64 -d > "$tmp_ca"

    if [[ ! -s "$tmp_ca" ]]; then
        echo "Error: Failed to extract CA certificate from secret cert-manager/internal-ca-secret."
        rm -f "$tmp_ca"
        exit 1
    fi

    echo "CA certificate saved to $tmp_ca"
    echo "Installing CA certificate to system trust store..."

    case "$DISTRO" in
        arch)
            sudo trust anchor --store "$tmp_ca"
            ;;
        fedora)
            sudo cp "$tmp_ca" /etc/pki/ca-trust/source/anchors/minikube.test.crt
            sudo chmod 644 /etc/pki/ca-trust/source/anchors/minikube.test.crt
            sudo update-ca-trust
            ;;
    esac

    rm -f "$tmp_ca"
    echo "CA certificate installed successfully."
    echo
    echo "Please restart your browsers for the new CA certificate to take effect."
}

# --- Port Forward ---

do_port_forward() {
    local service="$1"

    case "$service" in
        openbao)
            check_kubectl_context

            echo "Checking if all kustomizations are ready..."
            local not_ready
            not_ready=$(flux --context minikube get kustomizations --status-selector ready=false --no-header | wc -l)
            if [[ "$not_ready" -gt 0 ]]; then
                echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
                echo "Run '$0 status' to check progress."
                exit 1
            fi
            echo "All kustomizations are ready."
            echo

            echo "OpenBao root token:"
            kubectl --context minikube -n local-openbao get secrets openbao-init -o json | jq -r '.data.root_token | @base64d'
            echo

            echo "Starting port-forward for OpenBao (localhost:8200)..."
            kubectl --context minikube -n local-openbao port-forward services/local-openbao-openbao 8200:8200
            ;;
        rabbitmq)
            check_kubectl_context

            echo "Checking if all kustomizations are ready..."
            local not_ready
            not_ready=$(flux --context minikube get kustomizations --status-selector ready=false --no-header | wc -l)
            if [[ "$not_ready" -gt 0 ]]; then
                echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
                echo "Run '$0 status' to check progress."
                exit 1
            fi
            echo "All kustomizations are ready."
            echo

            echo "RabbitMQ credentials:"
            echo "  Username: $(kubectl --context minikube -n local-rabbitmq get secret portal-rabbitmq-default-user -o jsonpath='{.data.username}' | base64 -d)"
            echo "  Password: $(kubectl --context minikube -n local-rabbitmq get secret portal-rabbitmq-default-user -o jsonpath='{.data.password}' | base64 -d)"
            echo

            echo "Starting port-forward for RabbitMQ Management (localhost:15672)..."
            kubectl --context minikube -n local-rabbitmq port-forward services/portal-rabbitmq 15672:15672
            ;;
        postgres)
            check_kubectl_context

            echo "Checking if all kustomizations are ready..."
            local not_ready
            not_ready=$(flux --context minikube get kustomizations --status-selector ready=false --no-header | wc -l)
            if [[ "$not_ready" -gt 0 ]]; then
                echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
                echo "Run '$0 status' to check progress."
                exit 1
            fi
            echo "All kustomizations are ready."
            echo

            echo "PostgreSQL superuser credentials:"
            echo "  Username: $(kubectl --context minikube -n local-supabase get secret supabase-cluster-superuser -o jsonpath='{.data.username}' | base64 -d)"
            echo "  Password: $(kubectl --context minikube -n local-supabase get secret supabase-cluster-superuser -o jsonpath='{.data.password}' | base64 -d)"
            echo

            echo "Starting port-forward for PostgreSQL (localhost:5432)..."
            kubectl --context minikube -n local-supabase port-forward services/supabase-cluster-rw 5432:5432
            ;;
        greenmail)
            check_kubectl_context

            echo "Checking if all kustomizations are ready..."
            local not_ready
            not_ready=$(flux --context minikube get kustomizations --status-selector ready=false --no-header | wc -l)
            if [[ "$not_ready" -gt 0 ]]; then
                echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
                echo "Run '$0 status' to check progress."
                exit 1
            fi
            echo "All kustomizations are ready."
            echo

            echo "Starting port-forward for GreenMail (localhost:8025)..."
            kubectl --context minikube -n greenmail port-forward services/api 8025:80
            ;;
        *)
            echo "Error: Unknown service '$service'."
            echo "Supported services: openbao, rabbitmq, postgres, greenmail"
            exit 1
            ;;
    esac
}

# --- Completion ---

do_completion() {
    local shell="$1"

    case "$shell" in
        bash)
            cat <<'BASH_COMPLETION'
_leptostack() {
    local cur prev commands port_forward_services
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    commands="configure start stop status reset reconcile events update-dns add-trust port-forward completion version"
    port_forward_services="openbao rabbitmq postgres greenmail"

    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
        return 0
    fi

    case "${prev}" in
        port-forward)
            COMPREPLY=( $(compgen -W "${port_forward_services}" -- "${cur}") )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "zsh bash" -- "${cur}") )
            return 0
            ;;
    esac
}
complete -F _leptostack leptostack
BASH_COMPLETION
            ;;
        zsh)
            cat <<'ZSH_COMPLETION'
#compdef leptostack

_leptostack() {
    local -a commands
    commands=(
        'configure:Set up the development environment configuration'
        'start:Start LeptoStack'
        'stop:Stop LeptoStack'
        'status:Check LeptoStack status'
        'reset:Delete LeptoStack cluster and restart'
        'reconcile:Reconcile flux-system kustomization'
        'events:Watch all cluster events'
        'update-dns:Configure local DNS to resolve *.test via minikube'
        'add-trust:Add the internal CA certificate to system trust store'
        'port-forward:Port-forward a service'
        'completion:Generate shell completion script'
        'version:Show the leptostack version'
    )

    _arguments -C \
        '1:command:->command' \
        '*::arg:->args'

    case $state in
        command)
            _describe -t commands 'leptostack command' commands
            ;;
        args)
            case $words[1] in
                port-forward)
                    local -a services
                    services=('openbao:Port-forward OpenBao' 'rabbitmq:Port-forward RabbitMQ' 'postgres:Port-forward PostgreSQL' 'greenmail:Port-forward GreenMail')
                    _describe -t services 'service' services
                    ;;
                completion)
                    local -a shells
                    shells=('zsh:Generate zsh completion' 'bash:Generate bash completion')
                    _describe -t shells 'shell' shells
                    ;;
            esac
            ;;
    esac
}

compdef _leptostack leptostack
ZSH_COMPLETION
            ;;
        *)
            echo "Error: Unsupported shell '$shell'."
            echo "Supported shells: zsh, bash"
            echo
            echo "Usage:"
            echo "  eval \"\$($0 completion bash)\"   # for bash"
            echo "  eval \"\$($0 completion zsh)\"    # for zsh"
            exit 1
            ;;
    esac
}

# --- Version ---

do_version() {
    echo "leptostack ${SCRIPT_VERSION}"
}

# --- Main ---

usage() {
    echo "Usage: $0 {configure|start|stop|status|reset|reconcile|events|update-dns|add-trust|port-forward|completion|version}"
    echo
    echo "Commands:"
    echo "  configure      Set up the development environment configuration"
    echo "  start          Start LeptoStack"
    echo "  stop           Stop LeptoStack"
    echo "  status         Check LeptoStack status"
    echo "  reset          Delete LeptoStack cluster and restart"
    echo "  reconcile      Reconcile flux-system kustomization"
    echo "  events         Watch all cluster events"
    echo "  update-dns     Configure local DNS to resolve *.test via minikube"
    echo "  add-trust      Add the internal CA certificate to system trust store"
    echo "  port-forward   Port-forward a service (openbao, rabbitmq, postgres, greenmail)"
    echo "  completion     Generate shell completion script (zsh, bash)"
    echo "  version        Show the leptostack version"
    exit 1
}

if [[ $# -lt 1 ]]; then
    usage
fi

case "$1" in
    configure|start|reset)
        enforce_update
        ;;
    *)
        check_for_updates "false" || true
        ;;
esac

case "$1" in
    configure)     do_configure ;;
    start)         do_start ;;
    stop)          do_stop ;;
    status)        do_status ;;
    reset)         do_reset ;;
    reconcile)     do_reconcile ;;
    events)        do_events ;;
    update-dns)    do_update_dns ;;
    add-trust)     do_add_trust ;;
    port-forward)
        if [[ $# -lt 2 ]]; then
            echo "Usage: $0 port-forward {openbao|rabbitmq|postgres|greenmail}"
            exit 1
        fi
        do_port_forward "$2"
        ;;
    completion)
        if [[ $# -lt 2 ]]; then
            echo "Usage: $0 completion {zsh|bash}"
            exit 1
        fi
        do_completion "$2"
        ;;
    version)       do_version ;;
    *)             usage ;;
esac

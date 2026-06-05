#!/bin/bash

set -euo pipefail

CONFIG_DIR="${HOME}/.config/leptostack"
CONFIG_FILE="${CONFIG_DIR}/config"

MINIKUBE_MIN_VERSION="1.38.1"
FLUX_MIN_VERSION="2.8.7"

# --- Utility Functions ---

detect_distro() {
    local distro=""
    if [[ -f /etc/os-release ]]; then
        source /etc/os-release
        case "$ID" in
            fedora) distro="fedora" ;;
            ubuntu) distro="ubuntu" ;;
            arch)   distro="arch" ;;
        esac
    fi
    if [[ -z "$distro" ]]; then
        echo "Error: Unsupported Linux distribution."
        echo "Supported distributions: Fedora, Ubuntu, Arch Linux."
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

load_config() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        echo "Error: Configuration not found. Please run '$0 configure' first."
        exit 1
    fi
    source "$CONFIG_FILE"
}

save_config() {
    mkdir -p "$CONFIG_DIR"
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
EOF
    chmod 600 "$CONFIG_FILE"
}

# --- Configure ---

do_configure() {
    echo "=== LeptoStack Configuration ==="
    echo

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
    read -rp "Enter the path to the cluster within the repo [clusters/leptostack]: " CLUSTER_PATH
    CLUSTER_PATH="${CLUSTER_PATH:-clusters/leptostack}"

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

# --- Start ---

do_start() {
    load_config

    echo "=== Starting LeptoStack Development Environment ==="
    echo

    # Start minikube
    echo "Starting minikube..."
    minikube start
    echo

    echo "Enabling minikube addons..."
    minikube addons enable ingress
    minikube addons enable ingress-dns
    minikube addons enable registry
    minikube addons enable metrics-server
    echo

    # Verify kubectl context is minikube before running flux
    check_kubectl_context

    # Apply LeptoStack configuration
    echo "Applying LeptoStack configuration..."
    kubectl apply -f - <<'LEPTOSTACK_CONFIG'
apiVersion: v1
kind: Namespace
metadata:
  name: flux-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: leptostack-config
  namespace: flux-system
data:
  openbao_namespace: local-openbao

  supabase_namespace: local-supabase
  supabase_pgcluster: supabase-cluster
  supabase_hostname: supabase

  keycloak_namespace: local-keycloak
  keycloak_realm: local
  keycloak_hostname: sso

  rabbitmq_namespace: local-rabbitmq

  superset_namespace: local-superset
  superset_hostname: dashboard

  flowable_namespace: local-flowable

  centrifugo_namespace: local-centrifugo
  centrifugo_hostname: realtime

  camelk_namespace: local-camelk

  seaweedfs_namespace: local-seaweedfs
  seaweedfs_hostname: storage

  leptostack_domain: minikube.test
  leptostack_name: portal
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    cert-manager.io/cluster-issuer: internal-issuer
    nginx.ingress.kubernetes.io/proxy-body-size: 5g
  name: registry-ingress
  namespace: kube-system
spec:
  ingressClassName: nginx
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
LEPTOSTACK_CONFIG
    echo

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
            flux bootstrap github \
                --token-auth \
                --owner="$GIT_OWNER" \
                --repository="$GIT_REPO" \
                --branch="$GIT_BRANCH" \
                --path="$CLUSTER_PATH" \
                --personal
            ;;
        gitea)
            flux bootstrap gitea \
                --token-auth \
                --hostname="$GIT_HOSTNAME" \
                --owner="$GIT_OWNER" \
                --repository="$GIT_REPO" \
                --branch="$GIT_BRANCH" \
                --path="$CLUSTER_PATH" \
                --personal
            ;;
        gitlab)
            flux bootstrap gitlab \
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
    flux version || true
    echo

    echo "Flux kustomizations:"
    flux get kustomizations || true
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

    do_start
}

# --- Reconcile ---

do_reconcile() {
    check_kubectl_context
    echo "Reconciling flux-system..."
    flux reconcile kustomization flux-system --with-source
}

# --- Events ---

do_events() {
    check_kubectl_context
    kubectl events -A -w
}

# --- Update DNS ---

do_update_dns() {
    check_kubectl_context

    echo "Checking if all kustomizations are ready..."
    local not_ready
    not_ready=$(flux get kustomizations --status-selector ready=false --no-header | wc -l)
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
        echo "Error: NetworkManager is not configured with the dnsmasq plugin."
        echo "Please configure NetworkManager to use dnsmasq:"
        case "$DISTRO" in
            fedora)
                echo "  https://docs.fedoraproject.org/en-US/fedora-server/administration/dnsmasq/"
                ;;
            arch)
                echo "  https://wiki.archlinux.org/title/NetworkManager#dnsmasq"
                ;;
            ubuntu)
                echo "  https://manpages.ubuntu.com/manpages/focal/man5/NetworkManager.conf.5.html"
                ;;
        esac
        exit 1
    fi

    echo "NetworkManager dnsmasq plugin detected."
    echo "Configuring DNS for minikube..."
    local minikube_ip
    minikube_ip=$(minikube ip)
    echo "server=/test/${minikube_ip}" | sudo tee /etc/NetworkManager/dnsmasq.d/minikube.conf > /dev/null
    sudo systemctl restart NetworkManager
    echo "DNS configuration updated. *.test domains now resolve to ${minikube_ip}."
}

# --- Add Trust ---

do_add_trust() {
    check_kubectl_context

    echo "Checking if all kustomizations are ready..."
    local not_ready
    not_ready=$(flux get kustomizations --status-selector ready=false --no-header | wc -l)
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
    kubectl get secret internal-ca-secret -n cert-manager -o jsonpath='{.data.ca\.crt}' | base64 -d > "$tmp_ca"

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
            sudo update-ca-trust
            ;;
        ubuntu)
            sudo cp "$tmp_ca" /usr/local/share/ca-certificates/minikube.test.crt
            sudo update-ca-certificates
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
            not_ready=$(flux get kustomizations --status-selector ready=false --no-header | wc -l)
            if [[ "$not_ready" -gt 0 ]]; then
                echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
                echo "Run '$0 status' to check progress."
                exit 1
            fi
            echo "All kustomizations are ready."
            echo

            echo "OpenBao root token:"
            kubectl -n local-openbao get secrets openbao-init -o json | jq -r '.data.root_token | @base64d'
            echo

            echo "Starting port-forward for OpenBao (localhost:8200)..."
            kubectl -n local-openbao port-forward services/local-openbao-openbao 8200:8200
            ;;
        rabbitmq)
            check_kubectl_context

            echo "Checking if all kustomizations are ready..."
            local not_ready
            not_ready=$(flux get kustomizations --status-selector ready=false --no-header | wc -l)
            if [[ "$not_ready" -gt 0 ]]; then
                echo "Error: $not_ready kustomization(s) are not ready. Please wait for all kustomizations to become ready."
                echo "Run '$0 status' to check progress."
                exit 1
            fi
            echo "All kustomizations are ready."
            echo

            echo "RabbitMQ credentials:"
            echo "  Username: $(kubectl -n local-rabbitmq get secret local-rabbitmq-default-user -o jsonpath='{.data.username}' | base64 -d)"
            echo "  Password: $(kubectl -n local-rabbitmq get secret local-rabbitmq-default-user -o jsonpath='{.data.password}' | base64 -d)"
            echo

            echo "Starting port-forward for RabbitMQ Management (localhost:15672)..."
            kubectl -n local-rabbitmq port-forward services/local-rabbitmq 15672:15672
            ;;
        *)
            echo "Error: Unknown service '$service'."
            echo "Supported services: openbao, rabbitmq"
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

    commands="configure start stop status reset reconcile events update-dns add-trust port-forward completion"
    port_forward_services="openbao rabbitmq"

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
                    services=('openbao:Port-forward OpenBao' 'rabbitmq:Port-forward RabbitMQ')
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

# --- Main ---

usage() {
    echo "Usage: $0 {configure|start|stop|status|reset|reconcile|events|update-dns|add-trust|port-forward}"
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
    echo "  port-forward   Port-forward a service (openbao, rabbitmq)"
    echo "  completion     Generate shell completion script (zsh, bash)"
    exit 1
}

if [[ $# -lt 1 ]]; then
    usage
fi

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
            echo "Usage: $0 port-forward {openbao|rabbitmq}"
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
    *)             usage ;;
esac

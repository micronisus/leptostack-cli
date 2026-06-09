#Requires -Version 5.1

<#
.SYNOPSIS
    LeptoStack Development Environment management script for Windows.

.DESCRIPTION
    Manages the LeptoStack local development environment using minikube and FluxCD.

.PARAMETER Command
    The command to run: configure, start, stop, status, reset, reconcile, events, update-dns, add-trust, port-forward, completion.
#>

param(
    [Parameter(Position = 0)]
    [ValidateSet("configure", "start", "stop", "status", "reset", "reconcile", "events", "update-dns", "add-trust", "port-forward", "completion")]
    [string]$Command,

    [Parameter(Position = 1)]
    [string]$SubCommand
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$script:ConfigDir = Join-Path $env:USERPROFILE ".config\leptostack"
$script:ConfigFile = Join-Path $script:ConfigDir "config.json"

$script:MINIKUBE_MIN_VERSION = "1.38.1"
$script:FLUX_MIN_VERSION = "2.8.7"

# --- Utility Functions ---

function Test-CommandExists {
    param([string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Compare-Version {
    param([string]$Current, [string]$Minimum)
    $c = [version]$Current
    $m = [version]$Minimum
    return $c -ge $m
}

function Test-KubectlContext {
    $context = kubectl config current-context 2>$null
    if ($LASTEXITCODE -ne 0 -or $context -ne "minikube") {
        Write-Host "Error: kubectl context is not set to minikube (current: '$context')." -ForegroundColor Red
        Write-Host "Please switch context with: kubectl config use-context minikube"
        exit 1
    }
}

function Read-Config {
    if (-not (Test-Path $script:ConfigFile)) {
        Write-Host "Error: Configuration not found. Please run 'leptostack.ps1 configure' first." -ForegroundColor Red
        exit 1
    }
    $script:Config = Get-Content $script:ConfigFile | ConvertFrom-Json
}

function Save-Config {
    if (-not (Test-Path $script:ConfigDir)) {
        New-Item -ItemType Directory -Path $script:ConfigDir -Force | Out-Null
    }
    $script:Config | ConvertTo-Json | Set-Content $script:ConfigFile
}

# --- Configure ---

function Invoke-Configure {
    Write-Host "=== LeptoStack Configuration ===" -ForegroundColor Cyan
    Write-Host ""

    # Check minikube
    Write-Host "Checking minikube..."
    if (-not (Test-CommandExists "minikube")) {
        Write-Host "Error: minikube is not installed." -ForegroundColor Red
        Write-Host "Please download it from: https://minikube.sigs.k8s.io/docs/start/"
        exit 1
    }
    $mkVersionRaw = minikube version --short 2>$null
    $mkVersion = ($mkVersionRaw -replace '^v', '').Trim()
    if (-not (Compare-Version $mkVersion $script:MINIKUBE_MIN_VERSION)) {
        Write-Host "Error: minikube version $mkVersion is too old. Minimum required: $($script:MINIKUBE_MIN_VERSION)" -ForegroundColor Red
        Write-Host "Please update from: https://minikube.sigs.k8s.io/docs/start/"
        exit 1
    }
    Write-Host "  minikube $mkVersion OK" -ForegroundColor Green

    # Check flux
    Write-Host "Checking flux..."
    if (-not (Test-CommandExists "flux")) {
        Write-Host "Error: flux CLI is not installed." -ForegroundColor Red
        Write-Host "Please download it from: https://fluxcd.io/flux/installation/"
        exit 1
    }
    $fluxVersionRaw = flux -v 2>$null
    if ($fluxVersionRaw -match '(\d+\.\d+\.\d+)') {
        $fluxVersion = $Matches[1]
    } else {
        Write-Host "Error: Could not determine flux version." -ForegroundColor Red
        exit 1
    }
    if (-not (Compare-Version $fluxVersion $script:FLUX_MIN_VERSION)) {
        Write-Host "Error: flux version $fluxVersion is too old. Minimum required: $($script:FLUX_MIN_VERSION)" -ForegroundColor Red
        Write-Host "Please update from: https://fluxcd.io/flux/installation/"
        exit 1
    }
    Write-Host "  flux $fluxVersion OK" -ForegroundColor Green
    Write-Host ""

    # Check system resources
    $sysInfo = Get-CimInstance Win32_ComputerSystem
    $sysRamGB = [math]::Floor($sysInfo.TotalPhysicalMemory / 1GB)
    $sysCpus = (Get-CimInstance Win32_Processor | Measure-Object -Property NumberOfLogicalProcessors -Sum).Sum

    if ($sysRamGB -lt 30 -or $sysCpus -lt 8) {
        Write-Host "WARNING: LeptoStack Development Environment requires a minimum of 32GB RAM and 8 CPU cores." -ForegroundColor Yellow
        Write-Host "  Detected: ${sysRamGB}GB RAM, $sysCpus CPU cores"
        Write-Host ""
        $continueAnyway = Read-Host "Do you want to continue anyway? [y/N]"
        if ($continueAnyway -notmatch '^[Yy]$') {
            Write-Host "Aborted."
            exit 1
        }
        Write-Host ""
    }

    # Ask for repository URL
    $repoUrl = Read-Host "Enter the URL to the LeptoStack Local Development Environment Repository"

    # Parse URL
    $urlWithoutScheme = $repoUrl -replace '^https?://', ''
    $urlWithoutScheme = $urlWithoutScheme -replace '\.git$', ''
    $parts = $urlWithoutScheme -split '/'

    $gitHostname = $parts[0]
    $gitOwner = $parts[1]
    $gitRepo = $parts[2]

    if ([string]::IsNullOrEmpty($gitHostname) -or [string]::IsNullOrEmpty($gitOwner) -or [string]::IsNullOrEmpty($gitRepo)) {
        Write-Host "Error: Could not parse repository URL. Expected format: https://hostname/owner/repo" -ForegroundColor Red
        exit 1
    }

    Write-Host ""
    Write-Host "Parsed repository details:"
    Write-Host "  Hostname: $gitHostname"
    Write-Host "  Owner:    $gitOwner"
    Write-Host "  Repo:     $gitRepo"
    Write-Host ""

    # Ask for branch
    $gitBranch = Read-Host "Enter the branch name [main]"
    if ([string]::IsNullOrEmpty($gitBranch)) { $gitBranch = "main" }

    # Ask for cluster path
    $clusterPath = Read-Host "Enter the path to the cluster within the repo [clusters/leptostack]"
    if ([string]::IsNullOrEmpty($clusterPath)) { $clusterPath = "clusters/leptostack" }

    # Ask for Git server type
    Write-Host ""
    Write-Host "Select the Git Server:"
    Write-Host "  1) GitHub"
    Write-Host "  2) Gitea/Forgejo"
    Write-Host "  3) GitLab"
    $gitChoice = Read-Host "Enter your choice [1-3]"

    switch ($gitChoice) {
        "1" { $gitServer = "github" }
        "2" { $gitServer = "gitea" }
        "3" { $gitServer = "gitlab" }
        default {
            Write-Host "Error: Invalid choice." -ForegroundColor Red
            exit 1
        }
    }
    Write-Host "  Selected: $gitServer"
    Write-Host ""

    # Ask for PAT (masked input)
    $secPat = Read-Host "Enter your Git server Personal Access Token (PAT)" -AsSecureString
    $gitPat = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
        [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secPat)
    )
    Write-Host ""

    # Check minikube configuration
    $currentCpus = minikube config get cpus 2>$null
    if ($LASTEXITCODE -ne 0) { $currentCpus = "not set" }
    $currentMemory = minikube config get memory 2>$null
    if ($LASTEXITCODE -ne 0) { $currentMemory = "not set" }

    $minikubeCpus = 8
    $minikubeMemory = 24576

    Write-Host "Minikube configuration:"
    Write-Host "  Current CPUs:   $currentCpus"
    Write-Host "  Current Memory: $currentMemory"
    Write-Host ""
    Write-Host "Recommended: cpus=8, memory=24576"

    if ($currentCpus -ne "8" -or $currentMemory -ne "24576") {
        Write-Host ""
        $modifyConfig = Read-Host "Minikube will be configured with cpus=8 and memory=24576. Do you want to modify these values? [y/N]"
        if ($modifyConfig -match '^[Yy]$') {
            $customCpus = Read-Host "Enter number of CPUs [8]"
            if (-not [string]::IsNullOrEmpty($customCpus)) { $minikubeCpus = [int]$customCpus }
            $customMemory = Read-Host "Enter memory in MB [24576]"
            if (-not [string]::IsNullOrEmpty($customMemory)) { $minikubeMemory = [int]$customMemory }
        }
    }

    # Apply minikube config
    Write-Host ""
    Write-Host "Configuring minikube with cpus=$minikubeCpus and memory=$minikubeMemory..."
    minikube config set cpus $minikubeCpus
    minikube config set memory $minikubeMemory

    # Save configuration
    $script:Config = [PSCustomObject]@{
        GIT_SERVER     = $gitServer
        GIT_PAT        = $gitPat
        GIT_HOSTNAME   = $gitHostname
        GIT_OWNER      = $gitOwner
        GIT_REPO       = $gitRepo
        GIT_BRANCH     = $gitBranch
        CLUSTER_PATH   = $clusterPath
        MINIKUBE_CPUS  = $minikubeCpus
        MINIKUBE_MEMORY = $minikubeMemory
    }
    Save-Config

    Write-Host ""
    Write-Host "Configuration saved to $($script:ConfigFile)"
    Write-Host ""
    Write-Host "To start the development environment, run:"
    Write-Host "  .\leptostack.ps1 start"
}

# --- Start ---

function Invoke-Start {
    Read-Config

    Write-Host "=== Starting LeptoStack Development Environment ===" -ForegroundColor Cyan
    Write-Host ""

    # Start minikube
    Write-Host "Starting minikube..."
    minikube start
    if ($LASTEXITCODE -ne 0) { exit 1 }
    Write-Host ""

    Write-Host "Enabling minikube addons..."
    minikube addons enable ingress
    minikube addons enable ingress-dns
    minikube addons enable registry
    minikube addons enable metrics-server
    Write-Host ""

    # Verify kubectl context
    Test-KubectlContext

    # Apply LeptoStack configuration
    Write-Host "Applying LeptoStack configuration..."
    @"
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
"@ | kubectl apply -f -
    if ($LASTEXITCODE -ne 0) { exit 1 }
    Write-Host ""

    # Export PAT based on git server
    switch ($script:Config.GIT_SERVER) {
        "github" { $env:GITHUB_TOKEN = $script:Config.GIT_PAT }
        "gitea"  { $env:GITEA_TOKEN = $script:Config.GIT_PAT }
        "gitlab" { $env:GITLAB_TOKEN = $script:Config.GIT_PAT }
    }

    # Run flux bootstrap
    Write-Host "Running flux bootstrap for $($script:Config.GIT_SERVER)..."
    Write-Host ""

    switch ($script:Config.GIT_SERVER) {
        "github" {
            flux bootstrap github `
                --token-auth `
                --owner="$($script:Config.GIT_OWNER)" `
                --repository="$($script:Config.GIT_REPO)" `
                --branch="$($script:Config.GIT_BRANCH)" `
                --path="$($script:Config.CLUSTER_PATH)" `
                --personal
        }
        "gitea" {
            flux bootstrap gitea `
                --token-auth `
                --hostname="$($script:Config.GIT_HOSTNAME)" `
                --owner="$($script:Config.GIT_OWNER)" `
                --repository="$($script:Config.GIT_REPO)" `
                --branch="$($script:Config.GIT_BRANCH)" `
                --path="$($script:Config.CLUSTER_PATH)" `
                --personal
        }
        "gitlab" {
            flux bootstrap gitlab `
                --token-auth `
                --hostname="$($script:Config.GIT_HOSTNAME)" `
                --owner="$($script:Config.GIT_OWNER)" `
                --repository="$($script:Config.GIT_REPO)" `
                --branch="$($script:Config.GIT_BRANCH)" `
                --path="$($script:Config.CLUSTER_PATH)"
        }
    }
    if ($LASTEXITCODE -ne 0) { exit 1 }

    Write-Host ""
    Write-Host "Flux bootstrap has been started."
    Write-Host "You can check the status of the deployment by running:"
    Write-Host "  flux get kustomizations"
}

# --- Status ---

function Invoke-Status {
    Read-Config

    Write-Host "=== LeptoStack Status ===" -ForegroundColor Cyan
    Write-Host ""

    Test-KubectlContext

    Write-Host "Minikube status:"
    minikube status
    if ($LASTEXITCODE -ne 0) {
        Write-Host ""
        Write-Host "Minikube is not running. Start it with: .\leptostack.ps1 start" -ForegroundColor Yellow
        return
    }
    Write-Host ""

    Write-Host "Flux version:"
    flux version
    Write-Host ""

    Write-Host "Flux kustomizations:"
    flux get kustomizations
}

# --- Stop ---

function Invoke-Stop {
    Read-Config

    Write-Host "Stopping minikube..."
    minikube stop
    Write-Host "Minikube stopped."
}

# --- Reset ---

function Invoke-Reset {
    Read-Config

    $confirm = Read-Host "This will delete the minikube cluster and recreate it. Are you sure? [y/N]"
    if ($confirm -notmatch '^[Yy]$') {
        Write-Host "Aborted."
        exit 0
    }

    Write-Host "Deleting minikube cluster..."
    minikube delete
    Write-Host ""

    Invoke-Start
}

# --- Reconcile ---

function Invoke-Reconcile {
    Test-KubectlContext
    Write-Host "Reconciling flux-system..."
    flux reconcile kustomization flux-system --with-source
}

# --- Events ---

function Invoke-Events {
    Test-KubectlContext
    kubectl events -A -w
}

# --- Update DNS ---

function Invoke-UpdateDns {
    Test-KubectlContext

    Write-Host "Checking if all kustomizations are ready..."
    $notReady = (flux get kustomizations --status-selector ready=false --no-header 2>$null | Measure-Object -Line).Lines
    if ($notReady -gt 0) {
        Write-Host "Error: $notReady kustomization(s) are not ready. Please wait for all kustomizations to become ready." -ForegroundColor Red
        Write-Host "Run '.\leptostack.ps1 status' to check progress."
        exit 1
    }
    Write-Host "All kustomizations are ready."
    Write-Host ""

    $minikubeIp = (minikube ip).Trim()

    # On Windows, update the hosts file or use nrpt for DNS resolution
    Write-Host "Configuring DNS for minikube on Windows..."
    Write-Host ""
    Write-Host "Option 1: Add NRPT rule (requires Administrator):"
    Write-Host "  Add-DnsClientNrptRule -Namespace '.test' -NameServers '$minikubeIp'"
    Write-Host ""
    Write-Host "Option 2: Manually add entries to C:\Windows\System32\drivers\etc\hosts"
    Write-Host "  pointing *.test domains to $minikubeIp"
    Write-Host ""

    $addRule = Read-Host "Would you like to add the NRPT rule automatically? (requires Admin) [y/N]"
    if ($addRule -match '^[Yy]$') {
        $isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
            [Security.Principal.WindowsBuiltInRole]::Administrator
        )
        if (-not $isAdmin) {
            Write-Host "Error: This operation requires Administrator privileges." -ForegroundColor Red
            Write-Host "Please run PowerShell as Administrator and try again."
            exit 1
        }
        # Remove existing rule if present
        Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ".test" } | ForEach-Object {
            Remove-DnsClientNrptRule -Name $_.Name -Force
        }
        Add-DnsClientNrptRule -Namespace ".test" -NameServers $minikubeIp
        Write-Host "DNS NRPT rule added. *.test domains now resolve via $minikubeIp." -ForegroundColor Green
    }
}

# --- Add Trust ---

function Invoke-AddTrust {
    Test-KubectlContext

    Write-Host "Checking if all kustomizations are ready..."
    $notReady = (flux get kustomizations --status-selector ready=false --no-header 2>$null | Measure-Object -Line).Lines
    if ($notReady -gt 0) {
        Write-Host "Error: $notReady kustomization(s) are not ready. Please wait for all kustomizations to become ready." -ForegroundColor Red
        Write-Host "Run '.\leptostack.ps1 status' to check progress."
        exit 1
    }
    Write-Host "All kustomizations are ready."
    Write-Host ""

    # Extract CA certificate
    $tmpCa = Join-Path $env:TEMP "leptostack-ca-$([guid]::NewGuid().ToString('N').Substring(0,6)).crt"
    Write-Host "Extracting CA certificate from cert-manager/internal-ca-secret..."

    $caBase64 = kubectl get secret internal-ca-secret -n cert-manager -o jsonpath='{.data.ca\.crt}'
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrEmpty($caBase64)) {
        Write-Host "Error: Failed to extract CA certificate from secret cert-manager/internal-ca-secret." -ForegroundColor Red
        exit 1
    }

    [System.IO.File]::WriteAllBytes($tmpCa, [Convert]::FromBase64String($caBase64))

    Write-Host "CA certificate saved to $tmpCa"
    Write-Host "Installing CA certificate to Windows trust store..."

    $isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
        [Security.Principal.WindowsBuiltInRole]::Administrator
    )
    if (-not $isAdmin) {
        Write-Host "Error: Installing certificates requires Administrator privileges." -ForegroundColor Red
        Write-Host "Please run PowerShell as Administrator and try again."
        Remove-Item $tmpCa -Force
        exit 1
    }

    $cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($tmpCa)
    $store = New-Object System.Security.Cryptography.X509Certificates.X509Store(
        [System.Security.Cryptography.X509Certificates.StoreName]::Root,
        [System.Security.Cryptography.X509Certificates.StoreLocation]::LocalMachine
    )
    $store.Open([System.Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
    $store.Add($cert)
    $store.Close()

    Remove-Item $tmpCa -Force
    Write-Host "CA certificate installed successfully." -ForegroundColor Green
    Write-Host ""
    Write-Host "Please restart your browsers for the new CA certificate to take effect."
}

# --- Port Forward ---

function Invoke-PortForward {
    param([string]$Service)

    switch ($Service) {
        "openbao" {
            Test-KubectlContext

            Write-Host "Checking if all kustomizations are ready..."
            $notReady = (flux get kustomizations --status-selector ready=false --no-header 2>$null | Measure-Object -Line).Lines
            if ($notReady -gt 0) {
                Write-Host "Error: $notReady kustomization(s) are not ready." -ForegroundColor Red
                Write-Host "Run '.\leptostack.ps1 status' to check progress."
                exit 1
            }
            Write-Host "All kustomizations are ready."
            Write-Host ""

            Write-Host "OpenBao root token:"
            $secretJson = kubectl -n local-openbao get secrets openbao-init -o json | ConvertFrom-Json
            $rootToken = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($secretJson.data.root_token))
            Write-Host $rootToken
            Write-Host ""

            Write-Host "Starting port-forward for OpenBao (localhost:8200)..."
            kubectl -n local-openbao port-forward services/local-openbao-openbao 8200:8200
        }
        "rabbitmq" {
            Test-KubectlContext

            Write-Host "Checking if all kustomizations are ready..."
            $notReady = (flux get kustomizations --status-selector ready=false --no-header 2>$null | Measure-Object -Line).Lines
            if ($notReady -gt 0) {
                Write-Host "Error: $notReady kustomization(s) are not ready." -ForegroundColor Red
                Write-Host "Run '.\leptostack.ps1 status' to check progress."
                exit 1
            }
            Write-Host "All kustomizations are ready."
            Write-Host ""

            Write-Host "RabbitMQ credentials:"
            $username = kubectl -n local-rabbitmq get secret local-rabbitmq-default-user -o jsonpath='{.data.username}'
            $password = kubectl -n local-rabbitmq get secret local-rabbitmq-default-user -o jsonpath='{.data.password}'
            Write-Host "  Username: $([System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($username)))"
            Write-Host "  Password: $([System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($password)))"
            Write-Host ""

            Write-Host "Starting port-forward for RabbitMQ Management (localhost:15672)..."
            kubectl -n local-rabbitmq port-forward services/local-rabbitmq 15672:15672
        }
        default {
            Write-Host "Error: Unknown service '$Service'." -ForegroundColor Red
            Write-Host "Supported services: openbao, rabbitmq"
            exit 1
        }
    }
}

# --- Completion ---

function Invoke-Completion {
    param([string]$Shell)

    switch ($Shell) {
        "powershell" {
            @'
# PowerShell completion for leptostack.ps1
Register-ArgumentCompleter -CommandName leptostack.ps1 -Native -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commands = @(
        @{ Name = 'configure';    Description = 'Set up the development environment configuration' }
        @{ Name = 'start';        Description = 'Start LeptoStack' }
        @{ Name = 'stop';         Description = 'Stop LeptoStack' }
        @{ Name = 'status';       Description = 'Check LeptoStack status' }
        @{ Name = 'reset';        Description = 'Delete LeptoStack cluster and restart' }
        @{ Name = 'reconcile';    Description = 'Reconcile flux-system kustomization' }
        @{ Name = 'events';       Description = 'Watch all cluster events' }
        @{ Name = 'update-dns';   Description = 'Configure local DNS to resolve *.test via minikube' }
        @{ Name = 'add-trust';    Description = 'Add the internal CA certificate to system trust store' }
        @{ Name = 'port-forward'; Description = 'Port-forward a service' }
        @{ Name = 'completion';   Description = 'Generate shell completion script' }
    )

    $portForwardServices = @(
        @{ Name = 'openbao';  Description = 'Port-forward OpenBao' }
        @{ Name = 'rabbitmq'; Description = 'Port-forward RabbitMQ' }
    )

    $completionShells = @(
        @{ Name = 'powershell'; Description = 'Generate PowerShell completion' }
    )

    $elements = $commandAst.CommandElements
    $argCount = $elements.Count - 1  # Subtract the command itself

    if ($argCount -le 1) {
        $commands | Where-Object { $_.Name -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterValue', $_.Description)
        }
    } elseif ($argCount -eq 2) {
        $prevArg = $elements[1].ToString()
        switch ($prevArg) {
            'port-forward' {
                $portForwardServices | Where-Object { $_.Name -like "$wordToComplete*" } | ForEach-Object {
                    [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterValue', $_.Description)
                }
            }
            'completion' {
                $completionShells | Where-Object { $_.Name -like "$wordToComplete*" } | ForEach-Object {
                    [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterValue', $_.Description)
                }
            }
        }
    }
}
'@
        }
        default {
            Write-Host "Error: Unsupported shell '$Shell'." -ForegroundColor Red
            Write-Host "Supported shells: powershell"
            Write-Host ""
            Write-Host "Usage:"
            Write-Host "  .\leptostack.ps1 completion powershell | Invoke-Expression"
            exit 1
        }
    }
}

# --- Main ---

function Show-Usage {
    Write-Host "Usage: .\leptostack.ps1 <command> [args]"
    Write-Host ""
    Write-Host "Commands:"
    Write-Host "  configure      Set up the development environment configuration"
    Write-Host "  start          Start LeptoStack"
    Write-Host "  stop           Stop LeptoStack"
    Write-Host "  status         Check LeptoStack status"
    Write-Host "  reset          Delete LeptoStack cluster and restart"
    Write-Host "  reconcile      Reconcile flux-system kustomization"
    Write-Host "  events         Watch all cluster events"
    Write-Host "  update-dns     Configure local DNS to resolve *.test via minikube"
    Write-Host "  add-trust      Add the internal CA certificate to system trust store"
    Write-Host "  port-forward   Port-forward a service (openbao, rabbitmq)"
    Write-Host "  completion     Generate shell completion script (powershell)"
    exit 1
}

if ([string]::IsNullOrEmpty($Command)) {
    Show-Usage
}

switch ($Command) {
    "configure"    { Invoke-Configure }
    "start"        { Invoke-Start }
    "stop"         { Invoke-Stop }
    "status"       { Invoke-Status }
    "reset"        { Invoke-Reset }
    "reconcile"    { Invoke-Reconcile }
    "events"       { Invoke-Events }
    "update-dns"   { Invoke-UpdateDns }
    "add-trust"    { Invoke-AddTrust }
    "port-forward" {
        if ([string]::IsNullOrEmpty($SubCommand)) {
            Write-Host "Usage: .\leptostack.ps1 port-forward {openbao|rabbitmq}" -ForegroundColor Red
            exit 1
        }
        Invoke-PortForward $SubCommand
    }
    "completion" {
        if ([string]::IsNullOrEmpty($SubCommand)) {
            Write-Host "Usage: .\leptostack.ps1 completion {powershell}" -ForegroundColor Red
            exit 1
        }
        Invoke-Completion $SubCommand
    }
    default { Show-Usage }
}
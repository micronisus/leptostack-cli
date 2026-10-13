# install.ps1 installs the leptostack binary from the latest GitHub release.
#
#   irm https://raw.githubusercontent.com/micronisus/leptostack-cli/main/install.ps1 | iex
#
# It downloads the Windows zip, verifies it against checksums.txt with SHA-256,
# installs leptostack.exe to %LOCALAPPDATA%\leptostack\bin, and prepends that
# directory to the *user* PATH (no elevation required).
#
# Compatible with Windows PowerShell 5.1+ and PowerShell 7; it uses only built-in
# cmdlets (Invoke-RestMethod, Get-FileHash, Expand-Archive).
[CmdletBinding()]
param(
    [string]$InstallDir = "$env:LOCALAPPDATA\leptostack\bin"
)

$ErrorActionPreference = 'Stop'
$repo = 'micronisus/leptostack-cli'

$rawArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
switch ($rawArch) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "Unsupported architecture: $rawArch" }
}

$asset = "leptostack_windows_$arch.zip"
$base = "https://github.com/$repo/releases/latest/download"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("leptostack-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading $asset..."
    $assetPath = Join-Path $tmp $asset
    $sumsPath = Join-Path $tmp 'checksums.txt'
    Invoke-RestMethod -Uri "$base/$asset" -OutFile $assetPath
    Invoke-RestMethod -Uri "$base/checksums.txt" -OutFile $sumsPath

    Write-Host "Verifying checksum..."
    $line = Get-Content -Path $sumsPath |
        Where-Object { $_ -match ('\s\*?' + [regex]::Escape($asset) + '$') } |
        Select-Object -First 1
    if (-not $line) { throw "No checksum entry for $asset" }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA256 -Path $assetPath).Hash
    if ($actual.ToLower() -ne $expected.ToLower()) {
        throw "Checksum mismatch for $asset (expected $expected, got $actual)"
    }

    Write-Host "Installing leptostack to $InstallDir..."
    $expanded = Join-Path $tmp 'expanded'
    Expand-Archive -Path $assetPath -DestinationPath $expanded -Force
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $exe = Join-Path $expanded 'leptostack.exe'
    if (-not (Test-Path $exe)) { $exe = Join-Path $expanded 'leptostack' }
    Copy-Item -Path $exe -Destination (Join-Path $InstallDir 'leptostack.exe') -Force

    # Idempotently prepend the install dir to the user PATH.
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { $userPath = '' }
    $parts = $userPath.Split(';') | Where-Object { $_ -ne '' }
    if ($parts -notcontains $InstallDir) {
        $newPath = if ($userPath) { "$InstallDir;$userPath" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added $InstallDir to your user PATH. Reopen your terminal for it to take effect."
    }

    & (Join-Path $InstallDir 'leptostack.exe') version
    Write-Host "Installed leptostack. Get started with: leptostack config init; leptostack start"
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

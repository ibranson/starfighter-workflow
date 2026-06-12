# scripts/deploy.ps1 — build the daemon and push it to a Pi.
#
# Usage:
#   scripts\deploy.ps1 -RemoteHost ibranson@workflow.local
#   scripts\deploy.ps1 -RemoteHost workflow.local -SkipWeb
#   scripts\deploy.ps1 -RemoteHost workflow.local -SkipBuild   # reuse bin\sfworkflowd
#
# Steps:
#   1. (optional) build the SvelteKit SPA into internal/web/dist
#   2. cross-compile sfworkflowd for linux/arm64 (CGO-free, static)
#   3. scp the binary to /usr/local/bin/sfworkflowd
#   4. restart the systemd service
#   5. poll http://<host>:<port>/healthz for up to 10s
#   6. tail the last 30 journal lines

[CmdletBinding()]
param(
  [string]$RemoteHost = $env:SFWF_HOST,
  [string]$User       = $(if ($env:SFWF_USER) { $env:SFWF_USER } else { 'pi' }),
  [string]$SshKey     = $env:SFWF_SSH_KEY,
  [int]$Port          = 9090,
  [switch]$SkipWeb,
  [switch]$SkipBuild,
  [switch]$NoTail,
  [switch]$NoHealthz
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

if (-not $RemoteHost) { throw 'no host: pass -RemoteHost <pi-host> or set SFWF_HOST.' }

# Accept ssh user@host syntax in -RemoteHost.
if ($RemoteHost -match '^([^@]+)@(.+)$') {
  if (-not $PSBoundParameters.ContainsKey('User')) { $User = $Matches[1] }
  $RemoteHost = $Matches[2]
}

$sshOpts = @('-o', 'StrictHostKeyChecking=accept-new')
if ($SshKey) { $sshOpts += @('-i', $SshKey) }

function Invoke-Ssh([string[]]$cmd) {
  & ssh @sshOpts "$User@$RemoteHost" @cmd
  if ($LASTEXITCODE -ne 0) { throw "ssh failed: $($cmd -join ' ')" }
}
function Invoke-Scp([string]$src, [string]$dst) {
  & scp @sshOpts $src "$User@${RemoteHost}:$dst"
  if ($LASTEXITCODE -ne 0) { throw "scp failed: $src -> $dst" }
}
function Test-Healthz([string]$h, [int]$p, [int]$timeoutSec) {
  $url = "http://${h}:${p}/healthz"
  $deadline = (Get-Date).AddSeconds($timeoutSec)
  while ((Get-Date) -lt $deadline) {
    try {
      $r = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop
      if ($r.StatusCode -ge 200 -and $r.StatusCode -lt 300) { return $true }
    } catch { }
    Start-Sleep -Milliseconds 500
  }
  return $false
}

Push-Location $repoRoot
try {
  if (-not $SkipWeb) {
    Write-Host '[1/5] building SvelteKit SPA'
    Push-Location web
    try {
      if (-not (Test-Path 'node_modules')) {
        & npm install; if ($LASTEXITCODE -ne 0) { throw 'npm install failed' }
      }
      & npm run build; if ($LASTEXITCODE -ne 0) { throw 'npm run build failed' }
    } finally { Pop-Location }

    $dist = Join-Path $repoRoot 'internal\web\dist'
    if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    Copy-Item -Recurse -Force (Join-Path $repoRoot 'web\build\*') $dist
  } else {
    Write-Host '[1/5] skipping SvelteKit build'
  }

  if (-not $SkipBuild) {
    Write-Host '[2/5] cross-compiling sfworkflowd for linux/arm64'
    New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
    $env:GOOS = 'linux'; $env:GOARCH = 'arm64'; $env:CGO_ENABLED = '0'
    & go build -ldflags '-s -w' -o bin\sfworkflowd ./cmd/sfworkflowd
    $buildExit = $LASTEXITCODE
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
    if ($buildExit -ne 0) { throw 'go build failed' }
  } else {
    Write-Host '[2/5] skipping go build'
    if (-not (Test-Path 'bin\sfworkflowd')) { throw 'bin\sfworkflowd missing; cannot -SkipBuild on a fresh checkout.' }
  }

  Write-Host "[3/5] uploading binary to $User@${RemoteHost}"
  Invoke-Scp 'bin\sfworkflowd' '/tmp/sfworkflowd.new'
  Invoke-Ssh @('sudo', 'install', '-m', '0755', '/tmp/sfworkflowd.new', '/usr/local/bin/sfworkflowd')
  Invoke-Ssh @('sudo', 'rm', '-f', '/tmp/sfworkflowd.new')

  Write-Host '[4/5] restarting service'
  Invoke-Ssh @('sudo', 'systemctl', 'restart', 'sfworkflowd.service')

  if (-not $NoHealthz) {
    Write-Host "[5/5] waiting for http://${RemoteHost}:${Port}/healthz"
    if (Test-Healthz $RemoteHost $Port 10) {
      Write-Host 'healthz OK'
    } else {
      Write-Host '--- last 40 journal lines for context ---'
      try { Invoke-Ssh @('sudo', 'journalctl', '-u', 'sfworkflowd.service', '-n', '40', '--no-pager') } catch { }
      throw "healthz did not respond within 10s on http://${RemoteHost}:${Port}/healthz"
    }
  } else {
    Write-Host '[5/5] skipping healthz check'
  }

  if (-not $NoTail) {
    Write-Host ''
    Write-Host '--- last 30 lines of journal ---'
    Invoke-Ssh @('sudo', 'journalctl', '-u', 'sfworkflowd.service', '-n', '30', '--no-pager')
  }
} finally {
  Pop-Location
}

# scripts/provision.ps1 — one-time install of sfworkflowd on a Pi.
#
# Usage:
#   scripts\provision.ps1 -RemoteHost ibranson@workflow.local
#   scripts\provision.ps1 -RemoteHost workflow.local -ListenAddr ':9090'
#
# Steps:
#   1. verify SSH access
#   2. scp deploy/setup-pi.sh + deploy/sfworkflowd.service to /tmp
#   3. run setup-pi.sh under sudo (installs + enables the systemd unit)
#   4. write /etc/sfworkflowd/config.json
#
# Does NOT push the daemon binary — run scripts\deploy.ps1 afterwards.
# Does NOT change the hostname, networking, or anything the A/V app relies on.
#
# Idempotent. /etc/sfworkflowd/config.json IS overwritten on each run.

[CmdletBinding()]
param(
  [Parameter(Mandatory=$true)]
  [string]$RemoteHost,
  [string]$User        = $(if ($env:SFWF_USER) { $env:SFWF_USER } else { 'pi' }),
  [string]$SshKey      = $env:SFWF_SSH_KEY,
  [string]$DisplayName = '',
  [string]$DataDir     = '/var/lib/sfworkflowd',
  [string]$ListenAddr  = ':9090'
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

if ($RemoteHost -match '^([^@]+)@(.+)$') {
  if (-not $PSBoundParameters.ContainsKey('User')) { $User = $Matches[1] }
  $RemoteHost = $Matches[2]
}

if (-not $DisplayName) { $DisplayName = ($RemoteHost -replace '\.local$', '') }
if (-not $DisplayName) { $DisplayName = 'Repair Workflow' }

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

Push-Location $repoRoot
try {
  Write-Host "[1/4] verifying SSH access to $User@$RemoteHost"
  Invoke-Ssh @('true')

  Write-Host '[2/4] uploading setup-pi.sh + unit file'
  Invoke-Ssh @('mkdir', '-p', '/tmp/sfworkflowd-setup')
  Invoke-Scp 'deploy/setup-pi.sh'          '/tmp/sfworkflowd-setup/'
  Invoke-Scp 'deploy/sfworkflowd.service'  '/tmp/sfworkflowd-setup/'

  Write-Host '[3/4] running setup-pi.sh under sudo'
  Invoke-Ssh @('sudo', 'bash', '-c',
               "'cd /tmp/sfworkflowd-setup && chmod +x setup-pi.sh && ./setup-pi.sh'")

  Write-Host '[4/4] writing /etc/sfworkflowd/config.json'
  $cfg = [ordered]@{
    data_dir     = $DataDir
    display_name = $DisplayName
    http         = [ordered]@{ addr = $ListenAddr }
  }
  $json = $cfg | ConvertTo-Json -Depth 4
  $tmp = [System.IO.Path]::GetTempFileName()
  try {
    Set-Content -Path $tmp -Value $json -NoNewline -Encoding utf8
    Invoke-Scp $tmp '/tmp/sfworkflowd-config.json'
    Invoke-Ssh @('sudo', 'install', '-m', '0644', '/tmp/sfworkflowd-config.json', '/etc/sfworkflowd/config.json')
    Invoke-Ssh @('sudo', 'rm', '-f', '/tmp/sfworkflowd-config.json')
  } finally {
    Remove-Item -Force $tmp
  }

  $port = $ListenAddr.TrimStart(':')
  Write-Host ''
  Write-Host 'Provisioning complete.'
  Write-Host "  Display name: $DisplayName"
  Write-Host ''
  Write-Host 'Next steps:'
  Write-Host "  1. scripts\deploy.ps1 -RemoteHost $User@$RemoteHost"
  Write-Host "  2. Open http://${RemoteHost}:$port/ and create the first admin."
} finally {
  Pop-Location
}

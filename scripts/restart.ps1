# scripts/restart.ps1 — restart sfworkflowd on a Pi (no rebuild).
#   scripts\restart.ps1 -RemoteHost ibranson@workflow.local
[CmdletBinding()]
param(
  [string]$RemoteHost = $env:SFWF_HOST,
  [string]$User       = $(if ($env:SFWF_USER) { $env:SFWF_USER } else { 'pi' }),
  [string]$SshKey     = $env:SFWF_SSH_KEY
)
$ErrorActionPreference = 'Stop'
if (-not $RemoteHost) { throw 'no host: pass -RemoteHost or set SFWF_HOST.' }
if ($RemoteHost -match '^([^@]+)@(.+)$') {
  if (-not $PSBoundParameters.ContainsKey('User')) { $User = $Matches[1] }
  $RemoteHost = $Matches[2]
}
$sshOpts = @('-o', 'StrictHostKeyChecking=accept-new')
if ($SshKey) { $sshOpts += @('-i', $SshKey) }
& ssh @sshOpts "$User@$RemoteHost" 'sudo' 'systemctl' 'restart' 'sfworkflowd.service'
if ($LASTEXITCODE -ne 0) { throw 'restart failed' }
Write-Host 'restarted sfworkflowd.service'

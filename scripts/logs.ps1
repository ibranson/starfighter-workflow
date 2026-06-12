# scripts/logs.ps1 — follow the sfworkflowd journal on a Pi.
#   scripts\logs.ps1 -RemoteHost ibranson@workflow.local
[CmdletBinding()]
param(
  [string]$RemoteHost = $env:SFWF_HOST,
  [string]$User       = $(if ($env:SFWF_USER) { $env:SFWF_USER } else { 'pi' }),
  [string]$SshKey     = $env:SFWF_SSH_KEY,
  [int]$Lines         = 100
)
$ErrorActionPreference = 'Stop'
if (-not $RemoteHost) { throw 'no host: pass -RemoteHost or set SFWF_HOST.' }
if ($RemoteHost -match '^([^@]+)@(.+)$') {
  if (-not $PSBoundParameters.ContainsKey('User')) { $User = $Matches[1] }
  $RemoteHost = $Matches[2]
}
$sshOpts = @('-o', 'StrictHostKeyChecking=accept-new')
if ($SshKey) { $sshOpts += @('-i', $SshKey) }
& ssh @sshOpts "$User@$RemoteHost" 'sudo' 'journalctl' '-u' 'sfworkflowd.service' '-n' "$Lines" '-f' '--no-pager'

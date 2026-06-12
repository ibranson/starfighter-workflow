# sf.ps1 — convenience dispatcher for the common dev/deploy actions.
#
#   .\sf.ps1 provision ibranson@workflow.local
#   .\sf.ps1 deploy    ibranson@workflow.local
#   .\sf.ps1 logs      ibranson@workflow.local
#   .\sf.ps1 restart   ibranson@workflow.local
#   .\sf.ps1 run                                  # run locally on :9090
#   .\sf.ps1 build                                # cross-compile arm64 + web
#
# Any extra args after the host are passed through to the underlying script,
# e.g. .\sf.ps1 deploy host -SkipWeb
[CmdletBinding()]
param(
  [Parameter(Mandatory=$true, Position=0)]
  [ValidateSet('provision','deploy','logs','restart','run','build','web')]
  [string]$Command,
  [Parameter(Position=1, ValueFromRemainingArguments=$true)]
  [string[]]$Rest
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot

switch ($Command) {
  'run'   { & go run ./cmd/sfworkflowd -config (Join-Path $root 'dev-data\config.json'); break }
  'build' {
    Push-Location $root
    try {
      Push-Location web; try { npm install; npm run build } finally { Pop-Location }
      $dist = Join-Path $root 'internal\web\dist'
      Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
      New-Item -ItemType Directory -Force -Path $dist | Out-Null
      Copy-Item -Recurse -Force (Join-Path $root 'web\build\*') $dist
      $env:GOOS='linux'; $env:GOARCH='arm64'; $env:CGO_ENABLED='0'
      & go build -ldflags '-s -w' -o bin\sfworkflowd ./cmd/sfworkflowd
      Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
      Write-Host 'built bin\sfworkflowd (linux/arm64) + embedded SPA'
    } finally { Pop-Location }
    break
  }
  'web' {
    Push-Location (Join-Path $root 'web'); try { npm install; npm run build } finally { Pop-Location }
    $dist = Join-Path $root 'internal\web\dist'
    Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    Copy-Item -Recurse -Force (Join-Path $root 'web\build\*') $dist
    break
  }
  default {
    $script = Join-Path $root "scripts\$Command.ps1"
    $params = @{}
    if ($Rest -and $Rest.Count -gt 0) {
      # First positional remaining arg is the host; the rest pass through.
      $params['RemoteHost'] = $Rest[0]
      $passthru = $Rest[1..($Rest.Count-1)]
    } else {
      $passthru = @()
    }
    & $script @params @passthru
  }
}

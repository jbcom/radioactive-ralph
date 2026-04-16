param(
  [Parameter(Mandatory = $true)]
  [string]$Bin
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path $Bin)) {
  throw "binary not found: $Bin"
}

$tmp = Join-Path $env:RUNNER_TEMP ("ralph-scm-" + [guid]::NewGuid().ToString("N"))
$repo = Join-Path $tmp "repo"
$state = Join-Path $tmp "state"
New-Item -ItemType Directory -Force -Path $repo, $state | Out-Null

$serviceName = $null

function Cleanup {
  if ($serviceName) {
    sc.exe stop $serviceName *> $null
    sc.exe delete $serviceName *> $null
  }
  if (Test-Path $repo) {
    & $Bin service uninstall --repo-root $repo *> $null
  }
  if (Test-Path $tmp) {
    Remove-Item -Recurse -Force $tmp
  }
}

try {
  $env:RALPH_STATE_DIR = $state
  & $Bin init --repo-root $repo --yes | Out-Null
  $installLine = & $Bin service install --repo-root $repo --radioactive_ralph-bin $Bin --env "RALPH_STATE_DIR=$state"
  $configPath = ($installLine -split '\s+', 2)[1]
  if (-not $configPath) {
    throw "failed to parse install path from: $installLine"
  }
  & $Bin service list | Select-String -SimpleMatch $configPath | Out-Null

  $serviceName = [System.IO.Path]::GetFileNameWithoutExtension($configPath)
  sc.exe start $serviceName | Out-Null

  $ready = $false
  for ($i = 0; $i -lt 30; $i++) {
    try {
      $status = & $Bin status --repo-root $repo --json 2>$null | ConvertFrom-Json
      if ($status.repo_path) {
        $ready = $true
        break
      }
    } catch {
    }
    Start-Sleep -Seconds 1
  }
  if (-not $ready) {
    sc.exe query $serviceName
    throw "windows scm service never became ready"
  }

  & $Bin service status --repo-root $repo | Out-Null
  sc.exe stop $serviceName | Out-Null

  $stopped = $false
  for ($i = 0; $i -lt 30; $i++) {
    & $Bin status --repo-root $repo *> $null
    if ($LASTEXITCODE -ne 0) {
      $stopped = $true
      break
    }
    Start-Sleep -Seconds 1
  }
  if (-not $stopped) {
    sc.exe query $serviceName
    throw "windows scm service never stopped"
  }

  Write-Output "windows scm smoke: ok"
} finally {
  Cleanup
}

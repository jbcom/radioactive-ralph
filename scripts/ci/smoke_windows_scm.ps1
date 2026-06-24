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
$configPath = $null

function Write-Diagnostics {
  param([string]$Reason)

  Write-Output "windows scm diagnostics: $Reason"
  if ($serviceName) {
    Write-Output "sc.exe queryex $serviceName"
    sc.exe queryex $serviceName
    Write-Output "sc.exe qc $serviceName"
    sc.exe qc $serviceName
  }
  if ($configPath -and (Test-Path $configPath)) {
    Write-Output "service config: $configPath"
    Get-Content -Raw $configPath
  }
  if (Test-Path $state) {
    Write-Output "state tree: $state"
    Get-ChildItem -Force -Recurse $state | Select-Object FullName, Length, LastWriteTime | Format-Table -AutoSize
  }
  if ($serviceName) {
    Write-Output "recent Service Control Manager events for $serviceName"
    Get-WinEvent -FilterHashtable @{ LogName = 'System'; ProviderName = 'Service Control Manager'; StartTime = (Get-Date).AddMinutes(-10) } -ErrorAction SilentlyContinue |
      Where-Object { $_.Message -like "*$serviceName*" } |
      Select-Object TimeCreated, Id, LevelDisplayName, Message |
      Format-List
  }
}

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
    Write-Diagnostics "service did not become ready"
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
    Write-Diagnostics "service did not stop after stop request"
    throw "windows scm service never stopped"
  }

  Write-Output "windows scm smoke: ok"
} finally {
  Cleanup
}

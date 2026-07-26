param(
  [Parameter(Mandatory = $true)]
  [string]$Bin
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path $Bin)) {
  throw "binary not found: $Bin"
}

# Exercise the operator-owned lifecycle only. `service install` must
# create/update, start, and wait for Running; `service uninstall` must
# stop/wait/delete idempotently. sc.exe is diagnostic-only in this smoke.
$tmp = Join-Path $env:RUNNER_TEMP ("ralph-scm-" + [guid]::NewGuid().ToString("N"))
$project = Join-Path $tmp "project"
$stateOne = Join-Path $tmp "state-one"
$stateTwo = Join-Path $tmp "state-two"
New-Item -ItemType Directory -Force -Path $project, $stateOne, $stateTwo | Out-Null

$serviceName = "radioactive_ralph-supervisor"
$configPath = $null

function Write-Diagnostics {
  param(
    [string]$Reason,
    [string]$ActiveState
  )

  Write-Output "windows scm diagnostics: $Reason"
  Write-Output "sc.exe queryex $serviceName"
  sc.exe queryex $serviceName
  Write-Output "sc.exe qc $serviceName"
  sc.exe qc $serviceName
  if ($configPath -and (Test-Path $configPath)) {
    Write-Output "service config: $configPath"
    Get-Content -Raw $configPath
  }
  if ($ActiveState -and (Test-Path $ActiveState)) {
    Write-Output "state tree: $ActiveState"
    Get-ChildItem -Force -Recurse $ActiveState |
      Select-Object FullName, Length, LastWriteTime |
      Format-Table -AutoSize
  }
  Write-Output "recent Service Control Manager events for $serviceName"
  Get-WinEvent -FilterHashtable @{
    LogName = 'System'
    ProviderName = 'Service Control Manager'
    StartTime = (Get-Date).AddMinutes(-10)
  } -ErrorAction SilentlyContinue |
    Where-Object { $_.Message -like "*$serviceName*" } |
    Select-Object TimeCreated, Id, LevelDisplayName, Message |
    Format-List
}

function Cleanup {
  # Cleanup deliberately uses the public CLI too. A second uninstall is part
  # of the contract and must be a successful no-op.
  & $Bin service uninstall *> $null
  if (Test-Path $tmp) {
    Remove-Item -Recurse -Force $tmp
  }
}

function Initialize-Project {
  param([string]$StateDir)

  Push-Location $project
  try {
    $env:RALPH_STATE_DIR = $StateDir
    & $Bin --init | Out-Null
    if ($LASTEXITCODE -ne 0) {
      throw "project initialization failed for $StateDir"
    }
  } finally {
    Pop-Location
  }
}

function Test-SupervisorUp {
  param([string]$StateDir)

  Push-Location $project
  try {
    $env:RALPH_STATE_DIR = $StateDir
    $out = & $Bin 2>$null
    return ($LASTEXITCODE -eq 0) -and ($out -join "`n") -match "supervisor is up"
  } catch {
    return $false
  } finally {
    Pop-Location
  }
}

function Wait-Supervisor {
  param(
    [string]$StateDir,
    [bool]$ExpectedUp
  )

  for ($i = 0; $i -lt 30; $i++) {
    if ((Test-SupervisorUp $StateDir) -eq $ExpectedUp) {
      return $true
    }
    Start-Sleep -Seconds 1
  }
  return $false
}

function Config-PathFromInstall {
  param([string[]]$InstallOutput)

  $text = $InstallOutput -join "`n"
  if ($text -notmatch '(?m)installed and started supervisor service at (.+)$') {
    throw "failed to parse install path from: $text"
  }
  return $Matches[1].Trim()
}

try {
  Initialize-Project $stateOne
  Initialize-Project $stateTwo

  $firstInstall = & $Bin service install --bin $Bin --env "RALPH_STATE_DIR=$stateOne"
  if ($LASTEXITCODE -ne 0) {
    throw "first service install failed"
  }
  $configPath = Config-PathFromInstall $firstInstall
  if (-not (Test-Path $configPath)) {
    throw "service install did not persist config at $configPath"
  }
  $createdService = Get-CimInstance Win32_Service -Filter "Name='$serviceName'"
  if ($createdService.StartName -ne "LocalSystem") {
    throw "new SCM service identity is $($createdService.StartName), want LocalSystem"
  }
  if (-not (Wait-Supervisor $stateOne $true)) {
    Write-Diagnostics "first install did not become ready" $stateOne
    throw "CLI-installed Windows supervisor never became ready"
  }

  # Reinstall over the live service with changed environment. The CLI must
  # stop, update, restart, and wait for Running; no sc.exe lifecycle command
  # is allowed to make this pass.
  $secondInstall = & $Bin service install `
    --bin $Bin `
    --env "RALPH_STATE_DIR=$stateTwo" `
    --env "RALPH_MAX_PARALLEL=3"
  if ($LASTEXITCODE -ne 0) {
    throw "service reinstall failed"
  }
  $secondConfigPath = Config-PathFromInstall $secondInstall
  if ($secondConfigPath -ne $configPath) {
    throw "reinstall changed config path: $configPath -> $secondConfigPath"
  }
  $config = Get-Content -Raw $configPath | ConvertFrom-Json
  if ($config.extra_env.RALPH_STATE_DIR -ne $stateTwo) {
    throw "reinstall did not persist changed RALPH_STATE_DIR"
  }
  if ($config.extra_env.RALPH_MAX_PARALLEL -ne "3") {
    throw "reinstall did not persist RALPH_MAX_PARALLEL=3"
  }
  if ($null -ne $config.extra_env.PSObject.Properties["PATH"]) {
    throw "Windows SCM config persisted the installing administrator's inferred PATH"
  }
  if (-not (Wait-Supervisor $stateTwo $true)) {
    Write-Diagnostics "reinstalled service did not become ready with changed environment" $stateTwo
    throw "reinstalled Windows supervisor never became ready"
  }
  if (-not (Wait-Supervisor $stateOne $false)) {
    Write-Diagnostics "old state root remained live after reinstall" $stateOne
    throw "reinstall did not stop the old supervisor instance"
  }

  & $Bin service uninstall | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "service uninstall failed"
  }
  if (-not (Wait-Supervisor $stateTwo $false)) {
    Write-Diagnostics "service remained reachable after CLI uninstall" $stateTwo
    throw "CLI uninstall did not stop the Windows supervisor"
  }
  if (Test-Path $configPath) {
    throw "CLI uninstall did not remove persisted service config"
  }

  # Direct idempotency proof: absent service + absent config is still success.
  & $Bin service uninstall | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "second service uninstall was not idempotent"
  }

  Write-Output "windows scm smoke: ok"
} finally {
  Cleanup
}

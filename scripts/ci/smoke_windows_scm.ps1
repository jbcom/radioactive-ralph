param(
  [Parameter(Mandatory = $true)]
  [string]$Bin
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path $Bin)) {
  throw "binary not found: $Bin"
}

# The rewritten runtime is a single PER-USER supervisor keyed off the XDG
# state root — there is no per-repo service instance anymore (see
# internal/service's package doc comment). This smoke installs it as a
# native Windows SCM service (with RALPH_STATE_DIR pointed at an isolated
# dir via --env), starts it, confirms the plain client sees a live
# supervisor, stops it, and confirms the client reports it gone.
$tmp = Join-Path $env:RUNNER_TEMP ("ralph-scm-" + [guid]::NewGuid().ToString("N"))
$project = Join-Path $tmp "project"
$state = Join-Path $tmp "state"
New-Item -ItemType Directory -Force -Path $project, $state | Out-Null

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
  & $Bin service uninstall *> $null
  if (Test-Path $tmp) {
    Remove-Item -Recurse -Force $tmp
  }
}

function Test-SupervisorUp {
  Push-Location $project
  try {
    $env:RALPH_STATE_DIR = $state
    $out = & $Bin 2>$null
    return ($LASTEXITCODE -eq 0) -and ($out -join "`n") -match "supervisor is up"
  } catch {
    return $false
  } finally {
    Pop-Location
  }
}

try {
  Push-Location $project
  try {
    $env:RALPH_STATE_DIR = $state
    & $Bin --init | Out-Null
  } finally {
    Pop-Location
  }

  $installLine = & $Bin service install --radioactive_ralph-bin $Bin --env "RALPH_STATE_DIR=$state"
  $configPath = ($installLine -split '\s+')[-1]
  if (-not (Test-Path $configPath)) {
    throw "failed to parse install path from: $installLine"
  }

  $serviceName = [System.IO.Path]::GetFileNameWithoutExtension($configPath)
  sc.exe start $serviceName | Out-Null

  $ready = $false
  for ($i = 0; $i -lt 30; $i++) {
    if (Test-SupervisorUp) {
      $ready = $true
      break
    }
    Start-Sleep -Seconds 1
  }
  if (-not $ready) {
    Write-Diagnostics "service did not become ready"
    throw "windows scm-managed supervisor never became ready"
  }

  sc.exe stop $serviceName | Out-Null

  $stopped = $false
  for ($i = 0; $i -lt 30; $i++) {
    if (-not (Test-SupervisorUp)) {
      $stopped = $true
      break
    }
    Start-Sleep -Seconds 1
  }
  if (-not $stopped) {
    Write-Diagnostics "service did not stop after stop request"
    throw "windows scm-managed supervisor never stopped"
  }

  Write-Output "windows scm smoke: ok"
} finally {
  Cleanup
}

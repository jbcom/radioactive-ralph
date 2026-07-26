param(
  [Parameter(Mandatory = $true)]
  [string]$Bin
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
if (Get-Variable -Name PSNativeCommandUseErrorActionPreference -ErrorAction SilentlyContinue) {
  $PSNativeCommandUseErrorActionPreference = $false
}

if (-not (Test-Path $Bin)) {
  throw "binary not found: $Bin"
}

# Native Windows SCM persistence is deliberately disabled for v0.22. This
# smoke proves the release contract on a real Windows service manager:
#
# - install fails with safe alternatives before mutating SCM/config/state or
#   leaving a supervisor process;
# - a legacy SCM entry that names the exact Ralph binary reaches the native
#   process-entry guard and exits nonzero before loading config or starting a
#   supervisor;
# - status inspects a stopped legacy registration even when no config exists;
# - uninstall refuses an unrelated same-name cmd.exe registration without
#   stopping/deleting it or removing config;
# - uninstall stops a running SCM-aware fixture whose ImagePath matches the
#   exact historical Ralph shape, verifies deletion, and remains idempotent.
$tmp = Join-Path $env:RUNNER_TEMP ("ralph-scm-" + [guid]::NewGuid().ToString("N"))
$stateProbe = Join-Path $tmp "state-probe"
$serviceName = "radioactive_ralph-supervisor"
$userHome = [Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)
if ([string]::IsNullOrWhiteSpace($userHome)) {
  throw "could not resolve the current Windows user profile"
}
$configDir = Join-Path $userHome "AppData\Local\radioactive-ralph\services"
$configPath = Join-Path $configDir "$serviceName.json"
$configDirExisted = Test-Path $configDir
$resolvedBin = (Resolve-Path $Bin).Path
$ownsServiceNamespace = $false
$ralphProcessName = "radioactive_ralph.exe"
$guardDir = Join-Path $tmp ("guard-" + [guid]::NewGuid().ToString("N"))
$guardBin = Join-Path $guardDir $ralphProcessName
$canarySource = Join-Path $tmp "scm-execution-canary.go"
$canaryBin = Join-Path $tmp "scm-execution-canary.exe"
$canaryMarker = Join-Path $tmp "SCM-EXECUTED"
$guardHome = Join-Path $guardDir "arbitrary-home"
$guardConfigPath = Join-Path $guardHome "AppData\Local\radioactive-ralph\services\$serviceName.json"
$guardState = Join-Path $tmp "guard-state-must-not-exist"
$guardConfigCanary = Join-Path $tmp "guard-config-canary-must-not-exist"
$legacyDir = Join-Path $tmp ("running-legacy-" + [guid]::NewGuid().ToString("N"))
$legacySource = Join-Path $legacyDir "legacy-service.go"
$legacyBin = Join-Path $legacyDir $ralphProcessName
$legacyRunningMarker = Join-Path $legacyDir "RUNNING"
$legacyStoppedMarker = Join-Path $legacyDir "STOPPED"
$unknownCommand = $null
$guardCommand = $null
$legacyCommand = $null

function Convert-TraceSID {
  param([AllowNull()][object]$SIDBytes)

  $bytes = @($SIDBytes)
  if ($bytes.Count -eq 0) {
    return ""
  }
  return [Convert]::ToBase64String([byte[]]$bytes)
}

function Get-RalphService {
  $services = @(Get-CimInstance Win32_Service -Filter "Name='$serviceName'" -ErrorAction Stop)
  if ($services.Count -gt 1) {
    throw "SCM returned more than one registration named $serviceName"
  }
  if ($services.Count -eq 1) {
    return $services[0]
  }
  return $null
}

function Get-RalphSupervisorProcesses {
  return @(Get-CimInstance Win32_Process `
    -Filter "Name='$ralphProcessName'" `
    -ErrorAction Stop |
    Where-Object {
      $_.ExecutablePath -and
      ([string]::Equals($_.ExecutablePath, $resolvedBin, [StringComparison]::OrdinalIgnoreCase) -or
       [string]::Equals($_.ExecutablePath, $guardBin, [StringComparison]::OrdinalIgnoreCase) -or
       [string]::Equals($_.ExecutablePath, $legacyBin, [StringComparison]::OrdinalIgnoreCase)) -and
      $_.CommandLine -and
      $_.CommandLine -match '(?:^|\s)--supervisor(?:\s|$)'
    })
}

function Assert-NoRalphSupervisor {
  param([string]$Stage)

  $processes = @(Get-RalphSupervisorProcesses)
  if ($processes.Count -ne 0) {
    $details = $processes |
      Select-Object ProcessId, ExecutablePath, CommandLine |
      Format-Table -AutoSize |
      Out-String
    throw "$Stage left a radioactive_ralph supervisor process alive:`n$details"
  }
}

function Assert-ServiceAbsent {
  param([string]$Stage)

  $service = Get-RalphService
  if ($null -ne $service) {
    throw "$Stage left SCM service $serviceName in state $($service.State) with PID $($service.ProcessId)"
  }
}

function Assert-UnknownCollisionPreserved {
  param(
    [string]$Stage,
    [string]$ExpectedCommand
  )

  $service = Get-RalphService
  if ($null -eq $service) {
    throw "$Stage deleted unknown same-name SCM registration"
  }
  if ($service.State -ne "Stopped" -or [int]$service.ProcessId -ne 0) {
    throw "$Stage mutated unknown registration to state $($service.State) with PID $($service.ProcessId)"
  }
  if ($service.StartMode -ne "Disabled") {
    throw "$Stage changed unknown registration start mode to $($service.StartMode)"
  }
  if ($service.DisplayName -ne "radioactive_ralph supervisor" -or
      $service.Description -ne "Durable radioactive_ralph supervisor") {
    throw "$Stage changed unknown registration metadata"
  }
  if (-not [string]::Equals($service.PathName, $ExpectedCommand, [StringComparison]::OrdinalIgnoreCase)) {
    throw "$Stage changed unknown registration ImagePath:`nwant: $ExpectedCommand`ngot:  $($service.PathName)"
  }
}

function Assert-GuardServiceDefinition {
  param([string]$Stage)

  $service = Get-RalphService
  if ($null -eq $service) {
    throw "$Stage could not find SCM service $serviceName"
  }
  if ($service.State -ne "Stopped" -or [int]$service.ProcessId -ne 0) {
    throw "$Stage found SCM service $serviceName in state $($service.State) with PID $($service.ProcessId)"
  }
  if ($service.StartMode -ne "Manual") {
    throw "$Stage found SCM service $serviceName start mode $($service.StartMode), want Manual"
  }
  if ($service.DisplayName -ne "radioactive_ralph supervisor" -or
      $service.Description -ne "Durable radioactive_ralph supervisor") {
    throw "$Stage found non-historical display metadata"
  }
  foreach ($clause in @(
      $guardBin,
      "--supervisor",
      "--windows-service-config",
      $guardConfigPath
    )) {
    if (-not $service.PathName.Contains($clause)) {
      throw "$Stage service command omitted '$clause': $($service.PathName)"
    }
  }
  if (-not [string]::Equals($service.PathName, $guardCommand, [StringComparison]::OrdinalIgnoreCase)) {
    throw "$Stage SCM BinaryPathName changed:`nwant: $guardCommand`ngot:  $($service.PathName)"
  }
  return $service
}

function Wait-LegacyServiceRunning {
  param([string]$ExpectedCommand)

  for ($i = 0; $i -lt 120; $i++) {
    $service = Get-RalphService
    if ($null -eq $service) {
      throw "running legacy fixture disappeared before public uninstall"
    }
    if ($service.State -eq "Running" -and [int]$service.ProcessId -ne 0) {
      if (-not [string]::Equals($service.PathName, $ExpectedCommand, [StringComparison]::OrdinalIgnoreCase)) {
        throw "running legacy fixture ImagePath changed:`nwant: $ExpectedCommand`ngot:  $($service.PathName)"
      }
      return $service
    }
    Start-Sleep -Milliseconds 250
  }
  $last = Get-RalphService
  throw "legacy fixture did not reach Running/nonzero PID: $($last | Format-List * | Out-String)"
}

function Wait-GuardServiceStopped {
  for ($i = 0; $i -lt 120; $i++) {
    $service = Get-RalphService
    if ($null -eq $service) {
      throw "legacy execution-guard service disappeared before its exit status was inspected"
    }
    if ($service.State -eq "Stopped" -and
        [int]$service.ProcessId -eq 0) {
      return $service
    }
    Start-Sleep -Milliseconds 250
  }
  $last = Get-RalphService
  throw "legacy execution guard did not settle at Stopped/PID 0: $($last | Format-List * | Out-String)"
}

function Get-GuardPipePath {
  $bytes = [Text.Encoding]::UTF8.GetBytes($guardState)
  $digest = [Security.Cryptography.SHA256]::HashData($bytes)
  $token = [Convert]::ToHexString($digest).ToLowerInvariant().Substring(0, 12)
  return "\\.\pipe\radioactive_ralph-$token-service"
}

function Test-NamedPipeExists {
  param([string]$PipePath)

  $pipes = @([IO.Directory]::GetFiles("\\.\pipe\"))
  return $pipes -contains $PipePath
}

function Wait-ServiceAbsent {
  for ($i = 0; $i -lt 30; $i++) {
    if ($null -eq (Get-RalphService)) {
      return
    }
    Start-Sleep -Milliseconds 250
  }
  Assert-ServiceAbsent "timed out waiting for deletion"
}

function Invoke-Ralph {
  param([string[]]$Arguments)

  $output = @(& $Bin @Arguments 2>&1)
  return @{
    ExitCode = $LASTEXITCODE
    Output = ($output | Out-String).Trim()
  }
}

function Write-Diagnostics {
  Write-Output "windows scm diagnostics"
  Write-Output "sc.exe queryex $serviceName"
  & sc.exe queryex $serviceName
  Write-Output "sc.exe qc $serviceName"
  & sc.exe qc $serviceName
  if (Test-Path $configPath) {
    Write-Output "legacy service config: $configPath"
    Get-Content -Raw $configPath
  }
  if (Test-Path $stateProbe) {
    Write-Output "unexpected state tree: $stateProbe"
    Get-ChildItem -Force -Recurse $stateProbe |
      Select-Object FullName, Length, LastWriteTime |
      Format-Table -AutoSize
  }
  Write-Output "matching supervisor processes"
  Get-CimInstance Win32_Process `
    -Filter "Name='$ralphProcessName'" `
    -ErrorAction SilentlyContinue |
    Where-Object {
      $_.ExecutablePath -and
      ([string]::Equals($_.ExecutablePath, $resolvedBin, [StringComparison]::OrdinalIgnoreCase) -or
       [string]::Equals($_.ExecutablePath, $guardBin, [StringComparison]::OrdinalIgnoreCase) -or
       [string]::Equals($_.ExecutablePath, $legacyBin, [StringComparison]::OrdinalIgnoreCase)) -and
      $_.CommandLine -and
      $_.CommandLine -match '(?:^|\s)--supervisor(?:\s|$)'
    } |
    Select-Object ProcessId, ExecutablePath, CommandLine |
    Format-Table -AutoSize
  Write-Output "recent Service Control Manager events for $serviceName"
  Get-WinEvent -FilterHashtable @{
    LogName = "System"
    ProviderName = "Service Control Manager"
    StartTime = (Get-Date).AddMinutes(-10)
  } -ErrorAction SilentlyContinue |
    Where-Object { $_.Message -like "*$serviceName*" } |
    Select-Object TimeCreated, Id, LevelDisplayName, Message |
    Format-List
}

function Cleanup {
  if ($ownsServiceNamespace) {
    # The public uninstall is the primary cleanup path. Direct SCM deletion is
    # a last-resort test-harness cleanup only. This block is unreachable until
    # baseline proof establishes the service name and config path were absent,
    # so it can never destroy pre-existing operator state.
    try {
      & $Bin service uninstall *> $null
    } catch {
      # Continue to the bounded direct cleanup below.
    }
    $service = Get-RalphService
    if ($null -ne $service) {
      $ownedCommands = @($unknownCommand, $guardCommand, $legacyCommand) |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
      $definitionIsOwned = $false
      foreach ($ownedCommand in $ownedCommands) {
        if ([string]::Equals($service.PathName, $ownedCommand, [StringComparison]::OrdinalIgnoreCase)) {
          $definitionIsOwned = $true
          break
        }
      }
      if (-not $definitionIsOwned) {
        throw "cleanup refuses unrecognized same-name SCM registration: $($service.PathName)"
      }
      if ($service.State -ne "Stopped") {
        & sc.exe stop $serviceName *> $null
        Start-Sleep -Seconds 1
      }
      & sc.exe delete $serviceName *> $null
      Wait-ServiceAbsent
    }
    if (Test-Path $configPath) {
      Remove-Item -Force $configPath
    }
    if (-not $configDirExisted -and (Test-Path $configDir)) {
      $remaining = @(Get-ChildItem -Force $configDir)
      if ($remaining.Count -eq 0) {
        Remove-Item -Force $configDir
      }
    }
  }
  if (Test-Path $tmp) {
    Remove-Item -Recurse -Force $tmp
  }
}

try {
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null

  Assert-ServiceAbsent "initial state"
  if (Test-Path $configPath) {
    throw "initial state unexpectedly contains legacy config $configPath"
  }
  Assert-NoRalphSupervisor "initial state"
  $ownsServiceNamespace = $true

  # Build a deterministic, local child-execution canary. The command under
  # test remains the exact Ralph binary; --bin names this inert executable as
  # the would-be SCM payload. Any transient child launch writes the marker
  # before exiting, even if the installer subsequently rolls SCM state back.
  $markerLiteral = $canaryMarker | ConvertTo-Json -Compress
  @"
package main

import "os"

func main() {
	if err := os.WriteFile($markerLiteral, []byte("SCM child execution reached"), 0o600); err != nil {
		os.Exit(1)
	}
}
"@ | Set-Content -Encoding utf8NoBOM -Path $canarySource
  & go build -trimpath -o $canaryBin $canarySource
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path $canaryBin)) {
    throw "failed to build deterministic SCM execution canary"
  }

  $absentStatus = Invoke-Ralph -Arguments @("service", "status")
  if ($absentStatus.ExitCode -ne 0) {
    throw "service status failed before registration: $($absentStatus.Output)"
  }
  if ($absentStatus.Output -notmatch "supervisor service NOT installed" -or
      $absentStatus.Output -notmatch "windows-scm") {
    throw "absent service status was not explicit: $($absentStatus.Output)"
  }
  if (-not $configDirExisted -and (Test-Path $configDir)) {
    throw "absent service status created service config directory $configDir"
  }

  $install = Invoke-Ralph -Arguments @(
    "service", "install",
    "--bin", $canaryBin,
    "--env", "RALPH_STATE_DIR=$stateProbe"
  )
  if ($install.ExitCode -eq 0) {
    throw "native Windows service install unexpectedly succeeded: $($install.Output)"
  }
  foreach ($clause in @(
      "native Windows SCM service installation is disabled",
      "radioactive_ralph --supervisor",
      "WSL2",
      "systemd --user"
    )) {
    if (-not $install.Output.Contains($clause)) {
      throw "install rejection omitted '$clause': $($install.Output)"
    }
  }

  Assert-ServiceAbsent "rejected install"
  Assert-NoRalphSupervisor "rejected install"
  if (Test-Path $configPath) {
    throw "rejected install wrote service config $configPath"
  }
  if (Test-Path $stateProbe) {
    throw "rejected install created state root $stateProbe"
  }
  if (Test-Path $canaryMarker) {
    throw "rejected install transiently executed the SCM child payload: $canaryMarker"
  }
  if (-not $configDirExisted -and (Test-Path $configDir)) {
    throw "rejected install created service config directory $configDir"
  }

  # Ownership boundary proof: the stable service name alone is not authority
  # to stop or delete a registration. A disabled cmd.exe collision must be
  # rejected before ControlService/DeleteService, and its unrelated config
  # canary must remain byte-identical. The harness then removes only the
  # fixture it created through sc.exe so later proofs start from absence.
  $unknownCommand = "$env:SystemRoot\System32\cmd.exe /d /c exit 0"
  & sc.exe create $serviceName `
    "binPath=" $unknownCommand `
    "start=" "disabled" `
    "DisplayName=" "radioactive_ralph supervisor" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to create unknown same-name SCM collision"
  }
  & sc.exe description $serviceName "Durable radioactive_ralph supervisor" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to assign historical metadata to unknown same-name collision"
  }
  New-Item -ItemType Directory -Force -Path $configDir | Out-Null
  "UNKNOWN-COLLISION-CONFIG-MUST-REMAIN" |
    Set-Content -Encoding utf8NoBOM -Path $configPath
  $unknownConfigHash = (Get-FileHash -Algorithm SHA256 $configPath).Hash
  Assert-UnknownCollisionPreserved "unknown collision creation" $unknownCommand

  $unknownStatus = Invoke-Ralph -Arguments @("service", "status")
  if ($unknownStatus.ExitCode -eq 0) {
    throw "service status misreported unknown same-name collision as installed Ralph"
  }
  foreach ($clause in @(
      "verify radioactive_ralph-supervisor during inspection",
      "not a recognized legacy radioactive_ralph definition"
    )) {
    if (-not $unknownStatus.Output.Contains($clause)) {
      throw "unknown collision status rejection omitted '$clause': $($unknownStatus.Output)"
    }
  }
  Assert-UnknownCollisionPreserved "rejected unknown collision status" $unknownCommand
  if ((Get-FileHash -Algorithm SHA256 $configPath).Hash -ne $unknownConfigHash) {
    throw "unknown collision status changed legacy config $configPath"
  }

  $unknownUninstall = Invoke-Ralph -Arguments @("service", "uninstall")
  if ($unknownUninstall.ExitCode -eq 0) {
    throw "service uninstall unexpectedly removed unknown same-name collision"
  }
  foreach ($clause in @(
      "verify radioactive_ralph-supervisor before stop",
      "not a recognized legacy radioactive_ralph definition"
    )) {
    if (-not $unknownUninstall.Output.Contains($clause)) {
      throw "unknown collision rejection omitted '$clause': $($unknownUninstall.Output)"
    }
  }
  Assert-UnknownCollisionPreserved "rejected unknown collision uninstall" $unknownCommand
  if ((Get-FileHash -Algorithm SHA256 $configPath).Hash -ne $unknownConfigHash) {
    throw "unknown collision rejection changed legacy config $configPath"
  }
  Assert-NoRalphSupervisor "rejected unknown collision uninstall"

  & sc.exe delete $serviceName | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "direct harness cleanup failed to delete owned unknown collision"
  }
  Wait-ServiceAbsent
  Remove-Item -Force $configPath
  if (Test-Path $configPath) {
    throw "direct unknown-collision cleanup left config $configPath"
  }
  if (-not $configDirExisted -and (Test-Path $configDir)) {
    $remaining = @(Get-ChildItem -Force $configDir)
    if ($remaining.Count -eq 0) {
      Remove-Item -Force $configDir
    }
  }

  # Native process-entry proof: recreate the exact command shape used by an
  # earlier development build, but isolate every executable/config/state path
  # under this test's UUID temp root. The basename and exact argv are the
  # historical ownership signature; the UUID parent keeps paths isolated.
  New-Item -ItemType Directory -Force -Path $guardDir | Out-Null
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $guardConfigPath) | Out-Null
  Copy-Item -LiteralPath $resolvedBin -Destination $guardBin
  $resolvedBinHash = (Get-FileHash -Algorithm SHA256 $resolvedBin).Hash
  $guardBinHash = (Get-FileHash -Algorithm SHA256 $guardBin).Hash
  if (-not [string]::Equals($guardBinHash, $resolvedBinHash, [StringComparison]::OrdinalIgnoreCase)) {
    throw "SCM execution-guard copy differs from the built Ralph binary: source $resolvedBinHash, copy $guardBinHash"
  }
  @{
    extra_env = @{
      RALPH_STATE_DIR = $guardState
      RALPH_SCM_EXECUTION_CANARY = $guardConfigCanary
    }
  } | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8NoBOM -Path $guardConfigPath
  $guardConfigHash = (Get-FileHash -Algorithm SHA256 $guardConfigPath).Hash
  $guardPipePath = Get-GuardPipePath
  if (Test-NamedPipeExists $guardPipePath) {
    throw "isolated guard pipe unexpectedly existed before SCM launch: $guardPipePath"
  }

  $guardCommand = "`"$guardBin`" --supervisor --windows-service-config `"$guardConfigPath`""
  & sc.exe create $serviceName `
    "binPath=" $guardCommand `
    "start=" "demand" `
    "DisplayName=" "radioactive_ralph supervisor" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to create legacy SCM execution-guard registration"
  }
  & sc.exe description $serviceName "Durable radioactive_ralph supervisor" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to assign historical metadata to execution-guard registration"
  }
  $null = Assert-GuardServiceDefinition "legacy execution-guard registration"

  # Process-start tracing closes the gap where the byte-identical Ralph process
  # could start and exit between status polls. The baseline proves no owned
  # Ralph path is running, then the paired events bracket the one explicit SCM
  # start of this UUID-directory registration.
  $traceID = [guid]::NewGuid().ToString("N")
  $startSourceIdentifier = "ralph-scm-guard-start-$traceID"
  $stopSourceIdentifier = "ralph-scm-guard-stop-$traceID"
  $startSubscription = $null
  $stopSubscription = $null
  try {
    $startSubscription = Register-CimIndicationEvent `
      -Namespace "root/cimv2" `
      -Query "SELECT * FROM Win32_ProcessStartTrace WHERE ProcessName = '$ralphProcessName'" `
      -SourceIdentifier $startSourceIdentifier
    $stopSubscription = Register-CimIndicationEvent `
      -Namespace "root/cimv2" `
      -Query "SELECT * FROM Win32_ProcessStopTrace" `
      -SourceIdentifier $stopSourceIdentifier

    $startOutput = @(& sc.exe start $serviceName 2>&1)
    $startExitCode = $LASTEXITCODE
    $launchEvent = Wait-Event -SourceIdentifier $startSourceIdentifier -Timeout 15
    if ($null -eq $launchEvent) {
      throw "SCM start did not produce a $ralphProcessName process-start event: $($startOutput | Out-String)"
    }
    $guardProcessID = [uint32]$launchEvent.SourceEventArgs.NewEvent.ProcessID
    if ($guardProcessID -eq 0) {
      throw "SCM process-start event reported PID 0"
    }
    $guardProcessName = [string]$launchEvent.SourceEventArgs.NewEvent.ProcessName
    if (-not [string]::Equals(
        $guardProcessName,
        $ralphProcessName,
        [StringComparison]::OrdinalIgnoreCase
      )) {
      throw "SCM process-start event reported unexpected process name '$guardProcessName'"
    }
    $guardParentProcessID = [uint32]$launchEvent.SourceEventArgs.NewEvent.ParentProcessID
    $guardSessionID = [uint32]$launchEvent.SourceEventArgs.NewEvent.SessionID
    $guardSID = Convert-TraceSID $launchEvent.SourceEventArgs.NewEvent.Sid
    $guardProcessStartTraceTime = [uint64]$launchEvent.SourceEventArgs.NewEvent.TIME_CREATED
    # Win32_ProcessStopTrace still truncates long image names to 14 characters
    # on the Windows Server 2025 hosted runner. Bind that observed provider
    # representation to the full name captured by the paired start event.
    $guardStopTraceName = $guardProcessName.Substring(
      0,
      [Math]::Min(14, $guardProcessName.Length)
    )

    # Windows Server 2025 did not reliably deliver a provider-filtered
    # ProcessStopTrace even though the matching process had exited. Subscribe
    # to the full stop stream before launch, then consume events until the
    # exact start-trace PID appears. Removing every non-matching event prevents
    # Wait-Event from returning the same busy-runner process repeatedly.
    $stopEventFound = $false
    $stoppedProcessID = [uint32]0
    $guardProcessExit = [uint32]0
    $guardPIDStopCandidates = [System.Collections.Generic.List[string]]::new()
    $stopDeadline = (Get-Date).AddSeconds(15)
    while (-not $stopEventFound -and (Get-Date) -lt $stopDeadline) {
      $remainingSeconds = [Math]::Max(
        1,
        [int][Math]::Ceiling(($stopDeadline - (Get-Date)).TotalSeconds)
      )
      $candidate = Wait-Event `
        -SourceIdentifier $stopSourceIdentifier `
        -Timeout $remainingSeconds
      if ($null -eq $candidate) {
        continue
      }
      try {
        $candidateProcessID = [uint32]$candidate.SourceEventArgs.NewEvent.ProcessID
        $candidateProcessName = [string]$candidate.SourceEventArgs.NewEvent.ProcessName
        $candidateParentProcessID = [uint32]$candidate.SourceEventArgs.NewEvent.ParentProcessID
        $candidateSessionID = [uint32]$candidate.SourceEventArgs.NewEvent.SessionID
        $candidateSID = Convert-TraceSID $candidate.SourceEventArgs.NewEvent.Sid
        $candidateTraceTime = [uint64]$candidate.SourceEventArgs.NewEvent.TIME_CREATED
        if ($candidateProcessID -eq $guardProcessID) {
          $guardPIDStopCandidates.Add(
            "name='$candidateProcessName' parent=$candidateParentProcessID session=$candidateSessionID time=$candidateTraceTime exit=$([uint32]$candidate.SourceEventArgs.NewEvent.ExitStatus)"
          )
        }
        $candidateNameMatches = [string]::Equals(
          $candidateProcessName,
          $guardProcessName,
          [StringComparison]::OrdinalIgnoreCase
        ) -or [string]::Equals(
          $candidateProcessName,
          $guardStopTraceName,
          [StringComparison]::OrdinalIgnoreCase
        )
        if ($candidateProcessID -eq $guardProcessID -and
            $candidateParentProcessID -eq $guardParentProcessID -and
            $candidateSessionID -eq $guardSessionID -and
            $candidateSID -eq $guardSID -and
            $candidateNameMatches -and
            $candidateTraceTime -ge $guardProcessStartTraceTime) {
          $stoppedProcessID = $candidateProcessID
          $guardProcessExit = [uint32]$candidate.SourceEventArgs.NewEvent.ExitStatus
          $stopEventFound = $true
        }
      } finally {
        Remove-Event -EventIdentifier $candidate.EventIdentifier -ErrorAction SilentlyContinue
      }
    }
    if (-not $stopEventFound) {
      $candidateSummary = if ($guardPIDStopCandidates.Count -eq 0) {
        "none"
      } else {
        $guardPIDStopCandidates -join "; "
      }
      throw "SCM child process $guardProcessID did not produce an accepted process-stop event; PID-matching candidates: $candidateSummary"
    }
    if ($stoppedProcessID -ne $guardProcessID) {
      throw "SCM process trace PID mismatch: start $guardProcessID, stop $stoppedProcessID"
    }
    if ($guardProcessExit -ne 78) {
      throw "SCM-hosted Ralph process exited $guardProcessExit, want dedicated process-entry guard exit 78"
    }
  } finally {
    foreach ($source in @($startSourceIdentifier, $stopSourceIdentifier)) {
      Get-Event -SourceIdentifier $source -ErrorAction SilentlyContinue |
        Remove-Event -ErrorAction SilentlyContinue
      Unregister-Event -SourceIdentifier $source -ErrorAction SilentlyContinue
    }
    @($startSubscription, $stopSubscription) |
      Where-Object { $null -ne $_ } |
      Remove-Job -Force -ErrorAction SilentlyContinue
  }

  # SCM may remap a process that exits before StartServiceCtrlDispatcher to a
  # controller-specific status. The ProcessStopTrace ExitStatus above is the
  # authoritative child exit code; SCM must additionally settle at
  # Stopped/PID 0 and retain a nonzero controller-visible failure.
  $guardAfterExit = Wait-GuardServiceStopped
  if ([uint32]$guardAfterExit.ExitCode -eq 0) {
    throw "legacy SCM execution guard left a zero controller-visible exit status"
  }
  $guardProcesses = @(Get-CimInstance Win32_Process -Filter "ProcessId=$guardProcessID" -ErrorAction Stop)
  if ($guardProcesses.Count -ne 0) {
    throw "legacy SCM execution guard left process $guardProcessID alive"
  }
  Assert-NoRalphSupervisor "legacy SCM execution guard"
  if ((Get-FileHash -Algorithm SHA256 $guardConfigPath).Hash -ne $guardConfigHash) {
    throw "legacy SCM execution guard mutated its config file"
  }
  if (Test-Path $guardState) {
    throw "legacy SCM execution guard loaded config and created state at $guardState"
  }
  if (Test-Path $guardConfigCanary) {
    throw "legacy SCM execution guard materialized config canary $guardConfigCanary"
  }
  if (Test-NamedPipeExists $guardPipePath) {
    throw "legacy SCM execution guard left named pipe $guardPipePath"
  }
  Write-Output "legacy SCM execution guard: process $guardProcessID exit $guardProcessExit, sc.exe exit $startExitCode, service exit $($guardAfterExit.ExitCode)"

  # This fixture intentionally uses an isolated non-canonical config path so
  # it cannot qualify for public Ralph remediation. The harness created and
  # just proved this exact stopped definition, so remove it directly.
  & sc.exe delete $serviceName | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "direct harness cleanup failed after execution-guard proof"
  }
  Wait-ServiceAbsent
  Remove-Item -Force $guardConfigPath
  if (Test-Path $guardConfigPath) {
    throw "execution-guard cleanup left config $guardConfigPath"
  }
  Assert-NoRalphSupervisor "execution-guard cleanup"

  # Active STOP-rights proof. Build a benign SCM-aware fixture in an owned UUID
  # directory, but give it the historical executable basename and exact Ralph
  # marker argv. It reports Running, accepts Stop, records the transition, and
  # exits cleanly. Public uninstall must stop this live service before deletion.
  New-Item -ItemType Directory -Force -Path $legacyDir | Out-Null
  $serviceNameLiteral = $serviceName | ConvertTo-Json -Compress
  $runningMarkerLiteral = $legacyRunningMarker | ConvertTo-Json -Compress
  $stoppedMarkerLiteral = $legacyStoppedMarker | ConvertTo-Json -Compress
  @"
package main

import (
	"os"

	"golang.org/x/sys/windows/svc"
)

type legacyHandler struct{}

func writeMarker(path, value string) bool {
	return os.WriteFile(path, []byte(value), 0o600) == nil
}

func (legacyHandler) Execute(
	_ []string,
	requests <-chan svc.ChangeRequest,
	statuses chan<- svc.Status,
) (bool, uint32) {
	accepts := svc.AcceptStop
	statuses <- svc.Status{State: svc.StartPending}
	statuses <- svc.Status{State: svc.Running, Accepts: accepts}
	if !writeMarker($runningMarkerLiteral, "RUNNING") {
		return false, 1
	}
	for request := range requests {
		switch request.Cmd {
		case svc.Interrogate:
			statuses <- request.CurrentStatus
		case svc.Stop:
			statuses <- svc.Status{State: svc.StopPending}
			if !writeMarker($stoppedMarkerLiteral, "STOPPED") {
				return false, 1
			}
			return false, 0
		}
	}
	return false, 0
}

func main() {
	if err := svc.Run($serviceNameLiteral, legacyHandler{}); err != nil {
		os.Exit(2)
	}
}
"@ | Set-Content -Encoding utf8NoBOM -Path $legacySource
  & go build -trimpath -o $legacyBin $legacySource
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path $legacyBin)) {
    throw "failed to build benign running legacy SCM fixture"
  }

  $legacyCommand = "`"$legacyBin`" --supervisor --windows-service-config `"$configPath`""
  & sc.exe create $serviceName `
    "binPath=" $legacyCommand `
    "start=" "demand" `
    "DisplayName=" "radioactive_ralph supervisor" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to create running legacy SCM fixture registration"
  }
  & sc.exe description $serviceName "Durable radioactive_ralph supervisor" | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "failed to assign historical metadata to running legacy fixture"
  }

  # There is deliberately no config yet. This result can only come from an SCM
  # query, not from checking the historical config path on disk.
  $legacyStatus = Invoke-Ralph -Arguments @("service", "status")
  if ($legacyStatus.ExitCode -ne 0) {
    throw "service status failed for stopped legacy registration: $($legacyStatus.Output)"
  }
  if ($legacyStatus.Output -notmatch "supervisor service installed" -or
      $legacyStatus.Output -notmatch "windows-scm") {
    throw "status did not detect stopped legacy SCM registration without config: $($legacyStatus.Output)"
  }
  if (Test-Path $configPath) {
    throw "status wrote legacy config instead of remaining read-only: $configPath"
  }
  if (-not $configDirExisted -and (Test-Path $configDir)) {
    throw "status created service config directory instead of remaining read-only: $configDir"
  }
  $statusFixture = Get-RalphService
  if ($null -eq $statusFixture -or
      $statusFixture.State -ne "Stopped" -or
      -not [string]::Equals($statusFixture.PathName, $legacyCommand, [StringComparison]::OrdinalIgnoreCase)) {
    throw "service status did not preserve stopped historical fixture definition"
  }
  Assert-NoRalphSupervisor "service status"

  New-Item -ItemType Directory -Force -Path $configDir | Out-Null
  @{
    extra_env = @{
      RALPH_STATE_DIR = $stateProbe
    }
    legacy_cleanup_smoke = $true
  } | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8NoBOM -Path $configPath
  if (-not (Test-Path $configPath)) {
    throw "failed to create legacy service config $configPath"
  }
  $legacyConfigHash = (Get-FileHash -Algorithm SHA256 $configPath).Hash

  $legacyStartOutput = @(& sc.exe start $serviceName 2>&1)
  if ($LASTEXITCODE -ne 0) {
    throw "failed to start benign legacy fixture: $($legacyStartOutput | Out-String)"
  }
  $runningLegacy = Wait-LegacyServiceRunning $legacyCommand
  for ($i = 0; $i -lt 40 -and -not (Test-Path $legacyRunningMarker); $i++) {
    Start-Sleep -Milliseconds 250
  }
  if (-not (Test-Path $legacyRunningMarker)) {
    throw "legacy fixture reached Running but did not write running marker"
  }
  if ((Get-FileHash -Algorithm SHA256 $configPath).Hash -ne $legacyConfigHash) {
    throw "running legacy fixture changed config before public uninstall"
  }
  $runningLegacyPID = [uint32]$runningLegacy.ProcessId
  Write-Output "running legacy fixture: PID $runningLegacyPID"

  $uninstall = Invoke-Ralph -Arguments @("service", "uninstall")
  if ($uninstall.ExitCode -ne 0) {
    throw "service uninstall failed for running recognized legacy fixture: $($uninstall.Output)"
  }
  Wait-ServiceAbsent
  if (-not (Test-Path $legacyStoppedMarker)) {
    throw "public uninstall deleted legacy fixture without proving accepted Stop transition"
  }
  $legacyProcesses = @(Get-CimInstance Win32_Process -Filter "ProcessId=$runningLegacyPID" -ErrorAction Stop)
  if ($legacyProcesses.Count -ne 0) {
    throw "public uninstall left legacy fixture process $runningLegacyPID alive"
  }
  if (Test-Path $configPath) {
    throw "service uninstall did not remove legacy config $configPath"
  }
  Assert-NoRalphSupervisor "service uninstall"

  $secondUninstall = Invoke-Ralph -Arguments @("service", "uninstall")
  if ($secondUninstall.ExitCode -ne 0) {
    throw "second service uninstall was not idempotent: $($secondUninstall.Output)"
  }
  Assert-ServiceAbsent "idempotent uninstall"
  Assert-NoRalphSupervisor "idempotent uninstall"
  if (Test-Path $configPath) {
    throw "idempotent uninstall recreated legacy config $configPath"
  }

  Write-Output "windows scm fail-closed smoke: ok"
} catch {
  Write-Diagnostics
  throw
} finally {
  Cleanup
}

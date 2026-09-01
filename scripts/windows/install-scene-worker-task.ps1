param(
    [string]$EnvironmentFile = ".env.worker",
    [string]$TaskName = "GoGIF Scene Worker",
    [switch]$Start,
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$WorkerScript = Join-Path $PSScriptRoot "run-scene-worker.ps1"
$EnvironmentPath = if ([System.IO.Path]::IsPathRooted($EnvironmentFile)) {
    $EnvironmentFile
} else {
    Join-Path $RepoRoot $EnvironmentFile
}

if ($Uninstall) {
    $ExistingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -ne $ExistingTask) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
        Write-Host "Removed scheduled task: $TaskName"
    } else {
        Write-Host "Scheduled task is not installed: $TaskName"
    }
    exit 0
}

if (-not (Test-Path $WorkerScript -PathType Leaf)) {
    throw "Scene worker launcher not found: $WorkerScript"
}
if (-not (Test-Path $EnvironmentPath -PathType Leaf)) {
    throw "Scene worker environment file not found: $EnvironmentPath"
}

$PowerShell = (Get-Process -Id $PID).Path
$UserId = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$ActionArguments = @(
    "-NoLogo"
    "-NoProfile"
    "-ExecutionPolicy Bypass"
    ('-File "{0}"' -f $WorkerScript)
    ('-EnvironmentFile "{0}"' -f $EnvironmentPath)
) -join " "

$Action = New-ScheduledTaskAction -Execute $PowerShell -Argument $ActionArguments -WorkingDirectory $RepoRoot
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $UserId
$Principal = New-ScheduledTaskPrincipal `
    -UserId $UserId `
    -LogonType Interactive `
    -RunLevel Limited
$Settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -MultipleInstances IgnoreNew `
    -RestartCount 10 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -StartWhenAvailable
$Task = New-ScheduledTask -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings

$RegisteredTask = Register-ScheduledTask -TaskName $TaskName -InputObject $Task -Force -ErrorAction Stop
if ($null -eq $RegisteredTask -or $RegisteredTask.TaskName -ne $TaskName) {
    throw "Scheduled task registration did not return the expected task: $TaskName"
}
Write-Host "Installed scheduled task: $TaskName"
Write-Host "The worker will start at login and restart after transient failures."

if ($Start) {
    Start-ScheduledTask -TaskName $TaskName
    Write-Host "Started scheduled task: $TaskName"
}

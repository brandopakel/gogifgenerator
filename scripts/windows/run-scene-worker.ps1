param(
    [string]$EnvironmentFile = ".env.worker",
    [switch]$Once
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$EnvironmentPath = if ([System.IO.Path]::IsPathRooted($EnvironmentFile)) {
    $EnvironmentFile
} else {
    Join-Path $RepoRoot $EnvironmentFile
}

if (-not (Test-Path $EnvironmentPath -PathType Leaf)) {
    throw "Scene worker environment file not found: $EnvironmentPath. Copy .env.worker.example to .env.worker first."
}

foreach ($Line in Get-Content $EnvironmentPath) {
    $Value = $Line.Trim()
    if ($Value.Length -eq 0 -or $Value.StartsWith("#")) {
        continue
    }
    $Parts = $Value.Split("=", 2)
    if ($Parts.Length -ne 2 -or $Parts[0].Trim().Length -eq 0) {
        throw "Invalid environment entry in $EnvironmentPath"
    }
    [System.Environment]::SetEnvironmentVariable($Parts[0].Trim(), $Parts[1], "Process")
}

$Worker = Join-Path $RepoRoot "bin\gogif-scene-worker.exe"
if (-not (Test-Path $Worker -PathType Leaf)) {
    New-Item -ItemType Directory -Force (Split-Path $Worker) | Out-Null
    Push-Location $RepoRoot
    try {
        go build -trimpath -o $Worker ./cmd/gogif-scene-worker
    } finally {
        Pop-Location
    }
}

if ($Once) {
    & $Worker -once
} else {
    & $Worker
}
exit $LASTEXITCODE

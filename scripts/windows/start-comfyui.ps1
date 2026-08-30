[CmdletBinding()]
param(
    [string]$ComfyRoot = "$env:USERPROFILE\Documents\GoGIF\ComfyUI",
    [int]$Port = 8188
)

$ErrorActionPreference = 'Stop'
$python = Join-Path $ComfyRoot '.venv\Scripts\python.exe'
$main = Join-Path $ComfyRoot 'main.py'
if (-not (Test-Path -LiteralPath $python) -or -not (Test-Path -LiteralPath $main)) {
    throw "ComfyUI was not found at $ComfyRoot"
}

Push-Location $ComfyRoot
try {
    & $python $main --listen 127.0.0.1 --port $Port --disable-auto-launch
    if ($LASTEXITCODE -ne 0) {
        throw "ComfyUI exited with code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

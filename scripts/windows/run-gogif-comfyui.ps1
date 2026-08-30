[CmdletBinding()]
param(
    [string]$ComfyRoot = "$env:USERPROFILE\Documents\GoGIF\ComfyUI",
    [string]$Checkpoint = 'v1-5-pruned-emaonly-fp16.safetensors',
    [string]$Address = '127.0.0.1:8080'
)

$ErrorActionPreference = 'Stop'
$checkpointPath = Join-Path $ComfyRoot "models\checkpoints\$Checkpoint"
$inputDirectory = Join-Path $ComfyRoot 'input'
if (-not (Test-Path -LiteralPath $checkpointPath)) {
    throw "Checkpoint was not found at $checkpointPath"
}
if (-not (Test-Path -LiteralPath $inputDirectory)) {
    throw "ComfyUI input directory was not found at $inputDirectory"
}

$env:GOGIF_ADDR = $Address
$env:GOGIF_IMAGE_GENERATOR = 'comfyui'
$env:GOGIF_COMFYUI_URL = 'http://127.0.0.1:8188'
$env:GOGIF_COMFYUI_CHECKPOINT = $Checkpoint
$env:GOGIF_COMFYUI_INPUT_DIR = $inputDirectory

go run ./cmd/gogif
if ($LASTEXITCODE -ne 0) {
    throw "GoGIF exited with code $LASTEXITCODE"
}

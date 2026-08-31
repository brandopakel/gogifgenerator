# Local generation without API bills

GoGIF has three no-key creation paths. The built-in Go renderer is instant and dependency-free. Blender produces original procedural 3D art. ComfyUI runs a diffusion checkpoint locally and can transform one rights-approved Wikimedia image.

It also has separately opt-in hosted semantic paths using OpenAI's Image API or a ComfyUI FLUX Partner workflow. Every semantic adapter implements the same `internal/imagegen` contract. Normal **Realistic AI** requests animate that source with the lightweight Go renderer; only explicit **Studio Local** requests invoke the downstream cinematic pipeline.

A fourth, explicitly enabled cinematic path coordinates Blender, Unity 6.3, Unreal Engine 5, and FFmpeg. It can start from a local/reference image without a hosted dependency or use the configured hosted semantic generator. The editor installations remain local, large, and subject to their own activation, licensing, and hardware requirements. See [Cinematic pipeline](CINEMATIC_PIPELINE.md).

Local software does not create a vendor bill, but it still uses your disk, bandwidth, CPU/GPU, and electricity. Every downloaded checkpoint has its own license; “free to download” does not automatically mean unrestricted commercial use.

## Blender

[Blender is free and open source](https://www.blender.org/about/license/), requires no registration, and says the artwork produced with it belongs to its creator. GoGIF keeps the small `bpy`-using scene program in a separately marked GPL file.

```sh
GOGIF_IMAGE_GENERATOR=blender make run
```

This engine is procedural rather than generative AI. It currently turns the prompt and seed into a 3D still, then GoGIF adds motion and encodes the GIF. It does not accept a provider reference image.

## ComfyUI

[ComfyUI is open source and runs locally](https://docs.comfy.org/). Install it using the [official desktop instructions](https://docs.comfy.org/installation/desktop) or [manual installation guide](https://docs.comfy.org/installation/manual_install), then place a compatible checkpoint in its `models/checkpoints` directory. GoGIF uses only ComfyUI's documented native `/prompt`, `/history`, `/view`, and `/upload/image` routes; it does not use Comfy Cloud.

Start ComfyUI on loopback, then run GoGIF with the exact checkpoint filename shown in ComfyUI:

```sh
export GOGIF_IMAGE_GENERATOR=comfyui
export GOGIF_COMFYUI_URL=http://127.0.0.1:8188
export GOGIF_COMFYUI_CHECKPOINT=your-checkpoint.safetensors
make run
```

Text-to-image works with those settings. To enable the UI's **Use as reference** button, also provide the ComfyUI input directory:

```sh
export GOGIF_COMFYUI_INPUT_DIR=/absolute/path/to/ComfyUI/input
make run
```

The directory must exist and belong to the same machine/filesystem as the GoGIF process. GoGIF assigns a random filename under `input/gogif`, queues the native workflow, obtains the generated image, and deletes the uploaded reference before returning the GIF. If deletion fails, the request fails rather than silently retaining provider media.

The initial workflow uses standard checkpoint, CLIP, VAE, and KSampler nodes. A checkpoint that requires a custom graph or custom nodes needs its own audited workflow adapter.

The verified starter checkpoint is [Comfy-Org's Stable Diffusion 1.5 FP16 archive](https://huggingface.co/Comfy-Org/stable-diffusion-v1-5-archive). It is an older 2.13 GB model under the CreativeML Open RAIL-M license, chosen as a small compatibility baseline—not as the eventual production-quality model. Read its model card and license before shipping generated outputs.

The current M3 test Mac has only 8 GB of unified memory. ComfyUI's documented FLUX.2 Klein 4B distilled workflow cites roughly 8.4 GB of GPU memory before the operating system, GoGIF, Unity, or Unreal are counted, so that model is not a safe local target on this machine. Use a smaller compatibility checkpoint locally, a better-equipped GPU worker, or the hosted adapter for production-quality reference art.

## OpenAI semantic imagery

GoGIF uses the single-image Image API for prompt-only source art and its image-edit endpoint when controlled reference bytes are present. The API key is read only by the Go process and is omitted from `/api/v1/config`.

```sh
export OPENAI_API_KEY="your-project-key"
export GOGIF_ENABLE_PAID_IMAGE_GENERATION=true
export GOGIF_OPENAI_IMAGE_MODEL=gpt-image-2
export GOGIF_OPENAI_IMAGE_QUALITY=high
make run
```

The model supports larger source resolutions than many GIF canvases. GoGIF generates at a supported square/portrait/landscape size, decodes and bounds the response, center-crops it to the requested aspect ratio, and resamples it to the exact output dimensions before any engine receives it. Hosted generation can incur charges and is never enabled merely because an API key happens to be present. See [OpenAI's image generation guide](https://developers.openai.com/api/docs/guides/image-generation).

## Comfy hosted semantic imagery

For this 8-GB Mac, the practical production route is to run semantic inference on hosted GPU infrastructure and use the lightweight Go animation path by default. The current audited recipe uses ComfyUI's `FluxProUltraImageNode` in raw mode for natural-looking output, followed by `SaveImage`. Browser requests can set only the prompt, dimensions, and seed; they cannot provide nodes or workflow JSON.

```sh
export COMFY_CLOUD_API_KEY="your-comfy-account-key"
export GOGIF_ENABLE_PAID_IMAGE_GENERATION=true
export GOGIF_IMAGE_GENERATOR=comfyui-cloud
export GOGIF_COMFYUI_IMAGE_URL=https://cloud.comfy.org/api
export GOGIF_COMFYUI_IMAGE_RECIPE=flux-ultra
make run
```

GoGIF sends the key in `X-API-Key` only to Comfy Cloud and supplies it in ComfyUI's documented `extra_data` field for the Partner Node. It follows Cloud's current `/api/jobs/{id}` status contract rather than the deprecated history endpoint. The `/view` redirect is restricted to HTTPS and followed without the API key. Returned image bytes and decoded pixel dimensions are bounded before the image is cropped and resampled to the exact GIF canvas. Selecting **Studio Local** uses the same hosted source but then launches Blender, Unity, Unreal, and FFmpeg locally; it is not a cloud render farm.

Comfy Cloud API execution requires a Creator or Pro subscription. FLUX 1.1 Pro Ultra is a paid Partner Node, so its inference also consumes account credits. A local ComfyUI process can orchestrate the same hosted Partner Node by setting `GOGIF_COMFYUI_IMAGE_URL=http://127.0.0.1:8188`; this avoids the Cloud subscription requirement but still requires Comfy credits and a running local backend.

## ComfyUI 3D workflows

The **3D** tab is a separate output pipeline. It runs an allowlisted ComfyUI recipe, validates the returned binary glTF signature and 256 MiB size bound, stores the GLB as `media.KindModel`, and returns a managed model URL. The browser uses `<model-viewer>` for camera control and auto-rotation. Save `.glb` always works; file share and direct binary clipboard copy depend on the browser, with a managed-URL clipboard fallback.

Two prompt recipes are currently registered:

- `tripo-3.1`: detailed geometry and PBR textures; the preferred default.
- `hunyuan-3.1`: Hunyuan 3D 3.1 normal mesh with PBR textures.

Both are paid Comfy Partner Nodes. A Comfy account key, credits, and an explicit spend opt-in are required even when Comfy Desktop performs the orchestration locally:

```sh
export COMFY_CLOUD_API_KEY="your-comfy-account-key"
export GOGIF_ENABLE_PAID_MODEL_GENERATION=true
export GOGIF_MODEL_GENERATOR=comfyui
export GOGIF_COMFYUI_MODEL_URL=https://cloud.comfy.org/api
export GOGIF_COMFYUI_MODEL_RECIPE=tripo-3.1
make run
```

For local orchestration, use `GOGIF_COMFYUI_MODEL_URL=http://127.0.0.1:8188` with ComfyUI Desktop running. Remote endpoints must use HTTPS and a key; output redirects are followed without forwarding that key to object storage. Comfy's official workflows also include image-to-model and multiview-to-model. Those belong in later audited recipes once the upload lifecycle and job-progress UI are extended to 3D inputs.

## Using the Windows PC over Tailscale

The PC's NVIDIA GPU is useful, but it remains an optional deployment target; it is not a new architectural dependency. For text-to-image experiments, an SSH tunnel can expose PC loopback as Mac loopback:

```sh
ssh -N -L 8188:127.0.0.1:8188 awbp
```

For reference-image generation, run both GoGIF and ComfyUI on the PC so they share the same ComfyUI input directory. Keep both servers bound to loopback and tunnel GoGIF to the Mac instead:

```sh
ssh -N -L 8080:127.0.0.1:8080 awbp
```

Then open <http://localhost:8080> on the Mac. Termius can save the same local-forward rule. No Tailscale IP or remote hostname is embedded in GoGIF, so laptops without that PC continue to use the built-in Go renderer or Blender normally.

After the repository is cloned on Windows, the checked-in PowerShell helpers make the two-process setup repeatable:

```powershell
.\scripts\windows\start-comfyui.ps1
# In a second terminal:
.\scripts\windows\run-gogif-comfyui.ps1
```

## Controlled reference lifecycle

1. Search returns provider metadata and provider-hosted previews.
2. The server resolves the selected external ID again; it never trusts a URL sent by the browser.
3. Only derivative-approved results from an exact allowlisted HTTPS host proceed.
4. The source is capped at 20 MiB and checked as PNG, JPEG, GIF, or WebP while streaming to a secure temporary file.
5. ComfyUI receives the validated bytes and produces a new image.
6. The ComfyUI upload and GoGIF temporary file are deleted before the response succeeds.
7. Only the original GoGIF output is eligible for persistent blob storage. Its catalog record retains source ID, attribution, license, and share-alike obligations—not the source bytes.

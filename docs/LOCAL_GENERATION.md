# Local generation without API bills

GoGIF has three no-key creation paths. The built-in Go renderer is instant and dependency-free. Blender produces original procedural 3D art. ComfyUI runs a diffusion checkpoint locally and can transform one rights-approved Wikimedia image.

A fourth, explicitly enabled cinematic path coordinates Blender, Unity 6.3, Unreal Engine 5, and FFmpeg. It has no hosted API dependency, but the editor installations are large and have their own activation, licensing, and hardware requirements. See [Cinematic pipeline](CINEMATIC_PIPELINE.md).

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

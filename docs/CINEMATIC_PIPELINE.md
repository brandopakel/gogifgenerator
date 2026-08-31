# Local cinematic quality pipeline

GoGIF now has a concrete multi-engine contract:

```text
GPT Image 2 / ComfyUI / licensed reference image
              ↓
Blender 5.x ── textured FBX asset + preview
              ↓
Unity 6.3 ─── portable motion.json + transparent VFX PNGs
              ↓
Unreal 5 ──── cinematic beauty PNGs using FBX + motion.json
              ↓
Go ────────── animate/preserve semantic reference + blend Unreal/Unity passes
              ↓
FFmpeg ────── per-animation palette + looping GIF
```

Unity and Unreal are not interchangeable renderers in this design. Unity owns portable motion authoring and the transparent real-time VFX pass. Unreal consumes the Blender asset and Unity motion contract and owns the final lit beauty pass. GoGIF composites those passes only after both sequences match the manifest exactly. This preserves a clear contribution from all three engines without averaging unrelated finished images.

## Current readiness

The Go orchestrator, strict job manifest, Blender FBX stage, Unity batch project/script, Unreal tick-driven batch project/script, compositor, FFmpeg encoder, API capability reporting, fallback behavior, and contract tests are implemented.

The current Mac has Blender 5.2 LTS, FFmpeg 9, Unity 6000.3.23f1, Unreal Engine 5.8.2, Xcode 26.1.1, Apple's Metal toolchain, and Comfy Desktop installed. Unity Personal is activated. Unity produced a validated transparent PNG/motion sequence; Unreal imported the Blender FBX and produced a validated square PNG sequence; and an API request completed the full Blender → Unity → Unreal → FFmpeg path. A second request using a rights-approved temporary Wikimedia reference returned `reference+blender+unity-6.3+unreal-5+ffmpeg+local` and visually retained the semantic image.

This Mac has 8 GB of memory. Unreal's macOS requirements list 16 GB as the minimum and 32 GB as recommended, so it is suitable for functional testing but not representative production rendering. The cinematic pipeline remains opt-in, serialized, and selected only by `generation_mode=studio`. Normal `generation_mode=semantic` requests use hosted semantic imagery plus the lightweight Go animator and do not launch the editors.

Unity 6.3 LTS is the intended Unity target; Unity documents it as the current LTS line supported through December 2027. Its editor supports unattended `-batchmode` plus `-executeMethod`, which is what the Go runner uses. Unreal's official editor scripting supports `-ExecutePythonScript`, and its Movie Render Pipeline is the longer-term replacement for the starter high-resolution capture pass.

Official references:

- [Unity 6 release support](https://unity.com/releases/unity-6/support)
- [Unity editor command-line arguments](https://docs.unity3d.com/cn/current/Manual/EditorCommandLineArguments.html)
- [Unreal editor Python command-line scripting](https://dev.epicgames.com/documentation/en-us/unreal-engine/scripting-the-unreal-editor-using-python)
- [Unreal Movie Render Pipeline](https://dev.epicgames.com/documentation/en-us/unreal-engine/movie-render-pipeline-in-unreal-engine)
- [Blender command-line automation](https://docs.blender.org/manual/en/latest/advanced/command_line/index.html)

## Install and activate the editors

Install a current Unity 6000.3 patch through Unity Hub and Unreal Engine 5 through Epic Games Launcher. Finish any account activation or license acceptance interactively. Do not place account credentials in `.env` or command-line arguments.

Open each checked-in starter project once:

- `engines/unity`
- `engines/unreal/GoGIF.uproject`

Let imports and shader compilation finish, resolve any editor-requested project-version update, then close the editor. The committed Unity project contains `GoGIF.Editor.BatchRenderer.Render`; the Unreal project enables Python editor scripting and contains `Content/Python/gogif_render.py`.

Unreal 5.8 on macOS also needs the matching full Xcode and Metal toolchain. If Xcode is installed but not selected system-wide, either run `sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer` yourself or export `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer` for the GoGIF process. Never provide an administrator password to the app.

## Configure GoGIF

Use absolute paths. Example macOS configuration:

```sh
export GOGIF_ENABLE_QUALITY_PIPELINE=true
export GOGIF_BLENDER_EXECUTABLE=/opt/homebrew/bin/blender
export GOGIF_FFMPEG_EXECUTABLE=/opt/homebrew/bin/ffmpeg

export GOGIF_UNITY_EXECUTABLE="/Applications/Unity/Hub/Editor/6000.3.23f1/Unity.app/Contents/MacOS/Unity"
export GOGIF_UNITY_PROJECT="$PWD/engines/unity"

export GOGIF_UNREAL_EXECUTABLE="/Users/Shared/Epic Games/UE_5.8/Engine/Binaries/Mac/UnrealEditor-Cmd"
export GOGIF_UNREAL_PROJECT="$PWD/engines/unreal/GoGIF.uproject"
export GOGIF_UNREAL_SCRIPT="$PWD/engines/unreal/Content/Python/gogif_render.py"
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer

make run
```

Replace versioned editor paths with the versions actually installed. On Windows, point the executable variables at `Unity.exe`, `UnrealEditor-Cmd.exe`, and `ffmpeg.exe`.

Enabling these variables only advertises **Studio Local** as an available mode. It does not route ordinary **Realistic AI** creations through the editors. Select Studio explicitly in the PWA or send `"generation_mode":"studio"` to the generation API. Use `"generation_mode":"semantic"` for cloud source generation plus lightweight local animation, or `"generation_mode":"fast"` for zero-credit procedural Go rendering.

For AI-generated reference art, configure local ComfyUI or the hosted adapter. When the cinematic pipeline is enabled, a semantic still generator becomes its reference-imagery stage:

```sh
export GOGIF_IMAGE_GENERATOR=comfyui
export GOGIF_COMFYUI_URL=http://127.0.0.1:8188
export GOGIF_COMFYUI_CHECKPOINT=your-checkpoint.safetensors
```

or:

```sh
export OPENAI_API_KEY=your-project-key
export GOGIF_ENABLE_PAID_IMAGE_GENERATION=true
export GOGIF_IMAGE_GENERATOR=openai
export GOGIF_OPENAI_IMAGE_MODEL=gpt-image-2
export GOGIF_OPENAI_IMAGE_QUALITY=high
```

Without a semantic generator, Blender still creates a deterministic prompt-seeded scene; adding Unity and Unreal does not make that procedural geometry understand named subjects. A rights-approved selected provider image can become the temporary reference input. GoGIF carries that normalized image through textured Blender geometry and keeps it as an animated 2.5D compositor base, then blends the validated Unreal beauty and Unity VFX passes above it. This prevents FBX material-import differences from silently erasing the subject while adding restrained camera movement to a generated keyframe.

## Operational guarantees

- The feature is opt-in; merely installing an editor does not launch it.
- Only an explicit **Studio Local** request launches Blender, Unity, and Unreal. **Realistic AI** never enters this local engine chain.
- Enabling the pipeline is strict: GoGIF refuses to start when an executable, Unity 6.3 project, Unreal 5 project, or Unreal Python script is invalid.
- Engine processes receive a Go-authored manifest path, never raw user-supplied command arguments.
- Local cinematic jobs are serialized because Unity cannot safely open the same project from two editor processes at once; a canceled request can stop while waiting for the engine slot.
- All reference bytes, FBX files, motion data, frame sequences, logs, and intermediate GIFs live in a request-scoped temporary directory.
- PNG dimensions and counts are checked after Unity and Unreal; FBX, motion JSON, PNG, and GIF sizes are bounded. Unreal receives the manifest dimensions as its render viewport and its latent screenshot tasks yield between editor ticks.
- **Fast local** calls neither the semantic image provider nor the cinematic renderer. It never consumes hosted credits or labels its output as Unity or Unreal.
- A **Realistic AI** request is fail-closed. A missing or failed semantic generator returns a clear 503/502 response and never disguises procedural shapes as a subject-aware result.
- Provider-reference generation remains fail-closed when no configured path can safely transform the reference.

## Next quality work after editor installation

1. Run the checked-in Unity and Unreal projects on the NVIDIA Windows machine; macOS functional validation is complete.
2. Replace Unreal's starter high-resolution screenshot pass with a project-owned Movie Render Queue preset or Python executor for temporal samples, anti-aliasing, motion blur, and EXR output.
3. Complete Comfy Desktop's one-time MPS initialization and evaluate a small local checkpoint; use GPT Image 2 or a better-equipped GPU worker for high-quality prompt-only testing on this 8-GB Mac.
4. Add character rig and animation interchange, likely FBX animation clips or Alembic where both engine projects can reproduce it.
5. Measure end-to-end p50/p95 time, peak RAM/VRAM, failure rate, and visual scores before making the pipeline the default.
6. Add MP4/WebM output beside GIF so high-detail renders are not forced through a 256-color final format.

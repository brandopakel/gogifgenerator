# Experimental scene pipeline

Hosting, worker isolation, licensing gates, and the new asynchronous API contract are documented in [Scene workspace hosting](SCENE_HOSTING.md).

The product direction separates semantic creation from scene rendering:

```text
ComfyUI / licensed reference / generated GLB
                     ↓
Blender 5.x ── geometry, materials, rigs, portable assets
                     ↓
             choose one target
              ↙             ↘
Unity 6.3 ─ real-time      Unreal 5 ─ cinematic
animation / VFX / app      lighting / environments / film
              ↘             ↙
        MP4 or WebM master ──> optional GIF derivative
```

ComfyUI owns prompt understanding. Blender owns reusable asset preparation. Unity and Unreal are alternative scene targets, not filters that automatically improve one another. FFmpeg should preserve a high-color video master before making a palette-limited GIF when one is needed.

## Current readiness

The repository contains a strict job manifest, Blender FBX stage, Unity batch project/script, Unreal tick-driven batch project/script, compositor, FFmpeg encoder, capability reporting, and contract tests. That proof-of-concept currently runs all three editors in sequence. It remains useful for stage validation, but it is now a legacy developer path—not a user-facing GIF mode and not the target Scene architecture.

The current Mac has Blender 5.2 LTS, FFmpeg 9, Unity 6000.3.23f1, Unreal Engine 5.8.2, Xcode 26.1.1, Apple's Metal toolchain, and Comfy Desktop installed. Unity Personal is activated. Unity produced a validated transparent PNG/motion sequence; Unreal imported the Blender FBX and produced a validated square PNG sequence; and an API request completed the full Blender → Unity → Unreal → FFmpeg path. A second request using a rights-approved temporary Wikimedia reference returned `reference+blender+unity-6.3+unreal-5+ffmpeg+local` and visually retained the semantic image.

This Mac has 8 GB of memory. Unreal's macOS requirements list 16 GB as the minimum and 32 GB as recommended, so it is suitable for functional testing but not representative production rendering. The legacy chain remains opt-in and serialized behind `GOGIF_ENABLE_QUALITY_PIPELINE`. The PWA always sends `generation_mode=semantic`, which uses hosted semantic imagery plus the lightweight Go animator and does not launch any editor.

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

Enabling these variables configures the legacy developer pipeline; it does not add a visual-source choice to the PWA and does not route ordinary GIF creation through the editors. A deliberate API request with `"generation_mode":"studio"` is currently required to exercise the proof-of-concept. Use `"generation_mode":"semantic"` for the supported cloud-source plus lightweight-animation path.

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
- Only an explicit developer `generation_mode=studio` request launches the legacy chain. The PWA cannot select it.
- Enabling the pipeline is strict: GoGIF refuses to start when an executable, Unity 6.3 project, Unreal 5 project, or Unreal Python script is invalid.
- Engine processes receive a Go-authored manifest path, never raw user-supplied command arguments.
- Local cinematic jobs are serialized because Unity cannot safely open the same project from two editor processes at once; a canceled request can stop while waiting for the engine slot.
- All reference bytes, FBX files, motion data, frame sequences, logs, and intermediate GIFs live in a request-scoped temporary directory.
- PNG dimensions and counts are checked after Unity and Unreal; FBX, motion JSON, PNG, and GIF sizes are bounded. Unreal receives the manifest dimensions as its render viewport and its latent screenshot tasks yield between editor ticks.
- The `fast` compatibility mode calls neither the semantic provider nor the scene renderer and remains outside the product UI.
- A semantic GIF request is fail-closed. A missing or failed semantic generator returns a clear 503/502 response and never disguises procedural shapes as a subject-aware result.
- Provider-reference generation remains fail-closed when no configured path can safely transform the reference.

## Next Scene workspace work

1. Add a persistent scene-project manifest for assets, cameras, lights, animation, VFX, render target, output format, and provenance.
2. Split the current sequential runner into Blender preparation plus one renderer selected by `engine_target=unity` or `engine_target=unreal`.
3. Queue scene work asynchronously with progress events, cancellation, and a separately provisioned worker so the web/API Mac stays responsive.
4. Store reproducible source/intermediate assets and MP4/WebM masters; derive GIF only on request.
5. Add character rig/animation interchange through validated FBX animation or Alembic contracts.
6. Replace Unreal's starter screenshot pass with Movie Render Queue and measure latency, memory, failures, and visual quality before exposing Scene publicly.

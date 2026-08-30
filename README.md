# GoGIF

**One prompt. The right GIF—made or found.**

GoGIF is a Go-powered GIF creation and discovery app designed to feel equally at home in a browser, an installed phone/desktop PWA, and a browser extension. The current repository is a working vertical slice: enter an idea, receive a real animated GIF, download it, or search Wikimedia Commons, GifCities, the Prelinger Archives, and NASA's media library with no account or API key.

## What works now

- Pure-Go animated GIF renderer with orbit, pulse, wave, and confetti motion systems
- Free local Blender still generation, animated and encoded into GIFs by Go
- Native local ComfyUI adapter for text-to-image and one licensed reference image
- Capability-gated cinematic pipeline for AI/reference imagery → Blender FBX assets → Unity 6.3 motion/VFX → Unreal Engine 5 beauty frames → FFmpeg GIF encoding
- Natural-language art planning with a deterministic offline planner
- Optional, disabled-by-default OpenAI art direction through the Responses API and strict structured output
- Automatic local fallback if the AI provider is unavailable
- Responsive, installable PWA embedded in the Go binary
- Free Wikimedia Commons search through a normalized, rights-aware provider adapter
- Free GifCities search across Internet Archive's archived GeoCities GIF index
- Free Prelinger archival-film search with item-specific license normalization and on-demand, provider-hosted video previews
- Local WebVTT/SRT quote matching that jumps a selected Prelinger preview to the matching timecode
- Free NASA image/video search with provider-hosted playback and conservative media-usage restrictions
- Private photo, existing-GIF, and optional FFmpeg-backed short-video editor with trim, direct crop/caption manipulation, zoom, timing, and loop controls
- Undo/redo plus explicit IndexedDB drafts that keep source media and settings in the current browser
- Messages/Discord/Slack export presets, bounded size optimization, animation-quality controls, clipboard/file-or-link copy fallbacks, download, and native file sharing
- Allowlisted, size-bounded temporary Wikimedia reference fetching with deletion after each job
- Optional direct-to-GIPHY GIF and sticker search with required attribution and continuous pagination
- MemKV-backed asset catalog with an ephemeral zero-config fallback
- Content-addressed local blob storage for generated media
- Browser-extension development shell
- Bounds checking, request limits, graceful shutdown, tests, and CI

## Run it

Requirements: Go 1.26.5 or newer. FFmpeg is optional; when its executable is available on `PATH`, GoGIF automatically enables request-scoped MP4, MOV, M4V, and WebM trimming.

```sh
make run
```

Open <http://localhost:8080>. No account or API key is required for local GIF creation.

This is the zero-spend mode: local Go rendering, public-catalog discovery, in-memory metadata, and local filesystem output. It does not provision cloud storage or call a paid AI API. See [Zero-cost architecture](docs/ZERO_COST_ARCHITECTURE.md).

GoGIF is a connector, not a GIF warehouse: existing catalog media stays on its source host. Only original GoGIF outputs (and explicit user uploads) enter managed local storage. A licensed reference used for generation is temporary and is deleted after the new output is created.

For richer procedural art using the Blender already installed on this computer:

```sh
GOGIF_IMAGE_GENERATOR=blender make run
```

Blender is not a diffusion model: it creates an original prompt-seeded 3D scene without a model download, account, or network call. It cannot understand a character or location from text by itself. For semantic text-to-image generation and licensed reference remixes, use the ComfyUI setup in [Local generation](docs/LOCAL_GENERATION.md).

The multi-engine quality pipeline is implemented behind an explicit opt-in. The current test Mac has Blender 5.2, Unity 6000.3.23f1, Unreal Engine 5.8.2, FFmpeg 9, Xcode 26.1.1, and Apple's Metal toolchain installed. A real request has passed through every stage and returned `blender+unity-6.3+unreal-5+ffmpeg+local`. GoGIF still keeps the lightweight renderer as the default because this 8-GB Mac is below Unreal's stated minimum memory and a local semantic image model is not configured. See [Cinematic pipeline](docs/CINEMATIC_PIPELINE.md).

### Keys and accounts

| Capability | Key needed? | Cost behavior |
| --- | --- | --- |
| Go renderer, Blender, local ComfyUI, Unity/Unreal batch workers, MemKV | No API key | Runs on hardware you own; editor licenses and terms still apply |
| Local short-video trim with FFmpeg | No | Optional executable on the GoGIF server; source and decoded frames are request-scoped |
| Wikimedia Commons, GifCities, Prelinger, and NASA search | No | Source media remains provider-hosted; normal bandwidth only |
| Public ungated model checkpoint | Usually no | License and hardware requirements are model-specific |
| GIPHY search | Yes, `GIPHY_API_KEY` | Optional provider integration |
| OpenAI art-direction planner | Yes, `OPENAI_API_KEY` plus `GOGIF_ENABLE_PAID_AI=true` | Paid opt-in; never part of zero-spend mode |
| Comfy Cloud | Not used | GoGIF talks only to the local native ComfyUI API |

### Optional external services

The following integrations are off by default. Enabling a hosted AI account can incur charges, so they are not part of the zero-spend path. To explicitly turn on OpenAI-directed planning:

```sh
export OPENAI_API_KEY="your-project-key"
export OPENAI_MODEL="gpt-5-mini"
export GOGIF_ENABLE_PAID_AI="true"
make run
```

GoGIF ignores `OPENAI_API_KEY` unless `GOGIF_ENABLE_PAID_AI=true` is also set.

To add GIPHY results, create a platform-specific GIPHY API key and run:

```sh
export GIPHY_API_KEY="your-giphy-key"
make run
```

GIPHY requires search requests to originate from the client, so the web app receives this platform key at runtime. Never use a private server credential in that setting. See the [GIPHY API requirements](https://developers.giphy.com/docs/api/).

To persist generated asset records through your [`brandopakel/memkv`](https://github.com/brandopakel/memkv) `develop` branch, start MemKV with AOF enabled and eviction effectively disabled for the canonical catalog, then run:

```sh
export GOGIF_MEMKV_ADDR="127.0.0.1:8081"
export GOGIF_BLOB_DIR=".data/blobs"
make run
```

The GIF bytes go to content-addressed blob storage; MemKV holds the searchable record, provenance, rights, state, and later its set/sorted-set indexes. See [ADR 0001](docs/adr/0001-media-storage-and-memkv.md) for the exact split.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | Readiness and engine status |
| `GET` | `/api/v1/config` | Public client capabilities |
| `GET` | `/api/v1/providers/wikimedia/search?q=...` | Search Wikimedia Commons with normalized rights metadata |
| `GET` | `/api/v1/providers/gifcities/search?q=...` | Search GifCities and return source-linked archived GIFs |
| `GET` | `/api/v1/providers/prelinger/search?q=...` | Search Prelinger archival films without downloading video |
| `GET` | `/api/v1/providers/nasa/search?q=...` | Search NASA's image/video library without downloading media |
| `GET` | `/api/v1/providers/{provider}/items/{id}` | Revalidate an item and resolve its current renditions/captions |
| `GET` | `/api/v1/providers/{provider}/items/{id}/quote?q=...` | Match a quote against selected-item captions and return its time range |
| `GET` | `/api/v1/gifs/{id}` | Serve an original GoGIF asset; zero-config links last for the server session and MemKV keeps records across restarts |
| `POST` | `/api/v1/gifs/plan` | Inspect the prompt-derived animation plan |
| `POST` | `/api/v1/gifs/generate` | Stream an `image/gif` response |
| `POST` | `/api/v1/gifs/generate-from-reference` | Revalidate, temporarily fetch, locally transform, then delete an approved provider reference |
| `POST` | `/api/v1/gifs/generate-from-upload` | Edit a bounded request-scoped JPEG, PNG, GIF, MP4, MOV, M4V, or WebM; optionally optimize to a target size |

Example:

```sh
curl -sS http://localhost:8080/api/v1/gifs/generate \
  -H 'content-type: application/json' \
  -d '{"prompt":"we actually shipped it","width":480,"height":480}' \
  -o shipped.gif
```

## Architecture

The first release is intentionally a modular monolith:

```text
phone / desktop PWA ─┐
browser extension ───┼──> Go HTTP API ──┬─> local planner
web browser ─────────┘                   ├─> Go / Blender / ComfyUI ──> animated GIF
                                        ├─> Blender → Unity 6.3 → Unreal 5 → FFmpeg
                                        └─> provider adapters ──> Wikimedia / GifCities / Prelinger / NASA
```

The planner speaks a small, validated animation-spec contract. That keeps model vendors, renderers, and future native clients replaceable. The OpenAI adapter uses the Responses API with Structured Outputs, following the [official OpenAI API reference](https://developers.openai.com/api/reference/cli/resources/responses/methods/create).

Read [Architecture](docs/ARCHITECTURE.md) for boundaries and scaling decisions, [Media sources](docs/MEDIA_SOURCES.md) for the provider/rights matrix, [ADR 0001](docs/adr/0001-media-storage-and-memkv.md) for storage, and [Roadmap](docs/ROADMAP.md) for the staged product plan.

## Extension development

1. Start the API with `make run`.
2. Open the browser's extension page and enable developer mode.
3. Load the unpacked directory at `apps/extension`.
4. Use the toolbar popup to generate against `http://localhost:8080`.

The production extension will receive an environment-specific API URL during packaging; the checked-in shell deliberately targets local development.

Run `make builds` to create standalone server binaries for macOS, Windows, and Linux plus local Chrome, Edge, and Firefox extension ZIPs in `dist/`. See [Testing builds](docs/TEST_BUILDS.md) for iPhone, desktop, and extension instructions.

## Quality checks

```sh
make check
```

This formats Go code, runs `go vet`, then runs the full test suite with the race detector.

## Important product boundary

No service can truthfully or lawfully search “every GIF on the internet.” GoGIF will provide federated search across licensed providers, a user's private library, and the app's own generated catalog. Tenor is not a viable new integration because Google stopped accepting new API clients in January 2026; the provider boundary remains open so additional licensed sources can be evaluated without rewriting the product.

## Project status

Early MVP. The repository intentionally has no software license yet; choose the commercial/open-source licensing strategy before making it public.

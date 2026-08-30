# GoGIF

**One prompt. The right GIF—made or found.**

GoGIF is a Go-powered GIF creation and discovery app designed to feel equally at home in a browser, an installed phone/desktop PWA, and a browser extension. The current repository is a working vertical slice: enter an idea, receive a real animated GIF, download it, or search Wikimedia Commons with no account or API key.

## What works now

- Pure-Go animated GIF renderer with orbit, pulse, wave, and confetti motion systems
- Free local Blender still generation, animated and encoded into GIFs by Go
- Native local ComfyUI adapter for text-to-image and one licensed reference image
- Natural-language art planning with a deterministic offline planner
- Optional, disabled-by-default OpenAI art direction through the Responses API and strict structured output
- Automatic local fallback if the AI provider is unavailable
- Responsive, installable PWA embedded in the Go binary
- Free Wikimedia Commons search through a normalized, rights-aware provider adapter
- Allowlisted, size-bounded temporary Wikimedia reference fetching with deletion after each job
- Optional direct-to-GIPHY search integration with required attribution
- MemKV-backed asset catalog with an ephemeral zero-config fallback
- Content-addressed local blob storage for generated media
- Browser-extension development shell
- Bounds checking, request limits, graceful shutdown, tests, and CI

## Run it

Requirements: Go 1.26.5 or newer.

```sh
make run
```

Open <http://localhost:8080>. No account or API key is required for local GIF creation.

This is the zero-spend mode: local Go rendering, Wikimedia discovery, in-memory metadata, and local filesystem output. It does not provision cloud storage or call a paid AI API. See [Zero-cost architecture](docs/ZERO_COST_ARCHITECTURE.md).

GoGIF is a connector, not a GIF warehouse: existing catalog media stays on its source host. Only original GoGIF outputs (and explicit user uploads) enter managed local storage. A licensed reference used for generation is temporary and is deleted after the new output is created.

For richer procedural art using the Blender already installed on this computer:

```sh
GOGIF_IMAGE_GENERATOR=blender make run
```

Blender is not a diffusion model: it creates an original prompt-seeded 3D scene without a model download, account, or network call. For local diffusion and licensed reference remixes, use the ComfyUI setup in [Local generation](docs/LOCAL_GENERATION.md).

### Keys and accounts

| Capability | Key needed? | Cost behavior |
| --- | --- | --- |
| Go renderer, Blender, local ComfyUI, MemKV | No | Runs on hardware you own |
| Wikimedia Commons search/reference | No | Source media remains provider-hosted; normal bandwidth only |
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
| `GET` | `/api/v1/gifs/{id}` | Serve an original GoGIF asset when persistent local storage is enabled |
| `POST` | `/api/v1/gifs/plan` | Inspect the prompt-derived animation plan |
| `POST` | `/api/v1/gifs/generate` | Stream an `image/gif` response |
| `POST` | `/api/v1/gifs/generate-from-reference` | Revalidate, temporarily fetch, locally transform, then delete an approved provider reference |

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
                                        └─> provider adapters ──> Wikimedia Commons
```

The planner speaks a small, validated animation-spec contract. That keeps model vendors, renderers, and future native clients replaceable. The OpenAI adapter uses the Responses API with Structured Outputs, following the [official OpenAI API reference](https://developers.openai.com/api/reference/cli/resources/responses/methods/create).

Read [Architecture](docs/ARCHITECTURE.md) for boundaries and scaling decisions, [Media sources](docs/MEDIA_SOURCES.md) for the provider/rights matrix, [ADR 0001](docs/adr/0001-media-storage-and-memkv.md) for storage, and [Roadmap](docs/ROADMAP.md) for the staged product plan.

## Extension development

1. Start the API with `make run`.
2. Open the browser's extension page and enable developer mode.
3. Load the unpacked directory at `apps/extension`.
4. Use the toolbar popup to generate against `http://localhost:8080`.

The production extension will receive an environment-specific API URL during packaging; the checked-in shell deliberately targets local development.

## Quality checks

```sh
make check
```

This formats Go code, runs `go vet`, then runs the full test suite with the race detector.

## Important product boundary

No service can truthfully or lawfully search “every GIF on the internet.” GoGIF will provide federated search across licensed providers, a user's private library, and the app's own generated catalog. Tenor is not a viable new integration because Google stopped accepting new API clients in January 2026; the provider boundary remains open so additional licensed sources can be evaluated without rewriting the product.

## Project status

Early MVP. The repository intentionally has no software license yet; choose the commercial/open-source licensing strategy before making it public.

# Architecture

## Product shape

GoGIF has two jobs that should feel like one action:

1. **Create** — translate a human idea into an animation plan, then render it.
2. **Find** — federate a typed query across approved catalogs and personal media.

The input and result surfaces are shared. GIF and 3D outputs remain distinct media contracts: an animated image is not used as a container for a model, and a GLB retains its geometry, materials, and animation data. The systems behind creation and discovery are deliberately separate because generation is compute-bound and owned by GoGIF, while catalog search is governed by provider terms, attribution, analytics, and rate limits.

## Current boundaries

| Boundary | Responsibility | Current implementation |
| --- | --- | --- |
| `internal/planner` | Prompt → validated animation spec | Offline deterministic planner; optional OpenAI adapter |
| `internal/imagegen` | Prompt/reference images → generated still image | Local Blender procedural adapter and native ComfyUI adapter |
| `internal/modelgen` | Prompt → portable 3D asset | Allowlisted ComfyUI Tripo 3.1 and Hunyuan 3D 3.1 workflows returning validated GLB bytes |
| `internal/cinematic` | Multi-engine job manifest, stage isolation, pass compositing, and final sequence validation | Blender FBX stage, Unity 6.3 batch stage, Unreal Engine 5 batch stage, and FFmpeg adaptive-palette encoder |
| `internal/scene` | Persistent Scene projects and pull-worker coordination | engine-target allowlist, leases, retries, progress, cancellation, and artifact metadata |
| `internal/reference` | Approved provider item → bounded temporary input | Revalidation, exact-host allowlist, MIME/size checks, SHA-256, deletion |
| `internal/gif` | Stable domain contract and safety bounds | Dimensions, timing, palette, motion, caption |
| `internal/render` | Animation spec → encoded asset | Pure-Go indexed-color GIF renderer |
| `internal/video` | Bounded short clip → request-scoped source frames | Optional local FFmpeg adapter with isolated temporary files and cleanup |
| `internal/media` | Asset, rendition, provenance, and rights catalog | Validated JSON records persisted through the KV boundary |
| `internal/store` | Metadata and binary persistence seams | MemKV RESP adapter, memory KV, content-addressed filesystem blobs |
| `internal/provider` | Federated discovery and rights normalization | Clip-capable cached provider contract plus Wikimedia Commons, GifCities, Prelinger, NASA, and bounded Yarn metadata adapters |
| `internal/subtitle` | Provider captions → local quote time range | Bounded WebVTT/SRT parser with exact and ASR-tolerant matching |
| `internal/httpapi` | Public client contract | Standard-library HTTP server and embedded PWA |
| `webapp` | Universal interaction surface | Responsive PWA with sectioned provider search |
| `apps/extension` | Browser toolbar surface | Local-development MV3 client |

No package outside `internal/planner` knows which AI provider made a plan. No planner knows how pixels are rendered. This is the most important seam in the system.

## Why a modular monolith first

A single Go binary makes local development, self-hosting, and early deployment simple. It also gives us one observable request path while the product behavior is still changing. Splitting queues, workers, storage, and search services before workload data exists would make iteration slower.

The extraction points are already explicit:

- Move `internal/render` behind a job queue when generation latency exceeds an interactive request window.
- Move generated asset bytes to object storage when user libraries land.
- Add a search aggregator service only for providers whose terms permit server-side requests.
- Preserve the animation spec as the job payload and compatibility contract.

## AI planning

The model is an art director, not an unchecked code generator. It chooses only a short caption, five validated colors, and one renderer-supported motion. Strict schema output plus domain validation prevents unexpected fields and bounds compute.

Unless both `GOGIF_ENABLE_PAID_AI=true` and `OPENAI_API_KEY` are present, the local planner hashes the prompt into a deterministic plan. This explicit opt-in prevents an unrelated shell key from creating project charges. When the remote planner fails, the same local planner becomes an availability fallback. A response header identifies which engine produced each GIF.

Never send the OpenAI key to a client. In a multi-user deployment, add authentication, per-user quotas, abuse controls, and stable privacy-safe safety identifiers before opening the generation endpoint publicly.

## Search

Search is a federation problem. The Go API exposes Wikimedia Commons, GifCities, Prelinger, and NASA through normalized adapters, caches repeat queries for fifteen minutes, and links to provider-hosted media rather than mirroring it. Wikimedia uses Commons' media-specific search profile with image/drawing/video filtering, spelling rewrites, captions, and structured media data instead of generic wiki full-text ranking; short ambiguous terms can still have several valid meanings. Yarn has a deliberately narrow HTML metadata adapter: it makes one bounded phrase-search request, parses only result-card metadata and conservative pagination, validates canonical clip IDs, and constructs provider-hosted page/embed URLs. It never derives media CDN filenames or fetches movie bytes, and it reports the provider unavailable when a browser challenge appears. Because Yarn has no supported public API and its current live host challenges this backend, that connector is experimental rather than a dependable catalog. GifCities and NASA results intentionally retain unknown commercial/derivative permissions because their search responses do not provide item-level rights clearance. Prelinger search returns metadata and posters only; selecting a preview revalidates the item and resolves stable Internet Archive video and caption renditions on demand. Each provider adapter must own:

- terms and attribution compliance;
- platform-specific credentials;
- result normalization and canonical IDs;
- safety-rating mapping;
- share/view analytics required by the provider;
- pagination, caching, and rate-limit behavior.

The normalized contract can represent images, GIFs, clips, and videos. Clip results may include multiple renditions, duration, audio availability, caption tracks, quote-match time ranges, and allowed handling modes. These fields describe provider media; they do not grant additional usage rights.

Quote matching is selected-item work, not a bulk index crawl. For Prelinger, GoGIF resolves the current metadata, downloads one caption file under a strict 8 MB limit, parses its [WebVTT](https://www.w3.org/TR/webvtt1/) or SRT cues locally, and returns the closest time range. The browser then seeks the provider-hosted video to that range. Items without usable captions remain playable but do not receive a fabricated match.

GIPHY currently requires calls to be made from the client. When explicitly configured, the PWA receives a GIPHY platform key through public runtime configuration and shows those results in a separate, attributed section. Private-library search will use the Go API.

## Zero-spend operation

The default topology runs on the user's computer: one Go process, an in-memory catalog, local file bytes, public catalog APIs, and no hosted AI calls. MemKV can be enabled locally for persistent catalog records without modifying the MemKV repository. Blender and ComfyUI implement `internal/imagegen`; they receive validated bytes from the controlled reference fetcher, never an arbitrary URL.

Cloud object storage, managed databases, remote GPU workers, and hosted model APIs are optional deployment choices, not prerequisites. A public multi-user service cannot promise zero ongoing cost because its compute, bandwidth, and storage must run somewhere.

Paid Comfy Partner Nodes are a separate, explicit exception to zero-spend operation. `internal/modelgen` is not constructed unless a server-side Comfy account key, a selected generator, and `GOGIF_ENABLE_PAID_MODEL_GENERATION=true` are all present. Its recipe registry owns every node class, input, output, and size bound; the public API accepts only a recipe ID and validated prompt, never arbitrary workflow JSON.

## Rendering evolution

The pure-Go renderer is universally buildable and owns final cropping, captions, timing, palette conversion, and target-size retries. Photo and GIF inputs decode in-process under explicit pixel/frame limits. Short video remains optional: `internal/video/ffmpeg` writes one request to a private temporary directory, invokes a local executable without a shell, bounds the clip to fifteen seconds and forty-eight frames, decodes the result, and removes the directory. Future renderers can add better typography, stickers, subject tracking, animated WebP, and MP4 behind the same contracts.

The normal GIF contract is now deliberately small: ComfyUI or another configured semantic generator creates recognizable reference imagery, Go applies bounded 2.5D motion, and the encoder returns a GIF. Blender, Unity, and Unreal are not selectable quality levels in that flow.

The existing cinematic proof-of-concept is retained as developer infrastructure while it is refactored into a Scene job contract. In that target contract, Blender prepares portable assets and a scene selects either Unity 6.3 for real-time/interactivity/VFX or Unreal Engine 5 for cinematic rendering. FFmpeg creates MP4/WebM masters and optional GIF derivatives. Running both real-time engines consecutively is no longer the product architecture.

`internal/scene` implements the control plane and verified private artifact registry. `internal/sceneworker` implements the outbound-only worker loop and initial Unreal renderer. An authenticated request creates an owner-scoped project and queued job; a compatible remote worker claims a short lease, renews progress through heartbeats, renders in an isolated workspace, streams artifacts, and reports retryable or terminal completion. The API physically reopens stored artifacts before success. It remains a single writer while it uses the small KV interface. A production multi-replica deployment must replace that queue index with transactional claims or a managed at-least-once queue and preserve idempotent completion.

Every experimental engine job uses a private temporary workspace and a Go-authored versioned manifest. HTTP clients cannot choose filesystem paths or command arguments. Each stage has a deadline and bounded diagnostics, and downstream work begins only after the expected contract passes size, format, dimension, and count validation. The legacy proof-of-concept is disabled unless `GOGIF_ENABLE_QUALITY_PIPELINE=true` and is reachable only through the developer-only `generation_mode=studio` API. The PWA always sends `generation_mode=semantic`; neither 3D editor provides prompt-level synthesis by itself.

## Deployment path

1. Local/self-hosted Go binary with local files and optional local MemKV.
2. Optional local Blender or ComfyUI process behind the `internal/imagegen` contract.
3. Only if a funded public service is later approved: TLS hosting, managed object storage, backups, and quotas.
4. Only after measured demand: horizontally scaled API and CPU/GPU workers.

The browser is not a trusted boundary. Before public launch, add authentication, CSRF strategy if cookies are used, quotas, moderation, asset expiry, privacy controls, and structured observability.

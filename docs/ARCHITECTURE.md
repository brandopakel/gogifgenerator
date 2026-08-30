# Architecture

## Product shape

GoGIF has two jobs that should feel like one action:

1. **Create** — translate a human idea into an animation plan, then render it.
2. **Find** — federate a typed query across approved catalogs and personal media.

The input and result surfaces are shared. The systems behind them are deliberately separate because generation is compute-bound and owned by GoGIF, while catalog search is governed by provider terms, attribution, analytics, and rate limits.

## Current boundaries

| Boundary | Responsibility | Current implementation |
| --- | --- | --- |
| `internal/planner` | Prompt → validated animation spec | Offline deterministic planner; optional OpenAI adapter |
| `internal/imagegen` | Prompt/reference images → generated still image | Local Blender procedural adapter and native ComfyUI adapter |
| `internal/reference` | Approved provider item → bounded temporary input | Revalidation, exact-host allowlist, MIME/size checks, SHA-256, deletion |
| `internal/gif` | Stable domain contract and safety bounds | Dimensions, timing, palette, motion, caption |
| `internal/render` | Animation spec → encoded asset | Pure-Go indexed-color GIF renderer |
| `internal/media` | Asset, rendition, provenance, and rights catalog | Validated JSON records persisted through the KV boundary |
| `internal/store` | Metadata and binary persistence seams | MemKV RESP adapter, memory KV, content-addressed filesystem blobs |
| `internal/provider` | Federated discovery and rights normalization | Clip-capable cached provider contract plus Wikimedia Commons and GifCities adapters |
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

Search is a federation problem, not a web-crawling problem. The Go API currently exposes Wikimedia Commons and GifCities through normalized adapters, caches repeat queries for fifteen minutes, and links to provider-hosted media rather than mirroring it. GifCities results intentionally retain unknown commercial/derivative permissions because its search response does not provide per-file license metadata. Each provider adapter must own:

- terms and attribution compliance;
- platform-specific credentials;
- result normalization and canonical IDs;
- safety-rating mapping;
- share/view analytics required by the provider;
- pagination, caching, and rate-limit behavior.

The normalized contract can represent images, GIFs, clips, and videos. Clip results may include multiple renditions, duration, audio availability, caption tracks, quote-match time ranges, and allowed handling modes. These fields describe provider media; they do not grant additional usage rights.

GIPHY currently requires calls to be made from the client. When explicitly configured, the PWA receives a GIPHY platform key through public runtime configuration and shows those results in a separate, attributed section. Private-library search will use the Go API.

## Zero-spend operation

The default topology runs on the user's computer: one Go process, an in-memory catalog, local file bytes, Wikimedia's public API, and no hosted AI calls. MemKV can be enabled locally for persistent catalog records without modifying the MemKV repository. Blender and ComfyUI implement `internal/imagegen`; they receive validated bytes from the controlled reference fetcher, never an arbitrary URL.

Cloud object storage, managed databases, remote GPU workers, and hosted model APIs are optional deployment choices, not prerequisites. A public multi-user service cannot promise zero ongoing cost because its compute, bandwidth, and storage must run somewhere.

## Rendering evolution

The pure-Go renderer is fast to ship and universally buildable. It intentionally uses a small indexed palette and bitmap type. The next renderer can add image/video inputs, better typography, stickers, transforms, subtitles, cropping, optimization, animated WebP, and MP4. FFmpeg or a purpose-built worker is appropriate once those features are validated; it should remain behind the same spec/job interface.

## Deployment path

1. Local/self-hosted Go binary with local files and optional local MemKV.
2. Optional local Blender or ComfyUI process behind the `internal/imagegen` contract.
3. Only if a funded public service is later approved: TLS hosting, managed object storage, backups, and quotas.
4. Only after measured demand: horizontally scaled API and CPU/GPU workers.

The browser is not a trusted boundary. Before public launch, add authentication, CSRF strategy if cookies are used, quotas, moderation, asset expiry, privacy controls, and structured observability.

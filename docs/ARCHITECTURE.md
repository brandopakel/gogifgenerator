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
| `internal/gif` | Stable domain contract and safety bounds | Dimensions, timing, palette, motion, caption |
| `internal/render` | Animation spec → encoded asset | Pure-Go indexed-color GIF renderer |
| `internal/httpapi` | Public client contract | Standard-library HTTP server and embedded PWA |
| `webapp` | Universal interaction surface | Responsive PWA and direct licensed search |
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

When `OPENAI_API_KEY` is absent, the local planner hashes the prompt into a deterministic plan. When the remote planner fails, the same local planner becomes an availability fallback. A response header identifies which engine produced each GIF.

Never send the OpenAI key to a client. In a multi-user deployment, add authentication, per-user quotas, abuse controls, and stable privacy-safe safety identifiers before opening the generation endpoint publicly.

## Search

Search is a federation problem, not a web-crawling problem. Each provider adapter must own:

- terms and attribution compliance;
- platform-specific credentials;
- result normalization and canonical IDs;
- safety-rating mapping;
- share/view analytics required by the provider;
- pagination, caching, and rate-limit behavior.

GIPHY currently requires calls to be made from the client. The PWA therefore receives a GIPHY platform key through public runtime configuration and calls GIPHY directly. Private-library search will use the Go API.

## Rendering evolution

The pure-Go renderer is fast to ship and universally buildable. It intentionally uses a small indexed palette and bitmap type. The next renderer can add image/video inputs, better typography, stickers, transforms, subtitles, cropping, optimization, animated WebP, and MP4. FFmpeg or a purpose-built worker is appropriate once those features are validated; it should remain behind the same spec/job interface.

## Deployment path

1. One Go container behind TLS, with timeouts and an API key stored server-side.
2. CDN for the PWA shell; managed object storage for generated assets.
3. Postgres for accounts/projects and Redis or a managed queue for render jobs.
4. Horizontally scaled API and CPU/GPU worker pools with signed upload/download URLs.
5. Regional search/API edges only after provider policies and real latency data justify them.

The browser is not a trusted boundary. Before public launch, add authentication, CSRF strategy if cookies are used, quotas, moderation, asset expiry, privacy controls, and structured observability.

# GoGIF

**One prompt. The right GIF—made or found.**

GoGIF is a Go-powered GIF creation and discovery app designed to feel equally at home in a browser, an installed phone/desktop PWA, and a browser extension. The current repository is a working vertical slice: enter an idea, receive a real animated GIF, download it, or search Wikimedia Commons, GifCities, the Prelinger Archives, NASA's media library, and an experimental Yarn movie/TV metadata connector without pasting clip URLs.

## What works now

- Pure-Go animated GIF renderer retained for editing, tests, and development utilities
- Experimental Blender asset/still generation retained outside the normal subject-aware GIF flow
- Native local ComfyUI adapter for text-to-image and one licensed reference image
- Hosted-GPU ComfyUI FLUX 1.1 Pro Ultra adapter with an allowlisted server-owned workflow and exact output normalization
- First-class prompt-to-GLB creation through allowlisted ComfyUI Tripo 3.1 and Hunyuan 3D 3.1 workflows, with interactive preview, save, share, and clipboard/link fallbacks
- Server-side GPT Image 2 adapter for high-fidelity prompt-to-image and reference editing, separately paid and explicitly opt-in
- Opt-in experimental scene pipeline with Blender asset preparation, Unity 6.3 real-time motion/VFX, Unreal Engine 5 cinematic rendering, and FFmpeg encoding
- UI-hidden asynchronous Scene project/job foundation with owner isolation, target-aware worker leases, progress, retries, cooperative cancellation, and artifact contracts
- Cross-compiled outbound-only Windows Scene worker for Comfy reference acquisition → Blender FBX → Unreal frames → FFmpeg MP4/WebM, with heartbeat cancellation and verified private artifact upload
- Natural-language art planning with a deterministic offline planner
- Optional, disabled-by-default OpenAI art direction through the Responses API and strict structured output
- Automatic local fallback if the AI provider is unavailable
- Responsive, installable PWA embedded in the Go binary
- Free Wikimedia Commons search through its media-specific relevance profile, with visible match titles and normalized rights metadata
- Free GifCities search across Internet Archive's archived GeoCities GIF index
- Free Prelinger archival-film search with item-specific license normalization and on-demand, provider-hosted video previews
- Local WebVTT/SRT quote matching that jumps a selected Prelinger preview to the matching timecode
- Free NASA image/video search with provider-hosted playback and conservative media-usage restrictions
- First-class Clips search with quote-first cards, continuous provider pagination, and related-result navigation across reusable archives
- Experimental native-Go Yarn HTML metadata scraper driven by the same phrase input; results use provider-hosted posters and official click-to-load embeds, never inferred CDN downloads, proxying, transformation, or rehosting
- Private photo, existing-GIF, and optional FFmpeg-backed short-video editor with trim, direct crop/caption manipulation, zoom, timing, and loop controls
- Undo/redo plus explicit IndexedDB drafts that keep source media and settings in the current browser
- Messages/Discord/Slack export presets, bounded size optimization, animation-quality controls, native GIF copy where supported, PNG-frame/link fallbacks elsewhere, download, and native file sharing
- Allowlisted, size-bounded temporary Wikimedia reference fetching with deletion after each job
- Optional direct-to-GIPHY GIF and sticker search with required attribution and continuous pagination
- MemKV-backed asset catalog with an ephemeral zero-config fallback
- Content-addressed local blob storage for generated media
- Optional OIDC accounts with signed, HTTP-only sessions and verified-email identity linking
- Private per-user libraries with favorites, collections, pagination, soft deletion, byte/item limits, and expiring revocable share links
- Server-enforced Guest, Free, Creator, and Pro entitlements with atomic-in-process credit reservations around every creation
- Stripe-hosted subscription Checkout, Customer Portal, signed/idempotent webhooks, and automatic downgrade when a subscription no longer grants access
- Browser-extension development shell
- Bounds checking, request limits, graceful shutdown, tests, and CI

## Run it

Requirements: Go 1.26.5 or newer. FFmpeg is optional; when its executable is available on `PATH`, GoGIF automatically enables request-scoped MP4, MOV, M4V, and WebM trimming.

```sh
make run
```

Open <http://localhost:8080>. Search, editing, and the developer renderer work without an API key. The user-facing prompt-to-GIF flow is intentionally subject-aware and therefore requires a configured ComfyUI or OpenAI semantic image generator.

The bare process is the zero-spend development topology: public-catalog discovery, editing, the compatibility renderer/API, in-memory metadata, and local filesystem output. It does not provision cloud storage or call a paid AI API. Add local ComfyUI for a no-vendor-bill subject-aware GIF path. See [Zero-cost architecture](docs/ZERO_COST_ARCHITECTURE.md).

GoGIF is a connector, not a GIF warehouse: existing catalog media stays on its source host. Only original GoGIF outputs (and explicit user uploads) enter managed local storage. A licensed reference used for generation is temporary and is deleted after the new output is created.

For procedural development experiments using Blender:

```sh
GOGIF_IMAGE_GENERATOR=blender make run
```

Blender is not a diffusion model: it can build geometry, materials, rigs, and scene assets, but it cannot infer a named character or location from text by itself. GoGIF therefore no longer exposes Blender or procedural shapes as alternative GIF quality modes. Create → GIF always uses the configured semantic generator and fails closed if it is unavailable. The older `fast` and `studio` API modes remain for tests and explicit developer experiments; they are not choices in the product UI. See [Local generation](docs/LOCAL_GENERATION.md).

The previously validated multi-editor chain remains behind `GOGIF_ENABLE_QUALITY_PIPELINE` as an experimental developer capability. It is being reframed as a Scene workspace: ComfyUI supplies semantic concept/reference media, Blender prepares portable assets, and a project chooses **Unity or Unreal** as its render target. Unity and Unreal are no longer presented as mandatory consecutive “quality” filters for ordinary GIFs. FFmpeg should produce MP4/WebM masters and optional GIF derivatives. See [Scene pipeline](docs/CINEMATIC_PIPELINE.md).

### Where generation runs

The current private test deployment is self-hosted on the Mac and exposed to the owner's devices through Tailscale. Comfy Cloud hosts model inference, not the GoGIF web server and not the Blender/Unity/Unreal stages.

| Output | Work performed | Mac impact | Paid credits |
| --- | --- | --- | --- |
| **GIF** | Semantic keyframe on Comfy Cloud; bounded Go animation and GIF encoding on the Mac | Low to moderate | Yes, one hosted image workflow |
| **3D model** | Tripo/Hunyuan Partner Node on Comfy Cloud; GLB validation/storage on the Mac | Low during inference; large models still use browser and disk memory | Yes, one hosted 3D workflow |
| **Scene** (in development) | Comfy/reference art → Blender assets → Unity **or** Unreal render worker → MP4/WebM, with optional GIF export | Must run asynchronously on a suitably equipped worker | Provider plus worker compute |

Ordinary GIF and 3D requests do not launch Blender, Unity, or Unreal. If this Mac becomes hot or swaps heavily, an explicit developer scene-pipeline request is running; stop that experiment and move future scene rendering to an asynchronous GPU/CPU worker. Comfy Cloud hosts model inference only—it does not host the GoGIF web server or the installed editors.

## Accounts, libraries, and sustainable generation

The commercial account system is implemented but disabled by default. That preserves the current owner-testing deployment until an identity provider and Stripe account are deliberately connected. When accounts are enabled:

- Guests can search and edit small uploads; signing up is required before spending cloud generation credits or receiving a private library.
- Free accounts receive ten GoGIF credits per month, subject-aware GIF creation up to 480 px / 12 frames, and a private 25-item / 100 MiB library.
- Creator defaults to $15/month, 150 credits, 720 px / 18 frames, 3D creation, and 500 items / 5 GiB.
- Pro defaults to $39/month, 500 credits, 720 px / 24 frames, 3D plus future experimental Scene tools, and 2,500 items / 25 GiB.

GoGIF credits are an internal cost-control unit, not Comfy credits: Fast/edit costs 1, normal semantic generation costs 5, higher-quality semantic generation costs 8, Studio costs 30, and 3D costs 50. A reservation is written before work starts, consumed only after success, and released after failure. This prevents a failed Comfy job from charging the user while also preventing concurrent requests from overspending the same balance.

These prices and allowances are launch hypotheses, not a guarantee of margin. Before a public launch, measure Comfy Partner-node cost, GIF encoding time, storage, egress, payment fees, failed-job rate, and support per operation; then adjust `GOGIF_CREATOR_PRICE_CENTS`, `GOGIF_PRO_PRICE_CENTS`, or the server-side credit schedule. Never expose raw provider credits or a user-supplied price in the browser.

Authenticated creations are private and saved automatically. The Library supports GIF/3D filtering, favorites, collections, pagination, usage meters, soft deletion, and revocable seven-day share links. Direct `/api/v1/gifs/{id}` and `/api/v1/models/{id}` URLs enforce ownership; `/s/{token}` is the only public path for a private creation and the stored token is hashed.

### Enable accounts

GoGIF uses the standard [OIDC Authorization Code flow](https://openid.net/specs/openid-connect-core-1_0.html), so it can sit behind a provider such as Auth0, Clerk, Okta, Google, or another standards-compliant issuer. Register this callback with the provider: `https://YOUR_HOST/api/v1/auth/callback`. Then configure durable metadata and a 32+ character random session secret:

```sh
export GOGIF_PUBLIC_URL=https://YOUR_HOST
export GOGIF_AUTH_MODE=oidc
export GOGIF_SESSION_SECRET='replace-with-at-least-32-random-characters'
export GOGIF_OIDC_ISSUER=https://YOUR_ISSUER
export GOGIF_OIDC_CLIENT_ID=YOUR_CLIENT_ID
export GOGIF_OIDC_CLIENT_SECRET=YOUR_CLIENT_SECRET
export GOGIF_OIDC_REDIRECT_URL=https://YOUR_HOST/api/v1/auth/callback
export GOGIF_MEMKV_ADDR=127.0.0.1:8081
make run
```

OIDC startup fails closed without MemKV so accounts, identities, entitlements, library indexes, usage, and webhook idempotency do not silently disappear on restart. Run MemKV with AOF persistence and backups. The current repository/ledger implementation is for a single GoGIF API replica; use transactional database operations before horizontally scaling multiple writers.

For a one-owner local install, `GOGIF_AUTH_MODE=local`, `GOGIF_LOCAL_OWNER_EMAIL`, and the same session-secret/public-URL settings enable the Library without an external identity provider. Local mode is intentionally unmetered and cannot enable Stripe billing.

### Enable Stripe subscriptions

Create recurring Creator and Pro Prices in Stripe, enable the [Stripe Customer Portal](https://docs.stripe.com/customer-management/integrate-customer-portal), and subscribe a webhook endpoint at `https://YOUR_HOST/api/v1/billing/webhook` to at least `checkout.session.completed`, `customer.subscription.created`, `customer.subscription.updated`, and `customer.subscription.deleted`. Then add:

```sh
export GOGIF_ENABLE_BILLING=true
export STRIPE_SECRET_KEY=sk_live_or_test_...
export STRIPE_WEBHOOK_SECRET=whsec_...
export STRIPE_CREATOR_PRICE_ID=price_...
export STRIPE_PRO_PRICE_ID=price_...
export GOGIF_CREATOR_PRICE_CENTS=1500
export GOGIF_PRO_PRICE_CENTS=3900
```

Checkout and subscription metadata carry the immutable GoGIF user/plan IDs; the webhook body is size-bounded, [signature-verified](https://docs.stripe.com/webhooks/signature), and idempotently recorded. `active`, `trialing`, and `past_due` currently retain paid access as a short billing grace policy; other states downgrade to Free. Price amounts are shown from GoGIF's server configuration, while Stripe remains the payment source of truth. Use [Stripe test mode and its webhook tooling](https://docs.stripe.com/webhooks) before live mode.

### Keys and accounts

| Capability | Key needed? | Cost behavior |
| --- | --- | --- |
| Go renderer, Blender, local ComfyUI, Unity/Unreal batch workers, MemKV | No API key | Runs on hardware you own; editor licenses and terms still apply |
| Local short-video trim with FFmpeg | No | Optional executable on the GoGIF server; source and decoded frames are request-scoped |
| Wikimedia Commons, GifCities, Prelinger, and NASA search | No | Source media remains provider-hosted; normal bandwidth only |
| Yarn movie/TV quote metadata | No | Experimental bounded HTML parser; currently unavailable whenever Yarn returns its Cloudflare challenge. Official embeds only; GoGIF does not fetch or store Yarn media. |
| Public ungated model checkpoint | Usually no | License and hardware requirements are model-specific |
| GIPHY search | Yes, `GIPHY_API_KEY` | Optional provider integration |
| OpenAI art-direction planner | Yes, `OPENAI_API_KEY` plus `GOGIF_ENABLE_PAID_AI=true` | Paid opt-in; never part of zero-spend mode |
| OpenAI semantic imagery | Yes, `OPENAI_API_KEY` plus `GOGIF_ENABLE_PAID_IMAGE_GENERATION=true` | Separate paid opt-in; key stays server-side |
| Comfy hosted semantic imagery | Yes, `COMFY_CLOUD_API_KEY` plus `GOGIF_ENABLE_PAID_IMAGE_GENERATION=true` | FLUX Partner credits; Comfy Cloud execution also requires a Creator/Pro subscription |
| ComfyUI Partner Node 3D creation | Yes, `COMFY_CLOUD_API_KEY` plus `GOGIF_ENABLE_PAID_MODEL_GENERATION=true` | Separate paid opt-in; Tripo/Hunyuan credits and a Comfy account are required |

### Optional external services

The following integrations are off by default. Enabling a hosted AI account can incur charges, so they are not part of the zero-spend path. To explicitly turn on OpenAI-directed planning:

```sh
export OPENAI_API_KEY="your-project-key"
export OPENAI_MODEL="gpt-5-mini"
export GOGIF_ENABLE_PAID_AI="true"
make run
```

GoGIF ignores `OPENAI_API_KEY` unless `GOGIF_ENABLE_PAID_AI=true` is also set.

To create subject-aware source imagery with OpenAI's current Image API while keeping the key on the server:

```sh
export OPENAI_API_KEY="your-project-key"
export GOGIF_ENABLE_PAID_IMAGE_GENERATION="true"
export GOGIF_OPENAI_IMAGE_MODEL="gpt-image-2"
export GOGIF_OPENAI_IMAGE_QUALITY="high"
make run
```

This is independent from AI planning: enabling image generation does not implicitly enable the paid planner. GIF creation requests a high-resolution semantic keyframe, normalizes it to the GIF canvas, and applies restrained lightweight motion in Go. The installed 3D editors are reserved for explicit developer scene work. See the [official OpenAI image generation guide](https://developers.openai.com/api/docs/guides/image-generation).

To run subject-aware imagery on hosted GPU infrastructure through ComfyUI instead of loading diffusion weights on the GoGIF Mac:

```sh
export COMFY_CLOUD_API_KEY="your-comfy-account-key"
export GOGIF_ENABLE_PAID_IMAGE_GENERATION=true
export GOGIF_IMAGE_GENERATOR=comfyui-cloud
export GOGIF_COMFYUI_IMAGE_URL=https://cloud.comfy.org/api
export GOGIF_COMFYUI_IMAGE_RECIPE=flux-ultra
make run
```

This queues only GoGIF's server-owned FLUX 1.1 Pro Ultra graph, polls the current Cloud Jobs API, retrieves the generated image without forwarding the key to signed storage, and center-crops it to the requested GIF dimensions. Cloud API execution requires an eligible Comfy subscription and the Partner Node consumes Comfy credits.

Animated GIF is not a generally supported Web Clipboard MIME type. When the browser rejects `image/gif`, GoGIF's **Copy frame** action writes a real PNG still to the clipboard; **Share** and **Download GIF** preserve animation. If even PNG clipboard writing is unavailable, Copy falls back to the managed GIF URL.

To enable **Create → 3D model** with curated prompt-to-GLB workflows, create a Comfy account API key, add credits, and explicitly authorize model-generation spend. The current cloud-first configuration is:

```sh
export COMFY_CLOUD_API_KEY="your-comfy-account-key"
export GOGIF_ENABLE_PAID_MODEL_GENERATION=true
export GOGIF_MODEL_GENERATOR=comfyui
export GOGIF_COMFYUI_MODEL_URL=https://cloud.comfy.org/api
export GOGIF_COMFYUI_MODEL_RECIPE=tripo-3.1
make run
```

Alternatively, point `GOGIF_COMFYUI_MODEL_URL` at `http://127.0.0.1:8188` to let a running ComfyUI Desktop instance orchestrate Partner Nodes. GoGIF accepts only its server-owned Tripo 3.1 and Hunyuan 3D 3.1 graphs; browser clients cannot submit arbitrary Comfy workflows. GLBs are validated, stored as their own media kind, shown in an interactive viewer, and exposed through Save `.glb`. Copy and native file sharing are attempted only when the browser reports support; otherwise GoGIF copies the managed model URL. A real Tripo 3.1 validation returned a 57.96 MB GLB that Blender imported as two meshes, three materials, and 1,016,446 vertices.

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
| `GET` | `/api/v1/account` | Current principal, plan, credit balance, library usage, and public plan catalog |
| `GET` | `/api/v1/auth/login` | Begin OIDC sign-in |
| `GET` | `/api/v1/auth/callback` | Verify the OIDC flow and establish a signed session |
| `POST` | `/api/v1/auth/logout` | Clear the signed session |
| `GET` | `/api/v1/library` | List the signed-in user's private creations with cursor pagination |
| `PATCH` / `DELETE` | `/api/v1/library/{id}` | Favorite/rename or soft-delete an owned creation |
| `POST` / `DELETE` | `/api/v1/library/{id}/share` | Create/revoke an expiring public share link |
| `GET` / `POST` | `/api/v1/collections` | List/create private collections |
| `PATCH` / `DELETE` | `/api/v1/collections/{id}` | Rename/delete a private collection |
| `PUT` / `DELETE` | `/api/v1/collections/{id}/assets/{asset}` | Add/remove an owned creation |
| `POST` | `/api/v1/billing/checkout` | Create a Stripe-hosted subscription Checkout session |
| `POST` | `/api/v1/billing/portal` | Create a Stripe-hosted Customer Portal session |
| `POST` | `/api/v1/billing/webhook` | Verify and apply Stripe subscription events |
| `GET` | `/s/{token}` | Serve an unexpired shared creation without exposing its blob key |
| `GET` | `/api/v1/providers/wikimedia/search?q=...` | Search Wikimedia Commons with normalized rights metadata |
| `GET` | `/api/v1/providers/gifcities/search?q=...` | Search GifCities and return source-linked archived GIFs |
| `GET` | `/api/v1/providers/prelinger/search?q=...` | Search Prelinger archival films without downloading video |
| `GET` | `/api/v1/providers/nasa/search?q=...` | Search NASA's image/video library without downloading media |
| `GET` | `/api/v1/providers/yarn/search?q=...&cursor=page%3D2` | Parse bounded public Yarn result metadata for a phrase and return official clip/embed URLs; fails closed on a browser challenge |
| `GET` | `/api/v1/providers/{provider}/items/{id}` | Revalidate an item and resolve its current renditions/captions |
| `GET` | `/api/v1/providers/{provider}/items/{id}/quote?q=...` | Match a quote against selected-item captions and return its time range |
| `GET` | `/api/v1/gifs/{id}` | Serve an original GoGIF asset; zero-config links last for the server session and MemKV keeps records across restarts |
| `POST` | `/api/v1/gifs/plan` | Inspect the prompt-derived animation plan |
| `POST` | `/api/v1/gifs/generate` | Stream an `image/gif`; the PWA always sends `generation_mode=semantic` (`fast` and `studio` are developer-only compatibility modes) |
| `POST` | `/api/v1/gifs/generate-from-reference` | Revalidate, temporarily fetch, locally transform, then delete an approved provider reference |
| `POST` | `/api/v1/gifs/generate-from-upload` | Edit a bounded request-scoped JPEG, PNG, GIF, MP4, MOV, M4V, or WebM; optionally optimize to a target size |
| `POST` | `/api/v1/models/generate` | Run an allowlisted ComfyUI 3D recipe and stream a validated `model/gltf-binary` GLB |
| `GET` | `/api/v1/models/{id}` | Serve an original managed GoGIF GLB for preview, save, or link-based sharing |
| `POST` / `GET` | `/api/v1/scenes` | Enqueue or list authenticated Scene projects when the experimental job service is enabled |
| `GET` | `/api/v1/scenes/{id}` | Read owner-scoped Scene state, progress, and artifact metadata |
| `POST` | `/api/v1/scenes/{id}/cancel` | Cancel queued work or request cooperative worker cancellation |
| `POST` | `/api/v1/scene-jobs/...` | Worker-only claim, heartbeat, and finish protocol protected by a server secret |
| `PUT` | `/api/v1/scene-jobs/{id}/artifacts/{kind}` | Lease-bound streaming upload into private content-addressed storage |

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
browser extension ───┼──> Go HTTP API ──┬─> Comfy Cloud FLUX ──> Go animation/GIF
web browser ─────────┘                   ├─> Comfy Cloud Tripo/Hunyuan ──> validated GLB
                                        ├─> future Scene job ──> Blender ──> Unity OR Unreal ──> video/GIF
                                        └─> provider adapters ──> Wikimedia / GifCities / Prelinger / NASA / Yarn metadata
```

The planner speaks a small, validated animation-spec contract. That keeps model vendors, renderers, and future native clients replaceable. The OpenAI adapter uses the Responses API with Structured Outputs, following the [official OpenAI API reference](https://developers.openai.com/api/reference/cli/resources/responses/methods/create).

Read [Architecture](docs/ARCHITECTURE.md) for boundaries and scaling decisions, [Scene hosting](docs/SCENE_HOSTING.md) for the worker/hosting plan, [Media sources](docs/MEDIA_SOURCES.md) for the provider/rights matrix, [ADR 0001](docs/adr/0001-media-storage-and-memkv.md) for storage, and [Roadmap](docs/ROADMAP.md) for the staged product plan.

Build the Phase 1 Windows/NVIDIA Scene worker from macOS/Linux with
`make worker-windows`, or build and run it directly on Windows with
`scripts\windows\run-scene-worker.ps1`. Copy `.env.worker.example` to the
ignored `.env.worker` first; never commit worker or Comfy credentials.
After the one-shot readiness check passes, install its current-user scheduled
task with `scripts\windows\install-scene-worker-task.ps1 -Start`. The task runs
at login and restarts after transient failures without opening an inbound port.

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

Yarn is intentionally different from the API-backed providers. The older [getgetyarnio Bash project](https://github.com/cbrochtrup/getgetyarnio) scrapes search HTML and derives CDN filenames, while the supplied [Python gist](https://gist.github.com/lambdan/5c6c49b88f9d5ed72ccc955e879bf402) scrapes site pages and downloads clips. GoGIF now implements only the bounded metadata-search portion independently in Go: it requests public phrase-result HTML, parses clip IDs, quotes, titles, posters, duration, and pagination, then constructs Yarn's documented-by-its-UI official embed URL. It does not derive `y.yarn.co` MP4 filenames, download movie bytes, forward cookies, solve challenges, proxy, transform, or rehost clips.

As reviewed on 2026-08-30, GetYarn exposes no supported public developer API and its live search endpoint returns a Cloudflare browser challenge to this backend. The adapter therefore fails closed with `provider: unavailable` when challenged; it does not silently turn a phrase into an outbound search-link card. The parser is fully covered by fixture tests and will work only while Yarn serves compatible public HTML to the server. Dependable production search, downloads, paid-product use, or clip-to-GIF transformation still requires a written Yarn API/content agreement or another licensed clip API. A scraper repository's software license does not grant rights to the underlying movie and television footage.

## Project status

Private beta foundation. Creation, federated search, account ownership, libraries, quotas, plan gates, and Stripe plumbing are implemented. The current Tailscale deployment remains appropriate for owner/device testing; do not call it a public production launch until OIDC/Stripe test-mode validation, durable blob hosting or a persistent single host, abuse/rate controls, content moderation, privacy/terms/refund policies, observability, backups, async cloud job recovery, and measured unit economics are in place. The repository intentionally has no software license yet; choose the commercial/open-source licensing strategy before making it public.

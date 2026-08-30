# Roadmap

## Milestone 0 — working nucleus (now)

- Prompt-to-GIF vertical slice
- Offline and AI-directed planning
- Responsive installable PWA
- Wikimedia Commons search through the Go provider API
- GifCities search through the Go provider API
- Prelinger film search and on-demand provider-hosted video previews
- Local selected-item WebVTT/SRT quote matching and timed preview
- NASA image/video search with conservative usage-policy metadata
- Optional GIPHY search path
- Provider-aware continuous search loading on desktop and mobile
- Dedicated GIPHY sticker search with transparent-media results kept separate from GIF and source-media feeds
- Extension development shell
- Tests and CI

Exit condition: a new contributor can clone, run, create, download, and test without external services.

## Milestone 1 — lovable editor

- Upload a photo, existing GIF, or bounded short video (implemented; video decoding is enabled when local FFmpeg is available)
- Caption editing, drag/keyboard positioning, crop focus, zoom, video trim, timing, and loop controls (implemented)
- Undo/redo and explicit browser-local IndexedDB drafts (implemented)
- Clipboard GIF/link fallback, session-hosted share links, download, native file share sheet, and Messages/Discord/Slack export presets (implemented)
- Iterative GIF optimization with bounded size and animation-quality targets (implemented)
- Keyboard operation, focus visibility, live status, semantic labels, touch targets, and reduced-motion behavior (reviewed and covered)

Implementation status: feature-complete and ready for device/design-partner testing. Exit condition remains: ten design partners can make and share a useful GIF in under thirty seconds without guidance.

## Milestone 2 — useful AI, measured

- Subject-aware prompt-to-reference generation through local ComfyUI or separately opt-in GPT Image 2 (implemented; production evaluation pending)
- Fail-closed semantic mode that never substitutes procedural shapes for an unavailable AI subject (implemented)
- Cinematic prompt compiler plus restrained 2.5D motion for semantic keyframes (implemented)
- First-class prompt-to-GLB creation with curated ComfyUI Tripo/Hunyuan workflows, managed storage, interactive preview, Save, and capability-tested Copy/Share (implemented; paid provider setup and production evaluation pending)
- Multimodal planning from uploaded media
- Intent-based caption and moment selection
- Background removal and subject tracking
- Style packs expressed as renderer capabilities, not prompt folklore
- Prompt/version telemetry with opt-in evaluation datasets
- Cost, latency, safety, and fallback dashboards

Exit condition: evaluated AI plans outperform curated deterministic templates on task completion while meeting a defined latency and cost budget.

## Milestone 3 — discovery and libraries

- Provider abstraction with normalized ranking, pagination, ratings, and attribution
- Generated/private GIF library with tags and semantic search
- Favorites, recents, collections, and cross-device sync
- Provider share/view analytics
- Duplicate detection and canonical asset identity
- Evaluate additional licensed catalogs; do not crawl arbitrary copyrighted media

Exit condition: one query returns fast, policy-compliant, deduplicated results from every enabled source.

## Milestone 4 — universal distribution

- Production Chrome/Edge/Firefox extension packages
- Share-target PWA and OS intents
- Desktop shell only where filesystem/hotkey integration adds value
- Mobile store shells only where share-sheet or camera integration exceeds PWA capability
- Signed releases, update channels, crash reporting, and store-review checklists

Exit condition: the same account, projects, and generation API work across supported surfaces with surface-specific UX tests.

## Milestone 5 — high-powered engine

- Asynchronous render jobs and progress events
- Image/multiview-to-3D, 3D rigging/retargeting, smart topology, and direct Blender/Unity/Unreal import after prompt-to-GLB evaluation
- Composable render-stage contract and local runners: diffusion/reference imagery, textured Blender FBX geometry, Unity 6.3 portable motion/VFX, Unreal Engine 5 cinematic beauty frames, semantic-preserving Go pass compositing, and FFmpeg adaptive-palette encoding (implemented and functionally validated on macOS; production-class hardware validation remains)
- Capability-based engine selection and reproducible intermediate assets; do not combine independently rendered frames without an explicit compositing contract
- Reproducible projects and local storage lifecycle policies
- Optional object storage/CDN only for a funded hosted service
- Isolated CPU/GPU worker pools and renderer capability negotiation
- Animated WebP/AVIF and MP4 alongside GIF
- Rate limits, budgets, abuse prevention, moderation, and enterprise controls
- Load tests and SLOs based on production traffic

Exit condition: the system meets explicit p95 latency, reliability, quality, and unit-economics targets under expected peak load.

## Decisions needed before public launch

- Product name and trademark search
- Repository visibility and software license
- Commercial model and provider budget
- Content policy, retention policy, privacy terms, and age/rating defaults
- First launch platforms and supported browsers
- Whether user generations are private by default (recommended: yes)

# Roadmap

## Milestone 0 — working nucleus (now)

- Prompt-to-GIF vertical slice
- Offline and AI-directed planning
- Responsive installable PWA
- GIPHY search path
- Extension development shell
- Tests and CI

Exit condition: a new contributor can clone, run, create, download, and test without external services.

## Milestone 1 — lovable editor

- Upload a photo, short video, or existing GIF
- Caption editing, drag positioning, crop, timing, trim, and loop controls
- Undo/redo and saved local drafts
- Share sheet, clipboard, and export presets for major messaging/social platforms
- GIF optimization with size/quality targets
- Accessibility and reduced-motion review

Exit condition: ten design partners can make and share a useful GIF in under thirty seconds without guidance.

## Milestone 2 — useful AI, measured

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
- Object storage, CDN delivery, lifecycle policies, and reproducible projects
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

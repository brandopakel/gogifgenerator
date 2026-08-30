# Zero-cost architecture

GoGIF's default mode must not create a vendor bill. It uses the user's computer for the API, renderer, metadata, and generated files, and it searches public catalogs without copying their full archives.

## What runs where

```text
PWA / extension
      │
      ▼
local GoGIF API ── search metadata ──> Wikimedia Commons
      │                                  │
      │                                  └─ provider-hosted preview/original
      ├─ local Go renderer ──> GIF
      ├─ future local image model ──> source frames
      ├─ optional local MemKV ──> metadata/cache/jobs
      └─ content-addressed local files ──> original GoGIF outputs
```

There is no R2, S3, hosted database, paid model, or CDN in this path. Local operation still consumes the user's disk, bandwidth, CPU, and—when a local image model is added—possibly GPU memory and electricity.

## Search without warehousing the internet

The provider adapter returns metadata, provider-hosted thumbnails, original/source links, dimensions, author, attribution, license fields, and a conservative transformation policy. Repeat searches are cached briefly in the configured KV to reduce upstream load.

GoGIF does not bulk-download Wikimedia, Internet Archive, Yarn, or movie/TV catalogs. Search availability is not permission to copy or transform a work. Results with unknown rights stay review-only; no-derivatives results stay reference-only.

## Fetch and transform flow

When importing is implemented, it should happen only after the user selects an item:

1. Recheck the source record and license.
2. Reject reference-only or no-derivatives items for AI transformation.
3. Fetch from an allowlisted provider host with strict byte, MIME, dimension, redirect, and timeout limits.
4. Verify the decoded file and calculate SHA-256.
5. Pass validated bytes—not a remote URL—to an `internal/imagegen.Generator`.
6. Delete provider source bytes when the job ends.
7. Animate/render and save only the newly created result to local content-addressed storage.
8. Preserve source, author, license, attribution, model, prompt, and derivative obligations in the generated asset record.

This boundary prevents a local or hosted generator adapter from becoming an arbitrary-URL fetcher.

## AI engines

Hosted OpenAI/Google-style adapters remain possible behind the generator interface, but they are disabled and excluded from zero-spend mode. The zero-cost target is a model server running on the same computer. Model code may be free while model licenses, hardware requirements, and commercial-use terms still need review.

The existing Go renderer is already offline and deterministic. It creates useful motion graphics now; a local image generator can later supply richer source frames without changing the provider, GIF encoder, or client contracts.

## MemKV boundary

GoGIF uses the user's `develop` branch through its RESP protocol; it does not import or modify MemKV internals. Local MemKV is appropriate for provider cache entries, jobs, and generated-asset metadata. Existing provider media never enters MemKV, and all large bytes stay out of it. The MemKV repository should only be changed if GoGIF exposes a reproducible correctness bug, with a focused failing test first.

## Honest limit

A local/self-hosted product can avoid a recurring vendor bill. A public service for many users cannot be guaranteed free forever: someone must fund its bandwidth, storage, abuse protection, and generation compute. GoGIF therefore keeps hosted infrastructure as an explicit future opt-in with a budget, never an automatic upgrade.

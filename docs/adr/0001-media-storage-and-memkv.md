# ADR 0001: Separate media bytes from the MemKV catalog

- Status: Accepted
- Date: 2026-08-29

## Context

GoGIF needs to find provider-hosted media, accept uploads, generate new assets, deduplicate renditions, track rights, serve large objects efficiently, and keep searches and render jobs fast. Those workloads have different size, durability, and policy requirements.

`brandopakel/memkv`'s work lives on its `develop` branch. That branch is substantially ahead of upstream and provides corrected RESP framing/pipelining, bounded connection buffers, active TTL expiry, memory-based limits, approximate LRU/LFU eviction, sets, sorted sets, probabilistic structures, optional I/O threads, and append-only persistence with incremental rewrite. GoGIF will use the server through RESP rather than import its `internal` Go packages.

## Decision

Use two coordinated storage planes.

### 1. Media plane

Store only bytes owned by the user or created by GoGIF:

- user uploads;
- GoGIF-generated originals and their renditions.

The zero-spend implementation is a content-addressed local filesystem. A future funded hosted deployment may opt into an S3-compatible object store, but GoGIF will not provision or require one by default:

```text
sha256/{first-two-hex}/{full-sha256}
```

Originals are immutable. Every crop, GIF, MP4, WebP, thumbnail, and optimized export is a separate rendition referencing its source and generation parameters. A future hosted mode could use signed URLs and lifecycle rules; those are not part of local zero-spend mode.

External-provider media always remains at provider URLs. Even when a license permits transformation, source bytes are fetched into bounded temporary storage for the active job and then discarded. GoGIF retains only the newly generated output and its provenance.

### 2. Catalog and coordination plane

MemKV stores small JSON documents and indexes:

```text
media:v1:{assetID}                              asset, provenance, rights, renditions
provider:v1:{provider}:{externalID}             provider ID -> asset/cache record
blob:v1:sha256:{digest}                         object metadata and reference count
owner:v1:{ownerID}:assets                       set of private asset IDs
tag:v1:{normalizedTag}:assets                   set of asset IDs
created:v1:{scope}                              sorted set by creation time
job:v1:{jobID}                                  render state with TTL
idempotency:v1:{ownerID}:{requestHash}          request result with TTL
search:v1:{provider}:{policy}:{queryHash}        permitted provider cache with TTL
dedupe:v1:{scope}                               Bloom/Cuckoo membership filter
trend:v1:{scope}                                Count-Min sketch plus sorted-set leaders
```

The first checked-in Go interface covers `PING`, `GET`, `SET`, `DEL`, and TTL. Rich indexes will use a second MemKV-specific index interface so the durable asset document is not coupled to one command set.

For the MVP, MemKV with append-only persistence is the catalog system of record. Canonical records must run in a non-evicting instance: MemKV currently evicts after its key or memory limit rather than rejecting a write, so do not configure a memory bound on that instance and raise `-maxkeys` above the monitored catalog ceiling. A separately bounded MemKV instance may use LFU for disposable caches.

Before claiming high availability, the catalog needs a non-evicting write policy, tested backup/restore automation, and either replication or a durable secondary event log. GoGIF will not change the user's MemKV fork merely to pursue this roadmap; it will integrate through RESP and report a separately reproduced MemKV correctness bug if one appears.

## Runtime topology

```text
PWA / extension / future native shells
                  │
                  ▼
             GoGIF API
       ┌──────────┼──────────┐
       │          │          │
       ▼          ▼          ▼
 provider APIs  MemKV      render workers
 external URLs  metadata       │
                  │             ▼
                  └────── local blob files
```

Provider searches are federated but kept in separately attributed sections when terms prohibit blending. Search over GoGIF-owned/open media can combine lexical indexes, tags, popularity signals, and a later vector index freely.

## Write paths

### Generate

1. Validate prompt and options.
2. Create a renderer spec and render job record in MemKV.
3. Render once; calculate SHA-256 while writing the output.
4. Put immutable bytes into local content-addressed storage.
5. Write the asset document and secondary indexes to MemKV.
6. Return the asset ID plus a local asset URL.

### Upload/reference transformation

1. Create a short-lived job record in MemKV.
2. Stream a user upload or allowlisted provider reference to a bounded local temporary file.
3. Verify checksum, MIME signature, dimensions, duration, malware policy, and size.
4. Capture source, author, license, attribution, and permission fields.
5. Queue derivative rendering and fingerprinting.
6. Delete provider source bytes when the job ends; publish only the newly generated result after moderation and rights checks pass.

### Provider search

1. Normalize query, locale, rating, and provider policy.
2. Read a cached result only where provider terms permit it.
3. Query each provider from the required client/server boundary.
4. Preserve provider rank and attribution.
5. Return separate source sections plus GoGIF's own blended catalog.

## MemKV operating profile

For local integration, run the `develop` branch on loopback:

```sh
go run ./cmd \
  -host 127.0.0.1 \
  -port 8081 \
  -appendonly \
  -appendfsync everysec \
  -maxmemory 0 \
  -maxkeys 2147483647
```

Then run GoGIF with:

```sh
GOGIF_MEMKV_ADDR=127.0.0.1:8081 \
GOGIF_BLOB_DIR=.data/blobs \
make run
```

The unbounded development profile requires memory monitoring; it is not the final production safety model. No MemKV source change is required for this local integration. At larger scale, GoGIF should use a datastore configuration that rejects writes instead of evicting canonical records. Keep this MemKV deployment on loopback for local use. Settle permission/licensing before distributing a product that incorporates or deploys the fork.

## Consequences

- A KV remains the fast center of asset lookup, TTLs, jobs, ranking, and deduplication.
- Large GIF/video bodies do not consume MemKV memory or AOF bandwidth.
- Provider content cannot accidentally become an unlicensed mirror.
- Content hashes deduplicate identical bytes and make integrity checks cheap.
- Blob files and MemKV must be reconciled after partial failures; a later repair tool can scan unreferenced objects and missing renditions.
- Full-text and semantic retrieval require dedicated indexes as the corpus grows; they do not replace the canonical MemKV asset record.

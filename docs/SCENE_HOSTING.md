# Scene workspace hosting

Scene rendering must be asynchronous. The Go web/API process is a control plane; Blender and the selected renderer run on a separate worker. A browser request creates a project and returns immediately. Workers pull leases, report progress, upload artifacts directly to object storage, and then mark the job complete.

```text
browser
   │ create / poll / cancel
   ▼
GoGIF API ── project + job metadata ──> durable KV/database
   ▲                                      │
   │ worker leases + heartbeats           │ queue index
   │                                      ▼
GPU worker ── authenticated stream ──> private blob storage
   │
   ├─ Comfy semantic reference acquisition
   ├─ Blender asset preparation
   ├─ Unreal render (initial hosted target)
   └─ FFmpeg MP4/WebM master
```

The authenticated streaming upload is the Phase 1 implementation. It is
bounded, content-addressed, and physically reopened by the API before job
completion. A later S3 adapter can replace the file-backed blob store, followed
by direct presigned uploads when artifact size or worker count warrants it; the
worker's project/job contract does not change.

## Recommendation

Use three deliberate phases instead of moving the whole application into the cloud now.

### 1. Development: owned NVIDIA PC worker

Keep the current Go API on the Mac for private testing and run one pull-based worker on the owned Windows/NVIDIA machine over Tailscale. This validates leases, cancellation, Blender handoff, Unreal Movie Render Queue, artifact upload, and real render costs without paying for an idle cloud GPU. The worker needs outbound HTTPS only; never expose the editor or an inbound render-control port.

The worker executable now exists at `cmd/gogif-scene-worker`. Keep Scene disabled
on the live API until the Windows renderer passes the smoke test below. Enable
the control plane with:

```sh
export GOGIF_ENABLE_SCENE_JOBS=true
export GOGIF_SCENE_WORKER_TOKEN='a-random-secret-of-at-least-32-characters'
export GOGIF_SCENE_TARGETS=unreal
export GOGIF_MEMKV_ADDR=127.0.0.1:8081
export GOGIF_AUTH_MODE=local
export GOGIF_LOCAL_OWNER_EMAIL='owner@example.com'
export GOGIF_SESSION_SECRET='a-separate-random-secret-of-at-least-32-characters'
```

Use persistent MemKV for a single API replica during private testing. The current small KV interface serializes claims inside that replica. Before multiple API replicas, move claims to a transactional database or managed queue.

On the Windows/NVIDIA host:

1. Install or verify Go, Blender, Unreal Engine 5.8, FFmpeg, the latest NVIDIA
   Studio driver, Git, and Tailscale.
2. Clone this repository to `C:\gogifgenerator` and copy
   `.env.worker.example` to the ignored `.env.worker` file.
3. Put the same `GOGIF_SCENE_WORKER_TOKEN` in the API and worker files. Put the
   Comfy key only in the worker file. Adjust the executable paths to the actual
   installed versions.
4. Build and make one outbound-only claim:

```powershell
cd C:\gogifgenerator
.\scripts\windows\run-scene-worker.ps1 -Once
```

5. After the empty-queue handshake succeeds, keep the worker running:

```powershell
.\scripts\windows\run-scene-worker.ps1
```

For a persistent current-user worker that starts at Windows login and restarts
after transient failures, install and start the versioned scheduled task:

```powershell
.\scripts\windows\install-scene-worker-task.ps1 -Start
```

Inspect it with `Get-ScheduledTask -TaskName "GoGIF Scene Worker"`. Remove it
only when intended with
`.\scripts\windows\install-scene-worker-task.ps1 -Uninstall`. The task uses an
interactive, non-elevated principal because Unreal needs the signed-in user's
graphics session and Epic profile; the worker still makes outbound requests
only.

The worker rejects plain HTTP except on loopback, never opens a listening port,
never logs either secret, and removes each temporary workspace after
upload or failure. Every claim carries protocol version, worker version, target,
and Blender/Unreal/FFmpeg capabilities. Unsupported or incomplete workers are
rejected before they can lease work.

### 2. Design-partner beta: warm GPU Pod

Use a long-running or start-on-demand GPU Pod rather than a serverless function. Unreal editor images, shaders, and project caches are large, so repeated cold starts erase most serverless savings. A private worker image should pin the engine/project version and run as a non-root user. Start with one NVIDIA GPU, at least 32 GB system memory, and at least 16 GB VRAM; 48 GB VRAM and 64+ GB system memory leave more room for high-resolution scenes.

[RunPod Pods support private container templates](https://docs.runpod.io/pods/templates/manage-templates), and its [network volumes persist independently of compute](https://docs.runpod.io/storage/network-volumes). A network volume is useful for engine/project caches, but final artifacts should live in object storage with lifecycle policies rather than on a worker volume. RunPod documents that one network volume constrains workers to its datacenter and that simultaneous writers need application coordination.

Unreal publishes official Linux development/runtime containers, although Epic still labels container support Beta. [Epic's container overview](https://dev.epicgames.com/documentation/unreal-engine/overview-of-containers-in-unreal-engine) and [container quick start](https://dev.epicgames.com/documentation/unreal-engine/quick-start-guide-for-using-container-images-in-unreal-engine) require authenticated access to Epic's GitHub Container Registry. Treat those images and registry credentials as licensed build inputs, never public image layers.

### 3. Production: managed control plane and elastic workers

Once measured demand justifies it, a single-cloud AWS topology is operationally conservative:

- containerized Go API;
- PostgreSQL for users, projects, entitlements, jobs, idempotency, and billing state;
- S3 for private source/intermediate/final artifacts with short presigned access and lifecycle deletion;
- SQS plus a dead-letter queue for worker dispatch;
- EC2 G6/G6e workers, scaled from queue depth, with no public inbound access.

[SQS Standard is at-least-once](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/standard-queues-at-least-once-delivery.html), so worker completion and artifact writes must remain idempotent even after the current in-process queue is replaced. [S3 lifecycle rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lifecycle-mgmt.html) can expire intermediates independently of saved masters. [EC2 G6 instances use NVIDIA L4 GPUs](https://aws.amazon.com/ec2/instance-types/g6/) and support Vulkan/OpenGL plus NVIDIA video encoding; benchmark Unreal's exact project before selecting instance size.

AWS is not the first development target because it adds queue, IAM, database, autoscaling, image, and networking work before the render pipeline is proven. The worker lease contract avoids locking GoGIF to AWS or RunPod.

## Renderer decision and licensing boundary

Unreal is the initial hosted target. Epic documents [command-line Movie Render Queue](https://dev.epicgames.com/documentation/en-us/unreal-engine/using-command-line-rendering-with-move-render-queue-in-unreal-engine), recommended development hardware of 32 GB RAM and 8 GB or more graphics memory, and generally treats rendered video as a royalty-free non-engine product under the [Unreal Engine EULA](https://www.unrealengine.com/eula/unreal). Seat requirements and third-party asset licenses still apply.

Unity remains in the schema for local/internal experimentation and export workflows, but it is disabled by default. Unity's current Editor Software Terms say editor functionality or processing cannot be made available to end users through SaaS/cloud services without a separate grant. Before GoGIF offers hosted Unity rendering, obtain written licensing guidance from Unity. This is a product/legal gate, not just a technical installation step. The current [Unity plans page](https://unity.com/products) also describes revenue/funding thresholds and Build Server capacity.

## Implemented control plane and worker

The backend is intentionally UI-hidden and disabled by default:

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/scenes` | Validate and enqueue an owner-scoped Scene project |
| `GET` | `/api/v1/scenes` | List the signed-in owner's projects |
| `GET` | `/api/v1/scenes/{id}` | Read state, progress, stage, error, and artifact metadata |
| `POST` | `/api/v1/scenes/{id}/cancel` | Cancel queued work or request cooperative worker cancellation |
| `POST` | `/api/v1/scene-jobs/claim` | Lease the oldest target-compatible job to an authenticated worker |
| `POST` | `/api/v1/scene-jobs/{id}/heartbeat` | Renew the lease and publish bounded stage/progress state |
| `PUT` | `/api/v1/scene-jobs/{id}/artifacts/{kind}` | Stream a lease-bound artifact into private storage |
| `POST` | `/api/v1/scene-jobs/{id}/finish` | Retry/fail/cancel or finish with a matching MP4/WebM master record |

Worker calls use a server-side bearer token compared in constant time. Lease tokens are random per attempt. Expired work can be reclaimed up to three attempts; stale workers cannot heartbeat, upload, or finish after losing a lease. Project ownership is checked on every browser endpoint. Artifact keys are restricted to the project's object prefix and require bounded size, MIME, and SHA-256 metadata. A successful finish reopens every uploaded blob and checks its recorded digest and size; merely submitting plausible metadata is not sufficient.

`gogif-scene-worker` currently implements the complete first target:

```text
claim → Comfy FLUX semantic reference → Blender FBX preparation
      → Go motion contract → Unreal frames → FFmpeg MP4/WebM
      → upload video + poster + FBX → verified finish
```

It heartbeats throughout generation, rendering, encoding, and upload. A browser
cancel request cancels the active command context and is acknowledged as a
terminal canceled job. Worker shutdown deliberately leaves the lease to expire
so another attempt can safely reclaim it.

## Required before the UI switch appears

1. Render one Blender → Unreal → FFmpeg project on the Windows worker, then run real cancellation, worker-crash/reclaim, and corrupted-upload tests.
2. Add an owner-authorized artifact download/preview endpoint; private storage is currently worker-ingest only.
3. Add progress streaming or bounded browser polling and the real Scene workspace UI.
4. Add durable metering: reserve estimated credits, settle measured compute/storage after success, and release failed/canceled work.
5. Add moderation, per-plan duration/resolution limits, per-user queue caps, deadlines, and automatic intermediate/master retention rules.
6. Replace the Phase 1 file blob adapter with S3-compatible storage before moving the API off the owned Mac; add presigned direct uploads when measurements justify them.

Only then should Create expose `Scene` beside GIF and 3D model.

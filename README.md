# GoGIF

**One prompt. The right GIF—made or found.**

GoGIF is a Go-powered GIF creation and discovery app designed to feel equally at home in a browser, an installed phone/desktop PWA, and a browser extension. The current repository is a working vertical slice: enter an idea, receive a real animated GIF, download it, or search a licensed GIF catalog when a GIPHY key is configured.

## What works now

- Pure-Go animated GIF renderer with orbit, pulse, wave, and confetti motion systems
- Natural-language art planning with a deterministic offline planner
- Optional OpenAI art direction through the Responses API and strict structured output
- Automatic local fallback if the AI provider is unavailable
- Responsive, installable PWA embedded in the Go binary
- Direct-to-GIPHY search integration with required attribution
- Browser-extension development shell
- Bounds checking, request limits, graceful shutdown, tests, and CI

## Run it

Requirements: Go 1.26.5 or newer.

```sh
make run
```

Open <http://localhost:8080>. No account or API key is required for local GIF creation.

To turn on AI-directed planning:

```sh
export OPENAI_API_KEY="your-project-key"
export OPENAI_MODEL="gpt-5-mini"
make run
```

To enable catalog search, create a platform-specific GIPHY API key and run:

```sh
export GIPHY_API_KEY="your-giphy-key"
make run
```

GIPHY requires search requests to originate from the client, so the web app receives this platform key at runtime. Never use a private server credential in that setting. See the [GIPHY API requirements](https://developers.giphy.com/docs/api/).

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | Readiness and engine status |
| `GET` | `/api/v1/config` | Public client capabilities |
| `POST` | `/api/v1/gifs/plan` | Inspect the prompt-derived animation plan |
| `POST` | `/api/v1/gifs/generate` | Stream an `image/gif` response |

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
browser extension ───┼──> Go HTTP API ──> art planner ──> pure-Go renderer
web browser ─────────┘                    ├─ local          └─ animated GIF
                                         └─ OpenAI

web search client ─────────────────────────> licensed catalog APIs
```

The planner speaks a small, validated animation-spec contract. That keeps model vendors, renderers, and future native clients replaceable. The OpenAI adapter uses the Responses API with Structured Outputs, following the [official OpenAI API reference](https://developers.openai.com/api/reference/cli/resources/responses/methods/create).

Read [Architecture](docs/ARCHITECTURE.md) for boundaries and scaling decisions, and [Roadmap](docs/ROADMAP.md) for the staged product plan.

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

# Contributing

## Local loop

1. Install the Go version declared in `go.mod`.
2. Run `make run` and open <http://localhost:8080>.
3. Run `make check` before submitting a change.

Keep core behavior behind the existing domain boundaries. Model-provider code belongs in `internal/planner`; pixel encoding belongs in `internal/render`; HTTP-specific logic belongs in `internal/httpapi`.

## Pull requests

- Explain the user-visible outcome and important tradeoffs.
- Include tests for behavior changes.
- Avoid new runtime dependencies unless they materially improve the product.
- Never commit API keys, user media, or generated assets containing private data.
- Document any provider terms, attribution, analytics, or storage requirements introduced by an integration.

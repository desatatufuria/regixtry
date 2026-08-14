# Development

## Structure

- `cmd/regixtry`: entrypoint and CLI parsing.
- `internal/domain`: auth and registry invariants.
- `internal/app`: use cases.
- `internal/ports`: interfaces.
- `internal/infra`: adapters and Linux lifecycle.
- `internal/protocol/http`: API.
- `internal/tui`: Bubble Tea console.

## Build and tests

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

`gofmt -l .` should print nothing — the repository is expected to stay gofmt-clean, and `go vet ./...` is expected to report no warnings. The release workflow (`.github/workflows/release.yml`) runs `go test ./...` as a gate before every tagged release.

The Dockerfile uses `CGO_ENABLED=0`, builds `./cmd/regixtry`, and produces a Debian slim image with `ca-certificates`.

## Adding an endpoint

Routing is registered in `internal/protocol/http/router.go`. Logic must stay in `internal/app`, and invariants in `internal/domain`; the handler only translates HTTP, auth, and errors. Add tests under `internal/protocol/http/` and application/domain tests when behavior changes.

## Modifying the TUI

`internal/tui/model.go` holds screens, key handling, and rendering; `admin_client.go` holds the administrative HTTP client. The TUI must not read storage directly for registry decisions, nor fabricate an authenticated identity.

## Tests and evidence

Unit tests cover domain, stores, router, auth, lifecycle, and the TUI. Evidence scripts live under `docs/verification/scripts/`. Manual evidence alone does not equate to a general deployment guarantee.

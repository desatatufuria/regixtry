# Phase 4 verification: operator console and feature-manager compatibility

This slice verifies the operator console, admin protocol, and backend-driven feature-manager shell.

## Quick path

1. Run the Go suite for protocol, service, and TUI behavior.
2. Run the Docker push/pull smoke script against a local registry instance.
3. Run the TUI smoke script and confirm both the snapshot launch and the focused feature-manager tests pass.

## Verification checklist

- [ ] `go test ./...` passes.
- [ ] Protocol integration tests cover `/admin/v1/features/{name}` feature pages and `/admin/v1/features/{name}/actions/{actionID}` typed actions.
- [ ] Service tests cover generic summary pages, minimal pages, ordered Trivy sections, and declared actions.
- [ ] TUI tests prove backend-authored action help, minimal feature pages, and page refresh on selection changes.
- [ ] Docker CLI push/pull succeeds against the local server.
- [ ] TUI smoke output shows the snapshot launch plus feature-manager assertions.

## Commands

```bash
GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...

GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go run ./cmd/regixtry feature list -storage-root /tmp/tui-feature-manager-smoke -db /tmp/tui-feature-manager-smoke/metadata.db

GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go run ./cmd/regixtry feature status trivy -storage-root /tmp/tui-feature-manager-smoke -db /tmp/tui-feature-manager-smoke/metadata.db

docs/verification/scripts/docker-push-pull-smoke.sh /tmp/tui-feature-manager-smoke

docs/verification/scripts/tui-smoke.sh /tmp/tui-feature-manager-smoke
```

## Expected results

| Step | Expected result |
| --- | --- |
| Go test suite | All package tests pass. |
| `feature list` smoke | Output keeps the lightweight summary columns for every feature. |
| `feature status` smoke | Output still reports truthful runtime details for CLI inspection. |
| Feature-page protocol tests | `/admin/v1/features/{name}` returns `summary`, `header`, `sections`, and `actions`; typed action routes return backend-authored messages. |
| TUI smoke | The snapshot still launches, and focused TUI feature-manager tests confirm generic page rendering, minimal pages, and backend-authoritative help text. |

## Notes

- The apply environment requires explicit `GOMODCACHE`, `GOPATH`, and `GOSUMDB=off` values for Go commands.
- The snapshot mode does not complete an interactive login, so feature-manager smoke coverage is anchored in deterministic focused TUI tests.
- The feature manager intentionally avoids plugin frameworks; the backend declares only explicit header, field, row, and action payloads.

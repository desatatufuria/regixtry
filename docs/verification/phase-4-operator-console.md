# Phase 4 verification: operator console and protocol compatibility

This slice closes `registry-foundation` Phase 4.

## Quick path

1. Run the Go integration suite for protocol and TUI behavior.
2. Run the Docker push/pull smoke script against a local registry instance.
3. Run the TUI smoke script against the same storage root and confirm the repository view renders.

## Verification checklist

- [ ] Protocol integration tests pass for push/pull success.
- [ ] Protocol integration tests reject digest mismatches.
- [ ] Protocol integration tests cover anonymous pull on and off.
- [ ] Protocol integration tests reject manifest publish when a referenced blob is missing.
- [ ] Protocol integration tests prove incomplete uploads stay out of published catalog state.
- [ ] TUI smoke output shows repository inspection data without direct storage access.
- [ ] Docker CLI push/pull succeeds against the local server.

## PR work-unit alignment

| Work unit | What to verify first | Out of scope |
|---|---|---|
| PR 3 / Unit 3 | `go test ./...` for `internal/protocol/http`, `internal/tui`, and `cmd/registry` | Post-v1 mutations such as delete, retention, and GC |
| Docker smoke | `docs/verification/scripts/docker-push-pull-smoke.sh` | Remote storage backends, auth providers, multi-tenant setups |
| TUI smoke | `docs/verification/scripts/tui-smoke.sh` | Interactive visual polish beyond inspection flows |

## Commands

```bash
GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...

docs/verification/scripts/docker-push-pull-smoke.sh /tmp/registry-foundation-smoke

docs/verification/scripts/tui-smoke.sh /tmp/registry-foundation-smoke
```

## Expected results

| Step | Expected result |
|---|---|
| Go integration suite | All package tests pass. |
| Docker smoke | `docker pull` returns the image pushed into the local registry. |
| TUI smoke | Snapshot output includes `Registry Console` and the seeded repository name. |

## Notes

- The environment used for automated apply work requires explicit `GOMODCACHE`, `GOPATH`, and `GOSUMDB=off` values for Go commands.
- The Docker smoke script assumes a working local Docker daemon and an available loopback port.

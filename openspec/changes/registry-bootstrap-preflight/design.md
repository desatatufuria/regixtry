# Design: Registry Bootstrap Preflight

## Technical Approach

Keep the current bootstrap shape in `cmd/registry/main.go` and `internal/infra/install/linux/bootstrap.go`, but split post-artifact behavior into two explicit paths: start-service (default) and artifact-only (`--no-start`). The preflight gate runs after artifact generation and before `systemctl daemon-reload` / `systemctl enable --now`, matching the spec change for `installation-modes`.

## Architecture Decisions

| Decision | Options | Choice / Rationale |
|---|---|---|
| Bootstrap mode flag | Infer from missing startup side effects; add `--no-start` | Add `NoStart bool` to `BootstrapConfig` and parse `--no-start` in `parseBootstrapConfig`. This matches the existing flag-driven CLI contract and keeps rollback separate. |
| Preflight implementation | Probe with HTTP; inspect `ss`; attempt real bind | Attempt a temporary `net.Listen("tcp", cfg.Addr)` only for configured local binds (`127.0.0.1`, `localhost`, `::1`). A real bind verifies the exact startup resource, avoids shelling out during detection, and stays scoped to local addresses. |
| Failure output | Print ad hoc stderr in `run`; return typed error | Return a typed/bootstrap-specific error whose `Error()` renders a fixed multiline recovery block. This preserves the current runner interface and the existing top-level error printing path. |

## Data Flow

`registry bootstrap` → `parseBootstrapConfig(--no-start)` → `Bootstrapper.Run`
→ `ValidateConfig` / distro detect → `plan` → `writeArtifacts`
→ if `NoStart`: return success
→ if local bind: preflight temp listen on `cfg.Addr`
→ occupied: return recovery error
→ free: `systemctl daemon-reload` → `systemctl enable --now` → readiness probe.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `cmd/registry/main.go` | Modify | Add `--no-start` parsing and pass the new field through bootstrap config. |
| `cmd/registry/main_test.go` | Modify | Cover `--no-start` parsing and CLI-visible occupied-bind failure text. |
| `internal/infra/install/linux/bootstrap.go` | Modify | Add `NoStart`, local-bind classifier, preflight bind check, and rendered recovery error before service start. |
| `internal/infra/install/linux/bootstrap_test.go` | Modify | Cover no-start success, occupied local bind failure before systemctl, and non-local bypass. |

## Interfaces / Contracts

```go
type BootstrapConfig struct {
    // existing fields...
    NoStart bool
}

// Run contract:
// - NoStart=true: artifacts only; skip preflight, systemctl, and readiness probe.
// - NoStart=false + local bind occupied: fail before daemon-reload/enable.
// - Non-local binds: skip this preflight slice.
```

Recovery error shape:
- first line: occupied local bind summary with the configured address
- then exact commands, rendered from config:
  - `sudo ss -ltnp 'sport = :<port>'`
  - `sudo systemctl stop <service>.service`
  - `registry bootstrap ... --addr <new-addr> --public-url <matching-url>`

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Local-bind classification, `--no-start` parsing, recovery rendering | Table-driven Go tests in `cmd/registry/main_test.go` and `internal/infra/install/linux/bootstrap_test.go`. |
| Integration | Bootstrap artifact write + control-flow split | Stub `runCommand` / `probe`; assert `NoStart` skips startup and occupied local bind skips all systemctl calls. |
| E2E | N/A for this slice | Existing smoke scripts stay unchanged; this change is covered at package level. |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | N/A — no executable-file classification change | None | None |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A — no VCS command targeting | None | None |
| Commit state | staged, `commit -a`, empty index | N/A — no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A — no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A — no PR automation | None | None |

## Migration / Rollout

No migration required. Rollout is direct: default bootstrap still installs and starts the service, while `--no-start` is an explicit opt-out path.

## Open Questions

- [ ] Should the re-run recovery command suggest a placeholder port only, or derive the next example port from the occupied one?

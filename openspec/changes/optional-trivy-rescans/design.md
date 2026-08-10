# Design: Optional Trivy Rescans

## Technical Approach

Add a small scanning subsystem inside the existing Go binary. `cmd/regixtry/main.go` will wire a scan app service, SQLite-backed scan persistence, fixed Trivy process runner, and one bounded scheduler loop. Setup seeds defaults through managed env/provenance; runtime admin API mutates persisted settings in SQLite. Tag-triggered rescans resolve through the existing manifest store, then execute against an immutable digest reference. Slice 1 intentionally stops at backend management plus basic visibility; rich TUI settings/history/actions move to a later follow-up slice.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Scheduler shape | In-process bounded scheduler with one lease-owning loop and bounded workers | Helper binary; external cron only | Fits the current single-binary/single-node product, reuses existing lifecycle wiring, and avoids inventing service management now. |
| Settings authority | Setup writes startup defaults; admin API writes the authoritative mutable row | Env-only runtime config; setup mutating DB forever | Keeps install provenance truthful while letting operators change behavior without reinstalling. |
| Persistence | Add `scan_settings`, `scan_runs`, `scan_scheduler_state` to `internal/infra/metadata/sqlite/store.go` init | Separate DB; generic jobs tables | Narrow additive schema matches today’s SQLite bootstrap pattern and keeps review scope bounded. |
| Target identity | Persist requested reference plus resolved manifest digest; scan `<public-host>/<repo>@<digest>` | Scan tags directly; scan raw manifest bytes | Digest execution is immutable and matches existing manifest storage semantics. |
| Trivy execution | `exec.CommandContext` wrapper, explicit argv, shared cache dir, normal DB refresh, per-run timeout | Shell out through `sh -c`; always skip DB update | Fixed argv avoids injection and stale-default behavior. |
| Slice-1 control surface | Backend-authoritative admin endpoints first; richer TUI management deferred | Full API+TUI management in the same slice; env-only controls | Reduces first-slice coupling and review load while preserving the long-term backend-authoritative TUI direction. |

## Data Flow

    Setup env/provenance -> startup defaults
                         -> scan_settings (first boot if missing)
    Admin API -> scan service -> resolve tag via metadata.ResolveManifest
              -> scan_runs(queued/running/completed)
              -> Trivy runner -> summary + DB freshness persisted
              -> scheduler_state heartbeat/lease

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` | Modify | Wire scan service into `serve`; stop scheduler on shutdown. |
| `internal/ports/regixtry.go` | Modify | Extend metadata/app contracts for scan settings, runs, and scheduler state. |
| `internal/ports/auth.go` | Modify | Add admin scan settings/run contracts beside existing admin interfaces. |
| `internal/app/regixtry/` | Modify/Create | Add scan orchestration and tag->digest resolution helpers. |
| `internal/infra/metadata/sqlite/store.go` | Modify | Add additive scan tables and CRUD/lease methods. |
| `internal/infra/install/linux/{bootstrap.go,templates.go,provenance.go}` | Modify | Seed defaults, render env, and preserve provenance intent. |
| `internal/protocol/http/admin_handlers.go` | Modify | Add `/admin/v1/scan-settings`, `/admin/v1/scan-runs`, and latest-status routes. |
| `internal/infra/scanning/trivy/runner.go` | Create | Fixed-argv Trivy process runner, JSON parsing, timeout/error mapping. |
| `internal/app/scanning/scheduler.go` | Create | Lease-based bounded scheduler loop and shutdown handling. |
| `README.md`, `docs/` | Modify | Document setup/admin API usage, verification expectations, and deferred TUI follow-up. |

## Interfaces / Contracts

```go
type ScanSettings struct { Enabled bool; ScheduleEnabled bool; Interval time.Duration; Timeout time.Duration; CacheDir string; MaxConcurrency int }
type ScanTrigger struct { Repository string; Reference string; Trigger string }
type ScanRun struct { ID string; Repository string; RequestedRef string; Digest string; Status string; Trigger string; StartedAt, FinishedAt *time.Time; Critical, High, Medium, Low int; TrivyVersion string; DBUpdatedAt *time.Time; Error string }
```

Admin routes:
- `GET/PUT /admin/v1/scan-settings`
- `POST /admin/v1/scan-runs`
- `GET /admin/v1/scan-runs?repository=&limit=`

Manual triggers return the existing queued/running run when the same digest is already active; scheduled work skips locked digests.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | settings merge, lease rules, digest target builder, Trivy argv builder, timeout/error mapping | Strict TDD RED first in new scan packages; table-driven Go tests. |
| Integration | SQLite schema/bootstrap, tag->digest resolution, admin scan routes, scheduler recovery/overlap prevention | `httptest` + temp SQLite; relevant-package `go test` during RED/GREEN, full `go test ./...` in verify. |
| Verification | setup docs, admin API usage notes, and bounded scheduler expectations | Update docs and verification guidance in this slice; extend TUI smoke in the follow-up slice. |

## Threat Matrix

| Boundary | Minimum adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | `requirements.txt`, `CMakeLists.txt`, executable Markdown/MDX, `README.sh` | N/A — no executable-file classification input | Fixed Trivy executable/argv only | None |
| Git repository selection | `git -C`, relative paths, absolute paths | N/A — no Git operations | None | None |
| Commit state | staged, `commit -a`, empty index | N/A — no commit automation | None | None |
| Push state | tracking branch, first push, explicit refspec | N/A — no push automation | None | None |
| PR commands | explicit `--head`, environment prefix, composed commands | N/A — no PR automation | None | None |

Separate subprocess boundary requirement: the Trivy runner MUST use `exec.CommandContext`, never `sh -c`, and MUST have RED tests for timeout, non-zero exit, malformed JSON, and context cancellation.

## Migration / Rollout

Additive SQLite bootstrap only. Existing installs keep the feature disabled until defaults or admin settings enable it. No publish-path migration required.

## Deferred Follow-up

- Rich TUI settings/history/manual rescan management built on the admin API contracts from this slice.
- Broader UX polish and non-essential observability once the backend slice is proven.

## Open Questions

- [ ] None.

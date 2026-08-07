# Apply Progress: Registry Runtime Hardening

## Change
- Name: `registry-runtime-hardening`
- Mode: Standard
- Delivery: `exception-ok` / `stacked-to-main`
- Current slice: Work Unit 3 — complete via manual runtime verification evidence

## Completed Tasks
- [x] 1.1 Benchmark Go `http.Server` timeout defaults and small-registry TLS guidance; record target values for `cmd/registry/main.go` and `README.md`.
- [x] 1.2 Add RED tests in `cmd/registry/main_test.go` for `PublicURL`, TLS cert/key pairing, HTTPS-vs-HTTP mode mismatch, and derived `/auth/token` realm validation.
- [x] 1.3 Add RED tests in `cmd/registry/main_test.go` for safer `bootstrap-admin` secret precedence and discouraged `-password` compatibility messaging.
- [x] 2.1 Extend `serveConfig` and bootstrap config parsing in `cmd/registry/main.go` with `PublicURL`, TLS inputs, bounded shutdown/read-header settings, and safer secret input flags.
- [x] 2.2 Implement `normalizeRuntimeConfig` in `cmd/registry/main.go` to derive the canonical token realm and fail fast on scheme/host/port/path or TLS-mode mismatches.
- [x] 2.3 Update `serve` in `cmd/registry/main.go` to build bounded `http.Server` settings, choose `Serve` vs `ServeTLS`, and enforce finite shutdown behavior.
- [x] 2.4 Replace the nil-client fallback in `internal/tui/admin_client.go` with a bounded default timeout; keep injected clients untouched.
- [x] 3.1 Add GREEN/integration tests in `cmd/registry/main_test.go` for HTTPS startup, explicit local HTTP startup, bounded shutdown, and bootstrap guidance output.
- [x] 3.2 Add `internal/tui/admin_client_test.go` coverage for bounded default timeout and injected-client preservation.
- [x] 3.3 Update `docker-compose.yml` and `docs/verification/scripts/docker-push-pull-smoke.sh` for HTTPS-first runtime plus explicit HTTP local/dev verification.
- [x] 4.1 Update `README.md` with canonical `PublicURL`, TLS setup, startup failure cases, and preferred bootstrap secret entry guidance.
- [x] 4.2 Remove insecure/default examples superseded by the hardened flow and verify all operator-facing commands stay consistent across docs and compose.

## Files Changed
| File | Action | Notes |
|---|---|---|
| `cmd/registry/main.go` | Modified | Added canonical public URL/TLS parsing, default runtime timeout targets, startup normalization, and `bootstrap-admin` stdin secret handling with legacy argv warnings. |
| `cmd/registry/main_test.go` | Modified | Added table-driven coverage for public URL normalization, TLS mode validation, derived token realm checks, and bootstrap secret precedence/warnings. |
| `internal/tui/admin_client.go` | Modified | Replaced the nil-client fallback with a dedicated bounded-timeout HTTP client while preserving injected clients. |
| `internal/tui/admin_client_test.go` | Modified | Added focused coverage for bounded default timeout behavior and injected-client preservation. |
| `docker-compose.yml` | Modified | Switched the local helper runtime to canonical `REGISTRY_PUBLIC_URL` wiring instead of a manually duplicated token realm URL. |
| `docs/verification/scripts/docker-push-pull-smoke.sh` | Modified | Added canonical public URL/TLS inputs, stdin-based secret handling, HTTPS-aware readiness checks, and password-stdin Docker login. |
| `README.md` | Modified | Documented canonical `PublicURL` modes, startup failure cases, and preferred stdin-based bootstrap/login flows. |
| `openspec/changes/registry-runtime-hardening/tasks.md` | Modified | Marked all remaining Work Unit 3 and Phase 4 tasks complete based on the manual runtime verification evidence established in this session. |
| `openspec/changes/registry-runtime-hardening/apply-progress.md` | Modified | Refreshed cumulative apply progress to replace the blocked smoke note with the verified manual runtime evidence and final completion state. |

## Benchmark Notes
- Go `net/http` guidance favors `ReadHeaderTimeout` for header-bound protection while allowing handlers to manage body time separately; Work Unit 1 recorded `5s` for `ReadHeaderTimeout`, `30s` for `ReadTimeout`, `30s` for `WriteTimeout`, `120s` for `IdleTimeout`, and `10s` for graceful shutdown as the target defaults to wire in the serving slice.
- The runtime model keeps TLS optional but intentional: HTTPS public URLs require a complete cert/key pair, while explicit local HTTP mode forbids TLS inputs and derives `/auth/token` from the canonical public URL.
- Work Unit 2 applies those bounds in the actual `http.Server` construction and falls back to the hardened defaults when direct callers pass zero durations.
- The admin TUI now creates its own bounded default HTTP client instead of reusing the process-global `http.DefaultClient`, so operator calls no longer inherit unbounded timeout behavior by accident.
- The close-out slice moves local helper docs and smoke wiring to the canonical `REGISTRY_PUBLIC_URL` contract, so operator guidance no longer depends on manually duplicating `/auth/token` realm URLs.
- HTTPS smoke mode is now opt-in through `TLS_CERT_FILE` / `TLS_KEY_FILE`; curl-based readiness accepts `TLS_CA_FILE` for self-signed local verification while Docker push/pull still depends on daemon trust.

## Work Unit Evidence
| Work Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| 1 | `go test -count=1 ./cmd/registry -run 'TestParseServeConfig|TestNormalizeRuntimeConfig|TestParseBootstrapAdminConfig'` → `ok   registry/cmd/registry  0.007s` | `go test -count=1 ./cmd/registry -run TestServe` → `ok   registry/cmd/registry  0.079s` | `cmd/registry/main.go`, `cmd/registry/main_test.go`, and the Work Unit 1 task-state updates in `openspec/changes/registry-runtime-hardening/tasks.md` |
| 2 | `go test -count=1 ./cmd/registry ./internal/tui -run 'TestServe|TestNewHTTPAdminClient'` → `ok   registry/cmd/registry  0.123s` and `ok   registry/internal/tui  0.006s` | `go test -count=1 ./cmd/registry -run TestServe` → `ok   registry/cmd/registry  0.124s` (HTTP and HTTPS serve paths both responded and shut down cleanly) | `cmd/registry/main.go`, `cmd/registry/main_test.go`, `internal/tui/admin_client.go`, `internal/tui/admin_client_test.go`, and the Work Unit 2 task-state updates in `openspec/changes/registry-runtime-hardening/tasks.md` |
| 3 | `go test -count=1 ./cmd/registry ./internal/tui -run 'TestRunBootstrapAdminPrintsLegacyPasswordWarningToStderr|TestServe|TestNewHTTPAdminClient'` → `ok   registry/cmd/registry  0.417s` and `ok   registry/internal/tui  0.007s`; `go test -count=1 ./...` → `ok   registry/cmd/registry  1.422s`, `ok   registry/internal/app/auth  0.168s`, `ok   registry/internal/app/registry  0.347s`, `ok   registry/internal/domain/registry  0.008s`, `ok   registry/internal/infra/auth/postgres  0.250s`, `ok   registry/internal/infra/metadata/sqlite  0.165s`, `ok   registry/internal/infra/storage/fsblob  0.008s`, `ok   registry/internal/ports  0.006s`, `ok   registry/internal/protocol/http  1.180s`, `ok   registry/internal/tui  0.024s` | Manual runtime evidence (externally verified this session): `go run ./cmd/registry serve -public-url http://127.0.0.1:5560 -auth-token-realm http://127.0.0.1:5560/auth/token` against host Postgres `telemetry.host:15432` launched successfully; `GET /v2/_catalog` returned `401 Unauthorized` with Bearer challenge using the canonical realm; `/auth/token` issued a bearer token; authenticated Docker `login`, `push`, `pull`, and manifest fetch all succeeded against `127.0.0.1:5560`; server request logs confirmed the expected auth/runtime behavior. | `docker-compose.yml`, `docs/verification/scripts/docker-push-pull-smoke.sh`, `README.md`, and the final task-state updates in `openspec/changes/registry-runtime-hardening/tasks.md` |

## Remaining Tasks
- None.

## Status
12/12 tasks complete. Ready for verify.

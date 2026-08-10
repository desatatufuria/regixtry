# Apply Progress: optional-trivy-rescans

## Scope

- Delivery mode: single-pr (`size:exception` approved)
- Slice: slice-1 backend scope only
- Deferred and intentionally untouched: rich TUI scan management

## Completed Tasks

- [x] 1.1 RED: add `internal/infra/install/linux/{bootstrap,provenance}_test.go` cases for disabled defaults, bounded setup input, provenance recording, and truthful uninstall cleanup.
- [x] 1.2 GREEN: extend `internal/infra/install/linux/{bootstrap.go,templates.go,provenance.go}` and `cmd/regixtry/main.go` to capture optional Trivy env/defaults without enabling rescans by default.
- [x] 1.3 RED: add `internal/infra/metadata/sqlite/store_test.go` cases for `scan_settings`, `scan_runs`, and `scan_scheduler_state`, covering default-disabled reads plus completed/failed run persistence across reopen.
- [x] 1.4 GREEN: extend `internal/ports/{regixtry.go,auth.go}` and `internal/infra/metadata/sqlite/store.go` with scan settings/run/scheduler contracts and additive schema/bootstrap CRUD.
- [x] 2.1 RED: create `internal/infra/scanning/trivy/runner_test.go` for explicit argv, `exec.CommandContext` use, timeout, non-zero exit, malformed JSON, and context cancellation.
- [x] 2.2 GREEN: create `internal/infra/scanning/trivy/runner.go` to build fixed Trivy commands, parse JSON summaries, and persist scanner/db freshness evidence.
- [x] 2.3 RED: add `internal/app/regixtry/service_test.go` cases for settings bounds, tag-to-digest manual trigger queuing, unpublished-target rejection, and publish remaining non-blocking while runs exist.
- [x] 2.4 GREEN/REFACTOR: add scan orchestration in `internal/app/regixtry/` to resolve manifests, dedupe active digest runs, and expose latest/history queries.
- [x] 3.1 RED: extend `internal/protocol/http/router_test.go` for `GET/PUT /admin/v1/scan-settings`, `POST/GET /admin/v1/scan-runs`, auth enforcement, and backend-authoritative error/status payloads.
- [x] 3.2 GREEN: extend `internal/protocol/http/admin_handlers.go` and related admin services for scan settings, manual triggers, history, and latest-status routes.
- [x] 4.1 RED: create `internal/app/scanning/scheduler_test.go` for single-owner lease, stale-state reclaim, bounded concurrency, and overlap prevention before implementing `internal/app/scanning/scheduler.go` plus `cmd/regixtry/main.go` wiring/shutdown.
- [x] 4.2 GREEN/REFACTOR: wire startup scheduler ownership, heartbeat recovery, and settings-aware periodic batches without blocking publish flows.
- [x] 4.3 Update `README.md`, `docs/`, and verification guidance with optional rescan setup/admin API expectations and deferred TUI follow-up notes; finish with `go test ./...`.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 | `internal/infra/install/linux/bootstrap_test.go`, `internal/infra/install/linux/provenance_test.go` | Unit | ✅ `go test ./internal/infra/install/linux ./cmd/regixtry` | ✅ compile/test failures for missing Trivy install fields | ✅ install package tests passing | ✅ disabled + override cases | ✅ shared default helpers extracted |
| 1.2 | `internal/infra/install/linux/bootstrap_test.go`, `internal/infra/install/linux/provenance_test.go` | Unit | ✅ same baseline | ✅ env/provenance assertions added first | ✅ setup/bootstrap/install tests passing | ✅ defaults and managed cleanup paths covered | ✅ default helpers and intent parsing normalized |
| 1.3 | `internal/infra/metadata/sqlite/store_test.go` | Integration | ✅ `go test ./internal/infra/metadata/sqlite` | ✅ missing store methods/schema failed first | ✅ store package passing | ✅ settings + runs + scheduler reopen cases | ✅ row scanners extracted |
| 1.4 | `internal/infra/metadata/sqlite/store_test.go` | Integration | ✅ same baseline | ✅ ports/store contract additions broke compile first | ✅ persistence tests passing | ✅ default-disabled + completed/failed history cases | ✅ shared scan row decoding extracted |
| 2.1 | `internal/infra/scanning/trivy/runner_test.go` | Unit | N/A (new package) | ✅ runner tests written before implementation | ✅ runner package passing | ✅ argv + non-zero + malformed JSON + cancellation + no-shell cases | ✅ command wrapper isolated in `RunnerConfig` |
| 2.2 | `internal/infra/scanning/trivy/runner_test.go` | Unit | N/A (new package) | ✅ result parsing assertions added first | ✅ runner package passing | ✅ severity counting + error mapping cases | ✅ minimal JSON parser kept boundary-local |
| 2.3 | `internal/app/regixtry/service_test.go` | Integration | ✅ `go test ./internal/app/regixtry` | ✅ service scan queue/validation tests added first | ✅ service package passing | ✅ dedupe + missing target + publish non-blocking cases | ✅ scan orchestration isolated in `service_scanning.go` |
| 2.4 | `internal/app/regixtry/service_test.go` | Integration | ✅ same baseline | ✅ compile failures for missing service methods first | ✅ service package passing | ✅ settings normalization + digest queue path covered | ✅ scan gate and helpers extracted |
| 3.1 | `internal/protocol/http/router_test.go` | Integration | ✅ `go test ./internal/protocol/http` | ✅ admin scan route tests added first | ✅ router package passing | ✅ auth, valid update, invalid update, queue, list, missing target cases | ✅ JSON response helper reused |
| 3.2 | `internal/protocol/http/router_test.go` | Integration | ✅ same baseline | ✅ missing handler paths failed first | ✅ router package passing | ✅ settings + manual/history endpoints covered | ✅ dedicated scan decode/response helpers extracted |
| 4.1 | `internal/app/scanning/scheduler_test.go` | Unit | N/A (new package) | ✅ scheduler tests written before code | ✅ scheduler package passing | ✅ single-owner + stale reclaim cases | ✅ lease/service interfaces minimized |
| 4.2 | `internal/app/scanning/scheduler_test.go`, `cmd/regixtry/main_test.go` | Unit / Integration | ✅ `go test ./internal/app/scanning ./cmd/regixtry -run 'TestNewHandler|TestServe' -v` | ✅ startup wiring failures surfaced first | ✅ scheduler + command package tests passing | ✅ recovery and non-blocking startup coverage | ✅ startup defaults normalized before seeding |
| 4.3 | `README.md`, `docs/api.md`, `docs/installation.md`, `docs/verification/operator-admin-api.md` | Documentation | N/A | ✅ docs gaps identified from spec before edits | ✅ `go test ./...` passing after docs-linked code changes | ✅ setup + API + deferred TUI notes all documented | ➖ None needed |

## Test Summary

- Total tests written/extended: 13 task-level additions across install, SQLite, runner, service, router, and scheduler packages
- Total tests passing: `go test ./...` passed
- Layers used: Unit (`internal/infra/install/linux`, `internal/infra/scanning/trivy`, `internal/app/scanning`), Integration (`internal/infra/metadata/sqlite`, `internal/app/regixtry`, `internal/protocol/http`, `cmd/regixtry`)
- Approval tests: None — this slice added behavior rather than preserving a refactor-only seam
- Pure functions/helpers created: install default helpers, scan settings normalization helpers, scan settings response/decode helpers, scan run row decoders

## Work Unit Evidence

| Work Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| Setup defaults + SQLite persistence | `go test ./internal/infra/install/linux ./internal/infra/metadata/sqlite` → pass | `go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestServeStartsAndRespondsToPing|TestServeStartsAndRespondsToPingOverTLS|TestServeShutdownDeadlineStopsWaitingOnActiveUpload'` → pass | `internal/infra/install/linux/*`, `internal/infra/metadata/sqlite/*`, `internal/ports/regixtry.go`, `cmd/regixtry/main.go` |
| Trivy runner + scan app service + admin API/manual history | `go test ./internal/infra/scanning/trivy ./internal/app/regixtry ./internal/protocol/http` → pass | `go test ./internal/protocol/http -run 'TestRouterAdminScanSettingsRoutesRequireAuthAndPersistUpdates|TestRouterAdminScanRoutesQueueAndListRuns|TestRouterAdminScanRoutesRejectInvalidTargetsAndSettings'` → pass | `internal/infra/scanning/trivy/*`, `internal/app/regixtry/*`, `internal/protocol/http/*` |
| Scheduler recovery + docs/verification | `go test ./internal/app/scanning ./cmd/regixtry` → pass | `go test ./...` → pass | `internal/app/scanning/*`, `cmd/regixtry/main.go`, `README.md`, `docs/api.md`, `docs/installation.md`, `docs/verification/operator-admin-api.md` |

## Remaining Tasks

- [ ] Deferred follow-up: rich TUI settings/history/manual rescan actions

## Status

- Current batch status: ready for verify
- Task completion: 13/13 current-slice tasks complete

## Remediation Batch: Verify Follow-up

### Findings Addressed

- [x] Scheduler cadence now follows the persisted/operator-configured `settings.Interval` instead of a fixed 1-second loop.
- [x] Added runtime proof that a failed scan run does not hide already-published content.
- [x] Fixed the adjacent `go vet` warning by avoiding `sync.Mutex` copies in the scan runner test double.

### Remediation TDD Cycle Evidence

| Finding | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| Scheduler cadence respects `settings.Interval` | `internal/app/scanning/scheduler_test.go` | Unit | ✅ `go test ./internal/app/scanning ./internal/app/regixtry ./cmd/regixtry` | ✅ `go test ./internal/app/scanning -run TestSchedulerRespectsConfiguredInterval` failed with `calls = 5, want exactly 1 scheduled batch before configured interval elapses` | ✅ `go test ./internal/app/scanning -run 'TestSchedulerPreventsOverlapAndReclaimsStaleLease|TestSchedulerRespectsConfiguredInterval'` passed | ✅ Existing overlap/stale-lease case stayed green while the new interval case covered the separate cadence path | ✅ Replaced the fixed ticker with per-cycle interval selection and aligned handler wiring to seed the scheduler with persisted interval defaults |
| Failed scan keeps published content available | `internal/app/regixtry/service_test.go` | Integration | ✅ same baseline | ✅ `go test ./internal/app/regixtry -run TestServiceFailedScanDoesNotHidePublishedContent` initially failed while the new runtime proof was being introduced (`database is locked` during concurrent polling) | ✅ `go test ./internal/app/regixtry -run 'TestServicePublishRemainsAvailableWhileScanRunsExist|TestServiceFailedScanDoesNotHidePublishedContent'` passed | ✅ Pending-run publish coverage and failed-run read/pull coverage now both execute against the real service + SQLite path | ✅ Updated the fake scan runner to pointer receivers so `go vet` stays clean and the test helper no longer copies a `sync.Mutex` |

### Remediation Work Unit Evidence

| Evidence | Exact result |
|---|---|
| Focused test command and exact result | `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/scanning -run 'TestSchedulerPreventsOverlapAndReclaimsStaleLease|TestSchedulerRespectsConfiguredInterval'` → pass; `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/regixtry -run 'TestServicePublishRemainsAvailableWhileScanRunsExist|TestServiceFailedScanDoesNotHidePublishedContent'` → pass |
| Runtime harness command/scenario and exact result | `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/regixtry -run 'TestServicePublishRemainsAvailableWhileScanRunsExist|TestServiceFailedScanDoesNotHidePublishedContent'` → pass; covers pending publish availability plus failed-scan manifest read/open availability against the real service + SQLite path |
| Rollback boundary | `internal/app/scanning/scheduler.go`, `cmd/regixtry/main.go`, `internal/app/scanning/scheduler_test.go`, `internal/app/regixtry/service_test.go` |

### Remediation Verification

- `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/scanning ./internal/app/regixtry ./cmd/regixtry` → pass
- `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go vet ./...` → pass
- `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...` → pass

### Remediation Status

- Remediation batch status: ready for re-verify
- Verify findings fixed in this batch: 3/3

## Remediation Batch: Final Setup Provenance Runtime Proof

### Findings Addressed

- [x] Added end-to-end `setup` runtime proof for explicit custom `--trivy-*` inputs.
- [x] Proved the persisted lifecycle provenance bytes keep the exact custom Trivy values emitted by the setup path.

### Remediation TDD Cycle Evidence

| Finding | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| Custom `setup --trivy-*` inputs persist exact lifecycle provenance values | `cmd/regixtry/main_test.go` | Integration | ✅ `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupInteractivePromptAppliesPromptedAddrBeforePortConflict|TestRunSetupRollsBackWhenProvenanceSaveFails'` | ✅ `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance` failed first with `unknown field saveProvenanceHook in struct literal of type stubBootstrapRunner` | ✅ `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance'` passed | ✅ Existing default-provenance setup proof stayed green while the new runtime case covered explicit enabled/scheduled/duration/cache/binary/concurrency inputs and persisted JSON bytes | ✅ Extended the setup test double with provenance hooks so the runtime test can persist and read back real lifecycle provenance bytes without changing product code |

### Remediation Work Unit Evidence

| Evidence | Exact result |
|---|---|
| Focused test command and exact result | `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance'` → pass |
| Runtime harness command/scenario and exact result | `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance'` → pass; executes `setup` with explicit `--trivy-enabled`, `--trivy-schedule-enabled`, `--trivy-interval`, `--trivy-timeout`, `--trivy-cache-dir`, `--trivy-binary-path`, and `--trivy-max-concurrency`, then reads back the saved lifecycle provenance JSON and asserts those exact values |
| Rollback boundary | `cmd/regixtry/main_test.go` |

### Remediation Verification

- `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry ./internal/infra/install/linux` → pass
- `GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...` → pass

### Remediation Status

- Remediation batch status: ready for re-verify
- Verify findings fixed in this batch: 1/1

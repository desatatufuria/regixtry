# Apply Progress: TUI Feature Manager

## Change

- Change: `tui-feature-manager`
- Mode: Strict TDD
- Delivery mode: single PR (`size:exception` accepted)
- Review boundary: backend contracts, Bubble Tea shell, docs, and verification updates in one slice

## Completed Tasks

- [x] 1.1 RED coverage for generic summaries, minimal pages, ordered Trivy sections, and declared actions
- [x] 1.2 RED coverage for feature pages, typed action routes, and unknown feature/action failures
- [x] 1.3 RED coverage for generic page decoding, minimal-page rendering, selection refresh, and backend-authoritative action help
- [x] 2.1 Added `FeaturePage`, `FeatureSection`, `FeatureField`, `FeatureRow`, `FeatureAction`, and `FeatureActionResult`
- [x] 2.2 Built backend feature pages with Trivy config/runtime/runs/vulnerability/repository-alert sections
- [x] 2.3 Routed `GET /admin/v1/features/{name}` to feature pages and `POST /admin/v1/features/{name}/actions/{actionID}` to typed actions
- [x] 2.4 Kept summary-list payloads stable while retiring fixed-detail TUI assumptions
- [x] 3.1 Updated the admin client to read feature pages and execute declared actions
- [x] 3.2 Updated Bubble Tea session/model state to store feature pages and refresh after action completion
- [x] 3.3 Rendered shared headers, field/row sections, and minimal pages in the generic shell
- [x] 3.4 Removed remaining Trivy-shaped help/status assumptions from the TUI shell
- [x] 4.1 Updated TUI and verification docs for the feature-manager flow
- [x] 4.2 Updated `tui-smoke.sh` to validate snapshot launch plus focused feature-manager behavior
- [x] 4.3 Ran `go test ./...` and captured the evidence below for verify handoff

## Files Changed

| File | Action | What changed |
| --- | --- | --- |
| `internal/ports/regixtry.go` | Modified | Added generic feature page/action DTOs alongside existing summary/runtime structs |
| `internal/app/regixtry/feature_registry.go` | Modified | Built backend-declared feature pages and typed feature action execution |
| `internal/app/regixtry/service_test.go` | Modified | Added RED/GREEN coverage for minimal pages and ordered Trivy sections/actions |
| `internal/protocol/http/admin_handlers.go` | Modified | Served feature pages and typed action routes |
| `internal/protocol/http/router_test.go` | Modified | Added integration coverage for feature pages and action routes |
| `internal/tui/admin_client.go` | Modified | Added feature page decoding and typed action execution |
| `internal/tui/admin_client_test.go` | Modified | Added client coverage for feature pages and action routes |
| `internal/tui/session.go` | Modified | Replaced fixed feature status storage with `FeaturePage` state |
| `internal/tui/model.go` | Modified | Switched the feature screen to backend-declared pages/actions |
| `internal/tui/model_test.go` | Modified | Added TUI coverage for generic pages, minimal pages, and backend-authored help |
| `internal/tui/admin_views.go` | Modified | Rendered page header/sections generically |
| `internal/tui/session_test.go` | Modified | Updated admin session reset expectations for feature-page state |
| `cmd/regixtry/main_test.go` | Modified | Serialized runtime-manager factory swaps to remove parallel test flakiness during `go test ./...` |
| `docs/tui.md` | Replaced | Documented the generic feature-manager shell in English |
| `docs/verification/phase-4-operator-console.md` | Replaced | Documented verification flow for feature pages/actions |
| `docs/verification/scripts/tui-smoke.sh` | Replaced | Added snapshot + focused feature-manager smoke verification |
| `openspec/changes/tui-feature-manager/tasks.md` | Modified | Marked apply tasks complete |

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1.1 | `internal/app/regixtry/service_test.go` | Unit | ✅ `go test ./internal/app/regixtry ./internal/protocol/http ./internal/tui` | ✅ `go test ./internal/app/regixtry ./internal/protocol/http ./internal/tui` failed with `undefined: buildFeaturePage`, `service.GetFeaturePage undefined`, and missing `ports.FeatureSection` / `ports.FeatureAction` | ✅ `go test -count=1 ./internal/app/regixtry ./internal/protocol/http` passed | ✅ minimal non-Trivy page + ordered Trivy page cases | ✅ page assembly extracted into pure helpers |
| 1.2 | `internal/protocol/http/router_test.go` | Integration | ✅ same baseline | ✅ route tests initially failed with `405` on `/actions/disable` and page body missing `summary` | ✅ `go test -count=1 ./internal/app/regixtry ./internal/protocol/http` passed | ✅ page payload + action route + unknown target cases | ✅ handler dispatch simplified around `/actions/` |
| 1.3 | `internal/tui/admin_client_test.go`, `internal/tui/model_test.go` | Unit / Integration | ✅ same baseline | ✅ TUI tests initially failed with missing `GetFeaturePage`, `ExecuteFeatureAction`, and `ports.FeaturePage` symbols | ✅ `go test -count=1 ./internal/tui ./internal/protocol/http` passed | ✅ generic page render + minimal page + selection refresh cases | ✅ feature help now derives from declared actions |
| 2.1 | `internal/app/regixtry/service_test.go` | Unit | ✅ same baseline | ✅ DTO tests referenced missing page/action structs first | ✅ focused backend tests passed | ✅ page header + section + action permutations | ✅ explicit DTOs kept the contract small |
| 2.2 | `internal/app/regixtry/service_test.go` | Unit | ✅ same baseline | ✅ service tests failed before page builders existed | ✅ focused backend tests passed | ✅ config/runtime/runs/vulnerability/repository-alert coverage | ✅ summary building reused helper logic |
| 2.3 | `internal/protocol/http/router_test.go` | Integration | ✅ same baseline | ✅ router tests failed before page/action routing existed | ✅ focused backend tests passed | ✅ GET page + POST action + validation failures | ✅ kept legacy status/config endpoints intact while adding page/action routes |
| 2.4 | `internal/protocol/http/router_test.go`, `internal/app/regixtry/service_test.go` | Integration / Unit | ✅ same baseline | ✅ fixed-detail assumptions were still exposed in RED expectations | ✅ focused backend tests passed | ✅ summary list remained lightweight while page route became rich | ✅ obsolete fixed-detail TUI path removed from feature screen flow |
| 3.1 | `internal/tui/admin_client_test.go` | Integration | ✅ same baseline | ✅ client tests failed until feature page and action methods existed | ✅ `go test -count=1 ./internal/tui ./internal/protocol/http` passed | ✅ decode page + execute action cases | ✅ kept legacy client methods separate from new shell path |
| 3.2 | `internal/tui/model_test.go`, `internal/tui/session_test.go` | Unit | ✅ same baseline | ✅ model tests failed until `FeaturePage` state replaced fixed status | ✅ focused TUI tests passed | ✅ action confirmation + refresh + selection change cases | ✅ state reset helpers now clear feature pages consistently |
| 3.3 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ✅ render assertions failed before generic header/section rendering existed | ✅ focused TUI tests passed | ✅ generic page + minimal page cases | ✅ rendering helpers now share field/row logic |
| 3.4 | `internal/tui/model_test.go` | Unit | ✅ same baseline | ✅ old help/status assumptions no longer matched RED expectations | ✅ focused TUI tests passed | ✅ help text differs per selected feature page | ✅ action-help logic now reads backend-declared actions only |
| 4.1 | `docs/tui.md`, `docs/verification/phase-4-operator-console.md` | Docs | ➖ docs update | ✅ docs were outdated and Spanish-only for this slice | ✅ docs rewritten in English | ✅ operator guide + verification guide both updated | ✅ kept docs aligned with backend-driven shell |
| 4.2 | `docs/verification/scripts/tui-smoke.sh` | Verification script | ➖ script update | ✅ old smoke script asserted obsolete auth-disabled copy | ✅ `docs/verification/scripts/tui-smoke.sh "/tmp/opencode/tui-feature-manager-smoke"` passed | ✅ snapshot launch + focused feature-manager checks | ✅ smoke script simplified to deterministic coverage |
| 4.3 | `cmd/regixtry/main_test.go` + suite | Suite | ✅ focused baseline plus package tests | ✅ `go test ./...` exposed parallel flakiness around `swapFeatureRuntimeManagerFactory` | ✅ `go test -count=1 ./...` passed after serializing test factory swaps | ✅ reran focused and full suite separately | ✅ test helper now guards global runtime-manager swaps with a mutex |

## Test Summary

- Total tests written/updated: 9 focused feature-manager scenarios plus task checkbox/assertion updates
- Total focused commands passing: 2 focused package commands, 1 full-suite command, 1 smoke command
- Layers used: Unit (`internal/app/regixtry`, `internal/tui`), Integration (`internal/protocol/http`, client route tests), Runtime smoke (`docs/verification/scripts/tui-smoke.sh`)
- Approval tests: None — this slice was behavior-changing, not a pure refactor
- Pure functions/helpers added: page assembly and action-help helpers in the feature-manager path

## Work Unit Evidence

| Work unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
| --- | --- | --- | --- |
| Unit 1 — DTOs, page assembly, API | `go test -count=1 ./internal/app/regixtry ./internal/protocol/http` → exit 0; 2 packages passed (`regixtry/internal/app/regixtry` 1.328s, `regixtry/internal/protocol/http` 1.237s) | `go run ./cmd/regixtry feature list ... && go run ./cmd/regixtry feature status trivy ...` → exit 0; list kept lightweight summary columns, status reported truthful uninstalled runtime details | `internal/ports`, `internal/app/regixtry`, `internal/protocol/http` |
| Unit 2 — Bubble Tea shell | `go test -count=1 ./internal/tui ./internal/protocol/http` → exit 0; 2 packages passed (`regixtry/internal/tui` 0.051s, `regixtry/internal/protocol/http` 1.441s) | `docs/verification/scripts/tui-smoke.sh "/tmp/opencode/tui-feature-manager-smoke"` → exit 0; snapshot saved and focused feature-manager tests passed | `internal/tui/*` |
| Unit 3 — Docs and final verification | `go test -count=1 ./...` → exit 0; 14 packages passed and `internal/domain/auth` reported `[no test files]` | `docs/verification/scripts/tui-smoke.sh "/tmp/opencode/tui-feature-manager-smoke"` → exit 0; verified snapshot launch plus feature-manager action/help coverage | `docs/`, `docs/verification/scripts/`, `openspec/changes/tui-feature-manager/tasks.md`, test-only stabilization in `cmd/regixtry/main_test.go` |

## Deviations from Design

None — implementation stayed within the thin-shell, backend-driven page/action contract.

## Issues Found

- `go test ./...` exposed pre-existing parallel test flakiness in `cmd/regixtry/main_test.go` because multiple tests swapped the global feature-runtime-manager factory concurrently. The helper is now serialized with a mutex so the required test runner stays trustworthy.

## Remaining Tasks

- [ ] Verification phase artifact (`verify-report.md`) still needs to be authored in `sdd-verify` using this evidence.

## Status

14/14 apply tasks complete. Ready for `sdd-verify`.

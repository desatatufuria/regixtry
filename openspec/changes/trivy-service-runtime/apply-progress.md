# Apply Progress: Trivy Service Runtime

## Implementation Progress

**Change**: `trivy-service-runtime`
**Mode**: Strict TDD
**Delivery**: single-pr with accepted `size:exception`

### Completed Tasks
- [x] 1.1 RED service runtime validation and external-service projection tests
- [x] 1.2 RED SQLite/bootstrap/upgrade bridge tests
- [x] 1.3 GREEN/REFACTOR service-first DTO, app, and store changes
- [x] 2.1 RED HTTP probe/scan runner tests
- [x] 2.2 RED scanner-facing target and readiness tests
- [x] 2.3 GREEN/REFACTOR HTTP-backed Trivy runtime implementation
- [x] 3.1 RED CLI/admin/TUI service-runtime tests
- [x] 3.2 GREEN/REFACTOR operator surface updates
- [x] 4.1 Docs updated for localhost-container vs remote-service runtime
- [x] 4.2 Focused verification loops plus `go test ./...`

### Files Changed
| File | Action | What was done |
|---|---|---|
| `internal/ports/regixtry.go` | Modified | Replaced Trivy runtime contract with service-oriented fields and runtime mode metadata while keeping internal legacy bridge fields hidden. |
| `internal/app/regixtry/feature_registry.go` | Replaced | Projected Trivy as a built-in feature with `external_service` runtime status and one-way legacy bridge behavior. |
| `internal/app/regixtry/service_scanning.go` | Replaced | Validated service settings, enforced `registry_reachable_url`, and built scanner-facing image targets. |
| `internal/infra/scanning/trivy/runner.go` | Replaced | Added HTTP `/healthz`, `/version`, and `/scan` transport with bearer auth and TLS options. |
| `internal/infra/metadata/sqlite/store.go` | Modified | Added additive service columns and legacy column bridging in `scan_settings`. |
| `cmd/regixtry/main.go` | Modified | Switched feature CLI output/configuration to service fields and tightened setup migration messaging. |
| `internal/protocol/http/admin_handlers.go` | Modified | Exposed service runtime fields and kept `auth_token` write-only. |
| `internal/tui/admin_client.go` | Modified | Sent service-oriented feature configuration payloads. |
| `internal/tui/admin_views.go` | Modified | Rendered service URL, registry-reachable URL, TLS, and runtime details. |
| `README.md`, `docs/installation.md`, `docs/api.md`, `docs/cli.md` | Modified | Documented remote/localhost scanner models, readiness probes, and the legacy bridge. |
| `openspec/changes/trivy-service-runtime/tasks.md` | Modified | Marked all apply tasks complete. |

### Work Unit Evidence
| Unit | Focused test command and exact result | Runtime harness command and exact result | Rollback boundary |
|---|---|---|---|
| 1 | `go test ./internal/app/regixtry ./internal/infra/metadata/sqlite ./internal/infra/install/linux` → `ok regixtry/internal/app/regixtry (cached)` / `ok regixtry/internal/infra/metadata/sqlite (cached)` / `ok regixtry/internal/infra/install/linux (cached)` | `go test ./cmd/regixtry -run 'TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState|TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok regixtry/cmd/regixtry 0.328s` | `internal/ports`, `internal/app/regixtry`, `internal/infra/metadata/sqlite`, `internal/infra/install/linux` |
| 2 | `go test ./internal/infra/scanning/trivy ./internal/app/regixtry` → `ok regixtry/internal/infra/scanning/trivy (cached)` / `ok regixtry/internal/app/regixtry (cached)` | `go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok regixtry/cmd/regixtry 0.210s` | `internal/infra/scanning/trivy`, `internal/app/regixtry/service_scanning.go`, `internal/app/regixtry/feature_registry.go` |
| 3 | `go test ./cmd/regixtry ./internal/protocol/http ./internal/tui` → `ok regixtry/cmd/regixtry (cached)` / `ok regixtry/internal/protocol/http (cached)` / `ok regixtry/internal/tui (cached)` | `go test ./...` → `ok regixtry/cmd/regixtry (cached)` / `ok regixtry/internal/app/auth (cached)` / `ok regixtry/internal/app/regixtry (cached)` / `ok regixtry/internal/app/scanning (cached)` / `ok regixtry/internal/domain/regixtry (cached)` / `ok regixtry/internal/infra/auth/postgres (cached)` / `ok regixtry/internal/infra/install/linux (cached)` / `ok regixtry/internal/infra/install/releases (cached)` / `ok regixtry/internal/infra/metadata/sqlite (cached)` / `ok regixtry/internal/infra/scanning/trivy (cached)` / `ok regixtry/internal/infra/storage/fsblob (cached)` / `ok regixtry/internal/ports (cached)` / `ok regixtry/internal/protocol/http (cached)` / `ok regixtry/internal/tui (cached)` | `cmd/regixtry`, `internal/protocol/http`, `internal/tui`, docs |

### TDD Cycle Evidence
| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 | `internal/app/regixtry/service_test.go` | Unit | `go test ./internal/app/regixtry` baseline passed | ✅ service URL, bridge, and runtime tests added first | ✅ `go test ./internal/app/regixtry` | ✅ valid service config + loopback rejection + legacy bridge cases | ✅ extracted service-target validation and runtime projection |
| 1.2 | `internal/infra/metadata/sqlite/store_test.go`, `internal/infra/install/linux/{bootstrap,upgrade}_test.go` | Integration | `go test ./internal/infra/metadata/sqlite ./internal/infra/install/linux` baseline passed | ✅ additive-column and bridge tests added first | ✅ `go test ./internal/infra/metadata/sqlite ./internal/infra/install/linux` | ✅ new-service row + legacy row + existing-config-wins cases | ✅ additive migration kept reversible |
| 1.3 | `internal/app/regixtry/service_test.go`, `internal/infra/metadata/sqlite/store_test.go` | Unit/Integration | Covered by phase baseline | ✅ failing contract/store tests existed before code | ✅ `go test ./internal/app/regixtry ./internal/infra/metadata/sqlite` | ✅ configure/read/status paths covered | ✅ DTO cleanup plus hidden bridge fields |
| 2.1 | `internal/infra/scanning/trivy/runner_test.go` | Integration | `go test ./internal/infra/scanning/trivy` baseline passed | ✅ probe/auth/TLS/scan tests added first | ✅ `go test ./internal/infra/scanning/trivy` | ✅ health failure, version failure, malformed JSON, TLS CA, insecure TLS | ✅ replaced exec runner with shared HTTP client helpers |
| 2.2 | `internal/app/regixtry/service_test.go` | Unit | Covered by phase baseline | ✅ target-building and readiness tests added first | ✅ `go test ./internal/app/regixtry` | ✅ registry override + loopback fallback rejection + degraded runtime | ✅ target validation centralized in service scanning |
| 2.3 | `internal/infra/scanning/trivy/runner_test.go`, `internal/app/regixtry/service_test.go` | Unit/Integration | Covered by phase baseline | ✅ failing runtime tests existed before code | ✅ `go test ./internal/infra/scanning/trivy ./internal/app/regixtry` | ✅ probes and scan execution exercised distinct paths | ✅ shared transport and runtime mode projection |
| 3.1 | `cmd/regixtry/main_test.go`, `internal/protocol/http/router_test.go` | Integration | `go test ./cmd/regixtry ./internal/protocol/http ./internal/tui` baseline passed | ✅ CLI/admin/TUI service-runtime assertions added first | ✅ `go test ./cmd/regixtry ./internal/protocol/http ./internal/tui` | ✅ configure/status, write-only token, bridge-warning, and projection cases | ✅ responses normalized around service fields |
| 3.2 | `cmd/regixtry/main_test.go`, `internal/protocol/http/router_test.go`, `internal/tui/model_test.go` | Integration | Covered by phase baseline | ✅ failing surface tests existed before code | ✅ `go test ./cmd/regixtry ./internal/protocol/http ./internal/tui` | ✅ CLI, HTTP, and TUI each exercise different output paths | ✅ removed binary-oriented output and secret echoing |
| 4.1 | Docs review against tests/spec | Docs | N/A | ✅ docs gaps identified after runtime contract changes | ✅ docs updated to match tested behavior | ➖ single documentation behavior set | ✅ examples aligned on `registry_reachable_url` |
| 4.2 | Full suite | Integration | Focused loops green first | ✅ verify command chosen before final run | ✅ `go test ./...` | ✅ package loops plus full-suite pass | ✅ final formatting and stale-output cleanup |

### Test Summary
- **Total tests written/updated**: `internal/app/regixtry/service_test.go`, `internal/infra/scanning/trivy/runner_test.go`, `internal/infra/metadata/sqlite/store_test.go`, `internal/infra/install/linux/{bootstrap,upgrade}_test.go`, `cmd/regixtry/main_test.go`, `internal/protocol/http/router_test.go`
- **Total tests passing**: `go test ./...` passed for every package in the repository.
- **Layers used**: Unit and integration.
- **Approval tests**: Existing upgrade/bootstrap behavior assertions preserved lifecycle semantics while the runtime contract changed.
- **Pure functions/helpers added**: URL/loopback validation and HTTP/TLS helper paths were factored out of command execution.

### Deviations from Design
- The design text used `registry_address`; the implementation follows the spec and user scope with `registry_reachable_url`.
- The HTTP execution path uses explicit `/scan` requests plus `/healthz` and `/version` probes instead of shelling out through a local Trivy client.

### Issues Found
- `probeTrivyRuntime` is a package-level seam, so tests that swap it cannot run in parallel safely.

### Remaining Tasks
- [ ] None in apply scope.

### Workload / PR Boundary
- Mode: `size:exception`
- Current work unit: full change
- Boundary: service contract + SQLite bridge + HTTP runner + CLI/admin/TUI/docs
- Estimated review budget impact: exceeds the normal 400-line budget, explicitly allowed for this run.

### Status
10/10 tasks complete. Ready for `sdd-verify`.

## Remediation Batch: verify finding — status probe wiring

### Scope
- Fix production `feature status trivy` to use the real Trivy HTTP `/healthz` + `/version` probes.
- Restore the intended `registry_reachable_url` fallback to a non-loopback registry public URL during status evaluation.

### Files Changed
| File | Action | What was done |
|---|---|---|
| `internal/app/regixtry/feature_registry.go` | Modified | Replaced the local status stub with service-level runtime probing that validates registry reachability before calling the Trivy runner. |
| `internal/app/regixtry/service_scanning.go` | Modified | Extracted shared scanner-reachable registry base resolution so status and scan execution use the same fallback rules. |
| `internal/app/regixtry/service_test.go` | Modified | Replaced seam-swapped status tests with real `httptest`-backed `/healthz` + `/version` probes and added a non-loopback public URL fallback case. |
| `cmd/regixtry/main.go` | Modified | Wired `feature` commands to a real Trivy runner and optional `-public-url` fallback input. |
| `cmd/regixtry/main_test.go` | Modified | Verified `feature status trivy` reports ready/version from a real HTTP probe path and honors public URL fallback. |

### Work Unit Evidence
| Evidence | Result |
|---|---|
| Focused safety-net command and exact result | `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./internal/app/regixtry -run 'TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails|TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured|TestServiceBuildsScannerFacingTargetAndRejectsLoopbackFallback'` → `ok regixtry/internal/app/regixtry 0.301s`; `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok regixtry/cmd/regixtry (cached)` |
| RED command and exact result | `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./internal/app/regixtry -run 'TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails|TestServiceGetFeatureStatusFallsBackToNonLoopbackPublicURL|TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured'` → `FAIL` with `status runtime ... Health:"unknown"` and `registry_reachable_url is required when the registry public URL is loopback-only`; `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` → `FAIL` with `Runtime Health: unknown`, wanted `Runtime Health: ready` |
| GREEN focused command and exact result | `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./internal/app/regixtry -run 'TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails|TestServiceGetFeatureStatusFallsBackToNonLoopbackPublicURL|TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured'` → `ok regixtry/internal/app/regixtry 0.341s`; `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok regixtry/cmd/regixtry 0.184s` |
| Broader proof and exact result | `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./internal/app/regixtry ./cmd/regixtry` → `ok regixtry/internal/app/regixtry 1.867s` / `ok regixtry/cmd/regixtry 3.591s`; `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./...` → full repository pass including `ok regixtry/cmd/regixtry 3.557s`, `ok regixtry/internal/app/regixtry 2.067s`, `ok regixtry/internal/protocol/http 1.630s` |
| Runtime harness command and exact result | `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok regixtry/cmd/regixtry 0.184s` against a real `httptest` Trivy service serving `/healthz` and `/version` |
| Rollback boundary | Revert `internal/app/regixtry/feature_registry.go`, `internal/app/regixtry/service_scanning.go`, `internal/app/regixtry/service_test.go`, `cmd/regixtry/main.go`, and `cmd/regixtry/main_test.go` to restore the pre-remediation status path without touching scan execution, persistence, or docs. |

### TDD Cycle Evidence
| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| R1 status probe remediation | `internal/app/regixtry/service_test.go`, `cmd/regixtry/main_test.go` | Integration | ✅ `go test ./internal/app/regixtry -run 'TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails|TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured|TestServiceBuildsScannerFacingTargetAndRejectsLoopbackFallback'` and `go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` were green before edits | ✅ Added real HTTP probe assertions and public URL fallback coverage before production changes; both focused commands failed | ✅ Focused reruns passed after wiring the real probe path and CLI runtime setup | ✅ Covered ready probe success, version failure degradation, explicit registry override, and non-loopback public URL fallback | ✅ Extracted shared registry-base resolution and isolated runtime probing into service helpers, then ran `gofmt -w` plus broader tests |

### Remediation Status
Verify finding addressed locally. Ready for `sdd-verify` rerun.

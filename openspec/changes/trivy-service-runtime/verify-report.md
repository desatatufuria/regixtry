```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:93f34f41310ded4590ca1ddb2981cc691609a01ec044aa573d89d9d863d039d7
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 4/4
scenarios: 9/9
test_command: GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./...
test_exit_code: 0
test_output_hash: sha256:bee2bcd44098283d58cd74fb45a0379d155dc1a6a65ffefbc8a4b49158c06371
build_command: GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./... -run "^$"
build_exit_code: 0
build_output_hash: sha256:7513df638ab5a8d6be11d7f1950d29bb91af13d25f6a370def685b7932d22ef6
```

## Verification Report

**Change**: trivy-service-runtime
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 10 |
| Tasks complete | 10 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./... -run "^$"
ok  	regixtry/cmd/regixtry	0.054s [no tests to run]
ok  	regixtry/internal/app/auth	(cached) [no tests to run]
ok  	regixtry/internal/app/regixtry	0.007s [no tests to run]
ok  	regixtry/internal/app/scanning	(cached) [no tests to run]
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	(cached) [no tests to run]
ok  	regixtry/internal/infra/auth/postgres	(cached) [no tests to run]
ok  	regixtry/internal/infra/install/linux	(cached) [no tests to run]
ok  	regixtry/internal/infra/install/releases	(cached) [no tests to run]
ok  	regixtry/internal/infra/metadata/sqlite	(cached) [no tests to run]
ok  	regixtry/internal/infra/scanning/trivy	(cached) [no tests to run]
ok  	regixtry/internal/infra/storage/fsblob	(cached) [no tests to run]
ok  	regixtry/internal/ports	(cached) [no tests to run]
ok  	regixtry/internal/protocol/http	0.007s [no tests to run]
ok  	regixtry/internal/tui	(cached) [no tests to run]
```

**Tests**: ✅ Passed
```text
GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./...
ok  	regixtry/cmd/regixtry	(cached)
ok  	regixtry/internal/app/auth	(cached)
ok  	regixtry/internal/app/regixtry	(cached)
ok  	regixtry/internal/app/scanning	(cached)
?   	regixtry/internal/domain/auth	[no test files]
ok  	regixtry/internal/domain/regixtry	(cached)
ok  	regixtry/internal/infra/auth/postgres	(cached)
ok  	regixtry/internal/infra/install/linux	(cached)
ok  	regixtry/internal/infra/install/releases	(cached)
ok  	regixtry/internal/infra/metadata/sqlite	(cached)
ok  	regixtry/internal/infra/scanning/trivy	(cached)
ok  	regixtry/internal/infra/storage/fsblob	(cached)
ok  	regixtry/internal/ports	(cached)
ok  	regixtry/internal/protocol/http	(cached)
ok  	regixtry/internal/tui	(cached)
```

**Focused verification loops**:
- `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./internal/app/regixtry -run "TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails|TestServiceGetFeatureStatusFallsBackToNonLoopbackPublicURL|TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured|TestServiceBuildsScannerFacingTargetAndRejectsLoopbackFallback"` → `ok  regixtry/internal/app/regixtry 0.412s`
- `GOCACHE="/tmp/opencode/gocache" GOMODCACHE="/tmp/opencode/gomodcache" go test ./cmd/regixtry -run "TestRunFeatureCommandsManageBuiltInTrivyState|TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState"` → `ok  regixtry/cmd/regixtry 0.264s`

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | `apply-progress.md` includes the original 10-row TDD table plus a remediation row for the status-probe fix. |
| All tasks have tests | ✅ | 10/10 planned tasks have runtime or docs evidence, and the remediation row is backed by executable focused tests. |
| RED confirmed (tests exist) | ✅ | Referenced test files exist: `service_test.go`, `runner_test.go`, `store_test.go`, `bootstrap_test.go`, `upgrade_test.go`, `main_test.go`, `router_test.go`, `model_test.go`, `admin_client_test.go`. |
| GREEN confirmed (tests pass) | ✅ | Focused remediation reruns pass, and the final `go test ./...` pass is still green. |
| Triangulation adequate | ✅ | App, runner, CLI, HTTP, store, bootstrap, upgrade, and TUI layers exercise distinct service-runtime paths. |
| Safety Net for modified files | ✅ | Every executable row reports a pre-green baseline/focused command before the corresponding green step. |

**TDD Compliance**: 6/6 checks passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit | 0 | 0 | Go test runner only |
| Integration | 177 | 9 | `go test` + `httptest` + real sqlite/filesystem/Bubble Tea harnesses |
| E2E | 0 | 0 | not installed / not used |
| **Total** | **177** | **9** | |

---

### Changed File Coverage
| File | Line % | Branch % | Uncovered Lines | Rating |
|------|--------|----------|-----------------|--------|
| `cmd/regixtry/main.go` | 79.8% | N/A | `runWithIO`, env parsing helpers, and some setup/logout helpers | ⚠️ Low |
| `internal/app/regixtry/feature_registry.go` | 81.1% | N/A | `ValidateFeatureName` and `SetFeatureEnabled` edge paths | ⚠️ Acceptable |
| `internal/app/regixtry/service_scanning.go` | 57.8% | N/A | scheduled paths (`RunScheduledScans`, `queueScheduledScan`) and some failure branches | ⚠️ Low |
| `internal/infra/install/linux/bootstrap.go` | 70.8% | N/A | artifact writer helpers and default-value helpers | ⚠️ Low |
| `internal/infra/install/linux/provenance.go` | 72.3% | N/A | exported wrappers and formatting paths | ⚠️ Low |
| `internal/infra/install/linux/templates.go` | 100.0% | N/A | — | ✅ Excellent |
| `internal/infra/metadata/sqlite/store.go` | 60.9% | N/A | active-run/lease helpers and some scan row helpers | ⚠️ Low |
| `internal/infra/scanning/trivy/runner.go` | 83.5% | N/A | TLS/client and cert-loading edge branches only | ⚠️ Acceptable |
| `internal/protocol/http/admin_handlers.go` | 65.6% | N/A | feature/admin negative branches and generic JSON decode edges | ⚠️ Low |
| `internal/tui/admin_client.go` | 70.3% | N/A | grant/token and API error decoding branches | ⚠️ Low |
| `internal/tui/admin_views.go` | 81.6% | N/A | create-user/token/toggle render helpers only | ⚠️ Acceptable |

**Average changed file coverage**: 74.9%

---

### Assertion Quality
**Assertion quality**: ✅ All assertions in the changed test files verify real behavior; no tautologies, ghost loops, or empty-behavior-only assertions were found.

---

### Quality Metrics
**Linter**: ➖ Not available in the provided verification capabilities
**Type Checker**: ➖ Not available in the provided verification capabilities

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| External Service Runtime Contract | Operator saves a valid service runtime | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails` | ✅ COMPLIANT |
| External Service Runtime Contract | Unsupported service URL is submitted | `internal/app/regixtry/service_test.go > TestServiceRejectsOutOfBoundsScanSettings` | ✅ COMPLIANT |
| Registry Reachability Across Deployment Models | Localhost container deployment provides scanner-facing registry address | `internal/app/regixtry/service_test.go > TestServiceBuildsScannerFacingTargetAndRejectsLoopbackFallback` | ✅ COMPLIANT |
| Registry Reachability Across Deployment Models | Remote service cannot rely on loopback registry address | `internal/app/regixtry/service_test.go > TestServiceBuildsScannerFacingTargetAndRejectsLoopbackFallback` | ✅ COMPLIANT |
| Service Probing, Auth, TLS, and Legacy Bridge | Service probes succeed with configured security settings | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusProjectsExternalServiceRuntimeDetails`; `internal/infra/scanning/trivy/runner_test.go > TestRunnerProbesServiceHealthAndVersionAndExecutesScan`; `cmd/regixtry/main_test.go > TestRunFeatureCommandsManageBuiltInTrivyState` | ✅ COMPLIANT |
| Service Probing, Auth, TLS, and Legacy Bridge | Legacy binary-backed settings are bridged once | `cmd/regixtry/main_test.go > TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeImportsLegacyTrivySettingsWhenFeatureStateMissing`; `internal/app/regixtry/service_test.go > TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Supported host receives runnable registry artifacts | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunReportsSuccessAfterActivationAndReadiness` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Unsupported environment is not overstated | `internal/infra/install/linux/bootstrap_test.go > TestDetectHost`; `internal/infra/install/linux/bootstrap_test.go > TestBootstrapRunRejectsUnsupportedHost` | ✅ COMPLIANT |
| Linux Bootstrap Artifacts | Legacy Trivy inputs are treated as a temporary bridge | `cmd/regixtry/main_test.go > TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeImportsLegacyTrivySettingsWhenFeatureStateMissing` | ✅ COMPLIANT |

**Compliance summary**: 9/9 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| External service DTOs replace the binary steady-state contract | ✅ Implemented | `internal/ports/regixtry.go:54-69,134-166` expose `service_url`, `registry_reachable_url`, TLS/auth fields, and external-service runtime metadata while keeping auth/legacy fields hidden from operator-facing JSON. |
| Registry reachability and status fallback are scanner-safe | ✅ Implemented | `internal/app/regixtry/service_scanning.go:192-200` uses `registry_reachable_url` first, falls back to `scanHost` only when non-loopback, and returns a validation error otherwise. |
| Runtime status uses the real Trivy HTTP probe path | ✅ Implemented | `internal/app/regixtry/feature_registry.go:60-88` now gates readiness through `scanRunner.(trivyRuntimeProber).Probe`, `internal/infra/scanning/trivy/runner.go:35-61` performs real `/healthz` and `/version` requests, and `cmd/regixtry/main.go:995-1006` wires feature commands to `trivyinfra.New(...)` plus `cfg.PublicURL`. |
| Lifecycle/setup keeps binary inputs as a temporary bridge only | ✅ Implemented | `internal/infra/metadata/sqlite/store.go:422-483`, `cmd/regixtry/main.go:1087-1112`, and `internal/infra/install/linux/upgrade_test.go:271-318` preserve shared legacy knobs without re-establishing host-binary ownership. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Service-first DTO shape and write-only auth token | ✅ Yes | `internal/protocol/http/admin_handlers.go:229-305` projects service fields and omits `auth_token` from reads/status while still accepting it on writes. |
| HTTP runner replaces local subprocess execution | ✅ Yes | `internal/infra/scanning/trivy/runner.go` performs all probe/scan behavior over HTTP and contains no local Trivy subprocess path. |
| Runtime status reuses the service probe transport | ✅ Yes | `feature_registry.go:70-88` delegates runtime status to the Trivy runner probe instead of a local stub. |
| Design naming | ⚠️ Deviation | The design document still says `registry_address`; the spec, code, and operator surfaces use `registry_reachable_url`. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- Changed-file coverage averages 74.9%, with the weakest areas in `service_scanning.go`, `store.go`, `admin_handlers.go`, `bootstrap.go`, and `admin_client.go`.
- The design artifact still uses `registry_address` in one decision/flow description while the implemented contract and spec use `registry_reachable_url`.

**SUGGESTION**:
- Add an admin-route integration test that asserts `/admin/v1/features/trivy/status` returns runtime `health`, `version`, and degraded fallback details from a real HTTP-backed runner, not just a `runtime` object shell.
- Add scheduler-path coverage for `RunScheduledScans` and `queueScheduledScan` so the scanner-safe registry fallback rules stay protected outside manual scans and feature status.

### Verdict
PASS WITH WARNINGS
The remediation fixed the production status path: runtime readiness now uses real `/healthz` + `/version` HTTP probes, non-loopback public URL fallback is honored, and all 4 requirements / 9 scenarios are covered by passing runtime tests.

```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:df11b9d01955cf42f22e0910b90b11a3681cf46767797e8043e20051d3723006
verdict: pass
blockers: 0
critical_findings: 0
requirements: 7/7
scenarios: 15/15
test_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...
test_exit_code: 0
test_output_hash: sha256:bee2bcd44098283d58cd74fb45a0379d155dc1a6a65ffefbc8a4b49158c06371
build_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -run '^$' ./...
build_exit_code: 0
build_output_hash: sha256:4af9acfee92bb927b202b253cd53302fb12a0821e3378ff53e85fbf2ee2c404f
```

## Verification Report

**Change**: optional-trivy-rescans  
**Version**: N/A  
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 13 |
| Tasks complete | 13 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -run '^$' ./...
ok  	regixtry/cmd/regixtry	0.030s [no tests to run]
ok  	regixtry/internal/app/auth	(cached) [no tests to run]
ok  	regixtry/internal/app/regixtry	(cached) [no tests to run]
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
ok  	regixtry/internal/protocol/http	(cached) [no tests to run]
ok  	regixtry/internal/tui	(cached) [no tests to run]
```

**Tests**: ✅ 14 packages passed / ❌ 0 failed / ⚠️ 1 package without tests
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...
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

**Focused remediation proof — setup provenance with explicit `--trivy-*` inputs**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance'
ok  	regixtry/cmd/regixtry	(cached)
```

**Focused remediation proof — scheduler cadence**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/scanning -run 'TestSchedulerPreventsOverlapAndReclaimsStaleLease|TestSchedulerRespectsConfiguredInterval'
ok  	regixtry/internal/app/scanning	(cached)
```

**Focused remediation proof — failed-scan published-content availability**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/regixtry -run 'TestServicePublishRemainsAvailableWhileScanRunsExist|TestServiceFailedScanDoesNotHidePublishedContent'
ok  	regixtry/internal/app/regixtry	(cached)
```

**Coverage**: changed-file average 72.6% / threshold: N/A → ⚠️ Review attention needed

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | `apply-progress.md` includes 13 task rows plus remediation rows for scheduler cadence, failed-scan publish availability, and final setup provenance proof. |
| All tasks have tests | ⚠️ | 12/13 task rows map to executable test files; task 4.3 is a documentation row backed by a full-suite rerun. |
| RED confirmed (tests exist) | ✅ | All referenced test files exist, including `cmd/regixtry/main_test.go` for the custom setup/provenance runtime proof. |
| GREEN confirmed (tests pass) | ✅ | Focused setup, scheduler, and failed-scan proofs, plus `go vet ./...`, `go test -run '^$' ./...`, and full `go test ./...`, all pass today. |
| Triangulation adequate | ✅ | Setup default/custom provenance, scheduler overlap/cadence, and publish pending/failed-scan availability each execute as separate runtime cases. |
| Safety Net for modified files | ✅ | Modified-file rows declare focused baselines; new packages are correctly marked `N/A (new)`. |

**TDD Compliance**: 5/6 checks passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit | 28 | 4 | `go test` |
| Integration | 108 | 4 | `go test` |
| E2E | 0 | 0 | not installed |
| **Total** | **136** | **8** | |

---

### Changed File Coverage
| File | Line % | Branch % | Uncovered Lines | Rating |
|------|--------|----------|-----------------|--------|
| `cmd/regixtry/main.go` | 80.5% | — | L35-37, L48-50, L57-60, L74-81, L83, ... | ⚠️ Acceptable |
| `internal/app/regixtry/service.go` | 66.2% | — | L29-31, L33-35, L37-39, L56-58, L60-62, ... | ⚠️ Low |
| `internal/app/regixtry/service_scanning.go` | 53.3% | — | L19-24, L26-31, L44-47, L52-57, L59-61, ... | ⚠️ Low |
| `internal/app/scanning/scheduler.go` | 66.7% | — | L30-35, L40-42, L51-53, L62-64, L66-68, ... | ⚠️ Low |
| `internal/infra/install/linux/bootstrap.go` | 70.8% | — | L74-111, L124-126, L139-144, L146-154, L161-163, ... | ⚠️ Low |
| `internal/infra/install/linux/intent.go` | 86.2% | — | L50-55, L63-64, L68-69, L72-74, L77-79, ... | ⚠️ Acceptable |
| `internal/infra/install/linux/provenance.go` | 71.3% | — | L74-76, L80-82, L120-122, L124-126, L133-135, ... | ⚠️ Low |
| `internal/infra/install/linux/templates.go` | 100.0% | — | — | ✅ Excellent |
| `internal/infra/metadata/sqlite/store.go` | 60.6% | — | L22-24, L27-30, L40-42, L71-75, L79-81, ... | ⚠️ Low |
| `internal/infra/scanning/trivy/runner.go` | 76.9% | — | L31-33, L35-39, L48-53, L55-57, L89-92 | ⚠️ Acceptable |
| `internal/protocol/http/admin_handlers.go` | 66.4% | — | L18-21, L29-32, L43-44, L52-55, L59-62, ... | ⚠️ Low |

**Average changed file coverage**: 72.6%

---

### Assertion Quality
**Assertion quality**: ✅ All assertions verify real behavior

---

### Quality Metrics
**Linter**: ➖ Not available  
**Type Checker**: ➖ Not available  
**Vet**: ✅ Passed
```text
$ GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go vet ./...
```

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Optional Trivy Rescan Settings | Default settings stay disabled | `internal/infra/metadata/sqlite/store_test.go > TestStorePersistsDefaultDisabledScanSettings`; `internal/infra/install/linux/bootstrap_test.go > TestTemplateRendering` | ✅ COMPLIANT |
| Optional Trivy Rescan Settings | Unbounded settings are rejected | `internal/app/regixtry/service_test.go > TestServiceRejectsOutOfBoundsScanSettings`; `internal/protocol/http/router_test.go > TestRouterAdminScanRoutesRejectInvalidTargetsAndSettings` | ✅ COMPLIANT |
| Admin Settings And Manual Rescan API | Settings update becomes authoritative | `internal/protocol/http/router_test.go > TestRouterAdminScanSettingsRoutesRequireAuthAndPersistUpdates` | ✅ COMPLIANT |
| Admin Settings And Manual Rescan API | Tag-triggered rescan becomes digest-centric | `internal/app/regixtry/service_test.go > TestServiceQueuesDigestCentricManualScansAndDedupesActiveRuns`; `internal/protocol/http/router_test.go > TestRouterAdminScanRoutesQueueAndListRuns` | ✅ COMPLIANT |
| Admin Settings And Manual Rescan API | Unpublished targets are rejected | `internal/app/regixtry/service_test.go > TestServiceRejectsInvalidOrUnpublishedScanTargets`; `internal/protocol/http/router_test.go > TestRouterAdminScanRoutesRejectInvalidTargetsAndSettings` | ✅ COMPLIANT |
| Bounded Periodic Rescans | Active ownership prevents duplicate batches | `internal/app/scanning/scheduler_test.go > TestSchedulerPreventsOverlapAndReclaimsStaleLease` | ✅ COMPLIANT |
| Bounded Periodic Rescans | Stale scheduler state can be reclaimed | `internal/app/scanning/scheduler_test.go > TestSchedulerPreventsOverlapAndReclaimsStaleLease` | ✅ COMPLIANT |
| Persisted Run History And Freshness Evidence | Completed run keeps freshness evidence | `internal/infra/metadata/sqlite/store_test.go > TestStorePersistsScanRunsAndSchedulerStateAcrossReopen` | ✅ COMPLIANT |
| Persisted Run History And Freshness Evidence | Failed run remains visible | `internal/infra/metadata/sqlite/store_test.go > TestStorePersistsScanRunsAndSchedulerStateAcrossReopen` | ✅ COMPLIANT |
| Publish Availability Remains Non-Blocking | Publish succeeds while rescans are pending | `internal/app/regixtry/service_test.go > TestServicePublishRemainsAvailableWhileScanRunsExist` | ✅ COMPLIANT |
| Publish Availability Remains Non-Blocking | Scan failure does not hide published content | `internal/app/regixtry/service_test.go > TestServiceFailedScanDoesNotHidePublishedContent` | ✅ COMPLIANT |
| Managed Initial Rescan Configuration | Setup omits optional rescans | `internal/infra/install/linux/bootstrap_test.go > TestTemplateRendering`; `internal/infra/install/linux/bootstrap_test.go > TestBootstrapPlanEmitsLifecycleProvenance` | ✅ COMPLIANT |
| Managed Initial Rescan Configuration | Setup records optional rescan settings | `cmd/regixtry/main_test.go > TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance`; `internal/infra/install/linux/provenance_test.go > TestLoadInstalledIntentRecoversTrivyManagedSettings` | ✅ COMPLIANT |
| Rescan Configuration Provenance | Setup records managed rescan configuration | `cmd/regixtry/main_test.go > TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance`; `cmd/regixtry/main_test.go > TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance` | ✅ COMPLIANT |
| Rescan Configuration Provenance | Uninstall reports managed rescan cleanup truthfully | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully` | ✅ COMPLIANT |

**Compliance summary**: 15/15 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Optional Trivy Rescan Settings | ✅ Implemented | Scan settings are seeded once, normalized, persisted, and validated through `EnsureScanSettings`, `UpdateScanSettings`, and SQLite persistence. |
| Admin Settings And Manual Rescan API | ✅ Implemented | Admin settings and manual trigger/list routes are wired and resolve tags to digests before persisting runs. |
| Bounded Periodic Rescans | ✅ Implemented | `newHandler()` seeds `NewScheduler(..., settings.Interval, ...)` and `Scheduler.tick()` reuses the persisted `settings.Interval` before each wait cycle. |
| Persisted Run History And Freshness Evidence | ✅ Implemented | Completed and failed runs persist status, timestamps, counts, and DB freshness metadata in SQLite. |
| Publish Availability Remains Non-Blocking | ✅ Implemented | Publish/read paths remain independent from scan outcomes, and the failed-scan runtime proof covers `ResolveManifest` and `OpenManifest`. |
| Managed Initial Rescan Configuration | ✅ Implemented | The final setup remediation proves explicit `--trivy-*` inputs flow through `runSetup` and persist as managed lifecycle state. |
| Rescan Configuration Provenance | ✅ Implemented | Lifecycle provenance is written from the setup path and now has runtime proof for exact custom Trivy values. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| In-process bounded scheduler with one lease-owning loop and bounded workers | ✅ Yes | `internal/app/scanning/scheduler.go:60-80` derives each wait interval from current settings, and `cmd/regixtry/main.go:1736-1754` seeds the scheduler from persisted defaults. |
| Setup writes defaults; admin API writes authoritative mutable row | ✅ Yes | `EnsureScanSettings()` seeds the first row and `UpdateScanSettings()` remains the runtime authority. |
| Persistence via `scan_settings`, `scan_runs`, `scan_scheduler_state` | ✅ Yes | The additive SQLite schema and CRUD methods match the design. |
| Digest execution with fixed-argv Trivy runner | ✅ Yes | Manual scans resolve manifests to digests before persistence, and `runner.go` uses `exec.CommandContext` with explicit argv. |
| Backend-first slice, rich TUI deferred | ✅ Yes | The OpenSpec artifacts and docs still defer rich TUI management from slice 1. |

### Issues Found
**CRITICAL**:
- None.

**WARNING**:
- Changed-file coverage remains modest in several core files, especially `internal/app/regixtry/service_scanning.go` (53.3%), `internal/infra/metadata/sqlite/store.go` (60.6%), `internal/protocol/http/admin_handlers.go` (66.4%), and `internal/app/scanning/scheduler.go` (66.7%).
- Task 4.3 is documentation-only, so Strict TDD evidence for that row still relies on the full-suite rerun rather than a dedicated executable test file.

**SUGGESTION**:
- Add targeted execution coverage for `RunScheduledScans()` and SQLite lease methods to lift scheduler/store confidence without expanding slice scope.

### Canonical Verification Evidence
```text
focused_setup_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./cmd/regixtry -run 'TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance|TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance'
focused_setup_exit_code: 0
focused_setup_output_hash: sha256:3fa97365c3dd90e4c1fdd495175ad40b7ecfbae2400e1a7dd8023aa938f6b1a3
focused_scheduler_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/scanning -run 'TestSchedulerPreventsOverlapAndReclaimsStaleLease|TestSchedulerRespectsConfiguredInterval'
focused_scheduler_exit_code: 0
focused_scheduler_output_hash: sha256:a4429d13fb032fff81ca4d13cd2c2a2a4390d44121aac36d0862e4a2fbb6bd97
focused_service_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./internal/app/regixtry -run 'TestServicePublishRemainsAvailableWhileScanRunsExist|TestServiceFailedScanDoesNotHidePublishedContent'
focused_service_exit_code: 0
focused_service_output_hash: sha256:a46af814f97354ba9b203cabe72aacb8c999816c0c8a9d6acc095771a0dd2979
vet_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go vet ./...
vet_exit_code: 0
vet_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
build_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -run '^$' ./...
build_exit_code: 0
build_output_hash: sha256:4af9acfee92bb927b202b253cd53302fb12a0821e3378ff53e85fbf2ee2c404f
test_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...
test_exit_code: 0
test_output_hash: sha256:bee2bcd44098283d58cd74fb45a0379d155dc1a6a65ffefbc8a4b49158c06371
coverage_command: GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./... -coverprofile=/tmp/opencode/verify-optional-trivy-rescans-final/coverage.out
coverage_exit_code: 0
coverage_output_hash: sha256:b532e233d1caf41c579ef33010920d8ab5d0b94eafd18482f17d6497d3d012d7
coverage_profile_hash: sha256:798b7239043322328f79ce231afd14a177f541a068cb863e57e5ee2be16e08eb
```

### Verdict
PASS WITH WARNINGS
All 13 slice tasks are complete, the previously partial setup/provenance scenarios now have passing runtime proof, and the full strict verification suite passes; remaining concerns are non-blocking coverage depth and the documentation-only task row.

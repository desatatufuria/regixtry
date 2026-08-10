```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:8788dcc42c487df29a3789ec173712334d47b72a48a9b8bcac33adcfd503e9dd
verdict: fail
blockers: 1
critical_findings: 1
requirements: 6/7
scenarios: 12/13
test_command: go test ./...
test_exit_code: 0
test_output_hash: sha256:bee2bcd44098283d58cd74fb45a0379d155dc1a6a65ffefbc8a4b49158c06371
build_command: go test ./... -run '^$'
build_exit_code: 0
build_output_hash: sha256:a95209bbc24627f34d31e450fca23f1da448a850ef26f0e53e959cedaa975eaf
```

## Verification Report

**Change**: managed-trivy-runtime
**Version**: N/A
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 14 |
| Tasks complete | 14 |
| Tasks incomplete | 0 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go test ./... -run '^$'
ok   regixtry/cmd/regixtry                    0.039s [no tests to run]
ok   regixtry/internal/app/regixtry          0.007s [no tests to run]
ok   regixtry/internal/app/scanning          0.005s [no tests to run]
ok   regixtry/internal/infra/metadata/sqlite 0.006s [no tests to run]
ok   regixtry/internal/infra/scanning/trivy  0.007s [no tests to run]
ok   regixtry/internal/protocol/http         0.007s [no tests to run]
ok   regixtry/internal/tui                   0.036s [no tests to run]
build_output_hash=sha256:a95209bbc24627f34d31e450fca23f1da448a850ef26f0e53e959cedaa975eaf
```

**Tests**: ✅ Passed (`go test ./...`)
```text
$ go test ./...
ok   regixtry/cmd/regixtry
ok   regixtry/internal/app/regixtry
ok   regixtry/internal/app/scanning
ok   regixtry/internal/infra/metadata/sqlite
ok   regixtry/internal/infra/scanning/trivy
ok   regixtry/internal/protocol/http
ok   regixtry/internal/tui
test_output_hash=sha256:bee2bcd44098283d58cd74fb45a0379d155dc1a6a65ffefbc8a4b49158c06371
focused_output_hash=sha256:bee852bc1d3c9c0db14f9c6101d0feb7755fb89b1ee30cfede7cad018e37f19e
```

**Coverage**: project coverage collected with `go test ./... -coverprofile=...` → changed implementation files average **60.2%** (`coverprofile` hash `sha256:c2c5b3dfb9f1624e236ff0ef358efdef8cf20b2b4cbdbc5aa09fcc5cb6fa1526`, summary hash `sha256:3ac29287e6e0be4d3504900fff45d9114829b3ddddfec61a4242f9ec5baec3d3`).

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | `apply-progress.md` includes a `TDD Cycle Evidence` table. |
| All tasks have tests | ⚠️ | Evidence is grouped into 5 phase rows, not 14 task rows; implementation areas do have matching test files. |
| RED confirmed (tests exist) | ✅ | Verified focused test files exist for sqlite store, app service, scheduler, runtime manager, releases, runner, CLI, HTTP, and TUI. |
| GREEN confirmed (tests pass) | ✅ | Focused runs passed for persistence, migration, lifecycle, scan execution, scheduler, CLI, HTTP, and TUI status/install paths. |
| Triangulation adequate | ⚠️ | Runtime coverage spans unit and integration layers, but `apply-progress.md` omits the requested triangulation column and per-task counts. |
| Safety Net for modified files | ⚠️ | `apply-progress.md` omits the requested safety-net column, so pre-change regression proof is incomplete. |

**TDD Compliance**: 3/6 strict checks fully passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit | 6 | 3 | go test |
| Integration | 12 | 7 | go test |
| E2E | 0 | 0 | not installed |
| **Total** | **18** | **10** | |

---

### Changed File Coverage
| File | Line % | Branch % | Uncovered Lines | Rating |
|------|--------|----------|-----------------|--------|
| `cmd/regixtry/main.go` | 79.7% | N/A | `L35-L37`, `L48-L50`, `L65-L68`, `L82-L89`, `L131-L135`, `...` | ⚠️ Low |
| `internal/app/regixtry/feature_registry.go` | 79.3% | N/A | `L35-L38`, `L44-L46`, `L62-L64`, `L76-L78`, `L81-L85`, `...` | ⚠️ Low |
| `internal/app/regixtry/service_scanning.go` | 73.4% | N/A | `L19-L24`, `L26-L31`, `L44-L47`, `L52-L60`, `L62-L64`, `...` | ⚠️ Low |
| `internal/app/regixtry/feature_runtime.go` | 0.0% | N/A | `L10-L17`, `L20-L27`, `L30-L37` | ⚠️ Low |
| `internal/infra/metadata/sqlite/store.go` | 63.3% | N/A | `L22-L24`, `L27-L30`, `L40-L42`, `L71-L75`, `L79-L81`, `...` | ⚠️ Low |
| `internal/infra/scanning/trivy/runner.go` | 80.0% | N/A | `L27-L31`, `L37-L39`, `L51-L53`, `L55-L57`, `L62-L64`, `...` | ⚠️ Acceptable |
| `internal/infra/scanning/trivy/releases.go` | 39.5% | N/A | `L39-L41`, `L43-L79`, `L82-L85`, `L88-L90`, `L92-L94`, `...` | ⚠️ Low |
| `internal/infra/scanning/trivy/runtime_manager.go` | 50.8% | N/A | `L37-L39`, `L41-L43`, `L45-L46`, `L59-L96`, `L99-L116`, `...` | ⚠️ Low |
| `internal/protocol/http/admin_handlers.go` | 60.4% | N/A | `L18-L21`, `L29-L32`, `L47-L48`, `L53-L57`, `L59-L62`, `...` | ⚠️ Low |
| `internal/tui/admin_client.go` | 71.2% | N/A | `L52-L54`, `L62`, `L68-L70`, `L73-L81`, `L88-L90`, `...` | ⚠️ Low |
| `internal/tui/model.go` | 64.1% | N/A | `L21-L24`, `L300-L304`, `L319-L323`, `L328-L332`, `L336-L340`, `...` | ⚠️ Low |

**Average changed file coverage**: 60.2%

---

### Assertion Quality
**Assertion quality**: ✅ All audited change-related Go tests assert observable behavior; no tautologies, empty ghost loops, or assertion-free tests were found in the focused files.

---

### Quality Metrics
**Linter**: ➖ Not available
**Type Checker**: ➖ Not available as a separate tool; `go test ./... -run '^$'` compiled the project successfully.

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Private Runtime Ownership | Managed runtime is installed privately | `internal/infra/scanning/trivy/runtime_manager_test.go > TestRuntimeManagerInstallStagesActivationAndRetainsRollbackTarget` | ✅ COMPLIANT |
| Private Runtime Ownership | Operator intent remains separate from runtime state | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusSeparatesIntentFromManagedRuntimeState` | ✅ COMPLIANT |
| Verified Activation and Rollback Safety | Verified upgrade becomes active | `internal/infra/scanning/trivy/runtime_manager_test.go > TestRuntimeManagerInstallStagesActivationAndRetainsRollbackTarget` | ✅ COMPLIANT |
| Verified Activation and Rollback Safety | Verification or activation fails | `internal/infra/scanning/trivy/releases_test.go > TestVerifyArchiveChecksumRejectsMismatchesAndExtractsManagedBinary`; `internal/infra/scanning/trivy/runtime_manager_test.go > TestRuntimeManagerRestoresPreviousRuntimeWhenActivationProbeFails` | ✅ COMPLIANT |
| Runtime Lifecycle Status | Runtime status is ready | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusProjectsManagedRuntimeDetails` | ✅ COMPLIANT |
| Runtime Lifecycle Status | Legacy service-shaped state is present | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusReportsLegacyRuntimeMigrationRequired` | ✅ COMPLIANT |
| Managed Runtime Scan Execution | Manual scan uses the active runtime | `internal/app/regixtry/service_test.go > TestServiceManualAndScheduledScansUseManagedRuntimeStateAndIgnoreLegacyPaths`; `internal/infra/scanning/trivy/runner_test.go > TestRunnerProbesManagedVersionAndExecutesScan` | ✅ COMPLIANT |
| Managed Runtime Scan Execution | Scheduled batch uses the active runtime | `internal/app/scanning/scheduler_test.go > TestSchedulerTriggersManagedRuntimeBatchesThroughServiceContract`; `internal/app/regixtry/service_test.go > TestServiceManualAndScheduledScansUseManagedRuntimeStateAndIgnoreLegacyPaths` | ✅ COMPLIANT |
| Managed Trivy Runtime Visibility and Actions | Operator sees separated runtime state | `internal/protocol/http/admin_handlers_test.go > TestAdminFeatureStatusSeparatesIntentFromManagedRuntimeAndShowsMigrationTruth`; `internal/tui/model_test.go > TestModelFeatureViewShowsManagedRuntimeMigrationStateAndInstallAction` | ⚠️ PARTIAL |
| Managed Trivy Runtime Visibility and Actions | Legacy service model is superseded truthfully | `internal/protocol/http/admin_handlers_test.go > TestAdminFeatureStatusSeparatesIntentFromManagedRuntimeAndShowsMigrationTruth`; `internal/tui/model_test.go > TestModelFeatureViewShowsManagedRuntimeMigrationStateAndInstallAction` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Setup command is available | `cmd/regixtry/main_test.go > TestRunSetupPersistsCustomTrivyFlagsInLifecycleProvenance` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Feature runtime lifecycle command is requested | `cmd/regixtry/main_test.go > TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions` | ✅ COMPLIANT |
| Truthful Trivy Runtime Status Command | Feature status reports separated state | `cmd/regixtry/main_test.go > TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions`; `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusSeparatesIntentFromManagedRuntimeState` | ✅ COMPLIANT |

**Compliance summary**: 12/13 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Private Runtime Ownership | ✅ Implemented | `trivy_runtime_state` persists managed runtime identity separately; legacy scan settings only derive migration evidence. |
| Verified Activation and Rollback Safety | ✅ Implemented | `runtime_manager.go` stages downloads, verifies checksums, swaps the active symlink atomically, and restores the previous version on failed activation probe. |
| Runtime Lifecycle Status | ✅ Implemented | Runtime status exposes version, receipt path, verification timestamps, health checks, rollback availability, and migration/degraded detail. |
| Managed Runtime Scan Execution | ✅ Implemented | `service_scanning.go` resolves the active managed runtime and injects only managed `BinaryPath` and `CacheDir` into scans. |
| Managed Trivy Runtime Visibility and Actions | ❌ Incomplete | Admin API and CLI expose install/upgrade/rollback/status, but the TUI only wires install (`internal/tui/model.go:884-891`) and the feature help omits upgrade/rollback actions (`internal/tui/admin_views.go:38-40`). |
| Binary Lifecycle Entrypoints | ✅ Implemented | `runFeature` wires `feature install|upgrade|rollback|status trivy` without reassigning `setup` ownership. |
| Truthful Trivy Runtime Status Command | ✅ Implemented | CLI status output renders runtime status/health/version/detail separately from feature intent. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Managed layout under `${storageRoot}/features/trivy/...` | ✅ Yes | Runtime manager writes versions, downloads, receipts, cache, and active symlink under the managed root. |
| Persistence split between `scan_settings` and `trivy_runtime_state` | ✅ Yes | `feature_registry.go` keeps feature intent in `scan_settings`; runtime state comes from `trivy_runtime_state`. |
| Verify official release evidence before activation | ✅ Yes | `releases.go` enforces checksum verification before extraction/activation. |
| Restore previous pointer on failed activation | ✅ Yes | `runtime_manager.go` re-links the previous version when probe fails before activation completion. |
| Legacy service/binary settings become migration evidence only | ✅ Yes | `deriveLegacyTrivyRuntimeState()` reports `migration-required` and refuses to treat legacy paths as executable authority. |
| Expose runtime actions/status through admin and TUI feature surfaces | ⚠️ Partial | Admin API and CLI satisfy the design; TUI status is present but upgrade/rollback actions are not wired. |

### Issues Found
**CRITICAL**:
- TUI runtime actions are incomplete. `internal/tui/model.go:884-891` wires only the `i` install action, and `internal/tui/admin_views.go:38-40` exposes no upgrade or rollback interaction. This violates `openspec/changes/managed-trivy-runtime/specs/operator-admin-tui/spec.md:7` and the design expectation in `design.md:44`.

**WARNING**:
- Strict TDD evidence is incomplete at the artifact level: `apply-progress.md` groups TDD evidence by 5 phases instead of 14 task rows and omits the requested triangulation and safety-net columns.
- Changed-file coverage is low for several core implementation files, especially `internal/app/regixtry/feature_runtime.go` (0.0%), `internal/infra/scanning/trivy/releases.go` (39.5%), and `internal/infra/scanning/trivy/runtime_manager.go` (50.8%).

**SUGGESTION**:
- After adding TUI upgrade and rollback actions, add explicit TUI tests for those actions and an admin-client route test for `:upgrade` so the operator surface is verified end to end.

### Verdict
FAIL
Backend, CLI, admin API, persistence, migration, and managed-binary scan execution all verified successfully, but the change is not complete because the TUI does not expose the required managed runtime upgrade/rollback actions.

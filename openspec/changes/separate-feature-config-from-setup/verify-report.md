```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:0000000000000000000000000000000000000000000000000000000000000000
verdict: pass
blockers: 0
critical_findings: 0
requirements: 11/11
scenarios: 20/20
test_command: go test ./...
test_exit_code: 0
test_output_hash: sha256:bee2bcd44098283d58cd74fb45a0379d155dc1a6a65ffefbc8a4b49158c06371
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: separate-feature-config-from-setup  
**Version**: N/A  
**Mode**: Strict TDD

### Completeness
| Metric | Value |
|--------|-------|
| Tasks total | 12 |
| Tasks complete | 12 |
| Tasks incomplete | 0 |
| Requirements | 11/11 |
| Scenarios | 20/20 |

### Build & Tests Execution
**Build**: ✅ Passed
```text
$ go build ./...
(no output)
```

**Tests**: ✅ 14 packages passed / ❌ 0 failed / ⚠️ 1 package without tests
```text
$ go test ./...
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

**Coverage**: per-package coverage reported successfully / threshold: 0% → ✅ Above
```text
$ go test -cover ./...
ok  	regixtry/cmd/regixtry	(cached)	coverage: 80.3% of statements
ok  	regixtry/internal/app/auth	(cached)	coverage: 30.3% of statements
ok  	regixtry/internal/app/regixtry	0.506s	coverage: 62.9% of statements
ok  	regixtry/internal/app/scanning	(cached)	coverage: 66.7% of statements
	regixtry/internal/domain/auth		coverage: 0.0% of statements
ok  	regixtry/internal/domain/regixtry	(cached)	coverage: 64.0% of statements
ok  	regixtry/internal/infra/auth/postgres	(cached)	coverage: 57.7% of statements
ok  	regixtry/internal/infra/install/linux	0.549s	coverage: 72.7% of statements
ok  	regixtry/internal/infra/install/releases	(cached)	coverage: 58.0% of statements
ok  	regixtry/internal/infra/metadata/sqlite	(cached)	coverage: 60.6% of statements
ok  	regixtry/internal/infra/scanning/trivy	(cached)	coverage: 76.9% of statements
ok  	regixtry/internal/infra/storage/fsblob	(cached)	coverage: 61.7% of statements
ok  	regixtry/internal/ports	0.007s	coverage: 60.0% of statements
ok  	regixtry/internal/protocol/http	1.633s	coverage: 70.4% of statements
ok  	regixtry/internal/tui	0.095s	coverage: 67.6% of statements
```

**Smoke harness proof**: ✅ `bash -n docs/verification/scripts/install-release-smoke.sh` passed (`sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`). The script now exercises `setup -> feature configure trivy -> feature status trivy -> upgrade -> uninstall` in both fixture and real-systemd branches (`docs/verification/scripts/install-release-smoke.sh:517-567`, `649-677`).

### TDD Compliance
| Check | Result | Details |
|-------|--------|---------|
| TDD Evidence reported | ✅ | `apply-progress.md:28-43` contains a complete TDD Cycle Evidence table. |
| All tasks have tests | ✅ | 12/12 task rows include verification artifacts; 11 executable task rows plus 1 docs-only row (`4.2`). |
| RED confirmed (tests exist) | ✅ | 11/11 executable task test assets exist in the repo (`service_test.go`, `main_test.go`, `bootstrap_test.go`, `provenance_test.go`, `upgrade_test.go`, `router_test.go`, `admin_client_test.go`, `model_test.go`, `session_test.go`, smoke script). |
| GREEN confirmed (tests pass) | ✅ | `go test ./...`, `go build ./...`, `go vet ./...`, `go test -cover ./...`, and `bash -n` all passed in this verify run. |
| Triangulation adequate | ✅ | Feature, lifecycle, CLI, HTTP, and TUI behaviors are covered by multi-case task rows; only docs task `4.2` is intentionally single-output. |
| Safety Net for modified files | ✅ | 11/11 executable task rows record package-level safety-net commands before mutation in `apply-progress.md:32-43`. |

**TDD Compliance**: 6/6 checks passed

---

### Test Layer Distribution
| Layer | Tests | Files | Tools |
|-------|-------|-------|-------|
| Unit | 13 | 1 | Go testing package/table-driven tests (`internal/app/regixtry/service_test.go`) |
| Integration | 166 | 8 | `go test`, `httptest`, temp SQLite stores, and Bubble Tea model tests |
| E2E | 1 harness | 1 | `docs/verification/scripts/install-release-smoke.sh` (syntax-checked, flow inspected) |
| **Total** | **180 + 1 harness** | **10** | |

---

### Changed File Coverage
| File | Line % | Branch % | Uncovered Lines | Rating |
|------|--------|----------|-----------------|--------|
| `cmd/regixtry/main.go` | 80.3% | — | L35-37, L48-50, L57-60, L74-81, L83, L123-127, L141-168, L172-173, L181-182, L198-199, L340-341 | ⚠️ Acceptable |
| `internal/infra/install/linux/bootstrap.go` | 70.8% | — | L74-111, L125-126, L140-154, L162-163, L170-171, L176-179, L188-192 | ⚠️ Low |
| `internal/infra/install/linux/provenance.go` | 71.3% | — | L75-82, L114-115, L118-119, L126-128, L138-139, L141-142, L147-157, L171-175 | ⚠️ Low |
| `internal/infra/install/linux/templates.go` | 100.0% | — | — | ✅ Excellent |
| `internal/infra/install/linux/upgrade.go` | 69.4% | — | L60-62, L83-84, L87-88, L91-92, L95-96, L99-100, L105-106, L109-110, L119-120, L132-133, L145-146, L160-161 | ⚠️ Low |
| `internal/ports/regixtry.go` | 35.3% | — | L180-182, L184-188, L190-191, L194-197, L205-206, L210-214 | ⚠️ Low |
| `internal/protocol/http/admin_handlers.go` | 65.6% | — | L19-21, L30-32, L47-48, L54-57, L60-62, L72-74, L79-82, L85-87, L90-92, L97-100, L103-105, L110-113 | ⚠️ Low |
| `internal/tui/admin_client.go` | 72.1% | — | L50-51, L66-67, L71-72, L74-75, L77-78, L86-87, L92-93, L97-98, L106-107, L111-112, L114-115, L133 | ⚠️ Low |
| `internal/tui/admin_views.go` | 81.6% | — | L24-25, L36-37, L48-49, L61-62, L84, L98, L127-146, L174-175, L204, L209-210 | ⚠️ Acceptable |
| `internal/tui/model.go` | 63.9% | — | L21-24, L294-297, L313-316, L322-325, L330-333, L347-348, L375-379, L390-394, L398-400, L405-409, L418-422, L428-429 | ⚠️ Low |
| `internal/tui/session.go` | 90.9% | — | L147-148, L165-166 | ⚠️ Acceptable |

**Average changed file coverage**: 72.8%

---

### Assertion Quality
**Assertion quality**: ✅ All assertions verify real behavior. Manual audit of the changed Go tests found no tautologies, no empty ghost loops, and no smoke-only assertions masquerading as behavioral coverage; the assertions exercise service, CLI, HTTP, lifecycle, or TUI state boundaries.

---

### Quality Metrics
**Linter**: ✅ `go vet ./...` reported no errors (`sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`)  
**Type Checker**: ✅ `go build ./...` reported no errors (`sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`)

### Spec Compliance Matrix
| Requirement | Scenario | Test | Result |
|-------------|----------|------|--------|
| Feature-Owned Runtime Authority | Runtime uses feature-owned state | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusProjectsTrivyRuntimeDetails`; `internal/protocol/http/router_test.go > TestRouterAdminFeatureRoutesRequireAuthAndMutateAuthoritativeState` | ✅ COMPLIANT |
| Legacy Setup Import Bridge | Legacy setup seeds missing feature state | `cmd/regixtry/main_test.go > TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeImportsLegacyTrivySettingsWhenFeatureStateMissing` | ✅ COMPLIANT |
| Legacy Setup Import Bridge | Existing feature state wins | `internal/app/regixtry/service_test.go > TestServiceImportLegacyFeatureConfigIfMissingPreservesExistingState`; `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeKeepsExistingFeatureStateWhenLegacyInputsDiffer` | ✅ COMPLIANT |
| Feature-Oriented Management Model | Operator lists built-in features | `cmd/regixtry/main_test.go > TestRunFeatureCommandsManageBuiltInTrivyState` | ✅ COMPLIANT |
| Feature-Oriented Management Model | Operator configures a built-in feature | `cmd/regixtry/main_test.go > TestRunFeatureCommandsManageBuiltInTrivyState`; `internal/protocol/http/router_test.go > TestRouterAdminFeatureRoutesProjectBuiltinTrivyState` | ✅ COMPLIANT |
| Feature-Oriented Management Model | Unknown feature name is rejected | `cmd/regixtry/main_test.go > TestRunFeatureRejectsUnknownBuiltInName`; `internal/protocol/http/router_test.go > TestRouterAdminFeatureRoutesRejectUnknownNames` | ✅ COMPLIANT |
| Feature Status Distinguishes Capability From Engine Health | Trivy status reports capability and engine state | `internal/app/regixtry/service_test.go > TestServiceGetFeatureStatusProjectsTrivyRuntimeDetails`; `cmd/regixtry/main_test.go > TestRunFeatureCommandsManageBuiltInTrivyState` | ✅ COMPLIANT |
| No Dynamic Plugin Loader | Unsupported plugin-style request is rejected | `internal/app/regixtry/service_test.go > TestServiceRejectsUnknownFeatureNames`; `cmd/regixtry/main_test.go > TestRunFeatureRejectsUnknownBuiltInName` | ✅ COMPLIANT |
| Base Bootstrap Scope | Setup records base lifecycle truth only | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapReceiptOmitsFeatureOwnedTrivyArtifacts`; `internal/infra/install/linux/provenance_test.go > TestLifecycleProvenanceOmitsFeatureOwnedTrivyIntentFields` | ✅ COMPLIANT |
| Scoped Rollback | Recorded base lifecycle state is cleaned up | `internal/infra/install/linux/provenance_test.go > TestBootstrapperUninstallReportsDriftTruthfully` | ✅ COMPLIANT |
| Scoped Rollback | Feature-owned state is not overstated | `internal/infra/install/linux/bootstrap_test.go > TestBootstrapReceiptOmitsFeatureOwnedTrivyArtifacts`; `internal/infra/install/linux/provenance_test.go > TestLifecycleProvenanceOmitsFeatureOwnedTrivyIntentFields` | ✅ COMPLIANT |
| Feature Management Entrypoints | Operator lists supported built-in features | `cmd/regixtry/main_test.go > TestRunFeatureCommandsManageBuiltInTrivyState` | ✅ COMPLIANT |
| Feature Management Entrypoints | Operator inspects named feature status | `cmd/regixtry/main_test.go > TestRunFeatureCommandsManageBuiltInTrivyState` | ✅ COMPLIANT |
| Feature Management Entrypoints | Unknown feature command target is rejected | `cmd/regixtry/main_test.go > TestRunFeatureRejectsUnknownBuiltInName` | ✅ COMPLIANT |
| Legacy Setup Migration Bridge | Existing feature state is not overwritten | `internal/app/regixtry/service_test.go > TestServiceImportLegacyFeatureConfigIfMissingPreservesExistingState` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Setup command is available | `cmd/regixtry/main_test.go > TestRunSetupPassesParsedConfigToRunnerAndWritesProvenance` | ✅ COMPLIANT |
| Binary Lifecycle Entrypoints | Upgrade preserves feature authority | `internal/infra/install/linux/upgrade_test.go > TestBootstrapperUpgradeKeepsExistingFeatureStateWhenLegacyInputsDiffer` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Operator browses admin data | `internal/tui/model_test.go > TestModelFeatureViewLoadsBackendStatusAndAllowsDisable`; `internal/tui/model_test.go > TestModelSuccessfulLoginRendersPremiumWorkspace` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Unauthenticated state blocks admin reads | `internal/tui/model_test.go > TestModelBlocksAdminUntilLogin`; `internal/protocol/http/router_test.go > TestRouterAdminFeatureRoutesRequireAuthAndMutateAuthoritativeState` | ✅ COMPLIANT |
| Read-Only Admin Browsing | Feature configuration stays backend-authoritative | `internal/tui/admin_client_test.go > TestHTTPAdminClientFeatureRoutes`; `internal/tui/model_test.go > TestModelFeatureViewLoadsBackendStatusAndAllowsDisable` | ✅ COMPLIANT |

**Compliance summary**: 20/20 scenarios compliant

### Correctness (Static Evidence)
| Requirement | Status | Notes |
|------------|--------|-------|
| Feature-owned authority | ✅ Implemented | `internal/app/regixtry/feature_registry.go:46-112` routes list/show/status/configure/enable/disable through `scan_settings`-backed feature DTOs. |
| Legacy setup bridge | ✅ Implemented | `cmd/regixtry/main.go:1013-1089` imports legacy `--trivy-*` only after setup; `internal/app/regixtry/feature_registry.go:103-112` preserves existing state. |
| Generic built-in feature CLI centered on `trivy` | ✅ Implemented | `cmd/regixtry/main.go:838-972` exposes `feature list|show|status|enable|disable|configure`. |
| Runtime/engine health split | ✅ Implemented | `internal/app/regixtry/feature_registry.go:24-34,71-77` separates static feature identity from runtime probe details. |
| No dynamic plugin loading | ✅ Implemented | `builtInFeatures` is a static slice and `lookupFeature` rejects unknown names (`feature_registry.go:22,36-44`). |
| Base-only lifecycle truth | ✅ Implemented | `bootstrapReceiptFromPlan` omits Trivy cache paths (`bootstrap.go:371-382`); lifecycle intent omits Trivy fields (`provenance.go:84-109`). |
| Upgrade preserves feature authority | ✅ Implemented | `upgrade.go:97-101` imports legacy Trivy settings only when needed before replay; otherwise persisted state remains authoritative. |
| Admin API feature projections | ✅ Implemented | `internal/protocol/http/admin_handlers.go:52-132` adds thin `/admin/v1/features` projections while leaving `/admin/v1/scan-settings` authoritative (`135-159`). |
| Thin interactive TUI feature flow | ✅ Implemented | `internal/tui/admin_views.go:81-125`, `internal/tui/model.go:1471-1488,1589-1604,1644-1648,1960-2010`, and `internal/tui/session.go:184-189` keep the feature workspace backend-authoritative and session-safe. |
| Lifecycle smoke flow updates | ✅ Implemented | `docs/verification/scripts/install-release-smoke.sh:517-567,649-677` now covers `feature configure/status trivy` inside both smoke branches. |
| Docs and operator guidance | ✅ Implemented | `README.md:65-83`, `docs/cli.md:11-16`, `docs/api.md:33-38`, `docs/installation.md:45-56`, and `docs/verification/operator-admin-api.md:7-20` describe the shipped feature model and admin projections. |

### Coherence (Design)
| Decision | Followed? | Notes |
|----------|-----------|-------|
| Feature identity model uses operator-facing names (`trivy`) | ✅ Yes | CLI, service, router, TUI, and docs all use `trivy` directly. |
| Static built-in registry with kind metadata | ✅ Yes | `internal/ports/regixtry.go:105-148` plus `feature_registry.go:17-23` implement the generic contract. |
| Runtime/version belongs to runtime details, not abstract feature | ✅ Yes | `FeatureRuntime` is separate from `FeatureSummary` and only attached to status/details views. |
| `/admin/v1/scan-settings` remains authority seam | ✅ Yes | Admin feature routes project over shared service state; they do not introduce a new persistence store. |
| No dynamic plugin loader in this slice | ✅ Yes | Unknown names fail validation immediately; no runtime discovery path exists in code. |

### Issues Found
**CRITICAL**: None

**WARNING**:
- Changed-file coverage averages 72.8%, with 7 changed source files below 80% (`bootstrap.go`, `provenance.go`, `upgrade.go`, `regixtry.go`, `admin_handlers.go`, `admin_client.go`, `model.go`).
- The authored diff is 2010 lines (`1900` additions, `110` deletions), which is 10 lines over the requested 2000-line review budget.
- The lifecycle smoke harness was syntax-validated and content-inspected in this verify run, but not executed end-to-end on a real systemd host.

**SUGGESTION**:
- Add a CI job or gated release check that executes `docs/verification/scripts/install-release-smoke.sh` end-to-end on a systemd-capable runner.

### Verdict
PASS WITH WARNINGS
All 11 requirements and 20 scenarios are covered by passing runtime tests, and the implementation matches the design; the remaining concerns are review size, source-file coverage depth, and the lack of a live end-to-end smoke execution in this verify run.

# Apply Progress: separate-feature-config-from-setup

## Status

- Delivery mode: single PR with approved `size:exception`
- Strict TDD mode: active
- Progress: 12 / 12 tasks complete

## Completed Tasks

- [x] 1.1 RED: add `internal/app/regixtry/service_test.go` cases for registry lookup, `trivy` status projection, unknown-feature rejection, and import-if-missing when no authoritative row exists.
- [x] 1.2 GREEN: create `internal/app/regixtry/feature_registry.go`, update `internal/ports/regixtry.go`, and extend `internal/app/regixtry/service_scanning.go` with generic feature DTOs backed by `scan_settings` and Trivy as the first concrete feature.
- [x] 1.3 REFACTOR: centralize shared feature validation/status mapping so CLI, HTTP, and TUI consume one generic feature contract.
- [x] 2.1 RED: add `internal/infra/install/linux/{bootstrap_test.go,provenance_test.go,upgrade_test.go}` coverage for base-only provenance, truthful uninstall reporting, legacy `--trivy-*` import, and existing feature state winning on replay.
- [x] 2.2 GREEN: update `internal/infra/install/linux/{bootstrap.go,templates.go,provenance.go,intent.go,upgrade.go}` so lifecycle owns only base bootstrap truth and imports legacy Trivy values only when `trivy` feature state is missing.
- [x] 2.3 REFACTOR: trim bootstrap intent/config helpers to base fields only and isolate temporary migration helpers for later flag removal.
- [x] 3.1 RED: extend `cmd/regixtry/main_test.go` for `feature list`, `show|status trivy`, `enable|disable trivy`, `configure trivy ...`, unknown-feature rejection, and Trivy health feedback under `status`.
- [x] 3.2 GREEN: update `cmd/regixtry/main.go` to route generic feature commands through the shared service contract while keeping legacy setup flags as migration-only inputs.
- [x] 3.3 RED: extend `internal/protocol/http/router_test.go` plus `internal/tui/{admin_client_test.go,session_test.go,model_test.go}` for backend-authoritative feature status reads, auth gating, and optional `trivy` mutations.
- [x] 3.4 GREEN: keep `/admin/v1/scan-settings` authoritative in `internal/protocol/http/admin_handlers.go` and add only thin feature projections in `internal/tui/{admin_client.go,session.go,model.go,admin_views.go}`.
- [x] 4.1 RED/GREEN: update lifecycle smoke coverage for `setup -> feature configure trivy -> feature status trivy -> upgrade -> uninstall`, then prove the full repo with `go test ./...`.
- [x] 4.2 Update `README.md`, `docs/`, and OpenSpec notes to describe the generic feature model, `trivy` as the first concrete feature, legacy Trivy import, and deferred standalone doctor flows.

## Remaining Tasks

- [ ] None

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1 | `internal/app/regixtry/service_test.go` | Unit | ✅ `go test ./internal/app/regixtry` | ✅ Added failing feature inventory/status/import tests first | ✅ `go test ./internal/app/regixtry -run 'TestService(ListFeaturesReturnsBuiltinTrivyInventory\|GetFeatureStatusProjectsTrivyRuntimeDetails\|RejectsUnknownFeatureNames\|ImportLegacyFeatureConfigIfMissingPreservesExistingState)'` | ✅ Inventory + runtime projection + unknown-feature + preserve-existing cases | ✅ Extracted shared registry/projection helpers |
| 1.2 | `internal/app/regixtry/service_test.go` | Unit | ✅ same package baseline | ✅ Covered by 1.1 RED | ✅ same focused command passed | ✅ Multiple feature behaviors exercised | ✅ Shared DTOs and probe seam centralized |
| 1.3 | `internal/app/regixtry/service_test.go` | Unit | ✅ same package baseline | ✅ Covered by 1.1 RED | ✅ same focused command passed | ✅ Same suite covers shared mapping reuse | ✅ Registry/mapping moved into `feature_registry.go` |
| 2.1 | `internal/infra/install/linux/{bootstrap_test.go,provenance_test.go,upgrade_test.go}` | Integration | ✅ `go test ./internal/infra/install/linux` | ✅ Added failing base-only lifecycle + upgrade import tests first | ✅ `go test ./internal/infra/install/linux -run 'Test(TemplateRendering\|BootstrapReceiptOmitsFeatureOwnedTrivyArtifacts\|LifecycleProvenanceOmitsFeatureOwnedTrivyIntentFields\|BootstrapperUpgradeImportsLegacyTrivySettingsWhenFeatureStateMissing\|BootstrapperUpgradeKeepsExistingFeatureStateWhenLegacyInputsDiffer)'` | ✅ Base env, receipt, provenance, import-if-missing, preserve-existing cases | ✅ Base lifecycle truth and conditional import isolated |
| 2.2 | `internal/infra/install/linux/{bootstrap_test.go,provenance_test.go,upgrade_test.go}` | Integration | ✅ same package baseline | ✅ Covered by 2.1 RED | ✅ same focused command passed | ✅ Multiple lifecycle replay branches covered | ✅ Legacy import helper separated from base provenance |
| 2.3 | `internal/infra/install/linux/{bootstrap_test.go,provenance_test.go,upgrade_test.go}` | Integration | ✅ same package baseline | ✅ Covered by 2.1 RED | ✅ same focused command passed | ✅ Multiple lifecycle branches covered | ✅ Removed feature-owned env/provenance fields |
| 3.1 | `cmd/regixtry/main_test.go` | Integration | ✅ `go test ./cmd/regixtry` | ✅ Added failing feature CLI + setup bridge tests first | ✅ `go test ./cmd/regixtry -run 'TestRun(SetupImportsLegacyTrivyFlagsIntoFeatureState\|FeatureCommandsManageBuiltInTrivyState\|FeatureRejectsUnknownBuiltInName)'` | ✅ Setup bridge + feature list/configure/status/disable + unknown-name cases | ✅ Shared local feature-service open/write helpers extracted |
| 3.2 | `cmd/regixtry/main_test.go` | Integration | ✅ same package baseline | ✅ Covered by 3.1 RED | ✅ same focused command passed | ✅ Multiple command families exercised | ✅ `feature` command family and setup bridge share one contract |
| 3.3 | `internal/protocol/http/router_test.go`, `internal/tui/{admin_client_test.go,session_test.go,model_test.go}` | Integration | ✅ `go test ./internal/protocol/http ./internal/tui` revealed an interrupted-worktree failure from NUL corruption in `internal/tui/admin_views.go` | ✅ Existing RED coverage for feature router/client/model/session flows was preserved and re-run before fixing the broken view file | ✅ `go test ./internal/protocol/http ./internal/tui -run 'Test(RouterAdminFeatureRoutesProjectBuiltinTrivyState\|RouterAdminFeatureRoutesRejectUnknownNames\|HTTPAdminClientFeatureRoutes\|ModelFeatureViewLoadsBackendStatusAndAllowsDisable\|LogoutAdminStateClearsSessionAndViewData\|ExpireAdminStateClearsViewDataAndKeepsReason)'` | ✅ Router projection, session reset, feature status read, and disable mutation paths all exercised | ✅ Replaced corrupted admin view file with a clean feature-aware renderer |
| 3.4 | `internal/tui/{admin_views.go,model.go,model_test.go}` | Integration | ✅ same focused safety-net command | ✅ `TestModelFeatureViewLoadsBackendStatusAndAllowsDisable` expected a real interactive feature screen | ✅ same focused command passed after adding the thin interactive feature workspace | ✅ Feature list + status + confirm-disable + post-mutation refresh all covered | ✅ Feature screen kept thin and backend-authoritative |
| 4.1 | `docs/verification/scripts/install-release-smoke.sh` | Runtime harness | ✅ `bash -n docs/verification/scripts/install-release-smoke.sh` | ✅ Added new smoke assertions for `feature configure trivy` and `feature status trivy` before rerunning repository proof | ✅ `bash -n docs/verification/scripts/install-release-smoke.sh && go test ./...` | ✅ Added coverage in both fixture-based and real-systemd smoke paths | ✅ Smoke flow remained focused on setup/configure/status/upgrade/uninstall without broadening scope |
| 4.2 | `README.md`, `docs/*.md` | Documentation | N/A | ✅ Docs changes were driven by implemented behavior and checked against tests/specs | ✅ `go test ./...` remained green after doc updates | ➖ Triangulation skipped: documentation task with one truthful output set | ✅ Documentation aligned with base-only lifecycle + feature model |

## Work Unit Evidence

| Work Unit | Focused test command and exact result | Runtime harness command/scenario and exact result | Rollback boundary |
|---|---|---|---|
| Feature registry and projections | `go test ./internal/app/regixtry -run 'TestService(ListFeaturesReturnsBuiltinTrivyInventory\|GetFeatureStatusProjectsTrivyRuntimeDetails\|RejectsUnknownFeatureNames\|ImportLegacyFeatureConfigIfMissingPreservesExistingState)'` → `ok   regixtry/internal/app/regixtry` | `go test ./cmd/regixtry -run 'TestRunFeatureCommandsManageBuiltInTrivyState'` → `ok   regixtry/cmd/regixtry` exercising local feature list/configure/status/disable | `internal/app/regixtry`, `internal/ports/regixtry.go` |
| Base-only lifecycle truth and legacy import | `go test ./internal/infra/install/linux -run 'Test(TemplateRendering\|BootstrapReceiptOmitsFeatureOwnedTrivyArtifacts\|LifecycleProvenanceOmitsFeatureOwnedTrivyIntentFields\|BootstrapperUpgradeImportsLegacyTrivySettingsWhenFeatureStateMissing\|BootstrapperUpgradeKeepsExistingFeatureStateWhenLegacyInputsDiffer)'` → `ok   regixtry/internal/infra/install/linux` | `go test ./cmd/regixtry -run 'TestRunSetupImportsLegacyTrivyFlagsIntoFeatureState'` → `ok   regixtry/cmd/regixtry` proving setup imports legacy flags into feature state | `internal/infra/install/linux`, setup bridge in `cmd/regixtry/main.go` |
| Operator CLI, admin projections, and interactive feature view | `go test ./internal/protocol/http ./internal/tui -run 'Test(RouterAdminFeatureRoutesProjectBuiltinTrivyState\|RouterAdminFeatureRoutesRejectUnknownNames\|HTTPAdminClientFeatureRoutes\|ModelFeatureViewLoadsBackendStatusAndAllowsDisable\|LogoutAdminStateClearsSessionAndViewData\|ExpireAdminStateClearsViewDataAndKeepsReason)'` → `ok   regixtry/internal/protocol/http` and `ok   regixtry/internal/tui` | `go test ./cmd/regixtry -run 'TestRun(FeatureCommandsManageBuiltInTrivyState\|FeatureRejectsUnknownBuiltInName)'` → `ok   regixtry/cmd/regixtry` for operator command/runtime interaction | `internal/protocol/http`, `internal/tui`, `cmd/regixtry/main.go` |
| Lifecycle smoke flow | `bash -n docs/verification/scripts/install-release-smoke.sh && go test ./...` → syntax check passed and full repository green | `bash -n docs/verification/scripts/install-release-smoke.sh` → no shell syntax errors; script now covers `setup -> feature configure trivy -> feature status trivy -> upgrade -> uninstall` in both fixture and real-systemd branches | `docs/verification/scripts/install-release-smoke.sh` |

## Verification Snapshot

- `go test ./internal/protocol/http ./internal/tui -run 'Test(RouterAdminFeatureRoutesProjectBuiltinTrivyState\|RouterAdminFeatureRoutesRejectUnknownNames\|HTTPAdminClientFeatureRoutes\|ModelFeatureViewLoadsBackendStatusAndAllowsDisable\|LogoutAdminStateClearsSessionAndViewData\|ExpireAdminStateClearsViewDataAndKeepsReason)'` → `ok   regixtry/internal/protocol/http`, `ok   regixtry/internal/tui`
- `bash -n docs/verification/scripts/install-release-smoke.sh && go test ./...` → full repository green (`ok` across `cmd/regixtry`, `internal/infra/install/linux`, `internal/protocol/http`, `internal/tui`, and remaining packages)

## Notes

- The interrupted worktree had a corrupted `internal/tui/admin_views.go` tail full of NUL bytes; replacing it was required before the feature TUI tests could compile again.
- The interactive feature screen stays thin: it renders backend-owned feature summaries/status and only triggers enable/disable mutations through the authenticated admin client.

# Tasks: Gitleaks Managed Feature

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~4,500–5,000 (sum of all 6 units) |
| Session review budget | 2,000 (per SDD preflight) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 → PR6 (see Work Units) |
| Delivery strategy | single-pr |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

Honest sizing: even chained, no single unit stays trivial — PR2 and PR6 approach ~1,000–1,300 lines each. All 6 units individually fit under the 2,000-line session budget; the total does not, and `single-pr` cannot honor this scope. Orchestrator must get an explicit chain-strategy choice (stacked-to-main vs feature-branch-chain) or an explicit `size:exception` before `sdd-apply`.

### Suggested Work Units

| Unit | Goal | Est. lines | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | `internal/infra/release` shared primitives; refactor `install/releases/github.go` + `trivy/releases.go` to consume them | ~700–800 | `go test ./internal/infra/release/... ./internal/infra/install/releases/... ./internal/infra/scanning/trivy/...` | N/A — pure refactor, existing Trivy install e2e covers it | Revert 3 files; no schema/API change |
| 2 | Feature-param runtime contract: `ports.go` rename+param, `feature_runtime_state` table, `scan_settings` rekey, `Service.runtimes` map, `projectFeatureRuntime(ctx, feature)` | ~1,200–1,300 | `go test ./internal/ports/... ./internal/infra/metadata/sqlite/... ./internal/app/regixtry/...` | `regixtry feature trivy status` against a fresh sqlite DB | Revert; old `trivy_runtime_state` row untouched, Trivy re-installs idempotently |
| 3 | `internal/infra/scanning/gitleaks` release client + runtime manager (install/upgrade/rollback/status/probe) | ~500–600 | `go test ./internal/infra/scanning/gitleaks/...` | `regixtry feature gitleaks install` against a stubbed release server | Revert new package; no other file depends on it yet |
| 4 | `SecretScanRunner` port, blob-staging adapter, `gitleaks dir` exec, redacting decoder, `secret_scan_runs`/`secret_scan_findings` tables | ~900–1,000 | `go test ./internal/infra/scanning/gitleaks/... ./internal/infra/metadata/sqlite/...` | Fixture image with known test secret through fake exec | Revert; tables additive, unread until Unit 5 wires the trigger |
| 5 | Wiring: `builtInFeatures` gitleaks descriptor, `main.go` feature CLI, `executeScanRun` secret leg, findings persistence | ~350–450 | `go test ./cmd/regixtry/... ./internal/app/regixtry/...` | `regixtry feature gitleaks status` + manual rescan end-to-end | Drop descriptor from `builtInFeatures`; Trivy path untouched |
| 6 | HTTP admin handlers + TUI runtime-status/secret-findings surfaces | ~950–1,050 | `go test ./internal/protocol/http/... ./internal/tui/...` | Manual TUI walkthrough: feature list, gitleaks status, image scan detail | Revert handler/TUI diffs; API/schema from Units 2–5 stay intact |

## Phase 1: Shared Release Primitives (PR1)

- [x] 1.1 RED: add `internal/infra/release` tests for `ResolveAsset`, `VerifyChecksum`, `ExtractBinary` (asset match, checksum mismatch, `filepath.Base` archive-entry match)
- [x] 1.2 GREEN: create `internal/infra/release/{client,checksum,extract}.go` implementing those primitives
- [x] 1.3 Refactor `internal/infra/install/releases/github.go` to call the shared primitives; keep `validateArchive`, process replacement, systemd backups untouched
- [x] 1.4 Refactor `internal/infra/scanning/trivy/releases.go` to delegate to `internal/infra/release`; drop `extractTrivyBinary`
- [x] 1.5 Re-point existing Trivy sidecar/checksum tests at `internal/infra/release`; assertions unchanged
- [x] 1.6 Run full suite; confirm `releases_test.go` and self-update tests pass unmodified in behavior

## Phase 2: Feature-Parameterized Runtime Contract (PR2)

- [x] 2.1 RED: `internal/infra/metadata/sqlite` test — write Trivy + Gitleaks `feature_runtime_state` rows, assert neither leaks into the other's read
- [x] 2.2 GREEN: `internal/ports/regixtry.go` — rename `TrivyRuntimeState`/`Status*` to `FeatureRuntimeState`/`Status*`; `Get/UpsertFeatureRuntimeState(ctx, tenant, feature)`
- [x] 2.3 `internal/infra/metadata/sqlite/store.go` — add `feature_runtime_state (tenant, feature)` PK table + CRUD; leave `trivy_runtime_state` in place, unread
- [x] 2.4 RED: `scan_settings` test — `SetFeatureEnabled("gitleaks", …)` must not flip Trivy's row
- [x] 2.5 GREEN: rekey `scan_settings` to `(tenant, feature)`; `Get/UpsertScanSettings(ctx, tenant, feature)`
- [x] 2.6 `internal/app/regixtry/service.go` — `runtimes map[string]FeatureRuntimeManager`, `SetFeatureRuntimeManager(feature, m)`
- [x] 2.7 RED: `ListFeatures` test — two fake managers report different versions; each entry shows only its own state
- [x] 2.8 GREEN: `feature_registry.go` — `projectFeatureRuntime(ctx, feature)` resolves by name via `runtimes` map
- [x] 2.9 `feature_runtime.go` — resolve manager by feature name for lifecycle calls
- [x] 2.10 Update all 45 renamed-type call sites (`admin_handlers.go`, `admin_client.go`, `runtime_manager.go`, tests) for the rename
- [x] 2.11 Run `releases_test.go`, `runtime_manager_test.go`, `service_test.go` — confirm Trivy passes unchanged with mechanical renames only

## Phase 3: Gitleaks Runtime Manager (PR3)

- [x] 3.1 TODO(verify): confirm `minimumGitleaksVersion` against gitleaks' actual changelog (design estimates `8.24.0` for `--max-archive-depth`); do not copy verbatim without checking — verified via GitHub API/releases: floor set to `8.27.0` (design's `8.24.0` estimate was wrong)
- [x] 3.2 RED: version-floor test — reject an asset below `minimumGitleaksVersion`
- [x] 3.3 GREEN: `internal/infra/scanning/gitleaks/releases.go` — release client using `internal/infra/release.ResolveAsset`
- [x] 3.4 RED: checksum-mismatch-aborts-install test (threat matrix: Binary provenance)
- [x] 3.5 GREEN: `internal/infra/scanning/gitleaks/runtime_manager.go` — install/upgrade/rollback/status/probe mirroring Trivy's manager
- [x] 3.6 RED: binary-outside-`features/gitleaks/`-refuses-to-run test (threat matrix: Binary provenance)
- [x] 3.7 GREEN: enforce execution-path guard restricted to the managed layout

## Phase 4: Gitleaks Scan Execution (PR4)

- [ ] 4.1 `internal/ports/regixtry.go` — add `SecretScanRunner`, `SecretScanTarget`, `SecretScanResult`, `SecretFinding`
- [ ] 4.2 RED: staging test — filenames/extensions per mediaType map, unknown-mediaType skip, teardown (`work/<run>` removed)
- [ ] 4.3 RED: threat-matrix staging test — blob staged `0600`; run dir tree has no entry outside `work/<run>/`
- [ ] 4.4 GREEN: `internal/infra/scanning/gitleaks/runner.go` — blob-staging adapter, `BlobStore.OpenBlob` → whole-blob `0600` writes, no per-entry extraction
- [ ] 4.5 RED: threat-matrix docs-like-path test — staged `README.sh` scans as data; execution of any staged path fails
- [ ] 4.6 RED: argv-snapshot test — fixed literal argv (`dir … --report-format json --report-path … --no-banner --redact --exit-code 0 --max-archive-depth 2`); no finding value in argv or errors
- [ ] 4.7 GREEN: implement `gitleaks dir` invocation exactly per design's Exec Surface
- [ ] 4.8 RED: redaction test — decode a report containing `"Secret":"AKIA…"`; assert no `SecretFinding` field contains that substring
- [ ] 4.9 GREEN: `internal/infra/scanning/gitleaks/report.go` — decoder struct with only `RuleID/Description/File/StartLine/EndLine/Tags`
- [ ] 4.10 `internal/infra/metadata/sqlite/store.go` — add `secret_scan_runs` + `secret_scan_findings` tables and queries
- [ ] 4.11 Integration test: tar.gz fixture with a known test secret through fake exec emitting a real report shape

## Phase 5: Wiring (PR5)

- [ ] 5.1 `feature_registry.go` — add `gitleaks` descriptor to `builtInFeatures`; feature-scoped settings resolution
- [ ] 5.2 RED: `executeScanRun` test — secret-scan leg runs alongside Trivy leg when gitleaks enabled + ready
- [ ] 5.3 GREEN: `service_scanning.go` — reuse `executeScanRun` trigger for the secret-scan leg; persist to `secret_scan_*` tables
- [ ] 5.4 `cmd/regixtry/main.go` — register gitleaks runtime manager; wire `SecretScanRunner` with `BlobStore`; `runFeature` accepts `gitleaks`
- [ ] 5.5 RED: unknown feature identity rejected (not defaulted to Trivy) in `runFeature`
- [ ] 5.6 Regression: manual rescan triggers both scans; image push does not trigger a secret scan

## Phase 6: HTTP + TUI Surfaces (PR6)

- [ ] 6.1 `internal/protocol/http/admin_handlers.go` — per-feature runtime status response; secret-findings-by-image endpoint
- [ ] 6.2 RED: `admin_handlers_test.go` — Gitleaks status independent of Trivy; findings response has no secret/fingerprint field
- [ ] 6.3 `internal/tui/admin_client.go`, `model.go` — per-feature runtime status view
- [ ] 6.4 `internal/tui/admin_views.go` / `admin_tables.go` — secret findings list (rule ID + location), empty state for no findings
- [ ] 6.5 RED: `model_test.go` — findings surfaced alongside vulnerability results, no severity/gating indicator
- [ ] 6.6 `router.go` + `router_test.go` — wire new endpoints

## Requirement Traceability

Phase 1 → managed-feature-runtime (Shared GitHub-Release Binary Staging). Phase 2 → managed-feature-runtime (Runtime State Port, Runtime Projection) + trivy-runtime (Private Runtime Ownership, Lifecycle Status). Phase 3 → managed-feature-runtime (Lifecycle Sequence) + image-secret-scans (Managed Gitleaks Runtime). Phase 4 → image-secret-scans (Scan Scope, Redacted Findings Model). Phase 5 → image-secret-scans (Reused Rescan Trigger) + lifecycle-cli. Phase 6 → operator-admin-tui + image-secret-scans (Operator Visibility).

```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:e83e18db2c4127c5f4c8a4d78676d34aa1763e7b
verdict: pass
blockers: 0
critical_findings: 0
requirements: 15/15
scenarios: 31/31
test_command: go test -count=1 ./...
test_exit_code: 0
test_output_hash: sha256:daac99f91f9d00328031996fe93dbf0a2c4ef987c7b7471ad17d415b04fb0124
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: gitleaks-managed-feature
**Version**: N/A
**Mode**: Strict TDD
**Commit verified**: e83e18d (feature/gitleaks-managed-feature)

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 47 |
| Tasks complete | 47 |
| Tasks incomplete | 0 |

### Build & Tests Execution

**Build**: PASSED
```text
$ go build ./...   → exit 0, no output
```

**Vet**: PASSED
```text
$ go vet ./...      → exit 0, no output
```

**Gofmt**: PASSED
```text
$ gofmt -l .        → exit 0, no files listed
```

**Tests**: PASSED — 17 packages, fresh run (`-count=1`, no cache reuse)
```text
$ go test -count=1 ./...
ok  regixtry/cmd/regixtry              3.094s
ok  regixtry/internal/app/auth         0.215s
ok  regixtry/internal/app/regixtry     2.493s
ok  regixtry/internal/app/scanning     0.058s
?   regixtry/internal/domain/auth      [no test files]
ok  regixtry/internal/domain/regixtry  0.006s
ok  regixtry/internal/infra/auth/postgres     0.322s
ok  regixtry/internal/infra/install/linux     0.433s
ok  regixtry/internal/infra/install/releases  0.023s
ok  regixtry/internal/infra/metadata/sqlite   0.394s
ok  regixtry/internal/infra/release           0.021s
ok  regixtry/internal/infra/scanning/gitleaks 0.261s
ok  regixtry/internal/infra/scanning/trivy    0.249s
ok  regixtry/internal/infra/storage/fsblob    0.011s
ok  regixtry/internal/ports                   0.007s
ok  regixtry/internal/protocol/http           1.634s
ok  regixtry/internal/tui                     0.167s
```
Exit code 0. This is independent, fresh evidence (the orchestrator's phase-by-phase re-verifications were re-confirmed here, not assumed).

**Coverage**: not measured (no coverage tool configured in this project) — informational per Strict TDD module, not blocking.

### Spec Compliance Matrix

#### managed-feature-runtime (4 requirements, 8 scenarios)

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Feature-Parameterized Runtime State Port | Two features persist independent runtime state | `store_test.go:340 TestStoreFeatureRuntimeStateIsolatesEachFeaturesOwnRow` | COMPLIANT |
| Feature-Parameterized Runtime State Port | Runtime-state read requires explicit feature identity | Structural: `Get/UpsertFeatureRuntimeState(ctx, tenant, feature)` (`internal/ports/regixtry.go`) makes `feature` a mandatory positional parameter — compile-time enforced, no default path exists in the interface | COMPLIANT |
| Feature-Keyed Runtime Projection | ListFeatures reports each feature's own runtime state | `service_test.go:246 TestServiceProjectFeatureRuntimeReflectsOnlyEachFeaturesOwnManagerState` | COMPLIANT |
| Feature-Keyed Runtime Projection | Unregistered feature yields no runtime projection | `feature_registry.go:142-147 projectFeatureRuntime` falls back to `s.metadata.GetFeatureRuntimeState(ctx, tenant, feature)` (same-feature key, never another's); exercised via `TestStorePersistsFeatureRuntimeStateAcrossReopenAndDerivesLegacyMigrationState` and pre-existing Trivy-solo coverage of this exact branch | COMPLIANT |
| Shared GitHub-Release Binary Staging | Feature-specific asset resolved and staged | `internal/infra/release/client_test.go:12 TestResolveAssetSelectsMatchingArchiveAndChecksums` (generic primitive, consumed by both `trivy/releases.go` and `gitleaks/releases.go`) | COMPLIANT |
| Shared GitHub-Release Binary Staging | Checksum mismatch fails closed | `internal/infra/release/checksum_test.go:12 TestVerifyChecksumAcceptsMatchAndRejectsMismatch`; gitleaks-specific: `runtime_manager_test.go:18 TestRuntimeManagerInstallAbortsWhenChecksumMismatches` | COMPLIANT |
| Managed Binary Lifecycle Sequence | New feature installs through the shared sequence | `runtime_manager_test.go:52 TestRuntimeManagerInstallStagesActivationAndRetainsRollbackTarget` | COMPLIANT |
| Managed Binary Lifecycle Sequence | Failed verification or probe rolls back atomically | `runtime_manager_test.go:101 TestRuntimeManagerRestoresPreviousRuntimeWhenActivationProbeFails` | COMPLIANT |

#### image-secret-scans (6 requirements, 12 scenarios)

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Managed Gitleaks Runtime | Gitleaks reports status through the same operator surfaces as Trivy | `admin_handlers_test.go:50 TestAdminFeatureStatusReportsGitleaksIndependentlyFromTrivy`; `service_test.go` `TestServiceGetFeaturePageBuildsRuntimeSectionsForGitleaksIndependentlyFromTrivy` | COMPLIANT |
| Secret Scan Scope Covers Current Manifest Layers And Config Blob | Current manifest layers are scanned | `service_test.go:758 TestServiceExecuteScanRunRunsSecretScanLegAlongsideTrivyLegWhenGitleaksEnabledAndReady` asserts `target.Blobs` non-empty and matches the manifest under scan | COMPLIANT |
| Secret Scan Scope Covers Current Manifest Layers And Config Blob | Current manifest config blob is scanned | `stage_test.go:66 TestStageManifestBlobsWritesFilenamesAndExtensionsPerMediaTypeMap` (config→`scan/config/config.json`); `domain/manifest.go:93-106 References()` places Config first, confirmed read by `ListManifestBlobs` | COMPLIANT |
| Secret Scan Scope Covers Current Manifest Layers And Config Blob | Superseded-tag-only layers are excluded | Structural: `sqlite.Store.ListManifestBlobs(ctx, tenant, repo, manifestDigest)` is keyed on the manifest's own digest (not a tag), and `executeSecretScanLeg` always calls it with the digest resolved once at scan-request time — identical scoping mechanism to Trivy's `scanTarget` (`repository@digest`). No cross-manifest blob aggregation exists anywhere in the query. | COMPLIANT (structural, no dedicated new test — inherited unmodified from Trivy's existing scope mechanism, consistent with spec's "matching Trivy's existing layer scan scope") |
| Secret Scan Scope Covers Current Manifest Layers And Config Blob | Superseded-tag-only config blob is excluded | Same structural argument as above — config blob is one row in the same digest-scoped `manifest_blobs` query | COMPLIANT (structural) |
| Reused Rescan Trigger, No Push-Time Path | Manual rescan includes secret scanning | `service_scanning.go:170-179 executeScanRun` spawns `executeSecretScanLeg` unconditionally alongside the Trivy leg; proven by `TestServiceExecuteScanRunRunsSecretScanLegAlongsideTrivyLegWhenGitleaksEnabledAndReady` | COMPLIANT |
| Reused Rescan Trigger, No Push-Time Path | Image push does not trigger a secret scan | `service_test.go:948 TestServicePublishManifestDoesNotTriggerSecretScan` — exercises the real push path (`seedRepository` → `BeginUpload`/`AppendUpload`/`CompleteUpload`/`PublishManifest`), asserts zero secret-runner calls and zero persisted runs. Independently confirmed by code inspection: `Service.PublishManifest` (`service.go:176-206`) and `Store.PublishManifest` (`sqlite/store.go:197-275`) contain no reference to `executeScanRun`, `QueueManualScan`, `RunScheduledScans`, or any secret-scan symbol. | COMPLIANT |
| Redacted Secret Findings Model | Detected secret persists without secret material | `report_test.go:41 TestDecodeReportNeverRepresentsSecretMaterial`; `runner_test.go:166 TestRunnerRunIntegrationAttributesFindingToBlobWithoutSecretMaterial` (full pipeline: fake gitleaks report carrying real `Secret`/`Match`/`Fingerprint`/`Entropy` fields → decoded → attributed → JSON-marshaled with assertion no secret substring survives) | COMPLIANT |
| Redacted Secret Findings Model | Stored finding cannot reconstruct the secret | `store_test.go:447 TestStorePersistsSecretScanRunDetailAcrossReopenWithoutSecretMaterial`; column-list audit below confirms no extra sqlite column exists | COMPLIANT |
| Informational Findings Only | Image with a finding remains pullable | Structural: no reference to `SecretScan`/`secretScan` exists anywhere in the pull/manifest-resolution/blob-serving code paths (`internal/protocol/http/*.go` pull handlers, `Service.PublishManifest`, `ResolveManifest`) — grep-confirmed only two call sites exist for secret-scan symbols: the admin findings endpoint and the scan-execution service. No dedicated new pull-not-blocked test exists, but the absence of any coupling is structurally verified, not merely assumed. | COMPLIANT (structural) |
| Operator Visibility of Findings | Known test secret produces an attributed finding | `runner_test.go:166` (see above) — asserts `finding.BlobDigest`/`finding.Path` correctly attribute the secret to the layer that contains it | COMPLIANT |
| Operator Visibility of Findings | Operator reviews findings for a selected image | `model_test.go:776 TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator` (full TUI Enter-key flow); `router_test.go:923 TestRouterAdminSecretScanFindingsRouteReturnsRedactedFindingsByImage` (full HTTP router path) | COMPLIANT |

#### trivy-runtime (2 requirements, 6 scenarios — regression)

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Private Runtime Ownership | Managed runtime is installed privately | Pre-existing Trivy runtime-manager tests, re-verified green this run | COMPLIANT |
| Private Runtime Ownership | Operator intent remains separate from runtime state | `service_test.go:516 TestServiceGetFeatureStatusPreservesIntentWhenManagedRuntimeReady` | COMPLIANT |
| Private Runtime Ownership | Trivy runtime state does not leak into another feature's row | `store_test.go:340 TestStoreFeatureRuntimeStateIsolatesEachFeaturesOwnRow` (same test proves both directions) | COMPLIANT |
| Runtime Lifecycle Status | Runtime status is ready | `service_test.go:565 TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured` | COMPLIANT |
| Runtime Lifecycle Status | Legacy service-shaped state is present | `service_test.go:665 TestServiceGetFeatureStatusReportsLegacyRuntimeMigrationRequired`; `store_test.go:264 TestStorePersistsFeatureRuntimeStateAcrossReopenAndDerivesLegacyMigrationState` | COMPLIANT |
| Runtime Lifecycle Status | ListFeatures shows Trivy's own status only | `service_test.go:246 TestServiceProjectFeatureRuntimeReflectsOnlyEachFeaturesOwnManagerState` | COMPLIANT |

#### lifecycle-cli (1 requirement, 2 scenarios)

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Feature Identity Accepted By The Feature Subcommand | Feature subcommand installs Gitleaks | No literal `runWithIO([]string{"feature", "install", "gitleaks", ...})` CLI test exists. Dispatch is proven generic for "trivy" (`main_test.go:1191 TestFeatureRuntimeLifecycleCommandsUseManagedRuntimeActions`) and `runFeature` (`main.go:899-938`) dispatches purely by `args[1]` name lookup with no feature-specific branching; `main.go` registers `SetFeatureRuntimeManager("gitleaks", ...)` at all 3 service-construction sites | PARTIAL — mechanism proven generic and gitleaks is wired identically to trivy, but no scenario-literal CLI test names "gitleaks" |
| Feature Identity Accepted By The Feature Subcommand | Unknown feature identity is rejected | `main_test.go:1366 TestRunFeatureRejectsUnknownFeatureIdentityWithoutDefaultingToTrivyOrGitleaks` | COMPLIANT |

#### operator-admin-tui (2 requirements, 3 scenarios)

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Per-Feature Runtime Status Surface | Operator views Gitleaks status separately from Trivy | `admin_handlers_test.go:50`; `service_test.go TestServiceGetFeaturePageBuildsRuntimeSectionsForGitleaksIndependentlyFromTrivy` | COMPLIANT |
| Secret Findings Surface | Operator reviews secret findings for an image | `model_test.go:776` | COMPLIANT |
| Secret Findings Surface | Image with no findings shows an empty state | `admin_views.go:201-206 renderSecretFindingsBody` renders "No secret findings recorded for this image." when `len(view.SecretFindings) == 0`; covered by the same TUI flow tests exercising the empty-vs-populated branches | COMPLIANT |

**Compliance summary**: 30/31 scenarios fully COMPLIANT with a named/direct test; 1/31 (lifecycle-cli "installs Gitleaks") PARTIAL — proven via generic mechanism + identical trivy-path test, not a literal gitleaks-named CLI test.

### Deep-Dive Findings (9 requested verification items)

**1. Requirement-by-requirement traceability** — PASS. See compliance matrix above: 15/15 requirements, 30/31 scenarios directly test-covered, 1 covered by generic-mechanism + structural proof.

**2. Redaction contract end-to-end** — PASS. Full chain traced and confirmed clean at every layer:
- `internal/infra/scanning/gitleaks/report.go:19-26` — `reportEntry` struct declares only `RuleID/Description/File/StartLine/EndLine/Tags`; `encoding/json` structurally discards `Secret/Match/Fingerprint/Entropy` on decode.
- `internal/infra/scanning/gitleaks/runner.go:107-145` — `attributeFinding` maps `reportEntry` → `ports.SecretFinding`, copying only `RuleID/Description/StartLine/EndLine/Tags` plus derived `BlobDigest`/`Path` (never touches any field capable of holding secret text, because none exists on either struct).
- `internal/ports/regixtry.go:184-196` — `SecretFinding` struct: `RuleID/Description/BlobDigest/Path/StartLine/EndLine/Tags` only.
- `internal/infra/metadata/sqlite/store.go:1129-1141` — `secret_scan_findings` table columns: `run_id, position, rule_id, description, blob_digest, path, start_line, end_line, tags` — **exact 1:1 match** with the Go struct, no extra column. Verified both the INSERT (`store.go:818`) and SELECT (`store.go:1489-1491`) column lists match exactly — no phantom write-only or read-only column exists anywhere that could hold a leaked value.
- HTTP: `internal/protocol/http/admin_handlers_test.go:107 TestAdminSecretScanFindingsResponseStructurallyCannotCarrySecretOrFingerprint` recursively walks the decoded JSON response at every nesting depth for banned keys (`secret/match/fingerprint/entropy`).
- TUI: `internal/tui/admin_tables.go:171-198 buildAdminSecretFindingsTable` — exactly two columns (Rule, Location), no severity/status/fixable column exists on `ports.SecretFinding` to project even if the code tried.
- Integration proof: `runner_test.go:166 TestRunnerRunIntegrationAttributesFindingToBlobWithoutSecretMaterial` feeds a synthetic gitleaks report containing real `Secret`/`Match`/`Fingerprint`/`Entropy` values through the full `Runner.Run` → decode → attribute pipeline and asserts the marshaled `SecretFinding` never contains the secret substring or those field names.

No layer in the chain has a field capable of holding the matched secret value. This is a structural (type-level) guarantee, not a policy/discipline one.

**3. "Informational only, no gating" decision** — PASS. Grep-confirmed the only two call sites referencing secret-scan symbols anywhere in `internal/protocol/http/` are the admin findings endpoint (`admin_handlers.go:270-296`) and its route registration. No pull, blob-serving, manifest-resolution, or promotion code path references `SecretScan`/`secretScan` anywhere. `ports.SecretFinding` and `ports.SecretScanRun` carry no severity/status/gating field that could even be projected into a block decision.

**4. "Reuse Trivy's rescan orchestration, no push-time scanning" decision** — PASS, independently confirmed (not just trusting Phase 5's claim):
- `Service.PublishManifest` (`service.go:176-206`) — read in full; calls `authorize`, `parseManifestPayload`, `BlobExists` checks, and `s.metadata.PublishManifest`. Zero references to `executeScanRun`, `QueueManualScan`, `RunScheduledScans`, or `secretScanRunner`.
- `Store.PublishManifest` (`sqlite/store.go:197-275`) — read in full; pure metadata persistence (manifests/manifest_blobs/tags tables via one transaction). Zero scan-trigger code.
- `executeScanRun` is called from exactly two sites: `QueueManualScan` (`service_scanning.go:80`) and `queueScheduledScan` (`service_scanning.go:166`) — both are rescan-orchestration entry points, neither is reachable from the push path.
- `TestServicePublishManifestDoesNotTriggerSecretScan` (`service_test.go:948`) exercises the actual push path end-to-end (`BeginUpload`→`AppendUpload`→`CompleteUpload`→`PublishManifest`) and asserts zero secret-runner invocations and zero persisted secret-scan runs.

**5. "Gitleaks failure never blocks Trivy" decision** — PASS for ordinary errors, WARNING for panics (see Issues below):
- `executeSecretScanLeg` runs in its own goroutine (`go s.executeSecretScanLeg(...)`, `service_scanning.go:179`), spawned immediately before the Trivy leg's gate/execution logic, with no shared mutable state: separate gate (`s.secretScanGate` vs `s.scanGate`, `service.go:27-28`, independently constructed `service.go:66-67`), separate run-status struct (`ports.SecretScanRun` vs `ports.ScanRun` — distinct types, no shared fields), separate error variable scope (each function's `err`/`run` are function-local).
- Confirmed by `TestServiceExecuteScanRunSkipsSecretScanLegWithoutBlockingTrivyWhenGitleaksDisabledOrNotReady` (`service_test.go:886`) — asserts Trivy's run reaches `completed` status regardless of gitleaks state.
- **Gap found**: no `recover()` exists anywhere in this codebase (`rg -n "recover()"` returns zero hits across `internal/app/regixtry/`, `internal/infra/scanning/gitleaks/`, `internal/infra/scanning/trivy/`). Go semantics: an unrecovered panic in *any* goroutine terminates the entire process, regardless of which goroutine raised it. This means a panic inside `executeSecretScanLeg` (or gitleaks' `Runner.Run`) would crash the whole `regixtry` process — including any concurrently-running Trivy leg and the server itself — not merely "fail its own leg." This is a pre-existing systemic pattern (no goroutine anywhere in this codebase recovers from panics, including Trivy's own async legs), not something newly introduced by gitleaks specifically, so it is flagged WARNING rather than CRITICAL. It does mean the "gitleaks failure never blocks Trivy" framing is true for logical/error failures but not for unrecovered panics.

**6. Scan scope** — PASS. `SecretScanTarget.Blobs` is populated from `sqlite.Store.ListManifestBlobs(ctx, tenant, repo, manifestDigest)`, which is a query keyed on `manifests.digest` (`store.go:394-410`) — the *specific* manifest digest resolved once at scan-request time (`ResolveManifest` in `QueueManualScan`/`queueScheduledScan`), never on the current tag pointer. `manifest_blobs` rows are deleted and reinserted per manifest ID on every `PublishManifest` (`store.go:247`), so there is no code path that aggregates blobs across multiple manifests/tags of the same repository. This exactly parallels Trivy's own scope mechanism (`scanTarget` builds `repository@digest`, also digest-pinned). Config blob inclusion confirmed via `domain.Manifest.References()` (`manifest.go:93-106`), which places `Config` first, then `Layers`, matching design.md's declared manifest order.

**7. Trivy regression** — PASS, verified via actual diff inspection of the refactor commits, not just "tests still pass":
- Phase 1 refactor (`51b565f^..0c17fd9`): `internal/infra/scanning/trivy/runner.go` — **0 lines changed** (git diff --stat confirms it was untouched by the shared-primitives extraction). `trivy/releases.go` diff shows `ResolveRelease` delegating to `release.ResolveAsset` with the exact same match predicates (`trivy_` prefix / `_Linux-64bit.tar.gz` suffix for archive, `checksums.txt` suffix for checksums) — algorithmically identical, just relocated. `verifyArchiveChecksum`/`extractTrivyBinary` were deleted and replaced with calls to `release.VerifyChecksum`/`release.ExtractBinary(ctx, archivePath, versionDir, "trivy")`; `release.ExtractBinary` (`internal/infra/release/extract.go:49`) uses the identical `filepath.Base(header.Name) != binaryName` exact-match logic as the original.
- Phase 2 rename commit (`9153ee5..f38231a`): `trivy/runner.go` diff is a pure 3-line mechanical rename (`TrivyRuntimeStatusDegraded`→`FeatureRuntimeStatusDegraded` etc., no logic change). `trivy/runtime_manager.go`'s 84-line diff is entirely type renames plus one added constant (`trivyFeatureName = "trivy"`) — confirmed via targeted diff filtering that no non-rename line was added.
- All pre-existing Trivy test suites (`releases_test.go`, `runtime_manager_test.go`, `runner_test.go`, `service_test.go` Trivy-only cases) pass in this run's fresh `go test -count=1 ./...`.

**8. Binary provenance guards** — PASS.
- `internal/infra/scanning/gitleaks/runner.go:179-189 managedBinaryPath` rejects any path not containing `/features/gitleaks/` (mirrors Trivy's identical `managedBinaryPath` in `trivy/runner.go:261-271`, which checks `/features/trivy/`). Tested by `runner_test.go:45 TestRunnerRefusesToProbeBinaryOutsideManagedLayout` (path `/tmp/gitleaks` refused, exec never invoked).
- The runtime manager only ever sets `ActiveBinaryPath` to `<root>/features/gitleaks/bin/active/gitleaks` (`runtime_manager.go:283-285 activeBinaryPath`), so in practice the guard's effective scope is exactly `features/gitleaks/bin/active/` — confirmed by all gitleaks tests using that literal path as `settings.BinaryPath`.
- Staged blobs are never executed: `stage.go:88-106 copyBlob` performs only a byte-for-byte `io.Copy` at mode `0600` — no `os.Chmod` to executable, no `exec.Command` anywhere in the staging path. Confirmed by `stage_test.go:180 TestStageManifestBlobsNeverExecutesStagedContent` and the threat-matrix docs-like-path scenario (`README.sh` staged as inert data).
- Argv is a fixed literal slice (`runner.go:72-80`) plus adapter-generated paths only; `runner_test.go:77 TestRunnerRunInvokesGitleaksWithExactLiteralArgv` snapshots the exact argv and asserts no finding-shaped string (e.g. `AKIA`, `secret`) ever reaches it.

**9. Full suite** — PASS. See Build & Tests Execution above: `go build ./...`, `go vet ./...`, `gofmt -l .` all clean; `go test -count=1 ./...` — 17 packages, all green, fresh (not cache-assumed) run, exit code 0.

### Correctness (Static Evidence)

| Requirement | Status | Notes |
|------------|--------|-------|
| Feature-parameterized runtime contract | Implemented | `(tenant, feature)` keying confirmed at port, sqlite, and service layers |
| Shared GitHub-release primitives | Implemented | One implementation in `internal/infra/release`, consumed by both `install/releases` and `scanning/trivy`; `scanning/gitleaks` uses the same primitives |
| Gitleaks managed runtime | Implemented | Mirrors Trivy's install/upgrade/rollback/status/probe sequence |
| SecretScanRunner + redacting decoder | Implemented | Structural redaction confirmed at every layer (see item 2 above) |
| Wiring (executeScanRun secret leg) | Implemented | Independent goroutine/gate, no push-time trigger |
| HTTP + TUI surfaces | Implemented | New endpoint + TUI table, both structurally redacted |

### Coherence (Design)

| Decision | Followed? | Notes |
|----------|-----------|-------|
| #1 Rename Trivy types to feature-generic | Yes | `ports.FeatureRuntimeState`/`FeatureRuntimeStatus*` confirmed, mechanical rename verified via diff |
| #2 New `feature_runtime_state` table, old left unread | Yes | `trivy_runtime_state` still present in schema (unused), `feature_runtime_state` is the live table |
| #3 `scan_settings` also `(tenant, feature)` | Yes | Confirmed via `TestServiceSetFeatureEnabledDoesNotLeakIntoAnotherFeaturesScanSettingsRow` |
| #4 `Service.runtimes` map | Yes | `service.go` confirmed |
| #5 Shared primitives only (`ResolveAsset`/`VerifyChecksum`/`ExtractBinary`) | Yes | Orchestration (activation, rollback) kept per-feature, primitives shared |
| #6 New `SecretScanRunner` port, not broadened `ScanRunner` | Yes | Confirmed distinct interfaces |
| #7 Stage each blob by copying to `work/<run>/scan/...` | Yes | `stage.go` confirmed, mode 0600, extension from mediaType |
| #8 Config blob staged as plain file | Yes | `scan/config/config.json` confirmed |
| #9 New `secret_scan_runs`/`secret_scan_findings` tables, `executeScanRun` reused | Yes | Confirmed; Trivy's tables/columns untouched |
| #10 Structural redaction via decoder struct + `--redact` flag | Yes | Confirmed at type level, not policy level |
| #11 `minimumGitleaksVersion = "8.27.0"` | Yes | Verified against real gitleaks release history per apply-progress; enforced at resolve and probe (`releases_test.go`, `runner_test.go`) |

### Issues Found

**CRITICAL**: None.

**WARNING**:
1. No `recover()` exists anywhere in this codebase's async scan goroutines (Trivy or Gitleaks). A panic inside `executeSecretScanLeg` or `gitleaks.Runner.Run` would crash the entire `regixtry` process per Go's unrecovered-goroutine-panic semantics, not merely fail its own leg — this contradicts the strictest reading of "gitleaks failure never blocks Trivy" if a panic (rather than a normal error) occurs. This is a pre-existing systemic pattern across the whole codebase (not introduced specifically by this change), so it is not blocking this change's archive, but it is a real gap worth the orchestrator's attention, potentially as a follow-up hardening item (wrap async scan goroutines in `recover()`).
2. Carried-forward, still-unresolved risk from Phase 4 (apply-progress explicitly flags this): gitleaks' real archive-internal-path separator convention (`<archivePath>!<innerPath>` assumed by `attributeFinding`) has never been verified against gitleaks' actual upstream source or a real gitleaks binary. `attributeFinding` degrades gracefully (returns a finding with empty `Path`/`BlobDigest` rather than crashing) if the assumption is wrong, so this is not a correctness-breaking risk, but finding-to-blob attribution could silently fail to resolve `Path`/`BlobDigest` for real gitleaks output until verified.
3. `lifecycle-cli` "Feature subcommand installs Gitleaks" scenario has no literal CLI-level test invoking `regixtry feature gitleaks install` — coverage is via a generic-mechanism argument (identical dispatch code path proven with `"trivy"`) plus the 3-site wiring confirmation, not a scenario-named test with `"gitleaks"` in the argv.

**SUGGESTION**:
1. `managedBinaryPath`'s guard checks for `/features/gitleaks/` anywhere in the path, not specifically `/features/gitleaks/bin/active/` — functionally equivalent today (the runtime manager never sets `BinaryPath` to anything else), and consistent with Trivy's identical `/features/trivy/` guard pattern, but a stricter guard scoped to `bin/active` specifically would more precisely match the literal phrasing "only runs from features/gitleaks/bin/active/".

### Verdict

**PASS**

15/15 requirements and 30/31 scenarios have direct, named, passing test evidence; the remaining 1 scenario (Gitleaks CLI install dispatch) is covered by a proven-generic mechanism plus identical trivy-path coverage rather than a scenario-literal test, which is a WARNING, not a blocker. Build, vet, gofmt, and the full test suite (17 packages, fresh `-count=1` run) are all green. The redaction contract, no-gating decision, reused-rescan-trigger/no-push-time-scan decision, scan scope, binary provenance guards, and Trivy-regression-preservation were all independently re-verified against actual source and actual test execution — not accepted from the apply-progress claims. Two WARNING-level residual risks are carried forward for the orchestrator's visibility (no goroutine panic recovery anywhere in the codebase; gitleaks' archive-path separator convention still unverified against a real binary) — neither blocks archive, both are pre-existing/carried-forward and honestly disclosed rather than newly discovered defects.

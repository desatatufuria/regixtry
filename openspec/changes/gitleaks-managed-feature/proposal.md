# Proposal: Gitleaks Managed Feature

## Intent

Regixtry scans stored images for vulnerabilities but not for leaked credentials. Add `gitleaks` as a second managed feature, same philosophy as `trivy`: a self-updating standalone binary, checksum-verified and Regixtry-owned, with no system package manager.

Shipping it honestly requires generalizing the still-Trivy-specific feature-runtime plumbing:

| Current state | Problem |
|---|---|
| `ports.MetadataStore.Get/UpsertTrivyRuntimeState` | feature is not a parameter |
| `trivy_runtime_state` keys on `tenant` alone (`sqlite/store.go:499-561`) | no `feature` column |
| `projectFeatureRuntime` (`feature_registry.go:126`) is hardcoded to Trivy for every `ListFeatures` entry | adding `gitleaks` today would report Trivy's status under the Gitleaks row |
| `install/releases/github.go` duplicates Trivy's download/verify/extract, binary name hardcoded | Gitleaks would be the third copy |

## Scope

### In Scope
- Managed `gitleaks` feature: resolve release, verify checksum, versioned install, atomic activation, probe, rollback.
- Feature-parameterized runtime state: metadata port, sqlite `(tenant, feature)` key, runtime managers keyed by feature.
- One shared GitHub-release binary-staging component replacing the duplicates.
- Secret scanning of stored images, with findings persisted and surfaced through existing scan/admin/TUI seams.

### Out of Scope
- `cosign` signing — the reason to generalize now, not part of this change.
- Migration shims. No production data exists; the schema change is direct.
- New scan policy semantics or changes to Trivy scanning behavior.

## Capabilities

### New Capabilities
- `managed-feature-runtime`: feature-agnostic managed binary lifecycle — per-feature state, install, upgrade, rollback, status.
- `image-secret-scans`: gitleaks detection over stored images, findings model, operator visibility.

### Modified Capabilities
- `trivy-runtime`: runtime state becomes feature-keyed; Trivy becomes one instance of the generic contract.
- `lifecycle-cli`: feature lifecycle commands accept `gitleaks`.
- `operator-admin-tui`: per-feature runtime status plus a secret-findings surface.

## Approach

Generalize the seam first, then add Gitleaks on top of it, so the second feature lands as configuration rather than a parallel implementation. Preserve Trivy's proven sequence (resolve, verify SHA256, extract, activate atomically, probe, roll back on failure), parameterized by feature identity, asset naming, and binary name.

Execution is where the two features genuinely diverge. `ScanRunner.Run(ctx, imageRef, settings)` fits Trivy because Trivy pulls over Regixtry's own HTTP protocol; Gitleaks consumes a local path. Regixtry has layer access via `ListManifestBlobs` and `OpenBlob` but no rootfs-assembly utility. `sdd-design` decides whether to broaden `ScanRunner` or add a filesystem-oriented contract.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modified | Feature-parameterized runtime-state contract |
| `internal/infra/metadata/sqlite/store.go` | Modified | `feature` column, `(tenant, feature)` conflict key |
| `internal/app/regixtry/{service.go,feature_registry.go}` | Modified | Managers keyed by feature; fix `projectFeatureRuntime` |
| `internal/infra/scanning/gitleaks/` | New | Release client, runtime manager, scan runner |
| `internal/infra/install/releases/github.go` | Modified | Extract shared binary-staging component |
| `internal/protocol/http/`, `internal/tui/` | Modified | Per-feature status and secret findings |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Gitleaks cannot scan extension-less blobs via `--max-archive-depth` | Med | Spike in design; fall back to assembling a directory |
| Gitleaks CLI surface shifts (`dir` replaced `detect --no-git` in 8.19.0) | Med | Pin a version, verify flags against that binary |
| Generalization regresses working Trivy behavior | Med | Trivy runtime tests must pass unchanged |
| Stored findings expose credential material | Med | Decide redaction policy in spec before persisting |

## Rollback Plan

Revert the metadata-port and feature-registry changes and drop the `gitleaks` descriptor from `builtInFeatures`. The `feature` column is additive and can stay unused. Trivy's `features/trivy/` layout and receipts are untouched, so a rolled-back deployment keeps its active Trivy version.

## Dependencies

- Gitleaks official release assets plus checksums.
- Existing scan orchestration, feature-config, admin, and TUI seams.

## Success Criteria

- [ ] `ListFeatures` reports each feature's own runtime state; no feature shows another's.
- [ ] Gitleaks installs, upgrades, rolls back, and reports status through the same operator surfaces as Trivy.
- [ ] Scanning an image containing a known test secret produces a finding attributed to that image.
- [ ] Only one GitHub-release download/verify/extract implementation remains.

## Proposal question round — resolved

Confirmed by the user on 2026-08-11, all recommended defaults accepted:

1. **Redaction**: a stored finding includes rule ID and location (layer, path, line) only — never the matched secret or a fingerprint. The findings store must not become a second custody point for live credentials.
2. **Trigger**: reuse Trivy's existing manual and scheduled rescan orchestration. No separate push-time scan path in this change.
3. **Blocking**: findings are informational only in this change. No severity/policy dimension, no pull/promotion gating.
4. **History**: scan only the layers of the manifest currently under scan, matching Trivy's existing scope. Layers reachable only from older/superseded tags are out of scope.
5. **Config blob** (resolved 2026-08-11, after `sdd-design` surfaced the gap): scan scope includes the manifest's config blob (declared `ENV`, build history) in addition to its layer blobs. Container `ENV` values are a common, Trivy-uncovered leak vector for credentials, so excluding the config blob would leave an obvious gap in what "secret scanning" is expected to mean here.

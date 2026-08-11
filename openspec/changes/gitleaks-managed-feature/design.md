# Design: Gitleaks Managed Feature

## Technical Approach

Two layers. First, de-Trivy-ise the managed-feature seam: runtime state, feature settings, and runtime managers all become keyed by `(tenant, feature)`, and the duplicated GitHub-release primitives move to one package. Second, add `internal/infra/scanning/gitleaks` as a second instance of that seam, with its own execution contract because gitleaks consumes a local path, not a registry reference.

## Architecture Decisions

| # | Decision | Rejected alternative | Rationale |
|---|---|---|---|
| 1 | Rename `ports.TrivyRuntimeState`/`TrivyRuntimeStatus*` to `FeatureRuntimeState`/`FeatureRuntimeStatus*`; port becomes `Get/UpsertFeatureRuntimeState(ctx, tenant, feature)` | Keep Trivy names, add `feature` param | The type name is the lie; a `TrivyRuntimeState` describing gitleaks keeps leaking into TUI/HTTP naming. |
| 2 | New sqlite table `feature_runtime_state`, `PRIMARY KEY(tenant, feature)`; old `trivy_runtime_state` left in place, unread | `ALTER TABLE` + backfill; drop old table | No production data and no shim in scope. A new name needs no migration code and destroys nothing. |
| 3 | `scan_settings` also becomes `(tenant, feature)`: `Get/UpsertScanSettings(ctx, tenant, feature)` | Leave one row | Not optional: today `SetFeatureEnabled("gitleaks", …)` would flip Trivy's row — same bug class as `projectFeatureRuntime`. Trivy-only fields stay blank for gitleaks. |
| 4 | `Service.runtimes map[string]FeatureRuntimeManager`; `SetFeatureRuntimeManager(feature, m)`; `projectFeatureRuntime(ctx, feature)`; lifecycle methods resolve by name | Slice of managers; a `Name()` on the interface | Map lookup keeps `ListFeatures` honest per row and keeps `main.go` wiring explicit. |
| 5 | Extract only primitives to `internal/infra/release`: `ResolveAsset(ctx, AssetQuery)`, `VerifyChecksum`, `ExtractBinary(ctx, archive, dir, name)`. Self-update keeps its own orchestration | Full unification of both call sites | The duplication is the primitives; the orchestration differs (process replacement, systemd backups). `ExtractBinary` matches on `filepath.Base(header.Name)` so Trivy/gitleaks archives with README/LICENSE work; self-update keeps its stricter single-entry pre-check. |
| 6 | New port `SecretScanRunner`, not a broadened `ScanRunner` | Widen `ScanRunner.Run` | Trivy (registry ref over HTTP) and gitleaks (staged blobs) share nothing but `settings`; widening makes every implementation ignore half its input. |
| 7 | Stage each blob by copying `BlobStore.OpenBlob` into `work/<run>/scan/layers/<NNN>-<digest12><ext>`, `ext` from the manifest's declared `mediaType` | Symlink/hardlink to a blob path; rely on content sniffing | `BlobStore` exposes no path, so no local path may be assumed, and gitleaks does not follow symlinks by default. The manifest supplies the extension, so archive detection never depends on sniffing an extension-less digest file. Unknown mediaType is recorded as skipped, never silently dropped. |
| 8 | Config blob staged as `scan/config/config.json`, scanned as a plain file | Layers only | It belongs to the manifest under scan, and image-config `Env` is the most common leak site. |
| 9 | Findings persist to new `secret_scan_runs` + `secret_scan_findings`; the trigger reuses `executeScanRun` | Discriminator column on `scan_runs` | Leaves Trivy's severity columns and queries untouched, keeping "Trivy tests pass unchanged" verifiable. |
| 10 | Structural redaction: the decoder struct declares only `RuleID/Description/File/StartLine/EndLine/Tags`, so `encoding/json` discards `Secret`, `Match`, `Fingerprint`, `Entropy`. `--redact` is also passed | Decode everything, strip before persist | A caller cannot leak a field the type does not have; the CLI flag is defence in depth, not the control. |
| 11 | Floor `minimumGitleaksVersion = "8.27.0"` (introduction of `--max-archive-depth`, PR #1872, released 2025-06-01 — verified against gitleaks' real release history during `sdd-apply` Phase 3; the original `8.24.0` estimate here was wrong, that version predates the PR), enforced at release resolve and at probe | Accept any version | 8.19.0 replaced `detect --no-git --source` with `dir`; the runner emits only the new surface. One constant, one place. |

## Data Flow

    ListFeatures ─┬─→ GetScanSettings(tenant, feature)
                  └─→ projectFeatureRuntime(ctx, feature) ─→ runtimes[feature].Status

    executeScanRun ─→ trivy ScanRunner ─→ scan_runs / scan_run_findings
                   └─→ secret scan (if gitleaks enabled + ready)
                        ListManifestBlobs ─→ OpenBlob ─→ work/<run>/layers/NNN.tar.gz
                        ─→ gitleaks dir … ─→ report.json ─→ decoder (redacting)
                        ─→ secret_scan_runs / secret_scan_findings ─→ rm -rf work/<run>

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modify | Rename runtime-state types; feature-param on runtime-state and scan-settings methods; add `SecretScanRunner`, `SecretScanTarget`, `SecretScanResult`, `SecretFinding`, secret-run store methods |
| `internal/infra/metadata/sqlite/store.go` | Modify | `feature_runtime_state`, feature-keyed `scan_settings`, `secret_scan_runs`, `secret_scan_findings` |
| `internal/app/regixtry/service.go` | Modify | `runtimes` map, keyed setter, `secretScanRunner` field |
| `internal/app/regixtry/feature_registry.go` | Modify | `gitleaks` descriptor; `projectFeatureRuntime(ctx, feature)`; per-feature `buildFeaturePage` sections |
| `internal/app/regixtry/feature_runtime.go` | Modify | Resolve manager by feature name |
| `internal/app/regixtry/service_scanning.go` | Modify | Feature-scoped settings resolution; secret-scan leg in `executeScanRun` |
| `internal/infra/release/github.go` | Create | Shared resolve/verify/extract primitives |
| `internal/infra/scanning/trivy/releases.go` | Modify | Delegate to `internal/infra/release`; drop `extractTrivyBinary` |
| `internal/infra/install/releases/github.go` | Modify | Delegate resolve/verify/extract; keep self-update orchestration |
| `internal/infra/scanning/gitleaks/{releases,runtime_manager,runner,report}.go` | Create | Release query, lifecycle, staging + exec, redacting decoder |
| `cmd/regixtry/main.go` | Modify | Register both runtime managers; wire `SecretScanRunner` with `BlobStore` |

## Interfaces / Contracts

```go
type SecretScanRunner interface {
    Run(ctx context.Context, target SecretScanTarget, settings ScanSettings) (SecretScanResult, error)
}

type SecretScanTarget struct {
    Repository string
    Digest     string
    Blobs      []domain.Descriptor // manifest config + layers, in manifest order
}

type SecretFinding struct {
    RuleID      string   // never Secret/Match/Fingerprint
    Description string
    BlobDigest  string
    Path        string   // path inside the layer archive
    StartLine   int
    EndLine     int
    Tags        []string
}
```

Exec surface (single source of truth in `gitleaks/runner.go`):

```
<active>/gitleaks dir <work>/<run>/scan \
  --report-format json --report-path <work>/<run>/report.json \
  --no-banner --redact --exit-code 0 --max-archive-depth 2
```

`--exit-code 0` makes "leaks found" a success, so any non-zero exit is a real failure. mediaType→extension: `*.tar+gzip`/`*.tar.gzip`→`.tar.gz`, `*.tar`→`.tar`, `*.tar+zstd`→`.tar.zst`, `*config.v1+json`→`.json`.

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Unit | Feature-keyed state/settings isolation | sqlite: write trivy + gitleaks rows, assert neither reads the other |
| Unit | `projectFeatureRuntime` per feature | `ListFeatures` with two fake managers reporting different versions |
| Unit | Redaction | Decode a report containing `"Secret":"AKIA…"`; assert no `SecretFinding` field contains that substring |
| Unit | Staging | Fake `BlobStore`; assert filenames, extensions, unknown-mediaType skip, teardown |
| Unit | Version floor / argv | Fake exec captures argv; reject `8.18.0`; assert `dir` + flag set |
| Unit | Shared primitives | Existing Trivy sidecar/checksum tests re-pointed at `internal/infra/release`, assertions unchanged |
| Integration | End-to-end secret scan | tar.gz fixture with a known test secret through a fake exec emitting a real report shape |
| Regression | Trivy unchanged | `trivy`, `service`, `router`, `admin_handlers` suites pass with only mechanical renames |

## Threat Matrix

| Boundary | Adversarial cases | Applicability | Design response | Planned RED tests |
|---|---|---|---|---|
| Documentation-like paths | Layer contains `README.sh`, `CMakeLists.txt`, executable Markdown | Applicable | Staged content is data only; nothing extracted is ever executed. Only the managed gitleaks binary is executed, and only from `features/gitleaks/` (mirrors `managedBinaryPath`) | Staged fixture containing `README.sh` scans as data; execution of any staged path fails |
| Executable-file classification | Archive entry with mode `0755`; symlink/hardlink entries; `../` path traversal | Applicable | Staging writes only whole blobs (never per-entry extraction) with mode `0600`; gitleaks does archive traversal in-process | Blob staged `0600`; run dir tree contains no entry outside `work/<run>/` |
| Binary provenance | Attacker-supplied `binary_path`; unverified release asset | Applicable | Checksum verified before extract/activate; execution path guard restricted to the managed layout | Checksum mismatch aborts install; binary outside `features/gitleaks/` refuses to run |
| Argument composition | Digest/path interpolated into argv; leaked secret in argv or logs | Applicable | argv is a fixed literal slice plus adapter-generated paths — no operator string reaches argv; report goes to a file, never stdout | argv snapshot test; assert no finding value appears in argv or error strings |
| Git repository selection | `git -C`, relative/absolute repo paths | N/A | The `dir` subcommand is used exclusively; no VCS mode is reachable |
| Commit / push / PR state | staged, `commit -a`, refspecs | N/A | No VCS or PR automation in this change |

## Migration / Rollout

No data migration. `feature_runtime_state` and feature-keyed `scan_settings` are created empty, so after deploy each feature reports `uninstalled` until `regixtry feature runtime install <name>` runs — including Trivy, once. That re-install is idempotent and takes seconds; the old `trivy_runtime_state` row survives untouched for rollback.

## Open Questions — resolved

- [x] Spec alignment (resolved 2026-08-11): user confirmed scanning the image **config blob** in addition to layers is in scope; `specs/image-secret-scans/spec.md` was corrected to match decision 8.
- [x] `minimumGitleaksVersion` (resolved during `sdd-apply` Phase 3): verified as `8.27.0` against gitleaks' real release history, not the `8.24.0` estimate originally written here.

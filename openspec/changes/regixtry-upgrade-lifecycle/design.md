# Design: Regixtry Upgrade Lifecycle

## Technical Approach

Implement `regixtry upgrade` as a sibling Linux lifecycle path, not a `setup` rerun. `cmd/regixtry/main.go` will parse upgrade flags and delegate to an upgrade backend that: reconstructs installed intent from lifecycle provenance plus `regixtry.env`, resolves a target release, stages and verifies the replacement binary, snapshots managed artifacts, swaps binary/env/unit, restarts the recorded service, probes `/v2/`, and rolls back on failure. This satisfies the `lifecycle-cli` and `installation-modes` deltas while preserving current setup/uninstall ownership.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Upgrade backend | Add `internal/infra/install/linux/upgrade.go` plus helpers, separate from `Bootstrapper.Run()` | Reuse `Run()` directly | `writeArtifacts()` recreates `metadata.db`; upgrade must reuse rendering/probing logic without touching data files. |
| Release resolution | Add `internal/infra/install/releases/github.go` for latest/tag lookup, asset selection, checksum verification, and archive validation mirroring `install.sh` | Shell out to `install.sh` | Keeps lifecycle truth in Go, improves rollback control, and avoids split error semantics. |
| Intent source | Read provenance first, then recover missing fields from `regixtry.env` and classified `ManagedPaths` | Require re-entry; trust env only | Existing v1 provenance is incomplete, but env already carries public URL, addr, DB path, TLS files, and auth DSN. |
| Provenance evolution | Introduce v2 provenance with structured intent and last-installed ref/version; reader accepts v1 and upgrades in memory | Breaking format change | Successful upgrades can enrich future runs without breaking installed fleets. |

## Data Flow

`regixtry upgrade [--ref]`
→ load provenance + env
→ infer unit/env/bin/data paths + runtime TLS/auth intent
→ resolve release + download archive/checksums
→ verify sha256 + single `regixtry` payload
→ snapshot current binary/env/unit
→ `systemctl disable --now`
→ atomic binary rename/swap + env/unit rewrite + `daemon-reload`
→ `systemctl enable --now` + `/v2/` probe
→ write v2 provenance on success
→ else restore snapshot and restart previous binary

## File Changes

| File | Action | Description |
|---|---|---|
| `cmd/regixtry/main.go` | Modify | Add `parseUpgradeConfig`, `runUpgrade`, risky-prompt gating, and sudo-safe rerun guidance. |
| `cmd/regixtry/main_test.go` | Modify | Replace deferred-upgrade coverage with CLI contract tests. |
| `internal/infra/install/linux/upgrade.go` | Create | Orchestrate intent loading, staging, swap, restart, readiness, and rollback reporting. |
| `internal/infra/install/linux/intent.go` | Create | Reconstruct runtime intent from provenance/env and classify managed paths. |
| `internal/infra/install/linux/provenance.go` | Modify | Add v2 fields, backward-compatible loader, and success-only persistence. |
| `internal/infra/install/linux/bootstrap.go` | Modify | Extract shared render/probe helpers and separate setup-only DB creation from upgrade-safe artifact regeneration. |
| `internal/infra/install/releases/github.go` | Create | GitHub release API/download/checksum/archive helpers mirroring installer semantics. |
| `internal/infra/install/releases/github_test.go` | Create | Verify latest/tag resolution, checksum failures, and archive validation. |
| `internal/infra/install/linux/upgrade_test.go` | Create | Validate preservation, rollback, and partial-failure truthfulness. |
| `install.sh` | Modify | Keep downloader semantics aligned with the Go release helper and operator messaging. |

## Interfaces / Contracts

```go
type UpgradeConfig struct { Ref, ProvenancePath string; AssumeYes bool }
type InstalledIntent struct { Addr, PublicURL, RuntimeTLSMode, TLSCertFile, TLSKeyFile, AuthPostgresDSN, StorageRoot, DatabasePath, EnvPath, UnitPath, BinaryPath, ServiceName, InstalledRef string }
type UpgradeSnapshot struct { BinaryBackupPath string; EnvBody, UnitBody []byte }
type ReleaseAsset struct { Tag, ArchiveURL, ChecksumsURL string }
```

`AuthPostgresDSN` stays opaque: upgrade preserves the exact string, including host, db name, credentials, and `sslmode`. TLS mode is reconstructed with `ResolveRuntimeTLSMode`; existing installs with no DSN remain auth-disabled.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Provenance v1→v2 loading, env parsing, path classification, checksum/archive validation | Table-driven tests in `intent.go`, `provenance.go`, `github.go`. |
| Integration | Stage-before-stop, no `metadata.db` rewrite, restart/probe success, rollback restore | Stub file ops + `systemctl`/probe flows in `upgrade_test.go`. |
| E2E | Managed install upgraded by latest and explicit ref | Extend lifecycle smoke coverage after a real setup-managed install. |

## Threat Matrix

Reference file `references/threat-matrix.md` is absent; applying the required matrix inline.

| Boundary | Applicability | Safe / failure behavior | Planned RED test |
|---|---|---|---|
| Routing | N/A — no HTTP route contract change | — | — |
| Shell commands | Applicable | `install.sh` stays downloader-only; no shell delegation from `upgrade`. | CLI tests prove Go owns upgrade flow. |
| Subprocesses | Applicable | `systemctl` runs only after staging; failure triggers restore report. | Stop/start/daemon-reload failure cases. |
| VCS/PR automation | N/A — no VCS integration | — | — |
| Executable-file classification | Applicable | Accept only verified archive with single `regixtry` entry. | Wrong checksum / extra-entry archive fails before stop. |
| Process integration | Applicable | Success requires restart + `200/401` probe; rollback restarts prior binary. | Probe failure restores previous runnable state. |

## Migration / Rollout

No data migration for `metadata.db`. Rollout writes v2 provenance only after a successful upgrade; readers continue accepting v1 so existing installs remain upgradeable.

## Open Questions

- [ ] Should major-version upgrades require explicit confirmation even with `--yes` absent only, or always unless `--yes` is set?
- [ ] If env and provenance disagree on unit/binary path, should upgrade hard-stop or offer guided recovery?

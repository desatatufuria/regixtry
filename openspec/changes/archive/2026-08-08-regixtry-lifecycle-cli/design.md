# Design: Regixtry Lifecycle CLI

## Technical Approach

Move lifecycle UX into `cmd/regixtry/main.go` while keeping `internal/infra/install/linux/bootstrap.go` as the only truthful phase-1 setup backend. `install.sh` becomes downloader-only after verified binary placement; operators then run `regixtry setup` or `regixtry uninstall`. The design adds a separate lifecycle provenance record so uninstall can remove the installed binary and recorded service/runtime artifacts without replaying old flags. This matches the proposal and the `lifecycle-cli` / `installation-modes` specs.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Lifecycle surface | Add `setup`, `uninstall`, and deferred `upgrade` handling in `cmd/regixtry/main.go` | Keep UX in `install.sh`; expose only richer `bootstrap` flags | Keeps operator UX in the binary and preserves `bootstrap` as a lower-level scriptable backend. |
| Uninstall state | Add a new lifecycle provenance file, separate from the bootstrap receipt | Reuse receipt only; require uninstall flags | The current receipt lacks binary provenance and truthful status reporting scope. Separate provenance keeps rollback backend small and uninstall explicit. |
| Cleanup engine | Keep bootstrap rollback for backend rollback; add uninstall-specific reporting flow | Make `Rollback` do full uninstall | Rollback and uninstall now have different promises: internal cleanup vs operator-facing best-effort reversal. |

## Data Flow

`install.sh` download/verify ──→ installed `regixtry`
                               └──→ `regixtry setup`
                                        ├── validate mode + TTY contract
                                        ├── `daemon-sqlite` → bootstrap backend
                                        │                      ├── write env/unit/db/content/receipt
                                        │                      ├── `systemctl enable --now`
                                        │                      └── probe `/v2/`
                                        └── write lifecycle provenance on success

`regixtry uninstall`
  └── read lifecycle provenance ──→ stop/disable recorded service ──→ remove recorded paths ──→ remove installed binary last ──→ print truthful report

## File Changes

| File | Action | Description |
|---|---|---|
| `cmd/regixtry/main.go` | Modify | Add lifecycle subcommands, TTY mode resolution, deferred `upgrade`, and uninstall/setup orchestration. |
| `cmd/regixtry/main_test.go` | Modify | Cover command parsing, interactive/non-interactive rules, deferred upgrade, and uninstall reporting. |
| `internal/infra/install/linux/bootstrap.go` | Modify | Enrich receipt/provenance inputs and expose enough normalized state for lifecycle orchestration without duplicating path logic. |
| `internal/infra/install/linux/provenance.go` | Create | Define lifecycle provenance/load-save helpers and uninstall report model. |
| `internal/infra/install/linux/provenance_test.go` | Create | Cover drifted/missing-path reporting and binary-last cleanup ordering. |
| `install.sh` | Modify | Stop owning mode selection; remain release verification + binary placement only. |
| `docs/verification/scripts/install-release-smoke.sh` | Modify | Re-cut smoke coverage for downloader-only shell plus binary lifecycle flows. |
| `README.md` | Modify | Document setup/uninstall truthfully and keep Linux + systemd boundary explicit. |

## Interfaces / Contracts

```go
type LifecycleProvenance struct {
	Version       int      `json:"version"`
	Mode          string   `json:"mode"`
	InstalledBin  string   `json:"installed_bin"`
	ServiceName   string   `json:"service_name"`
	StatePath     string   `json:"state_path"`
	ManagedPaths  []string `json:"managed_paths"`
}

type CleanupItem struct {
	Path   string
	Status string // removed | missing | skipped | failed
	Detail string
}
```

Default command contract:
- `regixtry setup --mode daemon-sqlite|binary-only [bootstrap flags...]`
- `regixtry uninstall [--state-path <provenance-path>]`
- `regixtry upgrade` prints deferred status and exits non-zero.

TTY contract:
- Missing `--mode` + interactive stdin/stdout TTY → prompt for `binary-only` vs `daemon-sqlite`.
- Missing `--mode` without TTY → fail with explicit guidance.
- `binary-only` prints next steps only, writes no lifecycle provenance, and MUST NOT claim setup success.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Mode resolution, TTY gating, deferred upgrade, provenance serialization | Add focused Go table tests in `cmd/regixtry/main_test.go` and `provenance_test.go`. |
| Integration | Setup success/failure, uninstall truthfulness on drift, binary-last cleanup | Extend Linux bootstrap tests with fake `systemctl`, probe, and remove failures. |
| E2E | Downloader-only installer plus binary lifecycle UX | Update `docs/verification/scripts/install-release-smoke.sh` to run `regixtry setup` / `uninstall` after install. |

## Threat Matrix

Reference file `references/threat-matrix.md` is absent in this repo, so the required boundary review is applied inline.

| Boundary | Applicability | Safe / failure behavior | Planned RED test |
|---|---|---|---|
| Routing | N/A | No product HTTP routing change. | None. |
| Shell commands | Applicable | `install.sh` stays quoted and downloader-only; setup/uninstall messaging moves into Go. | Installer no longer prompts or shells into lifecycle mode selection. |
| Subprocesses | Applicable | Only explicit `systemctl` commands run; unsupported mode/host fails before claiming success. | Stub `enable --now`, `disable --now`, and TTY-missing cases. |
| VCS/PR automation | N/A | No git/GitHub automation change. | None. |
| Executable-file classification | Applicable | Release checksum/archive validation remains in `install.sh`; uninstall removes only the recorded installed binary. | Wrong archive still fails before install; uninstall ignores unrecorded binaries. |
| Process integration | Applicable | Success requires service activation plus `/v2/` reachability; uninstall reports drift instead of overclaiming reversal. | Probe failure, missing provenance entries, and already-deleted artifact cases. |

## Migration / Rollout

No data migration required. Roll out in one slice: ship downloader-only `install.sh`, new lifecycle commands, and docs together so shell and binary ownership do not diverge.

## Open Questions

- [ ] Should `regixtry uninstall` default to a fixed provenance path only, or also probe legacy bootstrap receipt locations for friendlier migration?

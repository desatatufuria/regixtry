# Proposal: Regixtry Lifecycle CLI

## Intent

Move lifecycle UX from `install.sh` into the `regixtry` binary so phase 1 delivers reversible installs: binary installed, `daemon-sqlite` service running, and registry reachable on supported Linux + systemd hosts.

## Scope

### In Scope
- Make `install.sh` downloader-only after verified binary placement.
- Add `regixtry setup` as the phase-1 lifecycle entrypoint for `binary-only` guidance and truthful `daemon-sqlite` setup.
- Add provenance-backed `regixtry uninstall` with best-effort cleanup and truthful reporting for partial or drifted state.

### Out of Scope
- `regixtry upgrade` implementation; reserve it for the immediate follow-up phase.
- Postgres-backed installation, uninstall, or upgrade flows.

## Capabilities

### New Capabilities
- `lifecycle-cli`: Binary-owned lifecycle commands for setup, uninstall, and future upgrade namespace.

### Modified Capabilities
- `installation-modes`: Success, rollback, and support boundaries shift from shell-led bootstrap toward binary-led lifecycle ownership on Linux + systemd.

## Approach

Keep `internal/infra/install/linux/bootstrap.go` as the truthful `daemon-sqlite` backend, but move operator choice, lifecycle messaging, and uninstall entrypoints into `cmd/regixtry/main.go`. Record lifecycle provenance beyond the current bootstrap receipt so uninstall can remove known artifacts without replaying old parameters.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `install.sh` | Modified | Downloader/installer only; no lifecycle chooser |
| `cmd/regixtry/main.go` | Modified | Add `setup` and `uninstall` lifecycle UX |
| `internal/infra/install/linux/bootstrap.go` | Modified | Reuse setup backend; extend provenance inputs |
| `docs/verification/scripts/install-release-smoke.sh` | Modified | Verify downloader-only install plus binary lifecycle flow |
| `README.md` | Modified | Reframe install, uninstall, and support truthfully |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Uninstall overclaims cleanup on drifted hosts | Med | Persist provenance and report exact removed/skipped items |
| Shell/binary UX diverges during transition | Med | Make `install.sh` downloader-only in the same slice |
| Scope expands into upgrade or Postgres too early | Med | Keep both explicitly deferred in proposal and later specs |

## Rollback Plan

Revert CLI lifecycle entrypoints, restore `install.sh` chooser behavior, and keep existing bootstrap rollback semantics while leaving already installed binaries untouched.

## Dependencies

- Existing `daemon-sqlite` bootstrap backend remains the only truthful automated deployment path.

## Success Criteria

- [ ] Phase 1 defines setup success as binary installed, service running, and registry reachable.
- [ ] Uninstall removes recorded lifecycle artifacts without requiring prior operator parameters.
- [ ] Product language stays Linux + systemd only and defers upgrade/Postgres follow-up work.

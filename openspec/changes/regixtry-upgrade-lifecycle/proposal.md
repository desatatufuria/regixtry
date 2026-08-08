# Proposal: Regixtry Upgrade Lifecycle

## Intent

Make `regixtry upgrade` a truthful in-place Linux + systemd lifecycle command that preserves installed operator intent, upgrades to latest or explicit versions, validates restart health, and rolls back automatically on failure.

## Scope

### In Scope
- Real `regixtry upgrade` CLI flow for lifecycle-managed installs.
- Intent reconstruction from provenance + `regixtry.env`, with compatibility for existing installs.
- Staged download, checksum verification, binary swap, restart, readiness check, and rollback.

### Out of Scope
- Non-Linux or non-systemd upgrade paths.
- New runtime modes, config reshaping, or installer UX redesign.

## Capabilities

### New Capabilities
- None

### Modified Capabilities
- `lifecycle-cli`: replace deferred upgrade behavior with a real operator upgrade contract.
- `installation-modes`: extend lifecycle truthfulness/rollback requirements to in-place upgrades.

## Approach

Add a sibling upgrade path instead of reusing setup blindly. The command should load current runtime intent from lifecycle provenance plus env, resolve target version (`latest` default, explicit ref optional), stage and verify the new binary, stop service only after staging succeeds, atomically replace managed artifacts, restart, probe `/v2/`, and restore prior binary/env/unit on failure.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` | Modified | Real upgrade CLI, silent-by-default prompts |
| `internal/infra/install/linux/bootstrap.go` | Modified | Upgrade-safe lifecycle orchestration |
| `internal/infra/install/linux/provenance.go` | Modified | Backward-compatible intent/version provenance |
| `install.sh` | Modified | Align release selection semantics |
| `openspec/specs/lifecycle-cli/spec.md` | Modified | Upgrade contract |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Data or config clobber during upgrade | Med | Never recreate DB; preserve env/unit/data paths |
| Broken service after binary swap | Med | Stage first, backup previous binary, health-check rollback |

## Rollback Plan

Restore the previous binary and prior generated env/unit artifacts, restart the recorded service, and leave provenance untouched if the new version fails staging, restart, or readiness validation.

## Dependencies

- Existing lifecycle provenance and `regixtry.env`
- GitHub release/checksum source used by current installer

## Success Criteria

- [ ] Same-shape upgrades complete without reconfiguration prompts.
- [ ] Explicit or latest version selection upgrades a lifecycle-managed install safely.
- [ ] Failed restart or health validation automatically restores the prior runnable state.

## Proposal question round

- Confirm whether major-version upgrades should always require operator confirmation.
- Confirm whether missing env/provenance fields should hard-stop or allow guided recovery.

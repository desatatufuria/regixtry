# Proposal: Registry Bootstrap Preflight

## Intent

Remove late bootstrap failures by detecting an occupied configured local bind address before service start, and by giving operators exact recovery steps instead of post-start ambiguity.

## Scope

### In Scope
- Add fail-fast preflight for the configured local bind address before default service start.
- Add an explicit `--no-start` path that generates artifacts without starting the service or running port preflight.
- Return exact operator recovery commands when bind preflight fails.

### Out of Scope
- Remote/public reachability checks beyond the configured local bind case.
- Broader environment diagnostics, automatic port reassignment, or service health redesign.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `installation-modes`: bootstrap success/failure rules change to include local bind preflight, explicit no-start behavior, and operator-facing recovery guidance.

## Approach

Keep the current `daemon-sqlite` bootstrap contract, but insert a local bind-address availability check before `systemctl enable --now`. Run that check only when bootstrap will start the service. If the address is occupied, fail before service activation and print exact commands to inspect and free or change the port. `--no-start` skips activation and preflight while still generating bootstrap artifacts.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `openspec/specs/installation-modes/spec.md` | Modified | Update bootstrap contract and success criteria. |
| `cmd/registry/main.go` | Modified | Extend bootstrap CLI contract with `--no-start`. |
| `internal/infra/install/linux/bootstrap.go` | Modified | Add preflight gate and failure guidance before service start. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Preflight blocks valid non-local setups | Low | Limit checks to configured local bind addresses only. |
| Operator confusion about skipped checks in `--no-start` | Medium | State clearly that artifacts were generated and startup validation was not run. |

## Rollback Plan

Remove the preflight gate and `--no-start` path, restoring bootstrap to immediate service activation after artifact generation.

## Dependencies

- Existing `daemon-sqlite` bootstrap flow and `installation-modes` spec.

## Success Criteria

- [ ] Occupied configured local bind addresses fail before service start.
- [ ] Failure output includes exact recovery commands for operators.
- [ ] `--no-start` completes with artifacts generated and service left stopped.

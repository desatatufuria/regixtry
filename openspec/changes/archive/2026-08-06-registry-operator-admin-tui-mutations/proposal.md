# Proposal: Registry Operator Admin TUI Mutations

## Intent

Add a safe first admin write path so operators can enable or disable users from the existing TUI users screen instead of switching to API tooling for simple account state changes.

## Scope

### In Scope
- Enable and disable user actions from the existing authenticated users screen.
- Explicit confirmation before mutation, plus clear success, conflict, expiry, and validation feedback.
- Post-mutation users refresh over the existing authenticated backend client.

### Out of Scope
- Password reset, grant writes, user creation, and admin-token writes.
- Batch actions, inline editing, extra detail panes, or local admin shortcuts/bypasses.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: extend the read-only admin browsing slice to allow confirmed user enable/disable mutations from the users screen while preserving backend-enforced safety rules.

## Approach

Extend the existing authenticated admin client with enable/disable write calls to the current backend routes, then add a narrow users-screen confirmation and status flow in the TUI model. Keep all authority in the backend: self-disable and last-active-admin disable remain server-enforced conflicts surfaced clearly in the TUI, with no local bypass logic.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/tui/admin_client.go` | Modified | Add enable/disable mutation calls and reuse auth/error normalization. |
| `internal/tui/model.go` | Modified | Add confirmation, mutation state, feedback, and refresh behavior on the users screen. |
| `internal/tui/model_test.go` | Modified | Update expectations from read-only users browsing to safe mutation behavior. |
| `openspec/specs/operator-admin-tui/spec.md` | Modified | Change product requirements for the first mutation slice. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Conflict outcomes feel ambiguous | Med | Preserve distinct TUI feedback for self-disable/last-active-admin conflicts. |
| Write-path UX grows beyond first slice | Low | Keep scope to users screen only; defer all other mutations. |

## Rollback Plan

Revert TUI mutation affordances and client write calls, restoring the prior authenticated read-only admin users flow while leaving backend mutation endpoints unchanged.

## Dependencies

- Existing authenticated admin backend routes for `:enable` and `:disable` remain the source of truth.

## Success Criteria

- [ ] Operators can confirm and execute user enable/disable from the authenticated users screen.
- [ ] Self-disable, last-active-admin, expiry, and validation failures surface as clear recoverable TUI feedback.

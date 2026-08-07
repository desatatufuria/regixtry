# Proposal: Registry Deployment Installer

## Intent

Make the release installer truthful about deployment choices. Operators need an interactive-first entrypoint that separates "install the binary" from "leave it running as a service" without implying Postgres-auth or container automation already exists.

## Scope

### In Scope
- Add a TTY-first deployment chooser that requires an explicit choice each run.
- Support `binary only` with verified install success plus clear manual next steps.
- Support `binary + daemon/service` only on Linux + systemd by reusing `daemon-sqlite` unchanged.

### Out of Scope
- Postgres-auth installer orchestration.
- Containerized or all-in-one automated installs.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `installation-modes`: expand the installer contract from flag-driven bootstrap only into a truthful deployment chooser with explicit `binary only` and `binary + daemon/service` outcomes.

## Approach

Keep `install.sh` as the release entrypoint. Add a thin interactive chooser in TTY mode, preserve flags/env for non-interactive automation, route the service branch to the existing `registry bootstrap --mode daemon-sqlite`, and show manual-today guidance for deferred modes.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `install.sh` | Modified | Add explicit deployment choice and next-step guidance |
| `openspec/specs/installation-modes/spec.md` | Modified | Re-baseline current facts and chooser behavior |
| `README.md` | Modified | Document truthful deployment paths and deferred modes |
| `docs/verification/scripts/install-release-smoke.sh` | Modified | Cover chooser branches and non-interactive safety |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| False support claims for deferred modes | High | Show only supported choices; frame others as manual today / automated later |
| Interactive flow breaks automation | Med | Keep flags/env authoritative and gate prompts on TTY |

## Rollback Plan

Remove chooser prompts, restore the prior install path, and keep the shipped `daemon-sqlite` bootstrap contract unchanged.

## Dependencies

- Existing `registry bootstrap --mode daemon-sqlite` backend and release installer flow.

## Success Criteria

- [ ] Operators must choose explicitly between `binary only` and `binary + daemon/service` in interactive TTY runs.
- [ ] `binary only` ends with installed binary plus clear next-step guidance.
- [ ] `binary + daemon/service` succeeds only on Linux + systemd through the unchanged `daemon-sqlite` path.
- [ ] Deferred Postgres-auth and container paths are documented as manual today, automated later.

# Proposal: Registry Installation Modes

## Intent

Turn installation from "binary only" into a truthful single-node deployment bootstrap. The first slice must let a self-hoster install the released binary, generate the required `daemon + SQLite` runtime artifacts, start the service by default, and reach a running registry on Linux without pretending container or Postgres modes are ready.

## Scope

### In Scope
- Add a flag-driven bootstrap contract for `--mode daemon-sqlite`.
- Generate minimum Linux operator artifacts: runtime env/config, storage/data paths, and service unit or equivalent start script.
- Start the service by default and document how operators verify reachability.

### Out of Scope
- Interactive mode selection or menu-first UX.
- Postgres-backed auth mode and all-container installation flows.

## Capabilities

### New Capabilities
- `installation-modes`: Defines operator-facing installation/bootstrap modes, starting with Linux `daemon + SQLite`.

### Modified Capabilities
- None.

## Approach

Keep the current release-binary installer intact, then layer a deterministic bootstrap flow on top of it. Flags remain the source of truth so automation, smoke coverage, and future interactive wrappers all share one contract. The first truthful mode is Linux-only `daemon + SQLite`; later modes extend the contract instead of replacing it.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `install.sh` | Modified | Accept mode flags and invoke bootstrap flow. |
| `cmd/registry/main.go` | Modified | Align bootstrap inputs with real runtime flags. |
| `README.md` | Modified | Document installation-mode contract and reachability checks. |
| `docs/verification/scripts/` | Modified | Add smoke coverage for render/apply/start behavior. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Service-start behavior differs across Linux hosts | Med | Keep v1 explicitly Linux-only and narrow supported service contract. |
| Bootstrap over-promises future modes | Med | Document `daemon-sqlite` as the only truthful v1 mode. |
| Rollback leaves operator confusion | Low | Limit rollback to generated artifacts and document binary retention clearly. |

## Rollback Plan

Remove generated service/bootstrap artifacts and stop/disable the created service, while leaving the installed `registry` binary untouched.

## Dependencies

- Existing Linux release-binary installation path.
- Runtime flags already exposed by `cmd/registry/main.go`.

## Success Criteria

- [ ] A single-node self-hoster can run one flag-driven flow for Linux `daemon + SQLite`.
- [ ] The flow generates artifacts, starts the service by default, and leaves the registry reachable.

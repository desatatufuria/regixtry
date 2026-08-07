# Apply Progress: Registry Deployment Installer

## Mode

Standard

## Delivery

- Strategy: exception-ok
- Current work unit: 2 — Publish truthful install guidance in `README.md` and close out hybrid apply artifacts
- Review budget note: final documentation slice stays within the stated 1200-line review budget and does not widen installer feature scope

## Completed Tasks

- [x] 1.1 Extend `docs/verification/scripts/install-release-smoke.sh` fixtures to support `/dev/tty`-driven chooser runs and explicit non-TTY failure capture.
- [x] 1.2 Add failing smoke scenarios in `docs/verification/scripts/install-release-smoke.sh` for interactive explicit choice, `--mode`/`REGISTRY_INSTALL_MODE` prompt bypass, and unsupported automated mode rejection.
- [x] 2.1 Update `install.sh` help text, defaults, and arg/env parsing so installer modes are `binary-only` or `daemon-sqlite` instead of defaulting silently to service bootstrap.
- [x] 2.2 Add `install.sh` chooser helpers that detect a controlling `/dev/tty`, require an explicit interactive choice, and fail with truthful guidance when no TTY and no mode are available.
- [x] 2.3 Add the `binary-only` success branch in `install.sh`, printing installed-binary next steps plus manual-today guidance for Postgres-auth and container deployment.
- [x] 2.4 Keep `install.sh` service delegation on `registry bootstrap --mode daemon-sqlite` unchanged, including rollback semantics and binary-retention failure messaging.
- [x] 3.1 Turn Phase 1 RED cases GREEN in `docs/verification/scripts/install-release-smoke.sh` for binary-only success, service-path success, non-TTY safety, and unsupported-host/service failures.
- [x] 3.2 Add smoke assertions in `docs/verification/scripts/install-release-smoke.sh` that service rollback preserves `${install_dir}/registry` while removing generated bootstrap artifacts only.
- [x] 4.1 Rewrite `README.md` install sections around `binary only` vs `binary + daemon/service`, Linux+systemd-only automation, and manual-today deferred paths.
- [x] 4.2 Document `install.sh` mode selection, non-interactive requirements, and rollback expectations in `README.md` so published behavior matches shipped behavior.

## Remaining Tasks

- [x] None.

## Files Changed

| File | Action | Notes |
|---|---|---|
| `install.sh` | Modified | Added installer-level `binary-only` / `daemon-sqlite` resolution, `/dev/tty` chooser, non-TTY failure guidance, deferred-path messaging, and binary-only next steps while preserving `daemon-sqlite` bootstrap delegation |
| `docs/verification/scripts/install-release-smoke.sh` | Modified | Added PTY-backed chooser execution, explicit mode bypass assertions, non-TTY/unsupported-mode failures, and preserved service rollback coverage |
| `README.md` | Modified | Reframed install docs around supported installer choices, explicit non-interactive mode selection, truthful deferred guidance, and daemon/service rollback scope |
| `openspec/changes/registry-deployment-installer/tasks.md` | Modified | Marked Work Unit 2 tasks complete |
| `openspec/changes/registry-deployment-installer/apply-progress.md` | Created | Recorded cumulative hybrid apply progress for both completed work units |

## Work Unit Evidence

| Evidence | Required value |
|---|---|
| Focused test command and exact result | `bash docs/verification/scripts/install-release-smoke.sh` → exit 0; all scripted chooser, binary-only, daemon-sqlite, non-TTY, unsupported-host, and rollback assertions passed after README alignment work |
| Runtime harness command/scenario and exact result | `N/A` — this slice only aligned documentation and apply artifacts with already-shipped installer behavior; runtime boundary remained the existing smoke harness contract validated by the focused command above |
| Rollback boundary | Revert `README.md`, `openspec/changes/registry-deployment-installer/tasks.md`, and `openspec/changes/registry-deployment-installer/apply-progress.md` without touching installer/runtime logic |

## Deviations from Design

- None — implementation matches design for Work Unit 2.

## Issues Found

- `openspec/changes/registry-deployment-installer/state.yaml` is still absent, so readiness and completion had to be derived from the proposal/spec/design/tasks/apply-progress artifacts instead of persisted state.

# Tasks: Registry Deployment Installer

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 280-420 |
| 1200-line budget risk | Low |
| 400-line budget risk | Medium |
| Chained PRs recommended | No |
| Suggested split | Single PR with 2 work units |
| Delivery strategy | exception-ok |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Medium

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Add chooser-safe installer behavior in `install.sh` with smoke coverage | Single PR | `bash docs/verification/scripts/install-release-smoke.sh` | Interactive and non-TTY cases inside `install-release-smoke.sh` | Revert `install.sh` + chooser fixtures/assertions in the smoke script |
| 2 | Publish truthful install guidance in `README.md` | Single PR | `bash docs/verification/scripts/install-release-smoke.sh` | N/A - docs validated by matching shipped installer contract | Revert README install/deployment sections only |

## Phase 1: Smoke Harness Foundation

- [x] 1.1 Extend `docs/verification/scripts/install-release-smoke.sh` fixtures to support `/dev/tty`-driven chooser runs and explicit non-TTY failure capture.
- [x] 1.2 Add failing smoke scenarios in `docs/verification/scripts/install-release-smoke.sh` for interactive explicit choice, `--mode`/`REGISTRY_INSTALL_MODE` prompt bypass, and unsupported automated mode rejection.

## Phase 2: Installer Mode Resolution

- [x] 2.1 Update `install.sh` help text, defaults, and arg/env parsing so installer modes are `binary-only` or `daemon-sqlite` instead of defaulting silently to service bootstrap.
- [x] 2.2 Add `install.sh` chooser helpers that detect a controlling `/dev/tty`, require an explicit interactive choice, and fail with truthful guidance when no TTY and no mode are available.
- [x] 2.3 Add the `binary-only` success branch in `install.sh`, printing installed-binary next steps plus manual-today guidance for Postgres-auth and container deployment.
- [x] 2.4 Keep `install.sh` service delegation on `registry bootstrap --mode daemon-sqlite` unchanged, including rollback semantics and binary-retention failure messaging.

## Phase 3: Verification and Integration

- [x] 3.1 Turn Phase 1 RED cases GREEN in `docs/verification/scripts/install-release-smoke.sh` for binary-only success, service-path success, non-TTY safety, and unsupported-host/service failures.
- [x] 3.2 Add smoke assertions in `docs/verification/scripts/install-release-smoke.sh` that service rollback preserves `${install_dir}/registry` while removing generated bootstrap artifacts only.

## Phase 4: Documentation Alignment

- [x] 4.1 Rewrite `README.md` install sections around `binary only` vs `binary + daemon/service`, Linux+systemd-only automation, and manual-today deferred paths.
- [x] 4.2 Document `install.sh` mode selection, non-interactive requirements, and rollback expectations in `README.md` so published behavior matches shipped behavior.

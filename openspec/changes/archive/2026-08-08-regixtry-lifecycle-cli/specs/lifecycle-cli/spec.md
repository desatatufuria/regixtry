# lifecycle-cli Specification

## Purpose

Define binary-owned setup and uninstall UX for reversible phase-1 installs on supported Linux + systemd hosts.

## Current Repository Facts

- `cmd/regixtry/main.go` currently exposes `serve`, `tui`, `bootstrap`, and `bootstrap-admin`; it does not expose `setup` or `uninstall`.
- `install.sh` currently places the release binary only.
- `internal/infra/install/linux/bootstrap.go` already contains the truthful `daemon-sqlite` setup and rollback backend.

## Requirements

### Requirement: Binary Lifecycle Entrypoints

The `regixtry` binary MUST expose `setup` and `uninstall` lifecycle commands. `setup` SHALL be the phase-1 operator entrypoint, and `upgrade` MAY remain reserved but MUST report deferred status in this slice.

#### Scenario: Setup command is available

- GIVEN a supported operator host
- WHEN the operator runs `regixtry setup`
- THEN the system SHALL enter the binary-owned lifecycle flow

#### Scenario: Deferred lifecycle command is requested

- GIVEN phase-1 lifecycle commands are installed
- WHEN the operator requests `regixtry upgrade`
- THEN the system MUST report that upgrade is not available in this slice

### Requirement: Setup Outcome Contract

`regixtry setup` MUST use the truthful `daemon-sqlite` backend and MUST declare success only when the binary is installed, the service is running, and the registry is reachable on Linux + systemd. If any condition fails, the system MUST fail the setup result.

#### Scenario: Setup reaches phase-1 success

- GIVEN a supported Linux + systemd host
- WHEN `regixtry setup` completes binary placement, service activation, and reachability verification
- THEN the system SHALL report successful setup

#### Scenario: Setup stays failed on partial completion

- GIVEN binary placement completed
- WHEN service activation or reachability verification fails
- THEN the system MUST report setup failure

### Requirement: Provenance-Driven Uninstall

`regixtry uninstall` MUST use persisted lifecycle provenance to perform best-effort cleanup without requiring previous operator flags. The system SHALL report removed, skipped, and already-missing items truthfully, and it MUST NOT claim full cleanup for unrecorded or drifted state.

#### Scenario: Recorded install is removed

- GIVEN lifecycle provenance exists for a prior setup
- WHEN the operator runs `regixtry uninstall`
- THEN the system SHALL remove recorded artifacts best effort and report the resulting state

#### Scenario: Drifted host is handled truthfully

- GIVEN some recorded artifacts are already missing or changed
- WHEN uninstall evaluates provenance
- THEN the system MUST report skipped or missing cleanup items without overstating reversal

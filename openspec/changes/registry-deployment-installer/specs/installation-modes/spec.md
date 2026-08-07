# Delta for installation-modes

## Current Repository Facts

- `install.sh` currently installs a verified Linux release binary, then defaults into `registry bootstrap --mode daemon-sqlite`.
- `daemon-sqlite` already generates systemd + SQLite artifacts, starts the service, and probes `/v2/` on supported Linux hosts.

## ADDED Requirements

### Requirement: Deferred Mode Guidance

The installer MUST describe Postgres-auth and container deployment as manual today and automated later. It MUST NOT present them as supported automated choices.

#### Scenario: Deferred paths are explained

- GIVEN an operator wants a Postgres-auth or container deployment
- WHEN the installer shows unsupported automated paths
- THEN the system MUST label them as manual today and automated later

## MODIFIED Requirements

### Requirement: Truthful Mode Contract

The installer MUST require an explicit deployment choice on interactive TTY runs. Non-interactive runs SHALL remain flag/env driven. The only truthful automated outcomes MUST be `binary only` and `binary + daemon/service`, and any other automated mode MUST be rejected.
(Previously: the installer centered on explicit flag-driven `daemon-sqlite` bootstrap only.)

#### Scenario: Interactive operator chooses

- GIVEN a TTY operator starts the installer without a preselected mode
- WHEN deployment choices are shown
- THEN the system MUST require an explicit choice between `binary only` and `binary + daemon/service`

#### Scenario: Automation stays non-interactive

- GIVEN a non-TTY run or explicit flags/environment inputs
- WHEN the installer resolves the requested path
- THEN the system SHALL honor the provided truthful mode without prompting, or fail clearly if the request is unsupported

### Requirement: Linux Bootstrap Artifacts

For `binary + daemon/service`, the system MUST reuse the existing `daemon-sqlite` bootstrap contract unchanged. This outcome SHALL be supported only on Linux hosts with systemd in the supported distro set, and it MUST reject other host or service-manager combinations without overstating support.
(Previously: `daemon-sqlite` was the only installer mode.)

#### Scenario: Supported service host receives runnable artifacts

- GIVEN an operator selects `binary + daemon/service` on a supported Linux + systemd host
- WHEN bootstrap completes
- THEN the system SHALL leave the runtime and service artifacts ready for activation

#### Scenario: Unsupported service host is rejected truthfully

- GIVEN an operator selects `binary + daemon/service` on an unsupported host environment
- WHEN compatibility is evaluated
- THEN the system MUST fail without claiming automated service support

### Requirement: Default Service Activation and Reachability

For `binary + daemon/service`, bootstrap MUST start the service by default and installation success SHALL mean the service is installed and reachable. For `binary only`, installation success SHALL mean the verified binary is installed plus clear manual next steps, and the system MUST NOT claim service activation.
(Previously: installation success always depended on service activation.)

#### Scenario: Binary-only install succeeds without daemon activation

- GIVEN an operator selects `binary only`
- WHEN the verified release binary is installed successfully
- THEN the system SHALL report success with manual next-step guidance and no service claim

#### Scenario: Service path still requires reachability

- GIVEN an operator selects `binary + daemon/service`
- WHEN activation or readiness fails
- THEN the system MUST report installation failure

### Requirement: Scoped Rollback

Rollback for `binary + daemon/service` MUST remove generated bootstrap artifacts and stop or disable the created service, but it MUST NOT uninstall the installed `registry` binary. The `binary only` path MUST NOT claim service rollback work that was never created.
(Previously: rollback was defined around the single bootstrap-first outcome.)

#### Scenario: Service rollback preserves the binary

- GIVEN a `binary + daemon/service` install created service and runtime artifacts
- WHEN the operator invokes rollback
- THEN the system SHALL remove generated artifacts and leave the binary installed

#### Scenario: Binary-only path has no service rollback claim

- GIVEN an operator completed `binary only`
- WHEN rollback expectations are communicated
- THEN the system MUST limit guidance to the installed binary and MUST NOT claim service-artifact cleanup occurred

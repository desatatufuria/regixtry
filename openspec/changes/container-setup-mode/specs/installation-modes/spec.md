# Delta for installation-modes

## Current Repository Facts

- `resolveSetupMode`/`runSetup` (`main.go:1785-1820`, `1251-1333`) dispatch only `binary-only` and `daemon-sqlite`; no `docker` case exists.
- `daemon-sqlite` never bundles Postgres; absent `-auth-postgres-dsn` means anonymous.
- `docker-compose.yml` needs a pre-created `dtf-netwok` network, hardcodes `POSTGRES_PASSWORD: registry`, and builds locally instead of pulling the published image.
- The published image already supports `-auth-postgres-dsn` and non-interactive `bootstrap-admin`; `docker exec -it <container> regixtry tui` already works but is undocumented.

## MODIFIED Requirements

### Requirement: Truthful Mode Contract

The system MUST present install outcomes through binary-owned commands. Truthful automated modes SHALL be `binary-only`, `daemon-sqlite`, and `docker`; `daemon-sqlite` requires Linux + systemd, `docker` requires Docker Engine with the Compose v2 plugin. Unsupported modes or targets MUST fail without implying unsupported Postgres, upgrade, or orchestration support.
(Previously: `daemon-sqlite` was the only supported automated install mode.)

#### Scenario: Supported lifecycle target is selected

- GIVEN `daemon-sqlite` on Linux + systemd, or `docker` on a host with Docker and Compose v2
- WHEN the lifecycle contract is evaluated
- THEN the system SHALL continue with that mode's supported flow

#### Scenario: Unsupported mode or target is requested

- GIVEN an unsupported mode, or `docker` on a host missing Docker or Compose
- WHEN validation runs
- THEN the system MUST fail with a clear unsupported result and MUST NOT attempt orchestration

## ADDED Requirements

### Requirement: Bundled-Postgres Docker Setup

For `docker` mode with no external DSN, setup MUST start the published image plus a bundled Postgres via Compose, generate a random database password, bootstrap the first admin non-interactively, and report success only once the stack is healthy, reachable, and auth-enabled.

#### Scenario: Default docker setup produces an auth-enabled stack

- GIVEN an operator runs `docker` mode with no `-auth-postgres-dsn`
- WHEN setup completes
- THEN success SHALL require bundled Postgres, the registry, and first-admin bootstrap to all succeed, reachable with auth enabled

#### Scenario: Stack does not become reachable

- GIVEN docker mode orchestration starts the compose stack
- WHEN the registry does not become reachable
- THEN the system MUST report installation failure, not a healthy stack

### Requirement: External Postgres Opt-Out for Docker Setup

Supplying `-auth-postgres-dsn` in `docker` mode MUST skip the bundled Postgres and wire auth plus first-admin bootstrap to the operator's external instance.

#### Scenario: External DSN skips the bundled database

- GIVEN an operator runs `docker` mode with `-auth-postgres-dsn` pointing at an existing Postgres instance
- WHEN setup completes
- THEN the system SHALL NOT start bundled Postgres and SHALL authenticate against the supplied external instance

### Requirement: Bundled Credential Disclosure

The generated bundled Postgres password MUST be persisted to a `0600` state-dir file and printed once on screen at setup's end. It MUST NOT appear in any other log, argument, or provenance record.

#### Scenario: Password is disclosed exactly once

- GIVEN a default `docker` setup generated a password
- WHEN setup finishes
- THEN the system SHALL print it once to the terminal and persist it to the `0600` state-dir file

#### Scenario: Password stays out of other output

- GIVEN a default `docker` setup completed
- WHEN any other log line, provenance record, or argument is inspected
- THEN the password MUST NOT appear in any of them

### Requirement: Shippable Compose Artifact

The shipped `docker-compose.yml` MUST require no pre-created external network, MUST NOT contain a hardcoded credential, and MUST reference the published `ghcr.io/desatatufuria/regixtry` image rather than building locally.

#### Scenario: Clean checkout runs without manual network setup

- GIVEN an operator runs `docker compose up -d` on a clean checkout
- WHEN Compose resolves the stack
- THEN it MUST succeed without a prior `docker network create` step

#### Scenario: No literal credential is shipped

- GIVEN the shipped `docker-compose.yml` is inspected
- WHEN credential fields are read
- THEN they MUST contain only interpolated references, never a literal secret

### Requirement: TUI Access via Docker Exec

Documentation MUST state `docker exec -it <container> regixtry tui -storage-root /var/lib/regixtry` as the supported TUI path for a `docker`-mode deployment, and MUST NOT claim host-side TUI access to a container's data.

#### Scenario: Operator reaches the TUI against a running container

- GIVEN a running `docker`-mode deployment
- WHEN the operator runs the documented `docker exec -it <container> regixtry tui` command
- THEN they MUST reach a functioning TUI, with no host-side path claimed as supported

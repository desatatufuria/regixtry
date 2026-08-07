# Installation Modes Specification

## Purpose

Define a truthful, flag-driven installation/bootstrap contract for single-node Linux operators, starting with `daemon + SQLite`.

## Current Repository Facts

- `install.sh` currently installs a verified Linux release binary only; it does not generate runtime or service artifacts.
- `cmd/registry/main.go` currently exposes `serve`, `tui`, and `bootstrap-admin`; no installation-mode bootstrap command exists today.
- The current README installer flow ends with running `registry` manually; service activation and reachability checks are not yet part of install success.

## Requirements

### Requirement: Truthful Mode Contract

The installer MUST accept explicit flag-driven mode selection. In this slice, the system SHALL treat `daemon-sqlite` as the only truthful bootstrap mode for operators, and it MUST reject unsupported modes rather than implying container or Postgres-backed installation is ready.

#### Scenario: Supported mode is selected

- GIVEN a single-node Linux operator invokes install bootstrap with `--mode daemon-sqlite`
- WHEN the installer validates the requested mode
- THEN the system SHALL continue with the `daemon + SQLite` bootstrap contract

#### Scenario: Unsupported mode is requested

- GIVEN an operator requests a mode other than `daemon-sqlite`
- WHEN validation runs
- THEN the system MUST fail with a clear unsupported-mode result

### Requirement: Linux Bootstrap Artifacts

For `daemon-sqlite`, the system MUST generate the minimum runtime artifacts required to run the service: configuration or environment material, storage/data paths, and a service definition or equivalent launcher for the target host. This slice MUST target Debian, Ubuntu, Linux Mint, RHEL 9.x, and RHEL 10.x with systemd, and it MUST reject Alpine host bootstrap as deferred rather than implying support.

#### Scenario: Supported Linux host receives runnable artifacts

- GIVEN the operator runs `--mode daemon-sqlite` on a supported target Linux environment
- WHEN bootstrap completes artifact generation
- THEN the system SHALL leave all required runtime and service artifacts ready for service activation

#### Scenario: Unsupported environment is not overstated

- GIVEN the operator runs bootstrap on an environment outside the supported target set
- WHEN compatibility is evaluated
- THEN the system MUST not report bootstrap success for that environment

### Requirement: Default Service Activation and Reachability

Bootstrap for `daemon-sqlite` MUST start the service by default, and installation success SHALL mean the service is installed and reachable. The system MUST provide a deterministic verification outcome that operators can use to confirm reachability.

#### Scenario: Bootstrap reaches minimum success

- GIVEN runtime artifacts were generated successfully
- WHEN default activation completes and the service becomes reachable
- THEN the system SHALL report successful installation

#### Scenario: Service does not become reachable

- GIVEN bootstrap generated artifacts but service activation or readiness fails
- WHEN success is evaluated
- THEN the system MUST report installation failure

### Requirement: Scoped Rollback

Rollback for `daemon-sqlite` MUST remove generated bootstrap artifacts and stop or disable the created service, but it MUST NOT uninstall the already installed `registry` binary.

#### Scenario: Operator rolls back a completed bootstrap

- GIVEN a `daemon-sqlite` bootstrap created service and runtime artifacts
- WHEN the operator invokes rollback
- THEN the system SHALL remove generated artifacts and leave the binary installed

#### Scenario: Rollback after failed activation

- GIVEN bootstrap failed after creating some service-related artifacts
- WHEN rollback runs
- THEN the system MUST clean up generated bootstrap artifacts without deleting the binary

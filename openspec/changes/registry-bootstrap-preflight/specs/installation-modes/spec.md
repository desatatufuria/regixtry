# Delta for Installation Modes

## Current Repository Facts (Reference Only)

- The main `installation-modes` spec currently requires default bootstrap success to mean service start plus reachability.
- The current contract does not define a `--no-start` outcome or a pre-start local bind-address gate.

## ADDED Requirements

### Requirement: Occupied Local Bind Recovery Guidance

When bootstrap fails because the configured local bind address is already occupied, the system MUST fail before service activation and MUST print exact operator recovery commands rendered from the active configuration. Those commands MUST cover inspecting the listener, stopping `registry.service` when it owns the port, and re-running bootstrap with a different bind address and matching public URL.

#### Scenario: Occupied local bind prints recovery commands

- GIVEN `daemon-sqlite` bootstrap will start the service and its configured local bind address is already occupied
- WHEN preflight evaluates startup eligibility
- THEN the system MUST fail before `systemctl enable --now`
- AND the failure output MUST include exact rendered commands such as `sudo ss -ltnp 'sport = :<port>'`, `sudo systemctl stop registry.service`, and a bootstrap re-run command with a different `--addr`

#### Scenario: Non-local bind is out of scope

- GIVEN bootstrap is configured with a bind address outside the local preflight scope
- WHEN preflight eligibility is evaluated
- THEN the system MUST NOT fail on this requirement alone

## MODIFIED Requirements

### Requirement: Default Service Activation and Reachability

Bootstrap for `daemon-sqlite` MUST start the service by default only after artifact generation and local-bind preflight succeed. Installation success SHALL mean one of two explicit outcomes: default start completed and the service is reachable, or the operator requested `--no-start`, artifacts were generated, and the service was not started. When `--no-start` is used, the system MUST skip service activation, reachability verification, and bind-address preflight. When a configured local bind address is already occupied, the system MUST fail before service activation rather than reporting a later readiness failure.
(Previously: bootstrap always started the service after artifact generation and judged success only from post-start reachability.)

#### Scenario: Bootstrap reaches minimum success

- GIVEN runtime artifacts were generated successfully and no occupied local bind blocked startup
- WHEN default activation completes and the service becomes reachable
- THEN the system SHALL report successful installation

#### Scenario: Service does not become reachable

- GIVEN bootstrap generated artifacts and startup was attempted after preflight passed
- WHEN service activation or readiness still fails
- THEN the system MUST report installation failure

#### Scenario: No-start generates artifacts without startup checks

- GIVEN the operator invokes `daemon-sqlite` bootstrap with `--no-start`
- WHEN artifact generation completes
- THEN the system SHALL report success with artifacts generated and the service left stopped

#### Scenario: Occupied local bind fails before service start

- GIVEN bootstrap will start the service and the configured local bind address is already occupied
- WHEN bootstrap evaluates startup eligibility
- THEN the system MUST fail before service activation or reachability probing

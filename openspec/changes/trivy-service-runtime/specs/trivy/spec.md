# Trivy Specification

## Purpose

Define the service-first runtime contract for the `trivy` feature.

## Requirements

### Requirement: External Service Runtime Contract

The system MUST model the `trivy` feature runtime as `external_service`. Configured state MUST include `service_url`, MAY include auth and TLS settings, and MUST NOT require `binary_path` or `cache_dir` as the steady-state contract. The system MUST reject `service_url` values whose scheme is not `http` or `https`.

#### Scenario: Operator saves a valid service runtime

- GIVEN the operator configures `trivy` with an `https` `service_url`
- WHEN the feature state is persisted
- THEN the system SHALL store the runtime as `external_service`

#### Scenario: Unsupported service URL is submitted

- GIVEN the operator configures `trivy` with a non-HTTP scheme
- WHEN validation runs
- THEN the system MUST reject the configuration

### Requirement: Registry Reachability Across Deployment Models

The system MUST support Trivy services running on localhost/container networks and on remote hosts. The system MUST expose `registry_reachable_url` as the scanner-facing registry address, and it MAY fall back to the registry public URL only when that URL is not loopback-only. When no scanner-reachable registry address exists, the system MUST reject ready/configured outcomes that assume scans can run.

#### Scenario: Localhost container deployment provides scanner-facing registry address

- GIVEN `service_url` points to a localhost Trivy container
- WHEN the operator sets `registry_reachable_url`
- THEN the system SHALL use that address for scanner-to-registry reachability

#### Scenario: Remote service cannot rely on loopback registry address

- GIVEN the registry public URL is loopback-only for the Trivy service
- WHEN no `registry_reachable_url` override is configured
- THEN the system MUST report the feature as not ready for scans

### Requirement: Service Probing, Auth, TLS, and Legacy Bridge

Runtime status MUST probe `/healthz` and `/version` on the configured Trivy service and MUST apply the configured auth and TLS settings during those probes. The system SHALL report runtime readiness only after both probes succeed. The system MAY import legacy binary-backed Trivy settings through a one-way bridge, but the persisted operator-facing result MUST be rewritten into the external-service shape and MUST NOT preserve host-binary ownership as the steady-state contract.

#### Scenario: Service probes succeed with configured security settings

- GIVEN the Trivy service requires configured auth and TLS inputs
- WHEN the system probes `/healthz` and `/version`
- THEN the system SHALL report the feature runtime as ready only if both probes succeed

#### Scenario: Legacy binary-backed settings are bridged once

- GIVEN existing Trivy state only contains legacy binary-backed inputs
- WHEN the migration bridge imports that state into feature config
- THEN the system MUST expose external-service config instead of host-binary assumptions

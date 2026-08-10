# feature-runtime-operations Specification

## Purpose

Define operator UX.

## Requirements

### Requirement: Staged Runtime Operation Feedback

Install and upgrade MUST show stages and SHALL finish truthfully.

#### Scenario: Install or upgrade shows staged progress

- GIVEN an operator starts install or upgrade
- WHEN the operation spans multiple stages
- THEN the system SHALL show staged progress
- AND the final message SHALL report success truthfully

#### Scenario: Failed operation stops with truthful status

- GIVEN install or upgrade is in progress
- WHEN any stage fails
- THEN the system MUST stop advancing progress
- AND the final output MUST identify the operation as failed

### Requirement: On-Demand Version Awareness

List, status, and feature-screen refreshes MUST return current version and SHOULD resolve latest on demand. If lookup fails, latest MUST be `unknown` and the read MUST still succeed. The system MUST NOT use background polling or stored metadata.

#### Scenario: Latest version is available on demand

- GIVEN latest lookup succeeds
- WHEN an operator requests list, status, or feature-screen data
- THEN the system SHALL return current and latest versions
- AND it SHALL indicate whether an update is available

#### Scenario: Latest version lookup degrades safely

- GIVEN a feature read needs latest-version awareness
- WHEN the lookup cannot complete
- THEN the system MUST still return the read
- AND latest version MUST be reported as `unknown`

### Requirement: Tabular Feature Inventory

`feature list` MUST render a fixed-width table with name, kind, enabled, configured, current, latest, and update columns.

#### Scenario: Feature list renders aligned operator rows

- GIVEN features are available
- WHEN the operator runs `feature list`
- THEN the system SHALL print a header and aligned rows
- AND each row SHALL include version columns

### Requirement: Anti-Overengineering Runtime UX

This capability MUST reuse request flows. It MUST NOT add a framework, daemon, websocket, worker, or generic operation engine.

#### Scenario: Runtime UX remains request scoped

- GIVEN an operator performs install, upgrade, list, or status work
- WHEN the capability executes
- THEN the system SHALL complete within the existing CLI or admin request flow
- AND no persistent orchestrator SHALL be required

# Delta for installation-modes

## ADDED Requirements

### Requirement: Managed Initial Rescan Configuration

For `daemon-sqlite`, setup MUST capture operator-supplied initial Trivy rescan settings in lifecycle-managed runtime artifacts, and it MUST leave rescans disabled when the operator does not opt in.

#### Scenario: Setup omits optional rescans

- GIVEN an operator runs setup without enabling Trivy rescans
- WHEN managed runtime artifacts are generated
- THEN the system SHALL record rescans as disabled by default

#### Scenario: Setup records optional rescan settings

- GIVEN an operator supplies valid initial Trivy rescan settings during setup
- WHEN managed runtime artifacts are generated
- THEN the system SHALL persist those bounded settings as lifecycle-managed runtime state

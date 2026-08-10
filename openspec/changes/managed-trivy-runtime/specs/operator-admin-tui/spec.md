# Delta for operator-admin-tui

## ADDED Requirements

### Requirement: Managed Trivy Runtime Visibility and Actions

The admin API and TUI MUST expose managed Trivy runtime state through existing feature surfaces, SHALL distinguish operator intent from runtime lifecycle state, and MUST provide truthful install, upgrade, rollback, and status actions for authorized operators.

#### Scenario: Operator sees separated runtime state

- GIVEN Trivy feature configuration and managed runtime metadata exist
- WHEN an authorized operator opens the Trivy feature view
- THEN the system SHALL show feature intent separately from runtime lifecycle state

#### Scenario: Legacy service model is superseded truthfully

- GIVEN persisted Trivy state still reflects the superseded HTTP-service model
- WHEN an authorized operator inspects or acts on the feature
- THEN the system MUST show guided migration or degraded runtime feedback instead of pretending the legacy model is healthy

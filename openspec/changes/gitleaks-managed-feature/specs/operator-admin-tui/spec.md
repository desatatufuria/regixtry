# Delta for operator-admin-tui

## ADDED Requirements

### Requirement: Per-Feature Runtime Status Surface

The TUI MUST display runtime status independently for each registered managed feature, sourced from that feature's own `(tenant, feature)` runtime state, and MUST NOT show one feature's status under another feature's entry.

#### Scenario: Operator views Gitleaks status separately from Trivy

- GIVEN Trivy and Gitleaks are both registered managed features
- WHEN the operator opens the feature runtime view
- THEN the TUI SHALL show each feature's own version, readiness, and rollback availability in its own entry

### Requirement: Secret Findings Surface

The TUI MUST provide a read-only view of persisted secret findings (rule ID and location) for a selected image, shown as informational alongside existing vulnerability scan results, with no severity or gating indicator.

#### Scenario: Operator reviews secret findings for an image

- GIVEN an authenticated operator selects an image that has persisted secret findings
- WHEN the operator opens its scan detail
- THEN the TUI SHALL list each finding's rule ID and location without exposing matched secret text

#### Scenario: Image with no findings shows an empty state

- GIVEN a selected image has no persisted secret findings
- WHEN the operator opens its scan detail
- THEN the TUI SHALL show a clear empty state rather than an error

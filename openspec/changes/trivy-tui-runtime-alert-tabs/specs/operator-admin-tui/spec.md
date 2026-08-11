# Delta for Operator Admin TUI

## ADDED Requirements

### Requirement: Trivy Feature Tabs

When the selected feature is Trivy, the Features screen MUST present exactly two operator tabs: `Runtime` and `Repository Alerts`. The `Runtime` tab SHALL remain the default entry view after Trivy page load or refresh. This slice MUST keep tab state Trivy-specific and MUST NOT require a reusable cross-feature tab schema.

#### Scenario: Runtime tab is the default Trivy landing view

- GIVEN an authenticated operator opens or refreshes the Trivy feature page
- WHEN the page data loads successfully
- THEN the TUI SHALL show the `Runtime` tab first
- AND the `Repository Alerts` tab SHALL remain available without leaving the feature screen

#### Scenario: Non-Trivy features keep the generic page shell

- GIVEN the selected feature is not Trivy
- WHEN the feature page renders
- THEN the TUI MUST NOT invent `Runtime` or `Repository Alerts` tabs

### Requirement: Trivy Configuration Modal Scope

Trivy configuration edits MUST be performed through a modal workflow opened from the `Runtime` tab. The modal MUST be limited to the current Trivy `FeatureConfigureInput` fields already supported by the backend, and it MUST NOT add new persisted settings, policy controls, or exclusion fields in this slice.

#### Scenario: Operator edits current Trivy settings in a modal

- GIVEN the operator is viewing the Trivy `Runtime` tab
- WHEN the operator opens configuration and submits valid existing settings fields
- THEN the TUI SHALL send only the current backend-supported configuration payload
- AND the screen SHALL return to the `Runtime` tab with backend-authoritative feedback

#### Scenario: Unsupported settings stay out of scope

- GIVEN the operator opens Trivy configuration
- WHEN the requested field is not part of the current backend-supported settings
- THEN the TUI MUST NOT expose or persist that field in the modal

### Requirement: Repository Alert Drill-Down Uses Scan Runs

The `Repository Alerts` tab MUST derive its navigation and drill-down from existing scan-run records returned by the current admin scan-run route. Operators SHALL be able to move from a repository-oriented alert list to a selected scan-run detail view within the same Trivy screen. This slice MUST NOT require new vulnerability-detail persistence.

#### Scenario: Operator drills into repository alerts from scan runs

- GIVEN Trivy scan-run records exist for one or more repositories
- WHEN the operator selects the `Repository Alerts` tab and chooses a listed repository alert
- THEN the TUI SHALL show drill-down data sourced from the related existing scan-run record
- AND the operator SHALL remain inside the Trivy feature screen

#### Scenario: Empty scan-run data remains recoverable

- GIVEN no matching scan-run records are available for repository alerts
- WHEN the operator opens the `Repository Alerts` tab
- THEN the TUI SHALL show a clear empty-state message
- AND the `Runtime` tab SHALL remain usable without alert data

### Requirement: Explicit Deferrals And Anti-Overengineering

This change MUST remain limited to the narrow operator slice above. The system MUST NOT add persisted per-vulnerability detail expansion, cross-screen alert workflows, exclusions, allowlists, policy engines, approval flows, or reusable feature-tab abstractions. Any deeper vulnerability review MAY be deferred to later changes backed by new APIs and specs.

#### Scenario: Deferred policy and persistence systems are not introduced

- GIVEN an operator is using the Trivy tabs delivered by this change
- WHEN the operator reviews runtime or repository alert data
- THEN the workflow MUST stay within current feature-page and scan-run capabilities
- AND no new persistence or policy-management system SHALL be required

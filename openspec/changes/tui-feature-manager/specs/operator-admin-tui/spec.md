# Delta for Operator Admin TUI

## ADDED Requirements

### Requirement: Generic Feature Summary List

The admin Features screen MUST present a shared summary list for every backend-declared feature. The list MUST remain lightweight and SHALL be limited to navigation-ready summary data such as identity, status, and generic badges. The shell MUST NOT require Trivy-specific fields to render the list.

#### Scenario: Operator browses shared feature summaries

- GIVEN an authenticated admin session exists
- WHEN the operator opens the Features screen
- THEN the TUI SHALL render the backend-provided feature summaries as a selectable list
- AND each item SHALL remain renderable without feature-specific detail fields

#### Scenario: Future feature still fits the list shell

- GIVEN the backend returns a non-Trivy feature summary with only shared summary fields
- WHEN the Features screen renders that item
- THEN the TUI MUST show it without inventing schedule, runtime, or vulnerability vocabulary

### Requirement: Backend-Declared Feature Detail Pages

The selected feature page MUST be rendered from backend-declared page data: a common header, ordered sections, and declared actions. The TUI MUST keep shell concerns limited to selection, refresh, focus, confirmation, and operator feedback. The system MUST NOT introduce pluginization, schema interpreters, or other overengineered extension layers for this slice.

#### Scenario: Backend controls visible sections and actions

- GIVEN a selected feature has backend-declared sections and actions
- WHEN the detail page is loaded
- THEN the TUI SHALL render sections in backend order
- AND the TUI SHALL expose only the declared actions for that feature

#### Scenario: Minimal feature page remains valid

- GIVEN a selected feature returns only a header and no extra sections or actions
- WHEN the detail page is rendered
- THEN the shell MUST remain usable and lightweight
- AND the TUI MUST NOT require a plugin mechanism to support that page

### Requirement: Trivy-Specific Rich Sections

Trivy MAY expose richer feature-specific sections for configuration, runtime, runs, vulnerabilities, and repository alerts. Those sections MUST be treated as Trivy payloads, and the system MUST NOT universalize them into required fields for all features.

#### Scenario: Trivy exposes richer operator detail

- GIVEN the selected feature is Trivy
- WHEN the backend returns Trivy-specific sections and actions
- THEN the TUI SHALL render those richer sections within the generic shell
- AND Trivy actions SHALL remain backend-authoritative

#### Scenario: Non-Trivy feature does not inherit Trivy semantics

- GIVEN the selected feature is not Trivy
- WHEN its page omits runtime, runs, vulnerabilities, or repository alerts
- THEN the TUI MUST render the returned page normally
- AND the shared contract MUST NOT force those Trivy concepts onto that feature

## MODIFIED Requirements

### Requirement: Read-Only Admin Browsing

After login, the TUI MUST provide admin views for users, per-user repository grants, per-user admin tokens, and feature summaries using `/admin/v1` endpoints. User views SHALL support confirmed single-user enable and disable mutations through backend-authoritative write routes, while grants and admin-token views MUST remain read-only. Feature detail pages MAY expose backend-declared feature actions, and the TUI SHALL treat the backend as authoritative for their availability and meaning. The TUI MUST preserve existing local registry inspection views, and it MUST NOT offer password reset, grant writes, user creation, admin-token writes, batch actions, inline editing, or unrelated navigation changes in this slice.
(Previously: Admin browsing covered users, grants, and admin tokens only, with no generic feature-manager behavior.)

#### Scenario: Operator browses admin data

- GIVEN an authenticated admin session exists
- WHEN the operator opens users, grants, admin-token, or feature views
- THEN the TUI SHALL load data from the corresponding `/admin/v1` endpoint
- AND only the views with backend-authorized mutations MAY expose actions

#### Scenario: Unauthenticated state blocks admin reads

- GIVEN no valid admin session exists
- WHEN the operator attempts to open an admin view
- THEN the TUI MUST block the view behind login
- AND local registry inspection MAY remain available without granting admin capabilities

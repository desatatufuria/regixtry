# Delta for operator-admin-tui

## MODIFIED Requirements

### Requirement: Read-Only Admin Browsing

After login, the TUI MUST provide admin views for users, per-user repository grants, per-user admin tokens, and built-in feature configuration status using `/admin/v1` endpoints. User views SHALL support confirmed single-user enable and disable mutations through backend-authoritative write routes, and feature views MAY expose confirmed enable, disable, or configure actions for built-in features such as Trivy. Grants and admin-token views MUST remain read-only. The TUI MUST preserve existing local registry inspection views, and it MUST NOT offer password reset, grant writes, user creation, admin-token writes, batch actions, inline editing, or extra detail panes in this slice.

(Previously: Admin browsing was limited to users, grants, and admin tokens, with only user enable/disable mutations allowed.)

#### Scenario: Operator browses admin data

- GIVEN an authenticated admin session exists
- WHEN the operator opens users, grants, admin-token, or feature views
- THEN the TUI SHALL load data from the corresponding `/admin/v1` endpoint

#### Scenario: Unauthenticated state blocks admin reads

- GIVEN no valid admin session exists
- WHEN the operator attempts to open an admin view
- THEN the TUI MUST block the view behind login

#### Scenario: Feature configuration stays backend-authoritative

- GIVEN an authenticated admin session exists
- WHEN the operator changes a built-in feature such as Trivy
- THEN the TUI MUST send the change through backend-authoritative feature routes

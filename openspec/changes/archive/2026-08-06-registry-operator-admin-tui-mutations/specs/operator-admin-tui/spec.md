# Delta for operator-admin-tui

## Current Repository Facts

- The main `operator-admin-tui` spec currently limits admin data browsing to read-only `/admin/v1` GET flows.
- Backend mutation authority already exists outside this slice and remains the source of truth for self-disable and last-admin protection.

## ADDED Requirements

### Requirement: Confirmed User Enable Disable Actions

The system MUST allow an authenticated operator to request enable or disable for a selected user from the users screen only after explicit confirmation. This slice MUST remain limited to single-user enable/disable actions and MUST NOT add batch actions, inline editing, extra detail panes, or other mutation types.

#### Scenario: Confirmed disable succeeds

- GIVEN an authenticated operator is focused on a user in the users screen
- WHEN the operator confirms a disable action for that user
- THEN the TUI SHALL send the disable request through the authenticated backend client
- AND the TUI SHALL show a success status and refresh the users list

#### Scenario: Confirmation declined prevents mutation

- GIVEN an authenticated operator has opened an enable or disable confirmation
- WHEN the operator cancels or dismisses the confirmation
- THEN the TUI MUST NOT send any mutation request

### Requirement: Mutation Failure Feedback

The system MUST surface recoverable feedback for mutation failures, including backend conflicts, expired sessions, and validation errors, without inventing local bypass behavior.

#### Scenario: Backend conflict is shown clearly

- GIVEN an authenticated operator confirms a disable action
- WHEN the backend rejects it because self-disable or last-active-admin protection applies
- THEN the TUI MUST keep the backend result authoritative
- AND the TUI SHALL show a clear conflict message without changing local state optimistically

#### Scenario: Expired session blocks mutation completion

- GIVEN an authenticated operator confirms an enable or disable action
- WHEN the backend rejects the request because the session is expired or invalid
- THEN the TUI MUST stop using that session
- AND the TUI SHALL return the operator to re-authenticate with expiry-specific feedback

#### Scenario: Validation failure remains recoverable

- GIVEN an authenticated operator confirms an enable or disable action
- WHEN the backend rejects the request as invalid
- THEN the TUI MUST show a clear recoverable validation message

## MODIFIED Requirements

### Requirement: Read-Only Admin Browsing

After login, the TUI MUST provide admin views for users, per-user repository grants, and per-user admin tokens using `/admin/v1` endpoints. User views SHALL support confirmed single-user enable and disable mutations through backend-authoritative write routes, while grants and admin-token views MUST remain read-only. The TUI MUST preserve existing local registry inspection views, and it MUST NOT offer password reset, grant writes, user creation, admin-token writes, batch actions, inline editing, or extra detail panes in this slice.

(Previously: all admin views were read-only and user enable/disable actions were explicitly prohibited.)

#### Scenario: Operator browses admin data

- GIVEN an authenticated admin session exists
- WHEN the operator opens users, grants, or admin-token views
- THEN the TUI SHALL load data from the corresponding `/admin/v1` endpoint
- AND only the users view MAY expose confirmed enable or disable actions

#### Scenario: Unauthenticated state blocks admin reads

- GIVEN no valid admin session exists
- WHEN the operator attempts to open an admin view
- THEN the TUI MUST block the view behind login
- AND local registry inspection MAY remain available without granting admin capabilities

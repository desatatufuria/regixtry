# Operator Admin TUI Specification

## Purpose

Define the first secure admin slice for the TUI: authenticated login, in-memory session handling, and read-only admin browsing over backend APIs.

## Current Repository Facts

- `cmd/regixtry/main.go` `runTUI` currently opens local blob/metadata stores and launches the inspection TUI directly.
- When `--auth-postgres-dsn` is set, the TUI only ensures bootstrap auth state and shows a notice; it still uses `localOperatorAccessController`, which authorizes all actions locally.
- The backend already exposes `GET|POST /auth/token`, `GET /admin/v1/users`, `GET /admin/v1/users/{id}/grants`, and `GET /admin/v1/users/{id}/admin-tokens`.

## Requirements

### Requirement: Operator Login Gate

The TUI MUST require an authenticated backend session before any admin view is shown, and it MUST obtain that session through `/auth/token` with operator-supplied username and password. The TUI MUST NOT provide a local admin shortcut when backend auth is configured.

#### Scenario: Successful login opens admin mode

- GIVEN the TUI is connected to an authenticated admin backend
- WHEN an operator submits valid username and password
- THEN the TUI SHALL exchange them for a bearer session through `/auth/token`
- AND the TUI SHALL enter authenticated admin navigation without exposing the password again

#### Scenario: Invalid credentials keep the session unauthenticated

- GIVEN the login screen is active
- WHEN `/auth/token` rejects the supplied credentials
- THEN the TUI MUST remain unauthenticated
- AND the TUI SHOULD show a clear recoverable login error

### Requirement: In-Memory Session Lifecycle

Session material MUST remain in process memory only. The TUI MUST clear password input, bearer token state, and derived admin session state on logout, process exit, or detected expiry. The TUI MUST return to a safe unauthenticated screen after expiry.

#### Scenario: Logout clears session state

- GIVEN an authenticated operator session exists
- WHEN the operator logs out
- THEN the TUI MUST discard all session material from memory
- AND the next admin action SHALL require a fresh login

#### Scenario: Expired session is handled safely

- GIVEN an authenticated operator session has expired or is rejected as invalid
- WHEN the TUI requests an admin resource
- THEN the TUI MUST stop using that session immediately
- AND the TUI SHALL redirect the operator to re-authenticate with an expiry-specific message

### Requirement: Read-Only Admin Browsing

After login, the TUI MUST provide admin views for users, per-user repository grants, and per-user admin tokens using `/admin/v1` endpoints. User views SHALL support confirmed single-user enable and disable mutations through backend-authoritative write routes, while grants and admin-token views MUST remain read-only. The TUI MUST preserve existing local registry inspection views, and it MUST NOT offer password reset, grant writes, user creation, admin-token writes, batch actions, inline editing, or extra detail panes in this slice.

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

# Operator Admin TUI Specification

## Purpose

Define the first secure admin slice for the TUI: authenticated login, in-memory session handling, read-only admin browsing over backend APIs, and operator configuration of content-trust policies.

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

### Requirement: Dedicated Global Signing Policy Modal

The operator MUST be able to open a dedicated signing policy modal,
sibling to `scanPolicyModal` and not an extension of it, showing the
global `Enabled` flag, configured trusted keys, and configured trusted
identities. The operator MUST be able to change and save all three,
round-tripping through the admin API and reflected back in the modal.

#### Scenario: Modal shows current global signing policy
- GIVEN the global signing policy has trusted keys and trusted identities
  configured
- WHEN the operator opens the signing modal
- THEN it SHALL show the current `Enabled` state, trusted keys, and
  trusted identities

#### Scenario: Operator saves a policy change
- GIVEN the signing modal is open
- WHEN the operator submits a changed `Enabled` value, key set, or
  identity set
- THEN the TUI SHALL persist it through the admin API and reflect the new
  values back in the modal

### Requirement: Signing Status Badge Is Text-Only

The system MUST render a persistent, text-only signing status badge,
following `scanPolicyBadge`'s established precedent of no icon or glyph
vocabulary.

#### Scenario: Badge reflects enabled and disabled states
- GIVEN the global signing policy is enabled
- WHEN the admin view renders the badge
- THEN it SHALL show a text-only "on" indication, and SHALL show a
  text-only "off" indication when the policy is disabled

### Requirement: Signing Is A Third Feature Cycle Option In The Override Modal

The existing `repositoryOverrideModal` MUST support `signing` as a third
value in its Feature field cycle, alongside `trivy` and `gitleaks`. No
new modal is introduced for per-repository signing configuration; the
existing modal's fields MUST adapt to signing's settings shape — 
including trusted identities — when `signing` is selected.

#### Scenario: Operator cycles to the signing feature
- GIVEN the repository override modal is open with Feature focused
- WHEN the operator cycles through Feature values
- THEN `signing` SHALL appear as a third option alongside `trivy` and
  `gitleaks`

#### Scenario: Modal fields adapt to signing's settings shape including identities
- GIVEN the operator has selected `signing` in the Feature field
- WHEN the modal renders its fields
- THEN it SHALL present signing's override fields — trusted keys and
  trusted identities — not Trivy/gitleaks' scan-path fields

## Out of Scope Note

No requirement above adds a new modal for per-repository signing
configuration; that reuses the existing `repositoryOverrideModal`. Only
the global policy gets a dedicated modal, now showing both anchor types.

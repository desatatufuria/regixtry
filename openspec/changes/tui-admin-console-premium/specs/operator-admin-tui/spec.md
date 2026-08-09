# Delta for Operator Admin TUI

## ADDED Requirements

### Requirement: Premium Workspace

The system MUST present authenticated admin mode as a workspace with a sidebar, main-panel forms, and short confirmation modals. Required colors: bg `#0D0D0F`; surfaces `#151518`/`#1C1C20`; border `#29292E`; text `#F2F0EB`/`#A09DA6`/`#66636C`; accent `#D6B56D`; complementary `#9D8FC1`; success `#78A083`; warning `#D6B56D`; error `#C87575`.

#### Scenario: Shell opens

- GIVEN an authenticated operator session exists
- WHEN the admin workspace is rendered
- THEN the TUI SHALL show sidebar navigation and a main action panel
- AND confirmations SHALL use short modal dialogs

#### Scenario: Failures stay recoverable

- GIVEN a backend validation or session error occurs
- WHEN the workspace shows feedback
- THEN the TUI MUST use themed status colors and keep the failing form visible

### Requirement: User Provisioning And Reset

The system MUST let an authenticated operator create a user, optionally set `is_admin=true` only at creation time, and reset that user's password through the existing HTTP API. The system MUST NOT offer post-creation `is_admin` edits or user deletion.

#### Scenario: Operator creates an admin user

- GIVEN an operator opens the create-user form
- WHEN valid user data is submitted with admin creation enabled
- THEN the TUI SHALL call the existing API and refresh the users view from backend state

#### Scenario: Unsupported admin edit is not offered

- GIVEN an operator is viewing an existing user
- WHEN the operator inspects available actions
- THEN the TUI MUST NOT expose post-creation `is_admin` editing or delete-user controls

### Requirement: Selected Grants

The system MUST let an operator add, change, and remove repository grants only for the selected user. Grant forms and results SHALL stay subordinate to that user and use only `/admin/v1` grant routes.

#### Scenario: Operator changes a selected user's grant

- GIVEN a user is selected in the sidebar
- WHEN the operator submits a grant change in the grants panel
- THEN the TUI SHALL send the change for that selected user only

#### Scenario: No selected user blocks grant writes

- GIVEN no user is selected
- WHEN the operator opens grant actions
- THEN the TUI MUST block grant mutation controls until a user is selected

### Requirement: Selected Tokens

The system MUST let an operator create and revoke admin tokens only for the selected user. New token secrets MUST be revealed once and MUST NOT be silently refreshed or persisted locally.

#### Scenario: Token secret is revealed once

- GIVEN token creation succeeds for the selected user
- WHEN the backend returns a new admin token secret
- THEN the TUI SHALL reveal it once in the selected-user token panel

#### Scenario: Revoke uses explicit confirmation

- GIVEN an admin token is listed for the selected user
- WHEN the operator confirms token revocation
- THEN the TUI SHALL call the revoke route and refresh token data from the backend

## MODIFIED Requirements

### Requirement: Read-Only Admin Browsing

After login, the TUI MUST provide an admin workspace for users, per-user grants, and per-user admin tokens using `/admin/v1` endpoints. User views SHALL support create user, create admin user at creation time, confirmed single-user enable or disable, and password reset through backend routes. Grants and admin-token views MUST stay contextualized to the selected user while supporting only the in-scope write actions of the existing HTTP contract. The TUI MUST preserve local inspection views, and it MUST NOT offer batch actions, inline editing, extra detail panes, unsupported HTTP routes, or out-of-scope actions.
(Previously: only enable/disable writes were allowed; grants and tokens stayed read-only.)

#### Scenario: Operator browses and mutates admin data in-context

- GIVEN an authenticated admin session exists
- WHEN the operator opens users, grants, or admin-token views and completes an in-scope action
- THEN the TUI SHALL load and refresh data from the corresponding HTTP endpoints
- AND grants and tokens SHALL stay scoped to the selected user

#### Scenario: Unauthenticated state blocks admin workspace access

- GIVEN no valid admin session exists
- WHEN the operator attempts to open an admin view
- THEN the TUI MUST block the workspace behind login
- AND local registry inspection MAY remain available without granting admin capabilities

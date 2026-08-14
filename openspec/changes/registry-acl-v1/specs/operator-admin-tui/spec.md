# Delta for operator-admin-tui

## Baseline Note

`screenAdminEditUserGrants`/`screenAdminAddGrant` already exist and are
reachable only from the admin-authenticated flow (`isAdminScreen` /
`isAdminPrincipalScreen`), which assumes a human `IsAdmin` session. This
delta opens grant mutation to repo-admin delegates, scopes it to their own
repositories, adds robot account screens, and adds the read-only role field
to user forms.

## ADDED Requirements

### Requirement: Robot Account Screens

The TUI MUST provide robot account screens mirroring the existing user and
token screens: create robot, bind a repository grant, issue a bounded-TTL
token, and revoke a token. These screens MUST be reachable by any operator
authorized to manage the target repository's grants, not only global admins.

#### Scenario: Operator manages a robot account end to end
- GIVEN an authenticated operator with sufficient grant authority on a repository
- WHEN the operator creates a robot, binds a grant on that repository, and issues a token
- THEN the TUI completes each step through `/admin/v1` and shows the one-time token secret

### Requirement: Read-Only Role Field On User Forms

The user create and edit forms MUST expose a field to set the
registry-wide read-only role, independent of the admin toggle.

#### Scenario: Operator sets the read-only role
- GIVEN an authenticated admin operator is editing a user
- WHEN the operator sets the read-only role field and saves
- THEN the TUI persists it through the admin API and reflects it back

## MODIFIED Requirements

### Requirement: Read-Only Admin Browsing

After login, the TUI MUST provide admin views for users, per-user
repository grants, robot accounts, and per-user admin tokens using
`/admin/v1` endpoints. User views SHALL support confirmed single-user
enable and disable mutations, and SHALL support setting the registry-wide
read-only role. Grant views MUST support put/delete mutations scoped to
the repositories the authenticated operator administers: a global admin
sees and manages every repository's grants; a `repo-admin` delegate sees
and manages only their own repositories' grants, and the TUI MUST NOT offer
`repo-admin` as a selectable role in a delegate's grant form. Admin-token
views for human users remain read-only. The TUI MUST preserve existing
local registry inspection views.

(Previously: grants and admin-token views were entirely read-only, with no
robot or read-only-role surface, and grant screens were reachable only by
global admins.)

#### Scenario: Operator browses admin data
- GIVEN an authenticated admin session exists
- WHEN the operator opens users, grants, robot accounts, or admin-token views
- THEN the TUI SHALL load data from the corresponding `/admin/v1` endpoint
- AND the users view MAY expose confirmed enable/disable and the read-only role toggle, and the grants view MAY expose put/delete mutations scoped to the operator's authority

#### Scenario: Unauthenticated state blocks admin reads
- GIVEN no valid admin session exists
- WHEN the operator attempts to open an admin view
- THEN the TUI MUST block the view behind login
- AND local registry inspection MAY remain available without granting admin capabilities

#### Scenario: Delegate sees only their own repositories' grants
- GIVEN an operator holds `repo-admin` on repository `team/app` only, not global admin
- WHEN the operator opens the grants view
- THEN the TUI SHALL show and allow mutation of `team/app`'s grants only

#### Scenario: Delegate cannot select repo-admin in the grant role picker
- GIVEN a repo-admin delegate is creating or editing a grant
- WHEN the operator opens the role field
- THEN `repo-admin` MUST NOT appear as a selectable value

# Delta for operator-access-administration

## MODIFIED Requirements

### Requirement: Repository grant management is reversible and delegate-bounded

The system MUST support listing, putting, and deleting repository grants
for a target user and MUST validate the target user, repository, and role
before mutating access. A global admin MAY manage grants on any
repository. A `repo-admin` delegate MAY manage grants only on repositories
where they hold `repo-admin`, and MUST be restricted to granting
`repo-reader` or `repo-writer` only: the delegate MUST NOT grant, transfer,
or self-assign `repo-admin`, and MUST NOT act on a repository they do not
administer. A delegate's grant listing MUST be scoped to their own
repositories only.

(Previously: grant management required global `IsAdmin` uniformly, with no
delegate path.)

#### Scenario: Admin replaces a repository grant
- GIVEN an authenticated admin actor and an existing target user
- WHEN the actor puts a repository grant for that user
- THEN the stored grant reflects the requested repository and role

#### Scenario: Invalid grant input is rejected
- GIVEN a missing user, invalid repository, or invalid role
- WHEN the client submits a grant mutation
- THEN the request is rejected

#### Scenario: Delegate grants repo-reader on their own repository
- GIVEN an actor holds `repo-admin` on `team/app` only
- WHEN the actor puts a `repo-reader` grant on `team/app` for another user
- THEN the stored grant reflects the requested repository and role

#### Scenario: Delegate is rejected granting repo-admin
- GIVEN an actor holds `repo-admin` on `team/app` only
- WHEN the actor attempts to grant `repo-admin` to another user on `team/app`
- THEN the request MUST be rejected

#### Scenario: Delegate is rejected self-assigning repo-admin
- GIVEN an actor holds `repo-admin` on `team/app` only
- WHEN the actor attempts to grant `repo-admin` to themselves on `team/app`
- THEN the request MUST be rejected

#### Scenario: Delegate is rejected acting outside their repository
- GIVEN an actor holds `repo-admin` on `team/app` only
- WHEN the actor attempts any grant mutation on `team/other`
- THEN the request MUST be rejected

#### Scenario: Delegate's grant listing is scoped to their own repositories
- GIVEN an actor holds `repo-admin` on `team/app` only
- WHEN the actor lists repository grants
- THEN the response MUST include only `team/app` grants

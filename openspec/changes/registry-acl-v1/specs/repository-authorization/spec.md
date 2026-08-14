# Delta for repository-authorization

## ADDED Requirements

### Requirement: Registry-Wide Read-Only Role Grants Read Everywhere

The system MUST support a global read-only role on a principal that grants
pull, catalog, and tag/manifest read access on every repository without a
per-repository `RepoGrant`. This role MUST NOT grant push, MUST NOT grant
`repo-admin` on any repository, and MUST NOT grant access to the admin
user/grant listings or any `/admin/v1/*` mutation.

#### Scenario: Read-only principal reads across all repositories
- GIVEN a principal has the registry-wide read-only role and no per-repository grants
- WHEN the principal requests the catalog or pulls tags/manifests from any repository
- THEN the request succeeds

#### Scenario: Read-only principal is rejected on push
- GIVEN a principal has only the registry-wide read-only role
- WHEN the principal attempts to push to any repository
- THEN the request MUST be rejected as unauthorized

#### Scenario: Read-only principal cannot see admin listings
- GIVEN a principal has only the registry-wide read-only role
- WHEN the principal requests the admin user listing or any grant listing
- THEN the request MUST be rejected

#### Scenario: Principals without the flag are unaffected
- GIVEN a principal has neither `IsAdmin` nor the read-only role
- WHEN the principal makes any request
- THEN authorization behavior MUST be identical to before this role existed

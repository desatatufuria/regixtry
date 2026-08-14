# Delta for operator-admin-http-api

## MODIFIED Requirements

### Requirement: Admin transport errors are explicit, with a narrower gate for grant routes

The system MUST distinguish unauthorized, forbidden, validation, and
conflict outcomes for `/admin/v1` without reusing ambiguous success or
challenge semantics. Every `/admin/v1` route MUST require global `IsAdmin`,
except the repository-grant sub-routes, which MUST instead require an
authenticated principal holding either global `IsAdmin` or `repo-admin` on
the target repository. This narrower check MUST apply only to the grant
sub-routes and MUST NOT be inherited by default by any other route sharing
the same dispatcher.

(Previously: every `/admin/v1` route required global admin uniformly, with
no per-route carve-out.)

#### Scenario: Missing or invalid bearer token
- GIVEN no valid admin bearer token
- WHEN the client calls `/admin/v1`
- THEN the response is unauthorized

#### Scenario: Non-admin actor calls a non-grant admin route
- GIVEN an authenticated non-admin, non-delegate actor
- WHEN the actor calls a non-grant `/admin/v1` route (e.g. user CRUD, tokens)
- THEN the response is forbidden

#### Scenario: Repo-admin delegate reaches grant sub-routes for their repository
- GIVEN an authenticated actor holds `repo-admin` on `team/app`
- WHEN the actor calls a grant sub-route scoped to `team/app`
- THEN the request is authorized without requiring global `IsAdmin`

#### Scenario: Repo-admin delegate is forbidden on a repository they don't administer
- GIVEN an authenticated actor holds `repo-admin` on `team/app` only
- WHEN the actor calls a grant sub-route scoped to `team/other`
- THEN the response is forbidden

#### Scenario: Repo-admin delegate is forbidden on non-grant admin routes
- GIVEN an authenticated actor holds `repo-admin` on a repository but not global `IsAdmin`
- WHEN the actor calls a non-grant `/admin/v1` route
- THEN the response is forbidden

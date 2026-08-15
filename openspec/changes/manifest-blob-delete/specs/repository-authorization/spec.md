# Delta for repository-authorization

## ADDED Requirements

### Requirement: Repo-Writer Authorizes Manifest Deletion, Symmetric With Push

A principal holding `repo-writer` or `repo-admin` on a repository MUST be
authorized to delete manifests/tags there via the new `ActionDelete` verb. A
`repo-reader`-only principal MUST NOT be authorized. Delete MUST NOT be
implied by `ActionPush`.

#### Scenario: Repo-writer and repo-admin succeed on delete
- GIVEN a principal has `repo-writer` (or `repo-admin`) on `team/app`
- WHEN the principal sends `DELETE` against a manifest/tag in `team/app`
- THEN the request is authorized

#### Scenario: Repo-reader is rejected on delete
- GIVEN a principal has only `repo-reader` on `team/app`
- WHEN the principal sends `DELETE` against `team/app`
- THEN the request is rejected as unauthorized

#### Scenario: Delete authorization is repository-scoped
- GIVEN a principal has `repo-writer` on `team/app` only
- WHEN the principal sends `DELETE` against `team/other`
- THEN the request is rejected as unauthorized

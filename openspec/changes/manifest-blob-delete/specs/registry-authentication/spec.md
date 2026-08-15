# Delta for registry-authentication

## ADDED Requirements

### Requirement: Delete Is A Distinct, Explicitly-Requestable Scope Action

The system MUST support `delete` as a distinct Docker scope action (e.g.
`repository:<name>:pull,push,delete`), derived only from a principal holding
`repo-writer` or higher. `delete` MUST NOT be implied by `push` alone.

#### Scenario: Writer-or-higher principal is issued the delete action
- GIVEN a principal has `repo-writer` or higher on `team/app`
- WHEN a bearer token or challenge is issued for `team/app`
- THEN its scope includes the `delete` action

#### Scenario: Push-only token is rejected on delete
- GIVEN a token scoped only `pull,push` on `team/app`
- WHEN that token is presented on a `DELETE` request against `team/app`
- THEN the request is rejected as unauthorized

#### Scenario: Explicitly-scoped delete token succeeds
- GIVEN a token scoped `pull,push,delete`, issued to a `repo-writer`+
  principal
- WHEN presented on a `DELETE` request against the same repository
- THEN the request is authorized

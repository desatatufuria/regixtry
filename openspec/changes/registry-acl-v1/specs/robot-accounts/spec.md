# Robot Accounts Specification

## Purpose

Define the non-interactive machine-identity lifecycle: creation, repository
grant binding, bounded-TTL token issuance, revocation, and permanent
exclusion from password login and from default human user listings.

## Requirements

### Requirement: Robot Identity Is Permanently Non-Interactive

A robot account MUST NOT authenticate through the password login path at
creation time or at any later time. This MUST be enforced as a permanent
guard in the login/lookup path, not only checked when the robot row is
created.

#### Scenario: Robot cannot be created with a usable password
- GIVEN an operator creates a robot account
- WHEN the robot row is persisted
- THEN it MUST NOT accept any password that later succeeds against the
  password login path

#### Scenario: Existing robot row is permanently rejected at login
- GIVEN a robot account row exists
- WHEN any password login attempt targets that row's username
- THEN the system MUST reject it, regardless of when the robot was created

### Requirement: Robot Repository Grant Binding

A robot account MUST own repository grants using the existing `RepoGrant`
model (`repo-reader`/`repo-writer`/`repo-admin`), and its issued tokens MUST
derive scope through the existing scope-derivation mechanism unchanged.

#### Scenario: Robot pulls and pushes per its granted role
- GIVEN a robot holds `repo-writer` on `team/app`
- WHEN the robot presents a token derived from that grant
- THEN pull and push on `team/app` succeed and actions outside the grant's
  role are rejected

#### Scenario: Robot token is denied on an ungranted repository
- GIVEN a robot holds a grant on `team/app` only
- WHEN it presents a token against `team/other`
- THEN the request MUST be rejected as unauthorized

### Requirement: Bounded, Revocable Robot Tokens

Robot token creation MUST enforce a maximum TTL ceiling; there MUST be no
never-expiring option. A robot token MUST be revocable at any time, and
revocation MUST take effect immediately.

#### Scenario: TTL above the ceiling is rejected
- GIVEN an operator requests a robot token with a TTL above the ceiling
- WHEN the creation request is submitted
- THEN the system MUST reject it

#### Scenario: Revoked token is denied immediately
- GIVEN a robot token is valid
- WHEN an operator revokes it
- THEN the next request using that token MUST be rejected

### Requirement: Robots Excluded From Default Human User Listing

The default user listing endpoint MUST exclude robot rows.

#### Scenario: Default listing omits robots
- GIVEN at least one robot account and one human user exist
- WHEN an operator requests the default user listing
- THEN the response MUST include the human user and MUST NOT include the
  robot

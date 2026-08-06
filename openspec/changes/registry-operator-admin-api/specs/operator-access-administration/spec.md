# Operator Access Administration Specification

## Purpose

Define reversible operator workflows for repository grants and admin credential tokens.

## Current Repository Facts (Non-normative)

- Backend admin methods already manage repo grants and admin credential tokens.
- Admin credential tokens already have bounded TTL and revocation support.

## Requirements

### Requirement: Repository grant management is reversible

The system MUST support listing, putting, and deleting repository grants for a target user and MUST validate the target user, repository, and role before mutating access.

#### Scenario: Admin replaces a repository grant

- GIVEN an authenticated admin actor and an existing target user
- WHEN the actor puts a repository grant for that user
- THEN the stored grant reflects the requested repository and role

#### Scenario: Invalid grant input is rejected

- GIVEN a missing user, invalid repository, or invalid role
- WHEN the client submits a grant mutation
- THEN the request is rejected

### Requirement: Admin credential tokens stay bounded and revocable

The system MUST support listing, creating, and revoking admin credential tokens, MUST return plaintext token material only at creation time, and MUST enforce existing TTL and target-user safety rules.

#### Scenario: Token creation returns one-time secret

- GIVEN an authenticated admin actor and an enabled target user
- WHEN the actor creates an admin credential token
- THEN the response includes the new secret once with token metadata

#### Scenario: Excessive TTL or disabled target is rejected

- GIVEN a create-token request exceeds the allowed TTL or targets a disabled user
- WHEN the client submits the request
- THEN the request is rejected

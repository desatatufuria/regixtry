# Registry Authentication Specification

## Purpose

Define how v1 clients authenticate to the registry and how operators manage bearer tokens.

## Current Repository Facts (Non-normative)

- Today the registry only exposes a Bearer challenge and a binary anonymous allow/deny controller.
- There is no login endpoint, no token persistence, and no revocation flow.

## Requirements

### Requirement: Local user login and token lifecycle

The system MUST authenticate local username/password users, MUST let admins create, list, and revoke preissued tokens, MUST persist auth data in Postgres, and MUST use conservative token expiry with explicit revocation.

#### Scenario: Password login issues a bearer token

- GIVEN an active local user with valid credentials
- WHEN the user submits a login request
- THEN the system issues a bearer token with an expiry and principal identity

#### Scenario: Revoked or expired token is rejected

- GIVEN a revoked or expired token
- WHEN the token is presented to a protected registry endpoint
- THEN the system rejects the request as unauthorized

### Requirement: Standard bearer challenge behavior

The system MUST return a standard Bearer challenge for unauthenticated access to protected endpoints and MUST challenge catalog, tags, and manifest/blob pulls whenever anonymous pull is disabled.

#### Scenario: Discovery is challenged when anonymous pull is off

- GIVEN anonymous pull is disabled
- WHEN an unauthenticated client requests catalog or tags
- THEN the response is unauthorized and includes the Bearer challenge

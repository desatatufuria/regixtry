# Operator User Administration Specification

## Purpose

Define the first reversible operator workflows for user administration.

## Current Repository Facts (Non-normative)

- Backend admin methods already exist for listing users, creating users, enabling or disabling users, and resetting passwords.
- Broader profile updates and deletion exist in code but are not yet exposed as a safe operator API.

## Requirements

### Requirement: First-slice user actions stay reversible

The system MUST support list user, create user, enable user, disable user, and reset password actions, and MUST NOT expose delete-user or broad profile-update endpoints in this slice.

#### Scenario: Admin creates and later enables a user

- GIVEN an authenticated admin actor
- WHEN the actor creates a user and later enables that user
- THEN both actions succeed through `/admin/v1`

#### Scenario: Out-of-scope user mutation is withheld

- GIVEN a client needs delete-user or broad profile edits
- WHEN the client uses the first operator admin API slice
- THEN that mutation is unavailable

### Requirement: Existing user safety rules are preserved

The system MUST reuse existing admin-only checks, password validation, username uniqueness, disabled-user handling, and last-active-admin protection.

#### Scenario: Weak reset password is rejected

- GIVEN an authenticated admin actor
- WHEN the actor submits an invalid replacement password
- THEN the request is rejected as validation failure

#### Scenario: Sole active admin cannot be disabled

- GIVEN exactly one active global admin remains
- WHEN an admin request tries to disable that account
- THEN the request is rejected as a conflict

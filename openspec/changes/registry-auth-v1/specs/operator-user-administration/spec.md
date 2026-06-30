# Operator User Administration Specification

## Purpose

Define the first-slice operator workflows for managing local users and repository grants.

## Current Repository Facts (Non-normative)

- Today the TUI is an inspection console for repositories, tags, manifests, blobs, and uploads.
- The TUI currently runs with a local operator access controller that bypasses registry authorization.

## Requirements

### Requirement: Minimal TUI user administration

The system MUST provide TUI workflows for user create, user update, password reset, user disable/enable, and repository grant assignment/removal for local users.

#### Scenario: Operator grants repository access

- GIVEN an operator is managing local users in the TUI
- WHEN the operator assigns `repo-writer` for repository `team/app` to a user
- THEN subsequent authorization decisions use that grant

#### Scenario: Operator resets a password

- GIVEN an existing local user
- WHEN the operator resets that user's password
- THEN the old password no longer authenticates and the new password does

### Requirement: Admin-only token administration

The system MUST restrict token create, list, and revoke operations to administrators and MUST NOT expose self-service token minting in this slice.

#### Scenario: Non-admin cannot preissue a token

- GIVEN an authenticated principal without administrator authority
- WHEN that principal attempts to create or revoke a token
- THEN the system rejects the operation as forbidden

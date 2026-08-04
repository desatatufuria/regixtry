# Repository Authorization Specification

## Purpose

Define repository-scoped permissions for discovery, pull, push, and administrative control.

## Current Repository Facts (Non-normative)

- Today access decisions are anonymous-only and do not carry principal claims.
- Current actions already distinguish `pull`, `push`, `catalog`, and `inspect`, but no repository grant model exists.

## Requirements

### Requirement: Fixed repository role grants

The system MUST support only `repo-reader`, `repo-writer`, and `repo-admin` grants scoped to a repository. Authorization decisions MUST evaluate the authenticated principal and the target repository.

#### Scenario: Reader can pull but not push

- GIVEN a user has `repo-reader` on repository `team/app`
- WHEN the user pulls manifests, blobs, or tags from `team/app`
- THEN the request succeeds and push operations remain forbidden

#### Scenario: Authorization is repository-scoped

- GIVEN a user has `repo-writer` on `team/app` only
- WHEN the user pushes or reads `team/other`
- THEN the system rejects the request as unauthorized

### Requirement: Discovery visibility follows grants

When anonymous pull is disabled, the system MUST limit catalog and tag discovery to repositories the principal is allowed to read.

#### Scenario: Catalog excludes unauthorized repositories

- GIVEN an authenticated user can read only `team/app`
- WHEN the user requests the catalog
- THEN the response includes `team/app` and excludes repositories without read access

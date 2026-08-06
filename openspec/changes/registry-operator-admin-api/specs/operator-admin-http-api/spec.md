# Operator Admin HTTP API Specification

## Purpose

Define the authenticated HTTP contract for operator administration.

## Current Repository Facts (Non-normative)

- Today the router exposes `/auth/token` and `/v2/*`, not operator admin routes.
- `bootstrap-admin` remains the only local break-glass mutation path.

## Requirements

### Requirement: Authenticated admin namespace

The system MUST expose operator administration only through authenticated `/admin/v1` HTTP endpoints and MUST NOT add local CLI or TUI mutation shortcuts for this slice.

#### Scenario: Admin bearer token reaches an in-scope route

- GIVEN an authenticated admin bearer token
- WHEN the client calls an in-scope `/admin/v1` endpoint
- THEN the request is authorized through the shared admin service boundary

#### Scenario: Registry auth flow stays unchanged

- GIVEN existing `/auth/token` and `/v2/*` traffic
- WHEN `/admin/v1` is added
- THEN registry authentication and challenge behavior remain unchanged

### Requirement: Admin transport errors are explicit

The system MUST distinguish unauthorized, forbidden, validation, and conflict outcomes for `/admin/v1` without reusing ambiguous success or challenge semantics.

#### Scenario: Missing or invalid bearer token

- GIVEN no valid admin bearer token
- WHEN the client calls `/admin/v1`
- THEN the response is unauthorized

#### Scenario: Non-admin actor calls an admin route

- GIVEN an authenticated non-admin actor
- WHEN the actor calls `/admin/v1`
- THEN the response is forbidden

# Operator User Administration Specification

## Purpose

Define the currently shipped auth-enabled TUI behavior for operator-facing auth administration.

## Current Repository Facts (Non-normative)

- Today the TUI is an inspection console for repositories, tags, manifests, blobs, and uploads.
- In auth-enabled mode, the shipped TUI intentionally does not authenticate a local operator into backend admin capabilities.
- Auth-backed user, grant, and token mutations are deferred until a real operator login flow exists.

## Requirements

### Requirement: Auth-enabled TUI remains inspection-oriented

When auth is enabled, the system MUST keep repository inspection available and MUST NOT enable local admin mutations through the TUI.

#### Scenario: Operator can still inspect repositories in auth-enabled mode

- GIVEN auth is enabled for the local runtime
- WHEN the operator launches the TUI snapshot or interactive inspection view
- THEN repository inspection data remains available

#### Scenario: Auth-backed admin shortcuts stay unavailable

- GIVEN auth is enabled for the local runtime
- WHEN the operator opens the TUI
- THEN the local TUI does not expose or activate backend admin mutation workflows

### Requirement: Auth-enabled TUI shows an explicit security notice

The system MUST render a clear notice that local auth-backed admin actions are intentionally disabled until a real operator login flow exists.

#### Scenario: Security notice explains the deferred admin path

- GIVEN auth is enabled for the local runtime
- WHEN the TUI renders its inspection view
- THEN the output explains that auth-backed admin actions are disabled in the local TUI

### Requirement: Safe operator login and admin UI remain deferred

The system MUST defer local auth-backed user, grant, and token mutations until a real operator authentication flow is designed and delivered.

#### Scenario: Deferred operator admin workflow is not claimed as shipped

- GIVEN the current auth-enabled TUI release
- WHEN operator-facing documentation or verification artifacts describe TUI behavior
- THEN they describe inspection plus the security notice, not shipped local admin mutation workflows

# Operator Console Specification

## Purpose

Define the v1 operator TUI as an inspection client for registry state.

## Requirements

### Requirement: Keyboard-First Operational Visibility

The system MUST provide a keyboard-first console that shows repository, tag, manifest, blob, and upload-state information for the connected registry instance.

#### Scenario: Operator inspects registry state

- GIVEN the registry contains repositories and related content state
- WHEN an operator navigates the console
- THEN the console shows current registry information without requiring direct storage access

#### Scenario: No content exists

- GIVEN the connected registry has no published repositories
- WHEN an operator opens the console
- THEN the console shows an empty-state view without implying missing or failed data

### Requirement: Thin Client Boundary

The console MUST act as an operator client and MUST NOT be the source of registry domain rules. Any maintenance action exposed in v1 SHALL execute through service-defined interfaces.

#### Scenario: Inspection request is issued

- GIVEN an operator requests manifest or blob details from the console
- WHEN the console retrieves the information
- THEN the console reflects service-provided results rather than recalculating registry state locally

#### Scenario: Unsupported domain mutation is requested

- GIVEN an operator attempts a capability outside v1 scope, including deletion or retention control
- WHEN the request is made from the console
- THEN the console indicates the capability is unavailable in v1

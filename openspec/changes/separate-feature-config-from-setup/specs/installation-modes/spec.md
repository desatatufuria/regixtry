# Delta for installation-modes

## ADDED Requirements

### Requirement: Base Bootstrap Scope

Setup provenance MUST describe only base bootstrap truth: binary/service lifecycle, network/TLS reachability, storage, and required auth/runtime inputs. Optional feature settings MUST be stored outside install-owned truth.

#### Scenario: Setup records base lifecycle truth only

- GIVEN an operator completes setup on a supported host
- WHEN lifecycle provenance is written
- THEN the system SHALL record only base bootstrap inputs and outcomes

## MODIFIED Requirements

### Requirement: Scoped Rollback

Rollback and uninstall MUST be provenance-driven best-effort cleanup for recorded base lifecycle assets only. The system SHALL remove recorded base artifacts and stop or disable recorded services without requiring prior operator parameters, and it MUST report removed, skipped, or already-missing items truthfully. The system MUST NOT claim cleanup of feature-owned runtime state unless that feature recorded separate lifecycle evidence.

(Previously: Rollback/uninstall spoke about recorded lifecycle artifacts in general without separating base assets from feature-owned state.)

#### Scenario: Recorded base lifecycle state is cleaned up

- GIVEN a prior setup recorded service and runtime artifacts
- WHEN rollback or uninstall runs
- THEN the system SHALL remove recorded base artifacts best effort and leave a truthful cleanup report

#### Scenario: Feature-owned state is not overstated

- GIVEN a feature persists its own runtime settings outside base provenance
- WHEN rollback or uninstall runs from base lifecycle provenance alone
- THEN the system MUST NOT claim that feature-owned state was removed

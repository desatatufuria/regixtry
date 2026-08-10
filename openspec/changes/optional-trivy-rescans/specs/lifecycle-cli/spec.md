# Delta for lifecycle-cli

## ADDED Requirements

### Requirement: Rescan Configuration Provenance

`regixtry setup` MUST persist managed Trivy rescan configuration as lifecycle provenance, and `regixtry uninstall` MUST use that provenance to remove or truthfully report managed rescan configuration cleanup without affecting published registry content.

#### Scenario: Setup records managed rescan configuration

- GIVEN setup completes with optional Trivy rescan settings
- WHEN lifecycle provenance is recorded
- THEN the system SHALL include the managed rescan configuration needed for truthful later lifecycle actions

#### Scenario: Uninstall reports managed rescan cleanup truthfully

- GIVEN lifecycle provenance records managed rescan configuration
- WHEN the operator runs `regixtry uninstall`
- THEN the system MUST remove or clearly report skipped managed rescan cleanup items without overstating cleanup

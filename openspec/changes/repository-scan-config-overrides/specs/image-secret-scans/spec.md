# Delta for image-secret-scans

## Baseline Note

Builds on the merged `image-secret-scans` capability
(`openspec/changes/gitleaks-managed-feature/specs/image-secret-scans/spec.md`).
This change adds a per-repository gitleaks config-path override; it does not
change scan scope, the redacted findings model, the informational
(non-gating) nature of secret findings, or operator visibility, and it does
not add a schedule/interval concept to gitleaks. **Corrected**: the
originally shipped "Reused Rescan Trigger, No Push-Time Path" requirement's
*trigger-source* claim still holds — gitleaks has no push-specific trigger
of its own, it only reuses the manual/scheduled rescan trigger constant —
but that requirement's practical "push never runs gitleaks" implication no
longer holds. `executeScanRun`
(`internal/app/regixtry/service_scanning.go:295,305`) unconditionally
launches `go s.executeSecretScanLeg(...)` regardless of `run.Trigger`, and
`executeScanRun` is itself reached by `queuePushScan`
(`internal/app/regixtry/service_scanning.go:261-270`), shipped by the
`scan-policy-gate` change after the original gitleaks spec was written.
Gitleaks therefore already executes on push today, using the same
trigger-agnostic secret-scan leg as manual and scheduled rescans.

## ADDED Requirements

### Requirement: Gitleaks Scan Honors A Per-Repository Config Path Override

When a repository has an override configured for the gitleaks feature, the
system MUST invoke that repository's gitleaks scan using the override's
`ConfigPath` field whenever it is set, passing it to gitleaks as its
configuration file. When a repository has no gitleaks override, the
gitleaks invocation MUST have no config-path argument, identical to today's
argv (which has no `--config` support at all).

#### Scenario: Repository with a config-path override changes the gitleaks invocation

- GIVEN a repository has a gitleaks override with `ConfigPath` set to a
  readable server-local `.gitleaks.toml` path
- WHEN a secret scan runs for that repository
- THEN the gitleaks invocation MUST include the config-path argument
  pointing at that path

#### Scenario: Repository without an override scans with unchanged argv

- GIVEN a repository has no gitleaks override row
- WHEN a secret scan runs for that repository
- THEN the gitleaks invocation MUST have no config-path argument, matching
  today's fixed argv

#### Scenario: Missing override config path fails the run

- GIVEN a repository's gitleaks override sets `ConfigPath` to a path that
  does not exist on the regixtry host at scan time
- WHEN a secret scan runs for that repository
- THEN the system MUST set that repository's `SecretScanRun` status to
  failed with the error populated, and MUST NOT execute the gitleaks scan
  without the configured config file

#### Scenario: Unreadable override config path fails the run

- GIVEN a repository's gitleaks override sets `ConfigPath` to a path that
  exists but is unreadable by the scanning process at scan time
- WHEN a secret scan runs for that repository
- THEN the system MUST set that repository's `SecretScanRun` status to
  failed with the error populated, and MUST NOT execute the gitleaks scan
  without the configured config file

### Requirement: A Disabling Gitleaks Override Suppresses That Repository's Secret Scan Execution

When a repository's resolved effective gitleaks settings (override, or
global when no override exists) have `Enabled = false`, the system MUST
suppress secret scan execution for that repository through the existing
manual and scheduled rescan orchestration gitleaks already reuses, without
introducing any new gitleaks trigger path.

#### Scenario: Overridden repository is skipped by the reused rescan orchestration

- GIVEN a repository has a gitleaks override with `Enabled = false`, and the
  gitleaks global settings have `Enabled = true`
- WHEN a manual or scheduled rescan is triggered for that repository
- THEN the system MUST NOT execute a gitleaks secret scan for that
  repository as part of that rescan

#### Scenario: Override re-enables secret scanning while the global row is disabled

- GIVEN a repository has a gitleaks override with `Enabled = true`, and the
  gitleaks global settings have `Enabled = false`
- WHEN a manual or scheduled rescan is triggered for that repository
- THEN the system MUST execute a gitleaks secret scan for that repository as
  part of that rescan

#### Scenario: Overridden repository does not scan on push

- GIVEN a repository has a gitleaks override with `Enabled = false`, and the
  gitleaks global settings have `Enabled = true`
- WHEN an image is pushed to that repository
- THEN the system MUST NOT execute a gitleaks secret scan for that
  repository as a consequence of that push

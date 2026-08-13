# Repository Config Overrides Specification

## Purpose

Define a generic, feature-agnostic mechanism for storing and resolving a
per-repository configuration override that replaces a scan feature's global
settings for one repository: full-row-replace resolution with global
fallback, inert-orphan repository-lifecycle semantics, fail-closed handling
of server-local override paths, and admin-authorized HTTP management.

## Current Repository Facts

- `loadFeatureSettings` (`internal/app/regixtry/feature_registry.go:237`)
  already establishes the row-presence-boundary pattern this mechanism
  reuses: a `NotFound` global settings row resolves to in-code defaults,
  never an error. Per-repository override resolution reuses that same
  presence/absence boundary against the global row.
- `domain.RepositoryRef` (`internal/domain/regixtry/repository.go:10`) is a
  bare `{Name string}`; no per-repository settings concept exists anywhere
  in the codebase before this change.
- `/admin/v1/scan-settings` is the existing admin-authorized global settings
  endpoint whose authorization this mechanism's admin endpoints mirror.
- `ports.ScanSettings.TLSCACertPath` is existing precedent for a
  server-local path the scanning process reads directly off its own
  filesystem, not content inside a scanned image.

## Requirements

### Requirement: Override Storage Is Generic Across Feature Names

The system MUST support storing and retrieving a repository-scoped
configuration override for any feature name without requiring a schema or
storage-shape change to support a new feature. A feature that has never
stored an override before MUST be able to persist and retrieve one through
the same generic mechanism used by every other feature.

#### Scenario: Override stored for an existing feature

- GIVEN a repository and a feature that already has override support wired
  up (for example Trivy)
- WHEN an override is stored for that repository and feature
- THEN a later read for that repository and feature MUST return the stored
  override

#### Scenario: A new feature stores an override with no schema change

- GIVEN a feature name that has never had an override stored before, and no
  dedicated table or column exists for it
- WHEN an override is stored for that feature name for some repository
- THEN the system MUST persist and later retrieve it through the same
  generic mechanism, without any migration or schema change

### Requirement: Full-Row-Replace Resolution With Global Fallback

The system MUST resolve a repository and feature's effective configuration
as follows: when an override row exists for that repository and feature,
all of its typed fields apply in full; when no override row exists, the
feature's global settings row applies in full. The system MUST NOT merge
individual fields across the override/global boundary.

#### Scenario: Override present applies in full

- GIVEN a repository has an override row for a feature, and the global
  settings row for that feature has different field values
- WHEN the effective configuration for that repository and feature is
  resolved
- THEN every field of the result MUST come from the override row

#### Scenario: Override absent falls back to global in full

- GIVEN a repository has no override row for a feature
- WHEN the effective configuration for that repository and feature is
  resolved
- THEN every field of the result MUST come from the feature's global
  settings row, unchanged from today's single-global-row behavior

#### Scenario: Override fields are never blended with the global row

- GIVEN a repository has an override row for a feature whose field values
  differ from the global row's field values for the same feature
- WHEN the effective configuration is resolved
- THEN no field of the result MAY come from the global row while the
  override row exists; the override row is used exclusively

### Requirement: Server-Local Override Paths Fail The Scan Run When Missing Or Unreadable

Any override field that names a server-local file path (for example an
ignore file, ignore policy, or scanner config path) MUST be treated as a
path read directly by the scanning process off the regixtry host's own
filesystem, never as content related to the scanned image. If that path is
missing or unreadable at scan time, the system MUST fail that feature's scan
run for that repository — recording a failed status with the error
populated on the run record — and MUST NOT proceed to scan using weaker or
default rules than the operator configured.

#### Scenario: Missing override path fails the run

- GIVEN a repository's override for a feature names a server-local path that
  does not exist on the regixtry host at scan time
- WHEN a scan run executes for that repository under that feature
- THEN the system MUST record that run as failed with a populated error, and
  MUST NOT execute the scan with default or weaker rules instead

#### Scenario: Unreadable override path fails the run

- GIVEN a repository's override for a feature names a server-local path that
  exists but is unreadable by the scanning process at scan time
- WHEN a scan run executes for that repository under that feature
- THEN the system MUST record that run as failed with a populated error, and
  MUST NOT execute the scan with default or weaker rules instead

### Requirement: A Disabling Override Suppresses That Repository's Scan Triggers For The Feature

When a repository's resolved effective settings for a feature have
`Enabled = false` — whether because the override sets it or, absent an
override, because the global row sets it — the system MUST suppress every
scan trigger this codebase runs for that feature and that repository,
including scheduled-sweep participation and, for features whose
orchestration includes a push-triggered scan, push-triggered scanning. This
MUST hold regardless of what the global row alone would otherwise allow.

#### Scenario: Override disables a repository while the global row stays enabled

- GIVEN a repository has an override for a feature with `Enabled = false`,
  and that feature's global settings have `Enabled = true`
- WHEN the scheduled sweep or, for a feature with a push trigger, a push for
  that repository would otherwise start a scan
- THEN the system MUST NOT queue or execute a scan run for that repository
  under that feature

#### Scenario: Override re-enables a repository while the global row is disabled

- GIVEN a repository has an override for a feature with `Enabled = true`,
  and that feature's global settings have `Enabled = false`
- WHEN the scheduled sweep or, for a feature with a push trigger, a push for
  that repository would otherwise start a scan
- THEN the system MUST queue and execute a scan run for that repository
  under that feature

### Requirement: Override Rows Are Not Cascade-Deleted On Repository Lifecycle Changes

The system MUST NOT delete, migrate, or otherwise clean up override rows
when a repository is deleted or renamed. Resolution MUST be an exact
repository-name lookup, so an override row left behind for a repository name
that no longer exists MUST have no observable effect on any current
repository.

#### Scenario: Deleted repository leaves an inert override row

- GIVEN a repository has an override row for a feature, and that repository
  is later deleted
- WHEN override cleanup is considered
- THEN the system MUST leave the override row in place, and it MUST have no
  effect because no current repository resolves to it

#### Scenario: A renamed repository does not carry its override forward

- GIVEN a repository with an override row is renamed to a new name
- WHEN the effective configuration is resolved for the new name
- THEN the system MUST resolve it as if no override exists (the old row is
  looked up by the old exact name only and is now inert)

### Requirement: Override Authorization Matches Existing Admin Scan-Settings Authorization

Reading, setting, or clearing a repository's feature override MUST require
exactly the same authorization already enforced for the existing global
scan-settings admin endpoint. This change MUST NOT introduce a new
permission surface, role, or scope.

#### Scenario: An admin caller can manage an override

- GIVEN a caller is authorized the same way the existing scan-settings admin
  endpoint already authorizes admin callers
- WHEN that caller reads, sets, or clears a repository's feature override
- THEN the system SHALL allow the request

#### Scenario: A non-admin caller is rejected

- GIVEN a caller does not hold the authorization the existing scan-settings
  admin endpoint already requires
- WHEN that caller attempts to read, set, or clear a repository's feature
  override
- THEN the system MUST reject the request without exposing override data

### Requirement: Admin HTTP Resource For One Repository And Feature Override

The system MUST provide an admin HTTP resource scoped to one repository and
one feature name that supports: reading the current override (or an
explicit indication that none exists and the global row applies), replacing
it in full, and deleting it. Deleting the override MUST immediately revert
that repository and feature to the global settings row.

#### Scenario: Reading returns the current override

- GIVEN a repository has an override row for a feature
- WHEN an authorized caller reads that repository and feature's override
  resource
- THEN the system SHALL return the override's current field values

#### Scenario: Reading indicates no override is set

- GIVEN a repository has no override row for a feature
- WHEN an authorized caller reads that repository and feature's override
  resource
- THEN the system SHALL indicate no override exists and that the global
  settings apply

#### Scenario: Writing creates or replaces the override

- GIVEN an authorized caller submits a full set of override field values for
  a repository and feature
- WHEN the write is processed
- THEN the system MUST persist it as that repository and feature's override,
  replacing any prior override in full

#### Scenario: Deleting reverts to global settings

- GIVEN a repository has an override row for a feature
- WHEN an authorized caller deletes that repository and feature's override
- THEN the system MUST remove the override row, and the next resolution for
  that repository and feature MUST use the global settings row in full

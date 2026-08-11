# Image Secret Scans Specification

## Purpose

Define secret-detection scanning of stored images using the managed Gitleaks runtime, with a redacted findings model and operator visibility, informational only in this change.

## Current Repository Facts

- Regixtry has layer access via `ListManifestBlobs` and `OpenBlob`, reused as-is for reading scanned image content.
- Vulnerability scanning already has manual and scheduled rescan orchestration (Trivy); this capability reuses that orchestration rather than adding a separate trigger path.

## Requirements

### Requirement: Managed Gitleaks Runtime

Gitleaks MUST be managed as one instance of the `managed-feature-runtime` contract: install, verify, activate, rollback, and status follow the same shared sequence as every other managed feature.

#### Scenario: Gitleaks reports status through the same operator surfaces as Trivy

- GIVEN Gitleaks is installed and active
- WHEN an operator requests feature status
- THEN the system SHALL report Gitleaks' version, readiness, and rollback availability through the same surfaces used for Trivy

### Requirement: Secret Scan Scope Covers Current Manifest Layers And Config Blob

The system MUST scan every layer blob referenced by the manifest currently under scan, matching Trivy's existing layer scan scope. The system MUST additionally scan that manifest's config blob (its declared `ENV` values and build history), staged as a plain file alongside the layers in the same run. Layer or config blobs reachable only from older or superseded tags MUST be excluded.

#### Scenario: Current manifest layers are scanned

- GIVEN an image manifest is queued for a scan
- WHEN the secret scan runs
- THEN the system SHALL scan every layer referenced by that manifest

#### Scenario: Current manifest config blob is scanned

- GIVEN an image manifest is queued for a scan
- WHEN the secret scan runs
- THEN the system SHALL stage that manifest's config blob as a plain file and scan it in the same run as the layers

#### Scenario: Superseded-tag-only layers are excluded

- GIVEN a layer is reachable only from a tag that no longer points at the current manifest
- WHEN a secret scan runs for the current manifest
- THEN the system MUST NOT scan that layer

#### Scenario: Superseded-tag-only config blob is excluded

- GIVEN a config blob is reachable only from a tag that no longer points at the current manifest
- WHEN a secret scan runs for the current manifest
- THEN the system MUST NOT scan that config blob

### Requirement: Reused Rescan Trigger, No Push-Time Path

Secret scans MUST run through the existing manual and scheduled rescan orchestration. This change MUST NOT add a separate push-time secret-scan trigger.

#### Scenario: Manual rescan includes secret scanning

- GIVEN an operator triggers a manual rescan for an image
- WHEN the rescan executes
- THEN the system SHALL run the secret scan through the same orchestration as the vulnerability scan

#### Scenario: Image push does not trigger a secret scan

- GIVEN an image is pushed to the registry
- WHEN the push completes
- THEN the system MUST NOT trigger a secret scan as a direct consequence of that push

### Requirement: Redacted Secret Findings Model

A persisted finding MUST include only the rule ID and location (layer, path, line). The system MUST NOT persist the matched secret text or any fingerprint derived from it.

#### Scenario: Detected secret persists without secret material

- GIVEN Gitleaks detects a secret in a scanned layer
- WHEN the finding is persisted
- THEN the stored record SHALL contain the rule ID and location only

#### Scenario: Stored finding cannot reconstruct the secret

- GIVEN a persisted finding is read back
- WHEN it is inspected
- THEN the system MUST NOT expose the original matched value or a value derived from it

### Requirement: Informational Findings Only

Secret findings MUST NOT gate image pull or promotion in this change. No severity or policy dimension is introduced.

#### Scenario: Image with a finding remains pullable

- GIVEN an image has one or more persisted secret findings
- WHEN a client pulls that image
- THEN the system SHALL serve the pull without blocking on the findings

### Requirement: Operator Visibility of Findings

Findings MUST be surfaced through existing scan/admin/TUI seams, attributed to the image they were found in.

#### Scenario: Known test secret produces an attributed finding

- GIVEN an image contains a known test secret
- WHEN it is scanned
- THEN the system SHALL produce a finding attributed to that image, visible through the existing scan surfaces

#### Scenario: Operator reviews findings for a selected image

- GIVEN an authenticated operator selects an image with findings
- WHEN the operator opens its scan detail
- THEN the system SHALL display the secret findings alongside vulnerability results

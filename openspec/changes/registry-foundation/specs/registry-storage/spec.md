# Registry Storage Specification

## Purpose

Define v1 storage behavior for durable local registry content and upload state.

## Requirements

### Requirement: Content-Addressed Local Persistence

The system MUST persist blobs by digest in local storage and MUST preserve manifest-to-blob referential integrity for published content.

#### Scenario: Published content is durable

- GIVEN a manifest references stored blobs
- WHEN the content is published and later requested
- THEN the system returns the manifest and each referenced blob from local storage

#### Scenario: Referenced blob is unavailable

- GIVEN a manifest references a blob that is not durably available
- WHEN publication is attempted
- THEN the system rejects publication and MUST NOT expose the manifest as available content

### Requirement: Safe Upload Lifecycle

The system MUST track in-progress uploads separately from published content and MUST prevent incomplete uploads from appearing in repository state. V1 MUST NOT require deletion, retention, or operator-triggered garbage collection.

#### Scenario: Upload remains incomplete

- GIVEN a client starts an upload but does not complete it
- WHEN a repository or tag listing is requested
- THEN the incomplete upload is not shown as published registry content

#### Scenario: Restart after incomplete upload

- GIVEN incomplete upload state exists before a service restart
- WHEN the service resumes operation
- THEN published content remains readable and incomplete state does not corrupt visible registry content

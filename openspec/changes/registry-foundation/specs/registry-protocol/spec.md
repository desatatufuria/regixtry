# Registry Protocol Specification

## Purpose

Define the externally observable registry API boundary for v1 image distribution behavior.

## Requirements

### Requirement: OCI-Compatible Content Exchange

The system MUST support OCI/Docker-compatible push and pull for manifests and blobs, and MUST validate digest-addressed reads and writes.

#### Scenario: Push and pull succeeds

- GIVEN a client uploads valid blobs and a valid manifest for a repository reference
- WHEN the client completes the push and later requests that reference
- THEN the system returns the same manifest and required blobs using registry-compatible responses

#### Scenario: Digest validation fails

- GIVEN a client sends content whose computed digest does not match the declared digest
- WHEN the system validates the request
- THEN the system rejects the request and MUST NOT publish the invalid content

### Requirement: Repository Discovery and Access Modes

The system MUST expose repository listing, tag listing, and manifest/blob inspection for authorized clients. The system MAY allow anonymous pull when explicitly configured. The system MUST prepare for auth challenge flows without requiring full RBAC in v1.

#### Scenario: Anonymous pull is enabled

- GIVEN anonymous pull is enabled for the instance
- WHEN an unauthenticated client requests an existing manifest or blob
- THEN the system serves the content without requiring credentials

#### Scenario: Anonymous pull is disabled

- GIVEN anonymous pull is disabled for the instance
- WHEN an unauthenticated client requests protected registry content
- THEN the system rejects the request with an authentication challenge-compatible response

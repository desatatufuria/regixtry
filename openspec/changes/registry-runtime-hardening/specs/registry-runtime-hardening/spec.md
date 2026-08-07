# Registry Runtime Hardening Specification

## Purpose

Define a safer default runtime posture for the single-binary registry while preserving an explicit local/dev path.

## Current Repository Facts

| Area | Current fact |
|------|--------------|
| Transport | `serve` currently runs plain HTTP unless TLS is added externally. |
| URL validation | Public URL and auth realm consistency are not enforced at startup. |
| Runtime bounds | Server timeouts and shutdown bounds are not fully hardened today. |
| Bootstrap secrets | `bootstrap-admin` password entry is currently argv-oriented. |

## Requirements

### Requirement: TLS Runtime Modes

The system MUST support a TLS-enabled serve path for self-hosted local operators and small production deployments. The system MAY allow non-TLS startup for explicit local/dev usage, but it MUST keep that mode intentional and accurately reflected in the canonical runtime URLs it advertises.

#### Scenario: TLS-enabled startup

- GIVEN TLS runtime inputs and an HTTPS canonical public URL
- WHEN the registry starts successfully
- THEN registry endpoints SHALL be served over HTTPS
- AND advertised runtime URLs SHALL remain HTTPS-consistent

#### Scenario: Explicit local HTTP startup

- GIVEN no TLS runtime inputs and an HTTP canonical public URL for local/dev use
- WHEN the registry starts successfully
- THEN startup SHALL remain allowed
- AND the runtime SHALL NOT imply HTTPS capability

### Requirement: Canonical Public URL and Realm Validation

The system MUST validate canonical public URL and auth realm configuration before serving traffic. The system MUST fail startup when scheme, host, port, or path expectations would produce an auth challenge URL that does not match the configured canonical runtime surface.

#### Scenario: Matching public URL and realm

- GIVEN a canonical public URL and auth realm that resolve to the same external runtime surface
- WHEN startup validation runs
- THEN the registry SHALL continue startup

#### Scenario: Mismatched public URL and realm

- GIVEN a canonical public URL and auth realm that disagree on the exposed runtime surface
- WHEN startup validation runs
- THEN startup MUST fail before serving traffic

### Requirement: Hardened HTTP Runtime Bounds

The system MUST apply bounded HTTP runtime behavior appropriate for long-running service operation. It MUST protect request handling and shutdown with finite limits so idle, slow, or draining connections do not leave the process in an unsafe default state.

#### Scenario: Normal bounded serving

- GIVEN a valid runtime configuration
- WHEN the registry accepts client traffic
- THEN request handling SHALL use finite runtime bounds
- AND graceful shutdown SHALL complete within a defined limit when clients drain normally

#### Scenario: Shutdown exceeds the bound

- GIVEN a shutdown request and clients that do not drain within the configured limit
- WHEN the shutdown bound is reached
- THEN the registry MUST stop waiting and complete termination behavior predictably

### Requirement: Bootstrap Admin Secret Guidance

The system SHOULD provide a safer bootstrap-admin secret entry path that avoids routine password exposure through command-line arguments. If compatibility paths that expose secrets more broadly remain available, the system MUST mark them as discouraged and operator-facing guidance MUST prefer the safer path.

#### Scenario: Safer secret entry path used

- GIVEN an operator using the preferred bootstrap-admin secret input path
- WHEN bootstrap-admin runs successfully
- THEN the command SHALL complete without requiring the secret in argv

#### Scenario: Compatibility secret path used

- GIVEN an operator using a less-safe compatibility secret path
- WHEN bootstrap-admin starts
- THEN the system SHALL present guidance that the path is discouraged

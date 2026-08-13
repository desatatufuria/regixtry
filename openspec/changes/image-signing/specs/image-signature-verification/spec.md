# Image Signature Verification Specification

## Purpose

Define regixtry's content-trust gate: static-public-key signature
verification of images at pull time, global and per-repository policy
resolution, fail-closed enforcement, and a non-gating status endpoint for
CI. regixtry never signs; it only verifies signatures produced externally
by `cosign`.

## Requirements

### Requirement: Global Trusted-Key Signing Policy

The system MUST maintain one global signing policy consisting of an
`Enabled` flag and a set of trusted public keys, defaulting to
`Enabled = false`. When no repository override applies, this policy governs
verification for every repository.

#### Scenario: Default policy leaves pulls unaffected
- GIVEN no operator has configured the global signing policy
- WHEN any repository is pulled
- THEN the system MUST NOT block the pull for lack of a signature

#### Scenario: Enabling the policy activates verification
- GIVEN an operator enables the global policy with at least one trusted key
- WHEN a pull is resolved for a repository with no override
- THEN the system MUST verify against those keys

### Requirement: Per-Repository Signing Override, Both Directions

The system MUST support a repository-scoped signing override on the same
`repository_feature_overrides` mechanism used by scan-execution features
(`feature_name="signing"`, full-row-replace). A repository MAY require
signing when the global policy does not, and MAY be exempted when the
global policy requires it.

#### Scenario: Override requires signing while global does not
- GIVEN the global policy is disabled and a repository's override has
  `Enabled = true`
- WHEN that repository is pulled
- THEN the system MUST enforce verification for that repository only

#### Scenario: Override exempts a repository while global requires signing
- GIVEN the global policy is enabled and a repository's override has
  `Enabled = false`
- WHEN that repository is pulled
- THEN the system MUST NOT enforce verification for that repository

#### Scenario: Clearing the override reverts to global policy
- GIVEN a repository has a signing override
- WHEN an authorized caller deletes it
- THEN the next pull MUST be governed by the global policy alone

### Requirement: Pull-Time Gate Is Fail-Closed

The gate MUST block a pull whenever the resolved policy is enabled and the
image cannot be affirmatively verified — no signature found, an unreadable
or expired trusted key, or any verification error. This is the OPPOSITE
default from the vulnerability scan gate, which is deliberately fail-open
on uncertainty; the two gates coexist with intentionally different
failure defaults.

#### Scenario: No signature blocks the pull
- GIVEN signing is enabled and the digest has no signature artifact
- WHEN the image is pulled
- THEN the system MUST reject with a policy-violation error

#### Scenario: Unreadable or expired key blocks the pull
- GIVEN signing is enabled and the trusted key is unreadable or expired
- WHEN the image is pulled
- THEN the system MUST reject; it MUST NOT fall back to unverified accept

#### Scenario: Verification error blocks the pull
- GIVEN signing is enabled and verification of a present signature errors
- WHEN the image is pulled
- THEN the system MUST reject with a policy-violation error

#### Scenario: Contrast with the vulnerability gate's fail-open default
- GIVEN a digest has neither a completed scan nor a verifiable signature
- WHEN it is pulled with both gates enabled
- THEN the vulnerability gate MUST NOT block for the missing scan, while
  the signing gate MUST block for the missing signature

### Requirement: Verification Correctness Against Real Cosign Signatures

The system MUST accept a signature produced by an unmodified
`cosign sign --key <key>` against a trusted key, and MUST reject a
tampered signature or one from a key outside the trusted set. How
verification is implemented is not constrained by this specification.

#### Scenario: Valid cosign signature is accepted
- GIVEN an image signed with `cosign sign --key cosign.key` using a
  trusted key
- WHEN pulled under an enabled policy
- THEN the system MUST allow the pull

#### Scenario: Tampered or wrong-key signature is rejected
- GIVEN a signature was tampered with, or signed by an untrusted key
- WHEN pulled under an enabled policy
- THEN the system MUST reject with a policy-violation error

### Requirement: Legacy Tag Convention Push Path Is Unmodified

Pushing a `cosign` signature via the legacy tag convention (an ordinary
manifest at tag `sha256-<digest-hex>.sig`) MUST succeed through the
existing manifest push path with no signature-specific handling; pushing
an image without a signature MUST remain legal regardless of policy.

#### Scenario: Legacy-tag signature push round-trips unmodified
- GIVEN a running registry with no signing-specific push code
- WHEN `cosign sign --key cosign.key <repo>@<digest>` pushes via the tag
  convention
- THEN the push MUST succeed identically to any other manifest push

#### Scenario: Pushing an unsigned image remains legal
- GIVEN signing is enabled for a repository
- WHEN an unsigned image is pushed to it
- THEN the push MUST succeed; only the pull is gated

### Requirement: Registry-Scoped Signature-Status Endpoint

The system MUST expose an endpoint reporting the verification verdict and
digest for a repository/reference, authorized with the same pull action as
an ordinary pull, not an admin action. It MUST NOT itself be blocked by
the policy it reports, and MUST NOT expose key material or trust
configuration.

#### Scenario: Caller with pull credentials reads status
- GIVEN a caller holds ordinary pull authorization
- WHEN it requests signature-status for a digest
- THEN the system MUST return a verdict and digest, no admin auth required

#### Scenario: Status endpoint is never itself gated
- GIVEN policy would block the pull of a digest
- WHEN signature-status is queried for that digest
- THEN the request MUST succeed and report the blocking verdict

#### Scenario: Status response excludes trust configuration
- GIVEN a caller queries signature-status
- WHEN the response returns
- THEN it MUST NOT include key material or trust configuration detail

## Non-Goals

- No OCI 1.1 Referrers API route or subject→referrer index.
- `PublishManifest`'s rejection of subject digests pointing at manifests
  rather than blobs (the referrers-mode push conflict) is NOT fixed here;
  only the legacy tag convention is supported for discovery.
- No keyless/OIDC identity matching, Fulcio, or Rekor lookups exist after
  this capability, partially or otherwise.
- regixtry never holds a private signing key and never produces one.

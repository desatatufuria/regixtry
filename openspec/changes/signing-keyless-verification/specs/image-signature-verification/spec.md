# Delta for Image Signature Verification

## Baseline Note

Baseline is the unarchived `image-signing` change's spec at
`openspec/changes/image-signing/specs/image-signature-verification/spec.md`.
Its `## Non-Goals` line "No keyless/OIDC identity matching, Fulcio, or
Rekor lookups exist after this capability" is superseded by this delta and
MUST be dropped when merged; every other non-goal there is unaffected.

## MODIFIED Requirements

### Requirement: Global Trusted-Key-Or-Identity Signing Policy

The system MUST maintain one global signing policy consisting of an
`Enabled` flag, a set of trusted public keys (`TrustedPublicKeys`), and a
set of trusted identities (`TrustedIdentities`, each a Fulcio-certificate
SAN regexp plus a required OIDC issuer), defaulting to `Enabled = false`
and both anchor sets empty. A signature verifies if it matches ANY
trusted key OR ANY trusted identity; anchors never combine with AND. When
no repository override applies, this policy governs every repository.
(Previously: policy held only a set of trusted public keys, no identity
anchors, and `Enabled` required a trusted key.)

#### Scenario: Default policy leaves pulls unaffected
- GIVEN no operator has configured the global signing policy
- WHEN any repository is pulled
- THEN the system MUST NOT block the pull for lack of a signature

#### Scenario: Enabling with a key or an identity activates verification
- GIVEN an operator enables the global policy with at least one trusted
  key or one trusted identity
- WHEN a pull is resolved for a repository with no override
- THEN the system MUST verify against every configured anchor of either
  kind

#### Scenario: Identity-only policy verifies with no trusted key
- GIVEN the global policy has trusted identities and zero trusted keys
- WHEN a repository with no override is pulled
- THEN the system MUST verify using the identity anchors alone

## ADDED Requirements

### Requirement: Per-Repository Trusted-Identity Override Is Full-Row-Replace

`TrustedIdentities` on `SigningOverride` MUST persist with the same
full-row-replace semantics as `TrustedPublicKeys`: saving an override
replaces its entire anchor set. An override saved with keys but no
identities MUST clear any global identities for that repository — the
same accepted sharp edge already documented for `TrustedPublicKeys`, not
a defect.

#### Scenario: Override with keys only clears inherited identities
- GIVEN the global policy has trusted identities configured
- WHEN a repository override is saved with keys but no identities
- THEN that repository's resolved policy MUST NOT include any identity

#### Scenario: Override may set identities independently of keys
- GIVEN a repository has no override
- WHEN an override is saved with identities and no keys
- THEN that repository's resolved policy MUST verify against those
  identities only

### Requirement: Offline Keyless (Fulcio/OIDC) Bundle Verification

The system MUST verify a Sigstore bundle's embedded certificate against a
pinned trusted root, match the certificate SAN against a trusted
identity's regexp and its issuer extension against that identity's OIDC
issuer, and verify the embedded Signed Entry Timestamp (SET) offline.
Verification MUST perform zero network I/O.

#### Scenario: Matching identity and issuer verifies offline
- GIVEN a bundle's SAN and issuer both match a trusted identity
- WHEN the bundle is verified
- THEN the system MUST accept it without any network call

#### Scenario: Wrong issuer fails closed
- GIVEN a bundle's SAN matches a trusted identity but the issuer does not
- WHEN verified
- THEN the system MUST reject it, distinctly from a chain or SET failure

#### Scenario: Untrusted Fulcio chain fails closed
- GIVEN a bundle's certificate does not chain to the pinned trusted root
- WHEN verified
- THEN the system MUST reject it

#### Scenario: Tampered or missing SET fails closed
- GIVEN a bundle's Signed Entry Timestamp is tampered or absent
- WHEN verified
- THEN the system MUST reject it

#### Scenario: Expired certificate fails closed
- GIVEN a bundle's certificate is expired relative to the SET timestamp
- WHEN verified
- THEN the system MUST reject it

### Requirement: Verified State Surfaces Matched Identity Distinctly

When a pull verifies via the identity path, the system MUST report the
matched identity (certificate SAN and OIDC issuer) as its own
operator-facing value, and MUST NOT overload the existing key-fingerprint
field to represent it.

#### Scenario: Identity match reports SAN and issuer separately
- GIVEN a signature verifies against a trusted identity
- WHEN the signature-status endpoint or pull-time gate reports the
  verdict
- THEN it MUST include the matched SAN and issuer as a distinct value,
  separate from any key-fingerprint field

### Requirement: TSA-Backed Bundles And Live Rekor Are Out Of Scope And Fail Closed

The system MUST NOT verify a bundle whose only timestamp proof is a TSA
signature with no Rekor `tlogEntries`; it MUST fail closed as
unverifiable. The system MUST NOT perform a live Rekor query, MUST NOT
accept an operator-supplied Fulcio root override, and MUST NOT
auto-update its trusted root via TUF.

#### Scenario: TSA-only bundle fails closed
- GIVEN a bundle carries a certificate and a TSA timestamp with no Rekor
  tlog entry
- WHEN verified
- THEN the system MUST reject it as unverifiable, not silently accept it

#### Scenario: No live Rekor fallback on offline SET failure
- GIVEN a bundle's offline SET check fails
- WHEN verified
- THEN the system MUST NOT attempt a live Rekor query as a fallback

### Requirement: Legacy Static-Key Verification Path Is Unchanged

Existing static-key (`TrustedPublicKeys`-only) and legacy SimpleSigning
`.sig` verification behavior MUST remain unchanged by this capability:
identical acceptance, rejection, and error paths as before identity
anchors existed.

#### Scenario: Static-key-only policy behaves identically
- GIVEN a repository's resolved policy has trusted keys and zero trusted
  identities
- WHEN an image signed with a trusted key is pulled
- THEN verification MUST behave exactly as before this capability, with
  no identity-path code invoked

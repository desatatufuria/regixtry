# Exploration: Keyless (Fulcio/OIDC) Signature Verification for regixtry's Signing Policy Gate

## Current State

`internal/app/regixtry/service_signing.go` implements a fail-closed pull-time content-trust gate (`enforceSigningPolicy` → `verifySignature` → `verifyBundleSignature` fallback). Verification has exactly one trust anchor shape today: a list of PEM ECDSA P-256 public keys (`SigningPolicySettings.TrustedPublicKeys`), parsed by `parseTrustedKeys`/`signing.ParseTrustedKey`, and every signature — legacy SimpleSigning (`.sig` tag) or modern Sigstore-Bundle DSSE (`sha256-<hex>` bundle-index tag, confirmed) — is checked with the same primitive: `signing.Verify(trusted.key, payload/paeBytes, signatureBase64)` (`internal/domain/signing/keys.go:62`), a plain `ecdsa.VerifyASN1` over a caller-supplied static key. There is no code path anywhere in the repo that inspects an X.509 certificate, a Fulcio-issued short-lived cert, an OIDC identity claim, or a Rekor transparency-log entry (confirmed by a repo-wide grep for `Fulcio|Rekor|sigstore-go|x509.Certificate|CertPool` — zero matches in production code).

`internal/domain/signing/bundle.go`'s `bundleDocumentEnvelope` struct (lines 179-186) explicitly and deliberately does **not** model `verificationMaterial` (`tlogEntries`, `timestampVerificationData`, and — per Sigstore Bundle v0.3 schema — the certificate/certificate-chain that would live under `verificationMaterial.certificate` or `x509CertificateChain`). The doc comment says so outright: "this package never reads or validates it." `ParseBundleDocument` only extracts `dsseEnvelope.{payload, payloadType, signatures}` — it silently discards everything a keyless verification path would need. This is a parsing gap, not just a verification gap: even reaching the cert bytes requires extending `bundleDocumentEnvelope`.

The PUSH path is confirmed unaffected: cosign's keyless output format (an OCI Image Index at `BundleIndexTag`, referrer manifests with `artifactType: SigstoreBundleMediaType`, layer holding a Sigstore Bundle JSON document) is structurally identical whether the DSSE envelope was signed with a static key or a Fulcio ephemeral key — the push/store path has no signature-type awareness at all, so it already accepts and stores both. The gap is entirely in the verify loop's trust-anchor comparison.

## Affected Areas

- `internal/app/regixtry/service_signing.go` — `verifySignature`/`verifyBundleSignature` would need a second trust-anchor branch (identity-based) alongside the existing key-based loop; `enforceSigningPolicy` policy resolution is otherwise reusable as-is.
- `internal/domain/signing/bundle.go` — `bundleDocumentEnvelope`/`ParseBundleDocument` must be extended to surface `verificationMaterial.certificate` (or `x509CertificateChain`) and, if Rekor inclusion is required, `verificationMaterial.tlogEntries`. This is pure JSON-shape work, no crypto.
- `internal/domain/signing/keys.go` — needs a parallel verification primitive to `Verify`/`ParseTrustedKey`: something like `VerifyKeylessCertificate(cert *x509.Certificate, fulcioRoots *x509.CertPool, identity *regexp.Regexp, issuer string) error` plus DSSE-signature-vs-certificate-pubkey verification (the cert's public key, not a configured key, becomes the ECDSA verify key).
- `internal/ports/regixtry.go` — `SigningPolicySettings` (lines 344-361) and `SigningOverride` (363-374) would need a parallel field, e.g. `TrustedIdentities []TrustedIdentity{CertificateIdentityRegexp, CertificateOIDCIssuer string}`, mirroring `TrustedPublicKeys`'s shape and the same full-row-replace override semantics. `RepositoryOverrideDetails` (483-495) needs the matching wire projection.
- `internal/infra/metadata/sqlite/store.go` — `GetSigningPolicySettings`/`UpsertSigningPolicySettings` (lines 1251-1300) and the `signing_policy_settings` table (2055-2061) currently persist one JSON column (`trusted_public_keys`). A new `trusted_identities` JSON column, added via the same `ALTER TABLE` migration pattern already used at line 2099, is the natural parallel.
- `internal/protocol/http/admin_handlers.go` — `decodeSigningPolicySettings` (600-627) and `signingPolicySettingsResponse` (629+) need parallel decode/validate/serialize logic for identity entries (regex compile-validate at write time, mirroring `NormalizePublicKeyPEM`'s configuration-time validation posture).
- `internal/tui/screen_signing_config.go`, `internal/tui/trusted_key_list.go`, `internal/tui/override_editor.go` — the TUI's global signing config screen and per-repository override editor both reuse `trustedKeyList` for key add/select/delete; a keyless config UI needs an analogous list widget for identity-regexp/issuer pairs.
- `internal/app/regixtry/service_signing_bundle_test.go`, `internal/domain/signing/bundle_test.go`, `keys_test.go` — all existing bundle/verification tests exercise only static-key fixtures.

## Dependency Landscape

`go.mod` (confirmed, read directly) has **no Sigstore/cosign dependency at all** — only stdlib crypto (`crypto/ecdsa`, `crypto/x509`, `crypto/elliptic`), plus TUI/DB libraries unrelated to signing. There is zero partial plumbing anywhere in the repo (confirmed by grep) for:

- A Fulcio root/intermediate CA trust bundle (would need embedding a static copy of Sigstore's TUF-distributed root, or fetching/pinning it — `sigstore/sigstore-go` bundles this, stdlib `x509` does not)
- Certificate-chain verification against that root (stdlib `crypto/x509.Certificate.Verify` *can* do the chain-verification mechanics once a root `CertPool` exists, since Fulcio issues standard X.509 certs — this part is stdlib-reachable)
- SAN/OIDC-issuer-extension parsing from a Fulcio cert (Fulcio embeds the OIDC issuer/identity in specific X.509v3 extension OIDs — not something stdlib `x509` parses out of the box; would need manual ASN.1 extension parsing or a Sigstore-aware library)
- Rekor client or offline Signed Entry Timestamp (SET) verification

Two real implementation paths exist: (a) add `github.com/sigstore/sigstore-go` (or the narrower `sigstore/cosign` verify packages) as a new dependency — heavier but battle-tested and handles Fulcio extension OIDs, root bundle updates, and Rekor verification correctly; or (b) hand-roll cert-chain + SAN-extension parsing against an embedded/pinned Fulcio root using stdlib `crypto/x509` only, consistent with this package's stated design philosophy ("Package signing holds every stdlib crypto... primitive," `cosign.go:1`) — more code, more long-term maintenance of a security-sensitive parser, but zero new supply-chain dependency, matching the project's current near-zero-dependency posture for the `signing` package specifically (note: this posture does NOT extend project-wide — bubbletea/pgx/etc. are already dependencies).

## Config/Persistence Reuse Shape

The `TrustedPublicKeys` plumbing is a clean, repeatable four-layer pattern confirmed end-to-end: `ports.SigningPolicySettings.TrustedPublicKeys []string` → `admin_handlers.go: decodeSigningPolicySettings` (per-entry validate via `signing.NormalizePublicKeyPEM`, reject `enabled:true` with zero usable entries, cap at `maxSigningPolicyTrustedKeys`) → `sqlite/store.go` JSON column (`trusted_public_keys TEXT NOT NULL DEFAULT '[]'`) → `internal/tui/trusted_key_list.go` shared add/select/delete widget used by both the global config screen and per-repository override editor. A `TrustedIdentities []TrustedIdentity` field can follow this exact same four-layer shape with a parallel `trusted_identities` JSON column and parallel TUI list widget — no architectural rework needed, this is additive.

## Approaches

1. **New Sigstore dependency (`sigstore/sigstore-go`)** — use the upstream library for Fulcio chain verification, OIDC identity/issuer extension parsing, and (optionally) Rekor verification.
   - Pros: correctness for a security-critical, spec-heavy format (X.509v3 extension OIDs, TUF-distributed roots that rotate); actively maintained against the real Sigstore ecosystem; less regixtry-owned crypto code to audit.
   - Cons: pulls in a nontrivial dependency tree (protobuf-based Sigstore bundle types, TUF client) into a codebase that currently has zero such dependencies; may not align with the `signing` package's explicit "I/O-free, stdlib-only" design statement without careful isolation; version/API churn risk from an external library still evolving.
   - Effort: Medium (mostly wiring — the hard crypto is delegated).

2. **Stdlib-only hand-rolled Fulcio verification** — embed/pin a Fulcio root CA bundle, use `crypto/x509.Certificate.Verify` for chain validation, manually parse the OIDC-issuer/identity X.509v3 extensions (documented OIDs), verify DSSE signature against the leaf cert's public key.
   - Pros: zero new dependency, consistent with the package's current stdlib-only philosophy and its "pure, I/O-free" function style; full control/auditability of a security-sensitive parser.
   - Cons: reimplementing spec-compliant Fulcio extension parsing and root-bundle rotation handling is real, ongoing security-relevant work; higher risk of subtle correctness bugs in a hand-rolled X.509 extension parser; root bundle staleness becomes regixtry's own operational problem (no TUF auto-update).
   - Effort: High.

3. **Defer / explicit non-goal for this change** — extend `bundleDocumentEnvelope` to at least parse and surface `verificationMaterial.certificate` (additive, low-risk), but leave actual Fulcio-chain/identity verification unimplemented, documenting the gate's current key-only trust model as an explicit, known limitation until Option 1 or 2 is chosen.
   - Pros: keeps `SigningPolicySettings.Enabled` gate honest ("only trusted keys are enforceable" stays documented) with minimal surface change.
   - Cons: doesn't close the actual gap the change is named for; a proposal built on this alone wouldn't satisfy the stated goal.
   - Effort: Low (but incomplete relative to the change's purpose).

## Recommendation

Option 1 (new `sigstore-go`-style dependency) is the more defensible default for a security-critical certificate-chain/extension-parsing surface — hand-rolling Fulcio X.509v3 extension parsing (Option 2) is the kind of code where subtle bugs are exactly what an attacker would target, and this project has no existing precedent of hand-rolling comparable PKI logic anywhere else. However, this is a real fork with a meaningful tradeoff (new dependency vs. stdlib-only posture) that the proposal/design phase should decide explicitly rather than this exploration deciding it. The Rekor online-vs-offline question should likewise be resolved in `design.md`, not here — recommend defaulting to offline SET verification (matches existing I/O-free package philosophy and pull-time latency bounds) unless the proposal phase surfaces a specific threat model requiring live Rekor confirmation.

## Risks

- Fulcio/OIDC verification is a new, security-critical crypto surface in a package whose current entire test suite has zero coverage of it — the implementation must budget for genuinely adversarial test fixtures (a real or realistically-shaped synthetic Fulcio cert), not just happy-path plumbing tests.
- `bundleDocumentEnvelope`'s current parser is a hard gate against unknown bundle content (it silently discards `verificationMaterial`) — any parser extension must preserve the existing "malformed JSON is a hard error, missing/unknown fields are tolerated" posture exactly, or it risks regressing the confirmed-working static-key bundle path.
- Choosing Option 1 changes the project's dependency-supply-chain surface; choosing Option 2 changes its security-code-ownership surface. Both are consequential enough that the proposal phase should present this fork to the user rather than silently pick one.
- If Rekor online verification is chosen, it introduces a new hard network dependency into a pull-time gate that has so far been deliberately fully local/offline — this could turn transient Rekor unavailability into a full-registry pull outage under a fail-closed gate.

## Open Questions for Proposal/Design

1. New Sigstore dependency (`sigstore-go`) vs. hand-rolled stdlib-only Fulcio/X.509v3 verification?
2. Rekor transparency-log inclusion: required online check, offline embedded SET verification, or not required at all for v1?
3. How is the Fulcio root/intermediate CA trust bundle obtained and kept current (embedded static copy, TUF client, operator-supplied)?
4. Does `TrustedIdentities` apply per-repository (mirroring `SigningOverride`) from day one, or only at the global `SigningPolicySettings` level initially?

## Ready for Proposal

Yes. Ground truth confirmed against current code for the verification gate's key-only trust model, the bundle parser's deliberate omission of `verificationMaterial`, the zero-Sigstore-dependency state of `go.mod`, the reusable four-layer config/persistence pattern, and the complete absence of keyless test fixtures.

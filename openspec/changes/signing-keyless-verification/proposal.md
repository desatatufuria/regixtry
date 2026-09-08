# Proposal: Keyless (Fulcio/OIDC) Signature Verification

## Intent

The pull-time signing gate accepts one trust-anchor shape: static ECDSA P-256 PEM keys. Keyless `cosign` signatures (the CI default — Fulcio short-lived certs + OIDC identity) can never verify, so operators must keep long-lived key material or leave the gate off. This adds identity anchors.

## Scope

### In Scope

- `ports.TrustedIdentity{CertificateIdentityRegexp, CertificateOIDCIssuer string}` and `TrustedIdentities []TrustedIdentity` on **both** `SigningPolicySettings` and `SigningOverride`, full-row-replace, exactly like `TrustedPublicKeys`.
- Anchor semantics are a flat OR: a signature verifies against any one trusted key **or** any one trusted identity. No cross-anchor combination logic.
- The same four layers: ports → admin decode/validate (regex compiled at write time, entry cap, `enabled` requires ≥1 usable anchor of either kind) → `trusted_identities` JSON column via the existing `ALTER TABLE` pattern → TUI list widget in the global signing modal and the override editor.
- `ParseBundleDocument` extended to surface `verificationMaterial` (certificate / `x509CertificateChain`, `tlogEntries`), preserving "malformed is a hard error, unknown fields tolerated".
- Offline verification: pinned Sigstore trusted root → Fulcio chain → identity regexp + issuer match → embedded Signed Entry Timestamp. No pull-time network.
- Identity branch inside `verifyBundleSignature`'s bundle loop, tried after the key loop fails. A verify via the identity path surfaces the matched identity (SAN + issuer) as its own operator-facing value, not squeezed into the key-fingerprint field.

### Out of Scope

- The legacy SimpleSigning `.sig` path — untouched. Identities apply to Sigstore bundles only.
- TUF client / auto-updating roots: `sigstore-go` verifies against any supplied trusted material and its TUF package is opt-in, so a pinned root suffices. Rotation is a documented release-time task; no operator-supplied root override (design confirms against the pinned version).
- **TSA-backed bundles** — a bundle carrying a certificate but no Rekor `tlogEntries`, timestamped by a timestamp authority instead. Named non-goal, not a silent gap: these stay unverifiable and fail closed, exactly as today.
- Live Rekor queries, attestation-predicate policy, push-path changes.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

> Baselines are in `openspec/changes/image-signing/specs/` (implemented, not yet merged into `openspec/specs/`).

- `image-signature-verification`: anchors become key-or-identity; keyless bundle rules added.
- `operator-admin-tui`: both signing modals gain a trusted-identity list.

## Approach

Isolate `sigstore-go` behind one new file, `internal/domain/signing/keyless.go`, its only importer. It takes regixtry-owned inputs (cert bytes, tlog entries, anchors) and returns regixtry-owned results (matched identity, typed error); no Sigstore type reaches `internal/ports` or `internal/app`. Static keys keep their stdlib path.

## Settled Decisions

All open forks are resolved; none carry into spec or design.

| Decision | Resolution | Rationale |
|----------|------------|-----------|
| Fulcio chain verification | New `sigstore-go` dependency | Hand-rolled X.509v3 extension parsing is security-critical with no precedent in this repo |
| Rekor | Offline SET already in `tlogEntries` | Keeps the gate local and fail-closed; no Rekor outage becomes a pull outage |
| Identity scope | Global **and** per-repository from day one | Mirrors `TrustedPublicKeys` exactly |
| Anchor combination | Flat OR across both lists | Matches existing "any one entry in a list suffices"; no new logic |
| Override semantics | Full-row-replace | Identical to `TrustedPublicKeys`. An override with keys and no identities clears global identities for that repository — the existing accepted sharp edge, documented identically for the new field |
| Root rotation | Documented release-time task | Sufficient for v1; no operator root override |
| TSA-backed bundles | Out of scope, fail closed | Named non-goal above |
| Verified-state reporting | Matched identity string, own value | Fingerprint shape is key-specific and would misrepresent an identity match |

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/domain/signing/keyless.go` | New | Wrapper + pinned root |
| `internal/domain/signing/bundle.go` | Modified | Parse `verificationMaterial` |
| `internal/app/regixtry/service_signing.go` | Modified | Identity branch |
| `internal/app/regixtry/repository_overrides.go` | Modified | Normalize/apply identities |
| `internal/ports/regixtry.go` | Modified | Field + wire projection |
| `internal/infra/metadata/sqlite/store.go` | Modified | New JSON column |
| `internal/protocol/http/admin_handlers.go` | Modified | Decode/validate/serialize |
| `internal/tui/` | Modified | Identity list widget |
| `go.mod` | Modified | Add `sigstore-go` |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Parser change regresses static-key path | Med | Additive fields; existing bundle tests pass unmodified |
| New dependency tree / API churn | Med | Single wrapper file, pinned version |
| Pinned Fulcio root goes stale | Med | Rotation documented as a release task; expiry surfaced in errors |
| Realistic keyless fixtures are hard | High | Budget synthetic Fulcio-shaped chains and adversarial cases |

## Rollback Plan

Revert the branch. The column is additive with a `'[]'` default, so an older binary ignores it and falls back to key-only trust. No migration, no manifest rewrite.

## Dependencies

- `github.com/sigstore/sigstore-go` (new, pinned) and its transitive tree.
- Pinned Sigstore `trusted_root.json` embedded in the binary.

## Success Criteria

- [ ] A real keyless `cosign`-signed image pulls when identity + issuer match, and is blocked when either does not.
- [ ] Static-key behavior unchanged; existing signing tests pass unmodified.
- [ ] Identities configurable globally and per repository via admin API and TUI.
- [ ] Verification performs zero network I/O.
- [ ] Tampered SET, wrong issuer, expired cert, and untrusted chain each fail closed distinctly.

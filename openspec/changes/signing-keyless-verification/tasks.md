# Tasks: Keyless (Fulcio/OIDC) Signature Verification (signing-keyless-verification)

## Mandatory Ordering Constraint (design.md isolation boundary)

`internal/domain/signing/keyless.go` is the sole `sigstore-go` importer and MUST
be buildable and unit-testable in near-isolation (own package, synthetic
fixtures, zero `ports`/`app`/`store`/`http`/`tui` import) before anything wires
into it. Phase 0 (real-artifact shape spike) is a hard prerequisite for
Phase 2 (`ParseBundleDocument`'s current `dsseEnvelope`-only assumption).
Phase 1 (dependency + pinned root) is a hard compile prerequisite for Phase 3
(`keyless.go`). Phase 4 (`ports.TrustedIdentity`) is a hard prerequisite for
Phase 5 (store column) and Phase 6 (app wiring), each layer's tests running
against the REAL previous layer, never a mock: Phase 5's store tests use the
real `ports.TrustedIdentity` type; Phase 6's app tests call the real
`keyless.VerifyKeyless` (Phase 3) against real stored settings (Phase 5).
Phase 7 (HTTP) and Phase 8 (TUI) both depend on Phase 6's `signatureMatch`
shape and normalize rules. Phase 9 (E2E) depends on everything and on
Phase 0's confirmed bundle shape.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1400–2000 (prod ~600–850, tests ~700–1050, docs ~50–100; excludes machine-generated `go.sum` and the pinned `trusted_root.json` asset, which inflate the raw diff further but are not hand-authored review risk) |
| 800-line budget risk | High (session-cached review budget is **800**, not the skill default 400) |
| Chained PRs recommended | Yes |
| Suggested split | 4 units, mapped to the design's isolation boundary: domain-isolated, ports+store, app-wiring, http+tui+e2e |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

**Rationale**: this is a new security-critical dependency (`sigstore-go`)
crossing five layers (domain, ports, sqlite store, HTTP admin, TUI) plus a
new adversarial fixture class (synthetic Fulcio-shaped chains via
`sigstore-go/pkg/testing/ca`, flagged High effort in the proposal's own Risks
table). `keyless.go` alone — pinned root parsing, verifier construction,
identity-anchor OR semantics, four distinct fail-closed paths (untrusted
chain, wrong issuer, expired cert, tampered/missing SET), plus its adversarial
test suite — is realistically 400–600 lines on its own before any wiring
exists. `verifySignature`/`verifyBundleSignature` also change return shape
(`signatureMatch{KeyFingerprint, Identity}` replacing a bare fingerprint
string) across all 10 codegraph-confirmed callers in
`internal/app/regixtry/service_signing.go` and `queries.go`, plus every layer
already carrying a `TrustedPublicKeys`-shaped sibling
(`normalizeSigningOverride`, `decodeSigningPolicySettings`,
`trusted_key_list.go`) needs an identical identity counterpart. Very likely to
exceed 800 lines as a single PR.

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Dependency + pinned root + domain-isolated bundle/keyless parsing (Phases 1–3) | PR 1 | `go test ./internal/domain/signing/... -run 'ParseBundleVerificationMaterial\|VerifyKeyless' -v` | N/A — pure package-level unit tests with synthetic fixtures; nothing calls `keyless.go` yet | Revert `keyless.go`, `assets/trusted_root.json`, `bundle.go`'s `ParseBundleVerificationMaterial`, and the `go.mod`/`go.sum` `sigstore-go` entry; zero other package imports it |
| 2 | `ports.TrustedIdentity` + sqlite `trusted_identities` column (Phases 4–5) | PR 2 | `go test ./internal/infra/metadata/sqlite/... -run 'SigningPolicySettings\|TrustedIdentity' -v` | N/A — store round-trip only, no HTTP/TUI surface yet | Revert `ports.TrustedIdentity`/field additions and `store.go`'s `ALTER TABLE`/scan/upsert; column is additive `'[]'` default, inert to reverted binaries |
| 3 | App wiring: identity branch, keys-OR-identities precondition, override normalize (Phase 6) | PR 3 | `go test ./internal/app/regixtry/... -run 'VerifyBundleSignature\|VerifySignature\|NormalizeSigningOverride\|ApplySigningOverridePayload\|SignatureStatus' -v` | N/A — service layer only; no operator-facing way to write identities exists until PR 4 | Revert `service_signing.go`'s `signatureMatch`/identity branch, `repository_overrides.go`'s identity normalize/apply, `queries.go`'s `VerifiedIdentity`/count; PR 2's column stays but unwritten |
| 4 | Admin HTTP decode/serialize, TUI identity list, E2E, regression (Phases 7–9) | PR 4 | `go test ./internal/protocol/http/... ./internal/tui/... -run 'SigningPolicySettings\|Override\|TrustedIdentity' -v` | `docs/verification/scripts/docker-push-pull-smoke.sh` extended: real keyless `cosign`-signed image — matching identity+issuer pulls, wrong issuer blocks | Revert `admin_handlers.go` decode/serialize additions, `tui/trusted_identity_list.go` + modal wiring, smoke-script additions; PRs 1–3 stay inert with no way to configure an identity |

## Phase 0: Real-Artifact Shape Spike (blocks Phase 2+, no PR — pre-implementation)

- [ ] 0.1 **Requires Bash + network (sdd-apply only).** Pull a real keyless
      `cosign sign` artifact — e.g. one already pushed by the
      `govault-csi-provider` release pipeline referenced in this change's
      origin context — and inspect its Sigstore Bundle referrer manifest's
      raw JSON via `crane`/`oras`/`curl` + registry API. Confirm whether it is
      `dsseEnvelope`-shaped (today's only shape `ParseBundleDocument`
      accepts) or `messageSignature`-shaped. **If `messageSignature`-shaped:
      STOP. Do not write Phase 2's parser extension. Flag this back to the
      user/design** — the "Open Questions" item in `design.md` is unresolved
      and Phase 2's approach (extending the `dsseEnvelope` path) is invalid
      for that shape; a new design decision is required before continuing.
      If `dsseEnvelope`-shaped, record the confirmed shape and proceed.

## Phase 1: Dependency + Pinned Root Asset (PR 1)

- [ ] 1.1 `go.mod`/`go.sum`: add `github.com/sigstore/sigstore-go v0.7.1`
      (pinned, confirmed version) and let its transitive tree resolve.
- [ ] 1.2 Create `internal/domain/signing/assets/trusted_root.json`: the
      pinned Sigstore public-good trusted root, sourced for the confirmed
      `sigstore-go@v0.7.1` root schema version.

## Phase 2: Domain — Bundle Verification-Material Parser (PR 1)

- [ ] 2.1 RED `internal/domain/signing/bundle_test.go`: table-driven cases for
      a new `ParseBundleVerificationMaterial(raw []byte) (BundleVerificationMaterial, error)` —
      certificate / `x509CertificateChain` present, `tlogEntries` present,
      malformed JSON is a hard error, unknown fields tolerated. Existing
      `ParseBundleDocument`/`BundleDSSE` struct-comparison tests MUST remain
      unmodified (design's zero-risk guarantee).
- [ ] 2.2 GREEN `internal/domain/signing/bundle.go`: implement
      `ParseBundleVerificationMaterial` and its envelope structs, additive
      only; `ParseBundleDocument` untouched.

## Phase 3: Domain — Keyless Verifier (PR 1)

- [ ] 3.1 RED `internal/domain/signing/keyless_test.go`: matching identity
      SAN + issuer verifies offline against a synthetic Fulcio-shaped chain
      built via `sigstore-go/pkg/testing/ca` and this package's own test
      trusted root (never the real pinned root in tests).
- [ ] 3.2 RED (same file): untrusted chain (cert not rooted in the trusted
      root) fails closed with a distinct typed error.
- [ ] 3.3 RED (same file): matching SAN but wrong OIDC issuer fails closed,
      distinctly from the chain-failure error (spec: "Wrong issuer fails
      closed").
- [ ] 3.4 RED (same file): certificate expired relative to the SET timestamp
      fails closed.
- [ ] 3.5 RED (same file): tampered and missing Signed Entry Timestamp each
      fail closed, distinctly from each other's error and from chain/issuer
      failures.
- [ ] 3.6 RED (same file): structural zero-network-I/O assertion — confirm no
      call path in `keyless.go` or its `pkg/verify` dependency reaches
      `net/http` (matches design's "no `net/http` import" verification).
- [ ] 3.7 GREEN `internal/domain/signing/keyless.go`: `VerifyKeyless(bundleRaw []byte, identities []TrustedIdentity, digest string) (matched string, err error)`;
      `//go:embed assets/trusted_root.json`; `sync.OnceValues` root+verifier
      construction; `verify.NewShortCertificateIdentity` per identity (OR
      semantics via repeated `verify.WithCertificateIdentity`); typed,
      distinct errors for each Phase 3.1–3.5 case. Only file importing
      `sigstore-go`.
- [ ] 3.8 REFACTOR: confirm `internal/domain/signing` still compiles and
      tests standalone with no `internal/ports`/`internal/app`/`store`/`http`
      import (isolation boundary check).
- [ ] 3.9 Confirm Phase 1–3 GREEN (Unit 1 focused test command); `go build ./...` clean.

## Phase 4: Ports — TrustedIdentity Type (PR 2)

- [ ] 4.1 GREEN `internal/ports/regixtry.go`: add
      `TrustedIdentity{CertificateIdentityRegexp, CertificateOIDCIssuer string}`
      and `TrustedIdentities []TrustedIdentity` fields on
      `SigningPolicySettings` and `SigningOverride`. Pure type addition, no
      RED here — exercised as a real (not mocked) type by Phase 5.1's RED test.

## Phase 5: Store — trusted_identities Column (PR 2)

- [ ] 5.1 RED `internal/infra/metadata/sqlite/store_test.go`:
      `GetSigningPolicySettings`/`UpsertSigningPolicySettings` round-trip
      `TrustedIdentities` using the real `ports.TrustedIdentity` type; an
      unset policy defaults to an empty slice, never `nil` vs `[]` drift.
- [ ] 5.2 GREEN `internal/infra/metadata/sqlite/store.go`:
      `ALTER TABLE signing_policy_settings ADD COLUMN trusted_identities TEXT NOT NULL DEFAULT '[]'`
      (+ matching `CREATE TABLE` column); extend `GetSigningPolicySettings`
      scan and the upsert statement's column list.
- [ ] 5.3 RED (same file): a per-repository `SigningOverride` with
      `TrustedIdentities` set round-trips through the existing generic
      override-row JSON blob unmodified (characterization — the override
      codec is already generic; this proves no store.go override-path change
      is needed).
- [ ] 5.4 Confirm Phase 4–5 GREEN (Unit 2 focused test command).

## Phase 6: App — Identity Branch, Precondition, Override Normalize (PR 3)

- [ ] 6.1 RED `internal/app/regixtry/service_signing_bundle_test.go`:
      characterization pinning today's `verifySignature`/
      `verifyBundleSignature` static-key-only accept/reject behavior
      byte-for-byte (spec: "Legacy Static-Key Verification Path Is
      Unchanged") — run and confirm GREEN unmodified before any refactor.
- [ ] 6.2 RED (same file): identity-only policy (zero trusted keys, ≥1
      trusted identity) verifies via a real (not mocked)
      `keyless.VerifyKeyless` call against a bundle fixture carrying
      `verificationMaterial` (spec: "Identity-only policy verifies with no
      trusted key").
- [ ] 6.3 RED (same file): enabling the policy requires ≥1 usable anchor of
      EITHER kind — zero keys AND zero identities still rejects.
- [ ] 6.4 RED (same file): a verified identity match reports SAN+issuer as
      its own distinct value, never overloading the key-fingerprint field
      (spec: "Identity match reports SAN and issuer separately").
- [ ] 6.5 GREEN `internal/app/regixtry/service_signing.go`: replace the bare
      fingerprint return with `signatureMatch{KeyFingerprint, Identity string}`;
      add the identity branch inside `verifyBundleSignature`'s loop, tried
      only after the key loop fails; extend `verifySignature`'s key
      precondition (line ~129) to keys-OR-identities.
- [ ] 6.6 REFACTOR: propagate `signatureMatch` through all 10
      codegraph-confirmed callers in `service_signing.go` and `queries.go`;
      re-run Phase 6.1's characterization suite and confirm it is still
      GREEN, unmodified.
- [ ] 6.7 RED `internal/app/regixtry/repository_overrides_test.go`:
      `normalizeSigningOverride` compiles each identity regexp at write
      time, rejects a malformed regexp, caps entries at
      `maxSigningPolicyTrustedKeys` (16, same constant reused for
      identities), rejects `enabled:true` with zero usable anchors of either
      kind; `applySigningOverridePayload` full-row-replaces identities,
      confirming an override saved with keys-only clears inherited global
      identities (spec: "Override with keys only clears inherited identities").
- [ ] 6.8 GREEN `internal/app/regixtry/repository_overrides.go`: implement
      identity normalize/apply mirroring `normalizeSigningOverride`'s
      existing key logic.
- [ ] 6.9 RED `internal/app/regixtry/queries_test.go`:
      `SignatureStatusDetail.VerifiedIdentity` is populated only on the
      identity-verification path; `SignatureStatusPolicy.TrustedIdentities`
      reports the configured count.
- [ ] 6.10 GREEN `internal/app/regixtry/queries.go`: add both fields.
- [ ] 6.11 Confirm Phase 6 GREEN (Unit 3 focused test command).

## Phase 7: HTTP — Admin Decode/Validate/Serialize (PR 4)

- [ ] 7.1 RED `internal/protocol/http/admin_handlers_test.go`:
      `decodeSigningPolicySettings` accepts `trusted_identities`, rejects
      more than 16 entries, rejects `enabled:true` with zero usable anchors
      of either kind, rejects an invalid regexp with a per-index error
      (mirrors `decodeSigningPolicySettings`'s existing key validation
      shape).
- [ ] 7.2 GREEN `internal/protocol/http/admin_handlers.go`: extend
      `decodeSigningPolicySettings` and `signingPolicySettingsResponse` with
      `trusted_identities`; extend the equivalent override decode/response
      path.
- [ ] 7.3 RED (same file, `httptest`): saving an override with keys but no
      identities over HTTP clears that repository's inherited identities
      (full-row-replace, HTTP-level confirmation of Phase 6.7).
- [ ] 7.4 Confirm Phase 7 GREEN.

## Phase 8: TUI — Trusted Identity List Widget (PR 4)

- [ ] 8.1 RED `internal/tui/trusted_identity_list_test.go`:
      `newTrustedIdentityList` add/select/delete behavior, mirroring
      `trusted_key_list_test.go`'s existing coverage for `trustedKeyList`.
- [ ] 8.2 GREEN `internal/tui/trusted_identity_list.go`: widget structurally
      mirroring `trusted_key_list.go` (SAN regexp + issuer pair entry, not a
      single string).
- [ ] 8.3 RED `internal/tui/screen_signing_config_test.go`: the global
      signing modal shows and edits `trusted_identities` alongside
      `trusted_public_keys`, round-tripping through the admin API (spec:
      "Modal shows current global signing policy").
- [ ] 8.4 GREEN `internal/tui/screen_signing_config.go`: wire
      `trustedIdentityList` into the modal alongside the existing `Keys`
      field.
- [ ] 8.5 RED `internal/tui/override_editor_test.go`: with `signing` selected
      in the Feature cycle, the modal's fields present trusted identities
      alongside trusted keys, not Trivy/gitleaks fields (spec: "Modal fields
      adapt to signing's settings shape including identities").
- [ ] 8.6 GREEN `internal/tui/override_editor.go`: wire the identity list
      into the `signing` feature branch (mirrors `e.keys`'s existing wiring
      at lines ~65, ~199, ~308–352).
- [ ] 8.7 Confirm Phase 8 GREEN (Unit 4 focused test command, partial).

## Phase 9: Integration / E2E / Regression (PR 4)

- [ ] 9.1 E2E `docs/verification/scripts/docker-push-pull-smoke.sh`: push a
      real keyless `cosign`-signed image (shape confirmed by Phase 0);
      matching identity+issuer policy pulls it; wrong-issuer policy blocks
      it, distinctly.
- [ ] 9.2 Confirm full `go test ./...` zero regressions; `gofmt -l .` clean;
      `go vet ./...` clean.
- [ ] 9.3 Docs: document trusted-identity configuration (global + override)
      and the pinned-root rotation-is-a-release-task note in the relevant
      operator docs.

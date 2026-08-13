# Tasks: Image Signature Verification Gate (image-signing)

## Mandatory Ordering Constraint (design.md Decision 1)

The exact byte layout of cosign's signature artifact (field ordering, the
`"Docker-manifest-digest"`/`"docker-manifest-digest"` casing ambiguity, and
whether `"optional"` is `null` or carries `creator`/`timestamp`) was
**documented, not executed** in `sdd-design` — no `cosign` binary, shell, or
network was available in that phase. Phase 0 below is therefore **task 1,
literally first**, and every parser/verifier task in Phase 1 onward
**depends on Phase 0's captured (or explicitly-marked-synthetic) fixture**.
No task in this file may hand-construct a fixture from this document's prose
as a silent substitute for a real `cosign sign` capture. If no `cosign`
binary is available when `sdd-apply` runs, Phase 0 records that fact
explicitly and every downstream fixture is marked
`synthetic, unconfirmed against real cosign output` — never silently worked
around, and restated in the verify-report (task 11.7).

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~3200–4100 (prod ~1200–1400, tests ~2000–2700) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes — likely more units than `repository-scan-config-overrides`'s 5 (~2000 lines), given the added fixture-capture gate, a brand-new I/O-free crypto package, 2 new HTTP endpoints, and a TUI surface with a 3-piece modal + badge + a deliberately-altered existing test |
| Suggested split | 7 units, see below |
| Delivery strategy | Not decided here — flagged for the orchestrator |

**Rationale**: 11 numbered design decisions touch 14 files (2 new domain
files, 1 new service file, ports/store/router/admin_handlers/feature_registry
modified, 4 TUI files modified) plus one wholly new testdata fixture set that
cannot be produced without an environment check. Unlike
`repository-scan-config-overrides`, this change also carries a **hard
regression constraint that must never regress** (`applyRepositoryOverride`'s
signature and all 6 call sites, plus the entire shipped
`repository_overrides_test.go` suite, byte-unchanged) stacked on top of a
**deliberate, flagged exception to that same constraint** (the 2→3 TUI cycle
test, which the "tests pass unchanged" guarantee explicitly does not cover).
Test surface is large: golden-fixture crypto tests, a negative
re-marshalling pin test, 6 separately-asserted fail-closed cases, a 5-state
status matrix, a threat-matrix set (HTTP path dispatch, untrusted artifact
parsing, signature transplant, fail-closed availability, key disclosure), and
a TUI 3-piece-modal-plus-badge-plus-cycle surface. Recommend chaining rather
than a single PR; suggested split below follows design.md's own File Changes
ordering (fixture capture and domain package first, storage/resolution next,
gate + feature registry + status endpoint, admin HTTP, TUI, integration
close-out last).

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 0 | Fixture capture (Phase 0) | PR 0 (or folded into PR 1's first commit) | N/A — produces testdata, no assertions of its own | `cosign` binary if present in the apply environment; otherwise a documented synthetic fallback | Delete `internal/domain/signing/testdata/` and the recorded provenance note |
| 1 | Domain signing package (Phase 1) | PR 1 | `go test ./internal/domain/signing/... -v` | N/A — pure functions | Delete `internal/domain/signing/` |
| 2 | Storage + codec generalization (Phases 2–3) | PR 2 | `go test ./internal/infra/metadata/sqlite/... -run SigningPolicySettings` and `go test ./internal/app/regixtry/... -run 'RepositoryOverride\|Normalize\|Apply'` | N/A — store + pure resolution | Revert `store.go` DDL block, `ports/regixtry.go` types/methods, `repository_overrides.go`'s generic helper and signing codec entry |
| 3 | Gate + feature registry + status endpoint (Phases 4–7) | PR 3 | `go test ./internal/app/regixtry/... -run 'SigningPolicy\|EnforceSigning\|OpenManifest\|SignatureStatus'` | Manual: seed a trusted key, sign a test payload with a throwaway ECDSA P-256 key, pull with/without a matching `.sig` tag | Revert `service_signing.go`, the `OpenManifest` insertion, `feature_registry.go` guards, `queries.go` DTOs, the router suffix case |
| 4 | Admin HTTP (Phase 8) | PR 4 | `go test ./internal/protocol/http/... -run 'SigningPolicy\|RepositoryOverride'` | Manual: `curl -X PUT /admin/v1/signing-policy` then `GET`; `curl -X PUT /admin/v1/features/signing/repository-overrides/<repo>` | Revert `admin_handlers.go`'s `signing-policy` case and decoder |
| 5 | TUI (Phase 9) | PR 5 | `go test ./internal/tui/... -run 'SigningPolicy\|RepositoryOverride\|Cycle'` | Debug print, modal render at height 24 (task 9.20) | Revert `session.go` struct, `admin_views.go` render fns + badge, `model.go` wiring + cycle change, `admin_client.go` methods |
| 6 | Cross-cutting integration + non-regression close-out (Phases 10–11) | PR 6 (or folded into whichever PR lands last) | `go test -count=1 ./...` | Manual: full round-trip with a real `cosign` invocation if available | N/A — verification only, no new production code |

## Phase 0: Cosign Fixture Capture — MUST run first, blocks Phase 1 entirely (Decision 1)

- [x] 0.1 Check whether a `cosign` binary is available in the apply
      environment (`which cosign` / equivalent). Record the result — this
      determines whether 0.2a or 0.2b runs.
      **Result: `which cosign` exits non-zero — NOT available in this
      apply environment (confirmed 2026-08-13).**
- [ ] 0.2a **N/A — no `cosign` binary was available in this apply
      environment.** Real capture was not performed. This branch stays
      unchecked until a future environment with a real `cosign` binary
      re-runs Phase 0.
- [x] 0.2b **`cosign` NOT available**: hand-constructed (via a throwaway,
      offline Go generator — not part of the shipped codebase or any test
      run) a fixture strictly matching design.md Decision 1a/1b's documented
      shape: `internal/domain/signing/testdata/payload.json` (SimpleSigning
      payload bytes), `internal/domain/signing/testdata/signature-manifest.json`
      (the `.sig` OCI manifest with one annotated simplesigning layer), and
      `internal/domain/signing/testdata/cosign.pub` (PEM ECDSA P-256 public
      key). Casing choice recorded: **lowercase**
      `"docker-manifest-digest"`, matching design.md Decision 1a's own worked
      JSON literal (the upstream `SIGNATURE_SPEC.md` example capitalizes it;
      design.md flags this as unresolved without a real capture — see
      `testdata/README.md` "Casing choice"). Explicitly marked
      `synthetic, unconfirmed against real cosign output` in
      `internal/domain/signing/testdata/README.md`, mirroring the posture
      `repository-scan-config-overrides/design.md:630-644` took for its
      unavailable Trivy binary.
- [x] 0.3 Record the outcome (cosign available: yes/no; fixture provenance:
      real capture vs. synthetic) both in this file's progress notes and
      carried forward verbatim into the verify-report at close-out (task
      11.7). This is a first-class finding, not a footnote.
      **Outcome: cosign NOT available; fixture provenance is SYNTHETIC
      (hand-constructed, offline generator), documented in
      `internal/domain/signing/testdata/README.md`. Must be restated at
      task 11.7/11.5(a).**

## Phase 1: Domain Signing Package (Decisions 1, 2) — depends on Phase 0

- [x] 1.1 RED `internal/domain/signing/cosign_test.go`: `SignatureTag` maps
      `sha256:<hex>` → `sha256-<hex>.sig`; rejects a malformed digest
      (missing `sha256:` prefix, empty hex, non-hex characters) — table-driven.
- [x] 1.2 RED: `ParseSignatureManifest` on the Phase 0 captured/synthetic
      `.sig` manifest bytes yields the expected `(PayloadDigest, Signature)`
      entries in order; ignores layers with a non-simplesigning media type
      and layers missing the signature annotation; enforces
      `MaxSignatureEntries` against a synthetic 65-layer manifest.
- [x] 1.3 GREEN: create `internal/domain/signing/cosign.go` —
      `SimpleSigningMediaType`, `SignatureAnnotationKey`,
      `SimpleSigningType`, `MaxSignatureManifestBytes`, `MaxPayloadBytes`,
      `MaxSignatureEntries`, `SignatureTag`, `SignatureEntry`,
      `ParseSignatureManifest` — exact signatures from design Decision 2.
- [x] 1.4 Confirm 1.1–1.2 GREEN: `go test ./internal/domain/signing/... -run 'SignatureTag|ParseSignatureManifest' -v`.
- [x] 1.5 RED `internal/domain/signing/keys_test.go`: `NormalizePublicKeyPEM`
      accepts a real-newline PEM block and a space-separated single-line
      form (as the TUI's field would submit), producing byte-identical
      canonical output for both; rejects Ed25519, RSA, P-384, and garbage
      input — table-driven.
- [x] 1.6 RED: `ParseTrustedKey` decodes the canonical stored PEM back into
      `*ecdsa.PublicKey`.
- [x] 1.7 RED: `Verify` accepts the Phase 0 captured/synthetic
      `(payload, signature, public key)` triple; rejects a one-byte-mutated
      payload, a mutated signature, a wrong key, and non-base64 input —
      golden fixture, table-driven.
- [x] 1.8 RED — **deliberate negative test, the Decision 1a pinning test**:
      decode the SimpleSigning payload JSON into a Go struct, re-marshal it,
      and assert the re-marshalled bytes hash differently from the original
      and that `Verify` rejects the re-marshalled version even though it
      round-trips as equivalent JSON. This is the test that proves hashing
      must happen over verbatim stored bytes, never a re-marshalled struct.
      **Note**: the initial fixture's field order was coincidentally
      identical to Go's alphabetical-map-key remarshal output, which would
      have made this test vacuous; the fixture was regenerated with
      realistic colon/comma spacing (documented in `testdata/README.md`) so
      the remarshal genuinely produces different bytes.
- [x] 1.9 RED: `CheckClaims` rejects a payload binding a different digest and
      a wrong `critical.type`; accepts a differing `docker-reference`
      (documented, intentional non-check per Decision 2) — table-driven.
- [x] 1.10 GREEN: create `internal/domain/signing/keys.go` —
      `NormalizePublicKeyPEM`, `ParseTrustedKey`, `Verify`, `CheckClaims`,
      using only `crypto/ecdsa`, `crypto/sha256`, `crypto/x509`,
      `encoding/pem`, `encoding/base64` from the standard library.
      (`CheckClaims` and its `simpleSigningPayload` claim-extraction type
      landed in `cosign.go` alongside the other cosign-format primitives,
      per design's File Changes table; `keys.go` holds
      `NormalizePublicKeyPEM`/`ParseTrustedKey`/`Verify`.)
- [x] 1.11 Confirm 1.5–1.9 GREEN: `go test ./internal/domain/signing/... -v`.
- [x] 1.12 Confirm `git diff go.mod go.sum` is empty — zero new dependency,
      the proposal's hard constraint.

## Phase 2: Ports & Storage — Global Signing Policy (Decision 4) — depends on Phase 1 (types only, no fixture dependency)

- [x] 2.1 Add `ports.SigningPolicySettings` and `ports.SigningOverride`
      structs to `internal/ports/regixtry.go`, beside `ScanPolicySettings`
      (`:91-95`), exact fields from design Decision 4.
- [x] 2.2 Add `GetSigningPolicySettings`/`UpsertSigningPolicySettings` to the
      `MetadataStore` interface, beside `Get/UpsertScanPolicySettings`
      (`:35-36`).
- [x] 2.3 RED `internal/infra/metadata/sqlite/store_test.go`:
      `GetSigningPolicySettings` on an absent row returns typed
      `domain.ErrorCodeNotFound`.
- [x] 2.4 RED: `UpsertSigningPolicySettings` then `Get` round-trips
      `Enabled`, `TrustedPublicKeys` (order preserved), `UpdatedAt`
      (`time.RFC3339Nano`).
- [x] 2.5 GREEN: append the `signing_policy_settings` `CREATE TABLE IF NOT
      EXISTS` to `Store.init()`'s statements slice, exact SQL from design
      Decision 4 — **`enabled INTEGER NOT NULL DEFAULT 0`, the deliberately
      inverted default vs. `scan_policy_settings`' `DEFAULT 1`**.
- [x] 2.6 GREEN: implement `Get/UpsertSigningPolicySettings` on `store.go` —
      `sql.ErrNoRows` → `domain.NewNotFoundError`, mirroring
      `Get/UpsertScanPolicySettings` (`store.go:500-534`).
- [x] 2.7 Confirm 2.3–2.4 GREEN: `go test ./internal/infra/metadata/sqlite/... -run SigningPolicySettings`.

## Phase 3: Codec Generalization — Generic Resolution Helper (Decision 5) — depends on Phase 2

**Hard regression constraint for this phase**: `applyRepositoryOverride`'s
existing signature and all 6 existing call sites in `service_scanning.go`
must be byte-unchanged, and the entire shipped
`repository_overrides_test.go` suite for Trivy/gitleaks must pass unmodified.
**Acceptance criterion for 3.9**: `git diff` on `service_scanning.go` shows
zero lines changed at the 6 call sites, and zero lines changed inside any
existing Trivy/gitleaks test function.

- [x] 3.1 RED `internal/app/regixtry/repository_overrides_test.go` (add new
      test functions only, do not touch existing ones):
      `resolveRepositoryOverride[T]` returns `settings` unchanged on
      `NotFound`, exercised for **both** `T=ports.ScanSettings` and
      `T=ports.SigningPolicySettings` — table-driven across both type
      instantiations.
- [x] 3.2 RED: `resolveRepositoryOverride` returns `settings` unchanged when
      `apply` is `nil` — reproduces today's `if !ok { return settings, nil }`
      branch (`repository_overrides.go:140-143`) for a codec that registers
      `Normalize` only.
- [x] 3.3 RED — regression pin: run the EXISTING `repository_overrides_test.go`
      suite as-is before any generalization lands, confirm it is green
      against the current unmodified code, and record that baseline (no new
      test code — this is a checkpoint, not an assertion to write).
      **Baseline recorded: `go test ./internal/app/regixtry/... -run
      'RepositoryOverride|Normalize|Apply' -v` → 12 top-level tests PASS, 0
      FAIL, before any Phase 3 code changes.**
- [x] 3.4 GREEN: implement the generic free function
      `resolveRepositoryOverride[T any]` in `repository_overrides.go`, exact
      code from design Decision 5 (type parameters are illegal on methods —
      this MUST be a free function taking `*Service`, not a method).
- [x] 3.5 GREEN: refactor `(s *Service) applyRepositoryOverride` to delegate
      to `resolveRepositoryOverride` internally — its signature MUST stay
      **byte-identical**:
      `func (s *Service) applyRepositoryOverride(ctx context.Context, tenant, repository, feature string, settings ports.ScanSettings) (ports.ScanSettings, error)`.
      Confirm via `git diff service_scanning.go` that all 6 call sites are
      untouched.
      **Confirmed: `git diff --stat internal/app/regixtry/service_scanning.go`
      is empty — zero lines changed in that file.**
- [x] 3.6 RED: `normalizeSigningOverride` rejects unknown fields
      (`DisallowUnknownFields`), rejects a key that fails
      `signing.NormalizePublicKeyPEM`, and rejects `enabled:true` with zero
      keys (the outage rule, Decision 7's mitigation applied at write time)
      — table-driven.
- [x] 3.7 RED: `applySigningOverridePayload` round-trips a payload into
      `ports.SigningPolicySettings` via plain `json.Unmarshal`, matching the
      strict-in/lenient-out asymmetry documented at
      `repository_overrides.go:101-106`.
- [x] 3.8 GREEN: add `signingFeatureName` constant; add
      `normalizeSigningOverride`, `applySigningOverridePayload`; add the
      third `signingFeatureName: {Normalize: normalizeSigningOverride}`
      entry to `repositoryOverrideCodecs` (no `Apply` — signing's override
      targets a different type, per Decision 5); add
      `(s *Service) applySigningRepositoryOverride`.
- [x] 3.9 Confirm 3.1–3.2, 3.6–3.7 GREEN, **and** confirm the existing
      `repository_overrides_test.go` Trivy/gitleaks tests pass byte-unmodified:
      `go test ./internal/app/regixtry/... -run 'RepositoryOverride|Normalize|Apply' -v`.
      **Confirmed: after generalization, all 12 baseline tests still PASS
      (byte-identical test bodies — `git diff` on
      `repository_overrides_test.go` and `service_scanning_test.go` shows
      zero removed/modified lines, only additions) plus 4 new Phase 3 test
      functions PASS. Total 16 top-level tests PASS, 0 FAIL.**

## Phase 4: Service Signing — Policy Resolution & Fail-Closed Enforcement (Decisions 6, 7) — depends on Phase 1, 2, 3

- [x] 4.1 RED `internal/app/regixtry/service_signing_test.go` (new):
      `GetSigningPolicySettings` on an empty store returns `{Enabled:
      false}` — the inverted default vs. `enforceScanPolicy`'s fail-open
      default.
- [x] 4.2 RED: `enforceSigningPolicy` with the resolved policy `Enabled:
      false` allows the pull without any verification attempt — the only
      allow-without-verify path — for both a signed and an unsigned digest.
- [x] 4.3 RED: `enforceSigningPolicy` with policy enabled and **no**
      `sha256-<hex>.sig` tag resolvable → `domain.ErrorCodePolicyViolation`.
- [x] 4.4 RED: policy enabled, `.sig` manifest present but malformed / no
      simplesigning layer carrying a signature annotation →
      `PolicyViolation`.
- [x] 4.5 RED: policy enabled, `.sig` present and parseable, but the payload
      blob is absent from the blob store → `PolicyViolation`.
- [x] 4.6 RED: policy enabled, zero configured keys parse as ECDSA P-256
      (store row hand-seeded to bypass the admin-time 400) →
      `PolicyViolation`.
- [x] 4.7 RED: policy enabled, every `(key, entry)` pair fails `Verify` →
      `PolicyViolation`.
- [x] 4.8 RED: policy enabled, a signature verifies cryptographically but
      `CheckClaims` binds a **different** digest → `PolicyViolation`
      (mismatched case; distinguished at the status layer in Phase 7, but
      the gate outcome here is the same 403-shaped error).
- [x] 4.9 RED: policy enabled, the Phase 0/1 captured (or synthetic,
      explicitly marked) fixture's valid signature by a trusted key → nil
      error, pull allowed — exercised through the service layer with
      store/blob doubles seeded from the fixture bytes.
- [x] 4.10 RED: a store/blob **infrastructure** error (not `NotFound`)
      during resolution propagates **unchanged**, not wrapped as
      `PolicyViolation` — service test with a failing store double, per
      Decision 6's table's last row.
- [x] 4.11 RED: `ResolveManifest` (the browse path) is never gated by
      `enforceSigningPolicy`, mirroring the existing scan-gate proof at
      `service_test.go:467-469`. (Implemented alongside Phase 5's OpenManifest
      wiring in `service_test.go` — `enforceSigningPolicy` is not reachable
      from any call site until the gate is wired, so this proof necessarily
      shares Phase 5's test.)
- [x] 4.12 GREEN: create `internal/app/regixtry/service_signing.go` —
      `signingFeatureName` (reconciled with 3.8: reused the existing
      declaration in `repository_overrides.go`, not redeclared),
      `enforceSigningPolicy`, `verifySignature`,
      `Get/UpdateSigningPolicySettings`, exact shape from design Decision 6.
- [x] 4.13 Confirm 4.1–4.11 GREEN:
      `go test ./internal/app/regixtry/... -run 'SigningPolicy|EnforceSigning|VerifySignature' -v`
      → 10/10 top-level tests PASS (12 incl. subtests), 0 FAIL.

## Phase 5: Pull-Time Gate Wiring in `OpenManifest` (Decision 6) — depends on Phase 4

- [x] 5.1 RED: `OpenManifest` with signing policy disabled is byte-identical
      to today for both signed and unsigned digests (proposal Success
      Criterion 2).
- [x] 5.2 RED: `OpenManifest` with signing policy enabled and a valid
      trusted signature → pull succeeds.
- [x] 5.3 RED: `OpenManifest` with signing policy enabled and no signature →
      403 `PolicyViolation`, proving `enforceSigningPolicy` runs after
      `enforceScanPolicy` without either masking the other.
- [x] 5.4 RED — **fail-closed vs. fail-open contrast, the spec's explicit
      scenario**: a digest with neither a completed scan nor a verifiable
      signature, both gates enabled — the vulnerability gate allows it
      (fail-open) while the signing gate blocks it (fail-closed), asserted
      in one test exercising `OpenManifest` once.
- [x] 5.5 GREEN: insert the `enforceSigningPolicy` call in `OpenManifest`
      (`queries.go:71-91`) immediately after the existing `enforceScanPolicy`
      call, exact insertion from design Decision 6.
- [x] 5.6 Confirm 5.1–5.4 GREEN: `go test ./internal/app/regixtry/... -run OpenManifest -v`
      → 6/6 top-level tests PASS (15 incl. subtests), 0 FAIL. Task 4.11's
      `TestServiceResolveManifestIgnoresSigningPolicyGate` (name does not
      match the `OpenManifest` filter) confirmed separately: PASS.

## Phase 6: Feature Registry — Builtin With No Runtime Manager (Decision 10) — depends on Phase 4

- [x] 6.1 **Before** any `feature_registry.go` change, run the full existing
      suite (`go test ./... -v`) and confirm — by running, not by grep —
      design.md's Open Questions claim that no test today asserts an exact
      `builtInFeatures` count or rejects `"signing"` as unsupported. Record
      the result explicitly; this is the flagged risk from design.md.
      **Result: baseline `go test -count=1 ./...` was green before any
      change. The risk DID materialize:
      `TestServiceListFeaturesReturnsBuiltinTrivyInventory`
      (`service_test.go`) asserted `ListFeatures()` via `reflect.DeepEqual`
      against a hardcoded 2-element `[]ports.FeatureSummary`, which is an
      exact-count assertion in effect even though it never spells out the
      literal number 2. No test rejected `"signing"` as unsupported. Fixed
      by updating that test's `want` slice to include the third `signing`
      entry — the feature is NOT hidden from `ListFeatures` to work around
      it.**
- [x] 6.2 RED `feature_registry_test.go`: with a `signing` `featureDescriptor
      {managedRuntime: false}` entry, `projectFeatureRuntime("signing")`
      returns an empty `ports.FeatureRuntime{}` (`Mode == ""`), not a
      fabricated `{Mode: Managed, Status: uninstalled}`.
- [x] 6.3 RED: `buildFeatureActions` for `signing` exposes exactly
      Enable/Disable/Configure and omits Install/Upgrade/Rollback — proves
      the existing `Mode == Managed` gates already do the right thing once
      6.2's empty runtime lands (no new gating code needed here, per
      design's "None needed" row).
- [x] 6.4 RED: `ExecuteFeatureAction("signing", "install-runtime")` (and
      `upgrade-runtime`, `rollback-runtime`) returns
      `domain.NewValidationError`, not the untyped 500
      `featureRuntimeManager` would otherwise produce — defense in depth.
- [x] 6.5 RED: `buildFeaturePage` for `signing` gates the Runtime section on
      `Mode == FeatureRuntimeModeManaged` (renders no Runtime section) and
      shows a signing-specific Policy fields section, not the Trivy/gitleaks
      Schedule/Interval/Timeout/Concurrency Config fields.
- [x] 6.6 RED: `loadFeatureSettings` for `signing` projects the
      `signing_policy_settings` row into a `ScanSettings{Enabled:
      policy.Enabled}` shell, with `configured` = "a `signing_policy_settings`
      row exists" — never reads or writes a `scan_settings(feature="signing")`
      row.
- [x] 6.7 RED: `ConfigureFeature("signing", ...)` is rejected outright with
      `domain.NewValidationError("signing is configured through the signing
      policy endpoint")` — proves nothing can create a stray `scan_settings`
      row behind the projection.
- [x] 6.8 RED — **one-bit invariant**: enabling via
      `ExecuteFeatureAction("signing", "enable")` and enabling via
      `UpdateSigningPolicySettings` move the same underlying bit — both
      paths converge to identical `GetSigningPolicySettings().Enabled` and
      identical `loadFeatureSettings`-projected state.
      **Deviation noted below (6.9): the admin `:enable`/`:disable` HTTP
      shortcut calls `SetFeatureEnabled` directly, not
      `ExecuteFeatureAction` — `SetFeatureEnabled` itself was special-cased
      for signing so both surfaces converge on one implementation
      (`setSigningFeatureEnabled`), not just `ExecuteFeatureAction`.**
- [x] 6.9 GREEN: add `featureDescriptor.managedRuntime`; convert
      `builtInFeatures` to the data-driven slice (trivy/gitleaks
      `managedRuntime: true`, signing `managedRuntime: false`); add the
      early-return guard in `projectFeatureRuntime`; add the three-action
      rejection in `ExecuteFeatureAction`; gate the Runtime section in
      `buildFeaturePage`; wire `loadFeatureSettings`'s signing projection;
      wire `ConfigureFeature`'s signing rejection — exact guards from design
      Decision 10's table, nothing larger.
      **Deviation from the literal task list, in scope**: `SetFeatureEnabled`
      (not just `ExecuteFeatureAction`'s "enable"/"disable" cases) was
      special-cased for signing, because `admin_handlers.go`'s existing
      `:enable`/`:disable` HTTP shortcut calls `SetFeatureEnabled` directly,
      bypassing `ExecuteFeatureAction` entirely; without this,
      `ConfigureFeature`'s new outright rejection (6.7) would have silently
      broken that shortcut for `signing`. Also added an outage-rule guard in
      the new `setSigningFeatureEnabled` (reject enabling with zero
      configured trusted keys) — the same invariant `normalizeSigningOverride`
      and the Phase 8 admin decoder enforce at write time, applied
      consistently at this third entry point too.
- [x] 6.10 Confirm 6.2–6.8 GREEN **and** the full suite
      (`go test -count=1 ./...`) is still green — explicit check for the
      third-`builtInFeatures`-entry risk design.md flagged, resolved by
      running the suite, not by inspection.
      **Confirmed: `go test ./internal/app/regixtry/... -run
      'Feature|ListFeatures|Signing' -v` all PASS, and full-repo
      `go test -count=1 ./...` green across all 18 packages.**

## Phase 7: Registry-Scoped Signature-Status Endpoint (Decision 9) — depends on Phase 4

- [x] 7.1 RED `signature_status_test.go`: `SignatureStatus` computes each of
      the five states (`unsigned`, `unverifiable`, `untrusted`, `mismatched`,
      `verified`) for corresponding fixture/double setups, **independent of
      whether `policy.Enabled` is true or false** — the state is always
      computed.
- [x] 7.2 RED: `WouldBlockPull == policy.Enabled && State != verified`,
      table-driven across the 5 states × 2 policy-enabled values.
      **Implemented in the same table-driven test as 7.1** (both properties
      asserted per case, 5 states × 2 policy-enabled values = 10 subtests).
- [x] 7.3 RED: `SignatureStatusPolicy.TrustedKeys` is a count only — the
      serialized JSON response never contains PEM bytes.
- [x] 7.4 GREEN: add `SignatureStatusResult`, `SignatureStatusPolicy`,
      `SignatureStatusDetail` types and the 5 state constants to
      `queries.go` beside `ScanStatusResult`; implement
      `(s *Service) SignatureStatus`, exact shape from design Decision 9.
- [x] 7.5 RED HTTP handler test: `GET
      /v2/<repo>/manifests/<ref>/signature-status` is reachable with
      ordinary `ActionPull`-only credentials, no admin auth; a tag literally
      named `signature-status` still routes to `handleManifest`, not the
      status handler — suffix matched against the **trimmed reference**,
      mirroring the existing `/scan-status` ordering hazard at
      `router.go:155-160`.
- [x] 7.6 RED: `signature-status` is **never** blocked by the gate — even
      when the resolved policy would 403 an actual pull of the same digest,
      the status endpoint still returns 200 with the blocking verdict.
- [x] 7.7 RED — **threat matrix, key material disclosure**: the
      `signature-status` response body, and the pull gate's 403
      `PolicyViolation` bodies from Phase 5, never contain `BEGIN PUBLIC
      KEY` or a raw base64 signature — asserted across both endpoints.
- [x] 7.8 GREEN: add the `/signature-status` suffix case in `handleV2`'s
      `manifests/` branch (`router.go`, beside the existing `/scan-status`
      case) and `handleManifestSignatureStatus`, copied from
      `handleManifestScanStatus`'s shape (GET only, `ActionPull` via
      `withPrincipal`, always 200).
- [x] 7.9 Confirm 7.1–7.7 GREEN:
      `go test ./internal/app/regixtry/... -run SignatureStatus -v` and
      `go test ./internal/protocol/http/... -run SignatureStatus -v`.
      **Confirmed: all app-layer SignatureStatus tests PASS (5 states × 2
      policy-enabled + count-only + non-gating + no-key-leakage +
      pull-authorization), and all HTTP-layer SignatureStatus tests PASS
      (verified-state shape, route collision, pull-only auth, non-gating,
      no-key-leakage across both endpoints). Full-repo
      `go test -count=1 ./...` green across all 18 packages, `go vet ./...`
      clean, `gofmt -l .` empty.**

## Phase 8: Admin HTTP Resource — Global Signing Policy (Decision 8) — depends on Phase 2, 3, 4

- [x] 8.1 RED `admin_handlers_test.go`: `GET /admin/v1/signing-policy`
      returns the current settings (200), including the code-level
      `{Enabled: false}` default when no row exists — never 404, mirroring
      `handleAdminScanPolicy`.
- [x] 8.2 RED: `PUT /admin/v1/signing-policy` fully replaces the settings
      and round-trips.
- [x] 8.3 RED: `PUT` with a key that fails `signing.NormalizePublicKeyPEM`
      (unparseable or non-ECDSA-P256) returns 400 naming the offending
      **index**, never echoing key bytes.
      **Note**: the codebase's established convention (`writeAdminError`,
      confirmed by `TestAdminScanPolicyPutRejectsUnknownSeverityThreshold`
      and `normalizeSigningOverride`'s own HTTP tests) maps every
      `domainauth`/`domainregistry` validation error to **422**, not a
      literal 400. The RED/GREEN tests assert 422, matching every other
      admin validation rejection in this codebase, not a new deviation.
- [x] 8.4 RED: `PUT` with `enabled: true` and zero usable keys returns 400
      (the outage rule) — the row is **not** stored, confirmed via a
      subsequent `GET` showing the prior/default state unchanged.
      (422 per the note above.)
- [x] 8.5 RED: `PUT` with more than 16 keys returns 400 (hot-path bound).
      (422 per the note above.)
- [x] 8.6 RED: the admin endpoints require `requireAdminPrincipal` — an
      ordinary pull-scoped credential is rejected, no new permission
      surface.
- [x] 8.7 GREEN: add the `signing-policy` case to `handleAdmin`'s dispatch
      (`admin_handlers.go`, beside `scan-policy`), `handleAdminSigningPolicy`
      modeled line-for-line on `handleAdminScanPolicy` (GET/PUT),
      `decodeSigningPolicySettings` enforcing the three rules from Decision
      8 (per-key normalize, outage rule, 16-key cap).
- [x] 8.8 Confirm 8.1–8.6 GREEN: `go test ./internal/protocol/http/... -run SigningPolicy -v`.
- [x] 8.9 RED: `PUT
      /admin/v1/features/signing/repository-overrides/<repo>` persists and
      round-trips through the **existing** generic repository-overrides HTTP
      resource — Phase 3's codec registration is what makes this work with
      **zero new HTTP route**, proving Decision 5's "one map entry" claim on
      the wire.
- [x] 8.10 RED: a signing override body with unknown fields, or
      `enabled: true` with zero keys, is rejected 400 via
      `normalizeSigningOverride` through the existing generic PUT handler —
      no new code path.
      (422 per the note above; confirmed no production code change was
      needed — the codec was already registered in Work Unit 2/3.)
- [x] 8.11 Confirm 8.9–8.10 GREEN:
      `go test ./internal/protocol/http/... -run RepositoryOverride -v` —
      explicit proof the existing repository-overrides resource needed
      **zero** handler changes for the third feature.

## Phase 9: TUI — Signing Policy Modal, Badge, Override Cycle (Decision 11) — depends on Phase 8

### Modal (3-piece pattern, mirroring `scanPolicyModal`)

- [x] 9.1 RED `session_test.go`: `signingPolicyModal.Active()` open/closed;
      `nextSigningPolicyField` cycles
      `Enabled -> AddKey -> ClearKeys -> Enabled`.
- [x] 9.2 GREEN: add `signingPolicyField`, `signingPolicyModal` struct,
      `Active()`, `nextSigningPolicyField` to `session.go` beside
      `scanPolicyModal`; add `AdminViewState.SigningPolicy`/
      `SigningPolicyModal` fields.
      **Note**: `SigningPolicyModal` is deliberately NOT zeroed in
      `clearSelectedAdminDetails`/`applyFeaturePage` — `ScanPolicyModal`
      (the mirrored precedent) is not zeroed there either; only
      `TrivyConfigModal`/`GitleaksConfigModal`/`ScanHistoryModal`/
      `RepositoryOverrideModal` are. Following the actual codebase pattern
      here rather than this task's literal text.
- [x] 9.3 RED `admin_views_test.go`: `renderSigningPolicyModal` row budgets
      per design Decision 11's table — 14 rows (0 keys, no error), 16 rows
      (3 keys, no error), 20 rows worst case (error, ≥4 keys, capped list +
      `"+N more"`).
- [x] 9.4 RED: `renderSigningPolicyModal` never renders a stored key as raw
      PEM — only truncated SHA-256/12 fingerprints.
- [x] 9.5 GREEN: add `renderSigningPolicyModal` to `admin_views.go`, plus one
      more `compositeOverlay` branch in `renderAdminWorkspace`.

### Badge (zero row cost)

- [x] 9.6 RED `admin_views_test.go`: `signingPolicyBadge` is text-only (no
      icon/glyph), rendering `Signing: OFF` (disabled) and
      `Signing: REQUIRED (N keys)` (enabled) — mirrors
      `TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph`
      in shape.
- [x] 9.7 RED: the badge is composed onto the **existing** "Feature Page"
      heading line only when the selected feature is `signing`, and adds
      **zero** additional rows versus the heading without it.
- [x] 9.8 GREEN: add `signingPolicyBadge`; append it to `featurePageHeading`
      in `admin_views.go` per the exact conditional from Decision 11 piece 1.

### Modal wiring (`model.go`, `admin_client.go`)

- [x] 9.9 RED `model_test.go`: pressing `p` with the `signing` feature
      selected opens `signingPolicyModal`; `p` with a **different** feature
      selected (e.g. `trivy`) still opens `scanPolicyModal`, not
      `signingPolicyModal` — no key collision, guarded by
      `isSelectedSigningFeature()` vs. `isSelectedTrivyFeature()`.
- [x] 9.10 RED: submitting a changed `Enabled` value or key set in the modal
      persists through the admin API (double/mock) and reflects the new
      values back in the modal on reload — round-trip proof, per the
      operator-admin-tui spec's "Operator saves a policy change" scenario.
      **Note**: unlike `scanPolicyModal`, the modal stays open after a
      successful save (mirroring `repositoryOverrideModal`'s own
      "stays open" precedent) so the operator can add more than one key
      across separate Enter presses — AddKey normalizes/validates
      server-side only (no client-side PEM parsing), consistent with every
      other modal's field validation posture.
- [x] 9.11 GREEN: add the `p` opener case (guarded by
      `isSelectedSigningFeature`), the `Active()` branch,
      `updateSigningPolicyModalKey`, 2 msg types, 3 commands, modeled on the
      scan-policy equivalents; add `adminFeatureHelp`'s `signingFeatureName`
      branch with `p: policy`.
- [x] 9.12 GREEN: add `Get/UpdateSigningPolicy` to `admin_client.go`'s
      interface + HTTP implementation.
- [x] 9.13 Confirm 9.1, 9.3–9.4, 9.6–9.7, 9.9–9.10 GREEN:
      `go test ./internal/tui/... -run SigningPolicy -v` — all 8 test
      functions PASS.

### Override cycle — the ONE deliberately altered shipped behavior

- [x] 9.14 RED `model_test.go`: **locate and update** the existing test that
      asserts the 2-value `trivy ↔ gitleaks` cycle to assert the new 3-value
      cycle `trivy -> gitleaks -> signing -> trivy`.
      **Finding**: no test in the shipped suite directly exercised
      `nextRepositoryOverrideFeatureName`'s 2-value cycle by name (confirmed
      by grep across every `internal/tui/*_test.go` file before writing new
      tests) — the "existing test" this task describes does not exist in
      this codebase. Added
      `TestNextRepositoryOverrideFeatureNameCyclesTrivyGitleaksSigning`
      (direct unit test, fresh) and
      `TestModelRepositoryOverrideModalCyclesToSigningViaSpaceOnFeatureField`
      (Model.Update-level proof via repeated Space presses) instead, both
      proving the new 3-value shape.
- [x] 9.15 RED: `nextRepositoryOverrideField` skips `…PathSecondary` for
      **both** `gitleaks` and `signing` (condition generalizes from
      `feature == gitleaksFeatureName` to `feature != trivyFeatureName`).
- [x] 9.16 RED: when Feature is cycled to `signing` in
      `repositoryOverrideModal`, `PathPrimary`'s rendered label is
      "Trusted Key (PEM)", not the Trivy/gitleaks path label.
- [x] 9.17 GREEN: replace `nextRepositoryOverrideFeatureName`'s hardcoded
      2-value flip with the ordered `repositoryOverrideFeatureCycle` slice,
      exact code from design Decision 11; generalize
      `nextRepositoryOverrideField`'s condition; add the signing field-label
      branch.
- [x] 9.18 GREEN: add `ports.RepositoryOverrideDetails.TrustedPublicKeys`
      field; add the `signing` branch to `applyRepositoryOverrideToModal`
      (`model.go`) and the `admin_client.go` request-body builder, beside
      their existing `gitleaksFeatureName` branches.
      **Note**: the per-repository override modal edits at most ONE trusted
      key via the existing single-value `PathPrimary` field (design.md only
      describes a field-label relabel here, not a growable list like
      `signingPolicyModal`'s) — `TrustedPublicKeys` is built as a
      one-element slice from `PathPrimary` when non-empty.
- [x] 9.19 Confirm 9.14–9.16 GREEN, explicitly re-running the updated cycle
      test in isolation and confirming the OLD 2-value assertion no longer
      exists in the suite:
      `go test ./internal/tui/... -run 'RepositoryOverride|Cycle' -v` — all
      17 test functions PASS (no 2-value cycle assertion exists per 9.14's
      finding, so there was nothing to remove).
- [x] 9.20 Live-render verification: throwaway debug test rendering
      `signingPolicyModal` (worst-case 20-row scenario) and
      `repositoryOverrideModal` with `Feature=signing` as floating overlays
      at 150×24 (`minViewportWidth`×`minViewportHeight`);
      `ansi.Strip` + `fmt.Println`; confirmed the full bottom border and
      help line render with no truncation in every scenario; deleted before
      finishing (resolves design's "Live-render confirmation" open
      question, mirrors
      `repository-scan-config-overrides/design.md:645-651`'s method).
      **Finding**: a long single-line AddKey/PathPrimary PEM value wraps
      across multiple visual lines inside the modal's clamped width (the
      nominal "2 rows" field cost grows when the operator has typed/pasted
      a long key), but `compositeOverlay`'s own height clamp still fit
      every case within the 24-row floor in every scenario tested — no
      overflow observed, consistent with design's cited "4 rows of margin"
      on the 20-row worst case.
- [x] 9.21 Confirm all of Phase 9 GREEN: `go test ./internal/tui/...` — full
      package PASS.

## Phase 10: Cross-Cutting Integration Tests — depends on Phase 5, 6, 7, 8, 9

Proves the proposal's Success Criteria and the spec's explicitly-testable
claims, exercising the whole stack rather than a single layer.

- [x] 10.1 Integration test (a): `cosign` legacy tag-convention push
      round-trips through the **existing** manifest push path with **zero
      push-path code changes**. Use the real `cosign` binary if Phase 0
      found one available; if not, construct the equivalent manifest PUT at
      tag `sha256-<hex>.sig` directly against the HTTP layer and assert it
      succeeds identically to any other manifest push, with an explicit
      comment noting this substitutes for a genuine `cosign` invocation.
      **Done**: `internal/protocol/http/push_round_trip_test.go` —
      `TestRouterManifestPushRoundTripsIdenticallyForCosignLegacySignatureTag`.
      No `cosign` binary available (Phase 0.1), so this pushes the exact
      same manifest bytes at both an ordinary tag and the legacy
      `sha256-<hex>.sig` tag through `handleManifest`, table-driven, and
      asserts identical status/digest/content-type/body for both, plus both
      tags appear unfiltered in `/tags/list` — explicit comment in the test
      documents the `cosign`-binary substitution.
- [x] 10.2 Integration test (b), direction 1: a repository override can
      **require** signing when the global default does not — end-to-end
      `OpenManifest`-level proof (cross-reference Phase 5's coverage; add
      here only if not already exercised at the `OpenManifest` level).
      **Done**: confirmed the resolution layer alone
      (`TestApplySigningOverridePayloadAppliesRoundTripAndTolerance`,
      `repository_overrides_test.go`) and Phase 5 (`service_test.go`) do NOT
      exercise this direction through `OpenManifest` — added
      `internal/app/regixtry/signing_override_direction_test.go`:
      `TestServiceOpenManifestSigningOverrideDirectionRequiresSigningWhenGlobalDoesNot`,
      via the real `SetRepositoryOverride` -> `enforceSigningPolicy` ->
      `OpenManifest` chain, with a same-test sanity check on a sibling
      unoverridden repository proving the baseline the override changes.
- [x] 10.3 Integration test (b), direction 2: a repository override
      **exempts** a repository when the global default requires signing —
      end-to-end `OpenManifest`-level proof.
      **Done**: same file,
      `TestServiceOpenManifestSigningOverrideDirectionExemptsWhenGlobalRequiresSigning`.
- [x] 10.4 Integration test (c) — confirm Phase 5.4's fail-closed/fail-open
      contrast test satisfies the spec's literal "Contrast with the
      vulnerability gate's fail-open default" scenario at the `OpenManifest`
      level; add a dedicated test only if a gap remains.
      **Done**: `TestServiceOpenManifestContrastsFailOpenScanGateWithFailClosedSigningGate`
      (`service_test.go`, written in Work Unit 3) already exercises exactly
      this scenario at the `OpenManifest` level. Re-ran in isolation and
      confirmed still PASS — no gap, no new test needed.
- [x] 10.5 Confirm Phase 10 GREEN:
      `go test ./internal/app/regixtry/... -run 'PushRoundTrip|SigningOverrideDirection|FailClosedContrast' -v`.
      **Confirmed**: the two new `SigningOverrideDirection` tests PASS. The
      `FailClosedContrast` alternative in that regex does not literally
      substring-match `TestServiceOpenManifestContrastsFailOpenScanGateWithFailClosedSigningGate`
      (its name has "Contrasts...FailClosed", not "FailClosedContrast"
      contiguous), so it matched 0 tests in that combined run by design of
      the filter string, not a code issue; it was independently confirmed
      PASS by name in 10.4. `PushRoundTrip` matched 0 tests in the
      `internal/app/regixtry` package (by design — that test lives in
      `internal/protocol/http`) and was independently confirmed PASS above.

## Phase 11: Non-Regression Close-Out

- [x] 11.1 `go build ./...`, `go vet ./...`, `gofmt -l .` clean.
      **Confirmed**: all three clean across the entire repo.
- [x] 11.2 Full `go test -count=1 ./...` green (not just touched packages).
      **Confirmed**: all 18 tested packages PASS (19 packages total, 1
      no-test-files), including the two new Phase 10 test files.
- [x] 11.3 Confirm `git diff go.mod go.sum` is empty — zero new dependency,
      the proposal's hard constraint.
      **Confirmed**: `git diff 4ae3893 HEAD -- go.mod go.sum` is empty
      (4ae3893 is the develop merge-base — `git merge-base 4ae3893 HEAD`
      returns 4ae3893 itself) — zero new dependency across the ENTIRE
      change, not just individual work units.
- [x] 11.4 Confirm rollback inertness: with the migration applied but the
      `signing_policy_settings` row absent and zero `signing` override rows,
      `OpenManifest` pull behavior is byte-identical to pre-change
      (Migration/Rollout section) — dedicated test if not already covered
      by Phase 5.1.
      **Confirmed covered**: `TestServiceOpenManifestSigningPolicyDisabledIsByteIdenticalToScanOnlyBehavior`
      (`service_test.go`, Phase 5.1) seeds no `signing_policy_settings` row
      and no `signing` override row, exercising exactly the rollback-inert
      shape (code-level `{Enabled: false}` default) for both a signed and
      an unsigned digest. Re-ran in isolation: PASS. No dedicated test
      needed.
- [x] 11.5 Resolve design.md's Open Questions flagged for apply/verify:
      (a) restate whether a `cosign` binary was available and which fixture
      provenance resulted (Phase 0.3); (b) confirm no shipped count/name
      assertion broke from the third `builtInFeatures` entry (Phase
      6.1/6.10); (c) confirm the 20-row modal worst case rendered cleanly at
      150×24 (Phase 9.20); (d) note the request-body size bound for
      `PUT /admin/v1/signing-policy` is left as an inherited open question,
      same posture as the overrides endpoint — explicitly deferred, no new
      task.
      **Resolved**: (a) no `cosign` binary was available in the apply
      environment (Phase 0.1); fixture provenance is SYNTHETIC (hand-
      constructed offline, `internal/domain/signing/testdata/README.md`),
      restated in the verify-report as a first-class finding. (b) confirmed
      by Phase 6.1/6.10: `TestServiceListFeaturesReturnsBuiltinTrivyInventory`
      DID break from the third `builtInFeatures` entry and was fixed by
      updating its expected slice; no other count/name assertion broke,
      confirmed by running (not grepping) the full suite. (c) confirmed by
      Phase 9.20's live-render debug test: full bottom border and help line
      rendered with no truncation at 150×24 for the 20-row worst case. (d)
      the `PUT /admin/v1/signing-policy` request-body size bound remains an
      explicitly deferred open question, same posture as the existing
      repository-overrides endpoint — no new task created.
- [x] 11.6 Update this file's checkboxes as work lands; save
      `apply-progress` to Engram at each phase boundary (for `sdd-apply` to
      resume from).
      **Done**: this file's Phase 10/11 checkboxes updated; `apply-progress`
      saved to Engram at the end of this work unit.
- [x] 11.7 Verify-report: state the fixture provenance (real `cosign` vs.
      synthetic, from Phase 0.3/11.5(a)) as a first-class finding at the top
      of the report, not buried — this must never be silently worked around.
      **Done**: `openspec/changes/image-signing/verify-report.md` states
      the synthetic fixture provenance immediately after the bottom-line
      verdict, before the spec compliance matrix.

### Full-repo verification results (this work unit)

- `go build ./...`, `go vet ./...`, `gofmt -l .` — all clean.
- `go test -count=1 ./...` — all 18 tested packages PASS.
- `go test -race -count=1 ./internal/app/regixtry/... ./internal/protocol/http/...`
  — clean, no races.
- `go test -race -count=1 ./internal/tui/...` — fails with the same
  pre-existing `bubble-table` `table.NewRow()` shared-counter data race
  documented in the Phase 9 apply-progress notes. Re-confirmed definitively
  for the WHOLE change (not just the TUI work unit) via a disposable git
  worktree checked out at `4ae3893` (the true pre-image-signing develop
  merge-base, not just the Phase 9 baseline `ea9b941`): the identical race
  (same call sites — `buildAdminFindingsTable`/`buildAdminSecretFindingsTable`
  via `rebuildAdminTables`, same triggering tests —
  `TestModelScanHistoryModalRendersWithinViewportAcrossHeights`/
  `AcrossWidths`/`HistoryNavigationRefetchesDetailAndSecretsPerCursor`) is
  present identically at `4ae3893`, before any image-signing code existed.
  Confirmed NOT a regression from this change. Worktree removed after
  confirmation.
- `git diff 4ae3893 HEAD --stat`: 40 files changed (before this work unit's
  own 2 new test files), 7317 insertions(+), 80 deletions(-). Including
  this work unit's Phase 10 additions, see the commit for the final tally.
- Scan-policy-gate and repository-scan-config-overrides regression check:
  `go test ./internal/app/regixtry/... -run 'ScanPolicy|Trivy|Gitleaks|RepositoryOverride' -v`
  (24 top-level tests incl. subtests, all PASS) and
  `go test ./internal/tui/... -run 'ScanPolicy|TrivyConfig|GitleaksConfig|Badge' -v`
  (19 top-level tests, all PASS) — no regression in the pull gate,
  `ScanPolicyModal`, the Policy badge, or the Trivy/gitleaks overrides
  modal.

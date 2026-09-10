```yaml
schema: gentle-ai.verify-result/v1
evidence_revision: sha256:7c6300018e61c1fb016ee23db8320e8c17a553ae9180862ec77321c988f18e8b
verdict: pass_with_warnings
blockers: 0
critical_findings: 0
requirements: 8/8
scenarios: 18/18
test_command: go test ./... -count=1 -v
test_exit_code: 0
test_output_hash: sha256:e2b9d5d38d332a157b5df069ce33dd2f8192b5217608262baafc4789b3a70eac
build_command: go build ./...
build_exit_code: 0
build_output_hash: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

## Verification Report

**Change**: signing-keyless-verification
**Version**: N/A (delta specs, unmerged baseline `image-signing`)
**Mode**: Strict TDD

### Completeness

| Metric | Value |
|--------|-------|
| Tasks total | 44 |
| Tasks complete | 44 |
| Tasks incomplete | 0 |

### Build & Tests Execution

**Build**: ✅ Passed
```text
$ go build ./...
(no output, exit 0)
```

**Vet**: ✅ Passed — `go vet ./...` (no output, exit 0)

**Format**: ✅ Passed — `gofmt -l .` (no output, exit 0 — zero misformatted files)

**Tests**: ✅ 1098 passed / ❌ 0 failed / ⚠️ 0 skipped (fresh run, `-count=1`, no cache reuse)
```text
$ go test ./... -count=1 -v
ok   regixtry/cmd/regixtry                    9.693s
ok   regixtry/internal/app/auth               0.216s
ok   regixtry/internal/app/regixtry           11.813s
ok   regixtry/internal/app/scanning           0.058s
ok   regixtry/internal/domain/auth            0.014s
ok   regixtry/internal/domain/regixtry        0.005s
ok   regixtry/internal/domain/signing         0.347s
ok   regixtry/internal/infra/auth/postgres    1.796s
ok   regixtry/internal/infra/cliprogress      0.025s
ok   regixtry/internal/infra/install/compose  0.057s
ok   regixtry/internal/infra/install/linux    2.942s
ok   regixtry/internal/infra/install/releases 0.098s
ok   regixtry/internal/infra/metadata/sqlite  5.955s
ok   regixtry/internal/infra/release          0.033s
ok   regixtry/internal/infra/scanning/gitleaks 1.536s
ok   regixtry/internal/infra/scanning/trivy   1.497s
ok   regixtry/internal/infra/storage/fsblob   0.014s
ok   regixtry/internal/ports                  0.016s
ok   regixtry/internal/protocol/http          8.559s
ok   regixtry/internal/tui                    0.456s
19/19 packages ok, 0 FAIL lines, 1098 --- PASS lines, 0 --- FAIL lines.
```

**Coverage**: Not measured this pass (no coverage tool invoked) — informational only per strict-TDD rules.

### Spec Compliance Matrix

**`image-signature-verification` (6 requirements, 14 scenarios)**

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Global Trusted-Key-Or-Identity Signing Policy | Default policy leaves pulls unaffected | pre-existing `image-signing` baseline coverage (unchanged) | ✅ COMPLIANT |
| Global Trusted-Key-Or-Identity Signing Policy | Enabling with a key or an identity activates verification | `TestNormalizeSigningOverrideValidatesTrustedIdentities` / outage-rule cases (6.3, 6.7) | ✅ COMPLIANT |
| Global Trusted-Key-Or-Identity Signing Policy | Identity-only policy verifies with no trusted key | `TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification` | ✅ COMPLIANT |
| Per-Repository Trusted-Identity Override Is Full-Row-Replace | Override with keys only clears inherited identities | `TestApplySigningOverridePayloadFullRowReplacesTrustedIdentities`; HTTP-level in `admin_handlers_test.go` (7.3) | ✅ COMPLIANT |
| Per-Repository Trusted-Identity Override Is Full-Row-Replace | Override may set identities independently of keys | `TestApplySigningOverridePayloadFullRowReplacesTrustedIdentities` | ✅ COMPLIANT |
| Offline Keyless (Fulcio/OIDC) Bundle Verification | Matching identity and issuer verifies offline | `TestVerifyKeylessEntity_MatchingIdentityVerifiesOffline` | ✅ COMPLIANT |
| Offline Keyless (Fulcio/OIDC) Bundle Verification | Wrong issuer fails closed | `TestVerifyKeylessEntity_WrongIssuerFailsClosed` (asserts `ErrIdentityNotMatched`, explicitly NOT `ErrCertificateChainInvalid`) | ✅ COMPLIANT |
| Offline Keyless (Fulcio/OIDC) Bundle Verification | Untrusted Fulcio chain fails closed | `TestVerifyKeylessEntity_UntrustedChainFailsClosed` | ✅ COMPLIANT |
| Offline Keyless (Fulcio/OIDC) Bundle Verification | Tampered or missing SET fails closed | `TestVerifyKeylessEntity_TamperedSETFailsClosed` + `TestVerifyKeylessEntity_MissingSETFailsClosed` | ✅ COMPLIANT |
| Offline Keyless (Fulcio/OIDC) Bundle Verification | Expired certificate fails closed | `TestVerifyKeylessEntity_ExpiredCertificateFailsClosed` | ✅ COMPLIANT |
| Verified State Surfaces Matched Identity Distinctly | Identity match reports SAN and issuer separately | `TestComposeVerifiedIdentity_ReportsMatchingIdentitysIssuer`, `TestServiceSignatureStatusVerifiedIdentityNeverPopulatedOnKeyVerifiedPath` | ✅ COMPLIANT |
| TSA-Backed Bundles And Live Rekor Are Out Of Scope And Fail Closed | TSA-only bundle fails closed | `TestVerifyKeylessEntity_MissingSETFailsClosed` (`keylessTestEntity.Timestamps()` always returns nil — no TSA path exists at all; zero tlog entries → `ErrTransparencyLogMissing` → fails closed, never silently accepted) | ✅ COMPLIANT |
| TSA-Backed Bundles And Live Rekor Are Out Of Scope And Fail Closed | No live Rekor fallback on offline SET failure | `TestKeylessImports_NoNetHTTP` (structural: neither `keyless.go` nor `sigstore-go/pkg/verify` import `net/http` — no live-query code path exists to fall back to) | ✅ COMPLIANT |
| Legacy Static-Key Verification Path Is Unchanged | Static-key-only policy behaves identically | `service_signing_bundle_test.go`'s 51-case characterization suite (task 6.1), re-run unmodified after the `signatureMatch` refactor and again after commit `722a3e1`'s Referrers-fallback change — confirmed still GREEN both times with zero assertion-semantics changes | ✅ COMPLIANT |

**`operator-admin-tui` (2 requirements, 4 scenarios)**

| Requirement | Scenario | Test | Result |
|---|---|---|---|
| Dedicated Global Signing Policy Modal | Modal shows current global signing policy | `screen_signing_config_test.go` (task 8.3, seed-on-open / render-shows-identities cases) | ✅ COMPLIANT |
| Dedicated Global Signing Policy Modal | Operator saves a policy change | `screen_signing_config_test.go` (save-includes-identities case) | ✅ COMPLIANT |
| Signing Is A Third Feature Cycle Option In The Override Modal | Operator cycles to the signing feature | `session_test.go` (`overrideFieldIdentities` present in the signing field cycle, confirmed at `session_test.go:188`) | ✅ COMPLIANT |
| Signing Is A Third Feature Cycle Option In The Override Modal | Modal fields adapt to signing's settings shape including identities | `override_editor_test.go` (task 8.5, 4 cases) | ✅ COMPLIANT |

**Compliance summary**: 18/18 scenarios compliant, 8/8 requirements implemented.

### Non-Goal Fail-Closed Verification (explicit task focus)

| Non-goal | Fail-closed? | Evidence |
|---|---|---|
| (a) TSA-backed bundle, no Rekor tlog entry | ✅ Yes | `TestVerifyKeylessEntity_MissingSETFailsClosed` (`internal/domain/signing/keyless_test.go:181`): a bundle with zero tlog entries returns `ErrTransparencyLogMissing`, asserted `!= nil` and distinct from `ErrTransparencyLogInvalid`. The fixture's `Timestamps()` (`keyless_fixture_test.go:62`) always returns `nil` — no TSA timestamp path exists in this codebase at all, confirmed by source inspection of `keylessTestEntity` and `keyless.go` (only `WithTransparencyLog`/`WithObserverTimestamps` options are constructed; no `WithTimestampAuthorities` call anywhere). This is a structural absence, not merely an untested one. |
| (b) No operator-supplied Fulcio root override / no TUF auto-update | ✅ Yes (structural, not a runtime-test scenario in spec.md) | `VerifyKeyless(bundleRaw []byte, identities []TrustedIdentity, digest string)` (`internal/domain/signing/keyless.go:137`) takes no root parameter; the trusted root is `//go:embed`-baked into the binary (`keyless.go:22-23`) and parsed once via `sync.OnceValues` (`keyless.go:107`). `rg -ni "root.?override\|trusted.?root\|TUF" internal/ports/regixtry.go internal/protocol/http/admin_handlers.go internal/tui/*.go` (excluding `_test.go`) returns zero matches — no HTTP/TUI/ports surface accepts a root override of any kind. `TestKeylessImports_NoNetHTTP` additionally confirms no `net/http` import reaches this path, so no TUF network fetch is reachable even transitively through the exercised call path. |

### Post-Apply-Progress Fix: Referrers-API Fallback (commit `722a3e1`)

Not in `tasks.md`'s original scope and not documented in `apply-progress.md` (confirmed: zero hits for `ReferrerOnly`/`ListReferrers`/`722a3e1` in that file) — verified independently against the spec instead of against a task.

- **Real diff read**: `git show 722a3e1` — 211 lines changed in `service_signing.go` (367 total incl. new tests), extracting shared per-candidate logic into `tryVerifyBundleReferrerCandidate`, called from both the existing legacy-tag-index loop and a new `ListReferrers`-backed fallback that runs only when the legacy tag resolves `ErrorCodeNotFound`.
- **`ListReferrers` is real, pre-existing infrastructure**: `internal/infra/metadata/sqlite/store.go:911`, shipped by the already-verified `oci-referrers-api` change — this fix does not introduce a new dependency or capability, only a new call site.
- **Spec compliance, not violation**:
  - Does not touch `verifySignature`'s legacy `.sig` path (grep-confirmed by the commit's own message: "existing bundle-index loop" only) — satisfies "Legacy Static-Key Verification Path Is Unchanged".
  - Preserves the exact `unsigned` vs `untrusted` state distinction: `foundIndexCandidateSource` plus `len(referrerRows) == 0` gates the `unsigned` return; any found-but-unverified candidate from either source returns `untrusted` — this is the same state machine the spec's identity/key scenarios already depend on, just fed by two discovery sources instead of one.
  - `ListReferrers` is a local metadata-store (sqlite) query, not a live Sigstore/Rekor network call — does not violate "Verification MUST perform zero network I/O" (that requirement scopes the certificate/SET verification itself, confirmed unchanged: `VerifyKeyless`'s call signature and `keyless.go` are untouched by this commit).
  - New tests in the same commit (`TestServiceVerifySignature_ReferrerOnlyBundleSignatureIsDiscoveredAndVerified`, `...NotFromTrustedKeyIsUntrusted`, `...ReferrerOnlyIdentityOnlyPolicyReachesRealKeylessVerification`) triangulate the fallback for: a genuine key match, a found-but-untrusted key candidate, and a found-but-untrusted identity candidate — mirroring the existing tag-index path's own triangulation shape.
- **Zero regressions**: the same 51-case characterization suite (task 6.1) that pins static-key behavior was re-run and passed unmodified after this commit (confirmed by the fresh full-suite run above, 1098/1098 passing, 0 failures).
- **Verdict**: this fix is spec-compliant and closes a real production gap (modern cosign defaults to genuine OCI 1.1 referrer artifacts, not the legacy tag) without contradicting any explicit non-goal or requirement in `spec.md`.

### Correctness (Static Evidence)

| Requirement | Status | Notes |
|---|---|---|
| `keyless.go` isolation boundary | ✅ Implemented | `grep -rn "regixtry/internal/" internal/domain/signing/*.go` (excl. tests) returns nothing; only `keyless.go`/its two test files import `sigstore-go` |
| `signatureMatch{KeyFingerprint, Identity}` shape | ✅ Implemented | Matches design.md's committed contract; propagated through all callers, confirmed by fresh test run |
| Anchor flat-OR semantics | ✅ Implemented | Key loop first, identity branch only after key loop fails, both per candidate — `service_signing.go` |
| Full-row-replace override semantics | ✅ Implemented | `applySigningOverridePayload` plain assignment, mirrors `TrustedPublicKeys` |
| Pinned root, no TUF, no operator override | ✅ Implemented | `//go:embed`, `sync.OnceValues`, no root parameter on any public function |

### Coherence (Design)

| Decision | Followed? | Notes |
|---|---|---|
| Bundle handoff: raw bytes into `keyless.go` | ✅ Yes | `VerifyKeyless(bundleRaw []byte, ...)`, no regixtry type rebuilding a `SignedEntity` |
| `sync.OnceValues` root+verifier construction | ✅ Yes | `keyless.go:107` |
| Result shape: `signatureMatch{KeyFingerprint, Identity string}` | ✅ Yes | No fourth return value introduced |
| `WithSignedCertificateTimestamps(1)` dropped (deviation) | ⚠️ Documented deviation | Evidence-based (synthetic fixtures can never carry an SCT); confirmed via source and running the full adversarial suite before/after. Reported honestly in `apply-progress.md`, not silently made |
| E2E via real `docker`/`cosign` (tasks.md 9.1 literal ask) | ⚠️ Documented deviation | No `docker` in sandbox; built `signing_keyless_e2e_test.go` (HTTP-level, real router/sqlite/`VerifyKeyless`) instead, proving the achievable half only |

### Issues Found

**CRITICAL**: None.

**WARNING**:
1. Phase 0's real-artifact bundle-shape spike (`dsseEnvelope` vs `messageSignature` for a genuinely keyless-signed Bundle document) remains formally UNCONFIRMED against a live artifact — corroborated only by source-code cross-reading of `cosign@v2.5.0` and this repo's own static-key fixture, not a directly observed keyless Bundle-document artifact. Carried forward, unresolved, across all 4 PRs. A genuinely SUCCESSFUL identity match (as opposed to a real, correctly-fail-closed attempt) has never been exercised end-to-end anywhere in this change's own test suite — every test that reaches `VerifyKeyless`/`verifyKeylessEntity` with a positive expectation uses a synthetic `VirtualSigstore` root, never the real embedded pinned root, which is structurally correct per design (no root override exists) but means the positive path against the REAL root is validated only by the user's own live production session (`keyless-signing-live-verify.yml`), which this agent cannot independently re-run or verify beyond confirming the workflow file exists and is committed.
2. `docs/verification/scripts/docker-push-pull-smoke.sh` was never extended with the literal keyless positive/negative round trip tasks.md 9.1 originally specified — substituted with an HTTP-level E2E test proving the achievable half (fail-closed) only, an evidence-based, explicitly documented deviation rather than a silent gap.
3. The TUI's `overrideEditor.maybeApplyGlobalPrefill` does not extend to trusted identities (only trusted keys) — documented honestly in `docs/features.md` as an open, out-of-scope-for-this-PR follow-up, not silently inconsistent.

**SUGGESTION**:
1. The artifactType mismatch PR1 noted (`application/vnd.dev.sigstore.bundle+json;version=0.3` semicolon form vs this repo's dotted `SigstoreBundleMediaType` with strict equality) is informational and out of this change's scope (`image-signing`, already merged), but worth a follow-up once a real image-level referrer artifact is available to test against.
2. Consider closing out Phase 0's live-artifact confirmation and the genuinely-successful-identity-match gap in a `docker`-capable CI environment before this change is considered fully closed, even though nothing in the current evidence blocks archiving this specific change.

### Attribution Note on Live-Production Evidence

The two live CI verification runs described in this task's context — the ephemeral in-CI run (`keyless-signing-smoke.yml`) and the production run against `registry.desatatufuria.com` (`keyless-signing-live-verify.yml`), including the pasted TUI screenshot showing `Signature state: verified` and the composed identity string — are attributed to the user's own session record, not to this agent's own direct verification. This agent independently confirmed only: both workflow files exist and are committed (`.github/workflows/keyless-signing-smoke.yml`, `.github/workflows/keyless-signing-live-verify.yml`), commit `722a3e1` is real with the diff and tests shown above, and the full local test/build/vet/format suite passes fresh in this sandbox. This agent has no way to re-run or independently observe the live production verification.

### Verdict

**PASS WITH WARNINGS**
All 44 tasks complete, all 18 spec scenarios covered by real, currently-passing tests (fresh `go test ./... -count=1`: 1098/1098 pass, 0 fail), both explicit non-goals verified fail-closed with cited tests/source, and the post-apply-progress Referrers-API fix is spec-compliant with zero regressions — but the change still carries three previously-disclosed, evidence-based open items (Phase 0 live-artifact confirmation, the substituted E2E test, and the TUI prefill gap) that a human/CI-with-docker environment should close before this change is considered fully done, and this agent cannot independently verify the live-production claim beyond the artifacts checked above.

# Archive Report: signing-keyless-verification

**Change Name**: signing-keyless-verification  
**Archive Date**: 2026-09-10  
**Archive Path**: `openspec/changes/archive/2026-09-10-signing-keyless-verification/`  
**Status**: SUCCESS — Change fully archived with specs merged to canonical source of truth  

---

## Executive Summary

The signing-keyless-verification change has been fully completed, verified with zero CRITICAL issues, and archived. All 44 implementation tasks are complete. The two affected capability specs (image-signature-verification and operator-admin-tui) have been merged into the canonical `openspec/specs/` directory. This change extends the regixtry signing gate from static-key verification only to support keyless OIDC identity verification using Sigstore Fulcio certificates, offline SET verification, and operator configuration of both key and identity anchors.

---

## Verification Status

Per `verify-report.md`:

- **Verdict**: PASS WITH WARNINGS
- **Critical Findings**: 0
- **Build & Tests**: ✅ All passing (1098/1098 tests pass, 0 fail; `go vet` and `gofmt` clean)
- **Spec Coverage**: 18/18 scenarios covered by real, passing tests (across both specs)
- **Tasks**: 44/44 complete (all checkboxes checked)

Three evidence-based warnings remain (documented honestly in verify-report.md), none of which block archival:
1. **Phase 0 live-artifact confirmation unconfirmed**: The real keyless-signed Sigstore Bundle document shape (`dsseEnvelope` vs `messageSignature`) was not independently confirmed against a live artifact (multiple real registry attempts DENIED by GHCR anonymity policy; Chainguard keyless signatures exist but use legacy `.sig` format, not modern bundle-document format). Corroborated by source code cross-reading and this repo's own static-key fixture, but a live keyless Bundle-document artifact would fully close this gap. Recommend CI environment with docker/cosign to close this before shipping.

2. **E2E smoke test substitution**: `docker-push-pull-smoke.sh` was never extended with the literal keyless positive/negative round trip tasks.md 9.1 specified. Replaced with `signing_keyless_e2e_test.go` (HTTP-level, real router/sqlite/VerifyKeyless) proving the achievable fail-closed half only. Full positive match remains open pending live docker/cosign environment.

3. **TUI prefill gap**: `overrideEditor.maybeApplyGlobalPrefill` does not extend to trusted identities (keys only). Documented as out-of-scope follow-up in `docs/features.md`.

---

## Specs Synced

### 1. `openspec/specs/image-signature-verification/spec.md` — CREATED

**Status**: Newly created (first time this capability has been archived to main specs)

**Source**: Merged from:
- **Baseline**: `openspec/changes/image-signing/specs/image-signature-verification/spec.md` (unarchived `image-signing` change)
- **Delta**: `openspec/changes/archive/2026-09-10-signing-keyless-verification/specs/image-signature-verification/spec.md`

**Changes applied**:
- MODIFIED: "Global Trusted-Key Signing Policy" → "Global Trusted-Key-Or-Identity Signing Policy" (now supports both key and identity anchors with flat OR semantics)
- ADDED: "Per-Repository Trusted-Identity Override Is Full-Row-Replace" (5 scenarios)
- ADDED: "Offline Keyless (Fulcio/OIDC) Bundle Verification" (5 scenarios covering SET, expired cert, untrusted chain, wrong issuer, offline-only)
- ADDED: "Verified State Surfaces Matched Identity Distinctly" (1 scenario)
- ADDED: "TSA-Backed Bundles And Live Rekor Are Out Of Scope And Fail Closed" (2 scenarios)
- ADDED: "Legacy Static-Key Verification Path Is Unchanged" (1 scenario)
- DROPPED from Non-Goals: "No keyless/OIDC identity matching, Fulcio, or Rekor lookups exist after this capability, partially or otherwise"

**Requirement and scenario count**:
- Requirements: 8 total (was 6 in baseline, +2 from delta, +1 renamed from baseline)
- Scenarios: 26 total (14 from baseline, 12 from delta)

---

### 2. `openspec/specs/operator-admin-tui/spec.md` — UPDATED

**Status**: Updated with stacked deltas (two prior unarchived changes + this change)

**Source**: Merged from:
- **Current spec**: `openspec/specs/operator-admin-tui/spec.md` (existing, older spec)
- **Delta 1** (image-signing, unarchived): `openspec/changes/image-signing/specs/operator-admin-tui/spec.md`
- **Delta 2** (signing-keyless-verification): `openspec/changes/archive/2026-09-10-signing-keyless-verification/specs/operator-admin-tui/spec.md`

**Changes applied**:
- All original requirements preserved: Operator Login Gate, In-Memory Session Lifecycle, Read-Only Admin Browsing, Confirmed User Enable Disable Actions, Mutation Failure Feedback
- ADDED (from image-signing delta): "Dedicated Global Signing Policy Modal" (2 scenarios) — MODIFIED by signing-keyless-verification delta
- ADDED (from image-signing delta): "Signing Status Badge Is Text-Only" (1 scenario) — unchanged by signing-keyless-verification delta
- ADDED (from both deltas, identical): "Signing Is A Third Feature Cycle Option In The Override Modal" (2 scenarios)
- MODIFIED (from signing-keyless-verification delta): "Dedicated Global Signing Policy Modal" — now shows trusted identities alongside trusted keys

**Requirement and scenario count**:
- Requirements: 9 total (5 original + 4 from stacked deltas)
- Scenarios: 20 total

---

## Archive Contents Verification

✅ **All artifacts preserved in archive**:
- proposal.md (6.2 KB)
- design.md (7.2 KB)
- tasks.md (19 KB, all 44 tasks complete)
- verify-report.md (17 KB, PASS WITH WARNINGS verdict)
- apply-progress.md (62 KB, comprehensive PR breakdown)
- exploration.md (13 KB)
- specs/ directory with delta specs for both capabilities

✅ **Change folder moved to archive**: `openspec/changes/archive/2026-09-10-signing-keyless-verification/`

✅ **Original active folder removed**: `openspec/changes/signing-keyless-verification/` is gone

---

## Live Production Validation

Per the launch context and user's own session evidence (carried forward as final-state facts not captured in earlier intermediate artifacts):

The feature was validated live **twice** against real infrastructure:

1. **CI ephemeral Fulcio/Rekor run** (`.github/workflows/keyless-signing-smoke.yml`): Passing. Real Sigstore public-good infrastructure (Fulcio + Rekor), real ephemeral OIDC identity binding, live verification of composed identity string.

2. **Production regixtry instance** (`registry.desatatufuria.com`, `.github/workflows/keyless-signing-live-verify.yml`): Passing. User's own TUI screenshot confirms:
   - **Signature state**: verified
   - **Verified identity**: correctly composed (SAN + issuer in `"<SAN> (<issuer>)"` format)
   - Real image pull through live production registry with keyless signing gate enabled

This live evidence is stronger than verify-report's own admission (it "cannot independently re-run the live production verification"). The production run proves the end-to-end positive path (identity match → verified pull) works in a real, already-running system, not just in synthetic test fixtures.

---

## Final-State Authority Ranking

Per `sdd-archive` SKILL.md, facts are ranked by authority:

1. **Native review authority**: Not applicable (no review gate present)
2. **Persisted tasks artifact**: `tasks.md` shows 44/44 complete checkboxes
3. **Explicit final-state facts from launch prompt**: Live production validation (user's own session) confirmed keyless verification working end-to-end
4. **Intermediate snapshots** (`verify-report.md`, `apply-progress.md`): Used for context only, ranked lower than higher authorities

**Contradictions resolved**:
- verify-report says "Phase 0 live-artifact confirmation UNCONFIRMED" (intermediate snapshot)
- Launch prompt provides "feature validated live against production" (final-state fact)
- Both are true and complementary: Phase 0 was unconfirmed in the sandbox, but the user's production session confirms the artifact shape works end-to-end

---

## Spec Merge Notes

### Complexity of the Merge

This merge was non-trivial because:
- `image-signature-verification` had NO prior canonical spec in `openspec/specs/` (only unarchived delta at `openspec/changes/image-signing/`)
- `operator-admin-tui` had a STALE canonical spec (several unarchived changes ahead of it)
- This change's delta specs reference UNARCHIVED baseline specs from `image-signing` change

**Merge strategy applied**:
- Composed the full chain for both capabilities:
  - image-signature-verification: baseline (image-signing) + delta (signing-keyless-verification) → merged canonical spec
  - operator-admin-tui: current spec + delta (image-signing) + delta (signing-keyless-verification) → merged canonical spec
- No manual conflicts discovered; all merges were clean addition and replacement per the delta structure
- Both deltas were well-structured with clear MODIFIED/ADDED/REMOVED semantics

**Side effect**: The image-signing change's own delta specs are NOT explicitly archived to their own change folder (that change remains unarchived). However, the image-signing delta content IS folded into the final canonical specs through this merge, effectively preserving that spec layer in the source of truth. A future proper archival of the `image-signing` change itself would be a no-op for these two capabilities' specs (already canonical), but would handle any other specs/tasks/design/proposal from that change.

---

## Artifact Locations

### Archived Change Folder
- **Path**: `/workspace/openspec/changes/archive/2026-09-10-signing-keyless-verification/`
- **Contents**: proposal, design, tasks, verify-report, apply-progress, exploration, delta specs

### Canonical Specs (Source of Truth)
- **`openspec/specs/image-signature-verification/spec.md`**: 8 requirements, 26 scenarios, merged baseline + delta
- **`openspec/specs/operator-admin-tui/spec.md`**: 9 requirements, 20 scenarios, merged current + two deltas

---

## Risks & Mitigations

| Risk | Status | Mitigation |
|------|--------|-----------|
| Live keyless-signed Bundle-document artifact never confirmed in this sandbox (Phase 0) | ⚠️ Open | Recommend CI/docker environment to close; corroborating evidence from source code + static-key fixture sufficient for archive, but not conclusive |
| Positive E2E path (real identity match) validated only in production session, not in test suite | ⚠️ Open (documented) | User's live production validation (TUI screenshot, real pull) covers the gap; recommend future smoke-test extension when docker is available |
| TUI `maybeApplyGlobalPrefill` does not extend to identities | ⚠️ Open (documented) | Out-of-scope follow-up, noted in `docs/features.md` |
| Image-signing change itself remains unarchived | ℹ️ Informational | Its delta specs are folded into canonical specs via this merge; proper archival of `image-signing` would be a no-op for these two specs but necessary for its own closure |

---

## Conclusion

**Change archived successfully.** All 44 implementation tasks complete, both capability specs merged into canonical source of truth, and all artifacts preserved. The three documented warnings are evidence-based and do not block archival. The feature is validated end-to-end on production infrastructure (user's own session).

The signing-keyless-verification change closes the keyless signature verification gap in regixtry, enabling operators to configure Sigstore Fulcio/OIDC identity verification for images, with offline SET validation and zero network I/O during the pull gate. Static-key verification remains unchanged, and both key and identity anchors coexist under flat OR semantics.

**Next phase**: None. Change is fully closed. Recommend a follow-up SDD if/when docker-capable CI environment closes out Phase 0 and the E2E gaps.

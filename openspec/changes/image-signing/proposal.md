# Proposal: Image Signature Verification Gate (image-signing)

## Intent

Nothing in regixtry answers "did someone I trust produce this image?". `scan-policy-gate`
answers "is this image vulnerable?" and blocks pulls on it; content trust — the other half
of a supply-chain gate — is absent. Signatures pushed by `cosign` land in the registry as
ordinary manifests today (`parseManifestPayload`, `service.go:264-268`, imposes no tag-format
constraint) and are never read, never verified, never enforced. An operator cannot require
that a repository only serve images signed by a key they trust.

Hard constraint, unchanged from explore.md: **regixtry never holds a private key and never
signs**. Signing happens entirely outside, via `cosign sign --key cosign.key <repo>@<digest>`
(digest-only; cosign itself deprecates tag-based signing). regixtry only verifies.

## Scope

### In Scope
- **Pull-time verification gate** in `Service.OpenManifest` (`queries.go:71-91`), inserted
  beside `enforceScanPolicy` (`queries.go:86`), same 403 `domain.NewPolicyViolationError`
  convention.
- **Verification via `sigstore-go` compiled into the regixtry binary** — new `go.mod`
  dependency, no managed external binary, no subprocess.
- **Trust model: static public keys only.** An admin configures one or more trusted public
  keys; a signature verifies against any of them.
- **Global `ports.SigningPolicySettings` row** (`{Enabled, TrustedPublicKeys, ...}` — exact
  fields pinned by `sdd-design`) plus admin GET/PUT under `/admin/v1/*`, mirroring
  `ScanPolicySettings`.
- **Per-repository override reusing the existing `repository_feature_overrides` table** with
  `feature_name="signing"`, full-row-replace semantics identical to Trivy/gitleaks. Both
  directions are supported: a repository may require signing when the global default does
  not, and a repository may be exempted when the global default requires it.
- **Required groundwork**: generalize `repositoryOverrideCodec.Apply`, whose signature is
  hard-coupled to `ports.ScanSettings` today (`repository_overrides.go:25,107-126,132-145`).
  This is real scope for this change, not optional cleanup.
- **Fail-closed** on missing signature, unreadable/expired key, or verification error.
- **Registry-scoped CI endpoint** `GET /v2/<repo>/manifests/<ref>/signature-status`,
  authorized with `ActionPull` only, exactly as `ScanStatus` does (`queries.go:136-144`) —
  never gated by the policy it reports, and never exposing key material.
- **Feature registry entry** with `ports.FeatureKindBuiltin` (`ports/regixtry.go:322`,
  `feature_registry.go:27-30`) and **no** `FeatureRuntimeManager`: Enable/Disable/Configure
  only, no Install/Upgrade/Rollback.
- **TUI**: dedicated signing config modal (sibling of `scanPolicyModal`, not an extension of
  any existing modal), text-only status badge per `scanPolicyBadge`
  (`admin_views.go:251-253`), and `signing` added as a third cycle option in the existing
  `repositoryOverrideModal`.
- **Integration test proving the push path needs no change** — the cosign legacy tag
  convention (`sha256-<digest-hex>.sig` as an ordinary tag) is expected to round-trip
  unmodified; this must be demonstrated, not assumed.

### Out of Scope
- **OCI 1.1 Referrers API** (`GET /v2/<name>/referrers/<digest>`) — no route, no
  subject→referrer index. cosign falls back to the tag convention on 404.
- **The `PublishManifest` / `BlobExists` bug is NOT fixed here** (explore.md:53-65,
  `service.go:237-245`): a cosign push in OCI-1.1-referrers mode, whose `subject` points at a
  manifest digest rather than a blob, is still falsely rejected as "manifest references
  missing blob" after this change. Only the tag-based convention is supported. A reader must
  not assume this bug was silently repaired.
- **Signing** — no key generation, storage, or signature production, under any scope.
- **Keyless / OIDC identity matching / Fulcio / Rekor transparency-log lookups** — entirely
  deferred; no partial support exists after this change.
- **Push-time behavior change** — pushing an unsigned image stays legal; verification is
  strictly pull-time.
- **Runtime lifecycle machinery** — no install/upgrade/rollback actions for this feature.

## Capabilities

### New Capabilities
- `image-signature-verification`: trusted-key configuration, pull-time signature verification
  gate with fail-closed posture, per-repository policy resolution, and the CI-facing
  `signature-status` endpoint.

### Modified Capabilities
- `repository-config-overrides`: override codec resolution MUST support feature payloads that
  target a settings type other than `ScanSettings`; `signing` becomes a third override feature.
- `feature-configuration`: `signing` is a registered builtin feature with configuration but no
  runtime manager; its version is informational only.
- `operator-admin-tui`: operator MUST configure global signing policy from a dedicated modal,
  see signing status as a badge, and set/clear a per-repository signing override.

## Approach

Approach 1 from explore.md — verification-only pull-time gate — chosen for the same
async-producer / sync-gate split `scan-policy-gate` already proved: signatures are produced
externally and asynchronously; regixtry only enforces at pull. The gate reads the resolved
policy (global row, replaced in full by a repository override when one exists), fetches the
signature artifact by the cosign tag convention for the resolved digest, and verifies it with
`sigstore-go` against the configured trusted public keys.

`sigstore-go` is imported rather than shelling out to `cosign verify`, deliberately breaking
from the `trivy`/`gitleaks` external-binary precedent: signature verification is stateless
cryptography over a public key, with no vulnerability-DB-style freshness lifecycle to justify
install/upgrade/rollback machinery. Its "version" is whatever module regixtry was compiled
against; upgrading it means shipping a new regixtry release through the existing
`regixtry upgrade` path. The canonical import path (`github.com/sigstore/sigstore-go`) and its
current API surface MUST be verified against live upstream documentation in `sdd-design`, not
assumed.

### Deliberate divergence: this gate is fail-closed

`scan-policy-gate` is deliberately **fail-open** on uncertainty — a missing or in-progress
scan does not block a pull, because scanning is asynchronous and its latency is not the
puller's fault. **This gate is fail-closed** and the reader must not carry the earlier posture
over. No signature, an unreadable or expired key, or any verification error blocks the pull.
The reasoning is that content trust exists precisely to catch the unsigned/unverifiable image:
"cannot verify" and "not trustworthy" are the same answer in every standard content-trust
model, whereas "not yet scanned" genuinely is not "vulnerable". The two gates therefore sit
side by side in `OpenManifest` with opposite defaults, on purpose.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `internal/ports/regixtry.go` | Modified | `SigningPolicySettings`, `SigningOverride`, store methods |
| `internal/infra/metadata/sqlite/store.go` | Modified | Signing policy row; reuses `repository_feature_overrides` |
| `internal/infra/verification/sigstore/` | New | `sigstore-go` verification adapter |
| `internal/app/regixtry/queries.go` | Modified | Gate in `OpenManifest`; `SignatureStatus` query |
| `internal/app/regixtry/service_signing.go` | New | Policy resolution + `enforceSigningPolicy` |
| `internal/app/regixtry/repository_overrides.go` | Modified | Generalized codec `Apply`; `signing` codec |
| `internal/app/regixtry/feature_registry.go` | Modified | `signing` builtin, no runtime manager |
| `internal/protocol/http/router.go`, `admin_handlers.go` | Modified | `signature-status` route; admin routes |
| `internal/tui/session.go`, `model.go`, `admin_views.go` | Modified | Signing modal, badge, override third feature |
| `go.mod` / `go.sum` | Modified | `sigstore-go` dependency |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| **Fail-closed gate blocks every pull the moment it is enabled** on a repository with no signatures yet | High | Disabled by default; TUI modal states the consequence before enabling; per-repo exemption override exists; `signature-status` lets CI check before rollout |
| `sigstore-go` drags a large transitive tree into a module with 13 lean direct deps | High | Vendor-audit the tree in `sdd-design`; verify build size/CI impact before committing to the import; the `cosign verify` subprocess remains the documented fallback |
| Generalizing `repositoryOverrideCodec.Apply` regresses shipped Trivy/gitleaks overrides | Med | Existing `repository_overrides_test.go` coverage must stay green unchanged; generalize the signature only, not the semantics |
| cosign in referrers mode hits the unfixed `BlobExists` rejection (`service.go:243`) | Med | Documented Out of Scope; integration test pins tag-convention mode explicitly and asserts the observed behavior of the other mode |
| Verification cost on every pull adds latency to the hot path | Med | Verify only when policy is enabled for that repository; caching by digest is a `sdd-design` decision |
| `signature-status` leaking key material or trust config to non-admin pull credentials | Med | Response carries verdict + digest only; asserted by test, mirroring `ScanStatusResult`'s shape |
| Two gates with opposite failure postures confuse operators | Low | Stated explicitly in spec and surfaced in TUI copy |

## Rollback Plan

`git revert` the change commits. Storage is additive (`CREATE TABLE IF NOT EXISTS` /
`INSERT`-only rows plus `feature_name="signing"` rows in the existing overrides table); a
reverted binary ignores the signing policy row, and the generalized codec map simply has no
`signing` entry, so unknown-feature rows resolve to "no override" via the existing
`repository_overrides.go:140-143` fallback. No manifest, blob, or existing-table change; any
`.sig` tags already pushed remain ordinary tags. Removing the `go.mod` dependency is part of
the same revert. Operationally, disabling the global toggle restores pre-change pull behavior
without a deploy.

## Dependencies

- `repository-scan-config-overrides` (shipped) — its `repository_feature_overrides` table and
  codec registry are reused and modified.
- `scan-policy-gate` (shipped) — read-only precedent for the gate and status endpoint; not
  modified.
- **New external dependency**: `sigstore-go` (canonical import path and API to be confirmed
  against upstream in `sdd-design`).
- External `cosign` CLI at the operator's side — not shipped, not managed by regixtry.

## Success Criteria

- [ ] `cosign sign --key cosign.key <repo>@<digest>` against a running regixtry round-trips
      with **zero push-path code changes**, proven by an integration test, not assumed.
- [ ] With signing policy disabled, pull behavior is byte-identical to today.
- [ ] With policy enabled and a valid signature by a trusted key, pull succeeds.
- [ ] With policy enabled and no signature, an unreadable/expired key, or a verification
      error, pull is blocked with 403 — each case asserted separately.
- [ ] A repository override can require signing when the global default does not, and exempt
      a repository when the global default requires it — both directions tested.
- [ ] Clearing the override reverts that repository to the global policy.
- [ ] Shipped Trivy and gitleaks override behavior is unchanged after the codec
      generalization, proven by the existing tests passing unmodified.
- [ ] `GET /v2/<repo>/manifests/<ref>/signature-status` returns a verdict with ordinary pull
      credentials, is never itself blocked by the gate, and exposes no key material.
- [ ] The signing feature exposes Enable/Disable/Configure and **no** Install/Upgrade/Rollback
      action in the admin API and TUI.

## Proposal question round — resolved

Decided with the user before this proposal; encoded above so `sdd-spec`/`sdd-design` do not
re-open them: (1) scope is Approach 1, verification-only, no Referrers API, `BlobExists` bug
explicitly deferred; (2) trust model is static public keys only, no keyless/OIDC/Rekor;
(3) granularity is global default + per-repository override on the existing overrides table,
both override directions supported, full-row-replace; (4) fail-closed, deliberately opposite
to `scan-policy-gate`; (5) `sigstore-go` imported, not an external managed binary;
(6) `FeatureKindBuiltin` with no `FeatureRuntimeManager`; (7) CI endpoint is registry-scoped
with `ActionPull`; (8) dedicated admin modal, per-repo config via the existing override modal;
(9) no push-time behavior change.

### Open questions for `sdd-design`

1. Exact field set and JSON shape of `SigningPolicySettings` / `SigningOverride` — in
   particular whether trusted keys are inline PEM or server-local file paths (the
   `TLSCACertPath` / `IgnoreFilePath` precedent favors server-local paths, and that precedent
   already defines "unreadable path fails the operation").
2. Whether verification results are cached by digest, and if so where and with what
   invalidation.
3. Whether the generalized codec uses Go generics, `any`, or a parallel typed registry.
4. `sigstore-go`'s current canonical import path, API, and transitive dependency weight —
   verify against upstream before committing to it.
5. The exact state vocabulary for `signature-status` (mirroring `ScanStatus`'s five states).

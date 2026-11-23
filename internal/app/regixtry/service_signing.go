package regixtry

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"sort"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

// Signature verification states, mirroring the vocabulary design.md
// Decision 9 defines for the (not-yet-built) signature-status endpoint.
// verifySignature returns one of these so the gate and a future status
// report can never disagree about what happened for a given digest. They
// are unexported here because Phase 7 (signature-status) is not part of
// this work unit; that phase either promotes these or defines its own
// exported constants carrying the same literal values.
const (
	signatureStateUnsigned     = "unsigned"
	signatureStateUnverifiable = "unverifiable"
	signatureStateUntrusted    = "untrusted"
	signatureStateMismatched   = "mismatched"
	signatureStateVerified     = "verified"
)

// GetSigningPolicySettings resolves the global image-signature verification
// policy with a code-level default fallback (design.md Decision 4): a fresh
// install with zero rows ever written reports {Enabled: false} — the
// inverted default vs. GetScanPolicySettings' {Enabled: true} default. A
// missed boot/migration path here must never accidentally turn the gate ON;
// {Enabled: false} is Go's zero value, so this is explicit for readability
// rather than a functional necessity.
func (s *Service) GetSigningPolicySettings(ctx context.Context) (ports.SigningPolicySettings, error) {
	settings, err := s.metadata.GetSigningPolicySettings(ctx, s.tenant(ctx))
	if domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.SigningPolicySettings{Enabled: false}, nil
	}
	return settings, err
}

// UpdateSigningPolicySettings persists the global signing policy,
// mirroring UpdateScanPolicySettings' shape.
func (s *Service) UpdateSigningPolicySettings(ctx context.Context, input ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	input.UpdatedAt = s.now()
	if err := s.metadata.UpsertSigningPolicySettings(ctx, s.tenant(ctx), input); err != nil {
		return ports.SigningPolicySettings{}, err
	}
	return input, nil
}

// enforceSigningPolicy is the pull-time content-trust gate (design.md
// Decision 6), called from OpenManifest immediately after enforceScanPolicy.
// Unlike enforceScanPolicy, which is deliberately fail-OPEN on uncertainty,
// this gate is fail-CLOSED: "cannot verify" and "not trustworthy" are the
// same answer. The two sit side by side with opposite defaults, on purpose.
//
// pushedBy is the digest's manifest.PushedBy (the UserID of whoever pushed
// it, or "" if unknown/legacy) -- needed only for the opt-in
// UnsignedSelfRead "pusher" exemption below.
func (s *Service) enforceSigningPolicy(ctx context.Context, repository, digest, pushedBy string) error {
	policy, err := s.GetSigningPolicySettings(ctx)
	if err != nil {
		return err
	}
	policy, err = s.applySigningRepositoryOverride(ctx, s.tenant(ctx), repository, policy)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return nil // the ONLY allow-without-verify path in this function
	}
	if _, _, err := s.verifySignature(ctx, repository, digest, policy); err != nil {
		// UnsignedSelfRead is an explicitly opt-in bootstrap exemption
		// (design discussion: the cosign chicken-and-egg -- cosign must GET
		// the manifest to know what to sign, but that GET is itself blocked
		// by this same fail-closed gate before the image is signed). "" and
		// "off" (and any other value, though write-time validation should
		// never let one through) fall straight through to the original verify
		// error, completely unchanged -- today's exact current behavior.
		if principal := ports.PrincipalFromContext(ctx); principal != nil {
			switch policy.UnsignedSelfRead {
			case "pusher":
				// An empty pushedBy (pre-migration legacy row) must never
				// match anyone, even a principal whose own UserID also
				// happens to be empty.
				if pushedBy != "" && principal.UserID == pushedBy {
					return nil
				}
			case "repo_push":
				// HasGrantedWriteAccess, not HasWriteAccess: the request
				// being blocked here is, by definition, a read (e.g.
				// cosign's own GET of the manifest it is about to sign),
				// so its token will essentially never itself carry push
				// scope even when the identity behind it has standing
				// push authorization. HasWriteAccess additionally requires
				// Scope.AllowsPush on THIS token and would make repo_push
				// mode unreachable for the exact scenario it exists to
				// unblock (confirmed live: cosign's read stayed DENIED
				// with repo_push configured until this was fixed).
				if principal.HasGrantedWriteAccess(repository) {
					return nil
				}
			}
		}
		return err
	}
	return nil
}

// signatureMatch carries which anchor kind (if either) a verified signature
// matched: KeyFingerprint is non-empty only when a trusted key matched
// (signing.Fingerprint of the exact trusted-key PEM), Identity is non-empty
// only when a trusted identity matched (the certificate's SAN and OIDC
// issuer, composed by composeVerifiedIdentity). The two never both carry a
// value: the identity branch is tried only after the key loop has already
// failed (design.md Data Flow). Replaces the previous bare matchedFingerprint
// string return -- design.md's "Result shape" decision explicitly rejects a
// fourth return value ("a matched identity is its own operator value, not
// squeezed into the key-fingerprint field").
type signatureMatch struct {
	KeyFingerprint string
	Identity       string
}

// verifySignature resolves and cryptographically verifies the digest's
// signature artifact against the resolved policy's trusted keys and/or
// trusted identities, returning the same (state, error) pair a future
// signature-status endpoint would report (design.md Decision 9). Every
// rejection surfaces as domain.NewPolicyViolationError (403) except a
// genuine store/blob infrastructure error, which propagates unchanged (500)
// -- a fault must never be mislabeled a missing or invalid signature
// (design.md Decision 6's truth table, last row).
// verifySignature returns (state, match, err). match is non-zero ONLY on the
// signatureStateVerified path -- every other return (unverifiable/mismatched/
// untrusted/unsigned, and every infrastructure-error path) returns a zero
// signatureMatch, never a fabricated value.
func (s *Service) verifySignature(ctx context.Context, repository, digest string, policy ports.SigningPolicySettings) (string, signatureMatch, error) {
	keys := parseTrustedKeys(policy.TrustedPublicKeys)
	// keys-OR-identities: enabling the policy (and reaching this call at
	// all) requires at least one usable anchor of EITHER kind (spec:
	// "Enabling with a key or an identity activates verification"). Only
	// when BOTH sets are empty is there nothing this pull could ever verify
	// against.
	if len(keys) == 0 && len(policy.TrustedIdentities) == 0 {
		return signatureStateUnverifiable, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: no usable trusted key or trusted identity is configured", repository, digest))
	}

	tag, err := signing.SignatureTag(digest)
	if err != nil {
		return signatureStateUnverifiable, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: %s", repository, digest, err.Error()))
	}

	repositoryRef, err := parseRepository(repository)
	if err != nil {
		return "", signatureMatch{}, err
	}

	// Signature discovery is deliberately NOT first-hit-wins: a repository
	// writer controls what sits at the legacy `.sig` tag, the bundle-index tag
	// and the referrers of a digest, so a single junk artifact must never be
	// able to deny a digest that another candidate validly signs. Both
	// discovery sources are always evaluated and the digest is accepted if ANY
	// candidate verifies under the policy; a denial is only reported when none
	// does (fail closed). Trust is unchanged: every accepted candidate passed
	// the same key/identity and claims checks as before.
	legacyFound := false
	var legacyState string
	var legacyErr error

	sigManifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, tag)
	switch {
	case err == nil:
		legacyFound = true
		var legacyMatch signatureMatch
		legacyState, legacyMatch, legacyErr = s.verifyLegacySignature(ctx, repository, digest, sigManifest.Payload, keys)
		if legacyErr == nil {
			return legacyState, legacyMatch, nil
		}
		if !domain.IsCode(legacyErr, domain.ErrorCodePolicyViolation) {
			return "", signatureMatch{}, legacyErr // infrastructure error propagates unchanged
		}
	case domain.IsCode(err, domain.ErrorCodeNotFound):
		// No legacy `.sig` tag: modern cosign (v3+, --key-based signing, and
		// keyless Fulcio/OIDC signing) never pushes one -- it pushes an OCI
		// Image Index at the suffix-less "sha256-<hex>" tag and/or a real
		// referrer artifact instead.
	default:
		return "", signatureMatch{}, err // infrastructure error propagates unchanged
	}

	bundleState, bundleMatch, bundleErr := s.verifyBundleSignature(ctx, repositoryRef, repository, digest, keys, policy.TrustedIdentities)
	if bundleErr == nil {
		return bundleState, bundleMatch, nil
	}
	if !domain.IsCode(bundleErr, domain.ErrorCodePolicyViolation) {
		return "", signatureMatch{}, bundleErr // infrastructure error propagates unchanged
	}

	if legacyFound && (bundleState == signatureStateUnsigned || legacyState == signatureStateMismatched) {
		// The bundle formats added nothing (or the legacy path saw the more
		// security-relevant failure): report the legacy outcome, exactly as
		// before bundle discovery existed.
		return legacyState, signatureMatch{}, legacyErr
	}
	return bundleState, signatureMatch{}, bundleErr
}

// Bundle candidate discovery is bounded by three independent limits so that a
// repository writer flooding one digest's referrers/bundle tags can neither
// make verification expensive nor starve a genuine signature:
//
//   - maxBundleCandidatesInspected: distinct bundle documents (by layer
//     digest) read and parsed per verification. Byte-identical bundles count
//     once. Reading is cheap and cryptography-free.
//   - maxBundleCryptoWork: signature verifications spent on candidates whose
//     signed claims bind THIS digest. Candidates whose claims bind another
//     digest (the replay of a public signature) can never verify here, so they
//     cost no cryptography in this phase; identical (payload, signature)
//     pairs inside differently wrapped bundles are verified once. Candidates
//     are tried cheapest first (static key, fewest signatures), in list order
//     otherwise, so the order never depends on an attacker-controlled value.
//   - maxSignatureCandidates: distinct claim-mismatching candidates probed
//     afterwards only to tell "mismatched" (a real trusted signature over
//     another digest) from "untrusted".
//
// Hitting the first two limits without a verified signature yields the
// distinct status unverifiable ("too many candidates"), never unsigned and
// never verified: the gate stays fail-closed. A verified result still needs a
// trusted signature that cryptographically verifies AND claims that bind this
// digest, exactly as before. The legacy `.sig` manifest has its own bound,
// signing.MaxSignatureEntries layers.
const (
	maxBundleCandidatesInspected = 1024
	maxBundleCryptoWork          = 16384
	maxSignatureCandidates       = 256
	// keylessWorkUnits is the weight of one keyless identity verification
	// (certificate chain, SCT and tlog checks) against maxBundleCryptoWork.
	keylessWorkUnits = 16
)

// maxSignaturesPerBundle bounds the distinct DSSE signature entries inspected
// inside ONE bundle document (identical entries are deduplicated first).
const maxSignaturesPerBundle = 16

// verifyLegacySignature verifies the legacy cosign `.sig` manifest payload
// against the trusted keys. Every entry is tried: an entry that does not
// verify, or that verifies but binds a different digest (a replayed
// signature), is skipped rather than terminal, so it cannot shadow a later
// valid entry. A denial is a policy violation (state mismatched when any
// replay was seen, else untrusted); a non-policy error is infrastructure.
func (s *Service) verifyLegacySignature(ctx context.Context, repository, digest string, manifestPayload []byte, keys []trustedKeyEntry) (string, signatureMatch, error) {
	entries, err := signing.ParseSignatureManifest(manifestPayload)
	if err != nil || len(entries) == 0 {
		return signatureStateUnverifiable, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: no usable signature entry found", repository, digest))
	}

	mismatched := false
	for _, entry := range entries {
		payload, err := s.openSignaturePayload(ctx, entry.PayloadDigest)
		if err != nil {
			return "", signatureMatch{}, err // infrastructure error propagates unchanged
		}
		if payload == nil {
			continue // payload blob missing or oversized; try the next entry
		}

		for _, trusted := range keys {
			if err := signing.Verify(trusted.key, payload, entry.Signature); err != nil {
				continue
			}
			if err := signing.CheckClaims(payload, digest); err != nil {
				mismatched = true // a real signature over a different digest: never accepted, but must not shadow other entries
				continue
			}
			return signatureStateVerified, signatureMatch{KeyFingerprint: signing.Fingerprint(trusted.pemText)}, nil
		}
	}

	if mismatched {
		return signatureStateMismatched, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: signature binds a different digest", repository, digest))
	}
	return signatureStateUntrusted, signatureMatch{}, domain.NewPolicyViolationError(
		fmt.Sprintf("pull of %s@%s is blocked by the signing policy: no signature validated against a trusted key", repository, digest))
}

// verifyBundleSignature is verifySignature's fallback path for modern
// cosign v3 (--key-based) signatures. It is called ONLY when the legacy
// `.sig` tag lookup above returned domain.ErrorCodeNotFound, and is
// structured to preserve that lookup's exact "genuinely unsigned" outcome
// byte-for-byte when this format is absent too: an image signed in neither
// format must behave exactly as before this fallback existed.
//
// Modern cosign pushes an OCI Image Index tagged signing.BundleIndexTag(digest)
// (the same hex, no ".sig" suffix) whose manifests[] are referrer entries
// with artifactType signing.SigstoreBundleMediaType. Each referrer manifest
// carries subject.digest pointing back at the signed image and one layer of
// that same media type -- a Sigstore Bundle document holding a DSSE
// envelope. This loop resolves every candidate referrer, filters to the one
// (if any) whose subject actually binds this digest, then verifies its DSSE
// signature exactly like the legacy loop verifies a SimpleSigning payload:
// PAE-encode, try every signature against every trusted key via the same
// signing.Verify, and bind claims via signing.CheckBundleClaims.
// verifyBundleSignature mirrors verifySignature's (state, match, err)
// contract: match is non-zero only on the signatureStateVerified path.
//
// identities is the resolved policy's TrustedIdentities: after the key loop
// below fails to match a candidate bundle document against any trusted key,
// and only if the candidate's own verificationMaterial actually carries a
// certificate (signing.ParseBundleVerificationMaterial's HasCertificate --
// cheap to check, avoids a wasted signing.VerifyKeyless call on an ordinary
// static-key bundle document), this tries the keyless (Fulcio/OIDC) identity
// path via signing.VerifyKeyless(bundlePayload, identities, digest) -- the
// SAME raw bundle document bytes the key loop already parsed via
// signing.ParseBundleDocument, since VerifyKeyless does its own bundle
// parsing internally (design.md "Bundle handoff" decision: raw bytes in,
// zero sigstore-go types out). This is the ONLY place the identity anchor is
// ever tried (spec: "Legacy Static-Key Verification Path Is Unchanged" --
// the legacy SimpleSigning `.sig` loop in verifySignature above never calls
// this).
func (s *Service) verifyBundleSignature(ctx context.Context, repositoryRef domain.RepositoryRef, repository, digest string, keys []trustedKeyEntry, identities []ports.TrustedIdentity) (string, signatureMatch, error) {
	indexTag, err := signing.BundleIndexTag(digest)
	if err != nil {
		return signatureStateUnverifiable, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: %s", repository, digest, err.Error()))
	}

	// foundIndexCandidateSource tracks whether the legacy bundle-index tag
	// itself resolved (regardless of how many, if any, of its entries turn
	// out usable) -- needed below to preserve the exact existing
	// unsigned-vs-untrusted distinction: an index that resolved but whose
	// entries never verified has always reported "untrusted", never
	// "unsigned" (today's unchanged behavior). Only when NEITHER discovery
	// source -- this legacy tag-index lookup NOR the real Referrers API
	// fallback below -- finds any candidate at all is this digest genuinely
	// unsigned.
	foundIndexCandidateSource := false
	indexUnusable := false
	mismatchSeen := false
	var candidates [][]byte

	indexManifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, indexTag)
	switch {
	case err == nil:
		foundIndexCandidateSource = true

		entries, parseErr := signing.ParseBundleIndex(indexManifest.Payload)
		if parseErr != nil {
			// A malformed index (writer-controlled) must not shadow genuine
			// referrers discovered through the Referrers API below.
			indexUnusable = true
			entries = nil
		}

		for _, entry := range entries {
			referrerManifest, resolveErr := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, entry.Digest)
			if resolveErr != nil {
				if domain.IsCode(resolveErr, domain.ErrorCodeNotFound) {
					continue // a listed referrer that no longer resolves; try the next candidate
				}
				return "", signatureMatch{}, resolveErr // infrastructure error propagates unchanged
			}

			candidates = append(candidates, referrerManifest.Payload)
		}
	case domain.IsCode(err, domain.ErrorCodeNotFound):
		// No legacy bundle-index tag: fall through and try the real OCI 1.1
		// Referrers API below instead of immediately reporting unsigned --
		// modern cosign (confirmed live via keyless-signing-smoke.yml) pushes
		// the signature as a genuine referrer artifact and writes no index
		// tag at all.
	default:
		return "", signatureMatch{}, err // infrastructure error propagates unchanged
	}

	// subjectDigest is unreachable-error-safe: digest already passed the
	// identical "sha256:<hex>" validation inside BundleIndexTag above.
	subjectDigest, err := domain.ParseDigest(digest)
	if err != nil {
		return "", signatureMatch{}, err
	}

	referrerRows, err := s.metadata.ListReferrers(ctx, s.tenant(ctx), repositoryRef, subjectDigest)
	if err != nil {
		return "", signatureMatch{}, err // infrastructure error propagates unchanged
	}

	for _, row := range referrerRows {
		candidates = append(candidates, row.Payload)
	}

	state, match, evalErr := s.evaluateBundleCandidates(ctx, digest, candidates, keys, identities)
	if evalErr != nil {
		return "", signatureMatch{}, evalErr // infrastructure error propagates unchanged
	}
	switch state {
	case signatureStateVerified:
		return state, match, nil
	case signatureStateMismatched:
		mismatchSeen = true
	case signatureStateUnverifiable:
		return signatureStateUnverifiable, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: unverifiable: too many candidates", repository, digest))
	}

	if !foundIndexCandidateSource && len(referrerRows) == 0 {
		// Neither the legacy `.sig` tag (verifySignature, above this call),
		// the legacy bundle-index tag, nor the real Referrers API found
		// anything at all: this digest has no signature artifact in any
		// known format. Today's exact existing unsigned outcome, unchanged.
		return signatureStateUnsigned, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: no signature found", repository, digest))
	}

	if mismatchSeen {
		return signatureStateMismatched, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: bundle signature binds a different digest", repository, digest))
	}
	if indexUnusable && len(referrerRows) == 0 {
		return signatureStateUnverifiable, signatureMatch{}, domain.NewPolicyViolationError(
			fmt.Sprintf("pull of %s@%s is blocked by the signing policy: no usable bundle signature entry found", repository, digest))
	}

	return signatureStateUntrusted, signatureMatch{}, domain.NewPolicyViolationError(
		fmt.Sprintf("pull of %s@%s is blocked by the signing policy: no bundle signature validated against a trusted key or trusted identity", repository, digest))
}

// signingWorkProbeKey carries an optional observer of the verification work
// (one call per unit of cryptographic work) so tests can assert the bound.
type signingWorkProbeKey struct{}

func recordSigningWork(ctx context.Context, units int) {
	if probe, ok := ctx.Value(signingWorkProbeKey{}).(func(int)); ok {
		probe(units)
	}
}

// bundleCandidate is one parsed bundle document whose referrer subject is the
// digest being verified.
type bundleCandidate struct {
	bundlePayload []byte
	payload       []byte
	pae           []byte
	paeKey        string
	signatures    []string // distinct, at most maxSignaturesPerBundle
	keylessShaped bool
}

// loadBundleCandidate reads and parses one bundle layer. It returns nil for a
// candidate that is not usable (missing/oversized blob, malformed document).
func (s *Service) loadBundleCandidate(ctx context.Context, layerDigest string, identities []ports.TrustedIdentity) (*bundleCandidate, error) {
	bundlePayload, err := s.openSignaturePayload(ctx, layerDigest)
	if err != nil {
		return nil, err // infrastructure error propagates unchanged
	}
	if bundlePayload == nil {
		return nil, nil
	}
	bundle, err := signing.ParseBundleDocument(bundlePayload)
	if err != nil {
		return nil, nil
	}
	payload, err := base64.StdEncoding.DecodeString(bundle.Payload)
	if err != nil {
		return nil, nil
	}
	pae := signing.PAE(bundle.PayloadType, payload)
	paeSum := sha256.Sum256(pae)

	// Identical signature strings are inspected once and at most
	// maxSignaturesPerBundle distinct entries are inspected: a
	// writer-controlled bundle must not multiply the crypto work. Entries
	// beyond that are ignored (fail closed for a genuine signature after them).
	seen := make(map[string]struct{}, maxSignaturesPerBundle)
	signatures := make([]string, 0, len(bundle.Signatures))
	for _, signatureB64 := range bundle.Signatures {
		if _, dup := seen[signatureB64]; dup {
			continue
		}
		if len(seen) >= maxSignaturesPerBundle {
			break
		}
		seen[signatureB64] = struct{}{}
		signatures = append(signatures, signatureB64)
	}

	keylessShaped := false
	if len(identities) > 0 {
		if vm, vmErr := signing.ParseBundleVerificationMaterial(bundlePayload); vmErr == nil && vm.HasCertificate {
			keylessShaped = true
		}
	}
	return &bundleCandidate{
		bundlePayload: bundlePayload,
		payload:       payload,
		pae:           pae,
		paeKey:        string(paeSum[:]),
		signatures:    signatures,
		keylessShaped: keylessShaped,
	}, nil
}

// verifyKeys tries every distinct signature of the candidate against every
// trusted key. failed memoizes (payload, signature) pairs already known not to
// verify, so differently wrapped copies of one signature cost nothing. work is
// decremented per verification; ok=false with out-of-work reports exhaustion.
func verifyCandidateKeys(ctx context.Context, cand *bundleCandidate, keys []trustedKeyEntry, failed map[string]struct{}, work *int) (matched *trustedKeyEntry, exhausted bool) {
	for _, signatureB64 := range cand.signatures {
		pairKey := cand.paeKey + signatureB64
		if _, known := failed[pairKey]; known {
			continue
		}
		for i := range keys {
			if *work < 1 {
				return nil, true
			}
			*work--
			recordSigningWork(ctx, 1)
			if err := signing.Verify(keys[i].key, cand.pae, signatureB64); err == nil {
				return &keys[i], false
			}
		}
		failed[pairKey] = struct{}{}
	}
	return nil, false
}

// evaluateBundleCandidates verifies the discovered referrer manifests of one
// digest. It returns signatureStateVerified (accepted),
// signatureStateMismatched (a trusted signature over another digest was
// confirmed and nothing verified), signatureStateUnverifiable (a work bound was
// hit before anything verified: too many candidates), or "" (nothing usable
// verified). A non-nil err is an infrastructure error that propagates
// unchanged. See maxBundleCandidatesInspected for the strategy.
func (s *Service) evaluateBundleCandidates(ctx context.Context, digest string, manifests [][]byte, keys []trustedKeyEntry, identities []ports.TrustedIdentity) (string, signatureMatch, error) {
	var matching, mismatching []*bundleCandidate
	seenLayers := make(map[string]struct{})
	exhausted := false

	for _, manifestPayload := range manifests {
		subjectDigest, layerDigest, parseErr := signing.ParseBundleReferrerManifest(manifestPayload)
		if parseErr != nil || subjectDigest == "" || layerDigest == "" {
			continue // not a usable bundle referrer manifest
		}
		if subjectDigest != digest {
			continue // a real signature, just not for THIS digest -- the filter must actually filter
		}
		if _, dup := seenLayers[layerDigest]; dup {
			continue // byte-identical bundle content counts once
		}
		if len(seenLayers) >= maxBundleCandidatesInspected {
			exhausted = true
			break
		}
		seenLayers[layerDigest] = struct{}{}

		cand, err := s.loadBundleCandidate(ctx, layerDigest, identities)
		if err != nil {
			return "", signatureMatch{}, err
		}
		if cand == nil {
			continue
		}
		if signing.CheckBundleClaims(cand.payload, digest) != nil {
			// The signed claims bind another digest: this candidate can never
			// verify here, so it spends no cryptography unless a mismatch has
			// to be confirmed below.
			mismatching = append(mismatching, cand)
			continue
		}
		matching = append(matching, cand)
	}

	// Cheapest first: static-key bundles before keyless ones, fewer
	// signatures first; stable, so ties keep the deterministic list order.
	sort.SliceStable(matching, func(i, j int) bool {
		if matching[i].keylessShaped != matching[j].keylessShaped {
			return !matching[i].keylessShaped
		}
		return len(matching[i].signatures) < len(matching[j].signatures)
	})

	work := maxBundleCryptoWork
	failed := make(map[string]struct{})
	for _, cand := range matching {
		trusted, out := verifyCandidateKeys(ctx, cand, keys, failed, &work)
		if trusted != nil {
			return signatureStateVerified, signatureMatch{KeyFingerprint: signing.Fingerprint(trusted.pemText)}, nil
		}
		if out {
			exhausted = true
			break
		}
		if !cand.keylessShaped {
			continue
		}
		// The key loop never matched this candidate; try the identity anchor
		// (OR semantics, never combined).
		if work < keylessWorkUnits {
			exhausted = true
			break
		}
		work -= keylessWorkUnits
		recordSigningWork(ctx, keylessWorkUnits)
		matchedSAN, keylessErr := signing.VerifyKeyless(cand.bundlePayload, toSigningIdentities(identities), digest)
		if keylessErr == nil {
			return signatureStateVerified, signatureMatch{Identity: composeVerifiedIdentity(matchedSAN, identities)}, nil
		}
	}
	if exhausted {
		return signatureStateUnverifiable, signatureMatch{}, nil
	}

	// Nothing verified. Confirm (bounded) whether a trusted key really signed
	// a claim for another digest: that is "mismatched" rather than "untrusted".
	probeWork := maxSignatureCandidates * len(keys)
	for i, cand := range mismatching {
		if i >= maxSignatureCandidates {
			break
		}
		if trusted, out := verifyCandidateKeys(ctx, cand, keys, failed, &probeWork); trusted != nil {
			return signatureStateMismatched, signatureMatch{}, nil
		} else if out {
			break
		}
	}
	return "", signatureMatch{}, nil
}

// toSigningIdentities maps the ports-layer trusted identities to
// signing.TrustedIdentity, the shape keyless.go's VerifyKeyless accepts --
// mirrors parseTrustedKeys' role for TrustedPublicKeys, minus any parsing
// step: unlike a PEM key, an identity has nothing to fail to parse here (its
// regexp was already validated at write time, normalizeSigningOverride).
func toSigningIdentities(identities []ports.TrustedIdentity) []signing.TrustedIdentity {
	converted := make([]signing.TrustedIdentity, 0, len(identities))
	for _, identity := range identities {
		converted = append(converted, signing.TrustedIdentity{
			CertificateIdentityRegexp: identity.CertificateIdentityRegexp,
			CertificateOIDCIssuer:     identity.CertificateOIDCIssuer,
		})
	}
	return converted
}

// composeVerifiedIdentity reports the matched certificate SAN together with
// the OIDC issuer of whichever configured TrustedIdentity's regexp actually
// matched it (spec: "Identity match reports SAN and issuer separately" --
// the operator-facing value must carry both, never just the bare SAN
// keyless.VerifyKeyless itself returns). A pure function: no I/O, no
// sigstore-go type crosses it. Falls back to the bare SAN if -- defensively,
// should be unreachable -- no configured entry's regexp matches, since
// VerifyKeyless only ever returns a match after confirming some configured
// identity's regexp+issuer pair matched.
func composeVerifiedIdentity(matchedSAN string, identities []ports.TrustedIdentity) string {
	for _, identity := range identities {
		matcher, err := regexp.Compile(identity.CertificateIdentityRegexp)
		if err != nil {
			continue // an unparseable regexp cannot have been the one that matched
		}
		if matcher.MatchString(matchedSAN) {
			return matchedSAN + " (" + identity.CertificateOIDCIssuer + ")"
		}
	}
	return matchedSAN
}

// openSignaturePayload reads one signature entry's payload blob, bounded by
// signing.MaxPayloadBytes (design.md Decision 8's hot-path bound). A missing
// blob (domain.ErrorCodeNotFound) or an oversized blob is reported as
// (nil, nil) — "this entry cannot be verified", not a hard error — so a
// pusher-controlled entry can never turn a genuine store/blob fault into a
// silent allow, nor a missing/oversized blob into a 500. A true
// infrastructure error still propagates unchanged.
func (s *Service) openSignaturePayload(ctx context.Context, payloadDigestValue string) ([]byte, error) {
	payloadDigest, err := domain.ParseDigest(payloadDigestValue)
	if err != nil {
		return nil, nil
	}

	reader, _, err := s.blobs.OpenBlob(ctx, payloadDigest)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer reader.Close()

	payload, err := io.ReadAll(io.LimitReader(reader, signing.MaxPayloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > signing.MaxPayloadBytes {
		return nil, nil
	}

	return payload, nil
}

// trustedKeyEntry pairs one configured trusted-key PEM with its parsed ECDSA
// public key, preserving the PEM-to-parsed-key association that
// parseTrustedKeys' plain []*ecdsa.PublicKey return previously discarded.
// verifySignature/verifyBundleSignature need this pairing to report WHICH
// original PEM matched a verified signature, via signing.Fingerprint(pemText).
type trustedKeyEntry struct {
	pemText string
	key     *ecdsa.PublicKey
}

// parseTrustedKeys parses every configured trusted key, silently skipping
// any entry that fails to parse. An unreadable/malformed key can reach this
// point only via a hand-seeded store row (the admin write path rejects it at
// normalizeSigningOverride/UpdateSigningPolicySettings time); skipping it
// here keeps the gate fail-closed on the remaining, still-unusable set
// rather than erroring in a way that could be mistaken for a verification
// outcome.
func parseTrustedKeys(pemKeys []string) []trustedKeyEntry {
	keys := make([]trustedKeyEntry, 0, len(pemKeys))
	for _, pemText := range pemKeys {
		key, err := signing.ParseTrustedKey(pemText)
		if err != nil {
			continue
		}
		keys = append(keys, trustedKeyEntry{pemText: pemText, key: key})
	}
	return keys
}

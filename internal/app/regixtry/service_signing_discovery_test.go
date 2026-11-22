package regixtry

import (
	"context"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
)

// The tests in this file pin the writer-shadowing invariant of signature
// discovery: a repository writer controls the legacy `.sig` tag, the
// bundle-index tag and the referrers of a digest, so planting junk at any of
// them must never deny a digest that another candidate validly signs, while a
// digest with NO candidate that verifies under the policy stays denied.

// seedJunkLegacySignatureTag publishes a writer-controlled legacy `.sig` tag
// that cannot be parsed into any signature entry.
func seedJunkLegacySignatureTag(t *testing.T, service *Service, repository, digest string) {
	t.Helper()

	tag, err := signing.SignatureTag(digest)
	if err != nil {
		t.Fatalf("signing.SignatureTag(%q) error = %v", digest, err)
	}
	if _, err := service.PublishManifest(context.Background(), repository, tag, "application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2,"layers":[]}`)); err != nil {
		t.Fatalf("PublishManifest(junk .sig at %s) error = %v", tag, err)
	}
	service.WaitForBackgroundWork()
}

func verifyForTest(t *testing.T, service *Service, repository, digest string, keys []string) (string, signatureMatch, error) {
	t.Helper()
	return service.verifySignature(context.Background(), repository, digest, signingPolicyForTest(t, true, keys))
}

func requirePolicyDenial(t *testing.T, state string, err error, wantState string) {
	t.Helper()
	if err == nil {
		t.Fatalf("verifySignature() error = nil, want a policy violation (state %q)", wantState)
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != wantState {
		t.Fatalf("verifySignature() state = %q, want %q", state, wantState)
	}
}

func TestServiceVerifySignature_JunkLegacyTagDoesNotShadowValidReferrerSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - junk legacy tag")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)
	seedJunkLegacySignatureTag(t, service, repository, digest)

	state, match, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (a valid referrer signature must not be shadowed by a junk .sig tag)", err)
	}
	if state != signatureStateVerified || match.KeyFingerprint == "" {
		t.Fatalf("verifySignature() = (%q, %+v), want verified with the matching key", state, match)
	}
}

func TestServiceVerifySignature_JunkLegacyTagDoesNotShadowValidBundleIndexSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - junk legacy tag, index signature")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedBundleSignatureArtifact(t, service, repository, digest, digest, bareHex(digest), key)
	seedJunkLegacySignatureTag(t, service, repository, digest)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
	}
}

func TestServiceVerifySignature_MalformedBundleIndexDoesNotShadowValidReferrerSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - poisoned index tag")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)
	seedMalformedBundleIndexManifest(t, service, repository, digest)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
	}
}

func TestServiceVerifySignature_ReplayedSignatureReferrersDoNotShadowValidReferrerSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - replayed referrers")
	other := seedArbitraryImageManifest(t, service, repository, " - replayed referrers, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	// A writer replays genuine signatures of ANOTHER image as referrers of
	// this digest. ECDSA signatures are randomized, so every poison referrer
	// has a distinct digest and some sort before the genuine one.
	for i := 0; i < 8; i++ {
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(other), key)
	}
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
	}
}

func TestServiceVerifySignature_UntrustedAndWrongSubjectReferrersDoNotShadowValidReferrerSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - noise referrers")
	other := seedArbitraryImageManifest(t, service, repository, " - noise referrers, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	attacker, _ := generateTestECDSAP256KeyPair(t)

	for i := 0; i < 8; i++ {
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), attacker) // untrusted key
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, other, bareHex(other), key)        // other subject
	}
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
	}
}

func TestServiceVerifySignature_TransplantedLegacySignatureDoesNotShadowValidReferrerSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - transplanted legacy sig")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	// The fixture's genuine signature (trusted below) is replayed at this
	// digest's legacy tag: it verifies cryptographically but binds another
	// digest, which used to be a terminal "mismatched" denial.
	publishFixtureSignatureManifestAt(t, service, repository, digest)
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{fixtureTrustedKeyPEM(t), keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
	}
}

func TestServiceVerifySignature_TransplantedLegacySignatureAloneStaysMismatched(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - transplant only")
	publishFixtureSignatureManifestAt(t, service, repository, digest)
	seedJunkLegacySignatureTag(t, service, repository, "sha256:"+bareHex(fixtureImageDigest)) // unrelated digest's tag

	state, _, err := verifyForTest(t, service, repository, digest, []string{fixtureTrustedKeyPEM(t)})
	requirePolicyDenial(t, state, err, signatureStateMismatched)
}

func TestServiceVerifySignature_OnlyJunkCandidatesStaysDenied(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - junk only")
	other := seedArbitraryImageManifest(t, service, repository, " - junk only, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	attacker, _ := generateTestECDSAP256KeyPair(t)

	seedJunkLegacySignatureTag(t, service, repository, digest)
	seedMalformedBundleIndexManifest(t, service, repository, digest)
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), attacker) // untrusted key
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, other, bareHex(other), key)        // genuine, other subject

	state, match, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	requirePolicyDenial(t, state, err, signatureStateUntrusted)
	if match != (signatureMatch{}) {
		t.Fatalf("verifySignature() match = %+v, want zero", match)
	}
}

func TestServiceVerifySignature_ReplayedReferrerAloneStaysMismatched(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - replayed referrer only")
	other := seedArbitraryImageManifest(t, service, repository, " - replayed referrer only, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedJunkLegacySignatureTag(t, service, repository, digest)
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(other), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	requirePolicyDenial(t, state, err, signatureStateMismatched)
}

func TestServiceVerifySignature_ManyWellFormedJunkReferrersStaysDeniedAndBounded(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - candidate cap")
	_, keyPEM := generateTestECDSAP256KeyPair(t)
	attacker, _ := generateTestECDSAP256KeyPair(t)

	// More well-formed referrers for this digest than the candidate cap.
	for i := 0; i < maxSignatureCandidates+8; i++ {
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), attacker)
	}

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	requirePolicyDenial(t, state, err, signatureStateUntrusted)
}

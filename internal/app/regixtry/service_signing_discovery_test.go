package regixtry

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestServiceVerifySignature_FloodOfUntrustedAndOtherSubjectReferrersDoesNotShadowValidSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - flood")
	other := seedArbitraryImageManifest(t, service, repository, " - flood, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	attacker, _ := generateTestECDSAP256KeyPair(t)

	for i := 0; i < maxSignatureCandidates+44; i++ {
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), attacker) // untrusted signer
	}
	for i := 0; i < 20; i++ {
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, other, bareHex(other), attacker) // other subject
	}
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified despite %d junk referrers", state, err, maxSignatureCandidates+44)
	}
}

func TestServiceVerifySignature_CapFailsClosedWhenManyTrustedSignedCandidatesNeverVerify(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - cap fail closed")
	other := seedArbitraryImageManifest(t, service, repository, " - cap fail closed, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	// Every candidate passes the trusted-signer match but binds another digest.
	for i := 0; i < maxSignatureCandidates+1; i++ {
		seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(other), key)
	}

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	requirePolicyDenial(t, state, err, signatureStateMismatched)
}

func TestServiceVerifySignature_OversizedBundleBlobIsSkippedNotRead(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - oversized blob")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	huge := make([]byte, signing.MaxPayloadBytes+4096)
	for i := range huge {
		huge[i] = 'x'
	}
	uploadBlobForTest(t, service, repository, huge)
	emptyConfig := []byte("{}")
	uploadBlobForTest(t, service, repository, emptyConfig)
	referrer, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.empty.v1+json", "digest": digestForTest(emptyConfig), "size": len(emptyConfig)},
		"layers":        []map[string]any{{"mediaType": signing.SigstoreBundleMediaType, "digest": digestForTest(huge), "size": len(huge)}},
		"subject":       map[string]any{"mediaType": "application/vnd.docker.distribution.manifest.v2+json", "digest": digest, "size": 949},
		"artifactType":  signing.SigstoreBundleMediaType,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if _, err := service.PublishManifest(context.Background(), repository, digestForTest(referrer), "application/vnd.oci.image.manifest.v1+json", referrer); err != nil {
		t.Fatalf("PublishManifest(oversized referrer) error = %v", err)
	}
	service.WaitForBackgroundWork()
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified (oversized blob skipped)", state, err)
	}
}

// seedReferrerWithBundleDoc publishes a referrer for subject whose single
// bundle layer is the given raw bundle document.
func seedReferrerWithBundleDoc(t *testing.T, service *Service, repository, subject string, bundleDoc []byte) {
	t.Helper()

	uploadBlobForTest(t, service, repository, bundleDoc)
	emptyConfig := []byte("{}")
	uploadBlobForTest(t, service, repository, emptyConfig)
	referrer, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.empty.v1+json", "digest": digestForTest(emptyConfig), "size": len(emptyConfig)},
		"layers":        []map[string]any{{"mediaType": signing.SigstoreBundleMediaType, "digest": digestForTest(bundleDoc), "size": len(bundleDoc)}},
		"subject":       map[string]any{"mediaType": "application/vnd.docker.distribution.manifest.v2+json", "digest": subject, "size": 949},
		"artifactType":  signing.SigstoreBundleMediaType,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if _, err := service.PublishManifest(context.Background(), repository, digestForTest(referrer), "application/vnd.oci.image.manifest.v1+json", referrer); err != nil {
		t.Fatalf("PublishManifest(referrer) error = %v", err)
	}
	service.WaitForBackgroundWork()
}

// bundleDocWithSignatures builds a bundle document over the in-toto statement
// binding statementHex with the given raw signature list.
func bundleDocWithSignatures(t *testing.T, statementHex string, signatures []string) []byte {
	t.Helper()

	entries := make([]map[string]string, 0, len(signatures))
	for _, sig := range signatures {
		entries = append(entries, map[string]string{"sig": sig})
	}
	raw, err := json.Marshal(map[string]any{
		"mediaType": signing.SigstoreBundleMediaType,
		"dsseEnvelope": map[string]any{
			"payload":     base64.StdEncoding.EncodeToString(bundleInTotoStatementPayload(t, statementHex)),
			"payloadType": "application/vnd.in-toto+json",
			"signatures":  entries,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal(bundle) error = %v", err)
	}
	return raw
}

func TestServiceVerifySignature_RepeatedReplayedSignatureInOneBundleSpendsTheCapOnce(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - repeated signature")
	other := seedArbitraryImageManifest(t, service, repository, " - repeated signature, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	// Several bundles each repeating one genuine replayed trusted-key
	// signature (bound to ANOTHER digest) 1000 times; some sort before the
	// genuine referrer. Each used to burn the whole cap on its own.
	for i := 0; i < 6; i++ {
		replayed := signBundlePayload(t, key, "application/vnd.in-toto+json", bundleInTotoStatementPayload(t, bareHex(other)))
		sigs := make([]string, 1000)
		for j := range sigs {
			sigs[j] = replayed
		}
		seedReferrerWithBundleDoc(t, service, repository, digest, bundleDocWithSignatures(t, bareHex(other), sigs))
	}
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
	}
}

func TestServiceVerifySignature_BundleSignatureEntryLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		junkFirst int
		wantOK    bool
	}{
		{name: "valid signature as the last allowed distinct entry is accepted", junkFirst: maxSignaturesPerBundle - 1, wantOK: true},
		{name: "valid signature beyond the entry limit is ignored and denied", junkFirst: maxSignaturesPerBundle, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			repository := "library/alpine"
			digest := seedArbitraryImageManifest(t, service, repository, " - entry limit "+tc.name)
			key, keyPEM := generateTestECDSAP256KeyPair(t)

			sigs := make([]string, 0, tc.junkFirst+1)
			for i := 0; i < tc.junkFirst; i++ {
				sigs = append(sigs, base64.StdEncoding.EncodeToString([]byte("junk-signature-"+string(rune('a'+i)))))
			}
			sigs = append(sigs, signBundlePayload(t, key, "application/vnd.in-toto+json", bundleInTotoStatementPayload(t, bareHex(digest))))
			seedReferrerWithBundleDoc(t, service, repository, digest, bundleDocWithSignatures(t, bareHex(digest), sigs))

			state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
			if tc.wantOK {
				if err != nil || state != signatureStateVerified {
					t.Fatalf("verifySignature() = (%q, %v), want verified", state, err)
				}
				return
			}
			requirePolicyDenial(t, state, err, signatureStateUntrusted)
		})
	}
}

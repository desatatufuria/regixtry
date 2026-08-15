package regixtry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

// signingPolicyForTest builds a ports.SigningPolicySettings value for tests
// that call service.verifySignature directly (bypassing
// GetSigningPolicySettings/repository overrides, which are already covered
// by the enforceSigningPolicy-level tests in service_signing_test.go).
func signingPolicyForTest(t *testing.T, enabled bool, keys []string) ports.SigningPolicySettings {
	t.Helper()
	return ports.SigningPolicySettings{Enabled: enabled, TrustedPublicKeys: keys}
}

// generateTestECDSAP256KeyPair mirrors generateTestECDSAP256PublicKeyPEM
// (repository_overrides_test.go) but also returns the private key: unlike
// the legacy `.sig` fixtures under internal/domain/signing/testdata, there
// is no throwaway cosign-bundle generator artifact to reuse here, so each
// bundle-format service test mints its own fresh key pair and signs inline.
func generateTestECDSAP256KeyPair(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey() error = %v", err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	return key, pemText
}

// bundleInTotoStatementPayload builds the minimal in-toto Statement JSON
// signing.CheckBundleClaims reads, binding subjectDigestHex (bare hex, no
// "sha256:" prefix, mirroring the real captured production fixture's shape
// documented in internal/domain/signing/testdata/README.md).
func bundleInTotoStatementPayload(t *testing.T, subjectDigestHex string) []byte {
	t.Helper()

	statement := map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []map[string]any{
			{
				"digest":      map[string]string{"sha256": subjectDigestHex},
				"annotations": map[string]any{},
			},
		},
		"predicateType": "https://sigstore.dev/cosign/sign/v1",
		"predicate":     map[string]any{},
	}
	raw, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("json.Marshal(in-toto statement) error = %v", err)
	}
	return raw
}

// signBundlePayload signs payload's DSSE PAE encoding with key, exactly
// mirroring what signing.Verify checks on the other side: sha256(PAE) via
// ecdsa.VerifyASN1.
func signBundlePayload(t *testing.T, key *ecdsa.PrivateKey, payloadType string, payload []byte) string {
	t.Helper()

	paeBytes := signing.PAE(payloadType, payload)
	hash := sha256.Sum256(paeBytes)
	sig, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatalf("ecdsa.SignASN1() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// buildBundleDocument builds a Sigstore Bundle document JSON whose single
// dsseEnvelope carries payload (the raw, not-yet-base64 in-toto statement
// bytes) signed by key.
func buildBundleDocument(t *testing.T, key *ecdsa.PrivateKey, payload []byte) []byte {
	t.Helper()

	const payloadType = "application/vnd.in-toto+json"
	signatureB64 := signBundlePayload(t, key, payloadType, payload)

	doc := map[string]any{
		"mediaType": signing.SigstoreBundleMediaType,
		"dsseEnvelope": map[string]any{
			"payload":     base64.StdEncoding.EncodeToString(payload),
			"payloadType": payloadType,
			"signatures":  []map[string]string{{"sig": signatureB64}},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal(bundle document) error = %v", err)
	}
	return raw
}

// seedBundleSignatureArtifact publishes a full modern cosign v3 bundle-
// format signature artifact chain in repository: the OCI Image Index at
// signing.BundleIndexTag(indexDigest), a referrer manifest whose
// subject.digest is subjectDigest, and the bundle document blob itself
// (a DSSE envelope over an in-toto statement binding statementDigestHex,
// signed by key). indexDigest, subjectDigest, and statementDigestHex are
// deliberately independent parameters -- tests below vary each one alone to
// exercise the subject-mismatch and tampered-claims cases without touching
// the others.
func seedBundleSignatureArtifact(t *testing.T, service *Service, repository string, indexDigest string, subjectDigest string, statementDigestHex string, key *ecdsa.PrivateKey) {
	t.Helper()

	ctx := context.Background()

	payload := bundleInTotoStatementPayload(t, statementDigestHex)
	bundleDoc := buildBundleDocument(t, key, payload)
	uploadBlobForTest(t, service, repository, bundleDoc)

	emptyConfig := []byte("{}")
	uploadBlobForTest(t, service, repository, emptyConfig)

	referrerManifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.empty.v1+json",
			"digest":    digestForTest(emptyConfig),
			"size":      len(emptyConfig),
		},
		"layers": []map[string]any{
			{
				"mediaType": signing.SigstoreBundleMediaType,
				"digest":    digestForTest(bundleDoc),
				"size":      len(bundleDoc),
			},
		},
		"subject": map[string]any{
			"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
			"digest":    subjectDigest,
			"size":      949,
		},
		"artifactType": signing.SigstoreBundleMediaType,
	}
	referrerBytes, err := json.Marshal(referrerManifest)
	if err != nil {
		t.Fatalf("json.Marshal(referrer manifest) error = %v", err)
	}
	referrerDigest := digestForTest(referrerBytes)

	if _, err := service.PublishManifest(ctx, repository, referrerDigest, "application/vnd.oci.image.manifest.v1+json", referrerBytes); err != nil {
		t.Fatalf("PublishManifest(referrer manifest) error = %v", err)
	}

	index := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests": []map[string]any{
			{
				"mediaType":    "application/vnd.oci.image.manifest.v1+json",
				"size":         len(referrerBytes),
				"digest":       referrerDigest,
				"artifactType": signing.SigstoreBundleMediaType,
			},
		},
	}
	indexBytes, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("json.Marshal(bundle index) error = %v", err)
	}

	tag, err := signing.BundleIndexTag(indexDigest)
	if err != nil {
		t.Fatalf("signing.BundleIndexTag(%q) error = %v", indexDigest, err)
	}
	if _, err := service.PublishManifest(ctx, repository, tag, "application/vnd.oci.image.index.v1+json", indexBytes); err != nil {
		t.Fatalf("PublishManifest(bundle index at %s) error = %v", tag, err)
	}
	service.WaitForBackgroundWork()
}

// bareHex strips digest's "sha256:" prefix, mirroring how CheckBundleClaims
// compares against an in-toto statement's bare-hex subject digest.
func bareHex(digest string) string {
	const prefix = "sha256:"
	return digest[len(prefix):]
}

// TestServiceVerifySignature_BundleFormatVerifiedSignatureAllowsPull is the
// modern-cosign-v3 happy path: an OCI Image Index (no ".sig" suffix)
// wrapping a referrer whose subject matches the target digest and whose DSSE
// signature verifies against a trusted key -> verified, pull allowed. This
// is the exact production scenario the work-unit description reports as
// currently broken (team/az-deploy-demo).
func TestServiceVerifySignature_BundleFormatVerifiedSignatureAllowsPull(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - bundle verified")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedBundleSignatureArtifact(t, service, repository, imageDigest, imageDigest, bareHex(imageDigest), key)

	policy := signingPolicyForTest(t, true, []string{keyPEM})

	state, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (the bundle-format signature must verify)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
}

// TestServiceVerifySignature_NeitherLegacyNorBundleSignaturePresentIsUnsigned
// is the default-safety regression: a digest with no signature artifact in
// EITHER format must report exactly today's pre-change "unsigned" outcome,
// byte-for-byte -- an unrelated unsigned image's error text must not change.
func TestServiceVerifySignature_NeitherLegacyNorBundleSignaturePresentIsUnsigned(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - no signature at all")
	_, keyPEM := generateTestECDSAP256KeyPair(t)
	policy := signingPolicyForTest(t, true, []string{keyPEM})

	state, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if state != signatureStateUnsigned {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateUnsigned)
	}
	wantMessage := "POLICY_VIOLATION: pull of " + repository + "@" + imageDigest + " is blocked by the signing policy: no signature found"
	if err.Error() != wantMessage {
		t.Fatalf("verifySignature() error = %q, want the exact unchanged message %q", err.Error(), wantMessage)
	}
}

// TestServiceVerifySignature_BundleReferrerSubjectMismatchIsNotAccepted is
// the subject-matching regression: a bundle index/referrer exists and its
// DSSE signature would verify cryptographically, but its subject.digest
// belongs to a DIFFERENT image than the one being pulled. The
// subject-matching filter must actually filter -- this signature must never
// be accepted for the digest under test.
func TestServiceVerifySignature_BundleReferrerSubjectMismatchIsNotAccepted(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	targetDigest := seedArbitraryImageManifest(t, service, repository, " - subject mismatch target")
	otherDigest := seedArbitraryImageManifest(t, service, repository, " - subject mismatch other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	// The bundle index sits at targetDigest's tag, but the referrer inside
	// it binds otherDigest -- a real signature for a DIFFERENT image.
	seedBundleSignatureArtifact(t, service, repository, targetDigest, otherDigest, bareHex(otherDigest), key)

	policy := signingPolicyForTest(t, true, []string{keyPEM})

	state, err := service.verifySignature(context.Background(), repository, targetDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation (pull must stay blocked for targetDigest)")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state == signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, must never be verified for a subject that does not match", state)
	}
}

// TestServiceVerifySignature_BundleSignatureNotFromTrustedKeyIsUntrusted is
// the untrusted-key case: a bundle-format entry whose DSSE signature does
// not verify against any configured trusted key -> untrusted, blocked.
func TestServiceVerifySignature_BundleSignatureNotFromTrustedKeyIsUntrusted(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - untrusted key")

	signingKey, _ := generateTestECDSAP256KeyPair(t)    // the key that actually signs
	_, trustedKeyPEM := generateTestECDSAP256KeyPair(t) // an unrelated key configured as trusted

	seedBundleSignatureArtifact(t, service, repository, imageDigest, imageDigest, bareHex(imageDigest), signingKey)
	policy := signingPolicyForTest(t, true, []string{trustedKeyPEM})

	state, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateUntrusted {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateUntrusted)
	}
}

// TestServiceVerifySignature_BundleSignatureVerifiesButClaimsMismatchIsMismatched
// is the most security-critical negative case: the DSSE signature verifies
// cryptographically against a trusted key (it is a real signature produced
// by a real trusted key), but the in-toto statement it signs binds a
// DIFFERENT digest than the one being pulled -- tampered/transplanted claims
// -> mismatched, blocked.
func TestServiceVerifySignature_BundleSignatureVerifiesButClaimsMismatchIsMismatched(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	targetDigest := seedArbitraryImageManifest(t, service, repository, " - claims mismatch target")
	differentDigest := seedArbitraryImageManifest(t, service, repository, " - claims mismatch different statement subject")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	// The referrer's subject correctly points at targetDigest (so it passes
	// the subject filter and is even considered), but the DSSE-signed
	// in-toto statement inside the bundle document actually binds
	// differentDigest -- a genuine signature by a trusted key, over the
	// wrong claims.
	seedBundleSignatureArtifact(t, service, repository, targetDigest, targetDigest, bareHex(differentDigest), key)
	policy := signingPolicyForTest(t, true, []string{keyPEM})

	state, err := service.verifySignature(context.Background(), repository, targetDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateMismatched {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateMismatched)
	}
}

// TestServiceVerifySignature_LegacySignaturePresentNeverTriesBundleFallback
// is the regression guard that the new fallback branch never even runs when
// the legacy `.sig` tag exists: a working legacy signature verifies exactly
// as before, even with a MALFORMED bundle index sitting at the very same
// digest's bundle-index tag. If the fallback branch were ever reached, it
// would trip over the malformed index and this test would fail -- so a
// clean pass here proves the fallback path was never entered.
func TestServiceVerifySignature_LegacySignaturePresentNeverTriesBundleFallback(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)
	seedMalformedBundleIndexManifest(t, service, repository, fixtureImageDigest)

	policy := signingPolicyForTest(t, true, []string{fixtureTrustedKeyPEM(t)})

	state, err := service.verifySignature(context.Background(), repository, fixtureImageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (the legacy signature must still verify, bundle fallback must never run)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
}

// seedMalformedBundleIndexManifest publishes deliberately unparseable bytes
// directly through the metadata store (bypassing PublishManifest's JSON
// validation, which would reject this payload) at digest's bundle-index tag
// -- standing in for a broken/foreign manifest an operator could have left
// at that tag. Used only to prove the legacy path never reaches this tag at
// all when the legacy `.sig` tag already resolved.
func seedMalformedBundleIndexManifest(t *testing.T, service *Service, repository string, digest string) {
	t.Helper()

	payload := []byte("not json, deliberately unparseable")
	manifest, err := domain.NewManifest("application/vnd.oci.image.index.v1+json", payload, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("domain.NewManifest(malformed bundle index) error = %v", err)
	}

	tag, err := signing.BundleIndexTag(digest)
	if err != nil {
		t.Fatalf("signing.BundleIndexTag(%q) error = %v", digest, err)
	}

	repo, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository(%q) error = %v", repository, err)
	}

	if err := service.metadata.PublishManifest(context.Background(), "tenant-a", repo, tag, manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("metadata.PublishManifest(malformed bundle index) error = %v", err)
	}
}

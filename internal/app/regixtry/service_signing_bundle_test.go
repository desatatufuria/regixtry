package regixtry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

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

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (the bundle-format signature must verify)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
	if want := signing.Fingerprint(keyPEM); match.KeyFingerprint != want {
		t.Fatalf("verifySignature() fingerprint = %q, want %q (the fingerprint of the trusted key that actually matched)", match.KeyFingerprint, want)
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

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if state != signatureStateUnsigned {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateUnsigned)
	}
	if match.KeyFingerprint != "" {
		t.Fatalf("verifySignature() fingerprint = %q, want empty for an unsigned digest", match.KeyFingerprint)
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

	state, match, err := service.verifySignature(context.Background(), repository, targetDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation (pull must stay blocked for targetDigest)")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state == signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, must never be verified for a subject that does not match", state)
	}
	if match.KeyFingerprint != "" {
		t.Fatalf("verifySignature() fingerprint = %q, want empty when not verified", match.KeyFingerprint)
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

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateUntrusted {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateUntrusted)
	}
	if match.KeyFingerprint != "" {
		t.Fatalf("verifySignature() fingerprint = %q, want empty for an untrusted signature", match.KeyFingerprint)
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

	state, match, err := service.verifySignature(context.Background(), repository, targetDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateMismatched {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateMismatched)
	}
	if match.KeyFingerprint != "" {
		t.Fatalf("verifySignature() fingerprint = %q, want empty when claims mismatch even though the signature itself verified", match.KeyFingerprint)
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

	state, match, err := service.verifySignature(context.Background(), repository, fixtureImageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (the legacy signature must still verify, bundle fallback must never run)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
	if want := signing.Fingerprint(fixtureTrustedKeyPEM(t)); match.KeyFingerprint != want {
		t.Fatalf("verifySignature() fingerprint = %q, want %q (the fingerprint of the trusted key that actually matched)", match.KeyFingerprint, want)
	}
}

// TestServiceVerifySignature_LegacyFormatMultipleTrustedKeysAttributesTheMatchingOneNotTheFirst
// is the Judgment Day coverage-gap RED test (dual-confirmed): every existing
// legacy-format test in this package configures exactly one trusted key, so
// the core new claim of this change -- correct per-key fingerprint
// attribution among multiple configured keys -- has never actually
// exercised a non-first matching key. An unrelated key is listed FIRST in
// TrustedPublicKeys, the fixture's own signing key SECOND: verification must
// still succeed, and the returned fingerprint must be the signing key's, not
// the first (unrelated) key's.
func TestServiceVerifySignature_LegacyFormatMultipleTrustedKeysAttributesTheMatchingOneNotTheFirst(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)

	unrelatedKeyPEM := generateTestECDSAP256PublicKeyPEM(t)
	signingKeyPEM := fixtureTrustedKeyPEM(t)
	policy := signingPolicyForTest(t, true, []string{unrelatedKeyPEM, signingKeyPEM})

	state, match, err := service.verifySignature(context.Background(), repository, fixtureImageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (the fixture signature must verify against its own key, wherever it sits in the trusted list)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
	if want := signing.Fingerprint(signingKeyPEM); match.KeyFingerprint != want {
		t.Fatalf("verifySignature() fingerprint = %q, want %q (the actual signing key's fingerprint, not the first/unrelated key's)", match.KeyFingerprint, want)
	}
	if unwanted := signing.Fingerprint(unrelatedKeyPEM); match.KeyFingerprint == unwanted {
		t.Fatal("verifySignature() attributed the fingerprint to the first (unrelated) key, not the one that actually signed")
	}
}

// TestServiceVerifySignature_BundleFormatMultipleTrustedKeysAttributesTheMatchingOneNotTheFirst
// is the bundle-format sibling of the legacy-format coverage-gap test above:
// the signing key sits SECOND in TrustedPublicKeys, behind an unrelated
// first key -- attribution must still point at the actual signer.
func TestServiceVerifySignature_BundleFormatMultipleTrustedKeysAttributesTheMatchingOneNotTheFirst(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - bundle multi-key attribution")
	signingKey, signingKeyPEM := generateTestECDSAP256KeyPair(t)
	_, unrelatedKeyPEM := generateTestECDSAP256KeyPair(t)
	seedBundleSignatureArtifact(t, service, repository, imageDigest, imageDigest, bareHex(imageDigest), signingKey)

	policy := signingPolicyForTest(t, true, []string{unrelatedKeyPEM, signingKeyPEM})

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (the bundle-format signature must verify against its own key, wherever it sits in the trusted list)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
	if want := signing.Fingerprint(signingKeyPEM); match.KeyFingerprint != want {
		t.Fatalf("verifySignature() fingerprint = %q, want %q (the actual signing key's fingerprint, not the first/unrelated key's)", match.KeyFingerprint, want)
	}
	if unwanted := signing.Fingerprint(unrelatedKeyPEM); match.KeyFingerprint == unwanted {
		t.Fatal("verifySignature() attributed the fingerprint to the first (unrelated) key, not the one that actually signed")
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
	manifest, err := domain.NewManifest("application/vnd.oci.image.index.v1+json", "", payload, nil, nil, nil, nil)
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

// TestServiceSignatureStatusReportsTheBundleIndexTagAndCountForABundleSignature
// is the RED test for a gap found live in production after this branch
// merged: SignatureStatus's State comes from verifySignature (already
// bundle-aware), but its Signature.Tag/SignatureCount come from the
// separate resolveSignatureManifestEntries, which only ever resolved the
// legacy `.sig` tag. Confirmed live: a real bundle-verified image reported
// state "verified" but signature.tag as a `.sig` tag that does not exist
// and signature_count: 0. resolveSignatureManifestEntries must fall back to
// the bundle index exactly like verifySignature already does, and report
// its real tag and a count of the referrer entries that actually match this
// digest's subject.
func TestServiceSignatureStatusReportsTheBundleIndexTagAndCountForABundleSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - bundle status")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedBundleSignatureArtifact(t, service, repository, imageDigest, imageDigest, bareHex(imageDigest), key)

	if _, err := service.UpdateSigningPolicySettings(context.Background(), ports.SigningPolicySettings{
		Enabled:           true,
		TrustedPublicKeys: []string{keyPEM},
	}); err != nil {
		t.Fatalf("UpdateSigningPolicySettings() error = %v", err)
	}

	result, err := service.SignatureStatus(context.Background(), repository, imageDigest)
	if err != nil {
		t.Fatalf("SignatureStatus() error = %v", err)
	}
	if result.State != SignatureStatusVerified {
		t.Fatalf("SignatureStatus() State = %q, want %q", result.State, SignatureStatusVerified)
	}
	if result.Signature == nil {
		t.Fatal("SignatureStatus() Signature = nil, want a populated detail for a verified bundle signature")
	}

	wantTag, err := signing.BundleIndexTag(imageDigest)
	if err != nil {
		t.Fatalf("signing.BundleIndexTag(%q) error = %v", imageDigest, err)
	}
	if result.Signature.Tag != wantTag {
		t.Fatalf("SignatureStatus() Signature.Tag = %q, want the bundle index tag %q, not a nonexistent legacy .sig tag", result.Signature.Tag, wantTag)
	}
	if result.Signature.SignatureCount != 1 {
		t.Fatalf("SignatureStatus() Signature.SignatureCount = %d, want 1 (the one real bundle referrer bound to this digest)", result.Signature.SignatureCount)
	}
}

// selfSignedCertDER mints a throwaway, syntactically valid self-signed X.509
// certificate -- standing in for a Fulcio leaf certificate structurally
// (verificationMaterial.certificate.rawBytes only needs to be a parseable
// certificate for signing.VerifyKeyless to reach real verification logic).
// It deliberately does NOT chain to any trusted root, least of all the real
// pinned Sigstore public-good root keyless.go verifies against (design.md:
// "no operator override", settled) -- no live keyless-signed Bundle-document
// artifact was obtainable in this sandbox (apply-progress.md's Phase 0
// note, carried forward unchanged from PR1), so a positive "verified via
// identity" outcome cannot be constructed offline; see
// TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification's
// own doc comment for what this fixture proves instead.
func selfSignedCertDER(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-identity-fixture"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() error = %v", err)
	}
	return der
}

// buildKeylessBundleDocument builds a Sigstore Bundle document JSON shaped
// like buildBundleDocument's dsseEnvelope (payload/payloadType/signatures --
// the identity branch never inspects bundle.Signatures itself; it hands the
// whole raw document to signing.VerifyKeyless instead, so the signature
// bytes here are deliberately arbitrary), PLUS a
// verificationMaterial.certificate block so
// signing.ParseBundleVerificationMaterial's HasCertificate reports true and
// the identity branch is genuinely reached. tlogEntries is intentionally
// omitted: keyless.VerifyKeyless's own precheck (before ever reaching chain
// validation) rejects a missing transparency log entry -- a real,
// deterministic failure mode, not a malformed-JSON short-circuit.
func buildKeylessBundleDocument(t *testing.T, certDER []byte, payload []byte) []byte {
	t.Helper()

	const payloadType = "application/vnd.in-toto+json"
	doc := map[string]any{
		"mediaType": signing.SigstoreBundleMediaType,
		"verificationMaterial": map[string]any{
			"certificate": map[string]any{"rawBytes": base64.StdEncoding.EncodeToString(certDER)},
		},
		"dsseEnvelope": map[string]any{
			"payload":     base64.StdEncoding.EncodeToString(payload),
			"payloadType": payloadType,
			"signatures":  []map[string]string{{"sig": base64.StdEncoding.EncodeToString([]byte("not-a-real-signature"))}},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal(keyless bundle document) error = %v", err)
	}
	return raw
}

// seedKeylessBundleSignatureArtifact mirrors seedBundleSignatureArtifact's
// referrer/index publishing structure exactly, swapping in
// buildKeylessBundleDocument for buildBundleDocument -- a keyless
// (certificate-carrying) bundle document instead of a key-signed one.
func seedKeylessBundleSignatureArtifact(t *testing.T, service *Service, repository string, indexDigest string, subjectDigest string, statementDigestHex string, certDER []byte) {
	t.Helper()

	ctx := context.Background()

	payload := bundleInTotoStatementPayload(t, statementDigestHex)
	bundleDoc := buildKeylessBundleDocument(t, certDER, payload)
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

// TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification
// is tasks.md 6.2: with zero trusted keys and >=1 trusted identity
// configured, verifyBundleSignature's identity branch is reached and calls
// the REAL (not mocked) signing.VerifyKeyless against a bundle fixture
// carrying verificationMaterial (spec: "Identity-only policy verifies with
// no trusted key").
//
// signing.VerifyKeyless always verifies against the real embedded pinned
// Sigstore public-good root (design.md: "no operator override", settled) --
// a certificate this fixture can mint offline can never legitimately chain
// to it, and no live keyless-signed Bundle-document artifact was obtainable
// in this sandbox (apply-progress.md's Phase 0 note, carried forward
// unchanged from PR1). This test proves the achievable, real half of that
// constraint: the identity anchor is genuinely exercised -- state becomes
// "untrusted" (a real verification ATTEMPT that failed), never
// "unverifiable" (reserved for the "no usable anchor configured at all"
// precondition zero keys alone used to always trip before this change).
// TestComposeVerifiedIdentity_ReportsMatchingIdentitysIssuer below
// separately proves the SAN+issuer composition a genuine match would
// report, without requiring an unfakeable live artifact.
func TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - identity only")
	certDER := selfSignedCertDER(t)
	seedKeylessBundleSignatureArtifact(t, service, repository, imageDigest, imageDigest, bareHex(imageDigest), certDER)

	policy := ports.SigningPolicySettings{
		Enabled: true,
		TrustedIdentities: []ports.TrustedIdentity{
			{CertificateIdentityRegexp: "^.*$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"},
		},
	}

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation (a self-signed test certificate can never chain to the real pinned root)")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateUntrusted {
		t.Fatalf("verifySignature() state = %q, want %q (a real verification attempt was made and failed, distinctly from %q which means no usable anchor was even configured)", state, signatureStateUntrusted, signatureStateUnverifiable)
	}
	if match.KeyFingerprint != "" || match.Identity != "" {
		t.Fatalf("verifySignature() match = %#v, want the zero value when nothing verified", match)
	}
}

// TestServiceVerifySignature_ZeroKeysAndZeroIdentitiesIsUnverifiable is
// tasks.md 6.3: enabling the policy requires >=1 usable anchor of EITHER
// kind -- zero keys AND zero identities still reports "unverifiable",
// distinctly from the "untrusted" a real (if doomed) identity-only
// verification attempt reports above.
func TestServiceVerifySignature_ZeroKeysAndZeroIdentitiesIsUnverifiable(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - zero anchors")
	policy := ports.SigningPolicySettings{Enabled: true}

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateUnverifiable {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateUnverifiable)
	}
	if match.KeyFingerprint != "" || match.Identity != "" {
		t.Fatalf("verifySignature() match = %#v, want the zero value", match)
	}
}

// TestComposeVerifiedIdentity_ReportsMatchingIdentitysIssuer is tasks.md
// 6.4: a verified identity match reports the matched certificate SAN
// together with the OIDC issuer of whichever configured TrustedIdentity's
// regexp actually matched it -- never just the bare SAN, and never
// overloading the key-fingerprint field (spec: "Identity match reports SAN
// and issuer separately"). Pure function, no crypto, no mocks: exercises the
// exact production logic an identity-verified pull composes
// signatureMatch.Identity with.
func TestComposeVerifiedIdentity_ReportsMatchingIdentitysIssuer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		matchedSAN string
		identities []ports.TrustedIdentity
		want       string
	}{
		{
			name:       "single configured identity",
			matchedSAN: "https://github.com/example/repo/.github/workflows/release.yml@refs/heads/main",
			identities: []ports.TrustedIdentity{
				{CertificateIdentityRegexp: "^https://github.com/example/.*$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"},
			},
			want: "https://github.com/example/repo/.github/workflows/release.yml@refs/heads/main (https://token.actions.githubusercontent.com)",
		},
		{
			name:       "multiple configured identities picks the actually-matching one, not the first",
			matchedSAN: "https://gitlab.com/example/repo//.gitlab-ci.yml@refs/heads/main",
			identities: []ports.TrustedIdentity{
				{CertificateIdentityRegexp: "^https://github.com/.*$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"},
				{CertificateIdentityRegexp: "^https://gitlab.com/.*$", CertificateOIDCIssuer: "https://gitlab.com"},
			},
			want: "https://gitlab.com/example/repo//.gitlab-ci.yml@refs/heads/main (https://gitlab.com)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := composeVerifiedIdentity(tt.matchedSAN, tt.identities)
			if got != tt.want {
				t.Fatalf("composeVerifiedIdentity(%q, %v) = %q, want %q", tt.matchedSAN, tt.identities, got, tt.want)
			}
		})
	}
}

// seedReferrerOnlyBundleSignatureArtifact publishes ONLY the referrer
// manifest (subject.digest = subjectDigest) and its bundle-document blob --
// deliberately never publishing the legacy signing.BundleIndexTag(subjectDigest)
// Image Index seedBundleSignatureArtifact always publishes alongside it. This
// is exactly what modern cosign actually does today (confirmed live via
// three real keyless-signing-smoke.yml runs): it pushes the signature as a
// genuine OCI 1.1 referrer artifact, discoverable only through the real
// Referrers API (ListReferrers), and writes no "sha256-<hex>" index tag at
// all. verifyBundleSignature's pre-fix tag-index-only lookup can never find
// a signature seeded this way; the Referrers API fallback must.
func seedReferrerOnlyBundleSignatureArtifact(t *testing.T, service *Service, repository string, subjectDigest string, statementDigestHex string, key *ecdsa.PrivateKey) {
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
	service.WaitForBackgroundWork()
}

// seedReferrerOnlyKeylessBundleSignatureArtifact mirrors
// seedReferrerOnlyBundleSignatureArtifact, swapping in
// buildKeylessBundleDocument (a certificate-carrying bundle document) for
// buildBundleDocument -- the referrer-only sibling of
// seedKeylessBundleSignatureArtifact.
func seedReferrerOnlyKeylessBundleSignatureArtifact(t *testing.T, service *Service, repository string, subjectDigest string, statementDigestHex string, certDER []byte) {
	t.Helper()

	ctx := context.Background()

	payload := bundleInTotoStatementPayload(t, statementDigestHex)
	bundleDoc := buildKeylessBundleDocument(t, certDER, payload)
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
	service.WaitForBackgroundWork()
}

// TestServiceVerifySignature_ReferrerOnlyBundleSignatureIsDiscoveredAndVerified
// is the RED characterization test for the real production gap this change
// fixes: modern cosign (any version defaulting to real OCI 1.1 referrers,
// confirmed live via keyless-signing-smoke.yml across multiple runs) never
// writes the legacy signing.BundleIndexTag Image Index at all -- it pushes
// the signature purely as a real referrer artifact, discoverable only
// through the Referrers API. Before this change, verifyBundleSignature only
// ever looked up BundleIndexTag and reported "unsigned" the instant that tag
// didn't resolve, never querying ListReferrers -- so a signature seeded
// EXACTLY this way (no index tag, referrer-only) was always rejected even
// with a valid trusted key configured. After the fix, this must verify.
func TestServiceVerifySignature_ReferrerOnlyBundleSignatureIsDiscoveredAndVerified(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - referrer-only bundle verified")
	key, keyPEM := generateTestECDSAP256KeyPair(t)
	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, imageDigest, bareHex(imageDigest), key)

	policy := signingPolicyForTest(t, true, []string{keyPEM})

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err != nil {
		t.Fatalf("verifySignature() error = %v, want nil (a referrer-only bundle signature, discoverable only via the real Referrers API, must verify)", err)
	}
	if state != signatureStateVerified {
		t.Fatalf("verifySignature() state = %q, want %q", state, signatureStateVerified)
	}
	if want := signing.Fingerprint(keyPEM); match.KeyFingerprint != want {
		t.Fatalf("verifySignature() fingerprint = %q, want %q (the fingerprint of the trusted key that actually matched)", match.KeyFingerprint, want)
	}
}

// TestServiceVerifySignature_ReferrerOnlyBundleSignatureNotFromTrustedKeyIsUntrusted
// is the referrer-only sibling of
// TestServiceVerifySignature_BundleSignatureNotFromTrustedKeyIsUntrusted: a
// signature exists ONLY as a real referrer (no index tag), but its DSSE
// signature does not verify against any configured trusted key -> untrusted,
// not unsigned (a real candidate WAS found and considered via the Referrers
// API, it just didn't verify -- preserving the tag-index path's own
// unsigned/untrusted distinction for this new discovery source too).
func TestServiceVerifySignature_ReferrerOnlyBundleSignatureNotFromTrustedKeyIsUntrusted(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - referrer-only untrusted key")

	signingKey, _ := generateTestECDSAP256KeyPair(t)    // the key that actually signs
	_, trustedKeyPEM := generateTestECDSAP256KeyPair(t) // an unrelated key configured as trusted

	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, imageDigest, bareHex(imageDigest), signingKey)
	policy := signingPolicyForTest(t, true, []string{trustedKeyPEM})

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateUntrusted {
		t.Fatalf("verifySignature() state = %q, want %q (a real referrer candidate was found via the Referrers API but did not verify)", state, signatureStateUntrusted)
	}
	if match.KeyFingerprint != "" {
		t.Fatalf("verifySignature() fingerprint = %q, want empty for an untrusted signature", match.KeyFingerprint)
	}
}

// TestServiceVerifySignature_ReferrerOnlyIdentityOnlyPolicyReachesRealKeylessVerification
// is the referrer-only sibling of
// TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification:
// a keyless (certificate-carrying) bundle document, discoverable ONLY via
// the real Referrers API (no index tag), with zero trusted keys and >=1
// trusted identity configured. This proves the Referrers API fallback also
// reaches the identity/keyless verification branch, not just the key branch
// -- state becomes "untrusted" (a real verification ATTEMPT that failed,
// since a self-signed test certificate can never chain to the real pinned
// Sigstore root), never "unverifiable" or "unsigned".
func TestServiceVerifySignature_ReferrerOnlyIdentityOnlyPolicyReachesRealKeylessVerification(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	imageDigest := seedArbitraryImageManifest(t, service, repository, " - referrer-only identity only")
	certDER := selfSignedCertDER(t)
	seedReferrerOnlyKeylessBundleSignatureArtifact(t, service, repository, imageDigest, bareHex(imageDigest), certDER)

	policy := ports.SigningPolicySettings{
		Enabled: true,
		TrustedIdentities: []ports.TrustedIdentity{
			{CertificateIdentityRegexp: "^.*$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"},
		},
	}

	state, match, err := service.verifySignature(context.Background(), repository, imageDigest, policy)
	if err == nil {
		t.Fatal("verifySignature() error = nil, want a policy violation (a self-signed test certificate can never chain to the real pinned root)")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("verifySignature() error = %v, want ErrorCodePolicyViolation", err)
	}
	if state != signatureStateUntrusted {
		t.Fatalf("verifySignature() state = %q, want %q (a real verification attempt was made via the Referrers API fallback and failed)", state, signatureStateUntrusted)
	}
	if match.KeyFingerprint != "" || match.Identity != "" {
		t.Fatalf("verifySignature() match = %#v, want the zero value when nothing verified", match)
	}
}

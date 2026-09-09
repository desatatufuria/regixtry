package regixtryhttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto/ecdsa"
	"crypto/elliptic"

	"regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
)

// TestE2E prefix (this file): Phase 9 task 9.1's E2E proof of the FULL
// keyless-identity stack -- HTTP config write (PUT /admin/v1/signing-policy
// with trusted_identities) -> sqlite persist (GetSigningPolicySettings) ->
// verify-time read (a real pull request through the router, reaching the
// REAL signing.VerifyKeyless via verifyBundleSignature's identity branch) --
// entirely through ordinary HTTP requests, not service-layer shortcuts,
// going one full layer further than
// TestServiceVerifySignature_IdentityOnlyPolicyReachesRealKeylessVerification
// (internal/app/regixtry/service_signing_bundle_test.go), which proves the
// identical real-verification-attempt claim but stops at the service layer.
//
// Carried forward from PR1's Phase 0 spike and PR3's Phase 6 (see
// apply-progress.md): no live keyless-signed Sigstore Bundle-document
// artifact was obtainable in this sandbox, and signing.VerifyKeyless always
// verifies against the REAL embedded pinned Sigstore public-good root with
// no operator override (design.md, settled) -- so no certificate mintable
// offline in this test can ever legitimately chain to it. This test
// therefore proves the achievable, real half of the full stack: a
// self-signed test certificate genuinely reaches signing.VerifyKeyless
// through the complete HTTP round trip, and the observable result is a real
// fail-closed 403 (an ATTEMPT that failed), never a silently-passed pull or
// a config value that was accepted but never actually read back at
// verify-time. A genuinely successful end-to-end match (wrong-issuer-blocks
// vs matching-issuer-allows, tasks.md 9.1's original ask) remains an open
// item requiring a live artifact or a docker-capable environment -- see
// apply-progress.md's "Open items" section.

// e2eKeylessSelfSignedCertDER mirrors
// internal/app/regixtry/service_signing_bundle_test.go's selfSignedCertDER
// exactly (same doc-comment rationale): a throwaway, syntactically valid,
// non-chaining certificate, standing in for a Fulcio leaf certificate
// structurally only.
func e2eKeylessSelfSignedCertDER(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "e2e-test-identity-fixture"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() error = %v", err)
	}
	return der
}

// e2eKeylessBundleInTotoStatementPayload mirrors
// service_signing_bundle_test.go's bundleInTotoStatementPayload.
func e2eKeylessBundleInTotoStatementPayload(t *testing.T, subjectDigestHex string) []byte {
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

// e2eKeylessBundleDocument mirrors
// service_signing_bundle_test.go's buildKeylessBundleDocument: a
// certificate-carrying, tlogEntries-omitted bundle document (a real,
// deterministic "transparency log missing" failure mode once verification
// runs, not a malformed-JSON short-circuit).
func e2eKeylessBundleDocument(t *testing.T, certDER []byte, payload []byte) []byte {
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

// pushManifestViaHTTP PUTs manifestBytes at the given reference through the
// router, mirroring seedSignatureStatusFixtureSignature's PUT shape.
func pushManifestViaHTTP(t *testing.T, router *Router, repository string, reference string, mediaType string, manifestBytes []byte) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, "/v2/"+repository+"/manifests/"+reference, bytes.NewReader(manifestBytes))
	req.Header.Set("Content-Type", mediaType)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("push manifest at %s status = %d, want %d, body = %s", reference, recorder.Code, http.StatusCreated, recorder.Body.String())
	}
}

// seedKeylessBundleSignatureArtifactViaHTTP publishes a full modern
// bundle-format keyless signature artifact chain (bundle document blob,
// referrer manifest, OCI Image Index at signing.BundleIndexTag) entirely
// through ordinary HTTP blob-upload and manifest-PUT requests -- the HTTP
// layer's own equivalent of service_signing_bundle_test.go's
// seedKeylessBundleSignatureArtifact, which publishes through
// service.PublishManifest directly instead.
func seedKeylessBundleSignatureArtifactViaHTTP(t *testing.T, router *Router, repository string, indexDigest string, subjectDigest string, statementDigestHex string, certDER []byte) {
	t.Helper()

	payload := e2eKeylessBundleInTotoStatementPayload(t, statementDigestHex)
	bundleDoc := e2eKeylessBundleDocument(t, certDER, payload)
	uploadBlobViaHTTP(t, router, repository, bundleDoc)

	emptyConfig := []byte("{}")
	uploadBlobViaHTTP(t, router, repository, emptyConfig)

	referrerManifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.empty.v1+json",
			"digest":    domain.DigestFromBytes(emptyConfig).String(),
			"size":      len(emptyConfig),
		},
		"layers": []map[string]any{
			{
				"mediaType": signing.SigstoreBundleMediaType,
				"digest":    domain.DigestFromBytes(bundleDoc).String(),
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
	referrerDigest := domain.DigestFromBytes(referrerBytes).String()
	pushManifestViaHTTP(t, router, repository, referrerDigest, "application/vnd.oci.image.manifest.v1+json", referrerBytes)

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
	pushManifestViaHTTP(t, router, repository, tag, "application/vnd.oci.image.index.v1+json", indexBytes)
}

// e2eKeylessBareHex mirrors service_signing_bundle_test.go's bareHex.
func e2eKeylessBareHex(digest string) string {
	const prefix = "sha256:"
	return digest[len(prefix):]
}

// TestE2EKeylessIdentityPolicyRoundTripsThroughHTTPConfigSqlitePersistAndPullTimeVerification
// is the Phase 9 task 9.1 E2E test: PUT /admin/v1/signing-policy with
// trusted_identities (HTTP config write) -> GetSigningPolicySettings reads
// back the exact stored value (sqlite persist, confirmed via a second GET)
// -> a real pull request reaches signing.VerifyKeyless through the complete
// router stack and fails closed, distinctly from the "no usable anchor
// configured at all" precondition (verify-time read). See this file's own
// package-level doc comment for why a genuinely successful match cannot be
// constructed offline in this sandbox.
func TestE2EKeylessIdentityPolicyRoundTripsThroughHTTPConfigSqlitePersistAndPullTimeVerification(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer drainingCleanup(router, cleanup)()

	// 1. HTTP config write: PUT the global signing policy with an
	// identity-only anchor (zero trusted keys), mirroring the admin
	// console's own write path (admin_handlers_test.go's
	// TestAdminSigningPolicyPutPersistsTrustedIdentitiesAndRoundTrips, one
	// layer up from a full pull-gate proof).
	putBody, err := json.Marshal(map[string]any{
		"enabled": true,
		"trusted_identities": []map[string]string{
			{"certificate_identity_regexp": "^.*$", "certificate_oidc_issuer": "https://token.actions.githubusercontent.com"},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/signing-policy", bytes.NewReader(putBody))
	putReq.Header.Set("Authorization", "Bearer admin-token")
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT /admin/v1/signing-policy status = %d, want %d, body = %s", putRecorder.Code, http.StatusOK, putRecorder.Body.String())
	}

	// 2. sqlite persist: confirm the exact stored value round-trips,
	// independent of the PUT response's own echo.
	stored, err := store.GetSigningPolicySettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetSigningPolicySettings() error = %v", err)
	}
	if len(stored.TrustedIdentities) != 1 || stored.TrustedIdentities[0].CertificateOIDCIssuer != "https://token.actions.githubusercontent.com" {
		t.Fatalf("stored = %#v, want the identity-only policy persisted", stored)
	}

	// 3. verify-time read: seed an ordinary image manifest and a keyless
	// bundle signature artifact entirely through HTTP, then pull it -- the
	// pull-time gate must call GetSigningPolicySettings (reading the exact
	// row just persisted above) and reach the REAL signing.VerifyKeyless.
	repository := "library/e2e-keyless"
	imagePayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"` + domain.DigestFromBytes([]byte("{}")).String() + `","size":2},"layers":[]}`)
	uploadBlobViaHTTP(t, router, repository, []byte("{}"))
	imageDigest := domain.DigestFromBytes(imagePayload).String()
	pushManifestViaHTTP(t, router, repository, imageDigest, "application/vnd.oci.image.manifest.v1+json", imagePayload)

	certDER := e2eKeylessSelfSignedCertDER(t)
	seedKeylessBundleSignatureArtifactViaHTTP(t, router, repository, imageDigest, imageDigest, e2eKeylessBareHex(imageDigest), certDER)

	pullReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+imageDigest, nil)
	pullRecorder := httptest.NewRecorder()
	router.ServeHTTP(pullRecorder, pullReq)

	if pullRecorder.Code != http.StatusForbidden {
		t.Fatalf("pull status = %d, want %d -- a real signing.VerifyKeyless attempt against a non-chaining test certificate must fail closed through the full HTTP stack, body = %s", pullRecorder.Code, http.StatusForbidden, pullRecorder.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(pullRecorder.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode pull error body error = %v, body = %s", err, pullRecorder.Body.String())
	}
	if len(errBody.Errors) != 1 || errBody.Errors[0].Code != "DENIED" {
		t.Fatalf("pull error body = %#v, want exactly one error with code %q (domain.ErrorCodePolicyViolation -- a real, distinct verification-attempt failure, not an unrelated error)", errBody, "DENIED")
	}

	// signature-status corroborates the same real attempt: state must be
	// "untrusted" (a verification ATTEMPT that failed), never "unverifiable"
	// (reserved for "no usable anchor configured at all", which would mean
	// the persisted trusted_identities was never actually read at
	// verify-time).
	statusReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+imageDigest+"/signature-status", nil)
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("signature-status status = %d, want %d, body = %s", statusRecorder.Code, http.StatusOK, statusRecorder.Body.String())
	}
	var statusBody struct {
		State  string `json:"state"`
		Policy struct {
			TrustedIdentities int `json:"trusted_identities"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &statusBody); err != nil {
		t.Fatalf("decode signature-status body error = %v, body = %s", err, statusRecorder.Body.String())
	}
	if statusBody.State != "untrusted" {
		t.Fatalf("signature-status state = %q, want %q (a real verification attempt reached and failed, not the zero-anchor precondition)", statusBody.State, "untrusted")
	}
	if statusBody.Policy.TrustedIdentities != 1 {
		t.Fatalf("signature-status policy.trusted_identities = %d, want %d (the persisted count read back at verify-time)", statusBody.Policy.TrustedIdentities, 1)
	}
}

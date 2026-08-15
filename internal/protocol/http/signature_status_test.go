package regixtryhttp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

// signatureStatusFixtureImagePayload/signatureStatusFixtureImageDigest
// mirror internal/app/regixtry/service_signing_test.go's identically-named
// literals: this exact payload's SHA-256 is the digest
// internal/domain/signing/testdata's synthetic fixture's payload.json
// claims in critical.image.docker-manifest-digest.
const signatureStatusFixtureImagePayload = "image-signing synthetic fixture image manifest v1"
const signatureStatusFixtureImageDigest = "sha256:d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4"

// signatureStatusFixtureSignatureBase64 is the exact base64 ASN.1 DER
// signature bytes carried by
// internal/domain/signing/testdata/signature-manifest.json's single layer
// annotation -- used to prove no response ever echoes it.
const signatureStatusFixtureSignatureBase64 = "MEUCIF4bpdl7sYB6SPgwtWWupCuZOjoq1kv9dVdoD/RlamJaAiEA/AwiTgxicjSnCJSzOWMc+5YjmHM2US5WUFFy97oiTmY="

func readSignatureStatusFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "domain", "signing", "testdata", name))
	if err != nil {
		t.Fatalf("reading signing testdata/%s: %v", name, err)
	}
	return data
}

func signatureStatusFixtureTrustedKeyPEM(t *testing.T) string {
	t.Helper()
	return string(readSignatureStatusFixture(t, "cosign.pub"))
}

// seedSignatureStatusFixtureImage publishes the fixture's exact image
// manifest payload directly through the metadata store, bypassing
// handleManifest's JSON-envelope parsing (the literal fixture payload is
// not valid OCI manifest JSON, exactly the same bypass
// service_signing_test.go's seedArbitraryImageManifest uses at the service
// layer), and confirms the computed digest matches
// signatureStatusFixtureImageDigest -- a canary against fixture drift.
func seedSignatureStatusFixtureImage(t *testing.T, metadataStore *metadata.Store, repository string) string {
	t.Helper()

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(signatureStatusFixtureImagePayload), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("domain.NewManifest() error = %v", err)
	}
	repo := domain.MustParseRepositoryRef(repository)
	if err := metadataStore.PublishManifest(context.Background(), "tenant-a", repo, "", manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("metadataStore.PublishManifest() error = %v", err)
	}
	if manifest.Digest.String() != signatureStatusFixtureImageDigest {
		t.Fatalf("fixture image manifest digest = %s, want %s (fixture provenance drifted)", manifest.Digest.String(), signatureStatusFixtureImageDigest)
	}
	return manifest.Digest.String()
}

// uploadBlobViaHTTP drives the ordinary begin/append/commit blob upload
// flow through the router, mirroring TestRouterUploadAndReadFlow's shape.
func uploadBlobViaHTTP(t *testing.T, router *Router, repository string, content []byte) {
	t.Helper()

	startReq := httptest.NewRequest(http.MethodPost, "/v2/"+repository+"/blobs/uploads/", nil)
	startRecorder := httptest.NewRecorder()
	router.ServeHTTP(startRecorder, startReq)
	location := startRecorder.Header().Get("Location")
	if location == "" {
		t.Fatalf("blob upload start for %s missing Location header, status = %d", repository, startRecorder.Code)
	}

	appendReq := httptest.NewRequest(http.MethodPatch, location, bytes.NewReader(content))
	appendRecorder := httptest.NewRecorder()
	router.ServeHTTP(appendRecorder, appendReq)
	if appendRecorder.Code != http.StatusAccepted {
		t.Fatalf("blob upload append status = %d, want %d", appendRecorder.Code, http.StatusAccepted)
	}

	digest := domain.DigestFromBytes(content).String()
	commitReq := httptest.NewRequest(http.MethodPut, location+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	router.ServeHTTP(commitRecorder, commitReq)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("blob upload commit status = %d, want %d, body = %s", commitRecorder.Code, http.StatusCreated, commitRecorder.Body.String())
	}
}

// seedSignatureStatusFixtureSignature uploads the fixture's config/payload
// blobs and publishes the fixture's `.sig` manifest bytes verbatim at the
// legacy tag for the given digest, entirely through ordinary HTTP requests
// (the `.sig` manifest is valid JSON, so it needs no store-level bypass).
func seedSignatureStatusFixtureSignature(t *testing.T, router *Router, repository string, digest string) {
	t.Helper()

	uploadBlobViaHTTP(t, router, repository, []byte("{}"))
	uploadBlobViaHTTP(t, router, repository, readSignatureStatusFixture(t, "payload.json"))

	tag, err := signing.SignatureTag(digest)
	if err != nil {
		t.Fatalf("signing.SignatureTag(%q) error = %v", digest, err)
	}

	req := httptest.NewRequest(http.MethodPut, "/v2/"+repository+"/manifests/"+tag, bytes.NewReader(readSignatureStatusFixture(t, "signature-manifest.json")))
	req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("push .sig manifest at tag %s status = %d, want %d, body = %s", tag, recorder.Code, http.StatusCreated, recorder.Body.String())
	}
}

// generateHTTPTestECDSAP256PublicKeyPEM produces a fresh, real-newline PEM
// public key -- a distinct key per call, unrelated to the fixture's signing
// key, this package's own equivalent of
// internal/app/regixtry/repository_overrides_test.go's identically-shaped
// helper.
func generateHTTPTestECDSAP256PublicKeyPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey() error = %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func seedSignatureStatusPolicy(t *testing.T, router *Router, enabled bool, keys []string) {
	t.Helper()

	if _, err := router.service.UpdateSigningPolicySettings(context.Background(), ports.SigningPolicySettings{Enabled: enabled, TrustedPublicKeys: keys}); err != nil {
		t.Fatalf("UpdateSigningPolicySettings() error = %v", err)
	}
}

// TestRouterManifestSignatureStatusReturnsVerifiedStateForValidSignature is
// the Phase 7 RED test (tasks.md 7.5/7.8): the endpoint is reachable at
// `GET /v2/<repo>/manifests/<ref>/signature-status` and returns the exact
// response shape design.md Decision 9 documents for a verified signature.
func TestRouterManifestSignatureStatusReturnsVerifiedStateForValidSignature(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
	defer drainingCleanup(router, cleanup)()

	repository := "library/alpine"
	digest := seedSignatureStatusFixtureImage(t, store, repository)
	seedSignatureStatusFixtureSignature(t, router, repository, digest)
	seedSignatureStatusPolicy(t, router, true, []string{signatureStatusFixtureTrustedKeyPEM(t)})

	req := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest+"/signature-status", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload struct {
		Repository     string         `json:"repository"`
		Reference      string         `json:"reference"`
		Digest         string         `json:"digest"`
		State          string         `json:"state"`
		WouldBlockPull bool           `json:"would_block_pull"`
		Policy         map[string]any `json:"policy"`
		Signature      map[string]any `json:"signature"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response error = %v, body = %s", err, recorder.Body.String())
	}
	if payload.Repository != repository || payload.Digest != digest {
		t.Fatalf("payload = %#v, want repository/digest to match the published manifest", payload)
	}
	if payload.State != "verified" {
		t.Fatalf("payload.State = %q, want %q", payload.State, "verified")
	}
	if payload.WouldBlockPull {
		t.Fatal("payload.WouldBlockPull = true, want false for a verified signature")
	}
	if payload.Policy == nil || payload.Policy["trusted_keys"] != float64(1) {
		t.Fatalf("payload.Policy = %#v, want trusted_keys = 1", payload.Policy)
	}
	if payload.Signature == nil {
		t.Fatal("payload.Signature = nil, want signature detail present for a resolved .sig tag")
	}
}

// TestRouterManifestSignatureStatusRouteCollisionWithTagNamedSignatureStatus
// is the Phase 7 RED test (tasks.md 7.5, design.md Decision 9): a tag
// literally named "signature-status" must still route to handleManifest,
// not handleManifestSignatureStatus, because the suffix match is checked
// against the trimmed reference, not against the raw path suffix -- mirrors
// TestRouterManifestScanStatusRouteCollisionWithTagNamedScanStatus.
func TestRouterManifestSignatureStatusRouteCollisionWithTagNamedSignatureStatus(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/signature-status", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("push manifest with tag \"signature-status\" status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}
	manifestDigest := manifestRecorder.Header().Get("Docker-Content-Digest")
	if manifestDigest == "" {
		t.Fatal("push manifest response missing Docker-Content-Digest")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/signature-status", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if getRecorder.Header().Get("Docker-Content-Digest") != manifestDigest {
		t.Fatalf("Docker-Content-Digest = %q, want %q (proves handleManifest served this, not handleManifestSignatureStatus)", getRecorder.Header().Get("Docker-Content-Digest"), manifestDigest)
	}
	if strings.Contains(getRecorder.Body.String(), `"would_block_pull"`) {
		t.Fatalf("body = %q, want the raw manifest payload, not the signature-status JSON shape", getRecorder.Body.String())
	}
}

// TestRouterManifestSignatureStatusRequiresPullAuthorization is the Phase 7
// RED test (tasks.md 7.5, spec.md "Caller with pull credentials reads
// status"): reachable with ordinary pull-only credentials, no admin auth --
// mirrors TestRouterManifestScanStatusRequiresPullAuthorization.
func TestRouterManifestSignatureStatusRequiresPullAuthorization(t *testing.T) {
	t.Parallel()

	accessController := ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"})

	t.Run("pull-only credential succeeds", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		digest := seedSignatureStatusFixtureImage(t, store, "library/alpine")
		seedRouter := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
		seedRouter.service.WaitForBackgroundWork()

		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{
			verify: &domainauth.Principal{
				Subject:  "atk_1",
				Username: "ci",
				Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("library/alpine"), Role: domainauth.RepoRoleReader}},
				Scopes:   []domainauth.Scope{{Type: "repository", Name: "library/alpine", Actions: []string{"pull"}, Canonical: "repository:library/alpine:pull"}},
			},
		})
		defer drainingCleanup(handler, cleanup)()

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/signature-status", nil)
		req.Header.Set("Authorization", "Bearer pull-only-token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
	})

	t.Run("no credential is rejected", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		digest := seedSignatureStatusFixtureImage(t, store, "library/alpine")
		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{})
		defer drainingCleanup(handler, cleanup)()

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/signature-status", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})
}

// TestRouterManifestSignatureStatusIsNeverGatedByThePolicyItReports is the
// Phase 7 RED test (tasks.md 7.6, spec.md "Status endpoint is never itself
// gated"): a digest that WOULD be blocked by an actual pull under the
// resolved policy still returns 200 from signature-status, reporting the
// blocking verdict instead of a 403.
func TestRouterManifestSignatureStatusIsNeverGatedByThePolicyItReports(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
	defer drainingCleanup(router, cleanup)()

	repository := "library/alpine"
	digest := seedSignatureStatusFixtureImage(t, store, repository)
	seedSignatureStatusPolicy(t, router, true, []string{signatureStatusFixtureTrustedKeyPEM(t)}) // enabled, no .sig published -> would 403 a pull

	pullReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest, nil)
	pullRecorder := httptest.NewRecorder()
	router.ServeHTTP(pullRecorder, pullReq)
	if pullRecorder.Code != http.StatusForbidden {
		t.Fatalf("pull status = %d, want %d (test setup sanity check: this digest must be blocked by the pull gate)", pullRecorder.Code, http.StatusForbidden)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest+"/signature-status", nil)
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("signature-status status = %d, want %d -- it must never itself be gated, body = %s", statusRecorder.Code, http.StatusOK, statusRecorder.Body.String())
	}

	var payload struct {
		State          string `json:"state"`
		WouldBlockPull bool   `json:"would_block_pull"`
	}
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response error = %v, body = %s", err, statusRecorder.Body.String())
	}
	if payload.State != "unsigned" || !payload.WouldBlockPull {
		t.Fatalf("payload = %#v, want state=unsigned would_block_pull=true", payload)
	}
}

// TestRouterSignatureStatusAndPullGate403NeverLeakKeyOrSignatureBytes is the
// Phase 7 RED test (tasks.md 7.7, the threat matrix's key-material-
// disclosure case): neither the signature-status response body nor the
// pull gate's 403 PolicyViolation body ever contains PEM key material or
// the raw base64 signature -- asserted across both endpoints.
func TestRouterSignatureStatusAndPullGate403NeverLeakKeyOrSignatureBytes(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
	defer drainingCleanup(router, cleanup)()

	repository := "library/alpine"
	digest := seedSignatureStatusFixtureImage(t, store, repository)
	seedSignatureStatusFixtureSignature(t, router, repository, digest)
	// An unrelated trusted key: the fixture signature will not validate
	// against it -- "untrusted" state, the shape the threat matrix's
	// "Reason" field example documents.
	seedSignatureStatusPolicy(t, router, true, []string{generateHTTPTestECDSAP256PublicKeyPEM(t)})

	pullReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest, nil)
	pullRecorder := httptest.NewRecorder()
	router.ServeHTTP(pullRecorder, pullReq)
	if pullRecorder.Code != http.StatusForbidden {
		t.Fatalf("pull status = %d, want %d (test setup sanity check)", pullRecorder.Code, http.StatusForbidden)
	}
	if strings.Contains(pullRecorder.Body.String(), "BEGIN PUBLIC KEY") || strings.Contains(pullRecorder.Body.String(), signatureStatusFixtureSignatureBase64) {
		t.Fatalf("pull gate 403 body = %s, must never echo key or signature bytes", pullRecorder.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest+"/signature-status", nil)
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("signature-status status = %d, want %d, body = %s", statusRecorder.Code, http.StatusOK, statusRecorder.Body.String())
	}
	if strings.Contains(statusRecorder.Body.String(), "BEGIN PUBLIC KEY") || strings.Contains(statusRecorder.Body.String(), signatureStatusFixtureSignatureBase64) {
		t.Fatalf("signature-status body = %s, must never echo key or signature bytes", statusRecorder.Body.String())
	}
}

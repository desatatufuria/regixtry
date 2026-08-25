package regixtryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
)

// TestAdminSigningKeyUsageRequiresAdminPrincipal is the same admin
// authorization boundary every other /admin/v1/signing-policy* route
// enforces (TestAdminSigningPolicyGetRequiresAdminPrincipal) -- no new
// permission surface for the key-usage advisory endpoint.
func TestAdminSigningKeyUsageRequiresAdminPrincipal(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	key := generateHTTPTestECDSAP256PublicKeyPEM(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy/key-usage?key="+url.QueryEscape(key), nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-admin caller to be rejected", recorder.Code)
	}
}

// TestAdminSigningKeyUsageRejectsMalformedKeyParam pins the validation
// boundary: an unparseable/non-PEM key query param is a domain validation
// error (422 in this codebase's writeAdminError mapping,
// domainregistry.ErrorCodeValidation), never a 500.
func TestAdminSigningKeyUsageRejectsMalformedKeyParam(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy/key-usage?key="+url.QueryEscape("not a pem at all"), nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for a malformed key param, body = %s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

// TestAdminSigningKeyUsageRejectsMissingKeyParam pins the same validation
// boundary for a wholly absent key param.
func TestAdminSigningKeyUsageRejectsMissingKeyParam(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy/key-usage", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for a missing key param, body = %s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

// TestAdminSigningKeyUsageReturnsZeroCountWhenNothingIsTagged is the empty-
// catalog happy path: a well-formed, unused key against a repository with
// no tags reports {count: 0, capped: false}.
func TestAdminSigningKeyUsageReturnsZeroCountWhenNothingIsTagged(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	key := generateHTTPTestECDSAP256PublicKeyPEM(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy/key-usage?key="+url.QueryEscape(key), nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var decoded struct {
		Count  int  `json:"count"`
		Capped bool `json:"capped"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(body) error = %v, body = %s", err, recorder.Body.String())
	}
	if decoded.Count != 0 || decoded.Capped {
		t.Fatalf("decoded = %#v, want {count: 0, capped: false}", decoded)
	}
}

// TestAdminSigningKeyUsageCountsAMatchingTaggedSignature is the wiring
// smoke test: a real tagged image whose legacy `.sig` signature verifies
// against the fixture key is reflected end to end through the HTTP
// endpoint -- the underlying matching logic itself is exhaustively covered
// at the service layer (service_signing_key_usage_test.go); this just pins
// that the route, query decoding, and JSON response actually connect.
func TestAdminSigningKeyUsageCountsAMatchingTaggedSignature(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	router := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	repository := "library/alpine"
	digest := seedSignatureStatusFixtureImage(t, store, repository)
	repo := domain.MustParseRepositoryRef(repository)
	manifest, err := store.ResolveManifest(context.Background(), "tenant-a", repo, digest)
	if err != nil {
		t.Fatalf("store.ResolveManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "latest", manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("store.PublishManifest(tag=latest) error = %v", err)
	}
	seedSignatureStatusFixtureSignature(t, router, repository, digest)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/signing-policy/key-usage?key="+url.QueryEscape(signatureStatusFixtureTrustedKeyPEM(t))+"&repository="+url.QueryEscape(repository), nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var decoded struct {
		Count  int  `json:"count"`
		Capped bool `json:"capped"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(body) error = %v, body = %s", err, recorder.Body.String())
	}
	if decoded.Count != 1 || decoded.Capped {
		t.Fatalf("decoded = %#v, want {count: 1, capped: false}", decoded)
	}
}

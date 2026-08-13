package regixtryhttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
)

// TestRouterManifestPushRoundTripsIdenticallyForCosignLegacySignatureTag is
// the Phase 10 integration test (tasks.md 10.1) proving the proposal's most
// important testable claim empirically, not by assertion: pushing a
// manifest at the cosign legacy signature-artifact tag convention
// (`sha256-<hex>.sig`) succeeds through the EXISTING manifest push path
// (handleManifest -> Service.PublishManifest) with ZERO signature-specific
// handling, behaving identically to an ordinary tag push of the same bytes.
//
// Phase 0 (tasks.md 0.1) recorded that no `cosign` binary was available in
// the apply environment, so this test substitutes a direct HTTP manifest PUT
// at the legacy tag for a genuine `cosign sign`/`cosign attach` invocation --
// noted explicitly here per task 10.1's fallback instruction. What this test
// DOES prove without any substitution: the push path itself (route
// dispatch, tag-name validation, blob-reference checks, storage, and the GET
// round trip) applies no special casing whatsoever to a tag that happens to
// match the `sha256-<hex>.sig` shape, exercised against the router exactly
// as any other manifest tag would be.
func TestRouterManifestPushRoundTripsIdenticallyForCosignLegacySignatureTag(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	layerContent := []byte("push-round-trip-layer")

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/"+repository+"/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadLocation := uploadStartRecorder.Header().Get("Location")
	if uploadLocation == "" {
		t.Fatal("blob upload start missing Location header")
	}

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewReader(layerContent))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)
	if appendRecorder.Code != http.StatusAccepted {
		t.Fatalf("blob upload append status = %d, want %d", appendRecorder.Code, http.StatusAccepted)
	}

	blobDigest := domain.DigestFromBytes(layerContent).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+blobDigest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("blob upload commit status = %d, want %d", commitRecorder.Code, http.StatusCreated)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blobDigest + `","size":22},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blobDigest + `","size":22}]}`)

	// legacySignatureTag is a syntactically valid cosign legacy signature
	// artifact tag (design.md Decision 1: SignatureTag maps sha256:<hex> ->
	// sha256-<hex>.sig) for a digest wholly unrelated to manifestPayload's
	// own digest -- exactly what a real `cosign sign` push targets: a tag
	// name derived from the SIGNED image's digest, not the signature
	// manifest's own digest.
	legacySignatureTag, err := signing.SignatureTag("sha256:2222222222222222222222222222222222222222222222222222222222222222")
	if err != nil {
		t.Fatalf("signing.SignatureTag() error = %v", err)
	}

	tests := []struct {
		name string
		tag  string
	}{
		{name: "ordinary tag", tag: "latest"},
		{name: "cosign legacy signature-artifact tag", tag: legacySignatureTag},
	}

	var digests []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			putReq := httptest.NewRequest(http.MethodPut, "/v2/"+repository+"/manifests/"+tt.tag, bytes.NewReader(manifestPayload))
			putReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			putRecorder := httptest.NewRecorder()
			handler.ServeHTTP(putRecorder, putReq)
			if putRecorder.Code != http.StatusCreated {
				t.Fatalf("push manifest at tag %q status = %d, want %d, body = %s", tt.tag, putRecorder.Code, http.StatusCreated, putRecorder.Body.String())
			}
			pushedDigest := putRecorder.Header().Get("Docker-Content-Digest")
			if pushedDigest == "" {
				t.Fatalf("push manifest at tag %q missing Docker-Content-Digest header", tt.tag)
			}
			digests = append(digests, pushedDigest)

			getReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+tt.tag, nil)
			getRecorder := httptest.NewRecorder()
			handler.ServeHTTP(getRecorder, getReq)
			if getRecorder.Code != http.StatusOK {
				t.Fatalf("get manifest at tag %q status = %d, want %d", tt.tag, getRecorder.Code, http.StatusOK)
			}
			if getRecorder.Header().Get("Docker-Content-Digest") != pushedDigest {
				t.Fatalf("get manifest at tag %q Docker-Content-Digest = %q, want %q", tt.tag, getRecorder.Header().Get("Docker-Content-Digest"), pushedDigest)
			}
			if getRecorder.Header().Get("Content-Type") != "application/vnd.oci.image.manifest.v1+json" {
				t.Fatalf("get manifest at tag %q Content-Type = %q, want application/vnd.oci.image.manifest.v1+json", tt.tag, getRecorder.Header().Get("Content-Type"))
			}
			if !bytes.Equal(getRecorder.Body.Bytes(), manifestPayload) {
				t.Fatalf("get manifest at tag %q body = %s, want byte-identical to the pushed payload", tt.tag, getRecorder.Body.String())
			}
		})
	}

	// Both pushes referenced the exact same manifest bytes, so the router
	// must have computed the exact same content digest for both tags --
	// proof that tag naming (including the legacy .sig shape) has zero
	// bearing on how the existing push path parses, stores, or serves a
	// manifest.
	if len(digests) == 2 && digests[0] != digests[1] {
		t.Fatalf("pushed digests = %v, want identical digests for identical manifest bytes regardless of tag", digests)
	}

	tagsReq := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/tags/list", nil)
	tagsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tagsRecorder, tagsReq)
	if tagsRecorder.Code != http.StatusOK {
		t.Fatalf("tags list status = %d, want %d", tagsRecorder.Code, http.StatusOK)
	}
	if !bytes.Contains(tagsRecorder.Body.Bytes(), []byte(legacySignatureTag)) {
		t.Fatalf("tags list body = %s, want it to contain the pushed legacy signature tag %q -- proving it is an ordinary tag like any other, not silently filtered or special-cased", tagsRecorder.Body.String(), legacySignatureTag)
	}
	if !bytes.Contains(tagsRecorder.Body.Bytes(), []byte("latest")) {
		t.Fatalf("tags list body = %s, want it to contain \"latest\"", tagsRecorder.Body.String())
	}
}

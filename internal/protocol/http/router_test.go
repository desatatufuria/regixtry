package registryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appregistry "registry/internal/app/registry"
	domain "registry/internal/domain/registry"
	metadata "registry/internal/infra/metadata/sqlite"
	"registry/internal/infra/storage/fsblob"
	"registry/internal/ports"
)

func TestRouterChallengesProtectedPull(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	if got := recorder.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected WWW-Authenticate header")
	}
}

func TestRouterUploadAndReadFlow(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	if uploadStartRecorder.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, want %d", uploadStartRecorder.Code, http.StatusAccepted)
	}

	uploadLocation := uploadStartRecorder.Header().Get("Location")
	uploadID := uploadStartRecorder.Header().Get("Docker-Upload-UUID")
	if uploadLocation == "" || uploadID == "" {
		t.Fatal("expected upload location and UUID headers")
	}

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)
	if appendRecorder.Code != http.StatusAccepted {
		t.Fatalf("append status = %d, want %d", appendRecorder.Code, http.StatusAccepted)
	}

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d", commitRecorder.Code, http.StatusCreated)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}

	getManifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	getManifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getManifestRecorder, getManifestReq)
	if getManifestRecorder.Code != http.StatusOK {
		t.Fatalf("get manifest status = %d, want %d", getManifestRecorder.Code, http.StatusOK)
	}

	var catalog struct {
		Repositories []string `json:"repositories"`
	}
	catalogReq := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	catalogRecorder := httptest.NewRecorder()
	handler.ServeHTTP(catalogRecorder, catalogReq)
	if catalogRecorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, want %d", catalogRecorder.Code, http.StatusOK)
	}
	if err := json.Unmarshal(catalogRecorder.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("catalog JSON error = %v", err)
	}
	if len(catalog.Repositories) != 1 || catalog.Repositories[0] != "library/alpine" {
		t.Fatalf("catalog.Repositories = %#v, want [library/alpine]", catalog.Repositories)
	}

	tagsReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/tags/list", nil)
	tagsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tagsRecorder, tagsReq)
	if tagsRecorder.Code != http.StatusOK {
		t.Fatalf("tags status = %d, want %d", tagsRecorder.Code, http.StatusOK)
	}
}

func TestRouterRejectsDigestMismatchOnBlobCommit(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	startReq := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startReq)
	uploadLocation := startRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	wrongDigest := domain.DigestFromBytes([]byte("different-payload")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+wrongDigest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	if commitRecorder.Code != http.StatusBadRequest {
		t.Fatalf("commit status = %d, want %d", commitRecorder.Code, http.StatusBadRequest)
	}

	blobReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/blobs/"+wrongDigest, nil)
	blobRecorder := httptest.NewRecorder()
	handler.ServeHTTP(blobRecorder, blobReq)
	if blobRecorder.Code != http.StatusNotFound {
		t.Fatalf("blob status = %d, want %d", blobRecorder.Code, http.StatusNotFound)
	}
}

func TestRouterAllowsAnonymousPullWhenConfigured(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()

	seedHandler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{})
	digest := seedPublishedManifest(t, seedHandler)
	readOnlyHandler := newRouterWithStores(blobStore, metadataStore, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPull: true}))

	manifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	manifestRecorder := httptest.NewRecorder()
	readOnlyHandler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusOK)
	}

	blobReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/blobs/"+digest, nil)
	blobRecorder := httptest.NewRecorder()
	readOnlyHandler.ServeHTTP(blobRecorder, blobReq)
	if blobRecorder.Code != http.StatusOK {
		t.Fatalf("blob status = %d, want %d", blobRecorder.Code, http.StatusOK)
	}
}

func TestRouterRejectsManifestWithMissingBlob(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	digest := "sha256:8a5a3d2cfb08cf0c22848f3322a7fd6f1300a0a176c1f907fdbd53f5b5d2e236"
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)

	if manifestRecorder.Code != http.StatusConflict {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusConflict)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusNotFound {
		t.Fatalf("get manifest status = %d, want %d", getRecorder.Code, http.StatusNotFound)
	}
}

func TestRouterKeepsIncompleteUploadsInvisibleFromPublishedContent(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	startReq := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startReq)
	uploadLocation := startRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	var catalog struct {
		Repositories []string `json:"repositories"`
	}
	catalogReq := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	catalogRecorder := httptest.NewRecorder()
	handler.ServeHTTP(catalogRecorder, catalogReq)

	if catalogRecorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, want %d", catalogRecorder.Code, http.StatusOK)
	}
	if err := json.Unmarshal(catalogRecorder.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("catalog JSON error = %v", err)
	}
	if len(catalog.Repositories) != 0 {
		t.Fatalf("catalog.Repositories = %#v, want empty", catalog.Repositories)
	}

	manifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusNotFound {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusNotFound)
	}
}

func seedPublishedManifest(t *testing.T, handler *Router) string {
	t.Helper()

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
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}

	return digest
}

func newTestRouter(t *testing.T, accessController ports.AccessController) (*Router, func()) {
	t.Helper()

	blobStore, metadataStore, cleanup := newTestStores(t)
	return newRouterWithStores(blobStore, metadataStore, accessController), cleanup
}

func newTestStores(t *testing.T) (*fsblob.Store, *metadata.Store, func()) {
	t.Helper()

	rootDir := t.TempDir()
	blobStore, err := fsblob.New(filepath.Join(rootDir, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}

	return blobStore, metadataStore, func() {
		_ = metadataStore.Close()
	}
}

func newRouterWithStores(blobStore *fsblob.Store, metadataStore *metadata.Store, accessController ports.AccessController) *Router {
	service := appregistry.NewService(
		blobStore,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver("tenant-a"),
		ports.NewInlineJobRunner(),
	)

	return NewRouter(service)
}

type allowAllAccessController struct{}

func (allowAllAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (allowAllAccessController) Challenge() ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "registry", Service: "registry"}
}

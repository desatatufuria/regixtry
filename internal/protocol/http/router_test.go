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

func newTestRouter(t *testing.T, accessController ports.AccessController) (*Router, func()) {
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

	service := appregistry.NewService(
		blobStore,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver("tenant-a"),
		ports.NewInlineJobRunner(),
	)

	return NewRouter(service), func() {
		_ = metadataStore.Close()
	}
}

type allowAllAccessController struct{}

func (allowAllAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (allowAllAccessController) Challenge() ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "registry", Service: "registry"}
}

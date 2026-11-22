package regixtryhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

type ociErrorBody struct {
	Errors []struct {
		Code string `json:"code"`
	} `json:"errors"`
}

func ociErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body ociErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || len(body.Errors) == 0 {
		t.Fatalf("response body %q is not an OCI error body: %v", recorder.Body.String(), err)
	}
	return body.Errors[0].Code
}

func manifestWithPadding(blobDigest string, size int, padding int) []byte {
	payload := routerManifestPayload(blobDigest, size)
	annotations := `,"annotations":{"pad":"` + strings.Repeat("a", padding) + `"}}`
	return append(bytes.TrimSuffix(payload, []byte("}")), annotations...)
}

func seedBlob(t *testing.T, handler *Router, repository string, content string) string {
	t.Helper()
	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/"+repository+"/blobs/uploads/", nil))
	digest := domain.DigestFromBytes([]byte(content)).String()
	put := httptest.NewRecorder()
	handler.ServeHTTP(put, httptest.NewRequest(http.MethodPut, start.Header().Get("Location")+"?digest="+digest, strings.NewReader(content)))
	if put.Code != http.StatusCreated {
		t.Fatalf("seed blob status = %d, want 201", put.Code)
	}
	return digest
}

// endlessBody never ends: without a cap, io.ReadAll on it never returns and
// allocates until the process dies.
type endlessBody struct{ read int64 }

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	b.read += int64(len(p))
	return len(p), nil
}

func TestRouterRejectsManifestOverTheDefaultCapWithoutBufferingIt(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	body := &endlessBody{}
	request := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", io.NopCloser(body))
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}
	if code := ociErrorCode(t, recorder); code != "SIZE_INVALID" {
		t.Fatalf("error code = %q, want SIZE_INVALID", code)
	}
	if body.read > 2*DefaultMaxManifestBytes {
		t.Fatalf("handler consumed %d bytes of an endless body, want it to stop shortly after %d", body.read, DefaultMaxManifestBytes)
	}
}

func TestRouterAcceptsLargeManifestUnderTheCapAndRejectsOneJustOverIt(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()
	blobDigest := seedBlob(t, handler, "library/alpine", "cap-layer")

	under := manifestWithPadding(blobDigest, len("cap-layer"), 3<<20)
	if int64(len(under)) >= DefaultMaxManifestBytes {
		t.Fatalf("fixture too large: %d", len(under))
	}
	request := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/big", bytes.NewReader(under))
	request.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("under-cap manifest status = %d, want 201 (%s)", recorder.Code, recorder.Body.String())
	}

	over := manifestWithPadding(blobDigest, len("cap-layer"), int(DefaultMaxManifestBytes))
	request = httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/toobig", bytes.NewReader(over))
	request.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-cap manifest status = %d, want 413", recorder.Code)
	}
}

func TestRouterManifestCapIsConfigurableAndCanBeDisabled(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()
	small := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil, WithMaxManifestBytes(512))
	blobDigest := seedBlob(t, small, "library/alpine", "cfg-layer")

	request := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/a", bytes.NewReader(manifestWithPadding(blobDigest, len("cfg-layer"), 1024)))
	request.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	recorder := httptest.NewRecorder()
	small.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("custom cap status = %d, want 413", recorder.Code)
	}

	unlimited := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil, WithMaxManifestBytes(0))
	request = httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/b", bytes.NewReader(manifestWithPadding(blobDigest, len("cfg-layer"), 5<<20)))
	request.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	recorder = httptest.NewRecorder()
	unlimited.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("disabled cap status = %d, want 201", recorder.Code)
	}
}

func TestRouterBlobUploadOverTheMaxSizeIsRejectedWhileStreaming(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobStore, err := fsblob.New(filepath.Join(rootDir, "content"), fsblob.WithMaxUploadSize(1024))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	handler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	defer drainingCleanup(handler, func() { _ = metadataStore.Close() })()

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	location := start.Header().Get("Location")

	// Endless PATCH body: must stop at the cap instead of filling the disk.
	body := &endlessBody{}
	request := httptest.NewRequest(http.MethodPatch, location, io.NopCloser(body))
	request.ContentLength = -1
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("endless PATCH status = %d, want 413", recorder.Code)
	}
	if code := ociErrorCode(t, recorder); code != "SIZE_INVALID" {
		t.Fatalf("error code = %q, want SIZE_INVALID", code)
	}

	// Chunk accumulation: 600 + 600 > 1024, the second chunk is refused and
	// the upload keeps its first 600 bytes.
	start = httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	location = start.Header().Get("Location")
	chunk := strings.Repeat("c", 600)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPatch, location, strings.NewReader(chunk)))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first chunk status = %d, want 202", first.Code)
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPatch, location, strings.NewReader(chunk)))
	if second.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("second chunk status = %d, want 413", second.Code)
	}
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, location, nil))
	if got := status.Header().Get("Range"); got != "0-599" {
		t.Fatalf("Range after rejected chunk = %q, want 0-599", got)
	}

	// A final PUT chunk that would overflow is also refused.
	put := httptest.NewRecorder()
	handler.ServeHTTP(put, httptest.NewRequest(http.MethodPut, location+"?digest="+domain.DigestFromBytes([]byte(chunk+chunk)).String(), strings.NewReader(chunk)))
	if put.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("overflowing PUT status = %d, want 413", put.Code)
	}
}

func TestRouterCancelUploadRoute(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	location := start.Header().Get("Location")
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, location, strings.NewReader("partial")))

	// Another repository cannot cancel it.
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, httptest.NewRequest(http.MethodDelete, strings.Replace(location, "library/alpine", "library/other", 1), nil))
	if other.Code != http.StatusNotFound {
		t.Fatalf("cross-repository DELETE status = %d, want 404", other.Code)
	}

	cancel := httptest.NewRecorder()
	handler.ServeHTTP(cancel, httptest.NewRequest(http.MethodDelete, location, nil))
	if cancel.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", cancel.Code)
	}

	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, location, strings.NewReader("x")))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s after cancel status = %d, want 404", method, recorder.Code)
		}
	}
	commit := httptest.NewRecorder()
	handler.ServeHTTP(commit, httptest.NewRequest(http.MethodPut, location+"?digest="+domain.DigestFromBytes([]byte("partial")).String(), nil))
	if commit.Code != http.StatusNotFound {
		t.Fatalf("PUT after cancel status = %d, want 404", commit.Code)
	}
}

func TestRouterCancelUploadRequiresPushOnTheUploadsRepository(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()

	seed := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	start := httptest.NewRecorder()
	seed.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/team/app/blobs/uploads/", nil))
	location := start.Header().Get("Location")

	tests := []struct {
		name       string
		repository string
		actions    []string
		role       domainauth.RepoRole
		want       int
	}{
		{name: "pull-only token is refused", repository: "team/app", actions: []string{"pull"}, role: domainauth.RepoRoleReader, want: http.StatusUnauthorized},
		{name: "push on another repository is refused", repository: "team/elsewhere", actions: []string{"pull", "push"}, role: domainauth.RepoRoleWriter, want: http.StatusUnauthorized},
		{name: "push on the uploads repository cancels", repository: "team/app", actions: []string{"pull", "push"}, role: domainauth.RepoRoleWriter, want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal := domainauth.Principal{
				Subject:  "atk_cancel",
				Username: "writer",
				Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef(tt.repository), Role: tt.role}},
				Scopes:   []domainauth.Scope{{Type: "repository", Name: tt.repository, Actions: tt.actions, Canonical: "repository:" + tt.repository + ":" + strings.Join(tt.actions, ",")}},
			}
			handler := newRouterWithStores(blobStore, metadataStore, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{verify: &principal})
			request := httptest.NewRequest(http.MethodDelete, location, nil)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.want {
				t.Fatalf("DELETE status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestRouterRefusesUploadsOverTheOpenUploadCap(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()
	handler.service.SetUploadLimits(0, 1)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first upload status = %d, want 202", first.Code)
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second upload status = %d, want 429", second.Code)
	}
	if code := ociErrorCode(t, second); code != "TOOMANYREQUESTS" {
		t.Fatalf("error code = %q, want TOOMANYREQUESTS", code)
	}

	// Cancelling the first frees the slot.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, first.Header().Get("Location"), nil))
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	if third.Code != http.StatusAccepted {
		t.Fatalf("upload after cancel status = %d, want 202", third.Code)
	}
}

func TestRouterCancelRemovesTempFile(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobStore, err := fsblob.New(filepath.Join(rootDir, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	handler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	defer drainingCleanup(handler, func() { _ = metadataStore.Close() })()

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil))
	location := start.Header().Get("Location")
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, location, strings.NewReader("partial")))

	uploadsDir := filepath.Join(rootDir, "content", "uploads")
	entries, _ := os.ReadDir(uploadsDir)
	if len(entries) != 1 {
		t.Fatalf("uploads dir entries = %d, want 1", len(entries))
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, location, nil))
	entries, _ = os.ReadDir(uploadsDir)
	if len(entries) != 0 {
		t.Fatalf("uploads dir entries after cancel = %d, want 0", len(entries))
	}
}

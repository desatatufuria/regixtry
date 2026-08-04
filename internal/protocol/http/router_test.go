package registryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appregistry "registry/internal/app/registry"
	domainauth "registry/internal/domain/auth"
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

	seedHandler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	digest := seedPublishedManifest(t, seedHandler)
	readOnlyHandler := newRouterWithStores(blobStore, metadataStore, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPull: true}), nil)

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

func TestRouterRejectsAnonymousPushByDefault(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "registry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) || !strings.Contains(challenge, `scope="repository:library/alpine:pull,push"`) {
		t.Fatalf("WWW-Authenticate = %q, want configured token realm URL with combined push scope", challenge)
	}
}

func TestRouterChallengesUnauthenticatedV2PingWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "registry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) || !strings.Contains(challenge, `service="registry"`) {
		t.Fatalf("WWW-Authenticate = %q, want bearer challenge for /v2/ ping", challenge)
	}
	if strings.Contains(challenge, `scope=`) {
		t.Fatalf("WWW-Authenticate = %q, did not expect scope on /v2/ ping challenge", challenge)
	}
}

func TestRouterAcceptsAuthenticatedV2PingWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "registry"}), fakeAuthService{
		verify: &domainauth.Principal{Subject: "user-1", Username: "alice"},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want empty on authenticated ping", got)
	}
}

func TestRouterKeepsV2PingOpenWhenAuthDisabled(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want empty when auth is disabled", got)
	}
}

func TestRouterLogsUnauthorizedChallengeDetails(t *testing.T) {
	var logs bytes.Buffer
	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "registry"}), fakeAuthService{}, WithLogger(log.New(&logs, "", 0)))
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	output := logs.String()
	if !strings.Contains(output, "method=POST") || !strings.Contains(output, "path=/v2/library/alpine/blobs/uploads/") || !strings.Contains(output, "status=401") {
		t.Fatalf("log output = %q, want method/path/status details", output)
	}
	if !strings.Contains(output, `auth_challenge="Bearer realm=\"http://127.0.0.1:5000/auth/token\"`) || !strings.Contains(output, `scope=\"repository:library/alpine:pull,push\"`) {
		t.Fatalf("log output = %q, want challenge hints", output)
	}
	if !strings.Contains(output, "duration=") {
		t.Fatalf("log output = %q, want duration", output)
	}
}

func TestRouterLogsRequestPathWithQueryString(t *testing.T) {
	var logs bytes.Buffer
	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}), fakeAuthService{
		loginResult: ports.LoginResult{BearerToken: "issued-token", ExpiresAt: time.Date(2026, 1, 2, 3, 19, 5, 0, time.UTC)},
	}, WithLogger(log.New(&logs, "", 0)))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=registry&scope=repository:team/app:pull", nil)
	req.SetBasicAuth("alice", "password123")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	output := logs.String()
	if !strings.Contains(output, "path=/auth/token?service=registry&scope=repository:team/app:pull") || !strings.Contains(output, "status=200") {
		t.Fatalf("log output = %q, want request URI and status", output)
	}
	if strings.Contains(output, "auth_challenge=") {
		t.Fatalf("log output = %q, did not expect auth challenge on success", output)
	}
}

func TestRouterIssuesAccessTokenFromBasicCredentials(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}), fakeAuthService{
		loginResult: ports.LoginResult{BearerToken: "issued-token", ExpiresAt: time.Date(2026, 1, 2, 3, 19, 5, 0, time.UTC), Scope: "repository:team/app:pull"},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=registry&scope=repository:team/app:pull", nil)
	req.SetBasicAuth("alice", "password123")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["token"] != "issued-token" {
		t.Fatalf("token = %#v, want issued-token", payload["token"])
	}
	if payload["scope"] != "repository:team/app:pull" {
		t.Fatalf("scope = %#v, want repository:team/app:pull", payload["scope"])
	}
}

func TestRouterRejectsPushWhenBearerScopeIsPullOnly(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}), fakeAuthService{
		verify: &domainauth.Principal{
			Subject:  "atk_1",
			Username: "alice",
			Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
			Scopes:   []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}},
		},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/team/app/blobs/uploads/", nil)
	req.Header.Set("Authorization", "Bearer pull-only-token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRouterRejectsMalformedTokenScopeRequests(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=registry&scope=repository:team/app", nil)
	req.SetBasicAuth("alice", "password123")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRouterChallengesProtectedPullWithConfiguredTokenRealm(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "registry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/team/app/manifests/latest", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) || !strings.Contains(challenge, `scope="repository:team/app:pull"`) {
		t.Fatalf("WWW-Authenticate = %q, want configured token realm URL with pull scope", challenge)
	}
}

func TestRouterRejectsInvalidBearerTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "expired", err: domainauth.NewExpiredTokenError("atk_expired")},
		{name: "revoked", err: domainauth.NewRevokedTokenError("atk_revoked")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}), fakeAuthService{verifyErr: tt.err})
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, "/v2/team/app/tags/list", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `scope="repository:team/app:pull"`) || !strings.Contains(got, `error="invalid_token"`) {
				t.Fatalf("WWW-Authenticate = %q, want scoped invalid_token challenge", got)
			}
		})
	}
}

func TestRouterAllowsAnonymousPushWhenConfigured(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPush: true}))
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
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
	return newRouterWithStores(blobStore, metadataStore, accessController, nil), cleanup
}

func newTestRouterWithAuth(t *testing.T, accessController ports.AccessController, authService ports.AuthService, options ...RouterOption) (*Router, func()) {
	t.Helper()

	blobStore, metadataStore, cleanup := newTestStores(t)
	return newRouterWithStores(blobStore, metadataStore, accessController, authService, options...), cleanup
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

func newRouterWithStores(blobStore *fsblob.Store, metadataStore *metadata.Store, accessController ports.AccessController, authService ports.AuthService, options ...RouterOption) *Router {
	service := appregistry.NewService(
		blobStore,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver("tenant-a"),
		ports.NewInlineJobRunner(),
	)

	return NewRouter(service, authService, options...)
}

type allowAllAccessController struct{}

func (allowAllAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (allowAllAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "registry", Service: "registry"}
}

type fakeAuthService struct {
	loginResult ports.LoginResult
	loginErr    error
	verify      *domainauth.Principal
	verifyErr   error
	lastScopes  []domainauth.Scope
}

func (f fakeAuthService) EnsureBootstrapAdmin(context.Context) error { return nil }
func (f fakeAuthService) ListUsers(context.Context, domainauth.Principal) ([]domainauth.User, error) {
	return nil, nil
}
func (f fakeAuthService) CreateUser(context.Context, domainauth.Principal, ports.CreateUserInput) (domainauth.User, error) {
	return domainauth.User{}, nil
}
func (f fakeAuthService) UpdateUser(context.Context, domainauth.Principal, ports.UpdateUserInput) (domainauth.User, error) {
	return domainauth.User{}, nil
}
func (f fakeAuthService) SetUserEnabled(context.Context, domainauth.Principal, string, bool) (domainauth.User, error) {
	return domainauth.User{}, nil
}
func (f fakeAuthService) DeleteUser(context.Context, domainauth.Principal, string) error { return nil }
func (f fakeAuthService) BootstrapAdmin(context.Context, ports.BootstrapAdminInput) (ports.BootstrapAdminResult, error) {
	return ports.BootstrapAdminResult{}, nil
}

func (f fakeAuthService) LoginWithPassword(_ context.Context, _ string, _ string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	f.lastScopes = append([]domainauth.Scope(nil), requestedScopes...)
	return f.loginResult, f.loginErr
}
func (f fakeAuthService) LoginWithPreissuedToken(_ context.Context, _ string, _ string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	f.lastScopes = append([]domainauth.Scope(nil), requestedScopes...)
	return f.loginResult, f.loginErr
}
func (f fakeAuthService) VerifyAccessToken(context.Context, string) (domainauth.Principal, error) {
	if f.verifyErr != nil {
		return domainauth.Principal{}, f.verifyErr
	}
	if f.verify == nil {
		return domainauth.Principal{}, nil
	}
	return *f.verify, nil
}
func (f fakeAuthService) CreateAdminToken(context.Context, domainauth.Principal, ports.CreateAdminTokenInput) (ports.CreatedAdminToken, error) {
	return ports.CreatedAdminToken{}, nil
}
func (f fakeAuthService) ListRepoGrants(context.Context, domainauth.Principal, string) ([]domainauth.RepoGrant, error) {
	return nil, nil
}
func (f fakeAuthService) ListAdminTokens(context.Context, domainauth.Principal, string) ([]domainauth.Token, error) {
	return nil, nil
}
func (f fakeAuthService) RevokeAdminToken(context.Context, domainauth.Principal, string) error {
	return nil
}
func (f fakeAuthService) ResetPassword(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) PutRepoGrant(context.Context, domainauth.Principal, string, string, domainauth.RepoRole) (domainauth.RepoGrant, error) {
	return domainauth.RepoGrant{}, nil
}
func (f fakeAuthService) DeleteRepoGrant(context.Context, domainauth.Principal, string, string) error {
	return nil
}

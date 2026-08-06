package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appregistry "registry/internal/app/registry"
	authpostgres "registry/internal/infra/auth/postgres"
	metadata "registry/internal/infra/metadata/sqlite"
	"registry/internal/infra/storage/fsblob"
	"registry/internal/ports"
)

func TestParseServeConfigAnonymousAccessDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig(nil)
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.AllowAnonymousPull {
		t.Fatal("AllowAnonymousPull = true, want false")
	}

	if cfg.AllowAnonymousPush {
		t.Fatal("AllowAnonymousPush = true, want false")
	}
}

func TestParseServeConfigAllowsAnonymousPushFlag(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{"-allow-anonymous-push", "-allow-anonymous-pull"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if !cfg.AllowAnonymousPull {
		t.Fatal("AllowAnonymousPull = false, want true")
	}

	if !cfg.AllowAnonymousPush {
		t.Fatal("AllowAnonymousPush = false, want true")
	}
}

func TestParseServeConfigParsesAuthPostgresDSN(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{"-auth-postgres-dsn", "postgres://auth"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.AuthPostgresDSN != "postgres://auth" {
		t.Fatalf("AuthPostgresDSN = %q, want %q", cfg.AuthPostgresDSN, "postgres://auth")
	}
}

func TestParseServeConfigParsesAuthTokenRealmURL(t *testing.T) {
	t.Parallel()

	cfg, err := parseServeConfig([]string{"-auth-token-realm", "http://127.0.0.1:5000/auth/token"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if cfg.AuthTokenRealmURL != "http://127.0.0.1:5000/auth/token" {
		t.Fatalf("AuthTokenRealmURL = %q, want %q", cfg.AuthTokenRealmURL, "http://127.0.0.1:5000/auth/token")
	}
}

func TestParseTUIConfigParsesAdminAPIBaseURL(t *testing.T) {
	t.Parallel()

	cfg, err := parseTUIConfig([]string{"-api-base-url", "http://127.0.0.1:5000/"})
	if err != nil {
		t.Fatalf("parseTUIConfig() error = %v", err)
	}

	if cfg.APIBaseURL != "http://127.0.0.1:5000" {
		t.Fatalf("APIBaseURL = %q, want %q", cfg.APIBaseURL, "http://127.0.0.1:5000")
	}
}

func TestParseTUIConfigRejectsRelativeAdminAPIBaseURL(t *testing.T) {
	t.Parallel()

	_, err := parseTUIConfig([]string{"-api-base-url", "/admin"})
	if err == nil {
		t.Fatal("parseTUIConfig() error = nil, want invalid admin API base URL")
	}
	if !strings.Contains(err.Error(), "absolute http(s) URL") {
		t.Fatalf("parseTUIConfig() error = %v, want absolute URL validation", err)
	}
}

func TestServeStartsAndRespondsToPing(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, listener, serveConfig{
			StorageRoot:        t.TempDir(),
			DatabasePath:       filepath.Join(t.TempDir(), "registry.db"),
			AllowAnonymousPull: true,
		}, stdout)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/v2/")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", requestErr)
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("serve() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "registry serving on") {
		t.Fatalf("stdout = %q, want start message", stdout.String())
	}
}

func TestNewHandlerAnonymousPushWiring(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		allowAnonymousPush bool
		wantStatus         int
	}{
		{
			name:       "anonymous push disabled stays unauthorized",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:               "anonymous push enabled allows upload start",
			allowAnonymousPush: true,
			wantStatus:         http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			storageRoot := t.TempDir()
			handler, cleanup, err := newHandler(serveConfig{
				StorageRoot:        storageRoot,
				DatabasePath:       filepath.Join(storageRoot, "registry.db"),
				AllowAnonymousPush: tt.allowAnonymousPush,
			})
			if err != nil {
				t.Fatalf("newHandler() error = %v", err)
			}
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusAccepted {
				if got := recorder.Header().Get("Location"); !strings.HasPrefix(got, "/v2/library/alpine/blobs/uploads/") {
					t.Fatalf("Location = %q, want upload location", got)
				}
			}
		})
	}
}

func TestNewHandlerFailsFastWhenAuthEnabledWithoutAdmin(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	storageRoot := t.TempDir()
	_, cleanup, err := newHandler(serveConfig{
		StorageRoot:     storageRoot,
		DatabasePath:    filepath.Join(storageRoot, "registry.db"),
		AuthPostgresDSN: filepath.Join(t.TempDir(), "auth.db"),
	})
	if err == nil {
		cleanup()
		t.Fatal("newHandler() error = nil, want bootstrap admin failure")
	}
	if !strings.Contains(err.Error(), "bootstrap-admin") {
		t.Fatalf("newHandler() error = %v, want bootstrap-admin guidance", err)
	}
}

func TestRunBootstrapAdminIsIdempotent(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	stdout := &bytes.Buffer{}
	args := []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}
	if err := run(context.Background(), args, stdout, io.Discard); err != nil {
		t.Fatalf("run(first bootstrap) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "bootstrapped global admin") {
		t.Fatalf("stdout = %q, want bootstrap message", stdout.String())
	}

	stdout.Reset()
	if err := run(context.Background(), args, stdout, io.Discard); err != nil {
		t.Fatalf("run(second bootstrap) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "already configured") {
		t.Fatalf("stdout = %q, want idempotent message", stdout.String())
	}

	storageRoot := t.TempDir()
	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:        storageRoot,
		DatabasePath:       filepath.Join(storageRoot, "registry.db"),
		AllowAnonymousPull: true,
		AuthPostgresDSN:    authDB,
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if challenge := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want Bearer challenge", challenge)
	}
}

func TestNewHandlerUsesConfiguredAuthTokenRealmInChallenge(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	if err := run(context.Background(), []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}

	storageRoot := t.TempDir()
	handler, cleanup, err := newHandler(serveConfig{
		StorageRoot:       storageRoot,
		DatabasePath:      filepath.Join(storageRoot, "registry.db"),
		AuthPostgresDSN:   authDB,
		AuthTokenRealmURL: "http://127.0.0.1:5000/auth/token",
	})
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	defer cleanup()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v2/team/app/tags/list", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) {
		t.Fatalf("WWW-Authenticate = %q, want configured token realm URL", challenge)
	}
}

func TestRunTUIRendersRepositorySnapshot(t *testing.T) {
	t.Parallel()

	storageRoot := t.TempDir()
	databasePath := filepath.Join(storageRoot, "registry.db")
	seedRegistryState(t, storageRoot, databasePath)

	stdout := &bytes.Buffer{}
	err := runTUI(tuiConfig{StorageRoot: storageRoot, DatabasePath: databasePath, Tenant: "tenant-a", Snapshot: true}, strings.NewReader("q"), stdout)
	if err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}

	view := stdout.String()
	if !strings.Contains(view, "Registry Console") || !strings.Contains(view, "library/alpine") {
		t.Fatalf("stdout = %q, want rendered repository view", view)
	}
}

func TestRunTUIDisablesAuthAdminShortcutWhenAuthIsEnabled(t *testing.T) {
	restore := swapAuthStoreOpener(t)
	defer restore()

	authDB := filepath.Join(t.TempDir(), "auth.db")
	if err := run(context.Background(), []string{"bootstrap-admin", "-auth-postgres-dsn", authDB, "-username", "admin", "-password", "change-me-now"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(bootstrap-admin) error = %v", err)
	}

	storageRoot := t.TempDir()
	databasePath := filepath.Join(storageRoot, "registry.db")
	seedRegistryState(t, storageRoot, databasePath)

	stdout := &bytes.Buffer{}
	err := runTUI(tuiConfig{StorageRoot: storageRoot, DatabasePath: databasePath, Tenant: "tenant-a", AuthPostgresDSN: authDB, Snapshot: true}, strings.NewReader("q"), stdout)
	if err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}

	view := stdout.String()
	if strings.Contains(view, "a: admin") {
		t.Fatalf("stdout = %q, want auth admin shortcut removed", view)
	}
	if strings.Contains(view, "Auth-backed admin actions are disabled in the local TUI until a real operator login flow exists.") {
		t.Fatalf("stdout = %q, want local admin shortcut notice removed", view)
	}
	if !strings.Contains(view, "library/alpine") {
		t.Fatalf("stdout = %q, want repository snapshot to stay available", view)
	}
}

func seedRegistryState(t *testing.T, storageRoot string, databasePath string) {
	t.Helper()

	blobStore, err := fsblob.New(filepath.Join(storageRoot, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(databasePath)
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	service := appregistry.NewService(blobStore, metadataStore, localOperatorAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	upload, err := service.BeginUpload(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	digest := "sha256:0139c1c77468f75e6763a4612262743bd47a36b26cb2863d662756b3377bb029"
	if _, err := service.CompleteUpload(context.Background(), "library/alpine", upload.ID, digest, nil); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	if _, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifest); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
}

func swapAuthStoreOpener(t *testing.T) func() {
	t.Helper()

	previous := openAuthStore
	openAuthStore = func(dsn string) (ports.AuthStore, error) {
		return authpostgres.NewWithDriver("sqlite", dsn)
	}

	return func() {
		openAuthStore = previous
	}
}

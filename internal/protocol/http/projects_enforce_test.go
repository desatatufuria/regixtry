package regixtryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appauth "regixtry/internal/app/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

type enforceRig struct {
	blobs *fsblob.Store
	store *metadata.Store
}

// newEnforceRig returns stores built WITH the store-level requirement; routers
// over it choose their own service flag, so a test can push "before" the
// requirement existed (router with require=false) and then enforce.
func newEnforceRig(t *testing.T, requireInStore bool) enforceRig {
	t.Helper()
	root := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	store, err := metadata.New(filepath.Join(root, "registry.db"), metadata.WithRequireProject(requireInStore))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return enforceRig{blobs: blobs, store: store}
}

func (r enforceRig) router(t *testing.T, controller ports.AccessController, require bool) *Router {
	t.Helper()
	router := newRouterWithStores(r.blobs, r.store, controller, nil)
	router.service.SetRequireProject(require)
	t.Cleanup(router.service.WaitForBackgroundWork)
	return router
}

func allowAll() ports.AccessController {
	return ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPush: true, AllowAnonymousPull: true})
}

type apiErrors struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func decodeErrors(t *testing.T, rec *httptest.ResponseRecorder) apiErrors {
	t.Helper()
	var body apiErrors
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
		t.Fatalf("body = %q, want one OCI error (%v)", rec.Body.String(), err)
	}
	return body
}

const missingProjectMessage = `project "odabank" does not exist; ask an administrator to create it`

func TestRequireProjectHTTPMissingProjectIsNameUnknownAtUploadStartAndManifestPut(t *testing.T) {
	t.Parallel()
	rig := newEnforceRig(t, true)
	on := rig.router(t, allowAll(), true)

	rec := serve(on, http.MethodPost, "/v2/odabank/payments/strike/blobs/uploads/", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("upload start status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if e := decodeErrors(t, rec).Errors[0]; e.Code != "NAME_UNKNOWN" || e.Message != missingProjectMessage {
		t.Fatalf("upload start error = %+v, want NAME_UNKNOWN %q", e, missingProjectMessage)
	}

	digest := domain.DigestFromBytes([]byte("x")).String()
	put := serve(on, http.MethodPut, "/v2/odabank/app/manifests/latest", imageManifestFor(digest, 1))
	if put.Code != http.StatusNotFound {
		t.Fatalf("manifest put status = %d, want 404 (%s)", put.Code, put.Body.String())
	}
	if e := decodeErrors(t, put).Errors[0]; e.Code != "NAME_UNKNOWN" || e.Message != missingProjectMessage {
		t.Fatalf("manifest put error = %+v", e)
	}
}

func TestRequireProjectHTTPCompleteUploadOfAnEarlierSessionIsRefused(t *testing.T) {
	t.Parallel()
	// Store without the requirement lets a session start; the router flips
	// the service flag on afterwards, and the store check is separate, so
	// build the second store view with the requirement over the SAME files.
	root := t.TempDir()
	blobs, _ := fsblob.New(filepath.Join(root, "content"))
	dbPath := filepath.Join(root, "registry.db")
	loose, _ := metadata.New(dbPath)
	start := newRouterWithStores(blobs, loose, allowAll(), nil)
	rec := serve(start, http.MethodPost, "/v2/ghost/app/blobs/uploads/", nil)
	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatalf("upload start status = %d", rec.Code)
	}
	start.service.WaitForBackgroundWork()
	_ = loose.Close()

	strict, err := metadata.New(dbPath, metadata.WithRequireProject(true))
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer strict.Close()
	on := newRouterWithStores(blobs, strict, allowAll(), nil)
	defer on.service.WaitForBackgroundWork()

	digest := domain.DigestFromBytes([]byte("late")).String()
	done := serve(on, http.MethodPut, location+"?digest="+digest, []byte("late"))
	if done.Code != http.StatusNotFound {
		t.Fatalf("complete status = %d, want 404 (%s)", done.Code, done.Body.String())
	}
	if e := decodeErrors(t, done).Errors[0]; e.Code != "NAME_UNKNOWN" || e.Message != `project "ghost" does not exist; ask an administrator to create it` {
		t.Fatalf("complete error = %+v", e)
	}
}

func TestRequireProjectHTTPFlatRepositoryRules(t *testing.T) {
	t.Parallel()
	rig := newEnforceRig(t, false)
	off := rig.router(t, allowAll(), false)
	digest := pushBlob(t, off, "miapp", []byte("legacy"))
	if rec := serve(off, http.MethodPut, "/v2/miapp/manifests/latest", imageManifestFor(digest, 6)); rec.Code != http.StatusCreated {
		t.Fatalf("legacy manifest put = %d", rec.Code)
	}

	// Enforce on a store that also requires it (same data, strict view).
	on := rig.router(t, allowAll(), true)
	if rec := serve(on, http.MethodPost, "/v2/miapp/blobs/uploads/", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("push to existing flat repository = %d, want 202", rec.Code)
	}
	// Existing flat blob mount form keeps working (the mount parameters are
	// ignored and a normal upload session starts, as today).
	if rec := serve(on, http.MethodPost, "/v2/miapp/blobs/uploads/?mount="+digest+"&from=other", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("mount into existing flat repository = %d, want 202", rec.Code)
	}

	rec := serve(on, http.MethodPost, "/v2/otraapp/blobs/uploads/", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("new flat repository status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if e := decodeErrors(t, rec).Errors[0]; e.Code != "NAME_INVALID" || e.Message != `repository "otraapp" has no project; use <project>/otraapp (flat repository names are no longer created)` {
		t.Fatalf("new flat error = %+v", e)
	}
	// A mount into a repository of a missing project is refused too.
	if rec := serve(on, http.MethodPost, "/v2/odabank/app/blobs/uploads/?mount="+digest+"&from=miapp", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("mount into missing project = %d, want 404", rec.Code)
	}
}

func TestRequireProjectHTTPReadsNeverConsultProjects(t *testing.T) {
	t.Parallel()
	rig := newEnforceRig(t, false)
	off := rig.router(t, allowAll(), false)
	for _, repository := range []string{"miapp", "gone/app"} {
		digest := pushBlob(t, off, repository, []byte("data-"+repository))
		if rec := serve(off, http.MethodPut, "/v2/"+repository+"/manifests/latest", imageManifestFor(digest, len(repository)+5)); rec.Code != http.StatusCreated {
			t.Fatalf("manifest put %s = %d", repository, rec.Code)
		}
	}

	// Enforcement on, and no project row exists for "gone".
	on := rig.router(t, allowAll(), true)
	for _, target := range []string{"/v2/miapp/manifests/latest", "/v2/gone/app/manifests/latest", "/v2/gone/app/tags/list", "/v2/_catalog"} {
		if rec := serve(on, http.MethodGet, target, nil); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (%s)", target, rec.Code, rec.Body.String())
		}
	}
}

// With enforcement off every flow answers exactly as a router that never heard
// of the setting (status, error body, headers except the per-upload UUID).
func TestRequireProjectOffIsIdenticalToDefaultRouter(t *testing.T) {
	t.Parallel()
	run := func(setFlag bool) []string {
		rig := newEnforceRig(t, false)
		router := rig.router(t, allowAll(), false)
		if !setFlag {
			router = newRouterWithStores(rig.blobs, rig.store, allowAll(), nil)
			defer router.service.WaitForBackgroundWork()
		}
		var trace []string
		record := func(label string, rec *httptest.ResponseRecorder) {
			body := rec.Body.String()
			trace = append(trace, label+" "+http.StatusText(rec.Code)+" "+strings.ReplaceAll(body, "\n", ""))
		}
		for _, repository := range []string{"nope/app", "miapp", "a/b/c"} {
			start := serve(router, http.MethodPost, "/v2/"+repository+"/blobs/uploads/", nil)
			record("start "+repository, start)
			location := start.Header().Get("Location")
			digest := domain.DigestFromBytes([]byte("p-" + repository)).String()
			record("patch "+repository, serve(router, http.MethodPatch, location, []byte("p-"+repository)))
			record("put "+repository, serve(router, http.MethodPut, location+"?digest="+digest, nil))
			record("manifest "+repository, serve(router, http.MethodPut, "/v2/"+repository+"/manifests/latest", imageManifestFor(digest, len("p-"+repository))))
			record("tags "+repository, serve(router, http.MethodGet, "/v2/"+repository+"/tags/list", nil))
			record("mount "+repository, serve(router, http.MethodPost, "/v2/"+repository+"/blobs/uploads/?mount="+digest+"&from=x", nil))
			record("delete "+repository, serve(router, http.MethodDelete, "/v2/"+repository+"/manifests/latest", nil))
		}
		return trace
	}

	withFlag, without := run(true), run(false)
	if len(withFlag) != len(without) {
		t.Fatalf("trace lengths differ: %d vs %d", len(withFlag), len(without))
	}
	for i := range withFlag {
		// Upload locations carry random UUIDs; compare the rest.
		if withFlag[i] != without[i] {
			t.Fatalf("flow %d differs:\n  flag set: %s\n  default:  %s", i, withFlag[i], without[i])
		}
	}
	// And the pushes really succeeded (no hidden enforcement).
	if !strings.Contains(withFlag[0], "Accepted") || !strings.Contains(withFlag[3], "Created") {
		t.Fatalf("unexpected off-flow trace: %v", withFlag[:4])
	}
}

type scopedByRepo struct{ push map[string]bool }

func (c scopedByRepo) Authorize(_ context.Context, action ports.Action) error {
	if action.Verb == ports.ActionPush && c.push[action.Repository] {
		return nil
	}
	return domain.NewUnauthorizedError("denied")
}

func (scopedByRepo) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
}

func TestRequireProjectHTTPAuthorizationBeforeExistence(t *testing.T) {
	t.Parallel()
	denied := scopedByRepo{push: map[string]bool{}}

	withoutProject := newEnforceRig(t, true).router(t, denied, true)
	withProjectRig := newEnforceRig(t, true)
	if err := withProjectRig.store.CreateProject(context.Background(), "tenant-a", "odabank", "root"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	withProject := withProjectRig.router(t, denied, true)

	for _, target := range []string{"/v2/odabank/app/blobs/uploads/"} {
		a := serve(withoutProject, http.MethodPost, target, nil)
		b := serve(withProject, http.MethodPost, target, nil)
		if a.Code != http.StatusUnauthorized || a.Code != b.Code || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) || a.Header().Get("WWW-Authenticate") != b.Header().Get("WWW-Authenticate") {
			t.Fatalf("responses differ (existence oracle): %d %q vs %d %q", a.Code, a.Body.String(), b.Code, b.Body.String())
		}
	}
	a := serve(withoutProject, http.MethodPut, "/v2/odabank/app/manifests/latest", imageManifestFor(domain.DigestFromBytes([]byte("x")).String(), 1))
	b := serve(withProject, http.MethodPut, "/v2/odabank/app/manifests/latest", imageManifestFor(domain.DigestFromBytes([]byte("x")).String(), 1))
	if a.Code != http.StatusUnauthorized || a.Code != b.Code || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Fatalf("manifest put responses differ: %d %q vs %d %q", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
}

func TestRequireProjectHTTPPushToExistingProjectAndOdabank2(t *testing.T) {
	t.Parallel()
	rig := newEnforceRig(t, true)
	if err := rig.store.CreateProject(context.Background(), "tenant-a", "odabank", "root"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	on := rig.router(t, allowAll(), true)

	digest := pushBlob(t, on, "odabank/payments/strike", []byte("image"))
	if rec := serve(on, http.MethodPut, "/v2/odabank/payments/strike/manifests/v1", imageManifestFor(digest, 5)); rec.Code != http.StatusCreated {
		t.Fatalf("manifest put = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := serve(on, http.MethodPost, "/v2/odabank2/x/blobs/uploads/", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("odabank2/x = %d, want 404", rec.Code)
	}
}

// Docker-style wire flow through the real router: token exchange with basic
// auth, then the bearer token on the push requests.
func TestRequireProjectHTTPDockerFlowWithTokenExchange(t *testing.T) {
	t.Parallel()
	rig := newEnforceRig(t, true)
	authService := appauth.NewService(newSQLiteAuthStore(t))
	if _, err := authService.BootstrapAdmin(context.Background(), ports.BootstrapAdminInput{Username: "admin", Password: "fixture-pass-9x"}); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	router := newRouterWithStores(rig.blobs, rig.store, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), authService)
	router.service.SetRequireProject(true)
	t.Cleanup(router.service.WaitForBackgroundWork)

	token := func() string {
		req := httptest.NewRequest(http.MethodGet, "/auth/token?service=regixtry&scope=repository:odabank/app:pull,push", nil)
		req.SetBasicAuth("admin", "fixture-pass-9x")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
			t.Fatalf("token exchange = %d %q (%v)", rec.Code, rec.Body.String(), err)
		}
		return body.Token
	}()
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v2/odabank/app/blobs/uploads/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := post()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("push to missing project = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if e := decodeErrors(t, rec).Errors[0]; e.Code != "NAME_UNKNOWN" || e.Message != missingProjectMessage {
		t.Fatalf("error = %+v", e)
	}

	if err := rig.store.CreateProject(context.Background(), "tenant-a", "odabank", "admin"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if rec := post(); rec.Code != http.StatusAccepted {
		t.Fatalf("push after project creation = %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
}

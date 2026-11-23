package regixtryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

const projectsTestPassword = "fixture-pass-9x"

// projectsHTTP is a router wired like cmd/regixtry: real auth service over a
// SQLite-backed auth store, real project service over the metadata store, the
// auth service as grant cleaner, event recorder and project directory.
type projectsHTTP struct {
	t          *testing.T
	handler    *Router
	auth       *appauth.Service
	store      *authpostgres.Store
	projects   *appregixtry.ProjectService
	meta       *metadata.Store
	admin      domainauth.Principal
	adminToken string
}

type projectsLookup struct{ meta *metadata.Store }

func (l projectsLookup) ProjectExists(ctx context.Context, project string) (bool, error) {
	return l.meta.ProjectExists(ctx, "tenant-a", project)
}

func newProjectsHTTP(t *testing.T, deleteEnabled bool) *projectsHTTP {
	t.Helper()

	authStore, err := authpostgres.NewWithDriver("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	authService := appauth.NewService(authStore)
	// A cheap comparison keeps the matrix fast: every test user shares the
	// fixture password and is stored with a placeholder hash.
	authService.ConfigureLogin(appauth.LoginPolicy{MaxConcurrency: 8, MaxWait: 5 * time.Second, Compare: func(_, password []byte) error {
		if string(password) == projectsTestPassword {
			return nil
		}
		return errors.New("mismatch")
	}})
	blobStore, metadataStore, cleanup := newTestStores(t)

	accessController := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	router := newRouterWithStores(blobStore, metadataStore, accessController, authService)
	router.service.SetDeleteEnabled(deleteEnabled)
	projects := appregixtry.NewProjectService(router.service, metadataStore)
	projects.SetGrantCleaner(authService)
	projects.SetEventRecorder(authService)
	authService.SetProjectLookup(projectsLookup{meta: metadataStore})
	router.projects = projects
	t.Cleanup(drainingCleanup(router, cleanup))

	bootstrap, err := authService.BootstrapAdmin(context.Background(), ports.BootstrapAdminInput{Username: "admin", Password: projectsTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	login, err := authService.LoginWithPassword(context.Background(), "admin", projectsTestPassword, nil)
	if err != nil {
		t.Fatal(err)
	}

	return &projectsHTTP{t: t, handler: router, auth: authService, store: authStore, projects: projects, meta: metadataStore,
		admin:      domainauth.Principal{UserID: bootstrap.User.ID, Username: "admin", IsAdmin: true},
		adminToken: login.BearerToken}
}

func (e *projectsHTTP) do(method, path, token, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var req *http.Request
	if reader != nil {
		req = httptest.NewRequest(method, path, reader)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *projectsHTTP) createProject(name string) {
	e.t.Helper()
	if rec := e.do(http.MethodPost, "/admin/v1/projects", e.adminToken, `{"name":"`+name+`"}`); rec.Code != http.StatusCreated {
		e.t.Fatalf("create project %q status = %d body=%s", name, rec.Code, rec.Body.String())
	}
}

// user creates an enabled user and returns it with an unrestricted token.
func (e *projectsHTTP) user(name string, mutate ...func(*ports.AdminCreateUserInput)) (ports.AdminUser, string) {
	e.t.Helper()
	input := ports.AdminCreateUserInput{Username: name, Password: projectsTestPassword, Enabled: true}
	for _, m := range mutate {
		m(&input)
	}
	now := time.Now().UTC()
	stored := domainauth.User{ID: "id-" + name, Username: name, PasswordHash: "placeholder", IsAdmin: input.IsAdmin, IsReadOnly: input.IsReadOnly, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := e.store.UpsertUser(context.Background(), stored); err != nil {
		e.t.Fatal(err)
	}
	user := ports.AdminUser{ID: stored.ID, Username: name}
	login, err := e.auth.LoginWithPassword(context.Background(), name, projectsTestPassword, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return user, login.BearerToken
}

func (e *projectsHTTP) grant(project, user string, role domainauth.RepoRole) {
	e.t.Helper()
	if _, err := e.auth.PutProjectGrant(context.Background(), e.admin, project, user, role); err != nil {
		e.t.Fatal(err)
	}
}

// registryToken runs the docker-style token exchange for one scope.
func (e *projectsHTTP) registryToken(user, scope string) (string, string) {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=regixtry&scope="+url.QueryEscape(scope), nil)
	req.SetBasicAuth(user, projectsTestPassword)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("token exchange status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeBody[map[string]any](e.t, rec)
	token, _ := body["token"].(string)
	scopeGranted, _ := body["scope"].(string)
	return token, scopeGranted
}

// push publishes one tiny manifest as repository:latest, authenticating as
// user with a token exchanged for exactly that repository.
func (e *projectsHTTP) push(user, repository string) {
	e.t.Helper()
	token, _ := e.registryToken(user, "repository:"+repository+":push,pull")
	layer := []byte("layer-of-" + repository)
	start := e.do(http.MethodPost, "/v2/"+repository+"/blobs/uploads/", token, "")
	location := start.Header().Get("Location")
	if start.Code != http.StatusAccepted || location == "" {
		e.t.Fatalf("upload start for %s status = %d body=%s", repository, start.Code, start.Body.String())
	}
	digest := domain.DigestFromBytes(layer).String()
	req := httptest.NewRequest(http.MethodPut, location+"?digest="+digest, bytes.NewReader(layer))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("blob commit status = %d body=%s", rec.Code, rec.Body.String())
	}
	manifest := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":` + itoa(len(layer)) + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":` + itoa(len(layer)) + `}]}`
	put := httptest.NewRequest(http.MethodPut, "/v2/"+repository+"/manifests/latest", strings.NewReader(manifest))
	put.Header.Set("Authorization", "Bearer "+token)
	put.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	putRec := httptest.NewRecorder()
	e.handler.ServeHTTP(putRec, put)
	if putRec.Code != http.StatusCreated {
		e.t.Fatalf("manifest put for %s status = %d body=%s", repository, putRec.Code, putRec.Body.String())
	}
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeBody[map[string]string](t, rec)["error"]
}

func TestAdminProjectsCreateListGetAndDelete(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)

	// Create: 201 with the project shape.
	rec := env.do(http.MethodPost, "/admin/v1/projects", env.adminToken, `{"name":"odabank"}`)
	if rec.Code != http.StatusCreated || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("create status = %d ct=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	created := decodeBody[map[string]any](t, rec)
	for _, key := range []string{"name", "created_at", "created_by", "repositories", "tags", "last_pushed"} {
		if _, ok := created[key]; !ok {
			t.Errorf("create response misses %q: %v", key, created)
		}
	}
	if len(created) != 6 || created["name"] != "odabank" || created["created_by"] != "admin" || created["repositories"] != float64(0) || created["last_pushed"] != nil {
		t.Fatalf("create response = %v", created)
	}

	// Duplicate: 409; invalid names: 422; unknown field and bad body: 422.
	if rec := env.do(http.MethodPost, "/admin/v1/projects", env.adminToken, `{"name":"odabank"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{"name":"Odabank"}`, `{"name":"a/b"}`, `{"name":"blobs"}`, `{"name":""}`, `{"name":"x","extra":1}`, `not json`, ``} {
		if rec := env.do(http.MethodPost, "/admin/v1/projects", env.adminToken, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("POST %q status = %d, want 422 body=%s", body, rec.Code, rec.Body.String())
		}
	}

	// Totals and last_pushed follow real pushes; odabank2 is not counted.
	env.createProject("odabank2")
	env.push("admin", "odabank/payments/strike")
	listed := decodeBody[map[string][]map[string]any](t, env.do(http.MethodGet, "/admin/v1/projects", env.adminToken, ""))["projects"]
	if len(listed) != 2 || listed[0]["name"] != "odabank" || listed[1]["name"] != "odabank2" {
		t.Fatalf("list = %v, want odabank then odabank2", listed)
	}
	if listed[0]["repositories"] != float64(1) || listed[0]["tags"] != float64(1) || listed[0]["last_pushed"] == nil {
		t.Fatalf("odabank totals = %v", listed[0])
	}
	if listed[1]["repositories"] != float64(0) || listed[1]["last_pushed"] != nil {
		t.Fatalf("odabank2 totals = %v", listed[1])
	}
	got := decodeBody[map[string]any](t, env.do(http.MethodGet, "/admin/v1/projects/odabank", env.adminToken, ""))
	if got["name"] != "odabank" || got["repositories"] != float64(1) {
		t.Fatalf("get = %v", got)
	}

	// Empty list is an empty JSON array, not null.
	empty := newProjectsHTTP(t, true)
	if body := empty.do(http.MethodGet, "/admin/v1/projects", empty.adminToken, "").Body.String(); strings.TrimSpace(body) != `{"projects":[]}` {
		t.Fatalf("empty list body = %q", body)
	}

	// Get: 404 unknown, 422 invalid name; slash cannot be addressed.
	if rec := env.do(http.MethodGet, "/admin/v1/projects/ghost", env.adminToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("get ghost status = %d", rec.Code)
	}
	for _, path := range []string{"/admin/v1/projects/Odabank", "/admin/v1/projects/blobs"} {
		if rec := env.do(http.MethodGet, path, env.adminToken, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET %s status = %d, want 422", path, rec.Code)
		}
	}
	for _, path := range []string{"/admin/v1/projects/odabank/payments", "/admin/v1/projects/odabank/other/x", "/admin/v1/projects/odabank/members/a/b"} {
		if rec := env.do(http.MethodGet, path, env.adminToken, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, rec.Code)
		}
	}

	// Delete: non-empty is a 409 with the count; empty deletes with 200.
	rec = env.do(http.MethodDelete, "/admin/v1/projects/odabank", env.adminToken, "")
	if rec.Code != http.StatusConflict || !strings.Contains(errorMessage(t, rec), "still has 1 repository") {
		t.Fatalf("delete non-empty status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodDelete, "/admin/v1/projects/odabank2", env.adminToken, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"deleted":"odabank2"}` {
		t.Fatalf("delete empty status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank2", env.adminToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}

	// Wrong methods.
	for _, tc := range []struct{ method, path, allow string }{
		{http.MethodPut, "/admin/v1/projects", "GET, POST"},
		{http.MethodDelete, "/admin/v1/projects", "GET, POST"},
		{http.MethodPost, "/admin/v1/projects/odabank", "GET, DELETE"},
		{http.MethodPatch, "/admin/v1/projects/odabank", "GET, DELETE"},
	} {
		rec := env.do(tc.method, tc.path, env.adminToken, "")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != tc.allow {
			t.Errorf("%s %s status = %d allow=%q, want 405 %q", tc.method, tc.path, rec.Code, rec.Header().Get("Allow"), tc.allow)
		}
	}
}

func TestAdminProjectDeleteNeedsDeletionEnabled(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, false)
	env.createProject("sandbox")

	rec := env.do(http.MethodDelete, "/admin/v1/projects/sandbox", env.adminToken, "")
	if rec.Code != http.StatusUnprocessableEntity || errorMessage(t, rec) != "project deletion is not enabled" {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Exactly like deleting a repository with the flag off.
	repoRec := env.do(http.MethodDelete, "/admin/v1/repositories/sandbox/api", env.adminToken, "")
	if repoRec.Code != rec.Code && repoRec.Code != http.StatusNotFound {
		t.Fatalf("repository delete status = %d, project delete = %d: the two must refuse alike", repoRec.Code, rec.Code)
	}
	if rec := env.do(http.MethodGet, "/admin/v1/projects/sandbox", env.adminToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("project vanished: %d", rec.Code)
	}
}

func TestAdminProjectRoutesWithoutProjectServiceAreUnsupported(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.handler.projects = nil

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/admin/v1/projects", ""},
		{http.MethodPost, "/admin/v1/projects", `{"name":"x"}`},
		{http.MethodGet, "/admin/v1/projects/x", ""},
		{http.MethodDelete, "/admin/v1/projects/x", ""},
	} {
		if rec := env.do(tc.method, tc.path, env.adminToken, tc.body); rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s status = %d, want 501", tc.method, tc.path, rec.Code)
		}
	}
	// Still authenticated first.
	if rec := env.do(http.MethodGet, "/admin/v1/projects", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", rec.Code)
	}
}

type failingCleaner struct {
	mu  sync.Mutex
	err error
	ok  ports.ProjectGrantCleaner
}

func (c *failingCleaner) PurgeProjectGrants(ctx context.Context, project string) (int64, error) {
	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return c.ok.PurgeProjectGrants(ctx, project)
}

// TestAdminProjectDeleteFailedPurgeIsATypedRetryableError: the row is gone,
// the grants are not; the client gets a clear 503, the retry converges, and
// project.deleted is written exactly once.
func TestAdminProjectDeleteFailedPurgeIsATypedRetryableError(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	cleaner := &failingCleaner{ok: env.auth}
	env.projects.SetGrantCleaner(cleaner)
	env.createProject("odabank")
	env.user("ana")
	env.grant("odabank", "ana", domainauth.RepoRoleReader)
	cleaner.mu.Lock()
	cleaner.err = errors.New("pq: connection refused to 10.1.2.3")
	cleaner.mu.Unlock()

	rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank", env.adminToken, "")
	want := `project "odabank" was deleted but its grants could not be purged; retry the delete`
	if rec.Code != http.StatusServiceUnavailable || errorMessage(t, rec) != want || strings.Contains(rec.Body.String(), "10.1.2.3") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cleaner.mu.Lock()
	cleaner.err = nil
	cleaner.mu.Unlock()
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank", env.adminToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d body=%s", rec.Code, rec.Body.String())
	}

	events := decodeBody[[]map[string]any](t, env.do(http.MethodGet, "/admin/v1/access-events?limit=50", env.adminToken, ""))
	deleted := 0
	for _, e := range events {
		if e["action"] == "project.deleted" {
			deleted++
		}
	}
	if deleted != 1 {
		t.Fatalf("project.deleted written %d times, want exactly once", deleted)
	}
}

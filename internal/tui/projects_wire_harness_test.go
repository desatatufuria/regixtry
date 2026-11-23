package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
	regixtryhttp "regixtry/internal/protocol/http"
)

const projectsWirePassword = "fixture-pass-9x"

// projectsWireLookup answers the auth service's "does this project exist".
type projectsWireLookup struct{ meta *metadata.Store }

func (l projectsWireLookup) ProjectExists(ctx context.Context, project string) (bool, error) {
	return l.meta.ProjectExists(ctx, "tenant-a", project)
}

// projectsWire is the real stack behind the project screens: the HTTP router
// with the registry service, the project service, the auth service and the
// sqlite stores, reached through the real HTTPAdminClient.
type projectsWire struct {
	t       *testing.T
	server  *httptest.Server
	client  *HTTPAdminClient
	auth    *appauth.Service
	store   *authpostgres.Store
	admin   domainauth.Principal
	session AdminSession
}

func newProjectsWire(t *testing.T, deleteEnabled bool) *projectsWire {
	t.Helper()
	dir := t.TempDir()
	blob, err := fsblob.New(filepath.Join(dir, "c"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	authStore, err := authpostgres.NewWithDriver("sqlite", "file:"+filepath.Join(dir, "a.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	as := appauth.NewService(authStore)
	as.ConfigureLogin(appauth.LoginPolicy{MaxConcurrency: 8, MaxWait: 5 * time.Second, Compare: func(_, password []byte) error {
		if string(password) == projectsWirePassword {
			return nil
		}
		return context.Canceled
	}})
	ac := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	svc := appregixtry.NewService(blob, meta, ac, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	svc.SetDeleteEnabled(deleteEnabled)
	projects := appregixtry.NewProjectService(svc, meta)
	projects.SetGrantCleaner(as)
	projects.SetEventRecorder(as)
	as.SetProjectLookup(projectsWireLookup{meta: meta})
	router := regixtryhttp.NewRouter(svc, as, regixtryhttp.WithProjects(projects))

	ctx := context.Background()
	boot, err := as.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "admin", Password: projectsWirePassword})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := &projectsWire{t: t, server: server, client: client, auth: as, store: authStore,
		admin: domainauth.Principal{UserID: boot.User.ID, Username: "admin", IsAdmin: true}}
	w.session = w.loginAs("admin")
	return w
}

func (w *projectsWire) loginAs(username string) AdminSession {
	w.t.Helper()
	login, err := w.auth.LoginWithPassword(context.Background(), username, projectsWirePassword, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return AdminSession{Username: username, BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}
}

// user creates an enabled user (readOnly for a read-only one).
func (w *projectsWire) user(name string, readOnly bool) ports.AdminUser {
	w.t.Helper()
	user, err := w.auth.CreateAdminUser(context.Background(), w.admin, ports.AdminCreateUserInput{Username: name, Password: projectsWirePassword, Enabled: true, IsReadOnly: readOnly})
	if err != nil {
		w.t.Fatal(err)
	}
	return user
}

// push publishes one tiny manifest as repository:latest, as the global admin.
func (w *projectsWire) push(repository string) {
	w.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, w.server.URL+"/auth/token?service=regixtry&scope="+url.QueryEscape("repository:"+repository+":push,pull"), nil)
	req.SetBasicAuth("admin", projectsWirePassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	var tok struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	_ = resp.Body.Close()
	do := func(method, target string, body []byte, contentType string) *http.Response {
		r, _ := http.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tok.Token)
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			w.t.Fatal(err)
		}
		_ = res.Body.Close()
		return res
	}
	layer := []byte("layer-of-" + repository)
	start := do(http.MethodPost, w.server.URL+"/v2/"+repository+"/blobs/uploads/", nil, "")
	location := start.Header.Get("Location")
	if start.StatusCode != http.StatusAccepted || location == "" {
		w.t.Fatalf("upload start for %s status = %d", repository, start.StatusCode)
	}
	if !strings.HasPrefix(location, "http") {
		location = w.server.URL + location
	}
	digest := regixtrydomain.DigestFromBytes(layer).String()
	if res := do(http.MethodPut, location+"?digest="+digest, layer, ""); res.StatusCode != http.StatusCreated {
		w.t.Fatalf("blob commit status = %d", res.StatusCode)
	}
	size := strconv.Itoa(len(layer))
	manifest := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":` + size + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":` + size + `}]}`
	if res := do(http.MethodPut, w.server.URL+"/v2/"+repository+"/manifests/latest", []byte(manifest), "application/vnd.oci.image.manifest.v1+json"); res.StatusCode != http.StatusCreated {
		w.t.Fatalf("manifest put for %s status = %d", repository, res.StatusCode)
	}
}

// env is a screenEnv for the real client with the given session.
func (w *projectsWire) env(session AdminSession, width, height int, known ...string) screenEnv {
	env := usersTestEnv(w.client, width, height)
	env.Session = session
	env.KnownRepositories = known
	return env
}

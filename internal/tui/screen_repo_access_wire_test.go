package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
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

type repoAccessRecordingTransport struct {
	mu   sync.Mutex
	urls []string
	next http.RoundTripper
}

func (r *repoAccessRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.RequestURI())
	r.mu.Unlock()
	return r.next.RoundTrip(req)
}

func TestRepoAccessReadsAllGrantsThroughTheRealEndpoint(t *testing.T) {
	dir := t.TempDir()
	blob, err := fsblob.New(filepath.Join(dir, "c"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	authStore, err := authpostgres.NewWithDriver("sqlite", filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authStore.Close()
	ac := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	as := appauth.NewService(authStore)
	svc := appregixtry.NewService(blob, meta, ac, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	router := regixtryhttp.NewRouter(svc, as)
	ctx := context.Background()
	boot, err := as.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "admin", Password: "fixture-pass-9x"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := as.LoginWithPassword(ctx, "admin", "fixture-pass-9x", nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := domainauth.Principal{UserID: boot.User.ID, Username: "admin", IsAdmin: true}
	type want struct {
		repo, user string
		role       domainauth.RepoRole
	}
	var truth []want
	for u := 0; u < 1; u++ {
		name := fmt.Sprintf("user%03d", u)
		usr, err := as.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: name, Password: "fixture-pass-9x", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		for r := 0; r < 230; r++ {
			repo := fmt.Sprintf("ns/r%03d", r)
			role := domainauth.RepoRoleReader
			if (u+r)%2 == 0 {
				role = domainauth.RepoRoleWriter
			}
			if r%4 == 0 {
				role = domainauth.RepoRoleAdmin
			}
			ref, err := regixtrydomain.ParseRepositoryRef(repo)
			if err != nil {
				t.Fatal(err)
			}
			// Seeded straight into the store: the endpoint under test is the read.
			at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			if err := authStore.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: usr.ID, Repository: ref, Role: role, CreatedAt: at, UpdatedAt: at}); err != nil {
				t.Fatal(err)
			}
			truth = append(truth, want{repo, name, role})
		}
	}
	server := httptest.NewServer(router)
	defer server.Close()
	rt := &repoAccessRecordingTransport{next: http.DefaultTransport}
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Transport: rt, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	m := newAdminReadyModel(t, client)
	m = resizeModel(t, m, 220, 60)
	m.adminSession = AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	u, cmd := m.Update(navigateMsg{To: screenAdminRepositoryAccess})
	m = repoAccessRun(t, u.(Model), cmd)
	// A filter that matches nothing walks every page.
	m = repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("zzz")...)...)
	s := repoAccessScreenOf(t, m)
	if !s.done || len(s.rows) != len(truth) {
		t.Fatalf("done=%v rows=%d want %d", s.done, len(s.rows), len(truth))
	}
	seen := map[string]bool{}
	for _, r := range s.rows {
		seen[r.repo+"|"+r.username] = true
	}
	for _, w := range truth {
		if !seen[w.repo+"|"+w.user] {
			t.Fatalf("missing %v", w)
		}
	}
	if len(rt.urls) != 2 {
		t.Fatalf("want 2 requests, got %d", len(rt.urls))
	}
	// clamp: ask 1000 directly
	page, err := client.ListAllRepositoryGrants(ctx, AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}, 1000, "")
	if err != nil || len(page.Grants) != 200 || page.Next == "" {
		t.Fatalf("clamp: %v %d %q", err, len(page.Grants), page.Next)
	}
}

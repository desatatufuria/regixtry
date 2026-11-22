package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// TestEditUserRevokesExactlyOneGrantThroughTheRealStack drives the Edit user
// screen's command through the real HTTP client, router, service and store:
// the revoked grant disappears, the user's other grants and another user's
// identical grant survive (team/api versus team/api2 included), and a repo
// administrator grant is revoked too because the operator is a global admin.
func TestEditUserRevokesExactlyOneGrantThroughTheRealStack(t *testing.T) {
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
	mk := func(name string) ports.AdminUser {
		u, err := as.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: name, Password: "fixture-pass-9x", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, bob := mk("alice"), mk("bob")
	put := func(userID, repo string, role domainauth.RepoRole) {
		ref := regixtrydomain.MustParseRepositoryRef(repo)
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := authStore.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: userID, Repository: ref, Role: role, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	put(alice.ID, "team/api", domainauth.RepoRoleWriter)
	put(alice.ID, "team/api2", domainauth.RepoRoleReader)
	put(alice.ID, "team/solo", domainauth.RepoRoleAdmin)
	put(bob.ID, "team/api", domainauth.RepoRoleWriter)

	server := httptest.NewServer(router)
	defer server.Close()
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	session := AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}
	env := usersTestEnv(client, 160, 30)
	env.Session = session

	s := newEditUserScreen(alice.ID, "alice")
	next, _ := settleScreen(s, env, s.Init(env))
	s = next.(editUserScreen)
	if len(s.grants) != 3 {
		t.Fatalf("alice has %d grants on screen, want 3", len(s.grants))
	}
	selectRepo := func(repo string) {
		t.Helper()
		s, _ = editPress(t, s, env, testKeyTab())
		for range len(s.grants) {
			if grant, _ := s.selectedGrant(); grant.Repository.String() == repo {
				return
			}
			s, _ = editPress(t, s, env, testKeyDown())
		}
		t.Fatalf("grant %s not on screen", repo)
	}
	revoke := func(repo string) {
		t.Helper()
		selectRepo(repo)
		s, _ = editPress(t, s, env, testKeyRunes("x"), testKeyEnter())
		if !strings.HasPrefix(s.notice, "Revoked alice on "+repo) {
			t.Fatalf("notice = %q (%v)", s.notice, s.noticeTone)
		}
	}
	names := func(userID string) map[string]bool {
		t.Helper()
		grants, err := client.ListUserGrants(ctx, session, userID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, g := range grants {
			out[g.Repository.String()] = true
		}
		return out
	}

	revoke("team/api")
	if got := names(alice.ID); got["team/api"] || !got["team/api2"] || !got["team/solo"] || len(got) != 2 {
		t.Fatalf("alice grants after revoking team/api = %v", got)
	}
	if got := names(bob.ID); !got["team/api"] {
		t.Fatalf("bob lost his identical grant: %v", got)
	}
	if len(s.grants) != 2 {
		t.Fatalf("screen shows %d grants after the reload, want 2", len(s.grants))
	}
	s.grantsFocused = false
	revoke("team/solo")
	if got := names(alice.ID); len(got) != 1 || !got["team/api2"] {
		t.Fatalf("alice grants after revoking the repo-admin grant = %v", got)
	}
}

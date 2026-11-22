package auth

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
)

const projectTestPassword = "fixture-pass-9x"

func pg(project string, role domainauth.RepoRole) domainauth.ProjectGrant {
	return domainauth.ProjectGrant{UserID: "user-1", Project: project, Role: role}
}

func rg(repo string, role domainauth.RepoRole) domainauth.RepoGrant {
	return domainauth.RepoGrant{UserID: "user-1", Repository: regixtrydomain.MustParseRepositoryRef(repo), Role: role}
}

// TestIntersectRequestedActionsWithProjectGrants pins S3: a project grant
// expands into actions on the exact requested repository, the role matrix
// holds per verb, and a repository outside the project gets nothing.
func TestIntersectRequestedActionsWithProjectGrants(t *testing.T) {
	t.Parallel()

	all := "%s:pull,push,delete"
	tests := []struct {
		name      string
		grants    []domainauth.RepoGrant
		projects  []domainauth.ProjectGrant
		requested string
		want      []string
	}{
		{"project reader gets pull only", nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleReader)}, "odabank/payments/strike", []string{"pull"}},
		{"project writer gets pull push delete", nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleWriter)}, "odabank/payments/strike", []string{"pull", "push", "delete"}},
		{"project admin gets pull push delete", nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleAdmin)}, "odabank/payments/strike", []string{"pull", "push", "delete"}},
		{"similar project name gets nothing", nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleAdmin)}, "odabank2/x", []string{}},
		{"flat repository with the project name gets nothing", nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleAdmin)}, "odabank", []string{}},
		{"repository of another project gets nothing", nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleAdmin)}, "other/x", []string{}},
		{"project reader plus repo writer widens to writer", []domainauth.RepoGrant{rg("odabank/x", domainauth.RepoRoleWriter)}, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleReader)}, "odabank/x", []string{"pull", "push", "delete"}},
		{"project writer plus repo reader never narrows", []domainauth.RepoGrant{rg("odabank/x", domainauth.RepoRoleReader)}, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleWriter)}, "odabank/x", []string{"pull", "push", "delete"}},
		{"exact grant alone is unchanged", []domainauth.RepoGrant{rg("odabank/x", domainauth.RepoRoleReader)}, nil, "odabank/x", []string{"pull"}},
		{"exact grant on a sibling does not leak", []domainauth.RepoGrant{rg("odabank/y", domainauth.RepoRoleWriter)}, nil, "odabank/x", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requested := mustParseTestScope(t, "repository:"+fmt.Sprintf(all, tt.requested))
			got := intersectRequestedActionsWithProjects(false, false, tt.grants, tt.projects, requested)
			if !equalStringSlices(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}

	// Global flags are unchanged by project grants.
	pullPush := mustParseTestScope(t, "repository:odabank/x:pull,push,delete")
	if got := intersectRequestedActionsWithProjects(false, true, nil, []domainauth.ProjectGrant{pg("odabank", domainauth.RepoRoleAdmin)}, pullPush); !equalStringSlices(got, []string{"pull"}) {
		t.Fatalf("read-only with project admin = %#v, want pull only", got)
	}
	if got := intersectRequestedActionsWithProjects(true, false, nil, nil, pullPush); !equalStringSlices(got, []string{"pull", "push", "delete"}) {
		t.Fatalf("admin = %#v", got)
	}
}

// TestIntersectRequestedActionsWrapperIsUnchangedForExactGrants is the
// differential: for exact grants only, the old entry point and the project
// aware one decide identically over a table of roles x repositories x verbs.
func TestIntersectRequestedActionsWrapperIsUnchangedForExactGrants(t *testing.T) {
	t.Parallel()

	roles := []domainauth.RepoRole{"", domainauth.RepoRoleReader, domainauth.RepoRoleWriter, domainauth.RepoRoleAdmin}
	repos := []string{"team/app", "team/other", "team", "team2/app"}
	verbs := []string{"pull", "push", "delete", "pull,push", "pull,push,delete", "push,delete"}
	for _, role := range roles {
		for _, isAdmin := range []bool{false, true} {
			for _, isReadOnly := range []bool{false, true} {
				for _, repo := range repos {
					for _, verb := range verbs {
						var grants []domainauth.RepoGrant
						if role != "" {
							grants = []domainauth.RepoGrant{rg("team/app", role)}
						}
						requested := mustParseTestScope(t, "repository:"+repo+":"+verb)
						old := intersectRequestedActions(isAdmin, isReadOnly, grants, requested)
						now := intersectRequestedActionsWithProjects(isAdmin, isReadOnly, grants, nil, requested)
						if !equalStringSlices(old, now) {
							t.Fatalf("role=%q admin=%t ro=%t %s:%s old=%v new=%v", role, isAdmin, isReadOnly, repo, verb, old, now)
						}
						// Independent oracle for the exact-grant rule.
						want := []string{}
						switch {
						case isAdmin:
							want = actionsOf(requested, true, true, true)
						case isReadOnly:
							want = actionsOf(requested, true, false, false)
						case repo == "team/app" && role != "":
							want = actionsOf(requested, role.AllowsRead(), role.AllowsWrite(), role.AllowsWrite())
						}
						if !equalStringSlices(now, want) {
							t.Fatalf("role=%q admin=%t ro=%t %s:%s got=%v oracle=%v", role, isAdmin, isReadOnly, repo, verb, now, want)
						}
					}
				}
			}
		}
	}
}

func actionsOf(s domainauth.Scope, pull, push, del bool) []string {
	out := []string{}
	if pull && s.AllowsPull() {
		out = append(out, "pull")
	}
	if push && s.AllowsPush() {
		out = append(out, "push")
	}
	if del && s.AllowsDelete() {
		out = append(out, "delete")
	}
	return out
}

func newRealStore(t *testing.T) *authpostgres.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "auth.db") + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	store, err := authpostgres.NewWithDriver("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedLoginUser(t *testing.T, store *authpostgres.Store, now time.Time, id string) {
	t.Helper()
	user := domainauth.User{ID: id, Username: id, PasswordHash: mustHashPassword(t, projectTestPassword), Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
}

func exchange(t *testing.T, service *Service, user string, raw ...string) (string, domainauth.Principal) {
	t.Helper()
	scopes, err := domainauth.ParseScopes(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.LoginWithPassword(context.Background(), user, projectTestPassword, scopes)
	if err != nil {
		t.Fatalf("LoginWithPassword() error = %v", err)
	}
	principal, err := service.VerifyAccessToken(context.Background(), result.BearerToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	return result.Scope, principal
}

// TestTokenExchangeExpandsProjectGrantToExactRepositoryScope is the real
// token exchange: login, scope request, token stored, principal rebuilt from
// the bearer, then the predicates a registry request would evaluate.
func TestTokenExchangeExpandsProjectGrantToExactRepositoryScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newRealStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seedLoginUser(t, store, now, "dev")
	seedLoginUser(t, store, now, "other")
	service := NewService(store)
	service.now = func() time.Time { return now }

	if err := store.PutProjectGrant(ctx, domainauth.ProjectGrant{UserID: "dev", Project: "odabank", Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	scope, principal := exchange(t, service, "dev", "repository:odabank/payments/strike:push,pull")
	if scope != "repository:odabank/payments/strike:pull,push" {
		t.Fatalf("token scope = %q, want the exact repository name with pull,push", scope)
	}
	if !principal.HasReadAccess("odabank/payments/strike") || !principal.HasWriteAccess("odabank/payments/strike") {
		t.Fatal("project writer token must pull and push inside the project")
	}
	if principal.HasDeleteAccess("odabank/payments/strike") {
		t.Fatal("a token scoped pull,push must not delete")
	}
	if principal.HasReadAccess("odabank/payments/other") {
		t.Fatal("the token is scoped to one exact repository, not to the project")
	}

	// Outside the project: nothing, and the token stays restricted (never unrestricted).
	for _, name := range []string{"odabank2/x", "odabank", "other/x"} {
		scope, principal := exchange(t, service, "dev", "repository:"+name+":pull,push")
		if scope != "" {
			t.Fatalf("scope for %q = %q, want nothing", name, scope)
		}
		if principal.IsUnrestricted() || principal.HasReadAccess(name) {
			t.Fatalf("request for %q must yield a restricted token without access", name)
		}
	}

	// A user without grants gets nothing from someone else's project grant.
	if scope, _ := exchange(t, service, "other", "repository:odabank/payments/strike:pull"); scope != "" {
		t.Fatalf("other user scope = %q, want nothing", scope)
	}

	// Delete requested by a writer via the project.
	if scope, p := exchange(t, service, "dev", "repository:odabank/x:delete"); scope != "repository:odabank/x:delete" || !p.HasDeleteAccess("odabank/x") {
		t.Fatalf("delete scope = %q", scope)
	}

	// Reader: pull only.
	if err := store.PutProjectGrant(ctx, domainauth.ProjectGrant{UserID: "dev", Project: "odabank", Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if scope, _ := exchange(t, service, "dev", "repository:odabank/x:pull,push,delete"); scope != "repository:odabank/x:pull" {
		t.Fatalf("reader scope = %q, want pull only", scope)
	}
}

func TestRevokingProjectGrantRemovesAccessButKeepsExactGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newRealStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seedLoginUser(t, store, now, "dev")
	service := NewService(store)
	service.now = func() time.Time { return now }

	if err := store.PutProjectGrant(ctx, domainauth.ProjectGrant{UserID: "dev", Project: "odabank", Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: "dev", Repository: regixtrydomain.MustParseRepositoryRef("odabank/exact"), Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	if scope, _ := exchange(t, service, "dev", "repository:odabank/other:push"); scope != "repository:odabank/other:push" {
		t.Fatalf("before revoke scope = %q", scope)
	}
	if err := store.DeleteProjectGrant(ctx, "dev", "odabank"); err != nil {
		t.Fatal(err)
	}
	if scope, p := exchange(t, service, "dev", "repository:odabank/other:push"); scope != "" || p.HasWriteAccess("odabank/other") {
		t.Fatalf("after revoke scope = %q, want nothing", scope)
	}
	scope, p := exchange(t, service, "dev", "repository:odabank/exact:pull,push")
	if scope != "repository:odabank/exact:pull" || !p.HasReadAccess("odabank/exact") || p.HasWriteAccess("odabank/exact") {
		t.Fatalf("exact grant after revoke: scope = %q", scope)
	}
}

// TestProjectGrantChangeWhileTokensAreIssued runs under -race: grants flip
// while tokens are issued and verified. Every result must be one of the two
// consistent outcomes, and after the final revoke nothing is granted.
func TestProjectGrantChangeWhileTokensAreIssued(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newRealStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seedLoginUser(t, store, now, "dev")
	service := NewService(store)
	service.now = func() time.Time { return now }
	grant := domainauth.ProjectGrant{UserID: "dev", Project: "odabank", Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}
	requested, err := domainauth.ParseScopes([]string{"repository:odabank/x:pull,push"})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := store.PutProjectGrant(ctx, grant); err != nil {
				errs <- err
				return
			}
			if err := store.DeleteProjectGrant(ctx, "dev", "odabank"); err != nil {
				errs <- err
				return
			}
		}
	}()
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				result, err := service.LoginWithPassword(ctx, "dev", projectTestPassword, requested)
				if err != nil {
					errs <- err
					return
				}
				if result.Scope != "" && result.Scope != "repository:odabank/x:pull,push" {
					errs <- fmt.Errorf("inconsistent scope %q", result.Scope)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if scope, _ := exchange(t, service, "dev", "repository:odabank/x:pull,push"); scope != "" {
		t.Fatalf("scope after final revoke = %q, want nothing", scope)
	}
}

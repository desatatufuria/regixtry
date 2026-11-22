package regixtry

import (
	"context"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// TestDeleteRepositoryAdminDecisionMatchesAuthDomain is the guard against the
// two copies of the repo-admin rule diverging: for one table of principals and
// repositories, the verdict of the service's delete authorization (a zero-scope
// admin session, as the TUI and the admin API use) must equal the auth
// domain's predicate, and both must equal the hand-written expectation.
func TestDeleteRepositoryAdminDecisionMatchesAuthDomain(t *testing.T) {
	t.Parallel()

	rr := func(repo string, role domainauth.RepoRole) domainauth.RepoGrant {
		return domainauth.RepoGrant{Repository: domain.MustParseRepositoryRef(repo), Role: role}
	}
	pr := func(project string, role domainauth.RepoRole) domainauth.ProjectGrant {
		return domainauth.ProjectGrant{Project: project, Role: role}
	}
	tests := []struct {
		name       string
		principal  domainauth.Principal
		repository string
		want       bool
	}{
		{"global admin", domainauth.Principal{IsAdmin: true}, "team/app", true},
		{"read-only is not admin", domainauth.Principal{IsReadOnly: true}, "team/app", false},
		{"exact admin", domainauth.Principal{Grants: []domainauth.RepoGrant{rr("team/app", domainauth.RepoRoleAdmin)}}, "team/app", true},
		{"exact writer", domainauth.Principal{Grants: []domainauth.RepoGrant{rr("team/app", domainauth.RepoRoleWriter)}}, "team/app", false},
		{"exact admin on a sibling", domainauth.Principal{Grants: []domainauth.RepoGrant{rr("team/other", domainauth.RepoRoleAdmin)}}, "team/app", false},
		{"project admin", domainauth.Principal{ProjectGrants: []domainauth.ProjectGrant{pr("team", domainauth.RepoRoleAdmin)}}, "team/app", true},
		{"project admin, deeper repository", domainauth.Principal{ProjectGrants: []domainauth.ProjectGrant{pr("team", domainauth.RepoRoleAdmin)}}, "team/a/b/c", true},
		{"project writer", domainauth.Principal{ProjectGrants: []domainauth.ProjectGrant{pr("team", domainauth.RepoRoleWriter)}}, "team/app", false},
		{"project admin of a similar name", domainauth.Principal{ProjectGrants: []domainauth.ProjectGrant{pr("team", domainauth.RepoRoleAdmin)}}, "team2/app", false},
		{"project admin does not cover the flat repository", domainauth.Principal{ProjectGrants: []domainauth.ProjectGrant{pr("team", domainauth.RepoRoleAdmin)}}, "team", false},
		{"project reader plus exact admin", domainauth.Principal{Grants: []domainauth.RepoGrant{rr("team/app", domainauth.RepoRoleAdmin)}, ProjectGrants: []domainauth.ProjectGrant{pr("team", domainauth.RepoRoleReader)}}, "team/app", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.principal.HasGrantedRepoAdminAccess(tt.repository); got != tt.want {
				t.Fatalf("domain HasGrantedRepoAdminAccess = %t, want %t", got, tt.want)
			}

			service, cleanup := newTestService(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}))
			defer cleanup()
			service.SetDeleteEnabled(true)
			seedCtx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: []domainauth.Scope{{Type: "repository", Name: tt.repository, Actions: []string{"pull", "push"}, Canonical: "repository:" + tt.repository + ":pull,push"}}})
			publishManifestWithTags(t, service, seedCtx, tt.repository, "latest")

			ctx := ports.ContextWithPrincipal(context.Background(), tt.principal)
			_, err := service.DeleteRepository(ctx, tt.repository)
			if allowed := err == nil; allowed != tt.want {
				t.Fatalf("service DeleteRepository allowed = %t (err = %v), want %t", allowed, err, tt.want)
			}
		})
	}
}

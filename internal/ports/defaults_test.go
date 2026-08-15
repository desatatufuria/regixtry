package ports

import (
	"context"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
)

func TestSingleTenantResolverUsesDefaults(t *testing.T) {
	t.Parallel()

	resolver := NewSingleTenantResolver("")
	if tenant := resolver.Resolve(context.Background()); tenant != DefaultTenant {
		t.Fatalf("tenant = %q, want %q", tenant, DefaultTenant)
	}
}

func TestConfigurableAccessController(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    AccessConfig
		action    Action
		wantError bool
	}{
		{name: "anonymous pull allowed", config: AccessConfig{AllowAnonymousPull: true}, action: Action{Verb: ActionPull}},
		{name: "anonymous push allowed", config: AccessConfig{AllowAnonymousPush: true}, action: Action{Verb: ActionPush}},
		{name: "push requires auth", config: AccessConfig{AllowAnonymousPull: true}, action: Action{Verb: ActionPush}, wantError: true},
		{name: "pull requires auth when disabled", config: AccessConfig{}, action: Action{Verb: ActionPull}, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller := NewConfigurableAccessController(tt.config)
			err := controller.Authorize(context.Background(), tt.action)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected authorization error")
				}

				if !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
					t.Fatalf("expected unauthorized error, got %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Authorize() error = %v", err)
			}
		})
	}
}

func TestPrincipalAccessController(t *testing.T) {
	t.Parallel()

	controller := NewPrincipalAccessController(Challenge{Realm: "regixtry", Service: "regixtry"})
	principal := testPrincipal("team/app", true)

	if err := controller.Authorize(context.Background(), Action{Verb: ActionPull, Repository: "team/app", Principal: principal}); err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}

	if err := controller.Authorize(context.Background(), Action{Verb: ActionPush, Repository: "team/other", Principal: principal}); err == nil {
		t.Fatal("expected unauthorized push for other repository")
	}
}

func TestPrincipalAccessControllerEnforcesTokenScope(t *testing.T) {
	t.Parallel()

	controller := NewPrincipalAccessController(Challenge{Realm: "regixtry", Service: "regixtry"})
	principal := &domainauth.Principal{
		IsAdmin: true,
		Scopes:  []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}},
	}

	if err := controller.Authorize(context.Background(), Action{Verb: ActionPush, Repository: "team/app", Principal: principal}); err == nil {
		t.Fatal("expected token-scoped admin push to be rejected")
	}
}

// TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly pins
// design.md Decision 5's wiring: principalAccessController.Authorize gains a
// case ActionDelete arm backed by Principal.HasDeleteAccess, so writer/admin
// pass and reader (and no principal at all) fail (manifest-blob-delete
// tasks.md 1.11).
func TestPrincipalAccessControllerAuthorizesDeleteForWriterAndAdminOnly(t *testing.T) {
	t.Parallel()

	controller := NewPrincipalAccessController(Challenge{Realm: "regixtry", Service: "regixtry"})
	deleteScope := domainauth.Scope{Type: "repository", Name: "team/app", Actions: []string{"pull", "push", "delete"}, Canonical: "repository:team/app:pull,push,delete"}

	writerPrincipal := &domainauth.Principal{
		Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
		Scopes: []domainauth.Scope{deleteScope},
	}
	adminPrincipal := &domainauth.Principal{
		IsAdmin: true,
		Scopes:  []domainauth.Scope{deleteScope},
	}
	readerPrincipal := &domainauth.Principal{
		Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleReader}},
		Scopes: []domainauth.Scope{deleteScope},
	}

	tests := []struct {
		name      string
		principal *domainauth.Principal
		wantError bool
	}{
		{name: "writer passes", principal: writerPrincipal},
		{name: "admin passes", principal: adminPrincipal},
		{name: "reader fails", principal: readerPrincipal, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := controller.Authorize(context.Background(), Action{Verb: ActionDelete, Repository: "team/app", Principal: tt.principal})
			if tt.wantError {
				if err == nil {
					t.Fatal("expected delete to be rejected")
				}
				return
			}

			if err != nil {
				t.Fatalf("Authorize() error = %v", err)
			}
		})
	}

	if err := controller.Authorize(context.Background(), Action{Verb: ActionDelete, Repository: "team/app"}); err == nil {
		t.Fatal("expected anonymous (nil principal) delete to be rejected")
	}
}

// TestConfigurableAccessControllerNeverAuthorizesAnonymousDelete pins the
// threat-matrix "anonymous destructive access" boundary: configurableAccessController
// is left untouched, so ActionDelete matches neither its anonymous-pull nor
// its anonymous-push arm and falls through to NewUnauthorizedError, even on
// an instance with both anonymous pull and push enabled (manifest-blob-delete
// tasks.md 1.11).
func TestConfigurableAccessControllerNeverAuthorizesAnonymousDelete(t *testing.T) {
	t.Parallel()

	controller := NewConfigurableAccessController(AccessConfig{AllowAnonymousPull: true, AllowAnonymousPush: true})

	err := controller.Authorize(context.Background(), Action{Verb: ActionDelete, Repository: "team/app"})
	if err == nil {
		t.Fatal("expected anonymous delete to be rejected even with anonymous pull/push enabled")
	}
	if !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestActionScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{name: "pull", action: Action{Verb: ActionPull, Repository: "team/app"}, want: "repository:team/app:pull"},
		{name: "inspect", action: Action{Verb: ActionInspect, Repository: "team/app"}, want: "repository:team/app:pull"},
		{name: "push includes pull", action: Action{Verb: ActionPush, Repository: "team/app"}, want: "repository:team/app:pull,push"},
		{name: "catalog", action: Action{Verb: ActionCatalog}, want: "regixtry:catalog:*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.action.Scope(); got != tt.want {
				t.Fatalf("Scope() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInlineJobRunnerExecutesFunction(t *testing.T) {
	t.Parallel()

	runner := NewInlineJobRunner()
	called := false

	err := runner.Run(context.Background(), "sync", func(context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !called {
		t.Fatal("expected job function to be called")
	}
}

func testPrincipal(repository string, canWrite bool) *domainauth.Principal {
	role := domainauth.RepoRoleReader
	scopeActions := []string{"pull"}
	if canWrite {
		role = domainauth.RepoRoleWriter
		scopeActions = []string{"pull", "push"}
	}

	return &domainauth.Principal{Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef(repository), Role: role}}, Scopes: []domainauth.Scope{{Type: "repository", Name: repository, Actions: scopeActions, Canonical: "repository:" + repository + ":" + strings.Join(scopeActions, ",")}}}
}

package ports

import (
	"context"
	"strings"
	"testing"

	domainauth "registry/internal/domain/auth"
	domain "registry/internal/domain/registry"
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

	controller := NewPrincipalAccessController(Challenge{Realm: "registry", Service: "registry"})
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

	controller := NewPrincipalAccessController(Challenge{Realm: "registry", Service: "registry"})
	principal := &domainauth.Principal{
		IsAdmin: true,
		Scopes:  []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}},
	}

	if err := controller.Authorize(context.Background(), Action{Verb: ActionPush, Repository: "team/app", Principal: principal}); err == nil {
		t.Fatal("expected token-scoped admin push to be rejected")
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
		{name: "catalog", action: Action{Verb: ActionCatalog}, want: "registry:catalog:*"},
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

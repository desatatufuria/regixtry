package ports

import (
	"context"
	"testing"

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

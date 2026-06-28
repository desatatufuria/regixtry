package ports

import (
	"context"

	domain "registry/internal/domain/registry"
)

const DefaultTenant = "default"

type AccessConfig struct {
	AllowAnonymousPull bool
	Realm              string
	Service            string
}

type singleTenantResolver struct {
	tenant string
}

func NewSingleTenantResolver(tenant string) TenantResolver {
	if tenant == "" {
		tenant = DefaultTenant
	}

	return singleTenantResolver{tenant: tenant}
}

func (r singleTenantResolver) Resolve(context.Context) string {
	return r.tenant
}

type configurableAccessController struct {
	allowAnonymousPull bool
	challenge          Challenge
}

func NewConfigurableAccessController(config AccessConfig) AccessController {
	realm := config.Realm
	if realm == "" {
		realm = "registry"
	}

	service := config.Service
	if service == "" {
		service = "registry"
	}

	return configurableAccessController{
		allowAnonymousPull: config.AllowAnonymousPull,
		challenge: Challenge{
			Scheme:  "Bearer",
			Realm:   realm,
			Service: service,
		},
	}
}

func (c configurableAccessController) Authorize(_ context.Context, action Action) error {
	if action.Verb == ActionPull && c.allowAnonymousPull {
		return nil
	}

	return domain.NewUnauthorizedError("authentication required")
}

func (c configurableAccessController) Challenge() Challenge {
	return c.challenge
}

type inlineJobRunner struct{}

func NewInlineJobRunner() JobRunner {
	return inlineJobRunner{}
}

func (inlineJobRunner) Run(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

package ports

import (
	"context"

	domain "regixtry/internal/domain/regixtry"
)

const DefaultTenant = "default"

type AccessConfig struct {
	AllowAnonymousPull bool
	AllowAnonymousPush bool
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
	allowAnonymousPush bool
	challenge          Challenge
}

func NewConfigurableAccessController(config AccessConfig) AccessController {
	realm := config.Realm
	if realm == "" {
		realm = "regixtry"
	}

	service := config.Service
	if service == "" {
		service = "regixtry"
	}

	return configurableAccessController{
		allowAnonymousPull: config.AllowAnonymousPull,
		allowAnonymousPush: config.AllowAnonymousPush,
		challenge: Challenge{
			Scheme:  "Bearer",
			Realm:   realm,
			Service: service,
		},
	}
}

func (c configurableAccessController) Authorize(_ context.Context, action Action) error {
	if action.Principal != nil {
		return nil
	}

	if (action.Verb == ActionPull || action.Verb == ActionInspect || action.Verb == ActionCatalog) && c.allowAnonymousPull {
		return nil
	}

	if action.Verb == ActionPush && c.allowAnonymousPush {
		return nil
	}

	return domain.NewUnauthorizedError("authentication required")
}

func (c configurableAccessController) Challenge(action Action) Challenge {
	challenge := c.challenge
	challenge.Scope = action.Scope()
	return challenge
}

type principalAccessController struct {
	challenge Challenge
}

func NewPrincipalAccessController(challenge Challenge) AccessController {
	if challenge.Scheme == "" {
		challenge.Scheme = "Bearer"
	}
	if challenge.Realm == "" {
		challenge.Realm = "regixtry"
	}
	if challenge.Service == "" {
		challenge.Service = "regixtry"
	}

	return principalAccessController{challenge: challenge}
}

func (c principalAccessController) Authorize(_ context.Context, action Action) error {
	if action.Principal == nil {
		return domain.NewUnauthorizedError("authentication required")
	}

	switch action.Verb {
	case ActionCatalog:
		if action.Principal.CanAccessCatalog() {
			return nil
		}
	case ActionPull, ActionInspect:
		if action.Repository == "" || action.Principal.HasReadAccess(action.Repository) {
			return nil
		}
	case ActionPush:
		if action.Repository != "" && action.Principal.HasWriteAccess(action.Repository) {
			return nil
		}
	}

	return domain.NewUnauthorizedError("authorization required")
}

func (c principalAccessController) Challenge(action Action) Challenge {
	challenge := c.challenge
	challenge.Scope = action.Scope()
	return challenge
}

type inlineJobRunner struct{}

func NewInlineJobRunner() JobRunner {
	return inlineJobRunner{}
}

func (inlineJobRunner) Run(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

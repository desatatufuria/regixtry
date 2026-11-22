package regixtry

import (
	"context"
	"fmt"
	"sync"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// ProjectService manages projects: the first-level grouping of repositories
// (the first path segment of a repository name). Every method is restricted to
// GLOBAL administrators, checked here and not left to the HTTP layer, because
// a project is the unit later grants and push checks hang from.
//
// It is a separate type, wrapping the registry Service, so it adds no field to
// Service and nothing on the push, read or delete paths depends on it. It
// reuses the Service's tenant resolver and its deletion flag
// (SetDeleteEnabled), so one switch governs destructive actions.
type ProjectService struct {
	service  *Service
	projects ports.ProjectStore

	mu      sync.RWMutex
	cleaner ports.ProjectGrantCleaner
}

// NewProjectService wires the project use cases over the registry service and
// a project store (the SQLite metadata store implements it).
func NewProjectService(service *Service, projects ports.ProjectStore) *ProjectService {
	return &ProjectService{service: service, projects: projects}
}

// SetGrantCleaner installs the seam through which project grants, which live
// in the auth database, are purged after a project is deleted. It is nil by
// default and nothing in this package implements it: cross-store orchestration
// (and the order "purge stale grants, then create" on creation) belongs to the
// admin API unit. Set it once at startup.
func (p *ProjectService) SetGrantCleaner(cleaner ports.ProjectGrantCleaner) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleaner = cleaner
}

func (p *ProjectService) grantCleaner() ports.ProjectGrantCleaner {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cleaner
}

// requireGlobalAdmin mirrors internal/app/auth's requireAdmin (unexported in
// another package): the principal must be a global administrator holding an
// unrestricted session. A missing principal is unauthorized, never trusted:
// unlike repository reads, projects have no local-operator mode.
func requireGlobalAdmin(ctx context.Context) (*domainauth.Principal, error) {
	principal := ports.PrincipalFromContext(ctx)
	if principal == nil {
		return nil, domain.NewUnauthorizedError("administrator authentication is required")
	}
	if !principal.IsAdmin {
		return nil, domainauth.NewForbiddenError("administrator privileges are required")
	}
	if !principal.IsUnrestricted() {
		return nil, domainauth.NewForbiddenError("a scope-limited token cannot be used for administration")
	}
	return principal, nil
}

func (p *ProjectService) available() error {
	if p.projects == nil {
		return domain.NewUnsupportedError("projects are not available in this deployment")
	}
	return nil
}

// CreateProject creates a project. Authorization first, then name validation,
// then the store (a duplicate is a conflict).
func (p *ProjectService) CreateProject(ctx context.Context, name string) error {
	principal, err := requireGlobalAdmin(ctx)
	if err != nil {
		return err
	}
	if err := p.available(); err != nil {
		return err
	}
	name, err = domain.ParseProjectName(name)
	if err != nil {
		return err
	}

	return p.projects.CreateProject(ctx, p.service.tenant(ctx), name, principal.Username)
}

// DeleteProject deletes an EMPTY project (no repository, no open upload). It
// needs deletion enabled exactly like DeleteRepository, checked after
// authorization so a caller without permission never learns the flag state.
// When a grant cleaner is installed it runs after the delete succeeded; a
// cleanup failure is returned but does not undo the delete (orphan grants of a
// missing project are harmless and are purged again on re-creation).
func (p *ProjectService) DeleteProject(ctx context.Context, name string) error {
	if _, err := requireGlobalAdmin(ctx); err != nil {
		return err
	}
	if err := p.available(); err != nil {
		return err
	}
	name, err := domain.ParseProjectName(name)
	if err != nil {
		return err
	}
	if !p.service.deleteEnabled {
		return domain.NewValidationError("project deletion is not enabled")
	}

	if err := p.projects.DeleteProjectIfEmpty(ctx, p.service.tenant(ctx), name); err != nil {
		return err
	}

	if cleaner := p.grantCleaner(); cleaner != nil {
		if err := cleaner.PurgeProjectGrants(ctx, name); err != nil {
			return fmt.Errorf("project %q was deleted but its grants could not be purged: %w", name, err)
		}
	}
	return nil
}

// ListProjects returns the tenant's projects with their totals.
func (p *ProjectService) ListProjects(ctx context.Context) ([]ports.ProjectSummary, error) {
	if _, err := requireGlobalAdmin(ctx); err != nil {
		return nil, err
	}
	if err := p.available(); err != nil {
		return nil, err
	}

	return p.projects.ListProjects(ctx, p.service.tenant(ctx))
}

// ProjectExists reports whether the tenant has the project.
func (p *ProjectService) ProjectExists(ctx context.Context, name string) (bool, error) {
	if _, err := requireGlobalAdmin(ctx); err != nil {
		return false, err
	}
	if err := p.available(); err != nil {
		return false, err
	}
	name, err := domain.ParseProjectName(name)
	if err != nil {
		return false, err
	}

	return p.projects.ProjectExists(ctx, p.service.tenant(ctx), name)
}

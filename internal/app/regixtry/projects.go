package regixtry

import (
	"context"
	"fmt"
	"sync"
	"time"

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

	// lifecycle serializes create and delete, so the purge of stale grants
	// and the insert of a re-created name (or a delete and its cleanup) never
	// interleave with another create or delete of the same name in this
	// process.
	lifecycle sync.Mutex
	// cleanupTimeout bounds each cleaner call (grantCleanupTimeout; a field
	// only so a test can shorten it).
	cleanupTimeout time.Duration

	mu      sync.RWMutex
	cleaner ports.ProjectGrantCleaner
	events  ports.ProjectEventRecorder
}

var _ ports.ProjectAdmin = (*ProjectService)(nil)

// NewProjectService wires the project use cases over the registry service and
// a project store (the SQLite metadata store implements it).
func NewProjectService(service *Service, projects ports.ProjectStore) *ProjectService {
	return &ProjectService{service: service, projects: projects, cleanupTimeout: grantCleanupTimeout}
}

// SetGrantCleaner installs the seam through which project grants, which live
// in the auth database, are purged: before a name is (re)created, so stale
// grants never come back to life, and after a project is deleted. It is nil
// by default (nothing is purged); main wires the auth service. Set it once at
// startup.
func (p *ProjectService) SetGrantCleaner(cleaner ports.ProjectGrantCleaner) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleaner = cleaner
}

// SetEventRecorder installs the seam that writes project.created and
// project.deleted into the access history. Recording is best effort and never
// fails the operation. Set it once at startup.
func (p *ProjectService) SetEventRecorder(events ports.ProjectEventRecorder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = events
}

func (p *ProjectService) recordEvent(ctx context.Context, actor domainauth.Principal, action domainauth.AccessAction, project string) {
	p.mu.RLock()
	events := p.events
	p.mu.RUnlock()
	if events != nil {
		events.RecordProjectEvent(ctx, actor, action, project)
	}
}

// grantCleanupTimeout bounds every call to the grant cleaner. The cleaner talks
// to the auth database while the lifecycle lock is held, so a stalled store
// must not block every create and delete of a project.
const grantCleanupTimeout = 15 * time.Second

// purgeGrants runs the cleaner under grantCleanupTimeout.
func (p *ProjectService) purgeGrants(ctx context.Context, cleaner ports.ProjectGrantCleaner, project string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cleanupTimeout)
	defer cancel()

	return cleaner.PurgeProjectGrants(ctx, project)
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

// CreateProject creates a project. Authorization first, then name validation.
// A name that already exists is a conflict and nothing is purged (its grants
// are live). Otherwise stale grants of the name (left by a failed cleanup of
// an earlier project) are purged BEFORE the row is inserted, so they can never
// come back to life; if the purge fails the project is not created.
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

	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()

	tenant := p.service.tenant(ctx)
	if cleaner := p.grantCleaner(); cleaner != nil {
		exists, err := p.projects.ProjectExists(ctx, tenant, name)
		if err != nil {
			return err
		}
		if exists {
			return domain.NewProjectExistsError(name)
		}
		if _, err := p.purgeGrants(ctx, cleaner, name); err != nil {
			return domainauth.NewUnavailableError(fmt.Sprintf("project %q was not created because its stale grants could not be purged; retry the create", name), err)
		}
	}

	if err := p.projects.CreateProject(ctx, tenant, name, principal.Username); err != nil {
		return err
	}
	p.recordEvent(ctx, *principal, domainauth.AccessActionProjectCreated, name)

	return nil
}

// DeleteProject deletes an EMPTY project (no repository, no open upload). It
// needs deletion enabled exactly like DeleteRepository, checked after
// authorization so a caller without permission never learns the flag state.
//
// The project row goes first, then its grants are purged through the cleaner.
// A purge failure is returned (wrapping the cause with %w) but does not undo
// the delete, and the delete is idempotent for cleanup: when the row is
// already gone the cleaner still runs, so a retry converges; NotFound is
// returned only when the project is absent AND the cleaner removed nothing
// (or none is installed). The deletion is recorded once, when the row goes.
func (p *ProjectService) DeleteProject(ctx context.Context, name string) error {
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
	if !p.service.deleteEnabled {
		return domain.NewValidationError("project deletion is not enabled")
	}

	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()

	deleteErr := p.projects.DeleteProjectIfEmpty(ctx, p.service.tenant(ctx), name)
	rowDeleted := deleteErr == nil
	if deleteErr != nil && !domain.IsCode(deleteErr, domain.ErrorCodeNotFound) {
		return deleteErr
	}
	if rowDeleted {
		p.recordEvent(ctx, *principal, domainauth.AccessActionProjectDeleted, name)
	}

	cleaner := p.grantCleaner()
	if cleaner == nil {
		return deleteErr
	}
	removed, err := p.purgeGrants(ctx, cleaner, name)
	if err != nil {
		return domainauth.NewUnavailableError(fmt.Sprintf("project %q was deleted but its grants could not be purged; retry the delete", name), err)
	}
	if !rowDeleted && removed == 0 {
		return deleteErr
	}

	return nil
}

// GetProject returns one project with its totals, global administrators only.
func (p *ProjectService) GetProject(ctx context.Context, name string) (ports.ProjectSummary, error) {
	if _, err := requireGlobalAdmin(ctx); err != nil {
		return ports.ProjectSummary{}, err
	}
	if err := p.available(); err != nil {
		return ports.ProjectSummary{}, err
	}
	name, err := domain.ParseProjectName(name)
	if err != nil {
		return ports.ProjectSummary{}, err
	}

	return p.projects.GetProject(ctx, p.service.tenant(ctx), name)
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

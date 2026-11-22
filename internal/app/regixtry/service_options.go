package regixtry

import (
	"context"

	domain "regixtry/internal/domain/regixtry"
)

// projectEnforcer is what the metadata store must offer for the fast
// "project must exist" check at upload start. The authoritative check lives
// in the store's write transaction (sqlite.WithRequireProject).
type projectEnforcer interface {
	ProjectExists(ctx context.Context, tenant string, name string) (bool, error)
	RepositoryExists(ctx context.Context, tenant string, name string) (bool, error)
}

// SetRequireProject turns on "the project must exist before pushing"
// (REGISTRY_REQUIRE_PROJECT). Off by default: behavior is then unchanged. Set
// once at startup, before serving. Reads (pull, tags, catalog) never consult
// projects.
func (s *Service) SetRequireProject(require bool) {
	s.requireProject = require
}

// requireProjectForPush runs AFTER authorize, so a caller without push
// permission is denied identically whether or not the project exists.
func (s *Service) requireProjectForPush(ctx context.Context, repository domain.RepositoryRef) error {
	if !s.requireProject {
		return nil
	}

	checker, ok := s.metadata.(projectEnforcer)
	if !ok {
		return domain.NewUnsupportedError("project enforcement is not available in this deployment")
	}

	tenant := s.tenant(ctx)
	if project, hasProject := domain.ProjectOf(repository); hasProject {
		exists, err := checker.ProjectExists(ctx, tenant, project)
		if err != nil {
			return err
		}
		if !exists {
			return domain.NewProjectNotFoundError(project)
		}
		return nil
	}

	exists, err := checker.RepositoryExists(ctx, tenant, repository.String())
	if err != nil {
		return err
	}
	if !exists {
		return domain.NewFlatRepositoryRefusedError(repository.String())
	}
	return nil
}

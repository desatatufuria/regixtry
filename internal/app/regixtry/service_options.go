package regixtry

import (
	"context"
	"time"

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

// Open-upload fairness defaults applied by NewService.
const (
	// DefaultMaxOpenUploadsPerIdentity bounds the open uploads one
	// authenticated identity may hold: a tenth of DefaultMaxOpenUploads, so no
	// fewer than ten identities are needed to fill the global cap, and far
	// above any legitimate client (a push opens a handful of uploads, one per
	// layer being uploaded in parallel).
	DefaultMaxOpenUploadsPerIdentity = 100
	// DefaultUploadMaxLifetime is the absolute age of an upload session,
	// measured from its creation. Appending never extends it. It is three
	// times DefaultUploadTTL so a push slower than the idle TTL still works.
	DefaultUploadMaxLifetime = 72 * time.Hour
)

// SetUploadFairness sets the per-identity open-upload cap and the absolute
// upload lifetime (zero or negative disables that bound). Call it before
// serving traffic.
func (s *Service) SetUploadFairness(maxOpenPerIdentity int, maxLifetime time.Duration) {
	s.maxOpenUploadsPerIdentity = maxOpenPerIdentity
	s.uploadMaxLifetime = maxLifetime
}

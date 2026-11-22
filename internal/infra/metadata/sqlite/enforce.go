package sqlite

import (
	"context"
	"database/sql"

	domain "regixtry/internal/domain/regixtry"
)

// Option configures a Store at construction.
type Option func(*Store)

// WithRequireProject makes LinkBlob and PublishManifestVerified refuse, inside
// their write transaction, a repository whose project does not exist and a NEW
// repository without a project. Because DeleteProjectIfEmpty goes through the
// same write queue, a project delete and a push serialize: the push either
// sees the project (and the delete then sees the repository and refuses) or
// the delete wins and the push is refused.
func WithRequireProject(require bool) Option {
	return func(s *Store) { s.requireProject = require }
}

// checkProjectForWrite is the authoritative "project must exist" check, run in
// the write transaction right before any repository state is created. No-op
// unless the store was built with WithRequireProject(true).
func (s *Store) checkProjectForWrite(ctx context.Context, tx *sql.Tx, tenant string, repository domain.RepositoryRef) error {
	if !s.requireProject {
		return nil
	}

	var exists bool
	if project, ok := domain.ProjectOf(repository); ok {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE tenant = ? AND name = ?)`, tenant, project).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.NewProjectNotFoundError(project)
		}
		return nil
	}

	// Flat name: only a repository that already exists keeps working.
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM repositories WHERE tenant = ? AND name = ?)`, tenant, repository.String()).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.NewFlatRepositoryRefusedError(repository.String())
	}
	return nil
}

// RepositoryExists reports whether the tenant has a repository row with that
// exact name (the fast, non-authoritative check used at upload start).
func (s *Store) RepositoryExists(ctx context.Context, tenant string, name string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM repositories WHERE tenant = ? AND name = ?)`, tenant, name).Scan(&exists)
	return exists, err
}

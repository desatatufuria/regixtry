package sqlite

import (
	"context"
	"database/sql"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// inProject is the exact "belongs to project" predicate on a repository name
// column: the name IS the project, or starts with "<project>/". It is a
// substr comparison, not LIKE, so the '_' and '.' allowed in project names are
// never wildcards and "odabank" never matches "odabank2/...". Both ? are the
// project name.
const (
	repositoryInProject = `(r.name = ? OR substr(r.name, 1, length(?) + 1) = ? || '/')`
	uploadInProject     = `(u.repository = ? OR substr(u.repository, 1, length(?) + 1) = ? || '/')`
)

// CreateProject inserts a project through the write queue. A duplicate
// (tenant, name) is a conflict.
func (s *Store) CreateProject(ctx context.Context, tenant string, name string, createdBy string) error {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO projects (tenant, name, created_at, created_by)
		VALUES (?, ?, ?, ?)
	`, tenant, name, time.Now().UTC().Format(time.RFC3339Nano), createdBy)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return domain.NewProjectExistsError(name)
	}

	return tx.Commit()
}

func (s *Store) ProjectExists(ctx context.Context, tenant string, name string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE tenant = ? AND name = ?)`, tenant, name).Scan(&exists)
	return exists, err
}

// ListProjects returns the tenant's projects with repository and tag counts
// and the last push, derived from repositories/tags/manifests by exact
// first-segment match.
func (s *Store) ListProjects(ctx context.Context, tenant string) ([]ports.ProjectSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name, p.created_at, p.created_by,
			(SELECT COUNT(*) FROM repositories r
				WHERE r.tenant = p.tenant AND (r.name = p.name OR substr(r.name, 1, length(p.name) + 1) = p.name || '/')),
			(SELECT COUNT(t.id) FROM repositories r
				JOIN tags t ON t.repository_id = r.id AND t.tenant = r.tenant
				WHERE r.tenant = p.tenant AND (r.name = p.name OR substr(r.name, 1, length(p.name) + 1) = p.name || '/')),
			(SELECT MAX(m.created_at) FROM repositories r
				JOIN tags t ON t.repository_id = r.id AND t.tenant = r.tenant
				JOIN manifests m ON m.id = t.manifest_id
				WHERE r.tenant = p.tenant AND (r.name = p.name OR substr(r.name, 1, length(p.name) + 1) = p.name || '/'))
		FROM projects p
		WHERE p.tenant = ?
		ORDER BY p.name ASC
	`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []ports.ProjectSummary
	for rows.Next() {
		var (
			summary    ports.ProjectSummary
			createdAt  string
			lastPushed sql.NullString
		)
		if err := rows.Scan(&summary.Name, &createdAt, &summary.CreatedBy, &summary.Repositories, &summary.Tags, &lastPushed); err != nil {
			return nil, err
		}
		if summary.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, err
		}
		if lastPushed.Valid {
			if summary.LastPushed, err = time.Parse(time.RFC3339Nano, lastPushed.String); err != nil {
				return nil, err
			}
		}
		projects = append(projects, summary)
	}
	return projects, rows.Err()
}

// DeleteProjectIfEmpty removes the project in ONE write transaction, and only
// when no repository of the project and no active upload of the project
// exists. It also removes the project's orphan repository_blobs links
// (uploads that never produced a manifest), like DeleteRepository does for one
// repository. Blob files stay until GC.
func (s *Store) DeleteProjectIfEmpty(ctx context.Context, tenant string, name string) error {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE tenant = ? AND name = ?)`, tenant, name).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.NewNotFoundError("project", name)
	}

	var repositories int
	if err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM repositories r WHERE r.tenant = ? AND `+repositoryInProject,
		tenant, name, name, name).Scan(&repositories); err != nil {
		return err
	}
	var openUploads int
	if err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM uploads u WHERE u.tenant = ? AND u.status = ? AND `+uploadInProject,
		tenant, string(domain.UploadStatusActive), name, name, name).Scan(&openUploads); err != nil {
		return err
	}
	if repositories > 0 || openUploads > 0 {
		return domain.NewProjectNotEmptyError(name, repositories, openUploads)
	}

	if _, err = tx.ExecContext(ctx, `
		DELETE FROM repository_blobs
		WHERE tenant = ? AND (repository = ? OR substr(repository, 1, length(?) + 1) = ? || '/')
	`, tenant, name, name, name); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM projects WHERE tenant = ? AND name = ?`, tenant, name); err != nil {
		return err
	}

	return tx.Commit()
}

var _ ports.ProjectStore = (*Store)(nil)

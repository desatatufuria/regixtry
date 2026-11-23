package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// Project membership is exact: a repository belongs to project P only when its
// name starts with "P/" (a flat repository named P belongs to no project). The
// range [P+"/", P+"0") is that prefix set ('0' is '/'+1 in byte order), so it
// uses the (tenant, name) index and treats '_' and '%' literally, unlike LIKE.
const (
	repositoryInProject = `r.name >= ? || '/' AND r.name < ? || '0'`
	uploadInProject     = `u.repository >= ? || '/' AND u.repository < ? || '0'`
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
	// One pass over the tenant's repositories grouped by first segment, then
	// joined to projects: O(repositories), independent of the project count.
	rows, err := s.db.QueryContext(ctx, `
		WITH usage AS (
			SELECT substr(r.name, 1, instr(r.name, '/') - 1) AS project,
				COUNT(DISTINCT r.id) AS repositories,
				COUNT(t.id) AS tags,
				MAX(m.created_at) AS last_pushed
			FROM repositories r
			LEFT JOIN tags t ON t.repository_id = r.id AND t.tenant = r.tenant
			LEFT JOIN manifests m ON m.id = t.manifest_id
			WHERE r.tenant = ? AND instr(r.name, '/') > 1
			GROUP BY project
		)
		SELECT p.name, p.created_at, p.created_by,
			COALESCE(u.repositories, 0), COALESCE(u.tags, 0), u.last_pushed
		FROM projects p
		LEFT JOIN usage u ON u.project = p.name
		WHERE p.tenant = ?
		ORDER BY p.name ASC
	`, tenant, tenant)
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

// GetProject returns one project with the same totals ListProjects computes,
// reading only the repositories under the exact "name/" prefix (the same range
// DeleteProjectIfEmpty uses), so its cost does not depend on the other
// projects. A missing project is a not found error.
func (s *Store) GetProject(ctx context.Context, tenant string, name string) (ports.ProjectSummary, error) {
	var (
		summary    ports.ProjectSummary
		createdAt  string
		lastPushed sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT p.name, p.created_at, p.created_by,
			COUNT(DISTINCT r.id), COUNT(t.id), MAX(m.created_at)
		FROM projects p
		LEFT JOIN repositories r ON r.tenant = p.tenant AND `+repositoryInProject+`
		LEFT JOIN tags t ON t.repository_id = r.id AND t.tenant = r.tenant
		LEFT JOIN manifests m ON m.id = t.manifest_id
		WHERE p.tenant = ? AND p.name = ?
		GROUP BY p.id
	`, name, name, tenant, name).Scan(&summary.Name, &createdAt, &summary.CreatedBy, &summary.Repositories, &summary.Tags, &lastPushed)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ProjectSummary{}, domain.NewNotFoundError("project", name)
	}
	if err != nil {
		return ports.ProjectSummary{}, err
	}
	if summary.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return ports.ProjectSummary{}, err
	}
	if lastPushed.Valid {
		if summary.LastPushed, err = time.Parse(time.RFC3339Nano, lastPushed.String); err != nil {
			return ports.ProjectSummary{}, err
		}
	}

	return summary, nil
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
		tenant, name, name).Scan(&repositories); err != nil {
		return err
	}
	var openUploads int
	if err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM uploads u WHERE u.tenant = ? AND u.status = ? AND `+uploadInProject,
		tenant, string(domain.UploadStatusActive), name, name).Scan(&openUploads); err != nil {
		return err
	}
	if repositories > 0 || openUploads > 0 {
		return domain.NewProjectNotEmptyError(name, repositories, openUploads)
	}

	if _, err = tx.ExecContext(ctx, `
		DELETE FROM repository_blobs
		WHERE tenant = ? AND repository >= ? || '/' AND repository < ? || '0'
	`, tenant, name, name); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM projects WHERE tenant = ? AND name = ?`, tenant, name); err != nil {
		return err
	}

	return tx.Commit()
}

var _ ports.ProjectStore = (*Store)(nil)

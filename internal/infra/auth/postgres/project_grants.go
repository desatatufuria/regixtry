package postgres

import (
	"context"
	"strings"

	domainauth "regixtry/internal/domain/auth"
)

// PutProjectGrant creates or updates the one grant of a user on a project.
// An update changes role and updated_at and keeps created_at.
func (s *Store) PutProjectGrant(ctx context.Context, grant domainauth.ProjectGrant) error {
	if err := grant.Validate(); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_project_grants (user_id, project, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(user_id, project) DO UPDATE SET
			role = excluded.role,
			updated_at = excluded.updated_at
	`, grant.UserID, grant.Project, string(grant.Role), formatTime(grant.CreatedAt), formatTime(grant.UpdatedAt))
	return err
}

// DeleteProjectGrant removes one user's grant on one project. It touches no
// exact repository grant of the same user.
func (s *Store) DeleteProjectGrant(ctx context.Context, userID string, project string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM auth_project_grants WHERE user_id = $1 AND project = $2`, strings.TrimSpace(userID), project)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domainauth.NewNotFoundError("project grant", project)
	}

	return nil
}

// ListProjectGrants returns every project grant of one user, ordered by
// project. The result is never nil.
func (s *Store) ListProjectGrants(ctx context.Context, userID string) ([]domainauth.ProjectGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project, role, created_at, updated_at
		FROM auth_project_grants
		WHERE user_id = $1
		ORDER BY project ASC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grants := make([]domainauth.ProjectGrant, 0)
	for rows.Next() {
		var project, role, createdAt, updatedAt string
		if err := rows.Scan(&project, &role, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		grant := domainauth.ProjectGrant{UserID: userID, Project: project, Role: domainauth.RepoRole(role), CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt)}
		if err := grant.Validate(); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}

	return grants, rows.Err()
}

// ListProjectGrantsByProject returns up to limit grants of one project,
// ordered by user id, strictly after afterUserID ("" starts at the beginning).
// Keyset pagination: pass the last returned UserID as the next afterUserID.
// The index (project, user_id) serves both the filter and the order.
func (s *Store) ListProjectGrantsByProject(ctx context.Context, project string, afterUserID string, limit int) ([]domainauth.ProjectGrant, error) {
	if limit <= 0 {
		return []domainauth.ProjectGrant{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, role, created_at, updated_at
		FROM auth_project_grants
		WHERE project = $1 AND user_id > $2
		ORDER BY user_id ASC
		LIMIT $3
	`, project, afterUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grants := make([]domainauth.ProjectGrant, 0)
	for rows.Next() {
		var userID, role, createdAt, updatedAt string
		if err := rows.Scan(&userID, &role, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		grant := domainauth.ProjectGrant{UserID: userID, Project: project, Role: domainauth.RepoRole(role), CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt)}
		if err := grant.Validate(); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}

	return grants, rows.Err()
}

// DeleteProjectGrantsByProject removes every grant of one project (orphan
// cleanup when the project is deleted) and returns how many it removed.
// Exact match on the project column; other projects are untouched.
func (s *Store) DeleteProjectGrantsByProject(ctx context.Context, project string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM auth_project_grants WHERE project = $1`, project)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

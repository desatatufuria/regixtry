package auth

import (
	"context"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

const allGrantsMaxLimit = 200

// ListAdminAllRepositoryGrants lists the grants of every user and robot on
// every repository, cataloged or not, ordered by repository then username.
// Global admin only. Disabled owners still appear: their grants exist. after
// is the opaque Next value of the previous page; Next is empty on the last
// page. Names, roles and timestamps only, never secret material.
func (s *Service) ListAdminAllRepositoryGrants(ctx context.Context, actor domainauth.Principal, limit int, after string) (ports.AdminGrantPage, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.AdminGrantPage{}, err
	}
	if limit < 1 || limit > allGrantsMaxLimit {
		return ports.AdminGrantPage{}, domainauth.NewValidationError("limit must be a whole number between 1 and 200")
	}

	var cursor *domainauth.GrantCursor
	if after != "" {
		decoded, err := domainauth.DecodeGrantCursor(after)
		if err != nil {
			return ports.AdminGrantPage{}, err
		}
		cursor = &decoded
	}

	// One extra row tells whether another page exists.
	rows, err := s.store.ListRepoGrantsPage(ctx, cursor, limit+1)
	if err != nil {
		return ports.AdminGrantPage{}, err
	}

	page := ports.AdminGrantPage{Grants: make([]ports.AdminGrantEntry, 0, min(len(rows), limit))}
	if len(rows) > limit {
		rows = rows[:limit]
		page.Next = domainauth.CursorFor(rows[limit-1]).Encode()
	}
	for _, row := range rows {
		page.Grants = append(page.Grants, ports.AdminGrantEntry{
			Repository: row.Repository,
			UserID:     row.UserID,
			Username:   row.Username,
			IsRobot:    row.IsRobot,
			Role:       row.Role,
			CreatedAt:  row.CreatedAt,
			UpdatedAt:  row.UpdatedAt,
		})
	}

	return page, nil
}

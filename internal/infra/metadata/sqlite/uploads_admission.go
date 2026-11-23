package sqlite

import (
	"context"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

// Upload admission outcomes returned by SaveUploadAdmitted.
const (
	UploadAdmitted       = ""
	UploadDeniedGlobal   = "global"
	UploadDeniedIdentity = "identity"
)

// SaveUploadAdmitted inserts a NEW upload row owned by owner only if the
// tenant holds fewer than maxOpen open uploads and owner holds fewer than
// maxPerOwner (zero or negative disables that bound). The counts and the
// insert are ONE statement through the write queue, so concurrent callers can
// never overshoot either cap. It returns UploadAdmitted, UploadDeniedGlobal or
// UploadDeniedIdentity; a denied upload leaves no row. With requireProject
// the project must still exist, as in SaveUpload.
func (s *Store) SaveUploadAdmitted(ctx context.Context, tenant string, state domain.UploadState, owner string, maxOpen int, maxPerOwner int) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}

	args := []any{
		state.ID, tenant, state.Repository.String(), string(state.Status), state.Size,
		state.StartedAt.UTC().Format(time.RFC3339Nano), state.UpdatedAt.UTC().Format(time.RFC3339Nano), state.Location, owner,
		maxOpen, tenant, maxOpen,
		maxPerOwner, tenant, owner, maxPerOwner,
	}
	condition := `(? <= 0 OR (SELECT COUNT(*) FROM uploads WHERE tenant = ?) < ?)
		AND (? <= 0 OR (SELECT COUNT(*) FROM uploads WHERE tenant = ? AND owner = ?) < ?)`
	project, guarded := domain.ProjectOf(state.Repository)
	guarded = guarded && s.requireProject
	if guarded {
		condition += ` AND EXISTS (SELECT 1 FROM projects WHERE tenant = ? AND name = ?)`
		args = append(args, tenant, project)
	}

	result, err := s.execWrite(ctx, `
		INSERT INTO uploads (id, tenant, repository, status, size, started_at, updated_at, location, owner)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE `+condition, args...)
	if err != nil {
		return "", err
	}
	if n, err := result.RowsAffected(); err == nil && n > 0 {
		return UploadAdmitted, nil
	}

	// Refused: say which bound did it (a read; the refusal itself was atomic).
	if guarded {
		exists, err := s.ProjectExists(ctx, tenant, project)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", domain.NewProjectNotFoundError(project)
		}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM uploads WHERE tenant = ?`, tenant).Scan(&total); err != nil {
		return "", err
	}
	if maxOpen > 0 && total >= maxOpen {
		return UploadDeniedGlobal, nil
	}
	return UploadDeniedIdentity, nil
}

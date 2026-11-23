package sqlite

import (
	"context"
	"database/sql"
	"time"
)

// projectsBackfillMarker names the schema_backfills row guarding
// backfillProjects.
const projectsBackfillMarker = "projects_from_repositories_v1"

// backfillProjects is the one-time, idempotent migration that creates one
// project per distinct first path segment of every existing slashed
// repository name, per tenant. Flat names (no slash) create no project. It only
// inserts, and commits the rows and the marker together, so a project the
// operator deletes later is never resurrected on the next start.
func (s *Store) backfillProjects() error {
	var marker int
	err := s.db.QueryRow(`SELECT 1 FROM schema_backfills WHERE name = ?`, projectsBackfillMarker).Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	tx, err := s.beginWrite(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`
		INSERT OR IGNORE INTO projects (tenant, name, created_at, created_by)
		SELECT DISTINCT tenant, substr(name, 1, instr(name, '/') - 1), ?, 'migration'
		FROM repositories
		WHERE instr(name, '/') > 1
	`, now); err != nil {
		return err
	}

	if _, err = tx.Exec(`INSERT OR IGNORE INTO schema_backfills (name, completed_at) VALUES (?, ?)`, projectsBackfillMarker, now); err != nil {
		return err
	}

	return tx.Commit()
}

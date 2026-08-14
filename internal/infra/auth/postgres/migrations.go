package postgres

import (
	"context"
	"database/sql"
	"strings"
)

func migrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS auth_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin BOOLEAN NOT NULL DEFAULT FALSE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS auth_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT '',
			accessor TEXT NOT NULL UNIQUE,
			secret_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			revoked_at TEXT NULL,
			FOREIGN KEY(user_id) REFERENCES auth_users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS auth_tokens_user_kind_idx ON auth_tokens (user_id, kind);`,
		`ALTER TABLE auth_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT '';`,
		`CREATE TABLE IF NOT EXISTS auth_repo_grants (
			user_id TEXT NOT NULL,
			repository TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(user_id, repository),
			FOREIGN KEY(user_id) REFERENCES auth_users(id) ON DELETE CASCADE
		);`,
		`ALTER TABLE auth_users ADD COLUMN is_read_only BOOLEAN NOT NULL DEFAULT FALSE;`,
	}
}

// tolerateDuplicateColumns lists additive columns whose re-applied ALTER
// TABLE statement is expected to fail with a duplicate-column error on a
// database that already has them (design.md Decision 8). bootstrapSchema
// swallows exactly these errors so migrationStatements() stays safely
// re-runnable across both PostgreSQL and modernc SQLite.
var tolerateDuplicateColumns = []string{"scope", "is_read_only"}

func bootstrapSchema(ctx context.Context, db *sql.DB) error {
	for _, statement := range migrationStatements() {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			if isTolerableDuplicateColumnError(err) {
				continue
			}
			return err
		}
	}

	return nil
}

func isTolerableDuplicateColumnError(err error) bool {
	for _, column := range tolerateDuplicateColumns {
		if isDuplicateColumnError(err, column) {
			return true
		}
	}

	return false
}

func isDuplicateColumnError(err error, column string) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate column name: "+column) || // modernc SQLite
		strings.Contains(message, `column "`+column+`" of relation "auth_users" already exists`) || // PostgreSQL (auth_users)
		strings.Contains(message, `column "`+column+`" of relation "auth_tokens" already exists`) // PostgreSQL (auth_tokens)
}

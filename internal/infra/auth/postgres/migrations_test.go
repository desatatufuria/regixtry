package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

func TestMigrationsAreIdempotent(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := bootstrapSchema(ctx, store.db); err != nil {
			t.Fatalf("bootstrapSchema() run %d error = %v", i+2, err)
		}
	}
}

func TestAccessEventsMigrationIsAdditiveAndGuarded(t *testing.T) {
	t.Parallel()

	var statements []string
	for _, statement := range migrationStatements() {
		if strings.Contains(statement, "auth_access_events") {
			statements = append(statements, statement)
		}
	}
	if len(statements) != 2 {
		t.Fatalf("access events statements = %d, want 2 (table and index)", len(statements))
	}
	if !strings.Contains(statements[0], "CREATE TABLE IF NOT EXISTS auth_access_events") {
		t.Errorf("table statement is not guarded: %s", statements[0])
	}
	if !strings.Contains(statements[1], "CREATE INDEX IF NOT EXISTS") {
		t.Errorf("index statement is not guarded: %s", statements[1])
	}
	for _, statement := range statements {
		upper := strings.ToUpper(statement)
		for _, banned := range []string{"FOREIGN KEY", "REFERENCES", "DROP ", "ALTER "} {
			if strings.Contains(upper, banned) {
				t.Errorf("statement must be additive and free of %q: %s", banned, statement)
			}
		}
	}
}

// TestAccessEventsMigrationKeepsExistingData builds a database as it was
// before this migration existed (every statement except the access events
// ones), fills it, then runs the full migration list on top.
func TestAccessEventsMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range migrationStatements() {
		if strings.Contains(statement, "auth_access_events") {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil && !isTolerableDuplicateColumnError(err) {
			t.Fatalf("legacy statement failed: %v", err)
		}
	}

	old := &Store{db: db}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	user := domainauth.User{ID: "u1", Username: "ana", PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := old.UpsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	repo, err := regixtrydomain.ParseRepositoryRef("team/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: "u1", Repository: repo, Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	token := domainauth.Token{ID: "t1", UserID: "u1", Kind: domainauth.TokenKindAdminCredential, Name: "ci", Accessor: "acc", SecretHash: "sh", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := old.CreateToken(ctx, token); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapSchema(ctx, db); err != nil {
		t.Fatalf("bootstrapSchema() on an existing database error = %v", err)
	}

	if got, err := old.GetUserByID(ctx, "u1"); err != nil || got.Username != "ana" {
		t.Fatalf("user after migration = %#v, %v", got, err)
	}
	if grants, err := old.ListRepoGrants(ctx, "u1"); err != nil || len(grants) != 1 || grants[0].Role != domainauth.RepoRoleReader {
		t.Fatalf("grants after migration = %#v, %v", grants, err)
	}
	if got, err := old.GetTokenByAccessor(ctx, domainauth.TokenKindAdminCredential, "acc"); err != nil || got.Name != "ci" {
		t.Fatalf("token after migration = %#v, %v", got, err)
	}
	if events, err := old.ListAccessEvents(ctx, 10); err != nil || events == nil || len(events) != 0 {
		t.Fatalf("events after migration = %#v, %v", events, err)
	}
}

// TestAccessEventsMigrationIsByteStableOnExistingData is the durable probe: a
// database holding only the baseline statements, with users, a robot, tokens
// and a grant, gets the full list applied three times.
func TestAccessEventsMigrationIsByteStableOnExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range migrationStatements() {
		if strings.Contains(statement, "auth_access_events") {
			break
		}
		if _, err := db.ExecContext(ctx, statement); err != nil && !isTolerableDuplicateColumnError(err) {
			t.Fatalf("baseline statement failed: %v", err)
		}
	}

	old := &Store{db: db}
	now := time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.UTC)
	for _, user := range []domainauth.User{
		{ID: "u1", Username: "ana", PasswordHash: "h1", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "u2", Username: "bob", PasswordHash: "h2", IsAdmin: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: "r1", Username: "bot", PasswordHash: domainauth.RobotPasswordHash, IsRobot: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := old.UpsertUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := regixtrydomain.ParseRepositoryRef("team/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: "r1", Repository: repo, Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for i, owner := range []string{"u1", "u2", "r1"} {
		token := domainauth.Token{ID: fmt.Sprintf("t%d", i), UserID: owner, Kind: domainauth.TokenKindAdminCredential, Name: "n", Accessor: fmt.Sprintf("acc%d", i), SecretHash: fmt.Sprintf("sh%d", i), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := old.CreateToken(ctx, token); err != nil {
			t.Fatal(err)
		}
	}

	dump := func() string {
		var out strings.Builder
		for _, query := range []string{
			`SELECT id, username, password_hash, is_admin, enabled, created_at, updated_at, is_read_only, is_robot FROM auth_users ORDER BY id`,
			`SELECT user_id, repository, role, created_at, updated_at FROM auth_repo_grants ORDER BY user_id, repository`,
			`SELECT id, user_id, kind, name, scope, accessor, secret_hash, expires_at, created_at, COALESCE(revoked_at, '') FROM auth_tokens ORDER BY id`,
		} {
			rows, err := db.QueryContext(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			columns, _ := rows.Columns()
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			for rows.Next() {
				if err := rows.Scan(pointers...); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&out, "%v\n", values)
			}
			rows.Close()
			out.WriteString("--\n")
		}
		return out.String()
	}
	before := dump()

	for run := 1; run <= 3; run++ {
		if err := bootstrapSchema(ctx, db); err != nil {
			t.Fatalf("bootstrapSchema() run %d error = %v", run, err)
		}
		if got := dump(); got != before {
			t.Fatalf("old rows changed after run %d:\nbefore:\n%s\nafter:\n%s", run, before, got)
		}
	}

	var table, index, foreign int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'auth_access_events'`).Scan(&table); err != nil || table != 1 {
		t.Fatalf("access events table count = %d, err = %v", table, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'auth_access_events_occurred_idx'`).Scan(&index); err != nil || index != 1 {
		t.Fatalf("access events index count = %d, err = %v", index, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'auth\_%' ESCAPE '\' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`).Scan(&foreign); err != nil || foreign != 0 {
		t.Fatalf("non auth_ tables = %d, err = %v", foreign, err)
	}
}

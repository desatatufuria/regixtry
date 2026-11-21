package postgres

import (
	"context"
	"database/sql"
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

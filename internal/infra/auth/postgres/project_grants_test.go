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

func seedUsers(t *testing.T, store *Store, ids ...string) time.Time {
	t.Helper()
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, id := range ids {
		user := domainauth.User{ID: id, Username: id, PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}
		if err := store.UpsertUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	return now
}

func projectGrant(user, project string, role domainauth.RepoRole, at time.Time) domainauth.ProjectGrant {
	return domainauth.ProjectGrant{UserID: user, Project: project, Role: role, CreatedAt: at, UpdatedAt: at}
}

func TestStoreProjectGrantsPutListDeleteAndTwoUsers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	now := seedUsers(t, store, "u1", "u2")

	for _, g := range []domainauth.ProjectGrant{
		projectGrant("u1", "odabank", domainauth.RepoRoleReader, now),
		projectGrant("u1", "alpha", domainauth.RepoRoleAdmin, now),
		projectGrant("u2", "odabank", domainauth.RepoRoleWriter, now),
	} {
		if err := store.PutProjectGrant(ctx, g); err != nil {
			t.Fatalf("PutProjectGrant() error = %v", err)
		}
	}

	got, err := store.ListProjectGrants(ctx, "u1")
	if err != nil || len(got) != 2 || got[0].Project != "alpha" || got[0].Role != domainauth.RepoRoleAdmin || got[1].Project != "odabank" || got[1].Role != domainauth.RepoRoleReader || got[1].UserID != "u1" {
		t.Fatalf("ListProjectGrants(u1) = %#v, %v", got, err)
	}
	if !got[1].CreatedAt.Equal(now) || !got[1].UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %v / %v, want %v", got[1].CreatedAt, got[1].UpdatedAt, now)
	}

	// Upsert changes the role and updated_at, keeps created_at.
	later := now.Add(time.Hour)
	upd := projectGrant("u1", "odabank", domainauth.RepoRoleWriter, later)
	upd.CreatedAt = later
	if err := store.PutProjectGrant(ctx, upd); err != nil {
		t.Fatal(err)
	}
	got, _ = store.ListProjectGrants(ctx, "u1")
	if len(got) != 2 || got[1].Role != domainauth.RepoRoleWriter || !got[1].CreatedAt.Equal(now) || !got[1].UpdatedAt.Equal(later) {
		t.Fatalf("after upsert = %#v", got)
	}

	if err := store.PutProjectGrant(ctx, projectGrant("u1", "a/b", domainauth.RepoRoleReader, now)); err == nil {
		t.Fatal("PutProjectGrant(invalid project) error = nil")
	}

	if err := store.DeleteProjectGrant(ctx, "u1", "odabank"); err != nil {
		t.Fatalf("DeleteProjectGrant() error = %v", err)
	}
	if err := store.DeleteProjectGrant(ctx, "u1", "odabank"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("second DeleteProjectGrant() error = %v, want not-found", err)
	}
	if other, _ := store.ListProjectGrants(ctx, "u2"); len(other) != 1 || other[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("u2 grants after deleting u1's = %#v", other)
	}
	if empty, err := store.ListProjectGrants(ctx, "nobody"); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ListProjectGrants(unknown) = %#v, %v, want empty non-nil", empty, err)
	}
}

func TestStoreListProjectGrantsByProjectPaginates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	ids := []string{"u1", "u2", "u3", "u4", "u5"}
	now := seedUsers(t, store, ids...)
	for _, id := range ids {
		if err := store.PutProjectGrant(ctx, projectGrant(id, "odabank", domainauth.RepoRoleReader, now)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PutProjectGrant(ctx, projectGrant("u1", "odabank2", domainauth.RepoRoleAdmin, now)); err != nil {
		t.Fatal(err)
	}

	var seen []string
	after := ""
	for {
		page, err := store.ListProjectGrantsByProject(ctx, "odabank", after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatalf("page size %d > 2", len(page))
		}
		for _, g := range page {
			if g.Project != "odabank" {
				t.Fatalf("page leaked project %q", g.Project)
			}
			seen = append(seen, g.UserID)
		}
		after = page[len(page)-1].UserID
	}
	if strings.Join(seen, ",") != "u1,u2,u3,u4,u5" {
		t.Fatalf("paged users = %v", seen)
	}
	if page, err := store.ListProjectGrantsByProject(ctx, "odabank", "", 0); err != nil || len(page) != 0 {
		t.Fatalf("limit 0 = %#v, %v, want empty", page, err)
	}
}

func TestStoreDeleteProjectGrantsByProjectIsExact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	now := seedUsers(t, store, "u1", "u2")
	for _, g := range []domainauth.ProjectGrant{
		projectGrant("u1", "odabank", domainauth.RepoRoleReader, now),
		projectGrant("u2", "odabank", domainauth.RepoRoleWriter, now),
		projectGrant("u1", "odabank2", domainauth.RepoRoleAdmin, now),
	} {
		if err := store.PutProjectGrant(ctx, g); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := store.DeleteProjectGrantsByProject(ctx, "odabank")
	if err != nil || removed != 2 {
		t.Fatalf("DeleteProjectGrantsByProject() = %d, %v, want 2", removed, err)
	}
	if left, _ := store.ListProjectGrants(ctx, "u1"); len(left) != 1 || left[0].Project != "odabank2" {
		t.Fatalf("remaining = %#v, want only odabank2", left)
	}
	if removed, err := store.DeleteProjectGrantsByProject(ctx, "odabank"); err != nil || removed != 0 {
		t.Fatalf("second delete = %d, %v, want 0 and no error", removed, err)
	}
}

func TestStoreDeleteUserRemovesProjectGrantsOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	now := seedUsers(t, store, "u1", "u2")
	for _, id := range []string{"u1", "u2"} {
		if err := store.PutProjectGrant(ctx, projectGrant(id, "odabank", domainauth.RepoRoleReader, now)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM auth_project_grants WHERE user_id = 'u1'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("project grants left for deleted user = %d, %v", count, err)
	}
	if left, _ := store.ListProjectGrants(ctx, "u2"); len(left) != 1 {
		t.Fatalf("other user's grants = %#v", left)
	}
}

// Replacing the exact repository grants of a user (and deleting one) must
// never touch project grants.
func TestStoreRepoGrantOperationsLeaveProjectGrantsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	now := seedUsers(t, store, "u1")
	repo := regixtrydomain.MustParseRepositoryRef("odabank/x")
	if err := store.PutProjectGrant(ctx, projectGrant("u1", "odabank", domainauth.RepoRoleReader, now)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: "u1", Repository: repo, Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteRepoGrant(ctx, "u1", repo); err != nil {
		t.Fatal(err)
	}
	if left, _ := store.ListProjectGrants(ctx, "u1"); len(left) != 1 || left[0].Role != domainauth.RepoRoleReader {
		t.Fatalf("project grants after repo grant operations = %#v", left)
	}
}

// TestProjectGrantsMigrationOnOldSchema builds a database as it was before
// the project grants table, fills it, and applies the full list three times:
// the table appears, old rows stay byte-identical, nothing else changes.
func TestProjectGrantsMigrationOnOldSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range migrationStatements() {
		if strings.Contains(statement, "auth_project_grants") {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil && !isTolerableDuplicateColumnError(err) {
			t.Fatalf("legacy statement failed: %v", err)
		}
	}
	old := &Store{db: db}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := old.UpsertUser(ctx, domainauth.User{ID: "u1", Username: "ana", PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	repo := regixtrydomain.MustParseRepositoryRef("team/app")
	if err := old.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: "u1", Repository: repo, Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	dump := func() string {
		var out strings.Builder
		for _, q := range []string{`SELECT id, username, password_hash, created_at FROM auth_users ORDER BY id`, `SELECT user_id, repository, role, created_at, updated_at FROM auth_repo_grants ORDER BY user_id, repository`} {
			rows, err := db.QueryContext(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]string, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				out.WriteString(strings.Join(vals, "|") + "\n")
			}
			rows.Close()
		}
		return out.String()
	}
	before := dump()

	for run := 1; run <= 3; run++ {
		if err := bootstrapSchema(ctx, db); err != nil {
			t.Fatalf("bootstrapSchema() run %d error = %v", run, err)
		}
		if got := dump(); got != before {
			t.Fatalf("old rows changed after run %d:\n%s\n%s", run, before, got)
		}
	}

	var table, index int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'auth_project_grants'`).Scan(&table); err != nil || table != 1 {
		t.Fatalf("project grants table count = %d, %v", table, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name = 'auth_project_grants_project_idx'`).Scan(&index); err != nil || index != 1 {
		t.Fatalf("project grants index count = %d, %v", index, err)
	}
	if err := old.PutProjectGrant(ctx, projectGrant("u1", "team", domainauth.RepoRoleReader, now)); err != nil {
		t.Fatalf("PutProjectGrant() on migrated database error = %v", err)
	}
}

func TestStorePutProjectGrantTrimsUserID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	now := seedUsers(t, store, "u1")

	if err := store.PutProjectGrant(ctx, projectGrant("  u1\t", "odabank", domainauth.RepoRoleReader, now)); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListProjectGrants(ctx, "u1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListProjectGrants() = %#v, %v, want the grant put with whitespace", got, err)
	}
	if err := store.DeleteProjectGrant(ctx, " u1 ", "odabank"); err != nil {
		t.Fatalf("DeleteProjectGrant() error = %v", err)
	}
	if err := store.PutProjectGrant(ctx, projectGrant("   ", "odabank", domainauth.RepoRoleReader, now)); err == nil {
		t.Fatal("PutProjectGrant(blank user) error = nil")
	}
}

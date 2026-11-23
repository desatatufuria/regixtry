package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

func admissionState(id string) domain.UploadState {
	now := time.Now().UTC()
	return domain.UploadState{ID: id, Repository: domain.MustParseRepositoryRef("team/a"), Status: domain.UploadStatusActive, StartedAt: now, UpdatedAt: now, Location: "/x/" + id}
}

func TestSaveUploadAdmittedEnforcesBothCaps(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	admit := func(id, owner string) string {
		t.Helper()
		denied, err := store.SaveUploadAdmitted(ctx, "t", admissionState(id), owner, 3, 2)
		if err != nil {
			t.Fatalf("SaveUploadAdmitted(%s) error = %v", id, err)
		}
		return denied
	}
	if admit("1", "u1") != UploadAdmitted || admit("2", "u1") != UploadAdmitted {
		t.Fatal("first two uploads of u1 must be admitted")
	}
	if got := admit("3", "u1"); got != UploadDeniedIdentity {
		t.Fatalf("third upload of u1 = %q, want identity denial", got)
	}
	if admit("4", "u2") != UploadAdmitted {
		t.Fatal("u2 must be admitted below both caps")
	}
	if got := admit("5", "u3"); got != UploadDeniedGlobal {
		t.Fatalf("fourth open upload = %q, want global denial", got)
	}
	uploads, err := store.ListUploads(ctx, "t", nil)
	if err != nil || len(uploads) != 3 {
		t.Fatalf("rows = %d, %v, want 3 (denied uploads leave nothing)", len(uploads), err)
	}
	// Another tenant has its own counts.
	if denied, err := store.SaveUploadAdmitted(ctx, "other", admissionState("6"), "u1", 3, 2); err != nil || denied != UploadAdmitted {
		t.Fatalf("other tenant = %q, %v, want admitted", denied, err)
	}
}

// A database created before the owner column existed upgrades in place; its
// rows count only against the global cap and the migration is idempotent.
func TestUploadsOwnerColumnMigrationIsAdditiveAndIdempotent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE uploads (id TEXT PRIMARY KEY, tenant TEXT NOT NULL, repository TEXT NOT NULL, status TEXT NOT NULL, size INTEGER NOT NULL, started_at TEXT NOT NULL, updated_at TEXT NOT NULL, location TEXT NOT NULL);
		INSERT INTO uploads VALUES ('old', 't', 'team/a', 'active', 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '/x/old');`); err != nil {
		t.Fatalf("legacy schema error = %v", err)
	}
	_ = raw.Close()

	for i := 0; i < 2; i++ {
		store, err := New(path)
		if err != nil {
			t.Fatalf("New() #%d error = %v", i, err)
		}
		if _, err := store.GetUpload(context.Background(), "t", "old"); err != nil {
			t.Fatalf("legacy upload unreadable after migration #%d: %v", i, err)
		}
		if i == 0 {
			// Legacy rows (owner '') count only against the global cap.
			if denied, err := store.SaveUploadAdmitted(context.Background(), "t", admissionState("new"), "", 0, 1); err != nil || denied != UploadAdmitted {
				t.Fatalf("anonymous bucket with one legacy row = %q, %v, want admitted (no per-owner cap for '')", denied, err)
			}
			if denied, err := store.SaveUploadAdmitted(context.Background(), "t", admissionState("named"), "u1", 0, 1); err != nil || denied != UploadAdmitted {
				t.Fatalf("identified owner next to legacy rows = %q, %v, want admitted", denied, err)
			}
		}
		_ = store.Close()
	}
}

package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

// seedLegacyRepositories writes repository rows straight into a schema-only
// store (no backfill ran yet).
func seedLegacyRepositories(t *testing.T, store *Store, rows map[string][]string) {
	t.Helper()
	for tenant, names := range rows {
		for _, name := range names {
			if _, err := store.db.Exec(`INSERT INTO repositories (tenant, name, created_at) VALUES (?, ?, ?)`, tenant, name, "2026-01-01T00:00:00Z"); err != nil {
				t.Fatalf("seed repository %s/%s: %v", tenant, name, err)
			}
		}
	}
}

func TestStoreBackfillCreatesOneProjectPerFirstSegmentAndIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.db")

	legacy := newSchemaOnlyStore(t, path)
	seedLegacyRepositories(t, legacy, map[string][]string{
		"tenant-a": {"odabank/api", "odabank/payments/strike", "gosse/web", "alpine", "odabank2/web"},
		"tenant-b": {"odabank/other", "flatb"},
	})
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	first, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	snapshot := func(store *Store) []string {
		rows, err := store.db.Query(`SELECT id, tenant, name, created_at, created_by FROM projects ORDER BY tenant, name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id int
			var tenant, name, createdAt, createdBy string
			if err := rows.Scan(&id, &tenant, &name, &createdAt, &createdBy); err != nil {
				t.Fatal(err)
			}
			out = append(out, fmt.Sprintf("%d|%s|%s|%s|%s", id, tenant, name, createdAt, createdBy))
		}
		return out
	}
	rowsFirst := snapshot(first)
	var marker string
	if err := first.db.QueryRow(`SELECT completed_at FROM schema_backfills WHERE name = ?`, projectsBackfillMarker).Scan(&marker); err != nil {
		t.Fatalf("projects backfill marker missing: %v", err)
	}
	if len(rowsFirst) != 4 {
		t.Fatalf("projects after backfill = %v, want 4 rows (tenant-a: gosse, odabank, odabank2; tenant-b: odabank)", rowsFirst)
	}
	for _, name := range []string{"alpine", "flatb"} {
		if scalar(t, first, `SELECT COUNT(*) FROM projects WHERE name = ?`, name) != 0 {
			t.Fatalf("flat repository %q produced a project", name)
		}
	}
	if scalar(t, first, `SELECT COUNT(*) FROM projects WHERE created_by <> 'migration'`) != 0 {
		t.Fatalf("backfilled projects must be created_by='migration'")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := New(path)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	defer second.Close()
	if got := snapshot(second); !reflect.DeepEqual(got, rowsFirst) {
		t.Fatalf("projects changed across reopen:\nfirst  = %v\nsecond = %v", rowsFirst, got)
	}
	var markerSecond string
	if err := second.db.QueryRow(`SELECT completed_at FROM schema_backfills WHERE name = ?`, projectsBackfillMarker).Scan(&markerSecond); err != nil || markerSecond != marker {
		t.Fatalf("marker after reopen = %q (%v), want unchanged %q", markerSecond, err, marker)
	}
	if scalar(t, second, `SELECT COUNT(*) FROM schema_backfills WHERE name = ?`, projectsBackfillMarker) != 1 {
		t.Fatalf("want exactly one backfill marker row")
	}
}

func TestStoreBackfillDoesNotResurrectADeletedProject(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.db")

	legacy := newSchemaOnlyStore(t, path)
	seedLegacyRepositories(t, legacy, map[string][]string{"tenant-a": {"odabank/api"}})
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DeleteRepository(context.Background(), "tenant-a", domain.MustParseRepositoryRef("odabank/api")); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProjectIfEmpty(context.Background(), "tenant-a", "odabank"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := projectNames(t, reopened, "tenant-a"); len(got) != 0 {
		t.Fatalf("projects after reopen = %v, want none (the marker gates the backfill)", got)
	}
}

// tableChecksums hashes every row of every table except the excluded ones, in
// a deterministic order.
func tableChecksums(t *testing.T, store *Store, exclude map[string]bool) map[string]string {
	t.Helper()
	tables, err := store.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !exclude[name] {
			names = append(names, name)
		}
	}
	tables.Close()

	sums := map[string]string{}
	for _, name := range names {
		rows, err := store.db.Query(`SELECT * FROM ` + name + ` ORDER BY rowid`)
		if err != nil {
			t.Fatalf("select %s: %v", name, err)
		}
		columns, _ := rows.Columns()
		hash := sha256.New()
		fmt.Fprintf(hash, "%v\n", columns)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(hash, "%v\n", values)
		}
		rows.Close()
		sums[name] = fmt.Sprintf("%x", hash.Sum(nil))
	}
	return sums
}

func TestStoreMigratesPreviousSchemaKeepingEveryExistingTableIdentical(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "registry.db")

	// A database at the previous schema: fully populated, no projects table.
	prior, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	publishInto(t, prior, "tenant-a", "odabank/api", "v1", `{"schemaVersion":2,"n":1}`)
	publishInto(t, prior, "tenant-a", "alpine", "latest", `{"schemaVersion":2,"n":2}`)
	publishInto(t, prior, "tenant-b", "odabank/web", "v1", `{"schemaVersion":2,"n":3}`)
	saveActiveUpload(t, prior, "tenant-a", "up-1", "odabank/api")
	if err := prior.LinkBlob(ctx, "tenant-a", domain.MustParseRepositoryRef("odabank/api"), domain.DigestFromBytes([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TABLE projects`,
		`DELETE FROM schema_backfills WHERE name = '` + projectsBackfillMarker + `'`,
	} {
		if _, err := prior.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	exclude := map[string]bool{"projects": true, "schema_backfills": true}
	before := tableChecksums(t, prior, exclude)
	if len(before) < 5 {
		t.Fatalf("checksummed only %d tables, want every existing table", len(before))
	}
	if err := prior.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := New(path)
	if err != nil {
		t.Fatalf("New() on the previous schema error = %v", err)
	}
	defer migrated.Close()

	after := tableChecksums(t, migrated, exclude)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("existing tables changed by the migration:\nbefore = %v\nafter  = %v", before, after)
	}
	if got := projectNames(t, migrated, "tenant-a"); !reflect.DeepEqual(got, []string{"odabank"}) {
		t.Fatalf("tenant-a projects = %v, want [odabank]", got)
	}
	if got := projectNames(t, migrated, "tenant-b"); !reflect.DeepEqual(got, []string{"odabank"}) {
		t.Fatalf("tenant-b projects = %v, want [odabank]", got)
	}
}

// Pins that the projects table is not consulted by existing paths: pushing,
// listing and the catalog behave identically with and without project rows.
func TestStoreRepositoryListingAndPushIgnoreTheProjectsTable(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// No project exists for "ghost" and the push still works.
	publishInto(t, store, "tenant-a", "ghost/app", "v1", `{"schemaVersion":2,"n":1}`)
	publishInto(t, store, "tenant-a", "alpine", "latest", `{"schemaVersion":2,"n":2}`)
	before, err := store.ListRepositoriesWithSummary(ctx, "tenant-a", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	catalogBefore, _ := store.Catalog(ctx, "tenant-a", 0, "")

	// Unrelated and empty projects change nothing in the listings.
	for _, name := range []string{"ghost", "empty", "zzz"} {
		if err := store.CreateProject(ctx, "tenant-a", name, "root"); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := store.ListRepositoriesWithSummary(ctx, "tenant-a", 0, "")
	catalogAfter, _ := store.Catalog(ctx, "tenant-a", 0, "")
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(catalogBefore, catalogAfter) {
		t.Fatalf("listings changed by project rows:\nbefore = %v / %v\nafter  = %v / %v", before, catalogBefore, after, catalogAfter)
	}
	if len(before) != 2 {
		t.Fatalf("repositories = %v, want ghost/app and alpine", before)
	}
}

func TestStoreProjectsConcurrentWithPushes(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	report := func(err error) {
		if err != nil {
			errs <- err
		}
	}
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("proj%d", i)
		wg.Add(3)
		go func() {
			defer wg.Done()
			report(store.CreateProject(ctx, "tenant-a", name, "root"))
		}()
		go func() {
			defer wg.Done()
			// Delete races the create: success, not found or non-empty are all fine.
			err := store.DeleteProjectIfEmpty(ctx, "tenant-a", name)
			if err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) && !errors.Is(err, domain.ErrProjectNotEmpty) {
				report(err)
			}
		}()
		go func() {
			defer wg.Done()
			_, err := store.ListProjects(ctx, "tenant-a")
			report(err)
		}()
	}
	// A push-like writer runs next to them.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			blobs := []domain.Descriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer")), Size: 5}}
			manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(fmt.Sprintf(`{"schemaVersion":2,"n":%d}`, i)), nil, blobs, nil, nil)
			if err != nil {
				report(err)
				return
			}
			report(store.PublishManifest(ctx, "tenant-a", domain.MustParseRepositoryRef("proj0/app"), fmt.Sprintf("t%d", i), manifest, blobs))
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// The invariant: a project holding a repository is never deleted.
	if scalar(t, store, `SELECT COUNT(*) FROM repositories WHERE name = 'proj0/app'`) != 1 {
		t.Fatalf("proj0/app repository missing")
	}
	projects, err := store.ListProjects(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		if project.Name == "proj0" && project.Repositories != 1 {
			t.Fatalf("proj0 = %+v, want one repository", project)
		}
	}
}

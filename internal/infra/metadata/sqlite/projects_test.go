package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

func publishInto(t *testing.T, store *Store, tenant string, repository string, tag string, payload string) {
	t.Helper()
	blobs := []domain.Descriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer")), Size: 5}}
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(payload), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), tenant, domain.MustParseRepositoryRef(repository), tag, manifest, blobs); err != nil {
		t.Fatalf("PublishManifest(%s) error = %v", repository, err)
	}
}

func saveActiveUpload(t *testing.T, store *Store, tenant string, id string, repository string) {
	t.Helper()
	now := time.Now().UTC()
	state := domain.UploadState{ID: id, Repository: domain.MustParseRepositoryRef(repository), Status: domain.UploadStatusActive, StartedAt: now, UpdatedAt: now, Location: "loc"}
	if err := store.SaveUpload(context.Background(), tenant, state); err != nil {
		t.Fatalf("SaveUpload() error = %v", err)
	}
}

func projectNames(t *testing.T, store *Store, tenant string) []string {
	t.Helper()
	projects, err := store.ListProjects(context.Background(), tenant)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	names := []string{}
	for _, project := range projects {
		names = append(names, project.Name)
	}
	return names
}

func TestStoreCreateProjectAndExistsAreTenantScoped(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if exists, err := store.ProjectExists(ctx, "tenant-a", "odabank"); err != nil || exists {
		t.Fatalf("ProjectExists() before create = (%v, %v), want (false, nil)", exists, err)
	}
	if err := store.CreateProject(ctx, "tenant-a", "odabank", "root"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if exists, err := store.ProjectExists(ctx, "tenant-a", "odabank"); err != nil || !exists {
		t.Fatalf("ProjectExists() after create = (%v, %v), want (true, nil)", exists, err)
	}
	if exists, _ := store.ProjectExists(ctx, "tenant-b", "odabank"); exists {
		t.Fatalf("ProjectExists() in another tenant = true, want false")
	}
	// The same name is free in the other tenant.
	if err := store.CreateProject(ctx, "tenant-b", "odabank", "root"); err != nil {
		t.Fatalf("CreateProject() in tenant-b error = %v", err)
	}

	projects, err := store.ListProjects(ctx, "tenant-a")
	if err != nil || len(projects) != 1 {
		t.Fatalf("ListProjects(tenant-a) = %v, %v, want one project", projects, err)
	}
	got := projects[0]
	if got.Name != "odabank" || got.CreatedBy != "root" || got.CreatedAt.IsZero() || got.Repositories != 0 || got.Tags != 0 || !got.LastPushed.IsZero() {
		t.Fatalf("project = %+v, want an empty project created by root", got)
	}
}

func TestStoreCreateProjectDuplicateIsRefused(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.CreateProject(ctx, "tenant-a", "odabank", "root"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	err := store.CreateProject(ctx, "tenant-a", "odabank", "someone-else")
	if !domain.IsCode(err, domain.ErrorCodeConflict) || !errors.Is(err, domain.ErrProjectExists) {
		t.Fatalf("duplicate CreateProject() error = %v, want conflict wrapping ErrProjectExists", err)
	}
	projects, _ := store.ListProjects(ctx, "tenant-a")
	if len(projects) != 1 || projects[0].CreatedBy != "root" {
		t.Fatalf("projects after duplicate = %+v, want the original untouched", projects)
	}
}

func TestStoreListProjectsCountsByExactFirstSegment(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	for _, name := range []string{"odabank", "odabank2", "od_bank", "odxbank", "gosse"} {
		if err := store.CreateProject(ctx, "tenant-a", name, "root"); err != nil {
			t.Fatalf("CreateProject(%s) error = %v", name, err)
		}
	}
	publishInto(t, store, "tenant-a", "odabank/payments/strike", "v1", `{"schemaVersion":2,"n":1}`)
	publishInto(t, store, "tenant-a", "odabank/payments/strike", "v2", `{"schemaVersion":2,"n":2}`)
	publishInto(t, store, "tenant-a", "odabank/api", "v1", `{"schemaVersion":2,"n":3}`)
	publishInto(t, store, "tenant-a", "odabank2/web", "v1", `{"schemaVersion":2,"n":4}`)
	// "odxbank" must not be matched by a LIKE wildcard in "od_bank".
	publishInto(t, store, "tenant-a", "odxbank/web", "v1", `{"schemaVersion":2,"n":5}`)
	// Another tenant's repository never counts.
	publishInto(t, store, "tenant-b", "gosse/web", "v1", `{"schemaVersion":2,"n":6}`)

	projects, err := store.ListProjects(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	type counts struct{ repos, tags int }
	got := map[string]counts{}
	order := []string{}
	for _, project := range projects {
		got[project.Name] = counts{project.Repositories, project.Tags}
		order = append(order, project.Name)
	}
	want := map[string]counts{
		"odabank":  {2, 3},
		"odabank2": {1, 1},
		"od_bank":  {0, 0},
		"odxbank":  {1, 1},
		"gosse":    {0, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(order, []string{"gosse", "od_bank", "odabank", "odabank2", "odxbank"}) {
		t.Fatalf("order = %v, want sorted by name", order)
	}
	for _, project := range projects {
		if project.Name == "odabank" && project.LastPushed.IsZero() {
			t.Fatalf("odabank LastPushed is zero, want the latest manifest time")
		}
		if project.Name == "gosse" && !project.LastPushed.IsZero() {
			t.Fatalf("gosse LastPushed = %v, want zero", project.LastPushed)
		}
	}
}

func TestStoreDeleteProjectIsExactAndRefusesNonEmpty(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	for _, name := range []string{"odabank", "odabank2"} {
		if err := store.CreateProject(ctx, "tenant-a", name, "root"); err != nil {
			t.Fatalf("CreateProject(%s) error = %v", name, err)
		}
	}
	publishInto(t, store, "tenant-a", "odabank2/web", "v1", `{"schemaVersion":2}`)

	// odabank holds nothing: odabank2/web must not make it non-empty.
	if err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "odabank"); err != nil {
		t.Fatalf("DeleteProjectIfEmpty(odabank) error = %v, want success", err)
	}
	if got := projectNames(t, store, "tenant-a"); !reflect.DeepEqual(got, []string{"odabank2"}) {
		t.Fatalf("projects = %v, want only odabank2", got)
	}

	err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "odabank2")
	if !domain.IsCode(err, domain.ErrorCodeConflict) || !errors.Is(err, domain.ErrProjectNotEmpty) {
		t.Fatalf("DeleteProjectIfEmpty(odabank2) error = %v, want non-empty conflict", err)
	}
	if got := projectNames(t, store, "tenant-a"); !reflect.DeepEqual(got, []string{"odabank2"}) {
		t.Fatalf("projects after refused delete = %v, want odabank2 kept", got)
	}

	if err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "missing"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteProjectIfEmpty(missing) error = %v, want not found", err)
	}
	if err := store.DeleteProjectIfEmpty(ctx, "tenant-b", "odabank2"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteProjectIfEmpty in another tenant error = %v, want not found", err)
	}
}

func TestStoreDeleteProjectAfterRepositoryDeleteSucceeds(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.CreateProject(ctx, "tenant-a", "odabank", "root"); err != nil {
		t.Fatal(err)
	}
	publishInto(t, store, "tenant-a", "odabank/api", "v1", `{"schemaVersion":2}`)
	if _, _, err := store.DeleteRepository(ctx, "tenant-a", domain.MustParseRepositoryRef("odabank/api")); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "odabank"); err != nil {
		t.Fatalf("DeleteProjectIfEmpty() error = %v, want success once the repository is gone", err)
	}
}

func TestStoreDeleteProjectWithOpenUploadIsRefused(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.CreateProject(ctx, "tenant-a", "odabank", "root"); err != nil {
		t.Fatal(err)
	}
	saveActiveUpload(t, store, "tenant-a", "up-1", "odabank/api")
	// An upload of a similarly named project or another tenant does not count.
	saveActiveUpload(t, store, "tenant-a", "up-2", "odabank2/api")
	saveActiveUpload(t, store, "tenant-b", "up-3", "odabank/api")

	err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "odabank")
	if !errors.Is(err, domain.ErrProjectNotEmpty) {
		t.Fatalf("DeleteProjectIfEmpty() error = %v, want non-empty conflict (open upload)", err)
	}

	if err := store.DeleteUpload(ctx, "tenant-a", "up-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "odabank"); err != nil {
		t.Fatalf("DeleteProjectIfEmpty() after the upload closed error = %v", err)
	}
}

func TestStoreDeleteProjectRemovesOrphanBlobLinksOfItsPrefixOnly(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	if err := store.CreateProject(ctx, "tenant-a", "odabank", "root"); err != nil {
		t.Fatal(err)
	}
	digest := domain.DigestFromBytes([]byte("orphan"))
	if err := store.LinkBlob(ctx, "tenant-a", domain.MustParseRepositoryRef("odabank/api"), digest); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkBlob(ctx, "tenant-a", domain.MustParseRepositoryRef("odabank2/api"), digest); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProjectIfEmpty(ctx, "tenant-a", "odabank"); err != nil {
		t.Fatal(err)
	}
	if linked, _ := store.IsBlobLinked(ctx, "tenant-a", domain.MustParseRepositoryRef("odabank/api"), digest); linked {
		t.Fatalf("odabank/api blob link survived the project delete")
	}
	if linked, _ := store.IsBlobLinked(ctx, "tenant-a", domain.MustParseRepositoryRef("odabank2/api"), digest); !linked {
		t.Fatalf("odabank2/api blob link was removed by deleting odabank")
	}
}

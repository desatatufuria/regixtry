package regixtry

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

type projectFixture struct {
	service  *Service
	projects *ProjectService
	store    *metadata.Store
}

func newProjectFixture(t *testing.T, deleteEnabled bool) projectFixture {
	t.Helper()
	store, err := metadata.New(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := NewService(nil, store, nil, nil, nil)
	service.SetDeleteEnabled(deleteEnabled)
	return projectFixture{service: service, projects: NewProjectService(service, store), store: store}
}

func adminContext() context.Context {
	return ports.ContextWithPrincipal(context.Background(), domainauth.Principal{Username: "root", IsAdmin: true})
}

type recordingCleaner struct {
	mu       sync.Mutex
	projects []string
	err      error
}

func (c *recordingCleaner) PurgeProjectGrants(_ context.Context, project string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.projects = append(c.projects, project)
	return c.err
}

func TestProjectServiceCreateListExistsAndDelete(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	exists, err := fixture.projects.ProjectExists(ctx, "odabank")
	if err != nil || !exists {
		t.Fatalf("ProjectExists() = (%v, %v), want (true, nil)", exists, err)
	}
	projects, err := fixture.projects.ListProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0].Name != "odabank" || projects[0].CreatedBy != "root" {
		t.Fatalf("ListProjects() = %+v, %v, want odabank created by root", projects, err)
	}

	if err := fixture.projects.DeleteProject(ctx, "odabank"); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); exists {
		t.Fatalf("project still exists after delete")
	}
}

func TestProjectServiceErrorsAreTyped(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()

	for _, name := range []string{"", "Odabank", "a/b", "blobs", strings.Repeat("a", 65)} {
		if err := fixture.projects.CreateProject(ctx, name); !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Errorf("CreateProject(%q) error = %v, want validation", name, err)
		}
		if err := fixture.projects.DeleteProject(ctx, name); !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Errorf("DeleteProject(%q) error = %v, want validation", name, err)
		}
		if _, err := fixture.projects.ProjectExists(ctx, name); !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Errorf("ProjectExists(%q) error = %v, want validation", name, err)
		}
	}

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.projects.CreateProject(ctx, "odabank"); !domain.IsCode(err, domain.ErrorCodeConflict) || !errors.Is(err, domain.ErrProjectExists) {
		t.Fatalf("duplicate CreateProject() error = %v, want conflict", err)
	}
	if err := fixture.projects.DeleteProject(ctx, "missing"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteProject(missing) error = %v, want not found", err)
	}
}

func TestProjectServiceDeleteRefusesNonEmptyProject(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	upload := domain.UploadState{ID: "up-1", Repository: domain.MustParseRepositoryRef("odabank/api"), Status: domain.UploadStatusActive, StartedAt: now, UpdatedAt: now, Location: "loc"}
	if err := fixture.store.SaveUpload(ctx, ports.DefaultTenant, upload); err != nil {
		t.Fatal(err)
	}
	if err := fixture.projects.DeleteProject(ctx, "odabank"); !errors.Is(err, domain.ErrProjectNotEmpty) || !domain.IsCode(err, domain.ErrorCodeConflict) {
		t.Fatalf("DeleteProject() error = %v, want non-empty conflict", err)
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); !exists {
		t.Fatalf("a refused delete removed the project")
	}
}

func TestProjectServiceRefusesEveryMethodForNonAdmins(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	if err := fixture.projects.CreateProject(adminContext(), "odabank"); err != nil {
		t.Fatal(err)
	}

	scopeLimited := domainauth.Principal{Username: "root", IsAdmin: true, Scopes: []domainauth.Scope{{Type: "repository", Name: "x", Actions: []string{"pull"}}}}
	callers := map[string]context.Context{
		"anonymous":    context.Background(),
		"regular user": ports.ContextWithPrincipal(context.Background(), domainauth.Principal{Username: "ana"}),
		"repo admin": ports.ContextWithPrincipal(context.Background(), domainauth.Principal{
			Username: "ana",
			Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("odabank/api"), Role: domainauth.RepoRoleAdmin}},
		}),
		"scope-limited admin token": ports.ContextWithPrincipal(context.Background(), scopeLimited),
	}
	for caller, ctx := range callers {
		operations := map[string]func() error{
			"CreateProject": func() error { return fixture.projects.CreateProject(ctx, "gosse") },
			"DeleteProject": func() error { return fixture.projects.DeleteProject(ctx, "odabank") },
			"ListProjects":  func() error { _, err := fixture.projects.ListProjects(ctx); return err },
			"ProjectExists": func() error { _, err := fixture.projects.ProjectExists(ctx, "odabank"); return err },
		}
		for operation, run := range operations {
			err := run()
			if !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) && !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
				t.Errorf("%s as %s: error = %v, want forbidden or unauthorized", operation, caller, err)
			}
		}
	}

	// Nothing leaked through: the project is intact and gosse was not created.
	projects, err := fixture.projects.ListProjects(adminContext())
	if err != nil || len(projects) != 1 || projects[0].Name != "odabank" {
		t.Fatalf("projects after refused calls = %+v, %v, want only odabank", projects, err)
	}
}

func TestProjectServiceDeleteRequiresDeletionEnabled(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, false)
	ctx := adminContext()
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatalf("CreateProject() must not need deletion enabled: %v", err)
	}

	err := fixture.projects.DeleteProject(ctx, "odabank")
	if !domain.IsCode(err, domain.ErrorCodeValidation) || !strings.Contains(err.Error(), "project deletion is not enabled") {
		t.Fatalf("DeleteProject() error = %v, want validation 'project deletion is not enabled'", err)
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); !exists {
		t.Fatalf("project removed although deletion is disabled")
	}

	// Authorization is checked before the flag: a non-admin learns nothing.
	if err := fixture.projects.DeleteProject(context.Background(), "odabank"); strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("anonymous caller learned the deletion flag state: %v", err)
	}
}

func TestProjectServiceGrantCleanupSeam(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cleaner := &recordingCleaner{}
	fixture.projects.SetGrantCleaner(cleaner)

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	if len(cleaner.projects) != 0 {
		t.Fatalf("cleaner called on create: %v", cleaner.projects)
	}

	// A refused delete never purges grants.
	if err := fixture.store.CreateProject(ctx, ports.DefaultTenant, "busy", "root"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := fixture.store.SaveUpload(ctx, ports.DefaultTenant, domain.UploadState{ID: "u", Repository: domain.MustParseRepositoryRef("busy/x"), Status: domain.UploadStatusActive, StartedAt: now, UpdatedAt: now, Location: "l"}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.projects.DeleteProject(ctx, "busy"); err == nil {
		t.Fatalf("delete of a busy project succeeded")
	}
	if len(cleaner.projects) != 0 {
		t.Fatalf("cleaner called for a refused delete: %v", cleaner.projects)
	}

	if err := fixture.projects.DeleteProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cleaner.projects, []string{"odabank"}) {
		t.Fatalf("cleaner calls = %v, want [odabank] after the delete", cleaner.projects)
	}
}

func TestProjectServiceGrantCleanupFailureDoesNotUndoTheDelete(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cleaner := &recordingCleaner{err: errors.New("auth store down")}
	fixture.projects.SetGrantCleaner(cleaner)

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	err := fixture.projects.DeleteProject(ctx, "odabank")
	if err == nil || !strings.Contains(err.Error(), "auth store down") {
		t.Fatalf("DeleteProject() error = %v, want the cleanup failure reported", err)
	}
	// The project is gone: orphan grants are harmless, a project without
	// grants for its members is not.
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); exists {
		t.Fatalf("project survived a cleanup failure")
	}
}

func TestProjectServiceConcurrentUse(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()

	var wg sync.WaitGroup
	for _, name := range []string{"a1", "a2", "a3", "a4"} {
		wg.Add(3)
		go func() { defer wg.Done(); _ = fixture.projects.CreateProject(ctx, name) }()
		go func() { defer wg.Done(); _ = fixture.projects.DeleteProject(ctx, name) }()
		go func() { defer wg.Done(); _, _ = fixture.projects.ListProjects(ctx) }()
	}
	wg.Wait()
}

func TestProjectsDoNotChangeRepositoryDeletion(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	// Existing behavior pinned: repository deletion neither consults nor
	// touches projects.
	if _, err := fixture.service.DeleteRepository(ctx, "odabank/missing"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteRepository() error = %v, want not found", err)
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); !exists {
		t.Fatalf("DeleteRepository touched the project")
	}
}

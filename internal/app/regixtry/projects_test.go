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

	appauth "regixtry/internal/app/auth"
	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
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
	// removed is what a purge reports; before runs first (under no lock).
	removed int64
	before  func(project string)
}

func (c *recordingCleaner) PurgeProjectGrants(_ context.Context, project string) (int64, error) {
	if c.before != nil {
		c.before(project)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.projects = append(c.projects, project)
	return c.removed, c.err
}

func (c *recordingCleaner) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.projects...)
}

type recordedProjectEvent struct {
	actor   string
	action  domainauth.AccessAction
	project string
}

type recordingEvents struct {
	mu     sync.Mutex
	events []recordedProjectEvent
}

func (r *recordingEvents) RecordProjectEvent(_ context.Context, actor domainauth.Principal, action domainauth.AccessAction, project string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedProjectEvent{actor: actor.Username, action: action, project: project})
}

func (r *recordingEvents) all() []recordedProjectEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedProjectEvent(nil), r.events...)
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
	if !reflect.DeepEqual(cleaner.calls(), []string{"odabank"}) {
		t.Fatalf("cleaner calls after create = %v, want the stale-grant purge of odabank", cleaner.calls())
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
	if !reflect.DeepEqual(cleaner.calls(), []string{"odabank"}) {
		t.Fatalf("cleaner called for a refused delete: %v", cleaner.calls())
	}

	if err := fixture.projects.DeleteProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cleaner.calls(), []string{"odabank", "odabank"}) {
		t.Fatalf("cleaner calls = %v, want the create purge then the delete purge", cleaner.calls())
	}
}

func TestProjectServiceGrantCleanupFailureDoesNotUndoTheDelete(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cleaner := &recordingCleaner{}
	fixture.projects.SetGrantCleaner(cleaner)

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	cleaner.err = errors.New("auth store down")
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

func TestProjectServiceCreatePurgesStaleGrantsBeforeInserting(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cleaner := &recordingCleaner{removed: 3}
	var existedAtPurge bool
	cleaner.before = func(project string) {
		existedAtPurge, _ = fixture.store.ProjectExists(ctx, ports.DefaultTenant, project)
	}
	fixture.projects.SetGrantCleaner(cleaner)

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if existedAtPurge {
		t.Fatal("the purge ran after the project row existed: stale grants could outlive the insert")
	}
	if !reflect.DeepEqual(cleaner.calls(), []string{"odabank"}) {
		t.Fatalf("purge calls = %v", cleaner.calls())
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); !exists {
		t.Fatal("project missing after create")
	}
}

func TestProjectServiceCreateOfAnExistingProjectNeverPurgesItsLiveGrants(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	cleaner := &recordingCleaner{removed: 5}
	fixture.projects.SetGrantCleaner(cleaner)

	err := fixture.projects.CreateProject(ctx, "odabank")
	if !domain.IsCode(err, domain.ErrorCodeConflict) || !errors.Is(err, domain.ErrProjectExists) {
		t.Fatalf("duplicate CreateProject() error = %v, want conflict", err)
	}
	if len(cleaner.calls()) != 0 {
		t.Fatalf("a duplicate create purged grants of a live project: %v", cleaner.calls())
	}
}

func TestProjectServiceCreateFailsWithoutInsertingWhenThePurgeFails(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cause := domainauth.NewForbiddenError("auth store refused")
	fixture.projects.SetGrantCleaner(&recordingCleaner{err: cause})

	err := fixture.projects.CreateProject(ctx, "odabank")
	if err == nil || !errors.Is(err, cause) || !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("CreateProject() error = %v, want the purge failure wrapped with %%w", err)
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); exists {
		t.Fatal("the project was created although stale grants could not be purged")
	}
}

// TestProjectServiceDeleteConvergesAfterAFailedPurge is the cross-store
// orchestration: the project row goes first, a failed purge leaves it gone,
// and a retry of the same delete converges instead of reporting not found.
func TestProjectServiceDeleteConvergesAfterAFailedPurge(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cleaner := &recordingCleaner{}
	events := &recordingEvents{}
	fixture.projects.SetGrantCleaner(cleaner)
	fixture.projects.SetEventRecorder(events)
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}

	cause := domainauth.NewValidationError("auth store down")
	cleaner.err = cause
	err := fixture.projects.DeleteProject(ctx, "odabank")
	if err == nil || !errors.Is(err, cause) || !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
		t.Fatalf("first DeleteProject() error = %v, want the purge failure wrapped with %%w", err)
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); exists {
		t.Fatal("the project row must be gone after the failed purge")
	}

	// Retry with a working cleaner that finds grants: success, not NotFound.
	cleaner.err = nil
	cleaner.removed = 2
	if err := fixture.projects.DeleteProject(ctx, "odabank"); err != nil {
		t.Fatalf("retry DeleteProject() error = %v, want success (cleanup converged)", err)
	}

	// Nothing left to clean either: now it is honestly not found.
	cleaner.removed = 0
	if err := fixture.projects.DeleteProject(ctx, "odabank"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("third DeleteProject() error = %v, want not found", err)
	}

	// The deletion was recorded once, when the row went, not on the retries.
	want := []recordedProjectEvent{
		{"root", domainauth.AccessActionProjectCreated, "odabank"},
		{"root", domainauth.AccessActionProjectDeleted, "odabank"},
	}
	if got := events.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestProjectServiceDeleteOfAnUnknownProjectWithoutACleanerIsNotFound(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	if err := fixture.projects.DeleteProject(adminContext(), "ghost"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteProject() error = %v, want not found", err)
	}
}

func TestProjectServiceRecordsEventsOnlyForRealChanges(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	events := &recordingEvents{}
	fixture.projects.SetEventRecorder(events)

	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	_ = fixture.projects.CreateProject(ctx, "odabank")  // conflict
	_ = fixture.projects.CreateProject(ctx, "Bad Name") // validation
	now := time.Now().UTC()
	upload := domain.UploadState{ID: "up", Repository: domain.MustParseRepositoryRef("odabank/api"), Status: domain.UploadStatusActive, StartedAt: now, UpdatedAt: now, Location: "l"}
	if err := fixture.store.SaveUpload(ctx, ports.DefaultTenant, upload); err != nil {
		t.Fatal(err)
	}
	_ = fixture.projects.DeleteProject(ctx, "odabank") // not empty
	_ = fixture.projects.DeleteProject(ctx, "ghost")   // not found

	want := []recordedProjectEvent{{"root", domainauth.AccessActionProjectCreated, "odabank"}}
	if got := events.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want only the successful create", got)
	}

	// Without deletion enabled nothing is deleted or recorded.
	disabled := newProjectFixture(t, false)
	disabled.projects.SetEventRecorder(events)
	_ = disabled.projects.CreateProject(ctx, "sandbox")
	if err := disabled.projects.DeleteProject(ctx, "sandbox"); !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("DeleteProject() error = %v, want the deletion-disabled refusal", err)
	}
	if got := events.all(); len(got) != 2 {
		t.Fatalf("events = %#v, want the sandbox create only added", got)
	}
}

func TestProjectServiceGetProject(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.projects.CreateProject(ctx, "odabank2"); err != nil {
		t.Fatal(err)
	}

	got, err := fixture.projects.GetProject(ctx, "odabank")
	if err != nil || got.Name != "odabank" || got.CreatedBy != "root" || got.Repositories != 0 {
		t.Fatalf("GetProject() = %+v, %v", got, err)
	}
	if _, err := fixture.projects.GetProject(ctx, "ghost"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetProject(ghost) error = %v, want not found", err)
	}
	if _, err := fixture.projects.GetProject(ctx, "Bad"); !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("GetProject(Bad) error = %v, want validation", err)
	}
	plain := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{Username: "plain"})
	if _, err := fixture.projects.GetProject(plain, "odabank"); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("GetProject(non-admin) error = %v, want forbidden", err)
	}
	if _, err := fixture.projects.GetProject(context.Background(), "odabank"); !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
		t.Fatalf("GetProject(anonymous) error = %v, want unauthorized", err)
	}
}

// TestProjectServiceWithTheRealAuthServiceAsCleanerAndRecorder runs the
// cross-store orchestration against the real auth service (its SQLite-backed
// store stands in for Postgres): stale grants are purged on create, grants and
// events follow the lifecycle, and a deleted-then-recreated name starts clean.
func TestProjectServiceWithTheRealAuthServiceAsCleanerAndRecorder(t *testing.T) {
	t.Parallel()
	ctx := adminContext()
	fixture := newProjectFixture(t, true)
	authStore, err := authpostgres.NewWithDriver("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	authService := appauth.NewService(authStore)
	fixture.projects.SetGrantCleaner(authService)
	fixture.projects.SetEventRecorder(authService)
	authService.SetProjectLookup(projectDirectory{projects: fixture.store})

	now := time.Now().UTC()
	if err := authStore.UpsertUser(ctx, domainauth.User{ID: "u-ana", Username: "ana", PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	admin := domainauth.Principal{UserID: "u-root", Username: "root", IsAdmin: true}

	// An orphan grant of a name that has no project (a failed earlier purge).
	if err := authStore.PutProjectGrant(ctx, domainauth.ProjectGrant{UserID: "u-ana", Project: "odabank", Role: domainauth.RepoRoleAdmin, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}
	if grants, _ := authStore.ListProjectGrants(ctx, "u-ana"); len(grants) != 0 {
		t.Fatalf("a stale grant revived on re-creation: %#v", grants)
	}

	if _, err := authService.PutProjectGrant(ctx, admin, "odabank", "ana", domainauth.RepoRoleWriter); err != nil {
		t.Fatalf("PutProjectGrant() error = %v", err)
	}
	if err := fixture.projects.DeleteProject(ctx, "odabank"); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}
	if grants, _ := authStore.ListProjectGrants(ctx, "u-ana"); len(grants) != 0 {
		t.Fatalf("grants survived the project: %#v", grants)
	}
	// With the project gone the auth service refuses new grants for it.
	if _, err := authService.PutProjectGrant(ctx, admin, "odabank", "ana", domainauth.RepoRoleReader); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("grant for a deleted project error = %v, want not found", err)
	}

	history, err := authStore.ListAccessEvents(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[domainauth.AccessAction]int{}
	for _, event := range history {
		actions[event.Action]++
		if event.Repository != "project:odabank" {
			t.Fatalf("event repository = %q, want project:odabank", event.Repository)
		}
	}
	if actions[domainauth.AccessActionProjectCreated] != 1 || actions[domainauth.AccessActionProjectDeleted] != 1 || actions[domainauth.AccessActionProjectGrantPut] != 1 {
		t.Fatalf("history = %v", actions)
	}
}

// projectDirectory adapts the metadata store to ports.ProjectLookup the way
// cmd/regixtry does.
type projectDirectory struct{ projects ports.ProjectStore }

func (d projectDirectory) ProjectExists(ctx context.Context, project string) (bool, error) {
	return d.projects.ProjectExists(ctx, ports.DefaultTenant, project)
}

// TestProjectServiceLifecycleRacesWithGrants runs under -race: projects are
// created and deleted while grants are put, with the real auth service as the
// cleaner. Whatever interleaving happens, once the last delete finishes no
// grant of a deleted project is left reachable.
func TestProjectServiceLifecycleRacesWithGrants(t *testing.T) {
	t.Parallel()
	ctx := adminContext()
	fixture := newProjectFixture(t, true)
	authStore, err := authpostgres.NewWithDriver("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authStore.Close() })
	authService := appauth.NewService(authStore)
	fixture.projects.SetGrantCleaner(authService)
	fixture.projects.SetEventRecorder(authService)
	authService.SetProjectLookup(projectDirectory{projects: fixture.store})
	now := time.Now().UTC()
	for _, name := range []string{"ana", "bob"} {
		if err := authStore.UpsertUser(ctx, domainauth.User{ID: "u-" + name, Username: name, PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	admin := domainauth.Principal{UserID: "u-root", Username: "root", IsAdmin: true}

	var wg sync.WaitGroup
	for round := 0; round < 6; round++ {
		wg.Add(4)
		go func() { defer wg.Done(); _ = fixture.projects.CreateProject(ctx, "race") }()
		go func() { defer wg.Done(); _ = fixture.projects.DeleteProject(ctx, "race") }()
		go func() {
			defer wg.Done()
			_, _ = authService.PutProjectGrant(ctx, admin, "race", "ana", domainauth.RepoRoleWriter)
		}()
		go func() {
			defer wg.Done()
			_ = authService.DeleteProjectGrant(ctx, admin, "race", "bob")
		}()
	}
	wg.Wait()

	// Converge: delete the project if it is still there; the cleanup is
	// idempotent, so a final delete leaves nothing behind either way.
	_ = fixture.projects.DeleteProject(ctx, "race")
	_ = fixture.projects.DeleteProject(ctx, "race")
	if exists, _ := fixture.projects.ProjectExists(ctx, "race"); exists {
		t.Fatal("project survived the final delete")
	}
	if grants, _ := authStore.ListProjectGrants(ctx, "u-ana"); len(grants) != 0 {
		// A Put that passed the existence check just before a delete can
		// land after its purge: harmless (no project, no repositories) and
		// purged by the next create. The final delete above purged again.
		t.Fatalf("orphan grants remain after the converging delete: %#v", grants)
	}
}

// hangingCleaner blocks until its context ends, like a stalled auth store.
type hangingCleaner struct {
	mu      sync.Mutex
	hang    bool
	removed int64
}

func (c *hangingCleaner) PurgeProjectGrants(ctx context.Context, _ string) (int64, error) {
	c.mu.Lock()
	hang := c.hang
	c.mu.Unlock()
	if hang {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return c.removed, nil
}

func TestProjectServiceBoundsTheCleanerAndReleasesTheLock(t *testing.T) {
	t.Parallel()
	fixture := newProjectFixture(t, true)
	ctx := adminContext()
	cleaner := &hangingCleaner{removed: 1}
	fixture.projects.SetGrantCleaner(cleaner)
	fixture.projects.cleanupTimeout = 100 * time.Millisecond
	if grantCleanupTimeout != 15*time.Second {
		t.Fatalf("grantCleanupTimeout = %v, want 15s", grantCleanupTimeout)
	}
	if err := fixture.projects.CreateProject(ctx, "odabank"); err != nil {
		t.Fatal(err)
	}

	cleaner.mu.Lock()
	cleaner.hang = true
	cleaner.mu.Unlock()
	start := time.Now()
	err := fixture.projects.DeleteProject(ctx, "odabank")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("DeleteProject() = %v after %v, want a deadline error within the bound", err, time.Since(start))
	}
	if exists, _ := fixture.projects.ProjectExists(ctx, "odabank"); exists {
		t.Fatal("the project row must be gone")
	}

	// The lock was released: another project can be created and a create that
	// hits the hanging cleaner fails within the bound too.
	start = time.Now()
	if err := fixture.projects.CreateProject(ctx, "other"); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("CreateProject() = %v after %v, want a bounded failure, not a blocked lock", err, time.Since(start))
	}

	// Retry converges once the store answers.
	cleaner.mu.Lock()
	cleaner.hang = false
	cleaner.mu.Unlock()
	if err := fixture.projects.DeleteProject(ctx, "odabank"); err != nil {
		t.Fatalf("retry DeleteProject() error = %v, want convergence", err)
	}
}

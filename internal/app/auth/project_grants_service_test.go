package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	"regixtry/internal/ports"
)

// fakeProjectLookup is the metadata side of project existence.
type fakeProjectLookup struct {
	mu    sync.Mutex
	names map[string]bool
	err   error
	calls int
}

func (f *fakeProjectLookup) ProjectExists(_ context.Context, project string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.names[project], nil
}

func (f *fakeProjectLookup) add(names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range names {
		f.names[name] = true
	}
}

type projectEnv struct {
	t        *testing.T
	store    *authpostgres.Store
	service  *Service
	projects *fakeProjectLookup
	now      time.Time
	admin    domainauth.Principal
}

func newProjectEnv(t *testing.T, projects ...string) *projectEnv {
	t.Helper()
	store := newRealStore(t)
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	service := NewService(store)
	// A ticking clock keeps the newest-first order of the history strict.
	var tick atomic.Int64
	service.now = func() time.Time { return now.Add(time.Duration(tick.Add(1)) * time.Second) }
	lookup := &fakeProjectLookup{names: map[string]bool{}}
	lookup.add(projects...)
	service.SetProjectLookup(lookup)
	return &projectEnv{t: t, store: store, service: service, projects: lookup, now: now, admin: domainauth.Principal{UserID: "u-admin", Username: "admin", IsAdmin: true}}
}

func (e *projectEnv) user(name string, mutate ...func(*domainauth.User)) domainauth.User {
	e.t.Helper()
	user := domainauth.User{ID: "id-" + name, Username: name, PasswordHash: "h", Enabled: true, CreatedAt: e.now, UpdatedAt: e.now}
	for _, m := range mutate {
		m(&user)
	}
	if err := e.store.UpsertUser(context.Background(), user); err != nil {
		e.t.Fatal(err)
	}
	return user
}

func (e *projectEnv) robot(name string) domainauth.User {
	return e.user(name, func(u *domainauth.User) { u.IsRobot = true; u.PasswordHash = domainauth.RobotPasswordHash })
}

func (e *projectEnv) events() []domainauth.AccessEvent {
	e.t.Helper()
	events, err := e.store.ListAccessEvents(context.Background(), 100)
	if err != nil {
		e.t.Fatal(err)
	}
	return events
}

func (e *projectEnv) put(actor domainauth.Principal, project, username string, role domainauth.RepoRole) error {
	_, err := e.service.PutProjectGrant(context.Background(), actor, project, username, role)
	return err
}

func TestPutProjectGrantByGlobalAdminCreatesAndChangesTheRole(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ana := env.user("ana")
	ctx := context.Background()

	grant, err := env.service.PutProjectGrant(ctx, env.admin, "odabank", "Ana", domainauth.RepoRoleReader)
	if err != nil {
		t.Fatalf("PutProjectGrant() error = %v", err)
	}
	if grant.UserID != ana.ID || grant.Project != "odabank" || grant.Role != domainauth.RepoRoleReader || grant.CreatedAt.IsZero() {
		t.Fatalf("grant = %#v", grant)
	}
	stored, _ := env.store.ListProjectGrants(ctx, ana.ID)
	if len(stored) != 1 || stored[0].Role != domainauth.RepoRoleReader {
		t.Fatalf("stored = %#v", stored)
	}

	// Changing the role updates the single row.
	if _, err := env.service.PutProjectGrant(ctx, env.admin, "odabank", "ana", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	stored, _ = env.store.ListProjectGrants(ctx, ana.ID)
	if len(stored) != 1 || stored[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("after update stored = %#v", stored)
	}

	// Access events: one per change, the project in the repository column.
	events := env.events()
	if len(events) != 2 {
		t.Fatalf("events = %#v, want 2", events)
	}
	newest, oldest := events[0], events[1]
	if oldest.Action != domainauth.AccessActionProjectGrantPut || oldest.Repository != "project:odabank" || oldest.Role != "repo-reader" || oldest.SubjectUsername != "ana" || oldest.SubjectUserID != ana.ID || oldest.ActorUsername != "admin" || oldest.Detail != "" {
		t.Fatalf("first event = %#v", oldest)
	}
	if newest.Action != domainauth.AccessActionProjectGrantPut || newest.Role != "repo-writer" || newest.Detail != "Reader to Writer" {
		t.Fatalf("second event = %#v", newest)
	}

	// Writing the same role again changes nothing and records nothing.
	if _, err := env.service.PutProjectGrant(ctx, env.admin, "odabank", "ana", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if len(env.events()) != 2 {
		t.Fatalf("an unchanged role recorded an event")
	}
}

func TestPutProjectGrantValidationAndExistence(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	env.user("ana")
	ctx := context.Background()

	tests := []struct {
		name    string
		project string
		user    string
		role    domainauth.RepoRole
		code    domainauth.ErrorCode
		regCode regixtrydomain.ErrorCode
	}{
		{name: "invalid role", project: "odabank", user: "ana", role: "owner", code: domainauth.ErrorCodeValidation},
		{name: "empty role", project: "odabank", user: "ana", role: "", code: domainauth.ErrorCodeValidation},
		{name: "unknown user", project: "odabank", user: "ghost", role: domainauth.RepoRoleReader, code: domainauth.ErrorCodeNotFound},
		{name: "missing project", project: "nowhere", user: "ana", role: domainauth.RepoRoleReader, code: domainauth.ErrorCodeNotFound},
		{name: "uppercase project", project: "Odabank", user: "ana", role: domainauth.RepoRoleReader, regCode: regixtrydomain.ErrorCodeValidation},
		{name: "slash in project", project: "odabank/x", user: "ana", role: domainauth.RepoRoleReader, regCode: regixtrydomain.ErrorCodeValidation},
		{name: "reserved project", project: "blobs", user: "ana", role: domainauth.RepoRoleReader, regCode: regixtrydomain.ErrorCodeValidation},
		{name: "empty project", project: "", user: "ana", role: domainauth.RepoRoleReader, regCode: regixtrydomain.ErrorCodeValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := env.put(env.admin, tt.project, tt.user, tt.role)
			switch {
			case tt.code != "":
				if !domainauth.IsCode(err, tt.code) {
					t.Fatalf("error = %v, want auth code %s", err, tt.code)
				}
			default:
				var registryErr *regixtrydomain.Error
				if !errors.As(err, &registryErr) || registryErr.Code != tt.regCode {
					t.Fatalf("error = %v, want registry code %s", err, tt.regCode)
				}
			}
		})
	}
	if grants, _ := env.store.ListProjectGrants(ctx, "id-ana"); len(grants) != 0 {
		t.Fatalf("a refused request stored grants: %#v", grants)
	}
	if len(env.events()) != 0 {
		t.Fatalf("a refused request recorded events: %#v", env.events())
	}
}

func TestPutProjectGrantNeedsAProjectDirectory(t *testing.T) {
	t.Parallel()
	store := newRealStore(t)
	service := NewService(store)
	now := time.Now().UTC()
	seedLoginUser(t, store, now, "ana")
	admin := domainauth.Principal{UserID: "u-admin", Username: "admin", IsAdmin: true}

	_, err := service.PutProjectGrant(context.Background(), admin, "odabank", "ana", domainauth.RepoRoleReader)
	var registryErr *regixtrydomain.Error
	if !errors.As(err, &registryErr) || registryErr.Code != regixtrydomain.ErrorCodeUnsupported {
		t.Fatalf("error = %v, want unsupported when no project directory is installed", err)
	}

	// A lookup failure is an error, never "exists".
	service.SetProjectLookup(&fakeProjectLookup{names: map[string]bool{"odabank": true}, err: errors.New("metadata down")})
	if _, err := service.PutProjectGrant(context.Background(), admin, "odabank", "ana", domainauth.RepoRoleReader); err == nil {
		t.Fatal("lookup failure was treated as success")
	}
}

func TestDeleteProjectGrantByGlobalAdmin(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank", "odabank2")
	ana := env.user("ana")
	ctx := context.Background()
	for _, project := range []string{"odabank", "odabank2"} {
		if err := env.put(env.admin, project, "ana", domainauth.RepoRoleWriter); err != nil {
			t.Fatal(err)
		}
	}
	exact := domainauth.RepoGrant{UserID: ana.ID, Repository: regixtrydomain.MustParseRepositoryRef("odabank/exact"), Role: domainauth.RepoRoleReader, CreatedAt: env.now, UpdatedAt: env.now}
	if err := env.store.PutRepoGrant(ctx, exact); err != nil {
		t.Fatal(err)
	}

	if err := env.service.DeleteProjectGrant(ctx, env.admin, "odabank", "ana"); err != nil {
		t.Fatalf("DeleteProjectGrant() error = %v", err)
	}
	left, _ := env.store.ListProjectGrants(ctx, ana.ID)
	if len(left) != 1 || left[0].Project != "odabank2" {
		t.Fatalf("project grants left = %#v, want only odabank2", left)
	}
	if repos, _ := env.store.ListRepoGrants(ctx, ana.ID); len(repos) != 1 {
		t.Fatalf("the exact repository grant must survive: %#v", repos)
	}
	events := env.events()
	if got := events[0]; got.Action != domainauth.AccessActionProjectGrantDelete || got.Repository != "project:odabank" || got.SubjectUsername != "ana" || got.ActorUsername != "admin" {
		t.Fatalf("event = %#v", got)
	}

	if err := env.service.DeleteProjectGrant(ctx, env.admin, "odabank", "ana"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("second delete error = %v, want not found", err)
	}
	if err := env.service.DeleteProjectGrant(ctx, env.admin, "odabank", "ghost"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("unknown user error = %v, want not found", err)
	}

	// An orphan grant of a project that no longer exists can still be removed.
	env.projects.mu.Lock()
	delete(env.projects.names, "odabank2")
	env.projects.mu.Unlock()
	if err := env.service.DeleteProjectGrant(ctx, env.admin, "odabank2", "ana"); err != nil {
		t.Fatalf("deleting an orphan grant error = %v", err)
	}
}

func TestProjectGrantAdminWrappersMatchTheServiceMethods(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	env.user("ana")
	ctx := context.Background()

	member, err := env.service.PutAdminProjectGrant(ctx, env.admin, ports.AdminPutProjectGrantInput{Project: "odabank", Username: "ANA", Role: domainauth.RepoRoleWriter})
	if err != nil || member.Username != "ana" || member.Role != domainauth.RepoRoleWriter || member.IsRobot {
		t.Fatalf("member = %#v, %v", member, err)
	}
	if err := env.service.DeleteAdminProjectGrant(ctx, env.admin, "odabank", "ana"); err != nil {
		t.Fatal(err)
	}
	if grants, _ := env.store.ListProjectGrants(ctx, "id-ana"); len(grants) != 0 {
		t.Fatalf("grant survived: %#v", grants)
	}
}

func TestProjectEventRecorderWritesProjectLifecycleEvents(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t)

	env.service.RecordProjectEvent(context.Background(), env.admin, domainauth.AccessActionProjectCreated, "odabank")
	env.service.RecordProjectEvent(context.Background(), env.admin, domainauth.AccessActionProjectDeleted, "odabank")
	// Not a project action: ignored, never recorded under a wrong shape.
	env.service.RecordProjectEvent(context.Background(), env.admin, domainauth.AccessActionGrantPut, "odabank")

	events := env.events()
	if len(events) != 2 {
		t.Fatalf("events = %#v, want 2", events)
	}
	if got := events[1]; got.Action != domainauth.AccessActionProjectCreated || got.Repository != "project:odabank" || got.ActorUsername != "admin" || got.ActorUserID != "u-admin" || got.SubjectUsername != "" {
		t.Fatalf("created event = %#v", got)
	}
	if got := events[0]; got.Action != domainauth.AccessActionProjectDeleted || got.Repository != "project:odabank" {
		t.Fatalf("deleted event = %#v", got)
	}
}

func TestPurgeProjectGrantsRemovesOnlyThatProject(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank", "odabank2")
	env.user("ana")
	env.user("bob")
	for _, project := range []string{"odabank", "odabank2"} {
		for _, name := range []string{"ana", "bob"} {
			if err := env.put(env.admin, project, name, domainauth.RepoRoleReader); err != nil {
				t.Fatal(err)
			}
		}
	}

	removed, err := env.service.PurgeProjectGrants(context.Background(), "odabank")
	if err != nil || removed != 2 {
		t.Fatalf("PurgeProjectGrants() = %d, %v, want 2", removed, err)
	}
	if left, _ := env.store.ListProjectGrantsByProject(context.Background(), "odabank2", "", 10); len(left) != 2 {
		t.Fatalf("odabank2 lost grants: %#v", left)
	}
	if removed, err := env.service.PurgeProjectGrants(context.Background(), "odabank"); err != nil || removed != 0 {
		t.Fatalf("second purge = %d, %v, want 0", removed, err)
	}
}

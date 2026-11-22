package auth

import (
	"context"
	"fmt"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

func projectAdminOf(projects ...string) domainauth.Principal {
	return actorWithProjects("padmin", domainauth.RepoRoleAdmin, projects...)
}

func actorWithProjects(name string, role domainauth.RepoRole, projects ...string) domainauth.Principal {
	actor := domainauth.Principal{UserID: "id-" + name, Username: name}
	for _, project := range projects {
		actor.ProjectGrants = append(actor.ProjectGrants, domainauth.ProjectGrant{UserID: actor.UserID, Project: project, Role: role})
	}
	return actor
}

// delegationActors are every kind of caller the project grant API must tell
// apart.
func delegationActors(t *testing.T, admin domainauth.Principal) map[string]domainauth.Principal {
	t.Helper()
	exactAdmin := domainauth.Principal{UserID: "id-exact", Username: "exact", Grants: []domainauth.RepoGrant{
		{UserID: "id-exact", Repository: regixtrydomain.MustParseRepositoryRef("odabank/x"), Role: domainauth.RepoRoleAdmin},
		// An exact admin on the FLAT repository named like the project is not
		// a project admin.
		{UserID: "id-exact", Repository: regixtrydomain.MustParseRepositoryRef("odabank"), Role: domainauth.RepoRoleAdmin},
	}}
	readOnlyAdmin := actorWithProjects("ro", domainauth.RepoRoleAdmin, "odabank")
	readOnlyAdmin.IsReadOnly = true
	robot := actorWithProjects("bot", domainauth.RepoRoleWriter, "odabank")
	limited := projectAdminOf("odabank")
	limited.Scopes = mustScopes(t, "repository:odabank/x:pull")

	return map[string]domainauth.Principal{
		"global admin":              admin,
		"project admin odabank":     projectAdminOf("odabank"),
		"project admin odabank2":    projectAdminOf("odabank2"),
		"project writer":            actorWithProjects("w", domainauth.RepoRoleWriter, "odabank"),
		"project reader":            actorWithProjects("r", domainauth.RepoRoleReader, "odabank"),
		"exact repo admin":          exactAdmin,
		"plain user":                {UserID: "id-plain", Username: "plain"},
		"read-only project admin":   readOnlyAdmin,
		"robot project writer":      robot,
		"scope-limited project adm": limited,
	}
}

// TestProjectGrantAuthorizationMatrix is the delegation matrix: who may list
// members and grant or revoke reader and writer, and only inside their own
// project.
func TestProjectGrantAuthorizationMatrix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type operation struct {
		name string
		run  func(env *projectEnv, actor domainauth.Principal, project string) error
	}
	operations := []operation{
		{"put reader", func(e *projectEnv, a domainauth.Principal, p string) error {
			return e.put(a, p, "target", domainauth.RepoRoleReader)
		}},
		{"put writer", func(e *projectEnv, a domainauth.Principal, p string) error {
			return e.put(a, p, "target", domainauth.RepoRoleWriter)
		}},
		{"put admin", func(e *projectEnv, a domainauth.Principal, p string) error {
			return e.put(a, p, "target", domainauth.RepoRoleAdmin)
		}},
		{"delete grant", func(e *projectEnv, a domainauth.Principal, p string) error {
			return e.service.DeleteProjectGrant(ctx, a, p, "victim")
		}},
		{"delete another admin", func(e *projectEnv, a domainauth.Principal, p string) error {
			return e.service.DeleteProjectGrant(ctx, a, p, "boss")
		}},
		{"list members", func(e *projectEnv, a domainauth.Principal, p string) error {
			_, err := e.service.ListProjectMembers(ctx, a, p, 10, "")
			return err
		}},
		{"list a user's project grants", func(e *projectEnv, a domainauth.Principal, _ string) error {
			_, err := e.service.ListUserProjectGrants(ctx, a, "id-target")
			return err
		}},
	}

	// allowed[actor][operation] lists the projects where the call succeeds.
	allowed := map[string]map[string][]string{
		"global admin": {
			"put reader": {"odabank", "odabank2"}, "put writer": {"odabank", "odabank2"}, "put admin": {"odabank", "odabank2"},
			"delete grant": {"odabank", "odabank2"}, "delete another admin": {"odabank", "odabank2"},
			"list members": {"odabank", "odabank2"}, "list a user's project grants": {"odabank", "odabank2"},
		},
		"project admin odabank": {
			"put reader": {"odabank"}, "put writer": {"odabank"}, "delete grant": {"odabank"}, "list members": {"odabank"},
		},
		"project admin odabank2": {
			"put reader": {"odabank2"}, "put writer": {"odabank2"}, "delete grant": {"odabank2"}, "list members": {"odabank2"},
		},
	}

	for actorName := range delegationActors(t, domainauth.Principal{}) {
		for _, op := range operations {
			for _, project := range []string{"odabank", "odabank2"} {
				t.Run(fmt.Sprintf("%s/%s/%s", actorName, op.name, project), func(t *testing.T) {
					t.Parallel()
					env := newProjectEnv(t, "odabank", "odabank2")
					actor := delegationActors(t, env.admin)[actorName]
					env.user("target")
					env.user("victim")
					boss := env.user("boss")
					for _, p := range []string{"odabank", "odabank2"} {
						_ = env.put(env.admin, p, "victim", domainauth.RepoRoleReader)
						_ = env.put(env.admin, p, "boss", domainauth.RepoRoleAdmin)
					}
					_ = boss

					err := op.run(env, actor, project)
					want := false
					for _, p := range allowed[actorName][op.name] {
						want = want || p == project
					}
					switch {
					case want && err != nil:
						t.Fatalf("error = %v, want success", err)
					case !want && !domainauth.IsCode(err, domainauth.ErrorCodeForbidden):
						t.Fatalf("error = %v, want forbidden", err)
					}
				})
			}
		}
	}
}

func TestProjectAdminCannotEscalateOrTouchAnotherAdmin(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()
	env.user("boss")
	env.user("dev")
	if err := env.put(env.admin, "odabank", "boss", domainauth.RepoRoleAdmin); err != nil {
		t.Fatal(err)
	}
	delegate := projectAdminOf("odabank")

	// Cannot grant project admin, not even to themselves or by upgrade.
	if err := env.put(delegate, "odabank", "dev", domainauth.RepoRoleAdmin); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("granting admin error = %v, want forbidden", err)
	}
	// Cannot change or revoke another project admin.
	if err := env.put(delegate, "odabank", "boss", domainauth.RepoRoleReader); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("demoting an admin error = %v, want forbidden", err)
	}
	if err := env.service.DeleteProjectGrant(ctx, delegate, "odabank", "boss"); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("revoking an admin error = %v, want forbidden", err)
	}
	grants, _ := env.store.ListProjectGrants(ctx, "id-boss")
	if len(grants) != 1 || grants[0].Role != domainauth.RepoRoleAdmin {
		t.Fatalf("the admin grant changed: %#v", grants)
	}
	before := len(env.events())

	// What they may do works and records who did it.
	if err := env.put(delegate, "odabank", "dev", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if err := env.service.DeleteProjectGrant(ctx, delegate, "odabank", "dev"); err != nil {
		t.Fatal(err)
	}
	events := env.events()
	if len(events) != before+2 || events[0].Action != domainauth.AccessActionProjectGrantDelete || events[0].ActorUsername != "padmin" || events[1].Action != domainauth.AccessActionProjectGrantPut || events[1].ActorUserID != "id-padmin" {
		t.Fatalf("events = %#v", events[:min(len(events), 3)])
	}

	// A refused call records nothing.
	if err := env.put(delegate, "odabank", "dev", domainauth.RepoRoleAdmin); err == nil {
		t.Fatal("escalation succeeded")
	}
	if len(env.events()) != before+2 {
		t.Fatal("a refused call recorded an event")
	}
}

// TestProjectGrantAuthorizationComesBeforeExistence pins that a caller without
// rights cannot tell a missing project from an existing one: same refusal,
// same message, every operation.
func TestProjectGrantAuthorizationComesBeforeExistence(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "exists")
	env.user("dev")
	ctx := context.Background()
	callers := map[string]domainauth.Principal{
		"plain user":           {UserID: "id-plain", Username: "plain"},
		"admin of another":     projectAdminOf("other"),
		"project writer":       actorWithProjects("w", domainauth.RepoRoleWriter, "exists", "ghost"),
		"admin of exact grant": {UserID: "id-e", Username: "e", Grants: []domainauth.RepoGrant{{UserID: "id-e", Repository: regixtrydomain.MustParseRepositoryRef("exists/x"), Role: domainauth.RepoRoleAdmin}}},
	}
	for name, caller := range callers {
		for _, op := range []string{"put", "delete", "list"} {
			messages := map[string]string{}
			for _, project := range []string{"exists", "ghost"} {
				var err error
				switch op {
				case "put":
					err = env.put(caller, project, "dev", domainauth.RepoRoleReader)
				case "delete":
					err = env.service.DeleteProjectGrant(ctx, caller, project, "dev")
				case "list":
					_, err = env.service.ListProjectMembers(ctx, caller, project, 10, "")
				}
				if !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
					t.Fatalf("%s %s %s error = %v, want forbidden", name, op, project, err)
				}
				messages[project] = err.Error()
			}
			if messages["exists"] != messages["ghost"] {
				t.Fatalf("%s %s leaks existence: %q vs %q", name, op, messages["exists"], messages["ghost"])
			}
		}
	}

	// The lookup is never even consulted for them.
	if env.projects.calls != 0 {
		t.Fatalf("the project directory was consulted %d times before authorization", env.projects.calls)
	}

	// A caller WITH rights gets the honest not found.
	if err := env.put(env.admin, "ghost", "dev", domainauth.RepoRoleReader); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("global admin on a missing project error = %v, want not found", err)
	}
	if _, err := env.service.ListProjectMembers(ctx, projectAdminOf("ghost"), "ghost", 10, ""); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("project admin of a missing project error = %v, want not found", err)
	}
}

func TestReadOnlyUsersCannotHoldWritingProjectGrants(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()
	env.user("viewer", func(u *domainauth.User) { u.IsReadOnly = true })

	for _, role := range []domainauth.RepoRole{domainauth.RepoRoleWriter, domainauth.RepoRoleAdmin} {
		err := env.put(env.admin, "odabank", "viewer", role)
		if !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
			t.Fatalf("role %s error = %v, want a validation refusal", role, err)
		}
		// Same for a project admin acting.
		if err := env.put(projectAdminOf("odabank"), "odabank", "viewer", role); err == nil {
			t.Fatalf("project admin granted %s to a read-only user", role)
		}
	}
	if grants, _ := env.store.ListProjectGrants(ctx, "id-viewer"); len(grants) != 0 {
		t.Fatalf("a refused grant was stored: %#v", grants)
	}
	if err := env.put(env.admin, "odabank", "viewer", domainauth.RepoRoleReader); err != nil {
		t.Fatalf("reader grant for a read-only user error = %v, want ok", err)
	}
}

// oldRequireAdminOrRepoAdmin is the previous implementation, kept as the
// oracle for the differential.
func oldRequireAdminOrRepoAdmin(actor domainauth.Principal, repository string) error {
	if !actor.IsUnrestricted() {
		return domainauth.NewForbiddenError("a scope-limited token cannot be used for administration")
	}
	if actor.IsAdmin {
		return nil
	}
	for _, grant := range actor.Grants {
		if grant.Repository.String() == repository && grant.Role.AllowsAdmin() {
			return nil
		}
	}
	return domainauth.NewForbiddenError("repository administrator privileges are required")
}

func TestRequireAdminOrRepoAdminIsUnchangedForExactGrants(t *testing.T) {
	t.Parallel()

	roles := []domainauth.RepoRole{"", domainauth.RepoRoleReader, domainauth.RepoRoleWriter, domainauth.RepoRoleAdmin}
	repos := []string{"team/app", "team/other", "team", "odabank/x", "odabank2/x", "odabank"}
	scopes := [][]domainauth.Scope{nil, mustScopes(t, "repository:team/app:pull")}
	count := 0
	for _, role := range roles {
		for _, isAdmin := range []bool{false, true} {
			for _, readOnly := range []bool{false, true} {
				for _, scope := range scopes {
					for _, repo := range repos {
						actor := domainauth.Principal{UserID: "u", IsAdmin: isAdmin, IsReadOnly: readOnly, Scopes: scope}
						if role != "" {
							actor.Grants = []domainauth.RepoGrant{{UserID: "u", Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: role}}
						}
						got, want := requireAdminOrRepoAdmin(actor, repo), oldRequireAdminOrRepoAdmin(actor, repo)
						if (got == nil) != (want == nil) || (got != nil && got.Error() != want.Error()) {
							t.Fatalf("role=%q admin=%t ro=%t scoped=%t %s: got %v, want %v", role, isAdmin, readOnly, scope != nil, repo, got, want)
						}
						count++
					}
				}
			}
		}
	}
	if count != 4*2*2*2*6 {
		t.Fatalf("cases = %d", count)
	}
}

func TestRequireAdminOrRepoAdminHonoursProjectAdmins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		actor domainauth.Principal
		repo  string
		want  bool
	}{
		{"project admin inside the project", projectAdminOf("odabank"), "odabank/payments/strike", true},
		{"project admin on a direct child", projectAdminOf("odabank"), "odabank/x", true},
		{"project admin on a similar project", projectAdminOf("odabank"), "odabank2/x", false},
		{"project admin on the flat repository of that name", projectAdminOf("odabank"), "odabank", false},
		{"project admin elsewhere", projectAdminOf("odabank"), "other/x", false},
		{"project writer", actorWithProjects("w", domainauth.RepoRoleWriter, "odabank"), "odabank/x", false},
		{"project reader", actorWithProjects("r", domainauth.RepoRoleReader, "odabank"), "odabank/x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireAdminOrRepoAdmin(tt.actor, tt.repo)
			if (err == nil) != tt.want {
				t.Fatalf("error = %v, want allowed=%t", err, tt.want)
			}
		})
	}

	readOnly := projectAdminOf("odabank")
	readOnly.IsReadOnly = true
	if err := requireAdminOrRepoAdmin(readOnly, "odabank/x"); err == nil {
		t.Fatal("a read-only principal delegated through a project admin grant")
	}
	limited := projectAdminOf("odabank")
	limited.Scopes = mustScopes(t, "repository:odabank/x:pull")
	if err := requireAdminOrRepoAdmin(limited, "odabank/x"); err == nil {
		t.Fatal("a scope-limited token delegated through a project admin grant")
	}
}

// TestProjectAdminManagesExactGrantsInsideTheProjectWithTheSameBounds is the
// repository grant API seen by a project admin: same escalation limits as an
// exact repository admin, nothing outside the project.
func TestProjectAdminManagesExactGrantsInsideTheProjectWithTheSameBounds(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()
	env.user("dev")
	env.user("peer")
	delegate := projectAdminOf("odabank")
	peerGrant := domainauth.RepoGrant{UserID: "id-peer", Repository: regixtrydomain.MustParseRepositoryRef("odabank/x"), Role: domainauth.RepoRoleAdmin, CreatedAt: env.now, UpdatedAt: env.now}
	if err := env.store.PutRepoGrant(ctx, peerGrant); err != nil {
		t.Fatal(err)
	}

	if _, err := env.service.PutRepositoryGrant(ctx, delegate, "odabank/payments/strike", "dev", domainauth.RepoRoleWriter); err != nil {
		t.Fatalf("grant inside the project error = %v", err)
	}
	if _, err := env.service.ListRepositoryGrants(ctx, delegate, "odabank/payments/strike"); err != nil {
		t.Fatalf("list inside the project error = %v", err)
	}
	if _, err := env.service.PutRepositoryGrant(ctx, delegate, "odabank/x", "dev", domainauth.RepoRoleAdmin); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("granting repo-admin error = %v, want forbidden", err)
	}
	if _, err := env.service.PutRepositoryGrant(ctx, delegate, "odabank/x", "peer", domainauth.RepoRoleReader); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("touching a repo admin error = %v, want forbidden", err)
	}
	if err := env.service.DeleteRepositoryGrant(ctx, delegate, "odabank/x", "peer"); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("revoking a repo admin error = %v, want forbidden", err)
	}
	for _, repo := range []string{"odabank2/x", "odabank", "other/x"} {
		if _, err := env.service.PutRepositoryGrant(ctx, delegate, repo, "dev", domainauth.RepoRoleReader); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("grant on %s error = %v, want forbidden", repo, err)
		}
		if _, err := env.service.ListRepositoryGrants(ctx, delegate, repo); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("list on %s error = %v, want forbidden", repo, err)
		}
	}
}

// TestRepositoryProjectAgreesWithTheRegistryDomain keeps the local first
// segment rule equal to the domain's ProjectOf.
func TestRepositoryProjectAgreesWithTheRegistryDomain(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"odabank", "odabank/x", "odabank/payments/strike", "a/b", "team.x/y", "odabank2/x"} {
		got, gotOK := repositoryProject(name)
		want, wantOK := regixtrydomain.ProjectOf(regixtrydomain.MustParseRepositoryRef(name))
		if got != want || gotOK != wantOK {
			t.Fatalf("%q: repositoryProject = (%q, %t), domain = (%q, %t)", name, got, gotOK, want, wantOK)
		}
	}
}

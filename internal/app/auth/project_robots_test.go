package auth

import (
	"context"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func (e *projectEnv) assertNoRobotLeftovers() {
	e.t.Helper()
	robots, err := e.store.ListRobots(context.Background())
	if err != nil || len(robots) != 0 {
		e.t.Fatalf("robots left behind: %#v, %v", robots, err)
	}
}

func TestCreateRobotWithProjectGrantsOneProjectRole(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank", "odabank2")
	ctx := context.Background()

	for _, role := range []domainauth.RepoRole{domainauth.RepoRoleReader, domainauth.RepoRoleWriter} {
		created, err := env.service.CreateRobot(ctx, env.admin, ports.CreateRobotInput{Name: "ci-" + string(role), Project: "odabank", Role: role, TTL: 24 * time.Hour})
		if err != nil {
			t.Fatalf("CreateRobot(project, %s) error = %v", role, err)
		}
		if !created.User.IsRobot || created.Secret == "" || created.Accessor == "" {
			t.Fatalf("created = %#v", created)
		}
		if created.Grant.Repository.String() != "" || created.ProjectGrant.Project != "odabank" || created.ProjectGrant.Role != role || created.ProjectGrant.UserID != created.User.ID {
			t.Fatalf("grants = %#v / %#v, want only the project grant", created.Grant, created.ProjectGrant)
		}
		projects, _ := env.store.ListProjectGrants(ctx, created.User.ID)
		repos, _ := env.store.ListRepoGrants(ctx, created.User.ID)
		if len(projects) != 1 || projects[0].Project != "odabank" || projects[0].Role != role || len(repos) != 0 {
			t.Fatalf("stored project=%#v repo=%#v", projects, repos)
		}
		// Token semantics are the repo robot's: an admin credential with the
		// requested lifetime, no scopes of its own.
		token, err := env.store.GetTokenByAccessor(ctx, domainauth.TokenKindAdminCredential, created.Accessor)
		if err != nil || token.UserID != created.User.ID || token.ExpiresAt.Sub(token.CreatedAt) != 24*time.Hour {
			t.Fatalf("token = %#v, %v, want an admin credential living 24h", token, err)
		}
	}
}

func TestCreateRobotWithProjectRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		input ports.CreateRobotInput
		check func(t *testing.T, err error)
	}{
		{"project admin role", ports.CreateRobotInput{Name: "bot", Project: "odabank", Role: domainauth.RepoRoleAdmin}, validation},
		{"invalid role", ports.CreateRobotInput{Name: "bot", Project: "odabank", Role: "owner"}, validation},
		{"both repository and project", ports.CreateRobotInput{Name: "bot", Repository: "odabank/x", Project: "odabank", Role: domainauth.RepoRoleReader}, validation},
		{"missing project", ports.CreateRobotInput{Name: "bot", Project: "nowhere", Role: domainauth.RepoRoleReader}, notFound},
		{"uppercase project", ports.CreateRobotInput{Name: "bot", Project: "Odabank", Role: domainauth.RepoRoleReader}, anyError},
		{"project with a slash", ports.CreateRobotInput{Name: "bot", Project: "odabank/x", Role: domainauth.RepoRoleReader}, anyError},
		{"neither repository nor project", ports.CreateRobotInput{Name: "bot", Role: domainauth.RepoRoleReader}, anyError},
		{"ttl above the ceiling", ports.CreateRobotInput{Name: "bot", Project: "odabank", Role: domainauth.RepoRoleReader, TTL: domainauth.MaxAdminTokenTTL + time.Second}, validation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newProjectEnv(t, "odabank")
			_, err := env.service.CreateRobot(ctx, env.admin, tt.input)
			tt.check(t, err)
			env.assertNoRobotLeftovers()
			if len(env.events()) != 0 {
				t.Fatalf("a refused robot recorded events: %#v", env.events())
			}
		})
	}

	// Only global administrators create robots, project admins included.
	env := newProjectEnv(t, "odabank")
	if _, err := env.service.CreateRobot(ctx, projectAdminOf("odabank"), ports.CreateRobotInput{Name: "bot", Project: "odabank", Role: domainauth.RepoRoleReader}); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("project admin creating a robot error = %v, want forbidden", err)
	}
	env.assertNoRobotLeftovers()
}

func validation(t *testing.T, err error) {
	t.Helper()
	if !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func notFound(t *testing.T, err error) {
	t.Helper()
	if !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
}

func anyError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want a refusal")
	}
}

// TestCreateRobotWithRepositoryIsUnchanged pins the repo-only path: same
// result shape, same grant, same event as before the project field existed.
func TestCreateRobotWithRepositoryIsUnchanged(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "team")
	ctx := context.Background()

	created, err := env.service.CreateRobot(ctx, env.admin, ports.CreateRobotInput{Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, TTL: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if created.Grant.Repository.String() != "team/app" || created.Grant.Role != domainauth.RepoRoleWriter || created.ProjectGrant != (domainauth.ProjectGrant{}) {
		t.Fatalf("grants = %#v / %#v", created.Grant, created.ProjectGrant)
	}
	if projects, _ := env.store.ListProjectGrants(ctx, created.User.ID); len(projects) != 0 {
		t.Fatalf("a repository robot got a project grant: %#v", projects)
	}
	events := env.events()
	if len(events) != 1 || events[0].Action != domainauth.AccessActionRobotCreated || events[0].Repository != "team/app" || events[0].Role != "repo-writer" || events[0].SubjectUsername != "ci" || events[0].Detail != "1 day" {
		t.Fatalf("events = %#v", events)
	}

	robots, err := env.service.ListAdminRobots(ctx, env.admin)
	if err != nil || len(robots) != 1 || robots[0].Repository != "team/app" || robots[0].Project != "" || robots[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("robots = %#v, %v", robots, err)
	}
	if lookups := env.projects.calls; lookups != 0 {
		t.Fatalf("a repository robot consulted the project directory %d times", lookups)
	}
	// A repository robot does not need a project directory at all.
	bare := NewService(newRealStore(t))
	if _, err := bare.CreateRobot(ctx, env.admin, ports.CreateRobotInput{Name: "ci2", Repository: "team/app", Role: domainauth.RepoRoleReader}); err != nil {
		t.Fatalf("repository robot without a project directory error = %v", err)
	}
}

func TestProjectRobotEventAndListings(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()

	created, err := env.service.CreateAdminRobot(ctx, env.admin, ports.AdminCreateRobotInput{Name: "release-bot", Project: "odabank", Role: domainauth.RepoRoleWriter, TTL: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if created.Robot.Project != "odabank" || created.Robot.Repository != "" || created.Robot.Role != domainauth.RepoRoleWriter || created.Robot.Username != "release-bot" || created.Secret == "" {
		t.Fatalf("created = %#v", created)
	}
	events := env.events()
	if len(events) != 1 || events[0].Action != domainauth.AccessActionRobotCreated || events[0].Repository != "project:odabank" || events[0].Role != "repo-writer" || events[0].SubjectUsername != "release-bot" {
		t.Fatalf("events = %#v", events)
	}

	robots, err := env.service.ListAdminRobots(ctx, env.admin)
	if err != nil || len(robots) != 1 || robots[0].Project != "odabank" || robots[0].Repository != "" || robots[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("robots = %#v, %v", robots, err)
	}

	// Robots count as members of the project listing.
	page, err := env.service.ListProjectMembers(ctx, env.admin, "odabank", 10, "")
	if err != nil || len(page.Members) != 1 || !page.Members[0].IsRobot || page.Members[0].Username != "release-bot" || page.Members[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("members = %#v, %v", page, err)
	}
}

func TestProjectRobotTokenExchangeFollowsTheProjectRole(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()

	created, err := env.service.CreateRobot(ctx, env.admin, ports.CreateRobotInput{Name: "release-bot", Project: "odabank", Role: domainauth.RepoRoleWriter})
	if err != nil {
		t.Fatal(err)
	}
	login := func(scope string) (string, domainauth.Principal) {
		t.Helper()
		result, err := env.service.LoginWithPreissuedToken(ctx, "release-bot", created.Secret, mustScopes(t, scope))
		if err != nil {
			t.Fatalf("LoginWithPreissuedToken() error = %v", err)
		}
		principal, err := env.service.VerifyAccessToken(ctx, result.BearerToken)
		if err != nil {
			t.Fatal(err)
		}
		return result.Scope, principal
	}

	scope, principal := login("repository:odabank/payments/strike:pull,push")
	if scope != "repository:odabank/payments/strike:pull,push" || !principal.HasWriteAccess("odabank/payments/strike") {
		t.Fatalf("scope = %q", scope)
	}
	for _, name := range []string{"odabank2/x", "odabank", "other/x"} {
		if scope, _ := login("repository:" + name + ":pull,push"); scope != "" {
			t.Fatalf("scope for %q = %q, want nothing", name, scope)
		}
	}

	// Revoking the member's project grant stops the next exchange.
	if err := env.service.DeleteProjectGrant(ctx, env.admin, "odabank", "release-bot"); err != nil {
		t.Fatal(err)
	}
	if scope, _ := login("repository:odabank/payments/strike:pull,push"); scope != "" {
		t.Fatalf("scope after revoke = %q, want nothing", scope)
	}
}

func TestProjectGrantsForRobotsAreReaderOrWriterAndExclusive(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()
	bot := env.robot("ci-bot")

	// Never admin, whoever asks.
	for _, actor := range []domainauth.Principal{env.admin, projectAdminOf("odabank")} {
		if err := env.put(actor, "odabank", "ci-bot", domainauth.RepoRoleAdmin); err == nil {
			t.Fatalf("a robot became project admin through %s", actor.Username)
		}
	}
	for _, role := range []domainauth.RepoRole{domainauth.RepoRoleReader, domainauth.RepoRoleWriter} {
		if err := env.put(projectAdminOf("odabank"), "odabank", "ci-bot", role); err != nil {
			t.Fatalf("project admin granting %s to a robot error = %v", role, err)
		}
	}

	// A robot holds one grant: with an exact repository grant already there a
	// project grant is a conflict.
	other := env.robot("repo-bot")
	exact := domainauth.RepoGrant{UserID: other.ID, Repository: regixtrydomain.MustParseRepositoryRef("odabank/x"), Role: domainauth.RepoRoleReader, CreatedAt: env.now, UpdatedAt: env.now}
	if err := env.store.PutRepoGrant(ctx, exact); err != nil {
		t.Fatal(err)
	}
	if err := env.put(env.admin, "odabank", "repo-bot", domainauth.RepoRoleReader); !domainauth.IsCode(err, domainauth.ErrorCodeConflict) {
		t.Fatalf("project grant for a repository robot error = %v, want conflict", err)
	}
	if grants, _ := env.store.ListProjectGrants(ctx, other.ID); len(grants) != 0 {
		t.Fatalf("a refused grant was stored: %#v", grants)
	}

	// Users are not subject to the single-grant rule.
	env.user("ana")
	if err := env.store.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: "id-ana", Repository: regixtrydomain.MustParseRepositoryRef("odabank/x"), Role: domainauth.RepoRoleReader, CreatedAt: env.now, UpdatedAt: env.now}); err != nil {
		t.Fatal(err)
	}
	if err := env.put(env.admin, "odabank", "ana", domainauth.RepoRoleWriter); err != nil {
		t.Fatalf("project grant for a user with a repository grant error = %v", err)
	}
	_ = bot
}

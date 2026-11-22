package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestListProjectMembersPaginatesUsersAndRobotsOfOneProject(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank", "odabank2")
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		env.user(fmt.Sprintf("user%d", i))
		if err := env.put(env.admin, "odabank", fmt.Sprintf("user%d", i), domainauth.RepoRoleReader); err != nil {
			t.Fatal(err)
		}
	}
	env.robot("ci-bot")
	if err := env.put(env.admin, "odabank", "ci-bot", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	env.user("other")
	if err := env.put(env.admin, "odabank2", "other", domainauth.RepoRoleAdmin); err != nil {
		t.Fatal(err)
	}

	seen := map[string]ports.AdminProjectMember{}
	after := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination does not end")
		}
		page, err := env.service.ListProjectMembers(ctx, env.admin, "odabank", 2, after)
		if err != nil {
			t.Fatalf("ListProjectMembers() error = %v", err)
		}
		if len(page.Members) > 2 || page.Members == nil {
			t.Fatalf("page = %#v", page)
		}
		for _, member := range page.Members {
			if _, dup := seen[member.Username]; dup {
				t.Fatalf("%s served twice", member.Username)
			}
			seen[member.Username] = member
		}
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if len(seen) != 5 {
		t.Fatalf("members = %v, want 4 users and 1 robot, no member of odabank2", seen)
	}
	if bot := seen["ci-bot"]; !bot.IsRobot || bot.Role != domainauth.RepoRoleWriter {
		t.Fatalf("robot member = %#v", bot)
	}
	if user := seen["user0"]; user.IsRobot || user.Role != domainauth.RepoRoleReader || user.CreatedAt.IsZero() {
		t.Fatalf("user member = %#v", user)
	}
	if _, leaked := seen["other"]; leaked {
		t.Fatal("a member of odabank2 leaked into odabank")
	}

	// A project with no members is an empty, non-nil page.
	env.projects.add("empty")
	page, err := env.service.ListProjectMembers(ctx, env.admin, "empty", 10, "")
	if err != nil || page.Members == nil || len(page.Members) != 0 || page.Next != "" {
		t.Fatalf("empty project page = %#v, %v", page, err)
	}

	// Exactly one full page does not invent a next cursor.
	page, err = env.service.ListProjectMembers(ctx, env.admin, "odabank", 5, "")
	if err != nil || len(page.Members) != 5 || page.Next != "" {
		t.Fatalf("full page = %#v, %v", page, err)
	}
}

func TestListProjectMembersRejectsBadInput(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()

	for _, limit := range []int{0, -1, 201} {
		if _, err := env.service.ListProjectMembers(ctx, env.admin, "odabank", limit, ""); !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
			t.Fatalf("limit %d error = %v, want validation", limit, err)
		}
	}
	if _, err := env.service.ListProjectMembers(ctx, env.admin, "odabank", 10, "%%%not-a-cursor"); !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
		t.Fatalf("bad cursor error = %v, want validation", err)
	}
	if _, err := env.service.ListProjectMembers(ctx, env.admin, "nowhere", 10, ""); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("missing project error = %v, want not found", err)
	}
	if _, err := env.service.ListProjectMembers(ctx, env.admin, "Odabank", 10, ""); err == nil {
		t.Fatal("an uppercase project name was accepted")
	}
}

func TestListUserProjectGrantsIsGlobalAdminOnlyAndSorted(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank", "alpha")
	ana := env.user("ana")
	ctx := context.Background()
	for _, project := range []string{"odabank", "alpha"} {
		if err := env.put(env.admin, project, "ana", domainauth.RepoRoleWriter); err != nil {
			t.Fatal(err)
		}
	}

	grants, err := env.service.ListUserProjectGrants(ctx, env.admin, ana.ID)
	if err != nil || len(grants) != 2 || grants[0].Project != "alpha" || grants[1].Project != "odabank" || grants[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("grants = %#v, %v", grants, err)
	}
	none, err := env.service.ListUserProjectGrants(ctx, env.admin, env.user("bob").ID)
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("a user without grants = %#v, %v, want an empty non-nil list", none, err)
	}
	if _, err := env.service.ListUserProjectGrants(ctx, env.admin, "missing"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("unknown user error = %v, want not found", err)
	}
	plain := domainauth.Principal{UserID: ana.ID, Username: "ana", ProjectGrants: []domainauth.ProjectGrant{{UserID: ana.ID, Project: "odabank", Role: domainauth.RepoRoleAdmin}}}
	if _, err := env.service.ListUserProjectGrants(ctx, plain, ana.ID); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("non-admin error = %v, want forbidden", err)
	}
	limited := domainauth.Principal{UserID: "u-admin", Username: "admin", IsAdmin: true, Scopes: mustScopes(t, "repository:odabank/x:pull")}
	if _, err := env.service.ListUserProjectGrants(ctx, limited, ana.ID); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("scope-limited admin error = %v, want forbidden", err)
	}
}

func mustScopes(t *testing.T, raw ...string) []domainauth.Scope {
	t.Helper()
	scopes, err := domainauth.ParseScopes(raw)
	if err != nil {
		t.Fatal(err)
	}
	return scopes
}

func TestAdminListWrappersMatchTheServiceMethods(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	env.user("ana")
	ctx := context.Background()
	if err := env.put(env.admin, "odabank", "ana", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}

	page, err := env.service.ListAdminProjectMembers(ctx, env.admin, "odabank", 10, "")
	if err != nil || len(page.Members) != 1 || page.Members[0].Username != "ana" {
		t.Fatalf("page = %#v, %v", page, err)
	}
	grants, err := env.service.ListAdminUserProjectGrants(ctx, env.admin, "id-ana")
	if err != nil || len(grants) != 1 || grants[0].Project != "odabank" || grants[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("grants = %#v, %v", grants, err)
	}
}

func TestListProjectMembersRejectsCraftedCursors(t *testing.T) {
	t.Parallel()
	env := newProjectEnv(t, "odabank")
	ctx := context.Background()

	for name, raw := range map[string]string{
		"NUL byte":       "id\x00x",
		"only NUL":       "\x00",
		"invalid UTF-8":  "id-\xff\xfe",
		"truncated rune": "id-\xe2\x82",
	} {
		cursor := base64.RawURLEncoding.EncodeToString([]byte(raw))
		if _, err := env.service.ListProjectMembers(ctx, env.admin, "odabank", 10, cursor); !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
			t.Errorf("%s: error = %v, want validation", name, err)
		}
	}
	good := base64.RawURLEncoding.EncodeToString([]byte("id-zzz"))
	if _, err := env.service.ListProjectMembers(ctx, env.admin, "odabank", 10, good); err != nil {
		t.Fatalf("a well-formed cursor error = %v", err)
	}
}

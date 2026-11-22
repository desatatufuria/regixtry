package regixtryhttp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type memberRow struct {
	Username  string `json:"username"`
	IsRobot   bool   `json:"is_robot"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type membersPage struct {
	Members []memberRow `json:"members"`
	Next    string      `json:"next"`
}

func TestAdminProjectMembersShapePaginationAndRoundTrip(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.createProject("odabank2")
	for _, name := range []string{"ana", "bob", "cy"} {
		env.user(name)
	}
	if _, err := env.auth.CreateAdminRobot(context.Background(), env.admin, ports.AdminCreateRobotInput{Name: "ci-bot", Project: "odabank", Role: domainauth.RepoRoleWriter}); err != nil {
		t.Fatal(err)
	}
	env.user("other")
	env.grant("odabank2", "other", domainauth.RepoRoleAdmin)

	// PUT answers with the member and creates; PUT again updates.
	rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/Ana", env.adminToken, `{"role":"repo-reader"}`)
	member := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || len(member) != 5 || member["username"] != "ana" || member["role"] != "repo-reader" || member["is_robot"] != false {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, name := range []string{"bob", "cy"} {
		if rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/"+name, env.adminToken, `{"role":"repo-writer"}`); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s status = %d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/ana", env.adminToken, `{"role":"repo-writer"}`); rec.Code != http.StatusOK || decodeBody[memberRow](t, rec).Role != "repo-writer" {
		t.Fatalf("PUT update status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Pages reassemble to the four members of odabank, no member of odabank2,
	// no id or secret in the bytes.
	seen := map[string]memberRow{}
	after := ""
	for pages := 0; pages < 6; pages++ {
		path := "/admin/v1/projects/odabank/members?limit=3"
		if after != "" {
			path += "&after=" + url.QueryEscape(after)
		}
		rec := env.do(http.MethodGet, path, env.adminToken, "")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("GET status = %d body=%s", rec.Code, rec.Body.String())
		}
		lowered := strings.ToLower(rec.Body.String())
		for _, banned := range []string{"user_id", "hash", "password", "secret", "accessor", "token"} {
			if strings.Contains(lowered, banned) {
				t.Fatalf("body contains %q: %s", banned, rec.Body.String())
			}
		}
		page := decodeBody[membersPage](t, rec)
		for _, row := range page.Members {
			if _, dup := seen[row.Username]; dup {
				t.Fatalf("%s served twice", row.Username)
			}
			seen[row.Username] = row
		}
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if len(seen) != 4 || seen["ci-bot"].IsRobot != true || seen["ana"].Role != "repo-writer" || seen["other"].Username != "" {
		t.Fatalf("members = %v", seen)
	}

	// Empty project: an empty array, not null.
	env.createProject("empty")
	if body := strings.TrimSpace(env.do(http.MethodGet, "/admin/v1/projects/empty/members", env.adminToken, "").Body.String()); body != `{"members":[],"next":""}` {
		t.Fatalf("empty members body = %s", body)
	}

	// Bad input.
	for _, query := range []string{"?limit=0", "?limit=201", "?limit=x", "?after=%25%25%25"} {
		if rec := env.do(http.MethodGet, "/admin/v1/projects/odabank/members"+query, env.adminToken, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET members%s status = %d, want 422", query, rec.Code)
		}
	}

	// DELETE: 204, then 404; the other project is untouched.
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank/members/ana", env.adminToken, ""); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank/members/ana", env.adminToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want 404", rec.Code)
	}
	if rec := env.do(http.MethodGet, "/admin/v1/projects/odabank2/members", env.adminToken, ""); len(decodeBody[membersPage](t, rec).Members) != 1 {
		t.Fatalf("odabank2 changed: %s", rec.Body.String())
	}
}

func TestAdminProjectMemberErrors(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.user("ana")
	env.user("viewer", func(in *ports.AdminCreateUserInput) { in.IsReadOnly = true })
	robot, err := env.auth.CreateAdminRobot(context.Background(), env.admin, ports.AdminCreateRobotInput{Name: "repo-bot", Repository: "odabank/x", Role: domainauth.RepoRoleReader})
	if err != nil {
		t.Fatal(err)
	}
	_ = robot

	tests := []struct {
		name         string
		method, path string
		body         string
		want         int
	}{
		{"invalid role", http.MethodPut, "/admin/v1/projects/odabank/members/ana", `{"role":"owner"}`, 422},
		{"missing role", http.MethodPut, "/admin/v1/projects/odabank/members/ana", `{}`, 422},
		{"unknown field", http.MethodPut, "/admin/v1/projects/odabank/members/ana", `{"role":"repo-reader","x":1}`, 422},
		{"no body", http.MethodPut, "/admin/v1/projects/odabank/members/ana", ``, 422},
		{"unknown user", http.MethodPut, "/admin/v1/projects/odabank/members/ghost", `{"role":"repo-reader"}`, 404},
		{"unknown project", http.MethodPut, "/admin/v1/projects/nowhere/members/ana", `{"role":"repo-reader"}`, 404},
		{"uppercase project", http.MethodPut, "/admin/v1/projects/Odabank/members/ana", `{"role":"repo-reader"}`, 422},
		{"reserved project", http.MethodGet, "/admin/v1/projects/blobs/members", ``, 422},
		{"list unknown project", http.MethodGet, "/admin/v1/projects/nowhere/members", ``, 404},
		{"read-only writer", http.MethodPut, "/admin/v1/projects/odabank/members/viewer", `{"role":"repo-writer"}`, 422},
		{"read-only admin", http.MethodPut, "/admin/v1/projects/odabank/members/viewer", `{"role":"repo-admin"}`, 422},
		{"read-only reader", http.MethodPut, "/admin/v1/projects/odabank/members/viewer", `{"role":"repo-reader"}`, 200},
		{"robot admin", http.MethodPut, "/admin/v1/projects/odabank/members/repo-bot", `{"role":"repo-admin"}`, 422},
		{"robot with repository grant", http.MethodPut, "/admin/v1/projects/odabank/members/repo-bot", `{"role":"repo-reader"}`, 409},
		{"delete unknown user", http.MethodDelete, "/admin/v1/projects/odabank/members/ghost", ``, 404},
		{"delete without grant", http.MethodDelete, "/admin/v1/projects/odabank/members/ana", ``, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := env.do(tt.method, tt.path, env.adminToken, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want >= 400 && errorMessage(t, rec) == "" {
				t.Fatalf("error body = %s", rec.Body.String())
			}
		})
	}
}

// TestAdminProjectAdminEscalationOverHTTP is the delegate path through the
// real router: what a project admin may and may not do, and that a caller
// without rights learns nothing about project existence.
func TestAdminProjectAdminEscalationOverHTTP(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.createProject("odabank2")
	env.user("dev")
	env.user("boss")
	_, padmin := env.user("padmin")
	_, plain := env.user("plain")
	env.grant("odabank", "padmin", domainauth.RepoRoleAdmin)
	env.grant("odabank", "boss", domainauth.RepoRoleAdmin)

	// Allowed: list, grant reader and writer, revoke.
	if rec := env.do(http.MethodGet, "/admin/v1/projects/odabank/members", padmin, ""); rec.Code != http.StatusOK || len(decodeBody[membersPage](t, rec).Members) != 2 {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, role := range []string{"repo-reader", "repo-writer"} {
		if rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/dev", padmin, `{"role":"`+role+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("grant %s status = %d body=%s", role, rec.Code, rec.Body.String())
		}
	}
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank/members/dev", padmin, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", rec.Code)
	}

	// Refused: admin role, another admin, another project, global routes.
	forbidden := []struct{ method, path, body string }{
		{http.MethodPut, "/admin/v1/projects/odabank/members/dev", `{"role":"repo-admin"}`},
		{http.MethodPut, "/admin/v1/projects/odabank/members/boss", `{"role":"repo-reader"}`},
		{http.MethodDelete, "/admin/v1/projects/odabank/members/boss", ``},
		{http.MethodPut, "/admin/v1/projects/odabank/members/padmin", `{"role":"repo-admin"}`},
		{http.MethodGet, "/admin/v1/projects/odabank2/members", ``},
		{http.MethodPut, "/admin/v1/projects/odabank2/members/dev", `{"role":"repo-reader"}`},
		{http.MethodDelete, "/admin/v1/projects/odabank2/members/dev", ``},
		{http.MethodPost, "/admin/v1/projects", `{"name":"mine"}`},
		{http.MethodDelete, "/admin/v1/projects/odabank", ``},
		{http.MethodGet, "/admin/v1/projects", ``},
	}
	for _, tc := range forbidden {
		if rec := env.do(tc.method, tc.path, padmin, tc.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403 body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// No existence oracle: a plain user gets byte-identical refusals for an
	// existing project and a missing one, on every member route.
	for _, tc := range []struct{ method, suffix, body string }{
		{http.MethodGet, "/members", ""},
		{http.MethodPut, "/members/dev", `{"role":"repo-reader"}`},
		{http.MethodDelete, "/members/dev", ""},
	} {
		existing := env.do(tc.method, "/admin/v1/projects/odabank"+tc.suffix, plain, tc.body)
		missing := env.do(tc.method, "/admin/v1/projects/nowhere"+tc.suffix, plain, tc.body)
		if existing.Code != http.StatusForbidden || missing.Code != http.StatusForbidden || existing.Body.String() != missing.Body.String() {
			t.Errorf("%s %s leaks existence: %d %q vs %d %q", tc.method, tc.suffix, existing.Code, existing.Body.String(), missing.Code, missing.Body.String())
		}
	}
}

func TestAdminUserProjectGrantsEndpoint(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.createProject("alpha")
	ana, _ := env.user("ana")
	env.grant("odabank", "ana", domainauth.RepoRoleWriter)
	env.grant("alpha", "ana", domainauth.RepoRoleReader)

	rec := env.do(http.MethodGet, "/admin/v1/users/"+ana.ID+"/project-grants", env.adminToken, "")
	grants := decodeBody[[]map[string]any](t, rec)
	if rec.Code != http.StatusOK || len(grants) != 2 || grants[0]["project"] != "alpha" || grants[0]["role"] != "repo-reader" || grants[1]["project"] != "odabank" || len(grants[0]) != 4 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(env.do(http.MethodGet, "/admin/v1/users/"+env.admin.UserID+"/project-grants", env.adminToken, "").Body.String()); body != `[]` {
		t.Fatalf("no grants body = %s", body)
	}
	if rec := env.do(http.MethodGet, "/admin/v1/users/nope/project-grants", env.adminToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown user status = %d", rec.Code)
	}
	// The existing repository grants route is unchanged and independent.
	if rec := env.do(http.MethodGet, "/admin/v1/users/"+ana.ID+"/grants", env.adminToken, ""); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `[]` {
		t.Fatalf("repository grants status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestProjectGrantWireFlow is the end-to-end flow a customer sees: create the
// project, give a user a project role, exchange a docker-style token scope,
// push with it, then revoke and watch the very next request, and the next
// exchange, get refused.
func TestProjectGrantWireFlow(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.createProject("odabank2")
	env.user("dev")

	if rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/dev", env.adminToken, `{"role":"repo-writer"}`); rec.Code != http.StatusOK {
		t.Fatalf("grant status = %d body=%s", rec.Code, rec.Body.String())
	}

	exchange := func(scope string) (string, string) {
		t.Helper()
		return env.registryToken("dev", scope)
	}

	token, scope := exchange("repository:odabank/payments/strike:push,pull")
	if token == "" || scope != "repository:odabank/payments/strike:pull,push" {
		t.Fatalf("token = %q scope = %q, want the exact repository with pull,push", token, scope)
	}
	env.push("dev", "odabank/payments/strike")

	// A request outside the project is refused with the same token.
	if rec := env.do(http.MethodPost, "/v2/odabank2/payments/blobs/uploads/", token, ""); rec.Code == http.StatusAccepted {
		t.Fatal("the token reached another project")
	}
	// And a new exchange for another project grants nothing.
	if _, scope := exchange("repository:odabank2/payments/strike:push,pull"); scope != "" {
		t.Fatalf("scope for odabank2 = %q, want nothing", scope)
	}

	// Revoke: the next request with the OLD token is refused.
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank/members/dev", env.adminToken, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", rec.Code)
	}
	rec := env.do(http.MethodPost, "/v2/odabank/payments/strike/blobs/uploads/", token, "")
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("request after revoke status = %d, want refusal body=%s", rec.Code, rec.Body.String())
	}
	if _, scope := exchange("repository:odabank/payments/strike:push,pull"); scope != "" {
		t.Fatalf("scope after revoke = %q, want nothing", scope)
	}
}

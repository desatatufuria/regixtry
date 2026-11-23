package regixtryhttp

import (
	"net/http"
	"testing"

	domainauth "regixtry/internal/domain/auth"
)

func TestAdminRobotWithProjectOverHTTP(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")

	rec := env.do(http.MethodPost, "/admin/v1/robots", env.adminToken, `{"name":"release-bot","project":"odabank","role":"repo-writer","ttl":"7d"}`)
	created := decodeBody[map[string]any](t, rec)
	robot, _ := created["robot"].(map[string]any)
	if rec.Code != http.StatusCreated || robot["project"] != "odabank" || robot["repository"] != "" || robot["role"] != "repo-writer" || created["secret"] == "" || created["secret"] == nil {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// The robot listing and the project members both show it.
	robots := decodeBody[[]map[string]any](t, env.do(http.MethodGet, "/admin/v1/robots", env.adminToken, ""))
	if len(robots) != 1 || robots[0]["project"] != "odabank" || robots[0]["repository"] != "" {
		t.Fatalf("robots = %v", robots)
	}
	members := decodeBody[membersPage](t, env.do(http.MethodGet, "/admin/v1/projects/odabank/members", env.adminToken, ""))
	if len(members.Members) != 1 || !members.Members[0].IsRobot || members.Members[0].Username != "release-bot" {
		t.Fatalf("members = %#v", members)
	}

	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"admin role", `{"name":"b1","project":"odabank","role":"repo-admin"}`, 422},
		{"both fields", `{"name":"b2","project":"odabank","repository":"odabank/x","role":"repo-reader"}`, 422},
		{"neither field", `{"name":"b3","role":"repo-reader"}`, 422},
		{"missing project", `{"name":"b4","project":"nowhere","role":"repo-reader"}`, 404},
		{"uppercase project", `{"name":"b5","project":"Odabank","role":"repo-reader"}`, 422},
	} {
		if rec := env.do(http.MethodPost, "/admin/v1/robots", env.adminToken, tc.body); rec.Code != tc.want {
			t.Errorf("%s status = %d, want %d body=%s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	if robots := decodeBody[[]map[string]any](t, env.do(http.MethodGet, "/admin/v1/robots", env.adminToken, "")); len(robots) != 1 {
		t.Fatalf("a refused request left a robot behind: %v", robots)
	}

	// Repository-only creation keeps its exact shape: no project key at all.
	rec = env.do(http.MethodPost, "/admin/v1/robots", env.adminToken, `{"name":"repo-bot","repository":"odabank/api","role":"repo-reader"}`)
	repoRobot := decodeBody[map[string]any](t, rec)["robot"].(map[string]any)
	if rec.Code != http.StatusCreated || repoRobot["repository"] != "odabank/api" || repoRobot["role"] != "repo-reader" {
		t.Fatalf("repo robot status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, has := repoRobot["project"]; has {
		t.Fatalf("a repository robot gained a project key: %v", repoRobot)
	}
}

// TestAdminRobotOneGrantOverHTTP drives the one-grant invariant through the
// real router, every combination, admin and delegate routes.
func TestAdminRobotOneGrantOverHTTP(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.createProject("odabank2")
	_, padmin := env.user("padmin")
	env.grant("odabank", "padmin", domainauth.RepoRoleAdmin)

	projectRobot := decodeBody[map[string]any](t, env.do(http.MethodPost, "/admin/v1/robots", env.adminToken, `{"name":"pbot","project":"odabank","role":"repo-reader"}`))["robot"].(map[string]any)
	repoRobot := decodeBody[map[string]any](t, env.do(http.MethodPost, "/admin/v1/robots", env.adminToken, `{"name":"rbot","repository":"odabank/x","role":"repo-reader"}`))["robot"].(map[string]any)
	pid, rid := projectRobot["id"].(string), repoRobot["id"].(string)

	tests := []struct {
		name         string
		method, path string
		token        string
		body         string
		want         int
	}{
		{"project robot: other project", http.MethodPut, "/admin/v1/projects/odabank2/members/pbot", env.adminToken, `{"role":"repo-reader"}`, 409},
		{"project robot: same project, new role", http.MethodPut, "/admin/v1/projects/odabank/members/pbot", env.adminToken, `{"role":"repo-writer"}`, 200},
		{"project robot: admin role", http.MethodPut, "/admin/v1/projects/odabank/members/pbot", env.adminToken, `{"role":"repo-admin"}`, 422},
		{"project robot: repo grant by user id", http.MethodPut, "/admin/v1/users/" + pid + "/grants/odabank/x", env.adminToken, `{"role":"repo-reader"}`, 409},
		{"project robot: repo-admin grant by user id", http.MethodPut, "/admin/v1/users/" + pid + "/grants/odabank/x", env.adminToken, `{"role":"repo-admin"}`, 409},
		{"project robot: repo grant by admin route", http.MethodPut, "/admin/v1/repositories/odabank/x/grants/pbot", env.adminToken, `{"role":"repo-reader"}`, 409},
		{"project robot: repo-admin by admin route", http.MethodPut, "/admin/v1/repositories/odabank/x/grants/pbot", env.adminToken, `{"role":"repo-admin"}`, 409},
		{"project robot: repo grant by delegate", http.MethodPut, "/admin/v1/repositories/odabank/x/grants/pbot", padmin, `{"role":"repo-reader"}`, 409},
		{"repo robot: project grant", http.MethodPut, "/admin/v1/projects/odabank/members/rbot", env.adminToken, `{"role":"repo-reader"}`, 409},
		{"repo robot: project grant by delegate", http.MethodPut, "/admin/v1/projects/odabank/members/rbot", padmin, `{"role":"repo-reader"}`, 409},
		{"repo robot: more repo grants as before", http.MethodPut, "/admin/v1/users/" + rid + "/grants/odabank/y", env.adminToken, `{"role":"repo-reader"}`, 200},
		{"repo robot: delegate repo grant as before", http.MethodPut, "/admin/v1/repositories/odabank/z/grants/rbot", padmin, `{"role":"repo-writer"}`, 200},
	}
	for _, tt := range tests {
		if rec := env.do(tt.method, tt.path, tt.token, tt.body); rec.Code != tt.want {
			t.Errorf("%s: status = %d, want %d body=%s", tt.name, rec.Code, tt.want, rec.Body.String())
		}
	}

	// The project robot still shows its (updated) project grant, nothing else.
	robots := decodeBody[[]map[string]any](t, env.do(http.MethodGet, "/admin/v1/robots", env.adminToken, ""))
	for _, robot := range robots {
		if robot["username"] == "pbot" && (robot["project"] != "odabank" || robot["role"] != "repo-writer" || robot["repository"] != "") {
			t.Fatalf("project robot listing = %v", robot)
		}
	}
	if grants := decodeBody[[]map[string]any](t, env.do(http.MethodGet, "/admin/v1/users/"+pid+"/grants", env.adminToken, "")); len(grants) != 0 {
		t.Fatalf("the project robot got repository grants: %v", grants)
	}
}

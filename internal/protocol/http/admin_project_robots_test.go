package regixtryhttp

import (
	"net/http"
	"testing"
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

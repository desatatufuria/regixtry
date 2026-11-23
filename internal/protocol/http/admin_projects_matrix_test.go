package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

// TestAdminProjectRoutesAuthorizationMatrix drives EVERY new route as every
// kind of caller through the real router.
func TestAdminProjectRoutesAuthorizationMatrix(t *testing.T) {
	t.Parallel()

	type route struct {
		name           string
		method, path   string
		body           string
		statusByCaller map[string]int
	}
	// callers: none, limited (global admin with a repository scope), plain,
	// padmin (project admin of odabank), padmin2 (of odabank2), admin.
	routes := []route{
		{"list projects", http.MethodGet, "/admin/v1/projects", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 200}},
		{"create project", http.MethodPost, "/admin/v1/projects", `{"name":"fresh"}`, map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 201}},
		{"get project", http.MethodGet, "/admin/v1/projects/odabank", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 200}},
		{"delete project", http.MethodDelete, "/admin/v1/projects/spare", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 200}},
		{"list members", http.MethodGet, "/admin/v1/projects/odabank/members", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 200, "padmin2": 403, "admin": 200}},
		{"put reader", http.MethodPut, "/admin/v1/projects/odabank/members/dev", `{"role":"repo-reader"}`, map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 200, "padmin2": 403, "admin": 200}},
		{"put admin", http.MethodPut, "/admin/v1/projects/odabank/members/dev", `{"role":"repo-admin"}`, map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 200}},
		{"delete member", http.MethodDelete, "/admin/v1/projects/odabank/members/victim", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 204, "padmin2": 403, "admin": 204}},
		{"delete another admin", http.MethodDelete, "/admin/v1/projects/odabank/members/boss", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 204}},
		{"user project grants", http.MethodGet, "/admin/v1/users/{dev}/project-grants", "", map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 200}},
		{"robot with project", http.MethodPost, "/admin/v1/robots", `{"name":"bot","project":"odabank","role":"repo-writer"}`, map[string]int{"none": 401, "limited": 403, "plain": 403, "padmin": 403, "padmin2": 403, "admin": 201}},
	}

	for _, rt := range routes {
		for _, caller := range []string{"none", "limited", "plain", "padmin", "padmin2", "admin"} {
			t.Run(rt.name+"/"+caller, func(t *testing.T) {
				t.Parallel()
				env := newProjectsHTTP(t, true)
				env.createProject("odabank")
				env.createProject("odabank2")
				env.createProject("spare")
				dev, _ := env.user("dev")
				env.user("victim")
				env.user("boss")
				_, plain := env.user("plain")
				_, padmin := env.user("padmin")
				_, padmin2 := env.user("padmin2")
				env.grant("odabank", "victim", domainauth.RepoRoleReader)
				env.grant("odabank", "boss", domainauth.RepoRoleAdmin)
				env.grant("odabank", "padmin", domainauth.RepoRoleAdmin)
				env.grant("odabank2", "padmin2", domainauth.RepoRoleAdmin)
				limitedLogin, err := env.auth.LoginWithPassword(context.Background(), "admin", projectsTestPassword, mustHTTPScopes(t, "repository:odabank/x:pull"))
				if err != nil {
					t.Fatal(err)
				}
				// The grants above exist after the tokens were issued: the
				// principal is rebuilt on every request, so padmin tokens see them.
				tokens := map[string]string{"none": "", "limited": limitedLogin.BearerToken, "plain": plain, "padmin": padmin, "padmin2": padmin2, "admin": env.adminToken}

				path := strings.ReplaceAll(rt.path, "{dev}", dev.ID)
				rec := env.do(rt.method, path, tokens[caller], rt.body)
				if want := rt.statusByCaller[caller]; rec.Code != want {
					t.Fatalf("%s %s as %s: status = %d, want %d body=%s", rt.method, path, caller, rec.Code, want, rec.Body.String())
				}
				if caller == "none" && rec.Header().Get("WWW-Authenticate") == "" {
					t.Fatal("401 without a WWW-Authenticate challenge")
				}
			})
		}
	}
}

func mustHTTPScopes(t *testing.T, raw ...string) []domainauth.Scope {
	t.Helper()
	scopes, err := domainauth.ParseScopes(raw)
	if err != nil {
		t.Fatal(err)
	}
	return scopes
}

// TestAdminProjectsDoNotShadowExistingAdminRoutes pins that the new prefix does
// not capture routes it must not: a project named like another admin segment
// is addressed only under projects/, and the members suffix cannot be
// confused with a project called "members".
func TestAdminProjectsDoNotShadowExistingAdminRoutes(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("members")
	env.createProject("users")
	env.user("dev")
	env.grant("members", "dev", domainauth.RepoRoleReader)

	// The project literally named "members" has its own detail and members.
	if rec := env.do(http.MethodGet, "/admin/v1/projects/members", env.adminToken, ""); rec.Code != http.StatusOK || decodeBody[map[string]any](t, rec)["name"] != "members" {
		t.Fatalf("detail of project members: %d %s", rec.Code, rec.Body.String())
	}
	rec := env.do(http.MethodGet, "/admin/v1/projects/members/members", env.adminToken, "")
	page := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || len(page["members"].([]any)) != 1 {
		t.Fatalf("members of project members: %d %s", rec.Code, rec.Body.String())
	}
	// Existing neighbours are untouched.
	for _, path := range []string{"/admin/v1/users", "/admin/v1/robots", "/admin/v1/repository-grants", "/admin/v1/access-events", "/admin/v1/features"} {
		if rec := env.do(http.MethodGet, path, env.adminToken, ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
	// Unknown shapes stay 404, never an unauthenticated information leak.
	for _, path := range []string{"/admin/v1/project", "/admin/v1/projectsx", "/admin/v1/projects/a/b/c/d"} {
		if rec := env.do(http.MethodGet, path, env.adminToken, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, rec.Code)
		}
	}
	if rec := env.do(http.MethodGet, "/admin/v1/projects/anything/members", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous members status = %d, want 401", rec.Code)
	}
}

func TestAdminProjectLifecycleRecordsAccessEvents(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	env.createProject("odabank")
	env.user("ana")
	if rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/ana", env.adminToken, `{"role":"repo-reader"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if rec := env.do(http.MethodPut, "/admin/v1/projects/odabank/members/ana", env.adminToken, `{"role":"repo-writer"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank/members/ana", env.adminToken, ""); rec.Code != http.StatusNoContent {
		t.Fatal(rec.Body.String())
	}
	if rec := env.do(http.MethodDelete, "/admin/v1/projects/odabank", env.adminToken, ""); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	rec := env.do(http.MethodGet, "/admin/v1/access-events?limit=50", env.adminToken, "")
	events := decodeBody[[]map[string]any](t, rec)
	var got []string
	for i := len(events) - 1; i >= 0; i-- { // oldest first
		e := events[i]
		if repo, _ := e["repository"].(string); strings.HasPrefix(repo, "project:") {
			got = append(got, e["action"].(string)+"|"+repo+"|"+str(e["subject"])+"|"+str(e["role"])+"|"+str(e["detail"])+"|"+str(e["actor"]))
		}
	}
	want := []string{
		"project.created|project:odabank||||admin",
		"project.grant.put|project:odabank|ana|repo-reader||admin",
		"project.grant.put|project:odabank|ana|repo-writer|Reader to Writer|admin",
		"project.grant.delete|project:odabank|ana|||admin",
		"project.deleted|project:odabank||||admin",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func TestAdminProjectRoutesAreRaceFree(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)
	ana, _ := env.user("ana")
	env.user("bob")
	done := make(chan struct{})
	end := time.Now().Add(1500 * time.Millisecond)
	ok := map[int]bool{200: true, 201: true, 204: true, 404: true, 409: true}
	run := func(f func() *httptest.ResponseRecorder) {
		go func() {
			defer func() { done <- struct{}{} }()
			for time.Now().Before(end) {
				if rec := f(); !ok[rec.Code] {
					t.Errorf("unexpected status %d body=%s", rec.Code, rec.Body.String())
					return
				}
			}
		}()
	}
	run(func() *httptest.ResponseRecorder {
		return env.do(http.MethodPost, "/admin/v1/projects", env.adminToken, `{"name":"race"}`)
	})
	run(func() *httptest.ResponseRecorder {
		return env.do(http.MethodDelete, "/admin/v1/projects/race", env.adminToken, "")
	})
	run(func() *httptest.ResponseRecorder {
		return env.do(http.MethodPut, "/admin/v1/projects/race/members/ana", env.adminToken, `{"role":"repo-writer"}`)
	})
	run(func() *httptest.ResponseRecorder {
		return env.do(http.MethodDelete, "/admin/v1/projects/race/members/bob", env.adminToken, "")
	})
	run(func() *httptest.ResponseRecorder {
		return env.do(http.MethodGet, "/admin/v1/projects/race/members", env.adminToken, "")
	})
	for i := 0; i < 5; i++ {
		<-done
	}

	// Converge: whatever happened, deleting leaves no reachable grant.
	env.do(http.MethodDelete, "/admin/v1/projects/race", env.adminToken, "")
	env.do(http.MethodDelete, "/admin/v1/projects/race", env.adminToken, "")
	grants, err := env.auth.ListAdminUserProjectGrants(context.Background(), env.admin, ana.ID)
	if err != nil || len(grants) != 0 {
		t.Fatalf("grants after the converging delete = %#v, %v", grants, err)
	}
	if rec := env.do(http.MethodGet, "/admin/v1/projects/race", env.adminToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("project survived: %d", rec.Code)
	}
}

func TestAdminProjectMemberRoutesRejectWrongMethods(t *testing.T) {
	t.Parallel()
	env := newProjectsHTTP(t, true)

	for _, tc := range []struct{ method, path, allow string }{
		{http.MethodPost, "/admin/v1/projects/odabank/members", "GET"},
		{http.MethodGet, "/admin/v1/projects/odabank/members/ana", "PUT, DELETE"},
		{http.MethodPost, "/admin/v1/users/some-id/project-grants", "GET"},
	} {
		rec := env.do(tc.method, tc.path, env.adminToken, "")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != tc.allow {
			t.Errorf("%s %s status = %d allow=%q, want 405 %q", tc.method, tc.path, rec.Code, rec.Header().Get("Allow"), tc.allow)
		}
	}
}

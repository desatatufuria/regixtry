package tui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func projectsClientAt(t *testing.T, handler http.HandlerFunc) (*HTTPAdminClient, AdminSession) {
	t.Helper()
	fixedNow := time.Date(2026, time.November, 21, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return fixedNow }
	return client, AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}
}

func writeAPIJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func TestHTTPAdminClientProjectReads(t *testing.T) {
	t.Parallel()
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bearer-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /admin/v1/projects":
			writeAPIJSON(w, 200, `{"projects":[{"name":"odabank","created_at":"2026-11-02T08:00:00Z","created_by":"admin","repositories":3,"tags":27,"last_pushed":"2026-11-19T08:00:00Z"},{"name":"sandbox","created_at":"2026-11-15T08:00:00Z","created_by":"admin","repositories":0,"tags":0,"last_pushed":null}]}`)
		case "GET /admin/v1/projects/odabank":
			writeAPIJSON(w, 200, `{"name":"odabank","created_at":"2026-11-02T08:00:00Z","created_by":"admin","repositories":3,"tags":27,"last_pushed":null}`)
		case "GET /admin/v1/projects/odabank/members":
			if r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("after") != "c+1/=" {
				t.Errorf("members query = %q, want limit clamped to 200 and the cursor unchanged", r.URL.RawQuery)
			}
			writeAPIJSON(w, 200, `{"members":[{"username":"carol","is_robot":false,"role":"repo-writer","created_at":"2026-11-10T08:00:00Z","updated_at":"2026-11-10T08:00:00Z"}],"next":"n2"}`)
		case "GET /admin/v1/users/u-1/project-grants":
			writeAPIJSON(w, 200, `[{"project":"odabank","role":"repo-reader","created_at":"2026-11-10T08:00:00Z","updated_at":"2026-11-10T08:00:00Z"}]`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
	ctx := context.Background()

	projects, err := client.ListProjects(ctx, session)
	if err != nil || len(projects) != 2 {
		t.Fatalf("ListProjects() = %v, %v", projects, err)
	}
	if p := projects[0]; p.Name != "odabank" || p.Repositories != 3 || p.Tags != 27 || p.LastPushed.IsZero() || p.CreatedBy != "admin" {
		t.Fatalf("project = %#v", p)
	}
	if !projects[1].LastPushed.IsZero() {
		t.Fatalf("a null last_pushed must decode to the zero time, got %v", projects[1].LastPushed)
	}
	if p, err := client.GetProject(ctx, session, "odabank"); err != nil || p.Name != "odabank" {
		t.Fatalf("GetProject() = %#v, %v", p, err)
	}
	page, err := client.ListProjectMembers(ctx, session, "odabank", 5000, "c+1/=")
	if err != nil || page.Next != "n2" || len(page.Members) != 1 || page.Members[0].Role != domainauth.RepoRoleWriter || page.Members[0].Username != "carol" {
		t.Fatalf("ListProjectMembers() = %#v, %v", page, err)
	}
	grants, err := client.ListUserProjectGrants(ctx, session, "u-1")
	if err != nil || len(grants) != 1 || grants[0].Project != "odabank" || grants[0].Role != domainauth.RepoRoleReader {
		t.Fatalf("ListUserProjectGrants() = %#v, %v", grants, err)
	}
}

func TestHTTPAdminClientProjectWrites(t *testing.T) {
	t.Parallel()
	var seen []string
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+" "+string(body))
		switch r.Method {
		case http.MethodPost:
			writeAPIJSON(w, 201, `{"name":"odabank","created_at":"2026-11-02T08:00:00Z","created_by":"admin","repositories":0,"tags":0,"last_pushed":null}`)
		case http.MethodDelete:
			if strings.Contains(r.URL.Path, "/members/") {
				w.WriteHeader(204)
				return
			}
			writeAPIJSON(w, 200, `{"deleted":"odabank"}`)
		case http.MethodPut:
			writeAPIJSON(w, 200, `{"username":"carol","is_robot":false,"role":"repo-writer","created_at":"2026-11-10T08:00:00Z","updated_at":"2026-11-10T08:00:00Z"}`)
		}
	})
	ctx := context.Background()
	if p, err := client.CreateProject(ctx, session, "odabank"); err != nil || p.Name != "odabank" {
		t.Fatalf("CreateProject() = %#v, %v", p, err)
	}
	if err := client.DeleteProject(ctx, session, "odabank"); err != nil {
		t.Fatalf("DeleteProject() = %v", err)
	}
	member, err := client.PutProjectMember(ctx, session, ports.AdminPutProjectGrantInput{Project: "odabank", Username: "carol", Role: domainauth.RepoRoleWriter})
	if err != nil || member.Username != "carol" {
		t.Fatalf("PutProjectMember() = %#v, %v", member, err)
	}
	if err := client.DeleteProjectMember(ctx, session, "odabank", "carol"); err != nil {
		t.Fatalf("DeleteProjectMember() = %v", err)
	}
	want := []string{
		`POST /admin/v1/projects {"name":"odabank"}`,
		`DELETE /admin/v1/projects/odabank `,
		`PUT /admin/v1/projects/odabank/members/carol {"role":"repo-writer"}`,
		`DELETE /admin/v1/projects/odabank/members/carol `,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests =\n%s\nwant\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestHTTPAdminClientProjectErrorsAreTyped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status  int
		message string
		is      func(error) bool
	}{
		{404, "project not found", IsAdminNotFound},
		{409, `project "odabank" still has 3 repositories; delete or finish them first`, IsAdminConflict},
		{422, "project deletion is not enabled", IsAdminUnprocessable},
		{403, "forbidden", IsAdminForbidden},
		{503, `project "x" was deleted but its grants could not be purged; retry the delete`, IsAdminUnavailable},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			client, session := projectsClientAt(t, func(w http.ResponseWriter, _ *http.Request) {
				writeAPIJSON(w, tc.status, `{"error":`+quoteJSON(tc.message)+`}`)
			})
			err := client.DeleteProject(context.Background(), session, "odabank")
			if err == nil || !tc.is(err) {
				t.Fatalf("error = %v, want the typed %d error", err, tc.status)
			}
			if got := adminErrorDetail(err); got != tc.message {
				t.Fatalf("detail = %q, want the server's bare message %q", got, tc.message)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Error() = %q lost the message", err.Error())
			}
			for _, other := range []func(error) bool{IsAdminNotFound, IsAdminConflict, IsAdminUnprocessable, IsAdminForbidden, IsAdminUnavailable} {
				if other(err) && !tc.is(err) {
					t.Fatalf("error matched a second status: %v", err)
				}
			}
		})
	}
}

func quoteJSON(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func TestHTTPAdminClientProjectExpiryMatchesTheOtherCalls(t *testing.T) {
	t.Parallel()
	expired := AdminSession{Username: "operator", BearerToken: "t", ExpiresAt: time.Date(2026, time.November, 21, 9, 0, 0, 0, time.UTC)}
	client, _ := projectsClientAt(t, func(http.ResponseWriter, *http.Request) { t.Error("an expired session must not reach the server") })
	if _, err := client.ListProjects(context.Background(), expired); !IsAdminSessionExpired(err) {
		t.Fatalf("expired session: error = %v, want session expired", err)
	}

	rejected, session := projectsClientAt(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	if err := rejected.DeleteProjectMember(context.Background(), session, "odabank", "carol"); !IsAdminSessionExpired(err) {
		t.Fatalf("rejected token: error = %v, want session expired", err)
	}
}

func TestHTTPAdminClientCreateRobotSendsProjectInsteadOfRepository(t *testing.T) {
	t.Parallel()
	var bodies []string
	client, session := projectsClientAt(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		writeAPIJSON(w, 201, `{"robot":{"id":"r1","username":"bot","project":"odabank","role":"repo-writer"},"secret":"s","accessor":"a"}`)
	})
	ctx := context.Background()
	created, err := client.CreateRobot(ctx, session, ports.AdminCreateRobotInput{Name: "bot", Project: "odabank", Role: domainauth.RepoRoleWriter})
	if err != nil || created.Robot.Project != "odabank" || created.Robot.Repository != "" {
		t.Fatalf("project robot = %#v, %v", created, err)
	}
	if _, err := client.CreateRobot(ctx, session, ports.AdminCreateRobotInput{Name: "bot", Repository: "team/api", Role: domainauth.RepoRoleReader}); err != nil {
		t.Fatal(err)
	}
	want := []string{`{"name":"bot","project":"odabank","role":"repo-writer"}`, `{"name":"bot","repository":"team/api","role":"repo-reader"}`}
	if strings.Join(bodies, "\n") != strings.Join(want, "\n") {
		t.Fatalf("bodies =\n%s\nwant\n%s", strings.Join(bodies, "\n"), strings.Join(want, "\n"))
	}
}

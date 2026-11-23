package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const capRepo = "odabank/payments"

// capVerbs are the decisions the matrix observes, in the order they run
// (destructive ones last).
var capVerbs = []string{"pull", "push", "catalog", "grants list", "grants put", "grants delete", "members list", "members put", "robot create", "delete manifest", "delete repository"}

type capSubject struct {
	name string
	opts func(*ports.AdminCreateUserInput)
	// kind: "admin", "robot", "grant:<source>:<role>", "scoped:<role>" (registry token used on every route)
	kind string
	want map[string]bool
}

func storeExactGrant(t *testing.T, e *projectsHTTP, user string, role domainauth.RepoRole) {
	t.Helper()
	now := time.Now().UTC()
	if err := e.store.PutRepoGrant(context.Background(), domainauth.RepoGrant{UserID: "id-" + user, Repository: regixtrydomain.MustParseRepositoryRef(capRepo), Role: role, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func storeProjectGrant(t *testing.T, e *projectsHTTP, user string, role domainauth.RepoRole) {
	t.Helper()
	now := time.Now().UTC()
	if err := e.store.PutProjectGrant(context.Background(), domainauth.ProjectGrant{UserID: "id-" + user, Project: "odabank", Role: role, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

// capExchange runs the docker-style token exchange (basic auth, one scope
// parameter per scope) and returns the bearer token.
func capExchange(t *testing.T, e *projectsHTTP, user, password string, scopes ...string) string {
	t.Helper()
	query := url.Values{"service": {"regixtry"}, "scope": scopes}
	req := httptest.NewRequest(http.MethodGet, "/auth/token?"+query.Encode(), nil)
	req.SetBasicAuth(user, password)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token exchange for %s status = %d body=%s", user, rec.Code, rec.Body.String())
	}
	token, _ := decodeBody[map[string]any](t, rec)["token"].(string)
	return token
}

func allowed(code int) bool { return code != http.StatusUnauthorized && code != http.StatusForbidden }

// capDecisions runs every verb as one caller through the real router.
func capDecisions(t *testing.T, e *projectsHTTP, user, adminBearer, registryBearer string) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	repoPath := "/admin/v1/repositories/" + capRepo
	got["pull"] = allowed(e.do(http.MethodGet, "/v2/"+capRepo+"/manifests/latest", registryBearer, "").Code)
	got["push"] = allowed(e.do(http.MethodPost, "/v2/"+capRepo+"/blobs/uploads/", registryBearer, "").Code)
	catalog := e.do(http.MethodGet, "/v2/_catalog", registryBearer, "")
	got["catalog"] = catalog.Code == http.StatusOK && strings.Contains(catalog.Body.String(), capRepo)
	got["grants list"] = allowed(e.do(http.MethodGet, repoPath+"/grants", adminBearer, "").Code)
	got["grants put"] = allowed(e.do(http.MethodPut, repoPath+"/grants/victim", adminBearer, `{"role":"repo-reader"}`).Code)
	got["grants delete"] = allowed(e.do(http.MethodDelete, repoPath+"/grants/victim", adminBearer, "").Code)
	got["members list"] = allowed(e.do(http.MethodGet, "/admin/v1/projects/odabank/members", adminBearer, "").Code)
	got["members put"] = allowed(e.do(http.MethodPut, "/admin/v1/projects/odabank/members/victim", adminBearer, `{"role":"repo-reader"}`).Code)
	got["robot create"] = allowed(e.do(http.MethodPost, "/admin/v1/robots", adminBearer, `{"name":"bot-`+user+`","project":"odabank","role":"repo-reader"}`).Code)
	got["delete manifest"] = allowed(e.do(http.MethodDelete, "/v2/"+capRepo+"/manifests/latest", registryBearer, "").Code)
	got["delete repository"] = allowed(e.do(http.MethodDelete, repoPath, adminBearer, "").Code)
	return got
}

// TestReadOnlyUsersGetAtMostReadFromAnyGrant pins S1 end to end: the same
// request set as every kind of principal, through the real router and the
// real token exchange. Read-only users hold exact/project grants of every
// role (rows written straight to the store, as a legacy database would).
func TestReadOnlyUsersGetAtMostReadFromAnyGrant(t *testing.T) {
	t.Parallel()

	readOnly := func(in *ports.AdminCreateUserInput) { in.IsReadOnly = true }
	roles := []struct {
		role domainauth.RepoRole
		name string
	}{{domainauth.RepoRoleReader, "reader"}, {domainauth.RepoRoleWriter, "writer"}, {domainauth.RepoRoleAdmin, "admin"}}

	// want builds the expected decisions of a NON-read-only user holding role
	// through source ("exact" or "project").
	want := func(role domainauth.RepoRole, source string) map[string]bool {
		w := map[string]bool{"pull": role.AllowsRead(), "push": role.AllowsWrite(), "catalog": role.AllowsRead(),
			"delete manifest": role.AllowsWrite(), "delete repository": role.AllowsAdmin(),
			"grants list": role.AllowsAdmin(), "grants put": role.AllowsAdmin(), "grants delete": role.AllowsAdmin(),
			"members list": role.AllowsAdmin() && source == "project", "members put": role.AllowsAdmin() && source == "project", "robot create": false}
		return w
	}
	readOnlyWant := map[string]bool{"pull": true, "catalog": true}

	subjects := []capSubject{{name: "global admin", kind: "admin", want: map[string]bool{
		"pull": true, "push": true, "catalog": true, "grants list": true, "grants put": true, "grants delete": true,
		"members list": true, "members put": true, "robot create": true, "delete manifest": true, "delete repository": true}}}
	for _, r := range roles {
		for _, source := range []string{"exact", "project"} {
			subjects = append(subjects,
				capSubject{name: "read-only/" + source + "/" + r.name, opts: readOnly, want: readOnlyWant, kind: "grant:" + source + ":" + string(r.role)},
				capSubject{name: "user/" + source + "/" + r.name, want: want(r.role, source), kind: "grant:" + source + ":" + string(r.role)},
			)
		}
	}
	subjects = append(subjects,
		// A project admin holding a registry-scoped token (the only kind that
		// can reach /v2 verbs) is not an administrator on the admin API.
		capSubject{name: "scope-limited token", kind: "scoped:" + string(domainauth.RepoRoleAdmin), want: map[string]bool{"pull": true, "push": true, "catalog": true, "delete manifest": true}},
		capSubject{name: "robot writer", kind: "robot", want: map[string]bool{"pull": true, "push": true, "catalog": true, "delete manifest": true}},
	)

	for _, subject := range subjects {
		t.Run(subject.name, func(t *testing.T) {
			t.Parallel()
			e := newProjectsHTTP(t, true)
			e.createProject("odabank")
			e.push("admin", capRepo)
			e.user("victim")
			if _, err := e.auth.PutProjectGrant(context.Background(), e.admin, "odabank", "victim", domainauth.RepoRoleReader); err != nil {
				t.Fatal(err)
			}

			var adminBearer, registryBearer string
			user := "subject"
			switch {
			case subject.kind == "admin":
				user = "admin"
				adminBearer = e.adminToken
				registryBearer = capExchange(t, e, "admin", projectsTestPassword, "repository:"+capRepo+":pull,push,delete", "regixtry:catalog:*")
			case subject.kind == "robot":
				created, err := e.auth.CreateAdminRobot(context.Background(), e.admin, ports.AdminCreateRobotInput{Name: "writer-bot", Repository: capRepo, Role: domainauth.RepoRoleWriter, TTL: time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				user = "writer-bot"
				adminBearer = created.Secret
				registryBearer = capExchange(t, e, user, created.Secret, "repository:"+capRepo+":pull,push,delete", "regixtry:catalog:*")
			default:
				var mutate []func(*ports.AdminCreateUserInput)
				if subject.opts != nil {
					mutate = append(mutate, subject.opts)
				}
				_, adminBearer = e.user(user, mutate...)
				parts := strings.Split(subject.kind, ":")
				if len(parts) == 3 && parts[0] == "grant" {
					role := domainauth.RepoRole(parts[2])
					if parts[1] == "exact" {
						storeExactGrant(t, e, user, role)
					} else {
						storeProjectGrant(t, e, user, role)
					}
				}
				if len(parts) == 2 && parts[0] == "scoped" {
					storeProjectGrant(t, e, user, domainauth.RepoRole(parts[1]))
				}
				registryBearer = capExchange(t, e, user, projectsTestPassword, "repository:"+capRepo+":pull,push,delete", "regixtry:catalog:*")
				if strings.HasPrefix(subject.kind, "scoped:") {
					adminBearer = registryBearer
				}
			}

			got := capDecisions(t, e, user, adminBearer, registryBearer)
			for _, verb := range capVerbs {
				if got[verb] != subject.want[verb] {
					t.Errorf("%s: %s allowed = %t, want %t", subject.name, verb, got[verb], subject.want[verb])
				}
			}
		})
	}
}

// TestReadOnlyUserTokenScopesContainPullOnly pins that token issue never
// puts push or delete in the scopes of a read-only user, even when legacy
// admin grants exist.
func TestReadOnlyUserTokenScopesContainPullOnly(t *testing.T) {
	t.Parallel()

	e := newProjectsHTTP(t, true)
	e.createProject("odabank")
	e.user("viewer", func(in *ports.AdminCreateUserInput) { in.IsReadOnly = true })
	storeExactGrant(t, e, "viewer", domainauth.RepoRoleAdmin)
	storeProjectGrant(t, e, "viewer", domainauth.RepoRoleAdmin)

	for _, repository := range []string{capRepo, "odabank/other", "elsewhere/x"} {
		_, granted := e.registryToken("viewer", "repository:"+repository+":pull,push,delete")
		if want := "repository:" + repository + ":pull"; granted != want {
			t.Errorf("granted scope for %s = %q, want %q", repository, granted, want)
		}
	}
}

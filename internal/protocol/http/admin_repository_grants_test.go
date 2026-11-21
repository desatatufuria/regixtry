package regixtryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

type grantsBody struct {
	Grants []map[string]any `json:"grants"`
	Next   string           `json:"next"`
}

func TestRouterAdminRepositoryGrants(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	ana, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ana", Password: "password123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	off, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "off", Password: "password123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct {
		id, repo string
		role     domainauth.RepoRole
	}{
		{ana.ID, "team/web", domainauth.RepoRoleWriter},
		{ana.ID, "alpha/api", domainauth.RepoRoleAdmin},
		{off.ID, "never/pushed", domainauth.RepoRoleReader},
	} {
		if _, err := authService.PutAdminUserRepoGrant(ctx, adminActor, ports.AdminPutRepoGrantInput{UserID: g.id, Repository: g.repo, Role: g.role}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := authService.SetUserEnabled(ctx, adminActor, off.ID, false); err != nil {
		t.Fatal(err)
	}
	robot, err := authService.CreateAdminRobot(ctx, adminActor, ports.AdminCreateRobotInput{Name: "ci-bot", Repository: "team/web", Role: domainauth.RepoRoleReader, TTL: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	get := func(query, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/admin/v1/repository-grants"+query, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	page := func(t *testing.T, rec *httptest.ResponseRecorder) grantsBody {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var out grantsBody
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
		return out
	}
	keys := func(p grantsBody) string {
		out := []string{}
		for _, g := range p.Grants {
			out = append(out, g["repository"].(string)+"/"+g["username"].(string))
		}
		return strings.Join(out, ",")
	}
	const fullOrder = "alpha/api/ana,never/pushed/off,team/web/ana,team/web/ci-bot"

	t.Run("shape, order and content type", func(t *testing.T) {
		rec := get("", adminToken)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q", ct)
		}
		got := page(t, rec)
		if keys(got) != fullOrder || got.Next != "" {
			t.Fatalf("grants = %s next=%q body=%s", keys(got), got.Next, rec.Body.String())
		}
		for _, row := range got.Grants {
			for _, key := range []string{"repository", "user_id", "username", "is_robot", "role", "created_at", "updated_at"} {
				if _, ok := row[key]; !ok {
					t.Errorf("missing %q in %v", key, row)
				}
			}
			if len(row) != 7 {
				t.Errorf("unexpected fields in %v", row)
			}
		}
		if got.Grants[1]["role"] != "repo-reader" || got.Grants[1]["is_robot"] != false || got.Grants[3]["is_robot"] != true || got.Grants[3]["user_id"] != robot.Robot.ID {
			t.Errorf("rows = %v", got.Grants)
		}
	})

	t.Run("raw bytes contain no secret hash or token", func(t *testing.T) {
		body := strings.ToLower(get("?limit=200", adminToken).Body.String())
		for _, banned := range []string{"secret", "hash", "plaintext", "password", "accessor", "token", "enabled", robot.Secret, robot.Accessor} {
			if banned != "" && strings.Contains(body, strings.ToLower(banned)) {
				t.Errorf("body contains %q: %s", banned, body)
			}
		}
	})

	t.Run("pages reassemble to the full list", func(t *testing.T) {
		for _, size := range []string{"1", "2", "3", "4", "200"} {
			var all []string
			after := ""
			for i := 0; i < 10; i++ {
				q := "?limit=" + size
				if after != "" {
					q += "&after=" + url.QueryEscape(after)
				}
				p := page(t, get(q, adminToken))
				if k := keys(p); k != "" {
					all = append(all, k)
				}
				if p.Next == "" {
					break
				}
				after = p.Next
			}
			if strings.Join(all, ",") != fullOrder {
				t.Errorf("limit %s reassembled %v", size, all)
			}
		}
		p := page(t, get("?limit=1", adminToken))
		if len(p.Grants) != 1 || p.Next == "" {
			t.Fatalf("limit=1 = %#v", p)
		}
	})

	t.Run("limit boundaries", func(t *testing.T) {
		for _, q := range []string{"?limit=1", "?limit=100", "?limit=200"} {
			if rec := get(q, adminToken); rec.Code != http.StatusOK {
				t.Errorf("%s status = %d", q, rec.Code)
			}
		}
		for _, q := range []string{"?limit=0", "?limit=201", "?limit=0001", "?limit=abc", "?limit=", "?limit=-1", "?limit=+5", "?limit=1.5", "?limit=1000"} {
			rec := get(q, adminToken)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "limit must be a whole number between 1 and 200") {
				t.Errorf("%s status = %d body=%s", q, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("malformed cursors are 422", func(t *testing.T) {
		valid := page(t, get("?limit=1", adminToken)).Next
		for _, after := range []string{"garbage!!", "anVuaw", "W10", valid[:len(valid)-2], valid + "=", valid + "AAAA", strings.Repeat("A", 100000), "%00", "e30"} {
			rec := get("?after="+url.QueryEscape(after), adminToken)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "after is not a valid cursor") {
				t.Errorf("after=%.20q status = %d body=%.200s", after, rec.Code, rec.Body.String())
			}
		}
		if rec := get("?after=", adminToken); rec.Code != http.StatusOK {
			t.Errorf("empty after status = %d, want the first page", rec.Code)
		}
	})

	t.Run("401 without or with a bad token", func(t *testing.T) {
		if rec := get("", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("no token status = %d", rec.Code)
		}
		if rec := get("", "garbage"); rec.Code != http.StatusUnauthorized {
			t.Errorf("garbage status = %d", rec.Code)
		}
	})

	t.Run("403 for a non-admin, a robot token and a repo-admin delegate", func(t *testing.T) {
		if _, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "plain", Password: "password123", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		plain, err := authService.LoginWithPassword(ctx, "plain", "password123", nil)
		if err != nil {
			t.Fatal(err)
		}
		if rec := get("", plain.BearerToken); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin status = %d body=%s", rec.Code, rec.Body.String())
		}
		robotLogin, err := authService.LoginWithPreissuedToken(ctx, "ci-bot", robot.Secret, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rec := get("", robotLogin.BearerToken); rec.Code != http.StatusForbidden {
			t.Errorf("robot status = %d", rec.Code)
		}
		delegateLogin, err := authService.LoginWithPassword(ctx, "ana", "password123", nil)
		if err != nil {
			t.Fatal(err)
		}
		if rec := get("", delegateLogin.BearerToken); rec.Code != http.StatusForbidden {
			t.Errorf("repo-admin delegate status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("other methods are 405 with Allow GET", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			req := httptest.NewRequest(method, "/admin/v1/repository-grants", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
				t.Errorf("%s status = %d Allow = %q", method, rec.Code, rec.Header().Get("Allow"))
			}
		}
	})
}

func TestRouterAdminRepositoryGrantsEmptyIsArray(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/repository-grants", nil)
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"grants":[],"next":""}` {
		t.Fatalf("status=%d body=%q, want 200 with an empty grants array", rec.Code, rec.Body.String())
	}
}

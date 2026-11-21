package regixtryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestRouterAdminAccessEvents(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	ana, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ana", Password: "password123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.PutAdminUserRepoGrant(ctx, adminActor, ports.AdminPutRepoGrantInput{UserID: ana.ID, Repository: "team/app", Role: domainauth.RepoRoleReader}); err != nil {
		t.Fatal(err)
	}
	if _, err := authService.PutAdminUserRepoGrant(ctx, adminActor, ports.AdminPutRepoGrantInput{UserID: ana.ID, Repository: "team/app", Role: domainauth.RepoRoleWriter}); err != nil {
		t.Fatal(err)
	}
	token, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: ana.ID, Name: "deploy", TTL: 12 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	robot, err := authService.CreateAdminRobot(ctx, adminActor, ports.AdminCreateRobotInput{Name: "ci-bot", Repository: "team/app", Role: domainauth.RepoRoleReader, TTL: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	get := func(query, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/admin/v1/access-events"+query, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	rows := func(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
		return out
	}

	t.Run("shape newest first and content type", func(t *testing.T) {
		rec := get("", adminToken)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q", ct)
		}
		got := rows(t, rec)
		if len(got) != 5 {
			t.Fatalf("rows = %d, want 5: %s", len(got), rec.Body.String())
		}
		actions := []string{}
		for _, row := range got {
			actions = append(actions, row["action"].(string))
		}
		if strings.Join(actions, ",") != "robot.created,token.issued,grant.put,grant.put,user.created" {
			t.Fatalf("actions = %v", actions)
		}
		if got[0]["subject"] != "ci-bot" || got[0]["repository"] != "team/app" || got[0]["role"] != "repo-reader" || got[0]["detail"] != "1 day" || got[0]["actor"] != "admin" {
			t.Errorf("robot row = %v", got[0])
		}
		if got[1]["token_name"] != "deploy" || got[1]["detail"] != "12 hours" || got[1]["subject"] != "ana" {
			t.Errorf("token row = %v", got[1])
		}
		if got[2]["detail"] != "Reader to Writer" {
			t.Errorf("changed grant row = %v", got[2])
		}
		if _, present := got[3]["detail"]; present {
			t.Errorf("a new grant has no detail, row = %v", got[3])
		}
		if got[4]["subject"] != "ana" || got[4]["detail"] != "standard user" {
			t.Errorf("user row = %v", got[4])
		}
		for _, row := range got {
			for _, key := range []string{"id", "at", "action", "actor"} {
				if _, ok := row[key]; !ok {
					t.Errorf("missing %q in %v", key, row)
				}
			}
			for key := range row {
				switch key {
				case "id", "at", "action", "actor", "subject", "repository", "role", "token_name", "detail":
				default:
					t.Errorf("unexpected field %q in %v", key, row)
				}
			}
		}
	})

	t.Run("raw bytes contain no secret hash or accessor", func(t *testing.T) {
		body := get("?limit=200", adminToken).Body.String()
		lower := strings.ToLower(body)
		for _, banned := range []string{"secret", "hash", "plaintext", "password", "accessor", "user_id", ana.ID, token.Secret, token.Accessor, robot.Secret, robot.Accessor, robot.Robot.ID} {
			if banned != "" && strings.Contains(lower, strings.ToLower(banned)) {
				t.Errorf("body contains %q: %s", banned, body)
			}
		}
	})

	t.Run("limit defaults to 20 and honours boundaries", func(t *testing.T) {
		if n := len(rows(t, get("?limit=1", adminToken))); n != 1 {
			t.Errorf("limit=1 rows = %d", n)
		}
		if n := len(rows(t, get("?limit=200", adminToken))); n != 5 {
			t.Errorf("limit=200 rows = %d", n)
		}
		if n := len(rows(t, get("?limit=2", adminToken))); n != 2 {
			t.Errorf("limit=2 rows = %d", n)
		}
	})

	t.Run("invalid limits are 422 with the message", func(t *testing.T) {
		for _, q := range []string{"?limit=0", "?limit=201", "?limit=0001", "?limit=abc", "?limit=", "?limit=-1", "?limit=+5", "?limit=%201", "?limit=1.5", "?limit=1000"} {
			rec := get(q, adminToken)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s status = %d body=%s", q, rec.Code, rec.Body.String())
				continue
			}
			if !strings.Contains(rec.Body.String(), "limit must be a whole number between 1 and 200") {
				t.Errorf("%s message = %s", q, rec.Body.String())
			}
		}
	})

	t.Run("401 without a token", func(t *testing.T) {
		if rec := get("", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d", rec.Code)
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
			t.Errorf("robot status = %d body=%s", rec.Code, rec.Body.String())
		}

		delegate, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "dele", Password: "password123", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authService.PutAdminUserRepoGrant(ctx, adminActor, ports.AdminPutRepoGrantInput{UserID: delegate.ID, Repository: "team/app", Role: domainauth.RepoRoleAdmin}); err != nil {
			t.Fatal(err)
		}
		delegateLogin, err := authService.LoginWithPassword(ctx, "dele", "password123", nil)
		if err != nil {
			t.Fatal(err)
		}
		if rec := get("", delegateLogin.BearerToken); rec.Code != http.StatusForbidden {
			t.Errorf("delegate status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("other methods are 405 with Allow GET", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			req := httptest.NewRequest(method, "/admin/v1/access-events", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
				t.Errorf("%s status = %d Allow = %q", method, rec.Code, rec.Header().Get("Allow"))
			}
		}
	})

	t.Run("no accidental sibling routes", func(t *testing.T) {
		for _, path := range []string{"/admin/v1/access-events/x", "/admin/v1/access-event"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s status = %d, want 404", path, rec.Code)
			}
		}
	})
}

func TestRouterAdminAccessEventsEmptyIsArray(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/access-events", nil)
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%q, want 200 []", rec.Code, rec.Body.String())
	}
}

func TestRouterAdminAccessEventsStaySmallWithGiantNames(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	ana, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ana", Password: "password123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	giant := strings.Repeat("日", 100_000)
	for i := 0; i < 200; i++ {
		if _, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: ana.ID, Name: giant, TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/access-events?limit=200", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if size := rec.Body.Len(); size > 400*1024 {
		t.Fatalf("response is %d bytes, want under 400 KB", size)
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || len(rows) != 200 {
		t.Fatalf("rows = %d, err = %v", len(rows), err)
	}
}

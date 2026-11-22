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

func TestRouterAdminExpiringTokens(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	owner, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "exp-owner", Password: "fixture-pass-9x", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, ttl time.Duration) ports.AdminCreatedToken {
		created, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: owner.ID, Name: name, TTL: ttl})
		if err != nil {
			t.Fatalf("CreateAdminUserToken(%s) error = %v", name, err)
		}
		return created
	}
	d := 24 * time.Hour
	tok1h := mk("in-1h", time.Hour)
	mk("in-13d", 13*d)
	mk("in-20d", 20*d)
	mk("in-400d", 400*d)

	get := func(query, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/admin/v1/admin-tokens/expiring"+query, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	// names returns only the tokens created by this test (the bootstrap
	// admin's own login token may also be in the window).
	names := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode: %v body=%s", err, rec.Body.String())
		}
		out := []string{}
		for _, row := range rows {
			if n, _ := row["name"].(string); strings.HasPrefix(n, "in-") {
				out = append(out, n)
			}
		}
		return strings.Join(out, ",")
	}

	t.Run("shape, content type and no secrets", func(t *testing.T) {
		rec := get("?within=14d", adminToken)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q", ct)
		}
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil || len(rows) == 0 {
			t.Fatalf("rows=%v err=%v body=%s", rows, err, rec.Body.String())
		}
		for _, k := range []string{"id", "accessor", "name", "user_id", "username", "is_robot", "created_at", "expires_at"} {
			if _, ok := rows[0][k]; !ok {
				t.Errorf("missing field %q in %v", k, rows[0])
			}
		}
		body := strings.ToLower(rec.Body.String())
		for _, banned := range []string{"secret", "hash", "plaintext", "password", strings.ToLower(tok1h.Secret)} {
			if banned != "" && strings.Contains(body, banned) {
				t.Errorf("body contains %q: %s", banned, rec.Body.String())
			}
		}
		if got := names(t, rec); got != "in-1h,in-13d" {
			t.Errorf("within=14d names = %s", got)
		}
	})
	t.Run("default within is 30d", func(t *testing.T) {
		if got := names(t, get("", adminToken)); got != "in-1h,in-13d,in-20d" {
			t.Fatalf("default names = %s", got)
		}
	})
	t.Run("within 12h", func(t *testing.T) {
		if got := names(t, get("?within=12h", adminToken)); got != "in-1h" {
			t.Fatalf("names = %s", got)
		}
	})
	t.Run("within 2y includes all", func(t *testing.T) {
		if got := names(t, get("?within=2y", adminToken)); got != "in-1h,in-13d,in-20d,in-400d" {
			t.Fatalf("names = %s", got)
		}
	})
	t.Run("invalid parameters are 422", func(t *testing.T) {
		for _, q := range []string{"?within=3y", "?within=731d", "?within=30", "?within=0d", "?within=-1d", "?within=1w", "?within=", "?within=d", "?within=1.5d", "?limit=0", "?limit=201", "?limit=abc", "?limit=", "?limit=-1", "?limit=+5", "?limit=0001", "?limit=+1", "?limit=%201", "?limit=1.5"} {
			if rec := get(q, adminToken); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s status = %d body=%s", q, rec.Code, rec.Body.String())
			}
		}
		if rec := get("?within=3y", adminToken); !strings.Contains(rec.Body.String(), "admin token ttl must be 2 years or shorter") {
			t.Errorf("3y message = %s", rec.Body.String())
		}
		if rec := get("?within=abc", adminToken); !strings.Contains(rec.Body.String(), "whole number followed by h, d or y") {
			t.Errorf("grammar message = %s", rec.Body.String())
		}
		if rec := get("?limit=0", adminToken); !strings.Contains(rec.Body.String(), "limit must be a whole number between 1 and 200") {
			t.Errorf("limit message = %s", rec.Body.String())
		}
	})
	t.Run("limit boundaries", func(t *testing.T) {
		for _, q := range []string{"?limit=1", "?limit=200"} {
			if rec := get(q, adminToken); rec.Code != http.StatusOK {
				t.Errorf("%s status = %d", q, rec.Code)
			}
		}
		var rows []map[string]any
		_ = json.Unmarshal(get("?within=2y&limit=1", adminToken).Body.Bytes(), &rows)
		if len(rows) != 1 {
			t.Errorf("limit=1 rows = %d", len(rows))
		}
	})
	t.Run("401 without token and 403 for non-admin", func(t *testing.T) {
		if rec := get("", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("no token status = %d", rec.Code)
		}
		if _, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "plain", Password: "fixture-pass-9x", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		login, err := authService.LoginWithPassword(ctx, "plain", "fixture-pass-9x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if rec := get("", login.BearerToken); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin status = %d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/v1/admin-tokens/expiring", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST status = %d", rec.Code)
		}
	})
}

func TestRouterAdminExpiringTokensEmptyIsArray(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/admin-tokens/expiring", nil)
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%q, want 200 []", rec.Code, rec.Body.String())
	}
}

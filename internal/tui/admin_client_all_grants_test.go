package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

func TestHTTPAdminClientListAllRepositoryGrants(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.November, 21, 10, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	newClient := func(t *testing.T, handler http.HandlerFunc) *HTTPAdminClient {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }
		return client
	}

	t.Run("sends limit and the opaque cursor unchanged and decodes the page", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/admin/v1/repository-grants" {
				t.Fatalf("request = %s %s, want GET /admin/v1/repository-grants", r.Method, r.URL.Path)
			}
			if got := r.URL.Query().Get("limit"); got != "200" {
				t.Fatalf("limit = %q, want 200", got)
			}
			if got := r.URL.Query().Get("after"); got != "a+b/c=d&e" {
				t.Fatalf("after = %q, want the cursor unchanged", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer bearer-token" {
				t.Fatalf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"grants":[{"repository":"team/api","user_id":"u1","username":"ci","is_robot":true,"role":"repo-writer","created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-02T00:00:00Z"}],"next":"n-2"}`))
		})
		page, err := client.ListAllRepositoryGrants(context.Background(), session, 200, "a+b/c=d&e")
		if err != nil {
			t.Fatalf("ListAllRepositoryGrants() error = %v", err)
		}
		if page.Next != "n-2" || len(page.Grants) != 1 {
			t.Fatalf("page = %#v, want one grant and next n-2", page)
		}
		if g := page.Grants[0]; g.Repository != "team/api" || g.UserID != "u1" || g.Username != "ci" || !g.IsRobot || g.Role != domainauth.RepoRoleWriter || g.CreatedAt.Day() != 1 {
			t.Fatalf("grant = %#v", g)
		}
	})

	t.Run("omits a zero limit and an empty cursor", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				t.Fatalf("query = %q, want none", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"grants":[],"next":""}`))
		})
		page, err := client.ListAllRepositoryGrants(context.Background(), session, 0, "")
		if err != nil || page.Next != "" || len(page.Grants) != 0 {
			t.Fatalf("page = %#v, err = %v", page, err)
		}
	})

	t.Run("clamps the limit to the server maximum of 200", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("limit"); got != "200" {
				t.Fatalf("limit = %q, want 200", got)
			}
			_, _ = w.Write([]byte(`{"grants":[],"next":""}`))
		})
		if _, err := client.ListAllRepositoryGrants(context.Background(), session, 5000, ""); err != nil {
			t.Fatalf("ListAllRepositoryGrants() error = %v", err)
		}
	})

	t.Run("an API error is returned", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		if _, err := client.ListAllRepositoryGrants(context.Background(), session, 10, ""); err == nil {
			t.Fatal("ListAllRepositoryGrants() error = nil, want an error")
		}
	})
}

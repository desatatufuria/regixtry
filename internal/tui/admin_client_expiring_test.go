package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadableWithin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		within time.Duration
		want   string
	}{
		{"days", 7 * 24 * time.Hour, "7d"},
		{"one day", 24 * time.Hour, "1d"},
		{"hours when not a whole day", 36 * time.Hour, "36h"},
		{"years when a whole 365-day multiple", 2 * 365 * 24 * time.Hour, "2y"},
		{"never seconds", 90 * time.Minute, "2h"},
		{"at least one hour", time.Second, "1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := readableWithin(tt.within); got != tt.want {
				t.Fatalf("readableWithin(%v) = %q, want %q", tt.within, got, tt.want)
			}
		})
	}
}

func TestHTTPAdminClientListExpiringTokens(t *testing.T) {
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

	t.Run("sends the window as a readable string and decodes the rows", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/admin/v1/admin-tokens/expiring" {
				t.Fatalf("request = %s %s, want GET /admin/v1/admin-tokens/expiring", r.Method, r.URL.Path)
			}
			if got := r.URL.Query().Get("within"); got != "7d" {
				t.Fatalf("within = %q, want 7d", got)
			}
			if got := r.URL.Query().Get("limit"); got != "200" {
				t.Fatalf("limit = %q, want 200", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer bearer-token" {
				t.Fatalf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"t1","accessor":"acc1","name":"laptop","user_id":"u1","username":"carol","is_robot":false,"created_at":"2026-11-01T00:00:00Z","expires_at":"2026-11-25T10:00:00Z"}]`))
		})
		tokens, err := client.ListExpiringTokens(context.Background(), session, 7*24*time.Hour, 200)
		if err != nil {
			t.Fatalf("ListExpiringTokens() error = %v", err)
		}
		if len(tokens) != 1 || tokens[0].Username != "carol" || tokens[0].Name != "laptop" || tokens[0].UserID != "u1" || tokens[0].ExpiresAt.Day() != 25 {
			t.Fatalf("tokens = %#v, want one carol/laptop row expiring on the 25th", tokens)
		}
	})

	t.Run("omits a zero window and a zero limit", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				t.Fatalf("query = %q, want none", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[]`))
		})
		if _, err := client.ListExpiringTokens(context.Background(), session, 0, 0); err != nil {
			t.Fatalf("ListExpiringTokens() error = %v", err)
		}
	})

	t.Run("clamps the limit to the server maximum of 200", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("limit"); got != "200" {
				t.Fatalf("limit = %q, want 200", got)
			}
			_, _ = w.Write([]byte(`[]`))
		})
		if _, err := client.ListExpiringTokens(context.Background(), session, time.Hour, 5000); err != nil {
			t.Fatalf("ListExpiringTokens() error = %v", err)
		}
	})

	t.Run("an API error is returned", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		if _, err := client.ListExpiringTokens(context.Background(), session, time.Hour, 10); err == nil {
			t.Fatal("ListExpiringTokens() error = nil, want an error")
		}
	})
}

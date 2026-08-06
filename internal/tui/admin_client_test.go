package tui

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"registry/internal/domain/auth"
)

func TestHTTPAdminClientLogin(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 22, 45, 0, 0, time.UTC)

	tests := []struct {
		name        string
		statusCode  int
		body        string
		assertError func(t *testing.T, err error)
		assert      func(t *testing.T, session AdminSession)
	}{
		{
			name:       "successful login returns session",
			statusCode: http.StatusOK,
			body:       `{"access_token":"bearer-token","expires_in":120}`,
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("Login() error = %v, want nil", err)
				}
			},
			assert: func(t *testing.T, session AdminSession) {
				t.Helper()
				if session.Username != "operator" {
					t.Fatalf("Username = %q, want %q", session.Username, "operator")
				}
				if session.BearerToken != "bearer-token" {
					t.Fatalf("BearerToken = %q, want %q", session.BearerToken, "bearer-token")
				}
				if got, want := session.ExpiresAt, fixedNow.Add(120*time.Second); !got.Equal(want) {
					t.Fatalf("ExpiresAt = %s, want %s", got, want)
				}
			},
		},
		{
			name:       "invalid credentials return recoverable error",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"invalid credentials"}`,
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err == nil {
					t.Fatal("Login() error = nil, want invalid credentials error")
				}
				if got, want := err.Error(), "login: invalid credentials"; got != want {
					t.Fatalf("Login() error = %q, want %q", got, want)
				}
			},
			assert: func(t *testing.T, session AdminSession) {
				t.Helper()
				if session.IsAuthenticated() {
					t.Fatalf("session = %#v, want unauthenticated zero value", session)
				}
			},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.Method, http.MethodGet; got != want {
					t.Fatalf("method = %q, want %q", got, want)
				}
				if got, want := r.URL.Path, "/auth/token"; got != want {
					t.Fatalf("path = %q, want %q", got, want)
				}
				if got, want := r.Header.Get("Authorization"), "Basic "+base64.StdEncoding.EncodeToString([]byte("operator:secret-pass")); got != want {
					t.Fatalf("Authorization = %q, want %q", got, want)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client, err := NewHTTPAdminClient(server.URL, server.Client())
			if err != nil {
				t.Fatalf("NewHTTPAdminClient() error = %v", err)
			}
			client.now = func() time.Time { return fixedNow }

			session, err := client.Login(context.Background(), "  operator  ", "secret-pass")
			tc.assertError(t, err)
			tc.assert(t, session)
		})
	}
}

func TestHTTPAdminClientListReads(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 22, 50, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	tests := []struct {
		name     string
		path     string
		run      func(t *testing.T, client *HTTPAdminClient, ctx context.Context, session AdminSession)
		response string
	}{
		{
			name:     "list users",
			path:     "/admin/v1/users",
			response: `[{"id":"u-1","username":"alice","is_admin":true,"enabled":true,"created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}]`,
			run: func(t *testing.T, client *HTTPAdminClient, ctx context.Context, session AdminSession) {
				t.Helper()
				users, err := client.ListUsers(ctx, session)
				if err != nil {
					t.Fatalf("ListUsers() error = %v", err)
				}
				if len(users) != 1 || users[0].Username != "alice" || !users[0].IsAdmin || !users[0].Enabled {
					t.Fatalf("users = %#v, want decoded admin user", users)
				}
			},
		},
		{
			name:     "list grants",
			path:     "/admin/v1/users/user/one/grants",
			response: `[{"user_id":"u-1","repository":{"name":"library/alpine"},"role":"repo-writer","created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}]`,
			run: func(t *testing.T, client *HTTPAdminClient, ctx context.Context, session AdminSession) {
				t.Helper()
				grants, err := client.ListUserGrants(ctx, session, "user/one")
				if err != nil {
					t.Fatalf("ListUserGrants() error = %v", err)
				}
				if len(grants) != 1 || grants[0].Repository.String() != "library/alpine" || grants[0].Role != auth.RepoRoleWriter {
					t.Fatalf("grants = %#v, want decoded repository grant", grants)
				}
			},
		},
		{
			name:     "list admin tokens",
			path:     "/admin/v1/users/u-1/admin-tokens",
			response: `[{"id":"t-1","user_id":"u-1","kind":"admin-credential","name":"console","accessor":"tok_abc","expires_at":"2026-09-04T22:00:00Z","created_at":"2026-08-04T22:00:00Z"}]`,
			run: func(t *testing.T, client *HTTPAdminClient, ctx context.Context, session AdminSession) {
				t.Helper()
				tokens, err := client.ListUserAdminTokens(ctx, session, "u-1")
				if err != nil {
					t.Fatalf("ListUserAdminTokens() error = %v", err)
				}
				if len(tokens) != 1 || tokens[0].Accessor != "tok_abc" || tokens[0].Kind != auth.TokenKindAdminCredential {
					t.Fatalf("tokens = %#v, want decoded admin token", tokens)
				}
			},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.Method, http.MethodGet; got != want {
					t.Fatalf("method = %q, want %q", got, want)
				}
				if got, want := r.URL.Path, tc.path; got != want {
					t.Fatalf("path = %q, want %q", got, want)
				}
				if got, want := r.Header.Get("Authorization"), "Bearer bearer-token"; got != want {
					t.Fatalf("Authorization = %q, want %q", got, want)
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			client, err := NewHTTPAdminClient(server.URL+"/", server.Client())
			if err != nil {
				t.Fatalf("NewHTTPAdminClient() error = %v", err)
			}
			client.now = func() time.Time { return fixedNow }

			tc.run(t, client, context.Background(), session)
		})
	}
}

func TestHTTPAdminClientMapsInvalidTokenToExpiredSession(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="registry", error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"expired access token"}`))
	}))
	defer server.Close()

	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return time.Date(2026, time.August, 4, 22, 55, 0, 0, time.UTC) }

	_, err = client.ListUsers(context.Background(), AdminSession{BearerToken: "bearer-token"})
	if err == nil {
		t.Fatal("ListUsers() error = nil, want expired-session error")
	}
	if !IsAdminSessionExpired(err) {
		t.Fatalf("ListUsers() error = %v, want expired-session error", err)
	}
	if got, want := err.Error(), AdminSessionExpiredReasonExpired; got != want {
		t.Fatalf("ListUsers() error = %q, want %q", got, want)
	}
}

func TestHTTPAdminClientRejectsLocallyExpiredSession(t *testing.T) {
	t.Parallel()

	client, err := NewHTTPAdminClient("https://registry.example.com", nil)
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return time.Date(2026, time.August, 4, 23, 0, 0, 0, time.UTC) }

	_, err = client.ListUsers(context.Background(), AdminSession{BearerToken: "bearer-token", ExpiresAt: client.now().Add(-time.Second)})
	if err == nil {
		t.Fatal("ListUsers() error = nil, want expired-session error")
	}
	if !IsAdminSessionExpired(err) {
		t.Fatalf("ListUsers() error = %v, want expired-session error", err)
	}
}

package tui

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"registry/internal/domain/auth"
	"registry/internal/ports"
)

func TestNewHTTPAdminClient(t *testing.T) {
	t.Parallel()

	t.Run("uses bounded default timeout when client is nil", func(t *testing.T) {
		t.Parallel()

		client, err := NewHTTPAdminClient("https://registry.example.com", nil)
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		if client.client == nil {
			t.Fatal("client.client = nil, want allocated HTTP client")
		}
		if client.client == http.DefaultClient {
			t.Fatal("client.client reused http.DefaultClient, want dedicated bounded client")
		}
		if client.client.Timeout != defaultAdminClientTimeout {
			t.Fatalf("client timeout = %s, want %s", client.client.Timeout, defaultAdminClientTimeout)
		}
	})

	t.Run("preserves injected client", func(t *testing.T) {
		t.Parallel()

		injected := &http.Client{Timeout: 42 * time.Second}
		client, err := NewHTTPAdminClient("https://registry.example.com", injected)
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		if client.client != injected {
			t.Fatal("client.client != injected client, want injected instance preserved")
		}
	})
}

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

func TestHTTPAdminClientUserMutations(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	activeSession := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	tests := []struct {
		name        string
		method      string
		path        string
		statusCode  int
		headers     map[string]string
		body        string
		run         func(context.Context, *HTTPAdminClient, AdminSession) (ports.AdminUser, error)
		assertError func(t *testing.T, err error)
		assertUser  func(t *testing.T, user ports.AdminUser)
		session     AdminSession
	}{
		{
			name:       "enable user succeeds",
			method:     http.MethodPost,
			path:       "/admin/v1/users/user/one:enable",
			statusCode: http.StatusOK,
			body:       `{"id":"u-1","username":"alice","is_admin":true,"enabled":true,"created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:15:00Z"}`,
			run: func(ctx context.Context, client *HTTPAdminClient, session AdminSession) (ports.AdminUser, error) {
				return client.EnableUser(ctx, session, "user/one")
			},
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("EnableUser() error = %v, want nil", err)
				}
			},
			assertUser: func(t *testing.T, user ports.AdminUser) {
				t.Helper()
				if user.Username != "alice" || !user.Enabled {
					t.Fatalf("user = %#v, want enabled alice", user)
				}
			},
			session: activeSession,
		},
		{
			name:       "disable user conflict stays recoverable",
			method:     http.MethodPost,
			path:       "/admin/v1/users/u-1:disable",
			statusCode: http.StatusConflict,
			body:       `{"error":"cannot disable the last active admin"}`,
			run: func(ctx context.Context, client *HTTPAdminClient, session AdminSession) (ports.AdminUser, error) {
				return client.DisableUser(ctx, session, "u-1")
			},
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err == nil {
					t.Fatal("DisableUser() error = nil, want conflict error")
				}
				if got, want := err.Error(), "mutate admin resource: cannot disable the last active admin"; got != want {
					t.Fatalf("DisableUser() error = %q, want %q", got, want)
				}
			},
			assertUser: func(t *testing.T, user ports.AdminUser) {
				t.Helper()
				if user != (ports.AdminUser{}) {
					t.Fatalf("user = %#v, want zero value on error", user)
				}
			},
			session: activeSession,
		},
		{
			name:       "validation failure stays recoverable",
			method:     http.MethodPost,
			path:       "/admin/v1/users/u-1:enable",
			statusCode: http.StatusBadRequest,
			body:       `{"error":"user id is required"}`,
			run: func(ctx context.Context, client *HTTPAdminClient, session AdminSession) (ports.AdminUser, error) {
				return client.EnableUser(ctx, session, "u-1")
			},
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err == nil {
					t.Fatal("EnableUser() error = nil, want validation error")
				}
				if got, want := err.Error(), "mutate admin resource: user id is required"; got != want {
					t.Fatalf("EnableUser() error = %q, want %q", got, want)
				}
			},
			assertUser: func(t *testing.T, user ports.AdminUser) {
				t.Helper()
				if user != (ports.AdminUser{}) {
					t.Fatalf("user = %#v, want zero value on error", user)
				}
			},
			session: activeSession,
		},
		{
			name:       "invalid token maps to expired session",
			method:     http.MethodPost,
			path:       "/admin/v1/users/u-1:disable",
			statusCode: http.StatusUnauthorized,
			headers: map[string]string{
				"WWW-Authenticate": `Bearer realm="registry", error="invalid_token"`,
			},
			body: `{"error":"expired access token"}`,
			run: func(ctx context.Context, client *HTTPAdminClient, session AdminSession) (ports.AdminUser, error) {
				return client.DisableUser(ctx, session, "u-1")
			},
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err == nil {
					t.Fatal("DisableUser() error = nil, want expired-session error")
				}
				if !IsAdminSessionExpired(err) {
					t.Fatalf("DisableUser() error = %v, want expired-session error", err)
				}
				if got, want := err.Error(), AdminSessionExpiredReasonExpired; got != want {
					t.Fatalf("DisableUser() error = %q, want %q", got, want)
				}
			},
			assertUser: func(t *testing.T, user ports.AdminUser) {
				t.Helper()
				if user != (ports.AdminUser{}) {
					t.Fatalf("user = %#v, want zero value on error", user)
				}
			},
			session: activeSession,
		},
		{
			name:   "locally expired session rejects mutation without request",
			method: http.MethodPost,
			path:   "/admin/v1/users/u-1:disable",
			run: func(ctx context.Context, client *HTTPAdminClient, session AdminSession) (ports.AdminUser, error) {
				return client.DisableUser(ctx, session, "u-1")
			},
			assertError: func(t *testing.T, err error) {
				t.Helper()
				if err == nil {
					t.Fatal("DisableUser() error = nil, want expired-session error")
				}
				if !IsAdminSessionExpired(err) {
					t.Fatalf("DisableUser() error = %v, want expired-session error", err)
				}
			},
			assertUser: func(t *testing.T, user ports.AdminUser) {
				t.Helper()
				if user != (ports.AdminUser{}) {
					t.Fatalf("user = %#v, want zero value on error", user)
				}
			},
			session: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(-time.Second)},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				if got, want := r.Method, tc.method; got != want {
					t.Fatalf("method = %q, want %q", got, want)
				}
				if got, want := r.URL.Path, tc.path; got != want {
					t.Fatalf("path = %q, want %q", got, want)
				}
				if got, want := r.Header.Get("Authorization"), "Bearer bearer-token"; got != want {
					t.Fatalf("Authorization = %q, want %q", got, want)
				}

				w.Header().Set("Content-Type", "application/json")
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				if tc.statusCode != 0 {
					w.WriteHeader(tc.statusCode)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client, err := NewHTTPAdminClient(server.URL, server.Client())
			if err != nil {
				t.Fatalf("NewHTTPAdminClient() error = %v", err)
			}
			client.now = func() time.Time { return fixedNow }

			user, err := tc.run(context.Background(), client, tc.session)
			tc.assertError(t, err)
			tc.assertUser(t, user)

			if tc.session.IsExpired(fixedNow) {
				if requestCount != 0 {
					t.Fatalf("requestCount = %d, want 0 for locally expired session", requestCount)
				}
				return
			}
			if requestCount != 1 {
				t.Fatalf("requestCount = %d, want 1", requestCount)
			}
		})
	}
}

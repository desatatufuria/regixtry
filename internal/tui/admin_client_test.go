package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestNewHTTPAdminClient(t *testing.T) {
	t.Parallel()

	t.Run("uses bounded default timeout when client is nil", func(t *testing.T) {
		t.Parallel()

		client, err := NewHTTPAdminClient("https://regixtry.example.com", nil)
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
		client, err := NewHTTPAdminClient("https://regixtry.example.com", injected)
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
		_, _ = w.Write([]byte(`{"access_token":"bearer-token","expires_in":120}`))
	}))
	defer server.Close()

	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return fixedNow }

	session, err := client.Login(context.Background(), " operator ", "secret-pass")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Username != "operator" || session.BearerToken != "bearer-token" {
		t.Fatalf("session = %#v, want normalized operator session", session)
	}
	if got, want := session.ExpiresAt, fixedNow.Add(120*time.Second); !got.Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", got, want)
	}
}

func TestHTTPAdminClientMutationRoutes(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	tests := []struct {
		name        string
		method      string
		path        string
		statusCode  int
		body        string
		run         func(t *testing.T, client *HTTPAdminClient)
		assertBody  func(t *testing.T, payload map[string]any)
		assertError func(t *testing.T, err error)
	}{
		{
			name:       "create user",
			method:     http.MethodPost,
			path:       "/admin/v1/users",
			statusCode: http.StatusCreated,
			body:       `{"id":"u-1","username":"alice","is_admin":true,"enabled":true,"created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				user, err := client.CreateUser(context.Background(), session, ports.AdminCreateUserInput{Username: "alice", Password: "secret-pass", IsAdmin: true, Enabled: true})
				if err != nil {
					t.Fatalf("CreateUser() error = %v", err)
				}
				if user.Username != "alice" || !user.IsAdmin {
					t.Fatalf("user = %#v, want decoded created user", user)
				}
			},
			assertBody: func(t *testing.T, payload map[string]any) {
				if payload["username"] != "alice" || payload["password"] != "secret-pass" {
					t.Fatalf("payload = %#v, want create-user body", payload)
				}
			},
		},
		{
			name:       "reset password",
			method:     http.MethodPost,
			path:       "/admin/v1/users/u-1:reset-password",
			statusCode: http.StatusNoContent,
			run: func(t *testing.T, client *HTTPAdminClient) {
				if err := client.ResetPassword(context.Background(), session, ports.AdminResetPasswordInput{UserID: "u-1", NewPassword: "next-pass"}); err != nil {
					t.Fatalf("ResetPassword() error = %v", err)
				}
			},
			assertBody: func(t *testing.T, payload map[string]any) {
				if payload["new_password"] != "next-pass" {
					t.Fatalf("payload = %#v, want reset-password body", payload)
				}
			},
		},
		{
			name:       "put grant",
			method:     http.MethodPut,
			path:       "/admin/v1/users/u-1/grants/library/alpine",
			statusCode: http.StatusOK,
			body:       `{"user_id":"u-1","repository":{"name":"library/alpine"},"role":"repo-writer","created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				grant, err := client.PutUserGrant(context.Background(), session, ports.AdminPutRepoGrantInput{UserID: "u-1", Repository: "library/alpine", Role: domainauth.RepoRoleWriter})
				if err != nil {
					t.Fatalf("PutUserGrant() error = %v", err)
				}
				if got, want := grant.Role, domainauth.RepoRoleWriter; got != want {
					t.Fatalf("grant.Role = %q, want %q", got, want)
				}
			},
			assertBody: func(t *testing.T, payload map[string]any) {
				if payload["role"] != string(domainauth.RepoRoleWriter) {
					t.Fatalf("payload = %#v, want repo-writer role", payload)
				}
			},
		},
		{
			name:       "delete grant",
			method:     http.MethodDelete,
			path:       "/admin/v1/users/u-1/grants/library/alpine",
			statusCode: http.StatusNoContent,
			run: func(t *testing.T, client *HTTPAdminClient) {
				if err := client.DeleteUserGrant(context.Background(), session, "u-1", "library/alpine"); err != nil {
					t.Fatalf("DeleteUserGrant() error = %v", err)
				}
			},
		},
		{
			name:       "create admin token",
			method:     http.MethodPost,
			path:       "/admin/v1/users/u-1/admin-tokens",
			statusCode: http.StatusCreated,
			body:       `{"token":{"id":"t-1","user_id":"u-1","kind":"admin-credential","accessor":"tok_abc","expires_at":"2026-09-04T22:00:00Z","created_at":"2026-08-04T22:00:00Z"},"secret":"super-secret","accessor":"tok_abc","expires_at":"2026-09-04T22:00:00Z","target_user":{"id":"u-1","username":"alice","is_admin":true,"enabled":true,"created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				created, err := client.CreateUserAdminToken(context.Background(), session, ports.AdminCreateTokenInput{UserID: "u-1", Name: "console", TTL: 3600 * time.Second})
				if err != nil {
					t.Fatalf("CreateUserAdminToken() error = %v", err)
				}
				if created.Secret != "super-secret" || created.TargetUser.Username != "alice" {
					t.Fatalf("created = %#v, want decoded token payload", created)
				}
			},
			assertBody: func(t *testing.T, payload map[string]any) {
				if payload["name"] != "console" {
					t.Fatalf("payload = %#v, want name console", payload)
				}
				if payload["ttl_seconds"] != float64(3600) {
					t.Fatalf("payload = %#v, want ttl_seconds 3600", payload)
				}
			},
		},
		{
			name:       "revoke token",
			method:     http.MethodDelete,
			path:       "/admin/v1/users/u-1/admin-tokens/tok_abc",
			statusCode: http.StatusNoContent,
			run: func(t *testing.T, client *HTTPAdminClient) {
				if err := client.RevokeUserAdminToken(context.Background(), session, "u-1", "tok_abc"); err != nil {
					t.Fatalf("RevokeUserAdminToken() error = %v", err)
				}
			},
		},
		{
			name:       "enable user",
			method:     http.MethodPost,
			path:       "/admin/v1/users/user/one:enable",
			statusCode: http.StatusOK,
			body:       `{"id":"u-1","username":"alice","is_admin":true,"enabled":true,"created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				user, err := client.EnableUser(context.Background(), session, "user/one")
				if err != nil {
					t.Fatalf("EnableUser() error = %v", err)
				}
				if !user.Enabled {
					t.Fatalf("user = %#v, want enabled user", user)
				}
			},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			var receivedBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.Method, tc.method; got != want {
					t.Fatalf("method = %q, want %q", got, want)
				}
				if got, want := r.URL.Path, tc.path; got != want {
					t.Fatalf("path = %q, want %q", got, want)
				}
				if got, want := r.Header.Get("Authorization"), "Bearer bearer-token"; got != want {
					t.Fatalf("Authorization = %q, want %q", got, want)
				}
				if r.Body != nil && (tc.method == http.MethodPost || tc.method == http.MethodPut) {
					_ = json.NewDecoder(r.Body).Decode(&receivedBody)
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

			tc.run(t, client)
			if tc.assertBody != nil {
				tc.assertBody(t, receivedBody)
			}
		})
	}
}

func TestHTTPAdminClientMapsInvalidTokenToExpiredSession(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="regixtry", error="invalid_token"`)
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
}

func TestHTTPAdminClientRejectsLocallyExpiredSession(t *testing.T) {
	t.Parallel()

	client, err := NewHTTPAdminClient("https://regixtry.example.com", nil)
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

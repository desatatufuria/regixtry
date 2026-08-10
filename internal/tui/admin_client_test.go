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

func TestHTTPAdminClientFeatureRoutes(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
		body       string
		run        func(t *testing.T, client *HTTPAdminClient)
	}{
		{
			name:       "list features",
			method:     http.MethodGet,
			path:       "/admin/v1/features",
			statusCode: http.StatusOK,
			body:       `[{"name":"trivy","kind":"builtin","enabled":true,"configured":true}]`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				features, err := client.ListFeatures(context.Background(), session)
				if err != nil {
					t.Fatalf("ListFeatures() error = %v", err)
				}
				if len(features) != 1 || features[0].Name != "trivy" {
					t.Fatalf("features = %#v, want builtin trivy inventory", features)
				}
			},
		},
		{
			name:       "feature status",
			method:     http.MethodGet,
			path:       "/admin/v1/features/trivy/status",
			statusCode: http.StatusOK,
			body:       `{"name":"trivy","kind":"builtin","enabled":true,"configured":true,"schedule_enabled":true,"interval":"6h0m0s","timeout":"10m0s","cache_dir":"/var/lib/regixtry/trivy-cache","binary_path":"trivy","max_concurrency":2,"runtime":{"health":"ready","version":"0.57.1"}}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				status, err := client.GetFeatureStatus(context.Background(), session, "trivy")
				if err != nil {
					t.Fatalf("GetFeatureStatus() error = %v", err)
				}
				if status.Runtime.Version != "0.57.1" || !status.Enabled {
					t.Fatalf("status = %#v, want decoded feature status", status)
				}
			},
		},
		{
			name:       "show feature",
			method:     http.MethodGet,
			path:       "/admin/v1/features/trivy",
			statusCode: http.StatusOK,
			body:       `{"name":"trivy","kind":"builtin","enabled":true,"configured":true,"schedule_enabled":false,"interval":"24h0m0s","timeout":"15m0s","cache_dir":"/var/lib/regixtry/trivy-cache","binary_path":"trivy","max_concurrency":1}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				details, err := client.GetFeature(context.Background(), session, "trivy")
				if err != nil {
					t.Fatalf("GetFeature() error = %v", err)
				}
				if details.Name != "trivy" || !details.Configured {
					t.Fatalf("details = %#v, want decoded feature details", details)
				}
			},
		},
		{
			name:       "configure feature",
			method:     http.MethodPut,
			path:       "/admin/v1/features/trivy/config",
			statusCode: http.StatusOK,
			body:       `{"name":"trivy","kind":"builtin","enabled":true,"configured":true,"schedule_enabled":true,"interval":"6h0m0s","timeout":"10m0s","cache_dir":"/var/lib/regixtry/trivy-cache","binary_path":"trivy","max_concurrency":2}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				_, err := client.ConfigureFeature(context.Background(), session, "trivy", ports.FeatureConfigureInput{Enabled: boolPtr(true), ScheduleEnabled: boolPtr(true), Interval: durationPtr(6 * time.Hour), Timeout: durationPtr(10 * time.Minute), CacheDir: stringPtr("/var/lib/regixtry/trivy-cache"), BinaryPath: stringPtr("trivy"), MaxConcurrency: intPtr(2)})
				if err != nil {
					t.Fatalf("ConfigureFeature() error = %v", err)
				}
			},
		},
		{
			name:       "enable feature",
			method:     http.MethodPost,
			path:       "/admin/v1/features/trivy:enable",
			statusCode: http.StatusOK,
			body:       `{"name":"trivy","kind":"builtin","enabled":true,"configured":true,"schedule_enabled":true,"interval":"6h0m0s","timeout":"10m0s","cache_dir":"/var/lib/regixtry/trivy-cache","binary_path":"trivy","max_concurrency":2}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				details, err := client.EnableFeature(context.Background(), session, "trivy")
				if err != nil {
					t.Fatalf("EnableFeature() error = %v", err)
				}
				if !details.Enabled {
					t.Fatalf("details = %#v, want enabled feature state", details)
				}
			},
		},
		{
			name:       "disable feature",
			method:     http.MethodPost,
			path:       "/admin/v1/features/trivy:disable",
			statusCode: http.StatusOK,
			body:       `{"name":"trivy","kind":"builtin","enabled":false,"configured":true,"schedule_enabled":true,"interval":"6h0m0s","timeout":"10m0s","cache_dir":"/var/lib/regixtry/trivy-cache","binary_path":"trivy","max_concurrency":2}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				details, err := client.DisableFeature(context.Background(), session, "trivy")
				if err != nil {
					t.Fatalf("DisableFeature() error = %v", err)
				}
				if details.Enabled {
					t.Fatalf("details = %#v, want disabled feature state", details)
				}
			},
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
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

func boolPtr(value bool) *bool { return &value }

func durationPtr(value time.Duration) *time.Duration { return &value }

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
			// Phase 3 task 3.7 RED test (design.md Decision 3 route table):
			// the repository segment ("team/app") contains "/" and is NOT
			// PathEscape-d, mirroring PutUserGrant/DeleteUserGrant's own
			// unescaped-repository precedent above.
			name:       "list repository grants",
			method:     http.MethodGet,
			path:       "/admin/v1/repositories/team/app/grants",
			statusCode: http.StatusOK,
			body:       `[{"username":"bob","role":"repo-writer","created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}]`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				grants, err := client.ListRepositoryGrants(context.Background(), session, "team/app")
				if err != nil {
					t.Fatalf("ListRepositoryGrants() error = %v", err)
				}
				if len(grants) != 1 || grants[0].Username != "bob" || grants[0].Role != domainauth.RepoRoleWriter {
					t.Fatalf("grants = %#v, want decoded repository grants", grants)
				}
			},
		},
		{
			name:       "put repository grant",
			method:     http.MethodPut,
			path:       "/admin/v1/repositories/team/app/grants/carol",
			statusCode: http.StatusOK,
			body:       `{"username":"carol","role":"repo-reader","created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:10:00Z"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				grant, err := client.PutRepositoryGrant(context.Background(), session, ports.AdminPutRepositoryGrantInput{Repository: "team/app", Username: "carol", Role: domainauth.RepoRoleReader})
				if err != nil {
					t.Fatalf("PutRepositoryGrant() error = %v", err)
				}
				if got, want := grant.Role, domainauth.RepoRoleReader; got != want {
					t.Fatalf("grant.Role = %q, want %q", got, want)
				}
			},
			assertBody: func(t *testing.T, payload map[string]any) {
				if payload["role"] != string(domainauth.RepoRoleReader) {
					t.Fatalf("payload = %#v, want repo-reader role", payload)
				}
			},
		},
		{
			name:       "delete repository grant",
			method:     http.MethodDelete,
			path:       "/admin/v1/repositories/team/app/grants/carol",
			statusCode: http.StatusNoContent,
			run: func(t *testing.T, client *HTTPAdminClient) {
				if err := client.DeleteRepositoryGrant(context.Background(), session, "team/app", "carol"); err != nil {
					t.Fatalf("DeleteRepositoryGrant() error = %v", err)
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
		{
			// registry-acl-v1 robot-deletion follow-up (PR 4, backend-only):
			// the client method is added here as part of the API contract;
			// the TUI key/UI wiring is a separate PR 5 follow-up.
			name:       "delete robot",
			method:     http.MethodDelete,
			path:       "/admin/v1/robots/robot-1",
			statusCode: http.StatusNoContent,
			run: func(t *testing.T, client *HTTPAdminClient) {
				if err := client.DeleteRobot(context.Background(), session, "robot-1"); err != nil {
					t.Fatalf("DeleteRobot() error = %v", err)
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

// TestHTTPAdminClientAdminRobotRoutes is task 5.5's RED test (design.md
// Decision 6's two new robot routes; enable/disable and token issue/list/
// revoke are deliberately NOT re-tested here -- they are reused unchanged
// via EnableUser/DisableUser/CreateUserAdminToken etc., already covered by
// TestHTTPAdminClientMutationRoutes, called with the robot's user ID like
// any other user ID).
func TestHTTPAdminClientAdminRobotRoutes(t *testing.T) {
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
		assertBody func(t *testing.T, payload map[string]any)
	}{
		{
			name:       "list robots",
			method:     http.MethodGet,
			path:       "/admin/v1/robots",
			statusCode: http.StatusOK,
			body:       `[{"id":"u-2","username":"robot$ci","repository":"team/app","role":"repo-writer","enabled":true,"created_at":"2026-08-04T22:00:00Z"}]`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				robots, err := client.ListRobots(context.Background(), session)
				if err != nil {
					t.Fatalf("ListRobots() error = %v", err)
				}
				if len(robots) != 1 || robots[0].Username != "robot$ci" || robots[0].Repository != "team/app" || robots[0].Role != domainauth.RepoRoleWriter {
					t.Fatalf("robots = %#v, want decoded robot list", robots)
				}
			},
		},
		{
			name:       "create robot",
			method:     http.MethodPost,
			path:       "/admin/v1/robots",
			statusCode: http.StatusCreated,
			body:       `{"robot":{"id":"u-2","username":"robot$ci","repository":"team/app","role":"repo-writer","enabled":true,"created_at":"2026-08-04T22:00:00Z"},"secret":"robot-secret","accessor":"tok_robot","expires_at":"2026-09-04T22:00:00Z"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				created, err := client.CreateRobot(context.Background(), session, ports.AdminCreateRobotInput{Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, TTL: 3600 * time.Second})
				if err != nil {
					t.Fatalf("CreateRobot() error = %v", err)
				}
				if created.Secret != "robot-secret" || created.Robot.Username != "robot$ci" {
					t.Fatalf("created = %#v, want decoded robot creation payload", created)
				}
			},
			assertBody: func(t *testing.T, payload map[string]any) {
				if payload["name"] != "ci" || payload["repository"] != "team/app" || payload["role"] != string(domainauth.RepoRoleWriter) {
					t.Fatalf("payload = %#v, want create-robot body", payload)
				}
				if payload["ttl_seconds"] != float64(3600) {
					t.Fatalf("payload = %#v, want ttl_seconds 3600", payload)
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

// TestHTTPAdminClientRepositoryOverrideRoutes is the Phase 8 task 8.8 RED
// test: Get/List/Set/ClearRepositoryOverride wire to design.md Decision 7's
// admin resource, GET's absent-row 404 becomes (details, exists=false, nil
// error) rather than a surfaced error (mirrors the modal's Exists
// semantics), the repository is NOT PathEscape-d (design.md Decision 8 wire
// shape -- escaping the slash-bearing repository would defeat the
// server-side split), and Set builds a feature-shaped body (config_path for
// gitleaks, ignore_file_path/ignore_policy_path for trivy).
func TestHTTPAdminClientRepositoryOverrideRoutes(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 13, 10, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	t.Run("get found", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.URL.Path, "/admin/v1/features/trivy/repository-overrides/library/alpine"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"repository":"library/alpine","feature":"trivy","enabled":true,"ignore_file_path":"/etc/trivy/ignore","ignore_policy_path":"/etc/trivy/policy.rego","updated_at":"2026-08-13T09:00:00Z"}`))
		}))
		defer server.Close()
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		details, exists, err := client.GetRepositoryOverride(context.Background(), session, "library/alpine", "trivy")
		if err != nil {
			t.Fatalf("GetRepositoryOverride() error = %v", err)
		}
		if !exists {
			t.Fatal("exists = false, want true for a 200 response")
		}
		if !details.Enabled || details.IgnoreFilePath != "/etc/trivy/ignore" || details.IgnorePolicyPath != "/etc/trivy/policy.rego" {
			t.Fatalf("details = %#v, want decoded override", details)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}))
		defer server.Close()
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		details, exists, err := client.GetRepositoryOverride(context.Background(), session, "library/alpine", "trivy")
		if err != nil {
			t.Fatalf("GetRepositoryOverride() error = %v, want nil (404 is a valid state, not an error)", err)
		}
		if exists {
			t.Fatal("exists = true, want false for a 404 response")
		}
		if !reflect.DeepEqual(details, ports.RepositoryOverrideDetails{}) {
			t.Fatalf("details = %#v, want zero value on 404", details)
		}
	})

	t.Run("set builds feature-shaped body and does not path-escape the repository", func(t *testing.T) {
		t.Parallel()
		var receivedPath string
		var receivedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			if got, want := r.Method, http.MethodPut; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"repository":"team/config","feature":"gitleaks","enabled":false,"config_path":"/etc/gitleaks/config.toml","updated_at":"2026-08-13T09:00:00Z"}`))
		}))
		defer server.Close()
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		details, err := client.SetRepositoryOverride(context.Background(), session, "team/config", "gitleaks", ports.RepositoryOverrideDetails{Enabled: false, ConfigPath: "/etc/gitleaks/config.toml"})
		if err != nil {
			t.Fatalf("SetRepositoryOverride() error = %v", err)
		}
		if got, want := receivedPath, "/admin/v1/features/gitleaks/repository-overrides/team/config"; got != want {
			t.Fatalf("path = %q, want %q (repository must not be PathEscape-d)", got, want)
		}
		if _, hasIgnoreFile := receivedBody["ignore_file_path"]; hasIgnoreFile {
			t.Fatalf("body = %#v, want no trivy-only fields for a gitleaks Set", receivedBody)
		}
		if got, want := receivedBody["config_path"], "/etc/gitleaks/config.toml"; got != want {
			t.Fatalf("body[config_path] = %#v, want %q", got, want)
		}
		if details.ConfigPath != "/etc/gitleaks/config.toml" {
			t.Fatalf("details = %#v, want decoded response", details)
		}
	})

	t.Run("clear uses DELETE and StatusNoContent", func(t *testing.T) {
		t.Parallel()
		var receivedMethod, receivedPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			receivedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		if err := client.ClearRepositoryOverride(context.Background(), session, "library/alpine", "trivy"); err != nil {
			t.Fatalf("ClearRepositoryOverride() error = %v", err)
		}
		if receivedMethod != http.MethodDelete {
			t.Fatalf("method = %q, want DELETE", receivedMethod)
		}
		if receivedPath != "/admin/v1/features/trivy/repository-overrides/library/alpine" {
			t.Fatalf("path = %q, want the unescaped repository resource path", receivedPath)
		}
	})

	t.Run("list decodes every stored override row", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.URL.Path, "/admin/v1/features/trivy/repository-overrides"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"repository":"library/alpine","feature":"trivy","enabled":false},{"repository":"team/api","feature":"trivy","enabled":true,"ignore_file_path":"/etc/trivy/ignore"}]`))
		}))
		defer server.Close()
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		overrides, err := client.ListRepositoryOverrides(context.Background(), session, "trivy")
		if err != nil {
			t.Fatalf("ListRepositoryOverrides() error = %v", err)
		}
		if len(overrides) != 2 {
			t.Fatalf("len(overrides) = %d, want 2", len(overrides))
		}
		if overrides[0].Repository != "library/alpine" || overrides[0].Enabled {
			t.Fatalf("overrides[0] = %#v, want disabled library/alpine", overrides[0])
		}
	})
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
		{
			name:       "install feature runtime",
			method:     http.MethodPost,
			path:       "/admin/v1/features/trivy:install",
			statusCode: http.StatusOK,
			body:       `{"status":"ready","active_version":"0.57.1","active_binary_path":"/var/lib/regixtry/features/trivy/bin/active/trivy","cache_dir":"/var/lib/regixtry/features/trivy/trivy-cache","receipt_path":"/var/lib/regixtry/features/trivy/receipts/0.57.1.json"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				state, err := client.InstallFeatureRuntime(context.Background(), session, "trivy", "0.57.1")
				if err != nil {
					t.Fatalf("InstallFeatureRuntime() error = %v", err)
				}
				if state.ActiveVersion != "0.57.1" || state.Status != ports.FeatureRuntimeStatusReady {
					t.Fatalf("state = %#v, want decoded runtime install state", state)
				}
			},
		},
		{
			name:       "rollback feature runtime",
			method:     http.MethodPost,
			path:       "/admin/v1/features/trivy:rollback",
			statusCode: http.StatusOK,
			body:       `{"status":"ready","active_version":"0.57.1","previous_version":"0.58.0"}`,
			run: func(t *testing.T, client *HTTPAdminClient) {
				state, err := client.RollbackFeatureRuntime(context.Background(), session, "trivy")
				if err != nil {
					t.Fatalf("RollbackFeatureRuntime() error = %v", err)
				}
				if state.ActiveVersion != "0.57.1" || state.PreviousVersion != "0.58.0" {
					t.Fatalf("state = %#v, want decoded runtime rollback state", state)
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

func TestHTTPAdminClientFeaturePageAndActionRoutes(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	t.Run("get feature page", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.Method, http.MethodGet; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := r.URL.Path, "/admin/v1/features/trivy"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"summary":{"name":"trivy","kind":"builtin","enabled":true,"configured":true},"header":[{"label":"Enabled","value":"true"}],"sections":[{"id":"config","title":"Configuration","kind":"fields","fields":[{"label":"Registry Reachable URL","value":"https://registry.internal:5443"}]},{"id":"runs","title":"Recent Runs","kind":"rows","rows":[{"title":"library/alpine@latest","status":"completed","detail":"critical=1 high=2"}]}],"actions":[{"id":"refresh","label":"Refresh"},{"id":"disable","label":"Disable","confirm_title":"Confirm Disable","confirm_message":"Confirm disable feature \"trivy\"?"}]}`))
		}))
		defer server.Close()

		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		page, err := client.GetFeaturePage(context.Background(), session, "trivy")
		if err != nil {
			t.Fatalf("GetFeaturePage() error = %v", err)
		}
		if page.Summary.Name != "trivy" || len(page.Sections) != 2 || len(page.Actions) != 2 {
			t.Fatalf("page = %#v, want decoded feature page", page)
		}
	})

	t.Run("execute feature action", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.Method, http.MethodPost; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := r.URL.Path, "/admin/v1/features/trivy/actions/disable"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":"Feature \"trivy\" disabled."}`))
		}))
		defer server.Close()

		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		result, err := client.ExecuteFeatureAction(context.Background(), session, "trivy", "disable")
		if err != nil {
			t.Fatalf("ExecuteFeatureAction() error = %v", err)
		}
		if got, want := result.Message, `Feature "trivy" disabled.`; got != want {
			t.Fatalf("result.Message = %q, want %q", got, want)
		}
	})
}

func TestHTTPAdminClientListScanRuns(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	t.Run("lists repository-filtered scan runs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.Method, http.MethodGet; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := r.URL.Path, "/admin/v1/scan-runs"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("repository"), "library/alpine"; got != want {
				t.Fatalf("repository query = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("limit"), "5"; got != want {
				t.Fatalf("limit query = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"run-1","repository":"library/alpine","requested_ref":"latest","digest":"sha256:111","status":"completed","trigger":"manual","created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:00:00Z","critical":1,"high":2,"medium":3,"low":4,"trivy_version":"0.57.1"}]`))
		}))
		defer server.Close()

		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		runs, err := client.ListScanRuns(context.Background(), session, "library/alpine", 5)
		if err != nil {
			t.Fatalf("ListScanRuns() error = %v", err)
		}
		if len(runs) != 1 || runs[0].Repository != "library/alpine" || runs[0].Critical != 1 {
			t.Fatalf("runs = %#v, want decoded scan run payload", runs)
		}
	})

	t.Run("omits empty repository filter and zero limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.URL.Path, "/admin/v1/scan-runs"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got := r.URL.Query().Get("repository"); got != "" {
				t.Fatalf("repository query = %q, want omitted", got)
			}
			if got := r.URL.Query().Get("limit"); got != "" {
				t.Fatalf("limit query = %q, want omitted", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		runs, err := client.ListScanRuns(context.Background(), session, "", 0)
		if err != nil {
			t.Fatalf("ListScanRuns() error = %v", err)
		}
		if len(runs) != 0 {
			t.Fatalf("runs = %#v, want empty decoded scan-run list", runs)
		}
	})
}

// TestHTTPAdminClientListRepositoryScanSummaries is the RED test for the
// repository-alerts-scan-coverage fix's TUI client leg: the new admin
// endpoint's one-row-per-repository response decodes straight through, and
// a zero/omitted limit omits the query param, mirroring ListScanRuns.
func TestHTTPAdminClientListRepositoryScanSummaries(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 15, 21, 20, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	t.Run("decodes one row per repository with run_count", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.Method, http.MethodGet; got != want {
				t.Fatalf("method = %q, want %q", got, want)
			}
			if got, want := r.URL.Path, "/admin/v1/repository-scan-summaries"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("limit"), "25"; got != want {
				t.Fatalf("limit query = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"run":{"id":"run-1","repository":"team/hot","requested_ref":"latest","digest":"sha256:111","status":"completed","trigger":"manual","created_at":"2026-08-15T21:00:00Z","updated_at":"2026-08-15T21:00:00Z","critical":1,"high":0,"medium":0,"low":0},"run_count":5}]`))
		}))
		defer server.Close()

		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		summaries, err := client.ListRepositoryScanSummaries(context.Background(), session, 25)
		if err != nil {
			t.Fatalf("ListRepositoryScanSummaries() error = %v", err)
		}
		if len(summaries) != 1 || summaries[0].Run.Repository != "team/hot" || summaries[0].RunCount != 5 {
			t.Fatalf("summaries = %#v, want one team/hot summary with run_count 5", summaries)
		}
	})

	t.Run("omits zero limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("limit"); got != "" {
				t.Fatalf("limit query = %q, want omitted", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }

		summaries, err := client.ListRepositoryScanSummaries(context.Background(), session, 0)
		if err != nil {
			t.Fatalf("ListRepositoryScanSummaries() error = %v", err)
		}
		if len(summaries) != 0 {
			t.Fatalf("summaries = %#v, want empty decoded list", summaries)
		}
	})
}

func TestHTTPAdminClientGetScanRunDetail(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/admin/v1/scan-runs/run-1"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run":{"id":"run-1","repository":"library/alpine","requested_ref":"latest","digest":"sha256:111","status":"completed","trigger":"manual","created_at":"2026-08-04T22:00:00Z","updated_at":"2026-08-04T22:00:00Z","critical":1,"trivy_version":"0.58.1"},"findings":[{"severity":"CRITICAL","vulnerability_id":"CVE-1","package_name":"openssl","installed_version":"3.0.0","fixed_version":"3.0.1","fixable":true}],"db_freshness":{"freshness_state":"fresh"},"reference_freshness":"current"}`))
	}))
	defer server.Close()

	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return fixedNow }

	detail, err := client.GetScanRunDetail(context.Background(), session, "run-1")
	if err != nil {
		t.Fatalf("GetScanRunDetail() error = %v", err)
	}
	if detail.Run.ID != "run-1" || detail.ReferenceFreshness != ports.ScanReferenceFreshnessCurrent || len(detail.Findings) != 1 {
		t.Fatalf("detail = %#v, want decoded scan-run detail payload", detail)
	}
}

func TestHTTPAdminClientGetSecretScanFindings(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/admin/v1/secret-scan-findings"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("repository"), "library/alpine"; got != want {
			t.Fatalf("repository query = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("digest"), "sha256:111"; got != want {
			t.Fatalf("digest query = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run":{"id":"secret-run-1","repository":"library/alpine","digest":"sha256:111","status":"completed"},"findings":[{"rule_id":"aws-access-token","path":"config.json","start_line":3,"end_line":3}]}`))
	}))
	defer server.Close()

	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return fixedNow }

	detail, err := client.GetSecretScanFindings(context.Background(), session, "library/alpine", "sha256:111")
	if err != nil {
		t.Fatalf("GetSecretScanFindings() error = %v", err)
	}
	if len(detail.Findings) != 1 || detail.Findings[0].RuleID != "aws-access-token" {
		t.Fatalf("detail = %#v, want decoded secret scan findings payload", detail)
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

// TestHTTPAdminClientGetUpdateChannel guards the tui-update-check feature's
// read path: GET /update-channel, decoded into the bare channel string.
func TestHTTPAdminClientGetUpdateChannel(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/update-channel"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channel":"insider"}`))
	}))
	defer server.Close()

	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return fixedNow }

	channel, err := client.GetUpdateChannel(context.Background(), session)
	if err != nil {
		t.Fatalf("GetUpdateChannel() error = %v", err)
	}
	if channel != "insider" {
		t.Fatalf("channel = %q, want %q", channel, "insider")
	}
}

// TestHTTPAdminClientSetUpdateChannel guards the tui-update-check feature's
// write path: PUT /admin/v1/update-channel with the requested channel in
// the body, decoding the persisted value back from the response.
func TestHTTPAdminClientSetUpdateChannel(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPut; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/admin/v1/update-channel"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		var payload struct {
			Channel string `json:"channel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload.Channel != "insider" {
			t.Fatalf("request body channel = %q, want %q", payload.Channel, "insider")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channel":"insider"}`))
	}))
	defer server.Close()

	client, err := NewHTTPAdminClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPAdminClient() error = %v", err)
	}
	client.now = func() time.Time { return fixedNow }

	channel, err := client.SetUpdateChannel(context.Background(), session, "insider")
	if err != nil {
		t.Fatalf("SetUpdateChannel() error = %v", err)
	}
	if channel != "insider" {
		t.Fatalf("channel = %q, want %q", channel, "insider")
	}
}

func boolPtr(value bool) *bool { return &value }

func durationPtr(value time.Duration) *time.Duration { return &value }

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

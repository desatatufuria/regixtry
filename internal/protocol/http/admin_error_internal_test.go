package regixtryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domainregistry "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const adminSecretLeak = "pq: connection refused to 10.0.0.5:5432 password=hunter2"

// failingAdminAuth makes every admin service call used below fail with err.
type failingAdminAuth struct {
	fakeAuthService
	err error
}

func (f failingAdminAuth) ListAdminUsers(context.Context, domainauth.Principal) ([]ports.AdminUser, error) {
	return nil, f.err
}

func (f failingAdminAuth) CreateAdminUserToken(context.Context, domainauth.Principal, ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	return ports.AdminCreatedToken{}, f.err
}

func (f failingAdminAuth) CreateAdminRobot(context.Context, domainauth.Principal, ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	return ports.AdminCreatedRobot{}, f.err
}

func (f failingAdminAuth) ListAdminRobots(context.Context, domainauth.Principal) ([]ports.AdminRobot, error) {
	return nil, f.err
}

func adminRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeAdminErrorBody(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body = %s", err, recorder.Body.String())
	}
	if len(payload) != 1 {
		t.Fatalf("payload = %#v, want exactly the admin {\"error\": ...} shape", payload)
	}
	message, ok := payload["error"].(string)
	if !ok {
		t.Fatalf("payload = %#v, want a string \"error\" field", payload)
	}
	return message
}

func TestAdminEndpointsAnswerGeneric500AndLogTheRealError(t *testing.T) {
	t.Parallel()

	admin := &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}
	backendErrors := []struct {
		name string
		err  error
	}{
		{"plain backend error", errors.New(adminSecretLeak)},
		{"registry error with unmapped code", &domainregistry.Error{Code: "backend_exploded", Message: adminSecretLeak}},
		{"auth error with unmapped code", &domainauth.Error{Code: "backend_exploded", Message: adminSecretLeak}},
	}
	endpoints := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create admin token", http.MethodPost, "/admin/v1/users/user-1/admin-tokens", `{"name":"ci"}`},
		{"create robot", http.MethodPost, "/admin/v1/robots", `{"name":"bot","repository":"team/app","role":"pull"}`},
		{"list robots", http.MethodGet, "/admin/v1/robots", ""},
		{"list users", http.MethodGet, "/admin/v1/users", ""},
	}

	for _, be := range backendErrors {
		for _, ep := range endpoints {
			t.Run(be.name+"/"+ep.name, func(t *testing.T) {
				t.Parallel()

				var logs bytes.Buffer
				blobStore, metadataStore, cleanup := newTestStores(t)
				router := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{},
					failingAdminAuth{fakeAuthService: fakeAuthService{verify: admin}, err: be.err},
					WithLogger(log.New(&logs, "", 0)))
				t.Cleanup(drainingCleanup(router, cleanup))

				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, adminRequest(ep.method, ep.path, ep.body))

				assertAdminGeneric500(t, recorder, &logs)
			})
		}
	}
}

func TestAdminScanSettingsStoreFailureAnswersGeneric500AndLogs(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	admin := &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}
	blobStore, metadataStore, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{},
		fakeAuthService{verify: admin}, WithLogger(log.New(&logs, "", 0)))
	t.Cleanup(drainingCleanup(router, cleanup))
	if err := metadataStore.Close(); err != nil {
		t.Fatalf("metadataStore.Close() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, adminRequest(http.MethodGet, "/admin/v1/scan-settings", ""))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := decodeAdminErrorBody(t, recorder); got != "internal server error" {
		t.Fatalf("error = %q, want the generic message", got)
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "closed") {
		t.Fatalf("body = %s, leaked the backend error", recorder.Body.String())
	}
	if !strings.Contains(logs.String(), "status=500") || !strings.Contains(logs.String(), "database is closed") {
		t.Fatalf("log = %q, want status=500 and the real backend error", logs.String())
	}
}

func assertAdminGeneric500(t *testing.T, recorder *httptest.ResponseRecorder, logs *bytes.Buffer) {
	t.Helper()

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := decodeAdminErrorBody(t, recorder); got != "internal server error" {
		t.Fatalf("error = %q, want the generic message", got)
	}
	for _, leaked := range []string{"hunter2", "10.0.0.5", "pq:"} {
		if strings.Contains(recorder.Body.String(), leaked) {
			t.Fatalf("body = %s, leaked %q", recorder.Body.String(), leaked)
		}
	}
	line := logs.String()
	if !strings.Contains(line, "status=500") || !strings.Contains(line, "error=") || !strings.Contains(line, "hunter2") {
		t.Fatalf("log = %q, want status=500 with the real error in an error= field", line)
	}
}

func TestWriteAdminErrorNon500MappingsStayUnchanged(t *testing.T) {
	t.Parallel()

	challenge := ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
	cases := []struct {
		name          string
		err           error
		wantStatus    int
		wantMessage   string
		wantChallenge bool
	}{
		{"registry validation", domainregistry.NewValidationError("bad input"), http.StatusUnprocessableEntity, "bad input", false},
		{"registry not found", domainregistry.NewNotFoundError("repository", "team/app"), http.StatusNotFound, errorMessageOf(domainregistry.NewNotFoundError("repository", "team/app")), false},
		{"registry conflict", domainregistry.NewConflictError("already exists"), http.StatusConflict, "already exists", false},
		{"registry unauthorized is 403", domainregistry.NewUnauthorizedError("not allowed here"), http.StatusForbidden, "not allowed here", false},
		{"registry unsupported is 501", &domainregistry.Error{Code: domainregistry.ErrorCodeUnsupported, Message: "capability is off"}, http.StatusNotImplemented, "capability is off", false},
		{"auth invalid credentials", domainauth.NewInvalidCredentialsError(), http.StatusUnauthorized, errorMessageOf(domainauth.NewInvalidCredentialsError()), true},
		{"auth forbidden", domainauth.NewForbiddenError("admin access is required"), http.StatusForbidden, "admin access is required", false},
		{"auth validation", domainauth.NewValidationError("bad scope"), http.StatusUnprocessableEntity, "bad scope", false},
		{"auth not found", domainauth.NewNotFoundError("user", "bob"), http.StatusNotFound, errorMessageOf(domainauth.NewNotFoundError("user", "bob")), false},
		{"auth conflict", domainauth.NewConflictError("duplicate"), http.StatusConflict, "duplicate", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			writeAdminError(recorder, tc.err, challenge)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if got := decodeAdminErrorBody(t, recorder); got != tc.wantMessage {
				t.Fatalf("error = %q, want %q", got, tc.wantMessage)
			}
			gotChallenge := recorder.Header().Get("WWW-Authenticate")
			if tc.wantChallenge && gotChallenge != challengeHeader(challenge) {
				t.Fatalf("WWW-Authenticate = %q, want %q", gotChallenge, challengeHeader(challenge))
			}
			if !tc.wantChallenge && gotChallenge != "" {
				t.Fatalf("WWW-Authenticate = %q, want none", gotChallenge)
			}
		})
	}
}

package regixtryhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

type errorEnvelope struct {
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func decodeErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()

	var payload errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body = %s", err, recorder.Body.String())
	}
	if len(payload.Errors) != 1 {
		t.Fatalf("Errors = %#v, want exactly one error, body = %s", payload.Errors, recorder.Body.String())
	}
	return payload
}

func errorMessageOf(err error) string {
	var registryErr *domain.Error
	if errors.As(err, &registryErr) {
		return registryErr.Message
	}
	var authErr *domainauth.Error
	if errors.As(err, &authErr) {
		return authErr.Message
	}
	return err.Error()
}

// newClosedStoreRouter builds a router whose metadata store was closed after
// construction, so every metadata-backed route fails with a raw backend error
// (not a domain error) -- the same shape as a locked or unreachable database.
func newClosedStoreRouter(t *testing.T, options ...RouterOption) *Router {
	t.Helper()

	blobStore, metadataStore, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil, options...)
	router.service.SetDeleteEnabled(true)
	t.Cleanup(drainingCleanup(router, cleanup))

	if err := metadataStore.Close(); err != nil {
		t.Fatalf("metadataStore.Close() error = %v", err)
	}
	return router
}

func assertGenericInternalError(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	payload := decodeErrorEnvelope(t, recorder)
	if payload.Errors[0].Code != "UNKNOWN" {
		t.Fatalf("code = %q, want %q (a 500 must never carry a not-found style code), body = %s", payload.Errors[0].Code, "UNKNOWN", recorder.Body.String())
	}
	if payload.Errors[0].Message != "internal server error" {
		t.Fatalf("message = %q, want the fixed generic message, body = %s", payload.Errors[0].Message, recorder.Body.String())
	}
	body := strings.ToLower(recorder.Body.String())
	for _, leaked := range []string{"sql", "database", "closed", "driver"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("body = %s, must not leak raw backend error detail (found %q)", recorder.Body.String(), leaked)
		}
	}
}

func TestRouterBackendFailureAnswersGeneric500(t *testing.T) {
	t.Parallel()

	digest := domain.DigestFromBytes([]byte("internal-error-delete")).String()
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"manifest GET", http.MethodGet, "/v2/library/alpine/manifests/latest"},
		{"manifest DELETE", http.MethodDelete, "/v2/library/alpine/manifests/" + digest},
		{"tags list", http.MethodGet, "/v2/library/alpine/tags/list"},
		{"blob upload start", http.MethodPost, "/v2/library/alpine/blobs/uploads/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := newClosedStoreRouter(t)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))

			assertGenericInternalError(t, recorder)
		})
	}
}

func TestWriteErrorUnmappedFailuresAnswerGeneric500(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		err         error
		defaultCode string
	}{
		{"raw error", errors.New("pq: connection refused to 10.0.0.5:5432"), "MANIFEST_UNKNOWN"},
		{"wrapped raw error", fmt.Errorf("publish: %w", errors.New("secret detail C")), "MANIFEST_INVALID"},
		{"unmapped registry code", &domain.Error{Code: domain.ErrorCode("SOMETHING_NEW"), Message: "secret detail A"}, "BLOB_UNKNOWN"},
		{"unmapped auth code", &domainauth.Error{Code: domainauth.ErrorCodeBootstrapRequired, Message: "secret detail B"}, "UNAUTHORIZED"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
			writeError(recorder, req, tc.err, ports.Challenge{Scheme: "Bearer", Realm: "regixtry"}, tc.defaultCode)

			assertGenericInternalError(t, recorder)
			body := recorder.Body.String()
			for _, leaked := range []string{"secret detail", "10.0.0.5", "pq:"} {
				if strings.Contains(body, leaked) {
					t.Fatalf("body = %s, must not contain %q", body, leaked)
				}
			}
		})
	}
}

func TestRouterLogsTheRealErrorBehindAnInternalFailure(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := newClosedStoreRouter(t, WithLogger(log.New(&logs, "", 0)))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil))

	line := logs.String()
	if !strings.Contains(line, "status=500") {
		t.Fatalf("log = %q, want the request line with status=500", line)
	}
	if !strings.Contains(line, "error=") || !strings.Contains(line, "database is closed") {
		t.Fatalf("log = %q, want the real backend error in an error= field", line)
	}
	if strings.Contains(recorder.Body.String(), "database is closed") {
		t.Fatalf("body = %s, the real error must reach the log only, never the client", recorder.Body.String())
	}
}

func TestRouterRequestLogHasNoErrorFieldForHandledFailures(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, nil, WithLogger(log.New(&logs, "", 0)))
	t.Cleanup(cleanup)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	if strings.Contains(logs.String(), "error=") {
		t.Fatalf("log = %q, a handled 404 must not carry an error= field", logs.String())
	}
}

func TestWriteErrorNon500MappingsStayUnchanged(t *testing.T) {
	t.Parallel()

	challenge := ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
	cases := []struct {
		name          string
		err           error
		defaultCode   string
		wantStatus    int
		wantCode      string
		wantChallenge bool
	}{
		{"not found keeps MANIFEST_UNKNOWN", domain.NewNotFoundError("manifest", "latest"), "MANIFEST_UNKNOWN", http.StatusNotFound, "MANIFEST_UNKNOWN", false},
		{"not found keeps BLOB_UNKNOWN", domain.NewNotFoundError("blob", "sha256:x"), "BLOB_UNKNOWN", http.StatusNotFound, "BLOB_UNKNOWN", false},
		{"not found keeps NAME_UNKNOWN", domain.NewNotFoundError("repository", "library/alpine"), "NAME_UNKNOWN", http.StatusNotFound, "NAME_UNKNOWN", false},
		{"unauthorized", domain.NewUnauthorizedError("authentication required"), "MANIFEST_UNKNOWN", http.StatusUnauthorized, "UNAUTHORIZED", true},
		{"policy violation", domain.NewPolicyViolationError("blocked by scan policy"), "MANIFEST_UNKNOWN", http.StatusForbidden, "DENIED", false},
		{"conflict keeps call-site code", domain.NewConflictError("manifest references missing blob"), "MANIFEST_INVALID", http.StatusConflict, "MANIFEST_INVALID", false},
		{"conflict with no call-site code is DENIED", domain.NewConflictError("conflict"), "", http.StatusConflict, "DENIED", false},
		{"invalid digest keeps call-site code", domain.NewInvalidDigestError("nope"), "BLOB_UNKNOWN", http.StatusBadRequest, "BLOB_UNKNOWN", false},
		{"validation with no call-site code is DIGEST_INVALID", domain.NewValidationError("bad request"), "", http.StatusBadRequest, "DIGEST_INVALID", false},
		{"auth invalid credentials", domainauth.NewInvalidCredentialsError(), "UNAUTHORIZED", http.StatusUnauthorized, "UNAUTHORIZED", true},
		{"auth forbidden", domainauth.NewForbiddenError("not allowed"), "MANIFEST_UNKNOWN", http.StatusUnauthorized, "UNAUTHORIZED", true},
		{"auth validation", domainauth.NewValidationError("bad scope"), "NAME_INVALID", http.StatusBadRequest, "NAME_INVALID", false},
		{"auth not found", domainauth.NewNotFoundError("user", "bob"), "NAME_UNKNOWN", http.StatusNotFound, "NAME_UNKNOWN", false},
		{"auth conflict", domainauth.NewConflictError("duplicate"), "UNAUTHORIZED", http.StatusConflict, "UNAUTHORIZED", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
			writeError(recorder, req, tc.err, challenge, tc.defaultCode)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			payload := decodeErrorEnvelope(t, recorder)
			if payload.Errors[0].Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", payload.Errors[0].Code, tc.wantCode)
			}
			if payload.Errors[0].Message != errorMessageOf(tc.err) {
				t.Fatalf("message = %q, want the domain message %q", payload.Errors[0].Message, errorMessageOf(tc.err))
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

func TestRouterInvalidRepositoryNameAnswers400NameInvalid(t *testing.T) {
	t.Parallel()

	digest := domain.DigestFromBytes([]byte("invalid-repository-delete")).String()
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"manifest GET", http.MethodGet, "/v2/Bad_Repo/manifests/latest"},
		{"manifest DELETE", http.MethodDelete, "/v2/Bad_Repo/manifests/" + digest},
		{"tags list", http.MethodGet, "/v2/Bad_Repo/tags/list"},
		{"blob upload start", http.MethodPost, "/v2/Bad_Repo/blobs/uploads/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler, cleanup := newTestRouter(t, allowAllAccessController{})
			t.Cleanup(cleanup)
			handler.service.SetDeleteEnabled(true)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (a malformed name is a client error), body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			payload := decodeErrorEnvelope(t, recorder)
			if payload.Errors[0].Code != "NAME_INVALID" {
				t.Fatalf("code = %q, want %q, body = %s", payload.Errors[0].Code, "NAME_INVALID", recorder.Body.String())
			}
			if !strings.Contains(payload.Errors[0].Message, "Bad_Repo") {
				t.Fatalf("message = %q, want the actionable invalid repository message", payload.Errors[0].Message)
			}
		})
	}
}

package regixtryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regixtry/internal/domain/auth"
	domainregistry "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

// seedCommittedBlob commits payload directly to blobStore (bypassing the
// upload HTTP protocol, irrelevant to these GC endpoint tests) and returns
// its digest.
func seedCommittedBlob(t *testing.T, blobStore *fsblob.Store, payload []byte) domainregistry.Digest {
	t.Helper()

	upload, err := blobStore.BeginUpload(context.Background(), domainregistry.MustParseRepositoryRef("library/alpine"))
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := blobStore.PutUploadChunk(context.Background(), upload.ID, strings.NewReader(string(payload))); err != nil {
		t.Fatalf("PutUploadChunk() error = %v", err)
	}
	digest := domainregistry.DigestFromBytes(payload)
	if _, err := blobStore.CommitUpload(context.Background(), upload.ID, digest); err != nil {
		t.Fatalf("CommitUpload() error = %v", err)
	}
	return digest
}

// backdateBlob rewinds a committed blob file's mtime past gcGraceWindow
// (design's fixed 24h constant), the same os.Chtimes technique used at the
// fsblob layer (T3), so an HTTP-level test can prove past-grace behavior
// without waiting 24 real hours or reaching into Service's unexported now
// field from a different package.
func backdateBlob(t *testing.T, contentRoot string, digest domainregistry.Digest) {
	t.Helper()

	path := filepath.Join(contentRoot, "blobs", digest.Algorithm(), digest.Encoded())
	old := time.Now().UTC().Add(-25 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("os.Chtimes() error = %v", err)
	}
}

// TestWriteAdminErrorMapsErrorCodeUnsupportedTo501 pins tasks.md 6.3
// (design.md D9): ErrorCodeUnsupported must map to 501, with the message
// (the only disambiguator on admin routes -- no code field at all) naming
// the exact flag.
func TestWriteAdminErrorMapsErrorCodeUnsupportedTo501(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	err := domainregistry.NewUnsupportedError("blob garbage collection delete is disabled (REGISTRY_GC_DELETE_ENABLED)")
	writeAdminError(recorder, err, ports.Challenge{})

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotImplemented)
	}
	if !strings.Contains(recorder.Body.String(), "REGISTRY_GC_DELETE_ENABLED") {
		t.Fatalf("body = %q, want it to name REGISTRY_GC_DELETE_ENABLED", recorder.Body.String())
	}
}

// TestAdminGCRoutesRequireAdminPrincipal is T12's report-endpoint half:
// unauthenticated/non-admin callers must be rejected on both the report
// create and report fetch routes, before any GC work runs.
func TestAdminGCRoutesRequireAdminPrincipal(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports", nil)
	postReq.Header.Set("Authorization", "Bearer reader-token")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code == http.StatusOK || postRecorder.Code == http.StatusCreated {
		t.Fatalf("POST /admin/v1/gc/reports status = %d, want a non-admin caller to be rejected", postRecorder.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/gc/reports/does-not-matter", nil)
	getReq.Header.Set("Authorization", "Bearer reader-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code == http.StatusOK {
		t.Fatalf("GET /admin/v1/gc/reports/{id} status = %d, want a non-admin caller to be rejected", getRecorder.Code)
	}
}

// TestAdminPostGCReportsReturns201WithDetail pins the report create route: a
// durable resource now exists (design.md's executor-level 201 call, not
// 202), so the response is the full GCReportDetail including the report id.
func TestAdminPostGCReportsReturns201WithDetail(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body struct {
		Report struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"report"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal() error = %v, body = %s", err, recorder.Body.String())
	}
	if body.Report.ID == "" {
		t.Fatalf("body.Report.ID is empty, body = %s", recorder.Body.String())
	}
	if body.Report.Status != "reported" {
		t.Fatalf("body.Report.Status = %q, want %q", body.Report.Status, "reported")
	}
}

// TestAdminGetGCReportResourceReturnsDetailOrNotFound covers both branches
// of the GET-by-id route: an unknown id is a 404, and a report freshly
// created via POST is fetchable by its id.
func TestAdminGetGCReportResourceReturnsDetailOrNotFound(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	unknownReq := httptest.NewRequest(http.MethodGet, "/admin/v1/gc/reports/does-not-exist", nil)
	unknownReq.Header.Set("Authorization", "Bearer admin-token")
	unknownRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unknownRecorder, unknownReq)
	if unknownRecorder.Code != http.StatusNotFound {
		t.Fatalf("GET unknown report status = %d, want %d", unknownRecorder.Code, http.StatusNotFound)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports", nil)
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusCreated {
		t.Fatalf("POST /admin/v1/gc/reports status = %d, want %d", postRecorder.Code, http.StatusCreated)
	}
	var created struct {
		Report struct {
			ID string `json:"id"`
		} `json:"report"`
	}
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/gc/reports/"+created.Report.ID, nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET known report status = %d, want %d; body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
}

// TestAdminGCDeleteReturns501NamingEnvVar is T4's HTTP half: with the flag
// off (the default), an authorized caller's delete request returns 501
// naming REGISTRY_GC_DELETE_ENABLED, the report stays "reported", and the
// blob file is untouched.
func TestAdminGCDeleteReturns501NamingEnvVar(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	payload := []byte("gc-http-flag-off-body")
	digest := seedCommittedBlob(t, blobStore, payload)

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports", nil)
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusCreated {
		t.Fatalf("POST /admin/v1/gc/reports status = %d, want %d; body = %s", postRecorder.Code, http.StatusCreated, postRecorder.Body.String())
	}
	var created struct {
		Report struct {
			ID string `json:"id"`
		} `json:"report"`
	}
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports/"+created.Report.ID+"/delete", nil)
	deleteReq.Header.Set("Authorization", "Bearer admin-token")
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusNotImplemented {
		t.Fatalf("POST delete status = %d, want %d; body = %s", deleteRecorder.Code, http.StatusNotImplemented, deleteRecorder.Body.String())
	}
	if !strings.Contains(deleteRecorder.Body.String(), "REGISTRY_GC_DELETE_ENABLED") {
		t.Fatalf("body = %q, want it to name REGISTRY_GC_DELETE_ENABLED", deleteRecorder.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/gc/reports/"+created.Report.ID, nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	var detail struct {
		Report struct {
			Status string `json:"status"`
		} `json:"report"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if detail.Report.Status != "reported" {
		t.Fatalf("report status after refused delete = %q, want %q", detail.Report.Status, "reported")
	}

	exists, err := blobStore.BlobExists(context.Background(), digest)
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}
	if !exists {
		t.Fatal("blob was removed from disk despite REGISTRY_GC_DELETE_ENABLED being off")
	}
}

// TestAdminGCDeleteUnauthorizedRejectedBeforeFlagCheck is T12's
// delete-endpoint half: an unauthorized caller must be rejected by
// requireAdminPrincipal (which runs before this route's handler body at
// all), not answered with the flag-disabled 501 (design.md Decision F).
func TestAdminGCDeleteUnauthorizedRejectedBeforeFlagCheck(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "reader-1", Username: "reader", IsAdmin: false}})

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports/some-report-id/delete", nil)
	req.Header.Set("Authorization", "Bearer reader-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusNotImplemented {
		t.Fatalf("status = %d, want an authorization rejection, not the flag-disabled 501 (Decision F)", recorder.Code)
	}
	if recorder.Code != http.StatusUnauthorized && recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 401 or 403", recorder.Code)
	}
}

// TestAdminGCDeleteRouteRejectsWrongMethod extends 5.4/6.12: now that the
// delete route exists, GET on it must 405 with Allow: POST, never fall
// through to a different branch.
func TestAdminGCDeleteRouteRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/gc/reports/some-report-id/delete", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if recorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("Allow header = %q, want %q", recorder.Header().Get("Allow"), http.MethodPost)
	}
}

// TestAdminGCDeleteEndpointDeletesAndReturnsTerminalDetail is the first
// RED/GREEN pair to exercise the delete route against a real Phase 8
// unlink: with the flag enabled, POST .../delete on a genuinely unreferenced,
// grace-expired candidate returns 200 with a terminal GCReportDetail --
// status "deleted", the candidate outcome "deleted", and the blob file
// actually gone from disk.
func TestAdminGCDeleteEndpointDeletesAndReturnsTerminalDetail(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	contentRoot := filepath.Join(rootDir, "content")
	blobStore, err := fsblob.New(contentRoot)
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	store, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()

	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	handler.service.SetGCDeleteEnabled(true)

	payload := []byte("gc-http-real-delete-body")
	digest := seedCommittedBlob(t, blobStore, payload)
	backdateBlob(t, contentRoot, digest)

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports", nil)
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusCreated {
		t.Fatalf("POST /admin/v1/gc/reports status = %d, want %d; body = %s", postRecorder.Code, http.StatusCreated, postRecorder.Body.String())
	}
	var created struct {
		Report struct {
			ID string `json:"id"`
		} `json:"report"`
	}
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports/"+created.Report.ID+"/delete", nil)
	deleteReq.Header.Set("Authorization", "Bearer admin-token")
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("POST delete status = %d, want %d; body = %s", deleteRecorder.Code, http.StatusOK, deleteRecorder.Body.String())
	}

	var result struct {
		Report struct {
			Status         string `json:"status"`
			DeletedCount   int    `json:"deleted_count"`
			BytesReclaimed int64  `json:"bytes_reclaimed"`
		} `json:"report"`
		Candidates []struct {
			Digest  string `json:"digest"`
			Outcome string `json:"outcome"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(deleteRecorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v, body = %s", err, deleteRecorder.Body.String())
	}
	if result.Report.Status != "deleted" {
		t.Fatalf("Report.Status = %q, want %q", result.Report.Status, "deleted")
	}
	if result.Report.DeletedCount != 1 {
		t.Fatalf("Report.DeletedCount = %d, want 1", result.Report.DeletedCount)
	}
	if result.Report.BytesReclaimed != int64(len(payload)) {
		t.Fatalf("Report.BytesReclaimed = %d, want %d", result.Report.BytesReclaimed, len(payload))
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Outcome != "deleted" {
		t.Fatalf("Candidates = %#v, want exactly one with outcome %q", result.Candidates, "deleted")
	}

	exists, err := blobStore.BlobExists(context.Background(), digest)
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}
	if exists {
		t.Fatal("blob file still exists on disk after a successful delete")
	}
}

// TestAdminGCReportDispatchRejectsUnknownSubaction is the threat-matrix
// admin-path-dispatch RED test: an unrecognised sub-action segment must 404,
// never silently fall through to the GET-by-id branch.
func TestAdminGCReportDispatchRejectsUnknownSubaction(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/gc/reports/some-id/bogus", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d for an unknown gc/reports/{id}/{subaction}", recorder.Code, http.StatusNotFound)
	}
}

// TestAdminGCDeleteRejectsUnknownReportID pins spec.md's "Delete without a
// report reference is rejected" scenario (Requirement: Delete Requires a
// Valid Prior Report) for a well-formed but never-existed report id. The
// flag is deliberately ON so a 404 here proves the ID-validation path
// itself, distinct from the flag-off 501 path already covered by
// TestAdminGCDeleteReturns501NamingEnvVar.
//
// Note: the empty-report-id half of the same spec scenario is covered at
// the service layer instead of here
// (TestDeleteByGCReportRejectsEmptyReportID in service_gc_test.go). An
// HTTP-level attempt to reach handleAdminGCReportResource's `id == ""`
// branch requires a double-slash path (".../gc/reports//delete"), which
// net/http's own ServeMux intercepts and 307-redirects before this
// application's routing code ever runs -- that branch is unreachable
// through this router's real HTTP surface, so testing it at the router
// layer would only prove net/http's path-cleaning behavior, not this
// application's rejection logic.
func TestAdminGCDeleteRejectsUnknownReportID(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, store, allowAllAccessController{}, fakeAuthService{verify: &auth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	handler.service.SetGCDeleteEnabled(true)

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/gc/reports/00000000-0000-0000-0000-000000000000/delete", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d for a delete request referencing an unknown report id (flag on)", recorder.Code, http.StatusNotFound)
	}
}

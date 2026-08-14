package regixtryhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"

	_ "modernc.org/sqlite"
)

func TestRouterChallengesProtectedPull(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	if got := recorder.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected WWW-Authenticate header")
	}
}

// TestRouterManifestMethodDispatchCharacterizesCurrentBehavior pins
// handleManifest's current method dispatch (design.md Testing Strategy):
// PUT/GET/HEAD behave as today, and any other method — including DELETE,
// which does not exist yet — falls through to the default 405 branch with
// Allow: PUT, GET, HEAD. This MUST land and pass before any task adds a
// DELETE case, so the Allow-list edit does not silently change an unasserted
// response (manifest-blob-delete tasks.md 1.1).
func TestRouterManifestMethodDispatchCharacterizesCurrentBehavior(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	if uploadStartRecorder.Code != http.StatusAccepted {
		t.Fatalf("upload start status = %d, want %d", uploadStartRecorder.Code, http.StatusAccepted)
	}
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	digest := domain.DigestFromBytes([]byte("manifest-dispatch-layer")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, bytes.NewBufferString("manifest-dispatch-layer"))
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("blob commit status = %d, want %d", commitRecorder.Code, http.StatusCreated)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":24},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":24}]}`)

	putReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/dispatch", bytes.NewReader(manifestPayload))
	putReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d, want %d", putRecorder.Code, http.StatusCreated)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/dispatch", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getRecorder.Code, http.StatusOK)
	}

	headReq := httptest.NewRequest(http.MethodHead, "/v2/library/alpine/manifests/dispatch", nil)
	headRecorder := httptest.NewRecorder()
	handler.ServeHTTP(headRecorder, headReq)
	if headRecorder.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want %d", headRecorder.Code, http.StatusOK)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v2/library/alpine/manifests/dispatch", nil)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE status = %d, want %d", deleteRecorder.Code, http.StatusMethodNotAllowed)
	}
	if got, want := deleteRecorder.Header().Get("Allow"), "PUT, GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

// publishRouterManifestTag uploads one blob and publishes a manifest that
// references it under tag, returning the manifest digest (Docker-Content-
// Digest from the PUT response) and the blob digest. The two are different
// values -- the manifest digest covers the JSON envelope, the blob digest
// covers raw content -- mirroring the inline upload-then-PUT sequence used
// throughout this file (e.g. TestRouterUploadAndReadFlow), generalized so
// manifest-blob-delete's DELETE tests have real, removable content.
func publishRouterManifestTag(t *testing.T, handler *Router, repository string, tag string, content string) (manifestDigest string, blobDigest string) {
	t.Helper()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/"+repository+"/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	if uploadStartRecorder.Code != http.StatusAccepted {
		t.Fatalf("upload start status = %d, want %d", uploadStartRecorder.Code, http.StatusAccepted)
	}
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	blobDigest = domain.DigestFromBytes([]byte(content)).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+blobDigest, bytes.NewBufferString(content))
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("blob commit status = %d, want %d", commitRecorder.Code, http.StatusCreated)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/v2/"+repository+"/manifests/"+tag, bytes.NewReader(routerManifestPayload(blobDigest, len(content))))
	putReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusCreated {
		t.Fatalf("PUT manifest status = %d, want %d", putRecorder.Code, http.StatusCreated)
	}

	return putRecorder.Header().Get("Docker-Content-Digest"), blobDigest
}

// routerManifestPayload builds a minimal valid OCI image manifest JSON
// envelope referencing one blob digest for both config and its single layer.
func routerManifestPayload(blobDigest string, size int) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":%q,"size":%d},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":%q,"size":%d}]}`, blobDigest, size, blobDigest, size))
}

// TestRouterDeleteManifestRespectsFlagAuthAndExistence covers design.md's
// Interfaces/Contracts table for DELETE (manifest-blob-delete tasks.md 4.1):
// flag-on authorized digest delete returns 202 with a removal body, flag-off
// authorized delete reuses the handleUploadState UNSUPPORTED precedent
// (never a bare 405), an unauthenticated caller is challenged for the
// distinct delete scope, and an absent reference answers MANIFEST_UNKNOWN.
func TestRouterDeleteManifestRespectsFlagAuthAndExistence(t *testing.T) {
	t.Parallel()

	t.Run("flag on, authorized digest delete returns 202 with removal body", func(t *testing.T) {
		t.Parallel()

		handler, cleanup := newTestRouter(t, allowAllAccessController{})
		defer cleanup()
		handler.service.SetDeleteEnabled(true)

		manifestDigest, _ := publishRouterManifestTag(t, handler, "library/alpine", "latest", "delete-flag-on-content")

		req := httptest.NewRequest(http.MethodDelete, "/v2/library/alpine/manifests/"+manifestDigest, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
		}

		var body struct {
			Repository      string   `json:"repository"`
			Reference       string   `json:"reference"`
			Digest          string   `json:"digest"`
			ManifestRemoved bool     `json:"manifestRemoved"`
			TagsRemoved     []string `json:"tagsRemoved"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if !body.ManifestRemoved || body.Digest != manifestDigest || len(body.TagsRemoved) != 1 || body.TagsRemoved[0] != "latest" {
			t.Fatalf("body = %#v, want manifestRemoved=true digest=%q tagsRemoved=[latest]", body, manifestDigest)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+manifestDigest, nil)
		getRecorder := httptest.NewRecorder()
		handler.ServeHTTP(getRecorder, getReq)
		if getRecorder.Code != http.StatusNotFound {
			t.Fatalf("get after delete status = %d, want %d", getRecorder.Code, http.StatusNotFound)
		}
	})

	t.Run("flag off, authorized delete returns 400 UNSUPPORTED and leaves the manifest intact", func(t *testing.T) {
		t.Parallel()

		handler, cleanup := newTestRouter(t, allowAllAccessController{})
		defer cleanup()

		manifestDigest, _ := publishRouterManifestTag(t, handler, "library/alpine", "latest", "delete-flag-off-content")

		req := httptest.NewRequest(http.MethodDelete, "/v2/library/alpine/manifests/"+manifestDigest, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}

		var payload struct {
			Errors []struct {
				Code string `json:"code"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if len(payload.Errors) != 1 || payload.Errors[0].Code != "UNSUPPORTED" {
			t.Fatalf("payload.Errors = %#v, want single UNSUPPORTED error", payload.Errors)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+manifestDigest, nil)
		getRecorder := httptest.NewRecorder()
		handler.ServeHTTP(getRecorder, getReq)
		if getRecorder.Code != http.StatusOK {
			t.Fatalf("get after refused delete status = %d, want %d (manifest must survive)", getRecorder.Code, http.StatusOK)
		}
	})

	t.Run("unauthenticated delete is challenged for the delete scope", func(t *testing.T) {
		t.Parallel()

		handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{})
		defer cleanup()
		handler.service.SetDeleteEnabled(true)

		req := httptest.NewRequest(http.MethodDelete, "/v2/team/app/manifests/latest", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `scope="repository:team/app:delete"`) {
			t.Fatalf("WWW-Authenticate = %q, want delete scope challenge", got)
		}
	})

	t.Run("unknown reference returns 404 MANIFEST_UNKNOWN", func(t *testing.T) {
		t.Parallel()

		handler, cleanup := newTestRouter(t, allowAllAccessController{})
		defer cleanup()
		handler.service.SetDeleteEnabled(true)

		req := httptest.NewRequest(http.MethodDelete, "/v2/library/alpine/manifests/missing-tag", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}

		var payload struct {
			Errors []struct {
				Code string `json:"code"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if len(payload.Errors) != 1 || payload.Errors[0].Code != "MANIFEST_UNKNOWN" {
			t.Fatalf("payload.Errors = %#v, want single MANIFEST_UNKNOWN error", payload.Errors)
		}
	})
}

// TestRouterManifestAllowHeaderIncludesDeleteForOtherMethods pins tasks.md
// 4.2: once DELETE is a real (flag-gated) verb on this route, the 405
// fallback for any other method must advertise it in Allow, growing from
// "PUT, GET, HEAD" (task 1.1's characterization) to "PUT, GET, HEAD, DELETE".
func TestRouterManifestAllowHeaderIncludesDeleteForOtherMethods(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/manifests/allow-header-check", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got, want := recorder.Header().Get("Allow"), "PUT, GET, HEAD, DELETE"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

// TestRouterDeleteOnOtherManifestSubroutesUnchanged pins the threat-matrix
// "HTTP method dispatch" boundary (tasks.md 4.4): adding a DELETE branch
// inside handleManifest's switch must not shadow or alter DELETE dispatch on
// any sibling route -- tags/list, blob reads, and upload state all keep
// answering exactly as they did before this change.
func TestRouterDeleteOnOtherManifestSubroutesUnchanged(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()
	handler.service.SetDeleteEnabled(true)

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadID := uploadStartRecorder.Header().Get("Docker-Upload-UUID")

	_, blobDigest := publishRouterManifestTag(t, handler, "library/alpine", "latest", "dispatch-guard-content")

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
	}{
		{name: "DELETE tags/list stays 405", method: http.MethodDelete, path: "/v2/library/alpine/tags/list", wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodGet},
		{name: "DELETE blob read stays 405", method: http.MethodDelete, path: "/v2/library/alpine/blobs/" + blobDigest, wantStatus: http.StatusMethodNotAllowed, wantAllow: "GET, HEAD"},
		{name: "DELETE upload state stays 400 UNSUPPORTED (unchanged upload-cancel stub)", method: http.MethodDelete, path: "/v2/library/alpine/blobs/uploads/" + uploadID, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if tt.wantAllow != "" {
				if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
					t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
				}
			}
		})
	}
}

// TestRouterRejectsDeleteWithPushOnlyTokenButAllowsPut pins the threat-matrix
// "Privilege reuse" boundary (tasks.md 4.5): a pull,push-scoped writer token
// must not silently gain delete just because HasWriteAccess already passes
// for push -- it needs the distinct delete scope action, and the same token
// keeps working for PUT.
func TestRouterRejectsDeleteWithPushOnlyTokenButAllowsPut(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()

	seedRouter := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	manifestDigest, blobDigest := publishRouterManifestTag(t, seedRouter, "team/app", "reuse-guard", "delete-privilege-reuse-content")

	principal := domainauth.Principal{
		Subject:  "atk_pushonly",
		Username: "writer",
		Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
		Scopes:   []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}},
	}
	handler := newRouterWithStores(blobStore, metadataStore, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{verify: &principal})
	handler.service.SetDeleteEnabled(true)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v2/team/app/manifests/"+manifestDigest, nil)
	deleteReq.Header.Set("Authorization", "Bearer pull-push-token")
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE status = %d, want %d", deleteRecorder.Code, http.StatusUnauthorized)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/v2/team/app/manifests/reuse-guard-still-pushable", bytes.NewReader(routerManifestPayload(blobDigest, len("delete-privilege-reuse-content"))))
	putReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	putReq.Header.Set("Authorization", "Bearer pull-push-token")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusCreated {
		t.Fatalf("PUT status = %d, want %d (same token must still push)", putRecorder.Code, http.StatusCreated)
	}
}

// TestRouterDeleteManifestAuthorizationMatrix pins tasks.md 4.6's integration
// table: role alone is insufficient (reader always fails, even carrying a
// delete scope) and scope alone is insufficient (a writer/admin missing the
// delete scope action fails) -- only role AND scope together grant delete.
func TestRouterDeleteManifestAuthorizationMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		principal  domainauth.Principal
		wantStatus int
	}{
		{
			name: "reader role with full delete scope is still unauthorized",
			principal: domainauth.Principal{
				Subject: "atk_reader", Username: "reader",
				Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleReader}},
				Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push", "delete"}, Canonical: "repository:team/app:pull,push,delete"}},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "writer role with delete scope succeeds",
			principal: domainauth.Principal{
				Subject: "atk_writer", Username: "writer",
				Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
				Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push", "delete"}, Canonical: "repository:team/app:pull,push,delete"}},
			},
			wantStatus: http.StatusAccepted,
		},
		{
			name: "repo-admin role with delete scope succeeds",
			principal: domainauth.Principal{
				Subject: "atk_repoadmin", Username: "repo-admin",
				Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleAdmin}},
				Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push", "delete"}, Canonical: "repository:team/app:pull,push,delete"}},
			},
			wantStatus: http.StatusAccepted,
		},
		{
			name: "writer role with pull,push token only is unauthorized",
			principal: domainauth.Principal{
				Subject: "atk_writer_pp", Username: "writer",
				Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
				Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "writer role with pull,push,delete token succeeds",
			principal: domainauth.Principal{
				Subject: "atk_writer_ppd", Username: "writer",
				Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
				Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push", "delete"}, Canonical: "repository:team/app:pull,push,delete"}},
			},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blobStore, metadataStore, cleanup := newTestStores(t)
			defer cleanup()

			seedRouter := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
			manifestDigest, _ := publishRouterManifestTag(t, seedRouter, "team/app", "matrix-"+tt.principal.Subject, "delete-matrix-content-"+tt.principal.Subject)

			principal := tt.principal
			handler := newRouterWithStores(blobStore, metadataStore, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{verify: &principal})
			handler.service.SetDeleteEnabled(true)

			req := httptest.NewRequest(http.MethodDelete, "/v2/team/app/manifests/"+manifestDigest, nil)
			req.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
		})
	}
}

// TestRouterDeleteManifestNeverTouchesBlobFiles pins the threat-matrix
// "Blob-store scope creep" boundary (tasks.md 4.7): neither the digest-
// cascade delete path nor the tag-only delete path opens, stats, or unlinks
// any blob file -- the content directory's file set and every file's bytes
// are identical before and after both delete operations.
func TestRouterDeleteManifestNeverTouchesBlobFiles(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobStore, err := fsblob.New(filepath.Join(rootDir, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	handler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	defer handler.service.WaitForBackgroundWork()
	handler.service.SetDeleteEnabled(true)

	digestDeleteTarget, _ := publishRouterManifestTag(t, handler, "library/alpine", "digest-target", "delete-blob-guard-digest")
	publishRouterManifestTag(t, handler, "library/alpine", "tag-target", "delete-blob-guard-tag")

	contentDir := filepath.Join(rootDir, "content")
	before := snapshotBlobFiles(t, contentDir)
	if len(before) == 0 {
		t.Fatal("expected at least one blob file to exist before delete")
	}

	digestDeleteReq := httptest.NewRequest(http.MethodDelete, "/v2/library/alpine/manifests/"+digestDeleteTarget, nil)
	digestDeleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(digestDeleteRecorder, digestDeleteReq)
	if digestDeleteRecorder.Code != http.StatusAccepted {
		t.Fatalf("digest delete status = %d, want %d", digestDeleteRecorder.Code, http.StatusAccepted)
	}

	tagDeleteReq := httptest.NewRequest(http.MethodDelete, "/v2/library/alpine/manifests/tag-target", nil)
	tagDeleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tagDeleteRecorder, tagDeleteReq)
	if tagDeleteRecorder.Code != http.StatusAccepted {
		t.Fatalf("tag delete status = %d, want %d", tagDeleteRecorder.Code, http.StatusAccepted)
	}

	after := snapshotBlobFiles(t, contentDir)
	if len(before) != len(after) {
		t.Fatalf("blob file count changed by delete: before = %d, after = %d", len(before), len(after))
	}
	for path, hash := range before {
		if after[path] != hash {
			t.Fatalf("blob file %q changed by delete: before hash = %q, after hash = %q", path, hash, after[path])
		}
	}
}

// snapshotBlobFiles returns a path -> content-hash map covering every
// regular file under dir, used to assert byte-identical blob storage across
// an operation that must never touch it.
func snapshotBlobFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files[relative] = domain.DigestFromBytes(data).String()
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk(%s) error = %v", dir, err)
	}
	return files
}

func TestWriteErrorMapsPolicyViolationTo403DeniedWithoutWWWAuthenticate(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	recorder := httptest.NewRecorder()

	writeError(recorder, req, domain.NewPolicyViolationError("blocked by scan policy"), ports.Challenge{Scheme: "Bearer", Realm: "regixtry"}, "MANIFEST_UNKNOWN")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want empty — re-authenticating cannot resolve a policy violation", got)
	}

	var payload struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response body error = %v", err)
	}
	if len(payload.Errors) != 1 || payload.Errors[0].Code != "DENIED" {
		t.Fatalf("payload.Errors = %#v, want single DENIED error", payload.Errors)
	}
}

func TestRouterUploadAndReadFlow(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	if uploadStartRecorder.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, want %d", uploadStartRecorder.Code, http.StatusAccepted)
	}

	uploadLocation := uploadStartRecorder.Header().Get("Location")
	uploadID := uploadStartRecorder.Header().Get("Docker-Upload-UUID")
	if uploadLocation == "" || uploadID == "" {
		t.Fatal("expected upload location and UUID headers")
	}

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)
	if appendRecorder.Code != http.StatusAccepted {
		t.Fatalf("append status = %d, want %d", appendRecorder.Code, http.StatusAccepted)
	}

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)
	if commitRecorder.Code != http.StatusCreated {
		t.Fatalf("commit status = %d, want %d", commitRecorder.Code, http.StatusCreated)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}

	getManifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	getManifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getManifestRecorder, getManifestReq)
	if getManifestRecorder.Code != http.StatusOK {
		t.Fatalf("get manifest status = %d, want %d", getManifestRecorder.Code, http.StatusOK)
	}

	var catalog struct {
		Repositories []string `json:"repositories"`
	}
	catalogReq := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	catalogRecorder := httptest.NewRecorder()
	handler.ServeHTTP(catalogRecorder, catalogReq)
	if catalogRecorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, want %d", catalogRecorder.Code, http.StatusOK)
	}
	if err := json.Unmarshal(catalogRecorder.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("catalog JSON error = %v", err)
	}
	if len(catalog.Repositories) != 1 || catalog.Repositories[0] != "library/alpine" {
		t.Fatalf("catalog.Repositories = %#v, want [library/alpine]", catalog.Repositories)
	}

	tagsReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/tags/list", nil)
	tagsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tagsRecorder, tagsReq)
	if tagsRecorder.Code != http.StatusOK {
		t.Fatalf("tags status = %d, want %d", tagsRecorder.Code, http.StatusOK)
	}
}

// TestRouterManifestScanStatusReturnsAllFiveStates covers design.md Decision
// 5's five distinct scan-status states, table-driven, asserting the exact
// response shape: `state`, `would_block_pull`, `policy`, and `scan` (omitted
// when unscanned).
func TestRouterManifestScanStatusReturnsAllFiveStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		seedRun         func(t *testing.T, store *metadata.Store, digest string)
		wantState       string
		wantBlocked     bool
		wantScanPresent bool
	}{
		{name: "unscanned", seedRun: func(t *testing.T, store *metadata.Store, digest string) {}, wantState: "unscanned", wantBlocked: false, wantScanPresent: false},
		{name: "queued is in_progress", seedRun: func(t *testing.T, store *metadata.Store, digest string) {
			seedRouterScanRun(t, store, digest, ports.ScanRunStatusQueued, 0, 0)
		}, wantState: "in_progress", wantBlocked: false, wantScanPresent: true},
		{name: "running is in_progress", seedRun: func(t *testing.T, store *metadata.Store, digest string) {
			seedRouterScanRun(t, store, digest, ports.ScanRunStatusRunning, 0, 0)
		}, wantState: "in_progress", wantBlocked: false, wantScanPresent: true},
		{name: "failed", seedRun: func(t *testing.T, store *metadata.Store, digest string) {
			seedRouterScanRun(t, store, digest, ports.ScanRunStatusFailed, 0, 0)
		}, wantState: "failed", wantBlocked: false, wantScanPresent: true},
		{name: "completed non-violating is clean", seedRun: func(t *testing.T, store *metadata.Store, digest string) {
			seedRouterScanRun(t, store, digest, ports.ScanRunStatusCompleted, 0, 0)
		}, wantState: "clean", wantBlocked: false, wantScanPresent: true},
		{name: "completed violating is blocked", seedRun: func(t *testing.T, store *metadata.Store, digest string) {
			seedRouterScanRun(t, store, digest, ports.ScanRunStatusCompleted, 3, 0)
		}, wantState: "blocked", wantBlocked: true, wantScanPresent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blobStore, store, cleanup := newTestStores(t)
			router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
			defer drainingCleanup(router, cleanup)()

			seedPublishedManifest(t, router)
			digest := manifestDigestFor(t, router, "library/alpine", "latest")
			tt.seedRun(t, store, digest)

			req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest/scan-status", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
			}

			var payload struct {
				Repository     string         `json:"repository"`
				Reference      string         `json:"reference"`
				Digest         string         `json:"digest"`
				State          string         `json:"state"`
				WouldBlockPull bool           `json:"would_block_pull"`
				Policy         map[string]any `json:"policy"`
				Scan           map[string]any `json:"scan"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response error = %v, body = %s", err, recorder.Body.String())
			}
			if payload.Repository != "library/alpine" || payload.Reference != "latest" || payload.Digest != digest {
				t.Fatalf("payload = %#v, want repository/reference/digest to match the published manifest", payload)
			}
			if payload.State != tt.wantState {
				t.Fatalf("payload.State = %q, want %q", payload.State, tt.wantState)
			}
			if payload.WouldBlockPull != tt.wantBlocked {
				t.Fatalf("payload.WouldBlockPull = %v, want %v", payload.WouldBlockPull, tt.wantBlocked)
			}
			if payload.Policy == nil {
				t.Fatal("payload.Policy = nil, want policy always present")
			}
			scanPresent := payload.Scan != nil
			if scanPresent != tt.wantScanPresent {
				t.Fatalf("scan field present = %v, want %v (body = %s)", scanPresent, tt.wantScanPresent, recorder.Body.String())
			}
		})
	}
}

// TestRouterManifestScanStatusRouteCollisionWithTagNamedScanStatus is the
// design.md Decision 5 route-dispatch regression guard: a tag literally
// named "scan-status" must still route to handleManifest, not
// handleManifestScanStatus, because the suffix match is checked against the
// trimmed reference, not against the raw path suffix.
func TestRouterManifestScanStatusRouteCollisionWithTagNamedScanStatus(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/scan-status", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("push manifest with tag \"scan-status\" status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}
	manifestDigest := manifestRecorder.Header().Get("Docker-Content-Digest")
	if manifestDigest == "" {
		t.Fatal("push manifest response missing Docker-Content-Digest")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/scan-status", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if getRecorder.Header().Get("Docker-Content-Digest") != manifestDigest {
		t.Fatalf("Docker-Content-Digest = %q, want %q (proves handleManifest served this, not handleManifestScanStatus)", getRecorder.Header().Get("Docker-Content-Digest"), manifestDigest)
	}
	if strings.Contains(getRecorder.Body.String(), `"would_block_pull"`) {
		t.Fatalf("body = %q, want the raw manifest payload, not the scan-status JSON shape", getRecorder.Body.String())
	}
}

// TestRouterManifestScanStatusRequiresPullAuthorization covers design.md
// Decision 5's auth scope: a pull-only credential succeeds, no credential
// 401s, and a credential scoped to a different repository is refused.
func TestRouterManifestScanStatusRequiresPullAuthorization(t *testing.T) {
	t.Parallel()

	accessController := ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"})

	t.Run("pull-only credential succeeds", func(t *testing.T) {
		t.Parallel()

		blobStore, store, digest, cleanup := seedScanStatusFixture(t)
		defer cleanup()
		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{
			verify: &domainauth.Principal{
				Subject:  "atk_1",
				Username: "ci",
				Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("library/alpine"), Role: domainauth.RepoRoleReader}},
				Scopes:   []domainauth.Scope{{Type: "repository", Name: "library/alpine", Actions: []string{"pull"}, Canonical: "repository:library/alpine:pull"}},
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/scan-status", nil)
		req.Header.Set("Authorization", "Bearer pull-only-token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
	})

	t.Run("no credential is rejected", func(t *testing.T) {
		t.Parallel()

		blobStore, store, digest, cleanup := seedScanStatusFixture(t)
		defer cleanup()
		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{})

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/scan-status", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})

	t.Run("credential scoped to a different repository is refused", func(t *testing.T) {
		t.Parallel()

		blobStore, store, digest, cleanup := seedScanStatusFixture(t)
		defer cleanup()
		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{
			verify: &domainauth.Principal{
				Subject:  "atk_1",
				Username: "ci",
				Scopes:   []domainauth.Scope{{Type: "repository", Name: "team/other", Actions: []string{"pull"}, Canonical: "repository:team/other:pull"}},
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/scan-status", nil)
		req.Header.Set("Authorization", "Bearer other-repo-token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})
}

// seedScanStatusFixture publishes library/alpine:latest through a
// permissive router (allowAllAccessController, no auth) so the push itself
// is never gated by the restrictive credentials each subtest exercises
// afterward, then returns the same underlying stores for a second, auth-
// restricted router to be built on top of.
func seedScanStatusFixture(t *testing.T) (*fsblob.Store, *metadata.Store, string, func()) {
	t.Helper()

	blobStore, store, cleanup := newTestStores(t)
	seedRouter := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
	seedPublishedManifest(t, seedRouter)
	digest := manifestDigestFor(t, seedRouter, "library/alpine", "latest")
	seedRouter.service.WaitForBackgroundWork()

	return blobStore, store, digest, cleanup
}

// manifestDigestFor reads back the manifest digest via a plain GET, since
// seedPublishedManifest returns the blob digest (used for its own blob
// commit/config/layer references), which is a different value from the
// manifest digest computed over the manifest JSON payload itself.
func manifestDigestFor(t *testing.T, router *Router, repository string, reference string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+reference, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	digest := recorder.Header().Get("Docker-Content-Digest")
	if digest == "" {
		t.Fatalf("GET manifest %s/%s missing Docker-Content-Digest, status = %d", repository, reference, recorder.Code)
	}
	return digest
}

func seedRouterScanRun(t *testing.T, store *metadata.Store, digest string, status string, critical int, high int) {
	t.Helper()

	now := time.Now().UTC()
	run := ports.ScanRun{ID: "run-" + status + "-" + digest, Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: status, Trigger: ports.ScanTriggerManual, CreatedAt: now, UpdatedAt: now, Critical: critical, High: high}
	if err := store.UpsertScanRun(context.Background(), "tenant-a", run); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}
}

func TestRouterRejectsDigestMismatchOnBlobCommit(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	startReq := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startReq)
	uploadLocation := startRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	wrongDigest := domain.DigestFromBytes([]byte("different-payload")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+wrongDigest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	if commitRecorder.Code != http.StatusBadRequest {
		t.Fatalf("commit status = %d, want %d", commitRecorder.Code, http.StatusBadRequest)
	}

	blobReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/blobs/"+wrongDigest, nil)
	blobRecorder := httptest.NewRecorder()
	handler.ServeHTTP(blobRecorder, blobReq)
	if blobRecorder.Code != http.StatusNotFound {
		t.Fatalf("blob status = %d, want %d", blobRecorder.Code, http.StatusNotFound)
	}
}

func TestRouterAllowsAnonymousPullWhenConfigured(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()

	seedHandler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, nil)
	digest := seedPublishedManifest(t, seedHandler)
	readOnlyHandler := newRouterWithStores(blobStore, metadataStore, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPull: true}), nil)

	manifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	manifestRecorder := httptest.NewRecorder()
	readOnlyHandler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusOK)
	}

	blobReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/blobs/"+digest, nil)
	blobRecorder := httptest.NewRecorder()
	readOnlyHandler.ServeHTTP(blobRecorder, blobReq)
	if blobRecorder.Code != http.StatusOK {
		t.Fatalf("blob status = %d, want %d", blobRecorder.Code, http.StatusOK)
	}
}

func TestRouterRejectsAnonymousPushByDefault(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) || !strings.Contains(challenge, `scope="repository:library/alpine:pull,push"`) {
		t.Fatalf("WWW-Authenticate = %q, want configured token realm URL with combined push scope", challenge)
	}
}

func TestRouterChallengesUnauthenticatedV2PingWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) || !strings.Contains(challenge, `service="regixtry"`) {
		t.Fatalf("WWW-Authenticate = %q, want bearer challenge for /v2/ ping", challenge)
	}
	if strings.Contains(challenge, `scope=`) {
		t.Fatalf("WWW-Authenticate = %q, did not expect scope on /v2/ ping challenge", challenge)
	}
}

func TestRouterAcceptsAuthenticatedV2PingWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), fakeAuthService{
		verify: &domainauth.Principal{Subject: "user-1", Username: "alice"},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want empty on authenticated ping", got)
	}
}

func TestRouterAdminBoundaryRejectsMissingBearerToken(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `realm="http://127.0.0.1:5000/auth/token"`) {
		t.Fatalf("WWW-Authenticate = %q, want bearer realm", got)
	}
}

func TestRouterAdminBoundaryRejectsNonAdminPrincipal(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{
		verify: &domainauth.Principal{Subject: "atk_1", UserID: "user-1", Username: "alice", IsAdmin: false},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestRouterListsAdminUsersWithoutPasswordHashes(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var users []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &users); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if _, ok := users[0]["password_hash"]; ok {
		t.Fatalf("users[0] = %#v, did not expect password_hash", users[0])
	}
	if _, ok := users[1]["password_hash"]; ok {
		t.Fatalf("users[1] = %#v, did not expect password_hash", users[1])
	}
	if strings.Contains(recorder.Body.String(), "password123") {
		t.Fatalf("body = %q, did not expect plaintext password", recorder.Body.String())
	}
	if users[0]["username"] != "admin" || users[1]["username"] != "bob" {
		t.Fatalf("users = %#v, want usernames [admin bob]", users)
	}
	if users[1]["enabled"] != true {
		t.Fatalf("users[1].enabled = %#v, want true", users[1]["enabled"])
	}
	if users[1]["is_admin"] != false {
		t.Fatalf("users[1].is_admin = %#v, want false", users[1]["is_admin"])
	}
}

func TestRouterAdminUserRoutesSupportCreateEnableDisableAndResetPassword(t *testing.T) {
	t.Parallel()

	handler, authService, _, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	createReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users", strings.NewReader(`{"username":"bob","password":"password123","enabled":false,"is_admin":false}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createReq)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRecorder.Code, http.StatusCreated)
	}

	var created ports.AdminUser
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	if created.Username != "bob" || created.Enabled {
		t.Fatalf("created = %#v, want disabled bob user", created)
	}

	enableReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+created.ID+":enable", nil)
	enableReq.Header.Set("Authorization", "Bearer "+adminToken)
	enableRecorder := httptest.NewRecorder()
	handler.ServeHTTP(enableRecorder, enableReq)

	if enableRecorder.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want %d", enableRecorder.Code, http.StatusOK)
	}

	var enabled ports.AdminUser
	if err := json.Unmarshal(enableRecorder.Body.Bytes(), &enabled); err != nil {
		t.Fatalf("json.Unmarshal(enable) error = %v", err)
	}
	if !enabled.Enabled {
		t.Fatalf("enabled = %#v, want enabled=true", enabled)
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+created.ID+":reset-password", strings.NewReader(`{"new_password":"password456"}`))
	resetReq.Header.Set("Authorization", "Bearer "+adminToken)
	resetReq.Header.Set("Content-Type", "application/json")
	resetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(resetRecorder, resetReq)

	if resetRecorder.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d, want %d", resetRecorder.Code, http.StatusNoContent)
	}

	if _, err := authService.LoginWithPassword(context.Background(), "bob", "password123", nil); !domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
		t.Fatalf("LoginWithPassword(old password) error = %v, want invalid credentials", err)
	}
	if _, err := authService.LoginWithPassword(context.Background(), "bob", "password456", nil); err != nil {
		t.Fatalf("LoginWithPassword(new password) error = %v", err)
	}

	disableReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+created.ID+":disable", nil)
	disableReq.Header.Set("Authorization", "Bearer "+adminToken)
	disableRecorder := httptest.NewRecorder()
	handler.ServeHTTP(disableRecorder, disableReq)

	if disableRecorder.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d", disableRecorder.Code, http.StatusOK)
	}

	var disabled ports.AdminUser
	if err := json.Unmarshal(disableRecorder.Body.Bytes(), &disabled); err != nil {
		t.Fatalf("json.Unmarshal(disable) error = %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("disabled = %#v, want enabled=false", disabled)
	}
}

func TestRouterAdminCreateUserRejectsUsernameConflicts(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	if _, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateAdminUser() setup error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/users", strings.NewReader(`{"username":"bob","password":"password123","enabled":true,"is_admin":false}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestRouterAdminDisableRejectsLastActiveAdmin(t *testing.T) {
	t.Parallel()

	handler, _, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+adminActor.UserID+":disable", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestRouterAdminResetPasswordRejectsWeakPasswords(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	user, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+user.ID+":reset-password", strings.NewReader(`{"new_password":"short"}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestRouterAdminUserMutationRoutesRemainUnavailable(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	user, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "delete user route", method: http.MethodDelete, path: "/admin/v1/users/" + user.ID},
		{name: "broad profile update route", method: http.MethodPut, path: "/admin/v1/users/" + user.ID, body: `{"username":"robert","enabled":false}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
		})
	}
}

func TestRouterAdminGrantRoutesSupportListPutReplaceAndDelete(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	user, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/users/"+user.ID+"/grants/team/app", strings.NewReader(`{"role":"repo-reader"}`))
	putReq.Header.Set("Authorization", "Bearer "+adminToken)
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)

	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d", putRecorder.Code, http.StatusOK)
	}

	var createdGrant ports.AdminRepoGrant
	if err := json.Unmarshal(putRecorder.Body.Bytes(), &createdGrant); err != nil {
		t.Fatalf("json.Unmarshal(put) error = %v", err)
	}
	if createdGrant.Repository.String() != "team/app" || createdGrant.Role != domainauth.RepoRoleReader {
		t.Fatalf("createdGrant = %#v, want team/app repo-reader", createdGrant)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/users/"+user.ID+"/grants", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}

	var grants []ports.AdminRepoGrant
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &grants); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("len(grants) = %d, want 1", len(grants))
	}
	if grants[0].Repository.String() != "team/app" || grants[0].Role != domainauth.RepoRoleReader {
		t.Fatalf("grants[0] = %#v, want repository team/app and role repo-reader", grants[0])
	}

	replaceReq := httptest.NewRequest(http.MethodPut, "/admin/v1/users/"+user.ID+"/grants/team/app", strings.NewReader(`{"role":"repo-admin"}`))
	replaceReq.Header.Set("Authorization", "Bearer "+adminToken)
	replaceReq.Header.Set("Content-Type", "application/json")
	replaceRecorder := httptest.NewRecorder()
	handler.ServeHTTP(replaceRecorder, replaceReq)

	if replaceRecorder.Code != http.StatusOK {
		t.Fatalf("replace status = %d, want %d", replaceRecorder.Code, http.StatusOK)
	}

	var replacedGrant ports.AdminRepoGrant
	if err := json.Unmarshal(replaceRecorder.Body.Bytes(), &replacedGrant); err != nil {
		t.Fatalf("json.Unmarshal(replace) error = %v", err)
	}
	if replacedGrant.Role != domainauth.RepoRoleAdmin {
		t.Fatalf("replacedGrant.Role = %q, want %q", replacedGrant.Role, domainauth.RepoRoleAdmin)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/users/"+user.ID+"/grants/team/app", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteReq)

	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", deleteRecorder.Code, http.StatusNoContent)
	}

	listAfterDeleteReq := httptest.NewRequest(http.MethodGet, "/admin/v1/users/"+user.ID+"/grants", nil)
	listAfterDeleteReq.Header.Set("Authorization", "Bearer "+adminToken)
	listAfterDeleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listAfterDeleteRecorder, listAfterDeleteReq)

	if listAfterDeleteRecorder.Code != http.StatusOK {
		t.Fatalf("list after delete status = %d, want %d", listAfterDeleteRecorder.Code, http.StatusOK)
	}
	if strings.TrimSpace(listAfterDeleteRecorder.Body.String()) != "[]" {
		t.Fatalf("body after delete = %q, want []", listAfterDeleteRecorder.Body.String())
	}
}

func TestRouterAdminGrantRoutesRejectInvalidInput(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	user, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	tests := []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{name: "invalid role", path: "/admin/v1/users/" + user.ID + "/grants/team/app", body: `{"role":"owner"}`, status: http.StatusUnprocessableEntity},
		{name: "invalid repository", path: "/admin/v1/users/" + user.ID + "/grants/Team/App", body: `{"role":"repo-reader"}`, status: http.StatusUnprocessableEntity},
		{name: "missing user", path: "/admin/v1/users/missing-user/grants/team/app", body: `{"role":"repo-reader"}`, status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
		})
	}
}

func TestRouterAdminTokenRoutesSupportListCreateAndScopedRevoke(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	user, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "bob",
		Password: "password123",
		Enabled:  true,
		IsAdmin:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+user.ID+"/admin-tokens", strings.NewReader(`{"name":"ci","ttl_seconds":3600}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createReq)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRecorder.Code, http.StatusCreated)
	}
	if strings.Contains(createRecorder.Body.String(), "secret_hash") {
		t.Fatalf("body = %q, did not expect secret hash", createRecorder.Body.String())
	}

	var createdToken map[string]any
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createdToken); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	secret, _ := createdToken["secret"].(string)
	if secret == "" {
		t.Fatalf("createdToken = %#v, want one-time secret", createdToken)
	}
	tokenPayload, ok := createdToken["token"].(map[string]any)
	if !ok {
		t.Fatalf("createdToken.token = %#v, want object", createdToken["token"])
	}
	accessor, _ := tokenPayload["accessor"].(string)
	if accessor == "" {
		t.Fatalf("tokenPayload = %#v, want accessor", tokenPayload)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/users/"+user.ID+"/admin-tokens", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}
	if strings.Contains(listRecorder.Body.String(), secret) {
		t.Fatalf("list body = %q, did not expect plaintext secret", listRecorder.Body.String())
	}

	var listed []ports.AdminToken
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if listed[0].Accessor != accessor {
		t.Fatalf("listed[0].Accessor = %q, want %q", listed[0].Accessor, accessor)
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/users/"+user.ID+"/admin-tokens/"+accessor, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+adminToken)
	revokeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(revokeRecorder, revokeReq)

	if revokeRecorder.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want %d", revokeRecorder.Code, http.StatusNoContent)
	}

	if _, err := authService.LoginWithPreissuedToken(context.Background(), user.Username, secret, nil); !domainauth.IsCode(err, domainauth.ErrorCodeRevokedToken) {
		t.Fatalf("LoginWithPreissuedToken(revoked) error = %v, want revoked token", err)
	}
}

func TestRouterAdminTokenRoutesRejectExcessiveTTLAndMismatchedRevoke(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	owner, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "owner",
		Password: "password123",
		Enabled:  true,
		IsAdmin:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(owner) error = %v", err)
	}
	other, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "other",
		Password: "password123",
		Enabled:  true,
		IsAdmin:  true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(other) error = %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+owner.ID+"/admin-tokens", strings.NewReader(`{"name":"ci","ttl_seconds":2592001}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createReq.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createReq)

	if createRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create status = %d, want %d", createRecorder.Code, http.StatusUnprocessableEntity)
	}

	seedReq := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+owner.ID+"/admin-tokens", strings.NewReader(`{"name":"seed","ttl_seconds":3600}`))
	seedReq.Header.Set("Authorization", "Bearer "+adminToken)
	seedReq.Header.Set("Content-Type", "application/json")
	seedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(seedRecorder, seedReq)

	if seedRecorder.Code != http.StatusCreated {
		t.Fatalf("seed create status = %d, want %d", seedRecorder.Code, http.StatusCreated)
	}

	var seeded map[string]any
	if err := json.Unmarshal(seedRecorder.Body.Bytes(), &seeded); err != nil {
		t.Fatalf("json.Unmarshal(seed) error = %v", err)
	}
	tokenPayload, ok := seeded["token"].(map[string]any)
	if !ok {
		t.Fatalf("seeded.token = %#v, want object", seeded["token"])
	}
	accessor, _ := tokenPayload["accessor"].(string)
	if accessor == "" {
		t.Fatalf("tokenPayload = %#v, want accessor", tokenPayload)
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/admin/v1/users/"+other.ID+"/admin-tokens/"+accessor, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+adminToken)
	revokeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(revokeRecorder, revokeReq)

	if revokeRecorder.Code != http.StatusNotFound {
		t.Fatalf("revoke status = %d, want %d", revokeRecorder.Code, http.StatusNotFound)
	}
}

func TestRouterAdminScanSettingsRoutesRequireAuthAndPersistUpdates(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	unauthReq := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-settings", nil)
	unauthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthRecorder, unauthReq)
	if unauthRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want %d", unauthRecorder.Code, http.StatusUnauthorized)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-settings", nil)
	getReq.Header.Set("Authorization", "Bearer admin-token")
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(getRecorder.Body.String(), `"enabled":false`) {
		t.Fatalf("body = %q, want disabled defaults", getRecorder.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-settings", strings.NewReader(`{"enabled":true,"schedule_enabled":true,"interval":"6h","timeout":"20m","service_url":"https://scanner.example.com","registry_reachable_url":"https://registry.internal:5443","max_concurrency":2}`))
	putReq.Header.Set("Authorization", "Bearer admin-token")
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d", putRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(putRecorder.Body.String(), `"schedule_enabled":true`) || !strings.Contains(putRecorder.Body.String(), `"max_concurrency":2`) {
		t.Fatalf("body = %q, want persisted scan settings", putRecorder.Body.String())
	}
}

func TestRouterAdminScanRoutesQueueAndListRuns(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()
	seedPublishedManifest(t, handler)

	putReq := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-settings", strings.NewReader(`{"enabled":true,"schedule_enabled":false,"interval":"24h","timeout":"15m","service_url":"https://scanner.example.com","registry_reachable_url":"https://registry.internal:5443","max_concurrency":1}`))
	putReq.Header.Set("Authorization", "Bearer admin-token")
	putReq.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putReq)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("settings status = %d, want %d", putRecorder.Code, http.StatusOK)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/scan-runs", strings.NewReader(`{"repository":"library/alpine","reference":"latest"}`))
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postReq.Header.Set("Content-Type", "application/json")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusAccepted {
		t.Fatalf("post status = %d, want %d", postRecorder.Code, http.StatusAccepted)
	}
	if !strings.Contains(postRecorder.Body.String(), `"digest":"sha256:`) {
		t.Fatalf("body = %q, want canonical digest", postRecorder.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-runs?repository=library/alpine&limit=5", nil)
	listReq.Header.Set("Authorization", "Bearer admin-token")
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(listRecorder.Body.String(), `"repository":"library/alpine"`) {
		t.Fatalf("body = %q, want scan run history", listRecorder.Body.String())
	}
}

func TestRouterAdminScanRoutesRejectInvalidTargetsAndSettings(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	settingsReq := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-settings", strings.NewReader(`{"enabled":true,"schedule_enabled":true,"interval":"0s","timeout":"0s","service_url":"ftp://scanner.example.com","registry_reachable_url":"http://127.0.0.1:5000","max_concurrency":0}`))
	settingsReq.Header.Set("Authorization", "Bearer admin-token")
	settingsReq.Header.Set("Content-Type", "application/json")
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsReq)
	if settingsRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("settings status = %d, want %d", settingsRecorder.Code, http.StatusUnprocessableEntity)
	}

	validSettingsReq := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-settings", strings.NewReader(`{"enabled":true,"schedule_enabled":false,"interval":"24h","timeout":"15m","service_url":"https://scanner.example.com","registry_reachable_url":"https://registry.internal:5443","max_concurrency":1}`))
	validSettingsReq.Header.Set("Authorization", "Bearer admin-token")
	validSettingsReq.Header.Set("Content-Type", "application/json")
	validSettingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(validSettingsRecorder, validSettingsReq)
	if validSettingsRecorder.Code != http.StatusOK {
		t.Fatalf("valid settings status = %d, want %d", validSettingsRecorder.Code, http.StatusOK)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/scan-runs", strings.NewReader(`{"repository":"library/alpine","reference":"missing"}`))
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postReq.Header.Set("Content-Type", "application/json")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusNotFound {
		t.Fatalf("post status = %d, want %d", postRecorder.Code, http.StatusNotFound)
	}
}

// TestRouterAdminSecretScanFindingsRouteReturnsRedactedFindingsByImage wires
// the secret-findings-by-image endpoint through the full Router (tasks.md
// 6.6): the "/admin/v1/" prefix mux registration already routes any admin
// subpath to handleAdmin, so this test proves the new route resolves
// end-to-end (not just via the admin_handlers_test.go direct-handler path)
// and rejects non-GET methods and missing required query parameters.
func TestRouterAdminSecretScanFindingsRouteReturnsRedactedFindingsByImage(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()
	handler := newRouterWithStores(blobStore, metadataStore, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})

	digest := "sha256:" + strings.Repeat("d", 64)
	if err := metadataStore.UpsertSecretScanRunDetail(context.Background(), "tenant-a", ports.SecretScanRunDetail{
		Run: ports.SecretScanRun{
			ID:         "secret-run-router-1",
			Repository: "library/alpine",
			Digest:     digest,
			Status:     ports.SecretScanRunStatusCompleted,
			Trigger:    ports.ScanTriggerManual,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		},
		Findings: []ports.SecretFinding{{RuleID: "generic-api-key", Path: "layers/000.tar.gz", StartLine: 4, EndLine: 4}},
	}); err != nil {
		t.Fatalf("UpsertSecretScanRunDetail() error = %v", err)
	}

	okReq := httptest.NewRequest(http.MethodGet, "/admin/v1/secret-scan-findings?repository=library/alpine&digest="+digest, nil)
	okReq.Header.Set("Authorization", "Bearer admin-token")
	okRecorder := httptest.NewRecorder()
	handler.ServeHTTP(okRecorder, okReq)
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", okRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(okRecorder.Body.String(), `"rule_id":"generic-api-key"`) {
		t.Fatalf("body = %q, want the persisted redacted finding", okRecorder.Body.String())
	}

	missingParamsReq := httptest.NewRequest(http.MethodGet, "/admin/v1/secret-scan-findings", nil)
	missingParamsReq.Header.Set("Authorization", "Bearer admin-token")
	missingParamsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingParamsRecorder, missingParamsReq)
	if missingParamsRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-params status = %d, want %d", missingParamsRecorder.Code, http.StatusUnprocessableEntity)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/secret-scan-findings?repository=library/alpine&digest="+digest, nil)
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status = %d, want %d", postRecorder.Code, http.StatusMethodNotAllowed)
	}

	notFoundReq := httptest.NewRequest(http.MethodGet, "/admin/v1/secret-scan-findings?repository=library/other&digest="+digest, nil)
	notFoundReq.Header.Set("Authorization", "Bearer admin-token")
	notFoundRecorder := httptest.NewRecorder()
	handler.ServeHTTP(notFoundRecorder, notFoundReq)
	if notFoundRecorder.Code != http.StatusNotFound {
		t.Fatalf("not-found status = %d, want %d", notFoundRecorder.Code, http.StatusNotFound)
	}
}

func TestRouterAdminFeatureRoutesProjectBuiltinTrivyState(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	configureReq := httptest.NewRequest(http.MethodPut, "/admin/v1/features/trivy/config", strings.NewReader(`{"enabled":true,"schedule_enabled":true,"interval":"6h","timeout":"20m","service_url":"https://scanner.example.com","registry_reachable_url":"https://registry.internal:5443","max_concurrency":2}`))
	configureReq.Header.Set("Authorization", "Bearer admin-token")
	configureReq.Header.Set("Content-Type", "application/json")
	configureRecorder := httptest.NewRecorder()
	handler.ServeHTTP(configureRecorder, configureReq)
	if configureRecorder.Code != http.StatusOK {
		t.Fatalf("configure status = %d, want %d", configureRecorder.Code, http.StatusOK)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features", nil)
	listReq.Header.Set("Authorization", "Bearer admin-token")
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(listRecorder.Body.String(), `"name":"trivy"`) {
		t.Fatalf("list body = %q, want builtin trivy feature", listRecorder.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/status", nil)
	statusReq.Header.Set("Authorization", "Bearer admin-token")
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusRecorder.Code, http.StatusOK)
	}
	for _, want := range []string{`"enabled":true`, `"schedule_enabled":true`, `"name":"trivy"`} {
		if !strings.Contains(statusRecorder.Body.String(), want) {
			t.Fatalf("status body = %q, want %q", statusRecorder.Body.String(), want)
		}
	}
}

func TestRouterAdminFeatureRoutesRejectUnknownNames(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/future-plugin", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestRouterAdminFeatureRoutesRequireAuthAndMutateAuthoritativeState(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	unauthReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/status", nil)
	unauthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthRecorder, unauthReq)
	if unauthRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want %d", unauthRecorder.Code, http.StatusUnauthorized)
	}

	seedReq := httptest.NewRequest(http.MethodPut, "/admin/v1/scan-settings", strings.NewReader(`{"enabled":false,"schedule_enabled":false,"interval":"24h","timeout":"15m","service_url":"https://scanner.example.com","registry_reachable_url":"https://registry.internal:5443","max_concurrency":1}`))
	seedReq.Header.Set("Authorization", "Bearer admin-token")
	seedReq.Header.Set("Content-Type", "application/json")
	seedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(seedRecorder, seedReq)
	if seedRecorder.Code != http.StatusOK {
		t.Fatalf("seed status = %d, want %d", seedRecorder.Code, http.StatusOK)
	}

	showReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy", nil)
	showReq.Header.Set("Authorization", "Bearer admin-token")
	showRecorder := httptest.NewRecorder()
	handler.ServeHTTP(showRecorder, showReq)
	if showRecorder.Code != http.StatusOK {
		t.Fatalf("show status = %d, want %d", showRecorder.Code, http.StatusOK)
	}
	for _, want := range []string{`"name":"trivy"`, `"enabled":false`, `"configured":true`} {
		if !strings.Contains(showRecorder.Body.String(), want) {
			t.Fatalf("show body = %q, want %q", showRecorder.Body.String(), want)
		}
	}

	enableReq := httptest.NewRequest(http.MethodPost, "/admin/v1/features/trivy:enable", nil)
	enableReq.Header.Set("Authorization", "Bearer admin-token")
	enableRecorder := httptest.NewRecorder()
	handler.ServeHTTP(enableRecorder, enableReq)
	if enableRecorder.Code != http.StatusOK {
		t.Fatalf("enable status = %d, want %d", enableRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(enableRecorder.Body.String(), `"enabled":true`) {
		t.Fatalf("enable body = %q, want enabled=true", enableRecorder.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy/status", nil)
	statusReq.Header.Set("Authorization", "Bearer admin-token")
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", statusRecorder.Code, http.StatusOK)
	}
	for _, want := range []string{`"enabled":true`, `"runtime":`, `"name":"trivy"`} {
		if !strings.Contains(statusRecorder.Body.String(), want) {
			t.Fatalf("status body = %q, want %q", statusRecorder.Body.String(), want)
		}
	}

	disableReq := httptest.NewRequest(http.MethodPost, "/admin/v1/features/trivy:disable", nil)
	disableReq.Header.Set("Authorization", "Bearer admin-token")
	disableRecorder := httptest.NewRecorder()
	handler.ServeHTTP(disableRecorder, disableReq)
	if disableRecorder.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d", disableRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(disableRecorder.Body.String(), `"enabled":false`) {
		t.Fatalf("disable body = %q, want enabled=false", disableRecorder.Body.String())
	}

	scanReq := httptest.NewRequest(http.MethodGet, "/admin/v1/scan-settings", nil)
	scanReq.Header.Set("Authorization", "Bearer admin-token")
	scanRecorder := httptest.NewRecorder()
	handler.ServeHTTP(scanRecorder, scanReq)
	if scanRecorder.Code != http.StatusOK {
		t.Fatalf("scan-settings status = %d, want %d", scanRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(scanRecorder.Body.String(), `"enabled":false`) {
		t.Fatalf("scan-settings body = %q, want authoritative disabled state", scanRecorder.Body.String())
	}
}

func TestRouterAdminFeaturePageRouteProjectsBackendDeclaredSectionsAndActions(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	configureReq := httptest.NewRequest(http.MethodPut, "/admin/v1/features/trivy/config", strings.NewReader(`{"enabled":true,"schedule_enabled":true,"interval":"6h","timeout":"20m","registry_reachable_url":"https://registry.internal:5443","max_concurrency":2}`))
	configureReq.Header.Set("Authorization", "Bearer admin-token")
	configureReq.Header.Set("Content-Type", "application/json")
	configureRecorder := httptest.NewRecorder()
	handler.ServeHTTP(configureRecorder, configureReq)
	if configureRecorder.Code != http.StatusOK {
		t.Fatalf("configure status = %d, want %d", configureRecorder.Code, http.StatusOK)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/features/trivy", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	for _, want := range []string{`"summary":`, `"sections":`, `"actions":`, `"id":"config"`, `"id":"runtime"`, `"id":"refresh"`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
		}
	}
}

func TestRouterAdminFeatureActionRouteExecutesTypedActionAndRejectsUnknownTargets(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, allowAllAccessController{}, fakeAuthService{verify: &domainauth.Principal{Subject: "atk_1", UserID: "admin-1", Username: "admin", IsAdmin: true}})
	defer cleanup()

	postReq := httptest.NewRequest(http.MethodPost, "/admin/v1/features/trivy/actions/disable", nil)
	postReq.Header.Set("Authorization", "Bearer admin-token")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, postReq)
	if postRecorder.Code != http.StatusOK {
		t.Fatalf("action status = %d, want %d", postRecorder.Code, http.StatusOK)
	}
	if !strings.Contains(postRecorder.Body.String(), `"message":"Feature \"trivy\" disabled."`) {
		t.Fatalf("action body = %q, want authoritative action message", postRecorder.Body.String())
	}

	unknownFeatureReq := httptest.NewRequest(http.MethodGet, "/admin/v1/features/future-plugin", nil)
	unknownFeatureReq.Header.Set("Authorization", "Bearer admin-token")
	unknownFeatureRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unknownFeatureRecorder, unknownFeatureReq)
	if unknownFeatureRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown feature status = %d, want %d", unknownFeatureRecorder.Code, http.StatusUnprocessableEntity)
	}

	unknownActionReq := httptest.NewRequest(http.MethodPost, "/admin/v1/features/trivy/actions/reindex", nil)
	unknownActionReq.Header.Set("Authorization", "Bearer admin-token")
	unknownActionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unknownActionRecorder, unknownActionReq)
	if unknownActionRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown action status = %d, want %d", unknownActionRecorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestRouterKeepsV2PingOpenWhenAuthDisabled(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want empty when auth is disabled", got)
	}
}

func TestRouterLogsUnauthorizedChallengeDetails(t *testing.T) {
	var logs bytes.Buffer
	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), fakeAuthService{}, WithLogger(log.New(&logs, "", 0)))
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	output := logs.String()
	if !strings.Contains(output, "method=POST") || !strings.Contains(output, "path=/v2/library/alpine/blobs/uploads/") || !strings.Contains(output, "status=401") {
		t.Fatalf("log output = %q, want method/path/status details", output)
	}
	if !strings.Contains(output, `auth_challenge="Bearer realm=\"http://127.0.0.1:5000/auth/token\"`) || !strings.Contains(output, `scope=\"repository:library/alpine:pull,push\"`) {
		t.Fatalf("log output = %q, want challenge hints", output)
	}
	if !strings.Contains(output, "duration=") {
		t.Fatalf("log output = %q, want duration", output)
	}
}

func TestRouterLogsRequestPathWithQueryString(t *testing.T) {
	var logs bytes.Buffer
	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{
		loginResult: ports.LoginResult{BearerToken: "issued-token", ExpiresAt: time.Date(2026, 1, 2, 3, 19, 5, 0, time.UTC)},
	}, WithLogger(log.New(&logs, "", 0)))
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=registry&scope=repository:team/app:pull", nil)
	req.SetBasicAuth("alice", "password123")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	output := logs.String()
	if !strings.Contains(output, "path=/auth/token?service=registry&scope=repository:team/app:pull") || !strings.Contains(output, "status=200") {
		t.Fatalf("log output = %q, want request URI and status", output)
	}
	if strings.Contains(output, "auth_challenge=") {
		t.Fatalf("log output = %q, did not expect auth challenge on success", output)
	}
}

func TestRouterIssuesAccessTokenFromBasicCredentials(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{
		loginResult: ports.LoginResult{BearerToken: "issued-token", ExpiresAt: time.Date(2026, 1, 2, 3, 19, 5, 0, time.UTC), Scope: "repository:team/app:pull"},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=registry&scope=repository:team/app:pull", nil)
	req.SetBasicAuth("alice", "password123")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["token"] != "issued-token" {
		t.Fatalf("token = %#v, want issued-token", payload["token"])
	}
	if payload["scope"] != "repository:team/app:pull" {
		t.Fatalf("scope = %#v, want repository:team/app:pull", payload["scope"])
	}
}

func TestRouterRejectsPushWhenBearerScopeIsPullOnly(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{
		verify: &domainauth.Principal{
			Subject:  "atk_1",
			Username: "alice",
			Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}},
			Scopes:   []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}},
		},
	})
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/team/app/blobs/uploads/", nil)
	req.Header.Set("Authorization", "Bearer pull-only-token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRouterRejectsMalformedTokenScopeRequests(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/auth/token?service=registry&scope=repository:team/app", nil)
	req.SetBasicAuth("alice", "password123")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRouterChallengesProtectedPullWithConfiguredTokenRealm(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"}), fakeAuthService{})
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/v2/team/app/manifests/latest", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `realm="http://127.0.0.1:5000/auth/token"`) || !strings.Contains(challenge, `scope="repository:team/app:pull"`) {
		t.Fatalf("WWW-Authenticate = %q, want configured token realm URL with pull scope", challenge)
	}
}

func TestRouterRejectsInvalidBearerTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "expired", err: domainauth.NewExpiredTokenError("atk_expired")},
		{name: "revoked", err: domainauth.NewRevokedTokenError("atk_revoked")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, cleanup := newTestRouterWithAuth(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}), fakeAuthService{verifyErr: tt.err})
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, "/v2/team/app/tags/list", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `scope="repository:team/app:pull"`) || !strings.Contains(got, `error="invalid_token"`) {
				t.Fatalf("WWW-Authenticate = %q, want scoped invalid_token challenge", got)
			}
		})
	}
}

func TestRouterAllowsAnonymousPushWhenConfigured(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPush: true}))
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}

func TestRouterRejectsManifestWithMissingBlob(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	digest := "sha256:8a5a3d2cfb08cf0c22848f3322a7fd6f1300a0a176c1f907fdbd53f5b5d2e236"
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)

	if manifestRecorder.Code != http.StatusConflict {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusConflict)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusNotFound {
		t.Fatalf("get manifest status = %d, want %d", getRecorder.Code, http.StatusNotFound)
	}
}

func TestRouterKeepsIncompleteUploadsInvisibleFromPublishedContent(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	startReq := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startReq)
	uploadLocation := startRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	var catalog struct {
		Repositories []string `json:"repositories"`
	}
	catalogReq := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	catalogRecorder := httptest.NewRecorder()
	handler.ServeHTTP(catalogRecorder, catalogReq)

	if catalogRecorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, want %d", catalogRecorder.Code, http.StatusOK)
	}
	if err := json.Unmarshal(catalogRecorder.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("catalog JSON error = %v", err)
	}
	if len(catalog.Repositories) != 0 {
		t.Fatalf("catalog.Repositories = %#v, want empty", catalog.Repositories)
	}

	manifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusNotFound {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusNotFound)
	}
}

// TestRouterReadOnlyPrincipalReadsAnyRepositoryRejectedOnPushAndAdmin pins
// repository-authorization's "Registry-Wide Read-Only Role Grants Read
// Everywhere" requirement end to end: a real login-issued token for a
// read-only user reads tags/manifests on a repository it has no explicit
// grant for, is rejected on push, and is rejected on the admin user listing.
func TestRouterReadOnlyPrincipalReadsAnyRepositoryRejectedOnPushAndAdmin(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	pushScopes, err := domainauth.ParseScopes([]string{"repository:library/alpine:pull,push"})
	if err != nil {
		t.Fatalf("ParseScopes(push) error = %v", err)
	}
	adminPushLogin, err := authService.LoginWithPassword(context.Background(), "admin", "password123", pushScopes)
	if err != nil {
		t.Fatalf("LoginWithPassword(admin push) error = %v", err)
	}
	seedPublishedManifestWithToken(t, handler, adminPushLogin.BearerToken)

	created, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username:   "reader",
		Password:   "password123",
		Enabled:    true,
		IsReadOnly: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}
	if !created.IsReadOnly {
		t.Fatalf("CreateAdminUser() result IsReadOnly = false, want true")
	}

	requestedScopes, err := domainauth.ParseScopes([]string{"repository:library/alpine:pull,push", "regixtry:catalog:*"})
	if err != nil {
		t.Fatalf("ParseScopes() error = %v", err)
	}
	loginResult, err := authService.LoginWithPassword(context.Background(), "reader", "password123", requestedScopes)
	if err != nil {
		t.Fatalf("LoginWithPassword() error = %v", err)
	}
	if !loginResult.Principal.IsReadOnly {
		t.Fatalf("LoginWithPassword() Principal.IsReadOnly = false, want true")
	}
	readOnlyToken := loginResult.BearerToken

	manifestReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/latest", nil)
	manifestReq.Header.Set("Authorization", "Bearer "+readOnlyToken)
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusOK {
		t.Fatalf("manifest read status = %d, want %d", manifestRecorder.Code, http.StatusOK)
	}

	tagsReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/tags/list", nil)
	tagsReq.Header.Set("Authorization", "Bearer "+readOnlyToken)
	tagsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tagsRecorder, tagsReq)
	if tagsRecorder.Code != http.StatusOK {
		t.Fatalf("tags list status = %d, want %d", tagsRecorder.Code, http.StatusOK)
	}

	catalogReq := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	catalogReq.Header.Set("Authorization", "Bearer "+readOnlyToken)
	catalogRecorder := httptest.NewRecorder()
	handler.ServeHTTP(catalogRecorder, catalogReq)
	if catalogRecorder.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, want %d", catalogRecorder.Code, http.StatusOK)
	}

	pushReq := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	pushReq.Header.Set("Authorization", "Bearer "+readOnlyToken)
	pushRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pushRecorder, pushReq)
	if pushRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("push status = %d, want %d", pushRecorder.Code, http.StatusUnauthorized)
	}

	adminUsersReq := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	adminUsersReq.Header.Set("Authorization", "Bearer "+readOnlyToken)
	adminUsersRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminUsersRecorder, adminUsersReq)
	if adminUsersRecorder.Code != http.StatusForbidden {
		t.Fatalf("admin users status = %d, want %d", adminUsersRecorder.Code, http.StatusForbidden)
	}

	adminGrantsReq := httptest.NewRequest(http.MethodGet, "/admin/v1/users/"+created.ID+"/grants", nil)
	adminGrantsReq.Header.Set("Authorization", "Bearer "+readOnlyToken)
	adminGrantsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminGrantsRecorder, adminGrantsReq)
	if adminGrantsRecorder.Code != http.StatusForbidden {
		t.Fatalf("admin grants status = %d, want %d", adminGrantsRecorder.Code, http.StatusForbidden)
	}
}

func seedPublishedManifest(t *testing.T, handler *Router) string {
	t.Helper()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}

	return digest
}

func seedPublishedManifestWithToken(t *testing.T, handler *Router, token string) string {
	t.Helper()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStart.Header.Set("Authorization", "Bearer "+token)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, bytes.NewBufferString("layer-one"))
	appendReq.Header.Set("Authorization", "Bearer "+token)
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitReq.Header.Set("Authorization", "Bearer "+token)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/latest", bytes.NewReader(manifestPayload))
	manifestReq.Header.Set("Authorization", "Bearer "+token)
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("manifest status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}

	return digest
}

func newTestRouter(t *testing.T, accessController ports.AccessController) (*Router, func()) {
	t.Helper()

	blobStore, metadataStore, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, metadataStore, accessController, nil)
	return router, drainingCleanup(router, cleanup)
}

func newTestRouterWithAuth(t *testing.T, accessController ports.AccessController, authService ports.AuthService, options ...RouterOption) (*Router, func()) {
	t.Helper()

	blobStore, metadataStore, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, metadataStore, accessController, authService, options...)
	return router, drainingCleanup(router, cleanup)
}

// drainingCleanup waits for the router's service to finish any in-flight
// push-triggered scan goroutine (queuePushScan, spawned from every
// PublishManifest) before running the underlying store cleanup — otherwise
// that goroutine can race t.TempDir()'s removal after the test returns.
func drainingCleanup(router *Router, cleanup func()) func() {
	return func() {
		router.service.WaitForBackgroundWork()
		cleanup()
	}
}

func newTestRouterWithRealAuth(t *testing.T) (*Router, *appauth.Service, domainauth.Principal, string, func()) {
	t.Helper()

	authStore := newSQLiteAuthStore(t)
	accessController := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	authService := appauth.NewService(authStore)
	handler, cleanup := newTestRouterWithAuth(t, accessController, authServiceOrFatal(t, authService))

	bootstrapResult, err := authService.BootstrapAdmin(context.Background(), ports.BootstrapAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		cleanup()
		_ = authStore.Close()
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}

	loginResult, err := authService.LoginWithPassword(context.Background(), "admin", "password123", nil)
	if err != nil {
		cleanup()
		_ = authStore.Close()
		t.Fatalf("LoginWithPassword() error = %v", err)
	}

	adminActor := domainauth.Principal{UserID: bootstrapResult.User.ID, Username: bootstrapResult.User.Username, IsAdmin: true}
	return handler, authService, adminActor, loginResult.BearerToken, func() {
		cleanup()
		_ = authStore.Close()
	}
}

func authServiceOrFatal(t *testing.T, service *appauth.Service) ports.AuthService {
	t.Helper()
	if service == nil {
		t.Fatal("expected auth service")
	}
	return service
}

func newTestStores(t *testing.T) (*fsblob.Store, *metadata.Store, func()) {
	t.Helper()

	rootDir := t.TempDir()
	blobStore, err := fsblob.New(filepath.Join(rootDir, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}

	return blobStore, metadataStore, func() {
		_ = metadataStore.Close()
	}
}

func newSQLiteAuthStore(t *testing.T) *authpostgres.Store {
	t.Helper()

	store, err := authpostgres.NewWithDriver("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("authpostgres.NewWithDriver() error = %v", err)
	}

	return store
}

func newRouterWithStores(blobStore *fsblob.Store, metadataStore *metadata.Store, accessController ports.AccessController, authService ports.AuthService, options ...RouterOption) *Router {
	service := appregixtry.NewService(
		blobStore,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver("tenant-a"),
		ports.NewInlineJobRunner(),
	)
	_, _ = service.EnsureScanSettings(context.Background(), ports.ScanSettings{
		Enabled:              false,
		ScheduleEnabled:      false,
		Interval:             24 * time.Hour,
		Timeout:              15 * time.Minute,
		RegistryReachableURL: "https://registry.internal",
		MaxConcurrency:       1,
	})
	_ = metadataStore.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         filepath.Join(os.TempDir(), "regixtry-router-trivy-cache"),
		UpdatedAt:        time.Now().UTC(),
	})

	return NewRouter(service, authService, options...)
}

type allowAllAccessController struct{}

func (allowAllAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (allowAllAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
}

type fakeAuthService struct {
	loginResult ports.LoginResult
	loginErr    error
	verify      *domainauth.Principal
	verifyErr   error
	lastScopes  []domainauth.Scope
}

func (f fakeAuthService) EnsureBootstrapAdmin(context.Context) error { return nil }
func (f fakeAuthService) ListUsers(context.Context, domainauth.Principal) ([]domainauth.User, error) {
	return nil, nil
}
func (f fakeAuthService) CreateUser(context.Context, domainauth.Principal, ports.CreateUserInput) (domainauth.User, error) {
	return domainauth.User{}, nil
}
func (f fakeAuthService) UpdateUser(context.Context, domainauth.Principal, ports.UpdateUserInput) (domainauth.User, error) {
	return domainauth.User{}, nil
}
func (f fakeAuthService) SetUserEnabled(context.Context, domainauth.Principal, string, bool) (domainauth.User, error) {
	return domainauth.User{}, nil
}
func (f fakeAuthService) DeleteUser(context.Context, domainauth.Principal, string) error { return nil }
func (f fakeAuthService) BootstrapAdmin(context.Context, ports.BootstrapAdminInput) (ports.BootstrapAdminResult, error) {
	return ports.BootstrapAdminResult{}, nil
}

func (f fakeAuthService) LoginWithPassword(_ context.Context, _ string, _ string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	f.lastScopes = append([]domainauth.Scope(nil), requestedScopes...)
	return f.loginResult, f.loginErr
}
func (f fakeAuthService) LoginWithPreissuedToken(_ context.Context, _ string, _ string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	f.lastScopes = append([]domainauth.Scope(nil), requestedScopes...)
	return f.loginResult, f.loginErr
}
func (f fakeAuthService) VerifyAccessToken(context.Context, string) (domainauth.Principal, error) {
	if f.verifyErr != nil {
		return domainauth.Principal{}, f.verifyErr
	}
	if f.verify == nil {
		return domainauth.Principal{}, nil
	}
	return *f.verify, nil
}
func (f fakeAuthService) CreateAdminToken(context.Context, domainauth.Principal, ports.CreateAdminTokenInput) (ports.CreatedAdminToken, error) {
	return ports.CreatedAdminToken{}, nil
}
func (f fakeAuthService) ListAdminUsers(context.Context, domainauth.Principal) ([]ports.AdminUser, error) {
	return nil, nil
}
func (f fakeAuthService) CreateAdminUser(context.Context, domainauth.Principal, ports.AdminCreateUserInput) (ports.AdminUser, error) {
	return ports.AdminUser{}, nil
}
func (f fakeAuthService) EnableAdminUser(context.Context, domainauth.Principal, string) (ports.AdminUser, error) {
	return ports.AdminUser{}, nil
}
func (f fakeAuthService) DisableAdminUser(context.Context, domainauth.Principal, string) (ports.AdminUser, error) {
	return ports.AdminUser{}, nil
}
func (f fakeAuthService) ResetAdminUserPassword(context.Context, domainauth.Principal, ports.AdminResetPasswordInput) error {
	return nil
}
func (f fakeAuthService) ListAdminUserRepoGrants(context.Context, domainauth.Principal, string) ([]ports.AdminRepoGrant, error) {
	return nil, nil
}
func (f fakeAuthService) PutAdminUserRepoGrant(context.Context, domainauth.Principal, ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error) {
	return ports.AdminRepoGrant{}, nil
}
func (f fakeAuthService) DeleteAdminUserRepoGrant(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) ListAdminUserTokens(context.Context, domainauth.Principal, string) ([]ports.AdminToken, error) {
	return nil, nil
}
func (f fakeAuthService) CreateAdminUserToken(context.Context, domainauth.Principal, ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	return ports.AdminCreatedToken{}, nil
}
func (f fakeAuthService) RevokeAdminUserToken(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) ListRepoGrants(context.Context, domainauth.Principal, string) ([]domainauth.RepoGrant, error) {
	return nil, nil
}
func (f fakeAuthService) ListAdminTokens(context.Context, domainauth.Principal, string) ([]domainauth.Token, error) {
	return nil, nil
}
func (f fakeAuthService) RevokeAdminToken(context.Context, domainauth.Principal, string) error {
	return nil
}
func (f fakeAuthService) ResetPassword(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) PutRepoGrant(context.Context, domainauth.Principal, string, string, domainauth.RepoRole) (domainauth.RepoGrant, error) {
	return domainauth.RepoGrant{}, nil
}
func (f fakeAuthService) DeleteRepoGrant(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) ListRepositoryGrants(context.Context, domainauth.Principal, string) ([]domainauth.RepoGrant, error) {
	return nil, nil
}
func (f fakeAuthService) PutRepositoryGrant(context.Context, domainauth.Principal, string, string, domainauth.RepoRole) (domainauth.RepoGrant, error) {
	return domainauth.RepoGrant{}, nil
}
func (f fakeAuthService) DeleteRepositoryGrant(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) ListAdminRepositoryGrants(context.Context, domainauth.Principal, string) ([]ports.AdminRepositoryGrant, error) {
	return nil, nil
}
func (f fakeAuthService) PutAdminRepositoryGrant(context.Context, domainauth.Principal, ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	return ports.AdminRepositoryGrant{}, nil
}
func (f fakeAuthService) DeleteAdminRepositoryGrant(context.Context, domainauth.Principal, string, string) error {
	return nil
}
func (f fakeAuthService) CreateRobot(context.Context, domainauth.Principal, ports.CreateRobotInput) (ports.CreatedRobot, error) {
	return ports.CreatedRobot{}, nil
}
func (f fakeAuthService) ListRobots(context.Context, domainauth.Principal) ([]domainauth.User, error) {
	return nil, nil
}
func (f fakeAuthService) CreateAdminRobot(context.Context, domainauth.Principal, ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	return ports.AdminCreatedRobot{}, nil
}
func (f fakeAuthService) ListAdminRobots(context.Context, domainauth.Principal) ([]ports.AdminRobot, error) {
	return nil, nil
}
func (f fakeAuthService) DeleteRobot(context.Context, domainauth.Principal, string) error { return nil }
func (f fakeAuthService) DeleteAdminRobot(context.Context, domainauth.Principal, string) error {
	return nil
}

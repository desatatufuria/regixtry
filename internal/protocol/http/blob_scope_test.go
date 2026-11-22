package regixtryhttp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// repoScopedController authorizes an action only on the repositories listed
// for its verb: two routers over the SAME stores, each with its own
// controller, stand in for two principals with different grants.
type repoScopedController struct {
	pull map[string]bool
	push map[string]bool
}

func (c repoScopedController) Authorize(_ context.Context, action ports.Action) error {
	switch action.Verb {
	case ports.ActionPull:
		if c.pull[action.Repository] || c.push[action.Repository] {
			return nil
		}
	case ports.ActionPush:
		if c.push[action.Repository] {
			return nil
		}
	}
	return domain.NewUnauthorizedError("denied")
}

func (repoScopedController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
}

func pushBlob(t *testing.T, handler http.Handler, repository string, content []byte) string {
	t.Helper()
	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v2/"+repository+"/blobs/uploads/", nil))
	location := start.Header().Get("Location")
	if location == "" {
		t.Fatalf("upload start for %s: status %d", repository, start.Code)
	}
	digest := domain.DigestFromBytes(content).String()
	commit := httptest.NewRecorder()
	handler.ServeHTTP(commit, httptest.NewRequest(http.MethodPut, location+"?digest="+digest, bytes.NewReader(content)))
	if commit.Code != http.StatusCreated {
		t.Fatalf("blob commit status = %d, want %d (%s)", commit.Code, http.StatusCreated, commit.Body.String())
	}
	return digest
}

func imageManifestFor(digest string, size int) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":%d},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"%s","size":%d}]}`, digest, size, digest, size))
}

func serve(handler http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, target, bytes.NewReader(body)))
	return rec
}

// TestBlobsAreScopedToTheRepositoryThatHoldsThem proves a blob is readable
// and referenceable in repository R only when R holds a record linking it.
func TestBlobsAreScopedToTheRepositoryThatHoldsThem(t *testing.T) {
	t.Parallel()

	blobStore, metadataStore, cleanup := newTestStores(t)
	defer cleanup()

	ownerA := newRouterWithStores(blobStore, metadataStore, repoScopedController{push: map[string]bool{"team/a": true}}, nil)
	readerA := newRouterWithStores(blobStore, metadataStore, repoScopedController{pull: map[string]bool{"team/a": true}}, nil)
	ownerB := newRouterWithStores(blobStore, metadataStore, repoScopedController{push: map[string]bool{"team/b": true}}, nil)
	defer ownerA.service.WaitForBackgroundWork()
	defer ownerB.service.WaitForBackgroundWork()

	secret := []byte("repository-a-private-layer")
	digest := pushBlob(t, ownerA, "team/a", secret)

	t.Run("reader of A still reads and HEADs the blob in A", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := serve(readerA, method, "/v2/team/a/blobs/"+digest, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s in A status = %d, want 200", method, rec.Code)
			}
		}
		if body := serve(readerA, http.MethodGet, "/v2/team/a/blobs/"+digest, nil).Body.String(); body != string(secret) {
			t.Fatalf("GET in A body = %q", body)
		}
	})

	t.Run("a principal with pull on B only cannot read A's blob through B", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := serve(ownerB, method, "/v2/team/b/blobs/"+digest, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s via B status = %d, want 404 (body %q)", method, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), string(secret)) {
				t.Fatalf("%s via B leaked the blob", method)
			}
		}
	})

	t.Run("a push to B cannot reference A's blob it never uploaded", func(t *testing.T) {
		rec := serve(ownerB, http.MethodPut, "/v2/team/b/manifests/latest", imageManifestFor(digest, len(secret)))
		if rec.Code != http.StatusConflict {
			t.Fatalf("manifest publish status = %d, want 409 (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("cross-repository mount is not implemented and grants no link", func(t *testing.T) {
		rec := serve(ownerB, http.MethodPost, "/v2/team/b/blobs/uploads/?mount="+digest+"&from=team/a", nil)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("mount request status = %d, want 202 (plain upload start)", rec.Code)
		}
		if rec := serve(ownerB, http.MethodGet, "/v2/team/b/blobs/"+digest, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("GET in B after mount request status = %d, want 404", rec.Code)
		}
	})

	t.Run("uploading the same content to B links it there", func(t *testing.T) {
		got := pushBlob(t, ownerB, "team/b", secret)
		if got != digest {
			t.Fatalf("digest = %s, want %s", got, digest)
		}
		if rec := serve(ownerB, http.MethodGet, "/v2/team/b/blobs/"+digest, nil); rec.Code != http.StatusOK {
			t.Fatalf("GET in B after upload status = %d", rec.Code)
		}
		if rec := serve(ownerB, http.MethodPut, "/v2/team/b/manifests/latest", imageManifestFor(digest, len(secret))); rec.Code != http.StatusCreated {
			t.Fatalf("manifest publish after upload status = %d (%s)", rec.Code, rec.Body.String())
		}
	})
}

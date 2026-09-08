package regixtryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

// secretScanStatusFixturePayload is an arbitrary literal used only to
// produce a stable, unique digest per test -- unlike signature-status, this
// endpoint's verdict has no cryptographic fixture dependency.
const secretScanStatusFixturePayload = "secret-scan-status fixture image manifest"

// seedSecretScanStatusFixtureImage publishes an arbitrary image manifest
// directly through the metadata store, mirroring
// seedSignatureStatusFixtureImage's own store-level bypass.
func seedSecretScanStatusFixtureImage(t *testing.T, metadataStore *metadata.Store, repository string, suffix string) string {
	t.Helper()

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(secretScanStatusFixturePayload+suffix), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("domain.NewManifest() error = %v", err)
	}
	repo := domain.MustParseRepositoryRef(repository)
	if err := metadataStore.PublishManifest(context.Background(), "tenant-a", repo, "", manifest, manifest.BlobReferences()); err != nil {
		t.Fatalf("metadataStore.PublishManifest() error = %v", err)
	}
	return manifest.Digest.String()
}

// seedSecretScanStatusRun inserts one secret_scan_runs row (plus
// findingCount dummy secret_scan_findings rows) directly through the
// metadata store, mirroring internal/app/regixtry/secret_scan_status_test.go's
// own seedSecretScanRunForStatus helper.
func seedSecretScanStatusRun(t *testing.T, metadataStore *metadata.Store, repository string, digest string, status string, findingCount int) {
	t.Helper()

	now := time.Now().UTC()
	run := ports.SecretScanRun{
		ID:         "run-" + digest + "-" + status,
		Repository: repository,
		Digest:     digest,
		Status:     status,
		Trigger:    ports.ScanTriggerManual,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if status == ports.SecretScanRunStatusCompleted || status == ports.SecretScanRunStatusFailed {
		finished := now
		run.FinishedAt = &finished
	}
	findings := make([]ports.SecretFinding, 0, findingCount)
	for i := 0; i < findingCount; i++ {
		findings = append(findings, ports.SecretFinding{RuleID: "leaked-credential", Path: "config/secrets.yaml", BlobDigest: digest, StartLine: 1, EndLine: 1})
	}
	if err := metadataStore.UpsertSecretScanRunDetail(context.Background(), "tenant-a", ports.SecretScanRunDetail{Run: run, Findings: findings}); err != nil {
		t.Fatalf("UpsertSecretScanRunDetail() error = %v", err)
	}
}

// TestRouterManifestSecretScanStatusReturnsVerdictShape is the Phase 2 RED
// test (tasks.md 2.1): the endpoint is reachable at
// `GET /v2/<repo>/manifests/<ref>/secret-scan-status` and returns the exact
// documented response shape -- an unscanned verdict omits the `scan` key, a
// completed run includes it with `status`/`finding_count`/`finished_at`.
func TestRouterManifestSecretScanStatusReturnsVerdictShape(t *testing.T) {
	t.Parallel()

	t.Run("unscanned verdict omits the scan object", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
		defer drainingCleanup(router, cleanup)()

		repository := "library/alpine"
		digest := seedSecretScanStatusFixtureImage(t, store, repository, "-unscanned")

		req := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest+"/secret-scan-status", nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response error = %v, body = %s", err, recorder.Body.String())
		}
		if payload["repository"] != repository || payload["digest"] != digest {
			t.Fatalf("payload = %#v, want repository/digest to match the published manifest", payload)
		}
		if payload["state"] != "unscanned" {
			t.Fatalf("payload[state] = %v, want %q", payload["state"], "unscanned")
		}
		if _, ok := payload["scan"]; ok {
			t.Fatalf("payload = %#v, want no \"scan\" key for an unscanned verdict", payload)
		}
	})

	t.Run("scanned verdict includes the scan object", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
		defer drainingCleanup(router, cleanup)()

		repository := "library/alpine"
		digest := seedSecretScanStatusFixtureImage(t, store, repository, "-scanned")
		seedSecretScanStatusRun(t, store, repository, digest, ports.SecretScanRunStatusCompleted, 2)

		req := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest+"/secret-scan-status", nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}

		var payload struct {
			State string `json:"state"`
			Scan  *struct {
				Status       string `json:"status"`
				FindingCount int    `json:"finding_count"`
			} `json:"scan"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response error = %v, body = %s", err, recorder.Body.String())
		}
		if payload.State != "findings_present" {
			t.Fatalf("payload.State = %q, want %q", payload.State, "findings_present")
		}
		if payload.Scan == nil {
			t.Fatal("payload.Scan = nil, want a scan object present for a completed run")
		}
		if payload.Scan.FindingCount != 2 {
			t.Fatalf("payload.Scan.FindingCount = %d, want 2", payload.Scan.FindingCount)
		}
	})
}

// TestRouterManifestSecretScanStatusRouteCollisionWithTagNamedSecretScanStatus
// is the Phase 2 RED test (tasks.md 2.2, design.md threat matrix's route
// collision boundary): a tag literally named "secret-scan-status" must still
// route to handleManifest, not handleManifestSecretScanStatus -- mirrors
// TestRouterManifestSignatureStatusRouteCollisionWithTagNamedSignatureStatus.
func TestRouterManifestSecretScanStatusRouteCollisionWithTagNamedSecretScanStatus(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestRouter(t, allowAllAccessController{})
	defer cleanup()

	uploadStart := httptest.NewRequest(http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil)
	uploadStartRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadStartRecorder, uploadStart)
	uploadLocation := uploadStartRecorder.Header().Get("Location")

	appendReq := httptest.NewRequest(http.MethodPatch, uploadLocation, strings.NewReader("layer-one"))
	appendRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendRecorder, appendReq)

	digest := domain.DigestFromBytes([]byte("layer-one")).String()
	commitReq := httptest.NewRequest(http.MethodPut, uploadLocation+"?digest="+digest, nil)
	commitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(commitRecorder, commitReq)

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	manifestReq := httptest.NewRequest(http.MethodPut, "/v2/library/alpine/manifests/secret-scan-status", strings.NewReader(string(manifestPayload)))
	manifestReq.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	manifestRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manifestRecorder, manifestReq)
	if manifestRecorder.Code != http.StatusCreated {
		t.Fatalf("push manifest with tag \"secret-scan-status\" status = %d, want %d", manifestRecorder.Code, http.StatusCreated)
	}
	manifestDigest := manifestRecorder.Header().Get("Docker-Content-Digest")
	if manifestDigest == "" {
		t.Fatal("push manifest response missing Docker-Content-Digest")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/secret-scan-status", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getReq)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", getRecorder.Code, http.StatusOK)
	}
	if getRecorder.Header().Get("Docker-Content-Digest") != manifestDigest {
		t.Fatalf("Docker-Content-Digest = %q, want %q (proves handleManifest served this, not handleManifestSecretScanStatus)", getRecorder.Header().Get("Docker-Content-Digest"), manifestDigest)
	}
	if strings.Contains(getRecorder.Body.String(), `"state"`) {
		t.Fatalf("body = %q, want the raw manifest payload, not the secret-scan-status JSON shape", getRecorder.Body.String())
	}
}

// TestRouterManifestSecretScanStatusRequiresPullAuthorization is the Phase 2
// RED test (tasks.md 2.3): no credential is 401 with a WWW-Authenticate
// challenge, and a pull-authorized credential succeeds -- mirrors
// TestRouterManifestSignatureStatusRequiresPullAuthorization.
func TestRouterManifestSecretScanStatusRequiresPullAuthorization(t *testing.T) {
	t.Parallel()

	accessController := ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"})

	t.Run("pull-only credential succeeds", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		digest := seedSecretScanStatusFixtureImage(t, store, "library/alpine", "-pull-ok")
		seedRouter := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
		seedRouter.service.WaitForBackgroundWork()

		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{
			verify: &domainauth.Principal{
				Subject:  "atk_1",
				Username: "ci",
				Grants:   []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef("library/alpine"), Role: domainauth.RepoRoleReader}},
				Scopes:   []domainauth.Scope{{Type: "repository", Name: "library/alpine", Actions: []string{"pull"}, Canonical: "repository:library/alpine:pull"}},
			},
		})
		defer drainingCleanup(handler, cleanup)()

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/secret-scan-status", nil)
		req.Header.Set("Authorization", "Bearer pull-only-token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
	})

	t.Run("no credential is rejected with a WWW-Authenticate challenge", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		digest := seedSecretScanStatusFixtureImage(t, store, "library/alpine", "-no-cred")
		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{})
		defer drainingCleanup(handler, cleanup)()

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/secret-scan-status", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if recorder.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("WWW-Authenticate header missing on 401")
		}
	})

	t.Run("credential scoped to a different repository is refused", func(t *testing.T) {
		t.Parallel()

		blobStore, store, cleanup := newTestStores(t)
		digest := seedSecretScanStatusFixtureImage(t, store, "library/alpine", "-other-repo")
		handler := newRouterWithStores(blobStore, store, accessController, fakeAuthService{
			verify: &domainauth.Principal{
				Subject: "atk_1",
				Scopes:  []domainauth.Scope{{Type: "repository", Name: "team/other", Actions: []string{"pull"}, Canonical: "repository:team/other:pull"}},
			},
		})
		defer drainingCleanup(handler, cleanup)()

		req := httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/"+digest+"/secret-scan-status", nil)
		req.Header.Set("Authorization", "Bearer other-repo-token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})
}

// TestRouterManifestSecretScanStatusMethodNotAllowed is the Phase 2 RED test
// (tasks.md 2.4): any non-GET method answers 405 with an Allow: GET header,
// table-driven.
func TestRouterManifestSecretScanStatusMethodNotAllowed(t *testing.T) {
	t.Parallel()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			blobStore, store, cleanup := newTestStores(t)
			router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
			defer drainingCleanup(router, cleanup)()

			digest := seedSecretScanStatusFixtureImage(t, store, "library/alpine", "-"+method)

			req := httptest.NewRequest(method, "/v2/library/alpine/manifests/"+digest+"/secret-scan-status", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d, want %d", method, recorder.Code, http.StatusMethodNotAllowed)
			}
			if got, want := recorder.Header().Get("Allow"), http.MethodGet; got != want {
				t.Fatalf("%s Allow = %q, want %q", method, got, want)
			}
		})
	}
}

// TestRouterManifestSecretScanStatusNeverLeaksBlockingOrFindingFields is the
// Phase 2 RED test (tasks.md 2.5, design.md threat matrix's capability/data
// disclosure boundary): the serialized response never contains
// would_block_pull or a severity-threshold key at any level, and never
// contains rule_id/path/a finding-array key -- ports.SecretFinding must
// never be referenced from SecretScanStatusScan.
func TestRouterManifestSecretScanStatusNeverLeaksBlockingOrFindingFields(t *testing.T) {
	t.Parallel()

	blobStore, store, cleanup := newTestStores(t)
	router := newRouterWithStores(blobStore, store, allowAllAccessController{}, nil)
	defer drainingCleanup(router, cleanup)()

	repository := "library/alpine"
	digest := seedSecretScanStatusFixtureImage(t, store, repository, "-leak-check")
	seedSecretScanStatusRun(t, store, repository, digest, ports.SecretScanRunStatusCompleted, 1)

	req := httptest.NewRequest(http.MethodGet, "/v2/"+repository+"/manifests/"+digest+"/secret-scan-status", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	body := recorder.Body.String()
	forbidden := []string{"would_block_pull", "severity_threshold", "rule_id", "\"path\"", "findings\""}
	for _, key := range forbidden {
		if strings.Contains(body, key) {
			t.Fatalf("body = %s, must never contain %q", body, key)
		}
	}
}

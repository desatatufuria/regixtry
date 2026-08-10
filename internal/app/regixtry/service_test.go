package regixtry

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

func TestServiceUploadPublishAndQuery(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	upload, err := service.BeginUpload(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}

	if _, err := service.AppendUpload(context.Background(), "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}

	blobPayload := []byte("layer-one")
	blob, err := service.CompleteUpload(context.Background(), "library/alpine", upload.ID, digestForTest(blobPayload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)

	published, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload)
	if err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	resolved, err := service.ResolveManifest(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("ResolveManifest() error = %v", err)
	}

	if resolved.Digest != published.Digest {
		t.Fatalf("resolved.Digest = %q, want %q", resolved.Digest, published.Digest)
	}

	catalog, err := service.Catalog(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	if len(catalog.Repositories) != 1 || catalog.Repositories[0] != "library/alpine" {
		t.Fatalf("catalog.Repositories = %#v, want [library/alpine]", catalog.Repositories)
	}

	tags, err := service.Tags(context.Background(), "library/alpine", 10, "")
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}

	if len(tags.Tags) != 1 || tags.Tags[0] != "latest" {
		t.Fatalf("tags.Tags = %#v, want [latest]", tags.Tags)
	}

	blobDetails, err := service.InspectBlob(context.Background(), "library/alpine", blob.Digest)
	if err != nil {
		t.Fatalf("InspectBlob() error = %v", err)
	}

	if blobDetails.Size != blob.Size {
		t.Fatalf("blobDetails.Size = %d, want %d", blobDetails.Size, blob.Size)
	}

	uploads, err := service.Uploads(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("Uploads() error = %v", err)
	}

	if len(uploads) != 0 {
		t.Fatalf("len(uploads) = %d, want 0", len(uploads))
	}
}

func TestServiceRejectsManifestWithMissingBlob(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:8a5a3d2cfb08cf0c22848f3322a7fd6f1300a0a176c1f907fdbd53f5b5d2e236","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"sha256:8a5a3d2cfb08cf0c22848f3322a7fd6f1300a0a176c1f907fdbd53f5b5d2e236","size":9}]}`)

	if _, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err == nil {
		t.Fatal("expected missing blob conflict")
	}
}

func TestServiceAuthorizesRepositoryActionsAndFiltersCatalog(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}))
	defer cleanup()

	adminCtx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: []domainauth.Scope{{Type: "registry", Name: "catalog", Actions: []string{"*"}, Canonical: "registry:catalog:*"}, {Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}, {Type: "repository", Name: "team/other", Actions: []string{"pull", "push"}, Canonical: "repository:team/other:pull,push"}}})
	seedRepository(t, service, adminCtx, "team/app")
	seedRepository(t, service, adminCtx, "team/other")

	readerCtx := ports.ContextWithPrincipal(context.Background(), principalForGrants("team/app", domainauth.RepoRoleReader, []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}}))
	catalog, err := service.Catalog(readerCtx, 10, "")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if len(catalog.Repositories) != 1 || catalog.Repositories[0] != "team/app" {
		t.Fatalf("catalog.Repositories = %#v, want [team/app]", catalog.Repositories)
	}

	if _, err := service.Tags(readerCtx, "team/app", 10, ""); err != nil {
		t.Fatalf("Tags() error = %v", err)
	}

	if _, err := service.BeginUpload(readerCtx, "team/app"); err == nil {
		t.Fatal("expected repo-reader push to be rejected")
	} else if !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}

	if _, err := service.Tags(readerCtx, "team/other", 10, ""); err == nil {
		t.Fatal("expected unauthorized tags access for other repository")
	}
}

func TestServiceAdminBypassesRepositoryChecks(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}))
	defer cleanup()

	adminCtx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}, {Type: "repository", Name: "team/other", Actions: []string{"pull", "push"}, Canonical: "repository:team/other:pull,push"}, {Type: "registry", Name: "catalog", Actions: []string{"*"}, Canonical: "registry:catalog:*"}}})
	seedRepository(t, service, adminCtx, "team/app")

	if _, err := service.BeginUpload(adminCtx, "team/other"); err != nil {
		t.Fatalf("BeginUpload() admin error = %v", err)
	}
}

func TestServiceQueuesDigestCentricManualScansAndDedupesActiveRuns(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, MaxConcurrency: 1, CacheDir: filepath.Join(t.TempDir(), "trivy-cache"), BinaryPath: "trivy"}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}

	first, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan(first) error = %v", err)
	}
	second, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan(second) error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("second run = %#v, want active run dedupe for %#v", second, first)
	}
	if first.Digest == "" {
		t.Fatalf("first run = %#v, want resolved digest", first)
	}
}

func TestServiceRejectsInvalidOrUnpublishedScanTargets(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, MaxConcurrency: 1, CacheDir: filepath.Join(t.TempDir(), "trivy-cache"), BinaryPath: "trivy"}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}

	if _, err := service.QueueManualScan(context.Background(), "library/alpine", "missing"); err == nil {
		t.Fatal("QueueManualScan() error = nil, want not found")
	}
}

func TestServiceRejectsOutOfBoundsScanSettings(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, err := service.UpdateScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: 0, Interval: 0, MaxConcurrency: 0, CacheDir: " ", BinaryPath: "trivy"})
	if err == nil {
		t.Fatal("UpdateScanSettings() error = nil, want validation error")
	}
}

func TestServiceListFeaturesReturnsBuiltinTrivyInventory(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	features, err := service.ListFeatures(context.Background())
	if err != nil {
		t.Fatalf("ListFeatures() error = %v", err)
	}

	want := []ports.FeatureSummary{{
		Name:       "trivy",
		Kind:       ports.FeatureKindBuiltin,
		Enabled:    false,
		Configured: false,
	}}
	if !reflect.DeepEqual(features, want) {
		t.Fatalf("ListFeatures() = %#v, want %#v", features, want)
	}
}

func TestServiceGetFeatureStatusProjectsTrivyRuntimeDetails(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	settings, err := service.ImportLegacyFeatureConfigIfMissing(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:         boolPtr(true),
		ScheduleEnabled: boolPtr(true),
		Interval:        durationPtr(6 * time.Hour),
		Timeout:         durationPtr(10 * time.Minute),
		CacheDir:        stringPtr(filepath.Join(t.TempDir(), "trivy-cache")),
		BinaryPath:      stringPtr("trivy-custom"),
		MaxConcurrency:  intPtr(2),
	})
	if err != nil {
		t.Fatalf("ImportLegacyFeatureConfigIfMissing() error = %v", err)
	}

	restoreProbe := swapTrivyRuntimeProbe(t, func(path string) ports.FeatureRuntime {
		if path != settings.BinaryPath {
			t.Fatalf("probe path = %q, want %q", path, settings.BinaryPath)
		}
		return ports.FeatureRuntime{Health: "ready", Version: "0.57.1", Detail: "binary reachable"}
	})
	defer restoreProbe()

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if status.Name != "trivy" || status.Kind != ports.FeatureKindBuiltin {
		t.Fatalf("status identity = %#v, want trivy builtin", status)
	}
	if !status.Enabled || !status.Configured || !status.ScheduleEnabled {
		t.Fatalf("status flags = %#v, want configured enabled schedule-enabled state", status)
	}
	if status.Interval != 6*time.Hour || status.Timeout != 10*time.Minute {
		t.Fatalf("status timings = %#v, want imported timings", status)
	}
	if status.Runtime.Health != "ready" || status.Runtime.Version != "0.57.1" {
		t.Fatalf("status runtime = %#v, want projected runtime details", status.Runtime)
	}
}

func TestServiceRejectsUnknownFeatureNames(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, err := service.GetFeature(context.Background(), "future-plugin")
	if err == nil || !strings.Contains(err.Error(), "unsupported feature") {
		t.Fatalf("GetFeature() error = %v, want unsupported feature rejection", err)
	}
}

func TestServiceImportLegacyFeatureConfigIfMissingPreservesExistingState(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	initial, err := service.ConfigureFeature(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:         boolPtr(true),
		ScheduleEnabled: boolPtr(false),
		Interval:        durationPtr(24 * time.Hour),
		Timeout:         durationPtr(15 * time.Minute),
		CacheDir:        stringPtr(filepath.Join(t.TempDir(), "configured-cache")),
		BinaryPath:      stringPtr("trivy"),
		MaxConcurrency:  intPtr(1),
	})
	if err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}

	imported, err := service.ImportLegacyFeatureConfigIfMissing(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:         boolPtr(false),
		ScheduleEnabled: boolPtr(true),
		Interval:        durationPtr(3 * time.Hour),
		Timeout:         durationPtr(20 * time.Minute),
		CacheDir:        stringPtr(filepath.Join(t.TempDir(), "legacy-cache")),
		BinaryPath:      stringPtr("trivy-legacy"),
		MaxConcurrency:  intPtr(4),
	})
	if err != nil {
		t.Fatalf("ImportLegacyFeatureConfigIfMissing() error = %v", err)
	}

	if imported != initial {
		t.Fatalf("ImportLegacyFeatureConfigIfMissing() = %#v, want preserved existing state %#v", imported, initial)
	}
}

func TestServicePublishRemainsAvailableWhileScanRunsExist(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/base")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, MaxConcurrency: 1, CacheDir: filepath.Join(t.TempDir(), "trivy-cache"), BinaryPath: "trivy"}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	if _, err := service.QueueManualScan(context.Background(), "library/base", "latest"); err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}

	upload, err := service.BeginUpload(context.Background(), "library/next")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/next", upload.ID, strings.NewReader("layer-two")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	payload := []byte("layer-two")
	blob, err := service.CompleteUpload(context.Background(), "library/next", upload.ID, digestForTest(payload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)
	if _, err := service.PublishManifest(context.Background(), "library/next", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
}

func TestServiceFailedScanDoesNotHidePublishedContent(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/base")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, MaxConcurrency: 1, CacheDir: filepath.Join(t.TempDir(), "trivy-cache"), BinaryPath: "trivy"}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	runner := &fakeScanRunner{started: make(chan struct{}, 1), err: errFakeScan}
	service.SetScanRunner(runner)

	queued, err := service.QueueManualScan(context.Background(), "library/base", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("scan runner did not start")
	}

	failed := waitForScanRunStatus(t, service, "library/base", queued.ID, ports.ScanRunStatusFailed)
	if failed.Digest == "" {
		t.Fatalf("failed run = %#v, want persisted digest", failed)
	}

	resolved, err := service.ResolveManifest(context.Background(), "library/base", "latest")
	if err != nil {
		t.Fatalf("ResolveManifest() error = %v", err)
	}
	if resolved.Digest != failed.Digest {
		t.Fatalf("resolved.Digest = %q, want failed scan digest %q", resolved.Digest, failed.Digest)
	}

	opened, err := service.OpenManifest(context.Background(), "library/base", "latest")
	if err != nil {
		t.Fatalf("OpenManifest() error = %v", err)
	}
	if opened.Digest.String() != failed.Digest {
		t.Fatalf("OpenManifest().Digest = %q, want %q", opened.Digest.String(), failed.Digest)
	}
	if got := runner.Calls(); len(got) != 1 {
		t.Fatalf("runner calls = %v, want exactly one scan execution", got)
	}
}

func newTestService(t *testing.T, accessController ports.AccessController) (*Service, func()) {
	t.Helper()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}

	service := NewService(
		blobs,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver("tenant-a"),
		ports.NewInlineJobRunner(),
	)

	return service, func() {
		_ = metadataStore.Close()
	}
}

type allowAllAccessController struct{}

func (allowAllAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (allowAllAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "regixtry", Service: "regixtry"}
}

func seedRepository(t *testing.T, service *Service, ctx context.Context, repository string) {
	t.Helper()

	upload, err := service.BeginUpload(ctx, repository)
	if err != nil {
		t.Fatalf("BeginUpload(%q) error = %v", repository, err)
	}

	if _, err := service.AppendUpload(ctx, repository, upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload(%q) error = %v", repository, err)
	}

	blobPayload := []byte("layer-one")
	blob, err := service.CompleteUpload(ctx, repository, upload.ID, digestForTest(blobPayload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload(%q) error = %v", repository, err)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)
	if _, err := service.PublishManifest(ctx, repository, "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest(%q) error = %v", repository, err)
	}
}

func boolPtr(value bool) *bool { return &value }

func durationPtr(value time.Duration) *time.Duration { return &value }

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

func swapTrivyRuntimeProbe(t *testing.T, probe func(string) ports.FeatureRuntime) func() {
	t.Helper()

	previous := probeTrivyRuntime
	probeTrivyRuntime = probe
	return func() {
		probeTrivyRuntime = previous
	}
}

func principalForGrants(repository string, role domainauth.RepoRole, scopes []domainauth.Scope) domainauth.Principal {
	return domainauth.Principal{Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef(repository), Role: role}}, Scopes: scopes}
}

func digestForTest(payload []byte) string {
	return domain.DigestFromBytes(payload).String()
}

type fakeScanRunner struct {
	started chan struct{}
	release chan struct{}
	err     error
	mu      sync.Mutex
	calls   []string
}

func (f *fakeScanRunner) Run(ctx context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, imageRef)
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ports.ScanResult{}, ctx.Err()
		}
	}
	if f.err != nil {
		return ports.ScanResult{}, f.err
	}
	updatedAt := time.Now().UTC()
	return ports.ScanResult{TrivyVersion: "0.54.0", DBUpdatedAt: &updatedAt, Critical: 1, High: 2, Medium: 3, Low: 4}, nil
}

func (f *fakeScanRunner) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

var errFakeScan = errors.New("fake scan failure")

func waitForScanRunStatus(t *testing.T, service *Service, repository string, runID string, wantStatus string) ports.ScanRun {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runs, err := service.ListScanRuns(context.Background(), repository, 10)
		if err != nil {
			if strings.Contains(err.Error(), "database is locked") {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			t.Fatalf("ListScanRuns() error = %v", err)
		}
		for _, run := range runs {
			if run.ID == runID && run.Status == wantStatus {
				return run
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %q did not reach status %q", runID, wantStatus)
	return ports.ScanRun{}
}

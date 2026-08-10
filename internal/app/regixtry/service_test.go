package regixtry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	trivy "regixtry/internal/infra/scanning/trivy"
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
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

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

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	if _, err := service.QueueManualScan(context.Background(), "library/alpine", "missing"); err == nil {
		t.Fatal("QueueManualScan() error = nil, want not found")
	}
}

func TestServiceRejectsOutOfBoundsScanSettings(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, err := service.UpdateScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: 0, Interval: 0, MaxConcurrency: 0, ServiceURL: "ftp://scanner.example.com", RegistryReachableURL: "http://127.0.0.1:5000"})
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

func TestServiceGetFeatureStatusProjectsManagedRuntimeDetails(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, err := service.ConfigureFeature(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:              boolPtr(true),
		ScheduleEnabled:      boolPtr(true),
		Interval:             durationPtr(6 * time.Hour),
		Timeout:              durationPtr(10 * time.Minute),
		RegistryReachableURL: stringPtr("https://registry.internal:5443"),
		MaxConcurrency:       intPtr(2),
	})
	if err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}
	verifiedAt := time.Now().UTC().Add(-time.Minute)
	if err := service.metadata.UpsertTrivyRuntimeState(context.Background(), "tenant-a", ports.TrivyRuntimeState{
		Status:           ports.TrivyRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		PreviousVersion:  "0.56.2",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         "/var/lib/regixtry/features/trivy/trivy-cache",
		ReceiptPath:      "/var/lib/regixtry/features/trivy/receipts/0.57.1.json",
		LastVerifiedAt:   &verifiedAt,
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTrivyRuntimeState() error = %v", err)
	}

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
	if status.ServiceURL != "" || status.RegistryReachableURL != "https://registry.internal:5443" {
		t.Fatalf("status endpoints = %#v, want managed intent projection", status)
	}
	if status.AuthToken != "" {
		t.Fatalf("status.AuthToken = %q, want redacted read-only output", status.AuthToken)
	}
	if status.Runtime.Mode != ports.FeatureRuntimeModeManaged || status.Runtime.Health != "ready" || status.Runtime.Version != "0.57.1" || !status.Runtime.RollbackAvailable {
		t.Fatalf("status runtime = %#v, want projected managed runtime details", status.Runtime)
	}
}

func TestServiceGetFeatureStatusPreservesIntentWhenManagedRuntimeReady(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.ConfigureFeature(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:         boolPtr(true),
		ScheduleEnabled: boolPtr(true),
		Interval:        durationPtr(6 * time.Hour),
		Timeout:         durationPtr(10 * time.Minute),
		RegistryReachableURL: stringPtr("https://registry.example.com"),
		MaxConcurrency:  intPtr(2),
	}); err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if status.Runtime.Health != "ready" || status.Runtime.Version != "0.57.1" {
		t.Fatalf("status.Runtime = %#v, want ready managed runtime", status.Runtime)
	}
	if status.RegistryReachableURL != "https://registry.example.com" {
		t.Fatalf("status.RegistryReachableURL = %q, want persisted managed intent", status.RegistryReachableURL)
	}
}

func TestServiceBuildsScannerFacingTargetAndRejectsLoopbackFallback(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetScanHost("http://127.0.0.1:5000")

	target, err := service.scanTarget(ports.ScanSettings{RegistryReachableURL: "https://registry.internal:5443"}, "library/alpine", "sha256:abc")
	if err != nil {
		t.Fatalf("scanTarget() error = %v", err)
	}
	if target != "registry.internal:5443/library/alpine@sha256:abc" {
		t.Fatalf("scanTarget() = %q, want registry-internal target", target)
	}

	_, err = service.scanTarget(ports.ScanSettings{}, "library/alpine", "sha256:abc")
	if err == nil || !strings.Contains(err.Error(), "registry_reachable_url") {
		t.Fatalf("scanTarget() error = %v, want registry_reachable_url validation", err)
	}
}

func TestServiceReadyStatusRequiresVersionProbeAndLegacyBridgeKeepsRuntimeUnconfigured(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	legacy, err := service.ImportLegacyFeatureConfigIfMissing(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:         boolPtr(true),
		ScheduleEnabled: boolPtr(true),
		Interval:        durationPtr(3 * time.Hour),
		Timeout:         durationPtr(17 * time.Minute),
		MaxConcurrency:  intPtr(4),
	})
	if err != nil {
		t.Fatalf("ImportLegacyFeatureConfigIfMissing() error = %v", err)
	}
	if legacy.ServiceURL != "" || legacy.RegistryReachableURL != "" {
		t.Fatalf("legacy details = %#v, want service contract without inherited binary fields", legacy)
	}

	if _, err := service.ConfigureFeature(context.Background(), "trivy", ports.FeatureConfigureInput{
		ServiceURL:           stringPtr("https://scanner.example.com"),
		RegistryReachableURL: stringPtr("https://registry.internal"),
		Timeout:              durationPtr(10 * time.Minute),
		Interval:             durationPtr(6 * time.Hour),
		MaxConcurrency:       intPtr(2),
	}); err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if status.Runtime.Health != "migration-required" {
		t.Fatalf("status.Runtime = %#v, want migration-required runtime", status.Runtime)
	}

	legacyStatus, err := service.GetFeature(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeature() error = %v", err)
	}
	if legacyStatus.Runtime != (ports.FeatureRuntime{}) {
		t.Fatalf("GetFeature() runtime = %#v, want runtime omitted outside status view", legacyStatus.Runtime)
	}
}

func TestServiceGetFeatureStatusSeparatesIntentFromManagedRuntimeState(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	settings, err := service.ConfigureFeature(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:              boolPtr(true),
		ScheduleEnabled:      boolPtr(true),
		Interval:             durationPtr(6 * time.Hour),
		Timeout:              durationPtr(10 * time.Minute),
		RegistryReachableURL: stringPtr("https://registry.internal:5443"),
		MaxConcurrency:       intPtr(2),
	})
	if err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}
	if settings.ServiceURL != "" {
		t.Fatalf("settings.ServiceURL = %q, want feature intent without runtime service ownership", settings.ServiceURL)
	}

	verifiedAt := time.Now().UTC().Add(-time.Minute)
	healthAt := verifiedAt.Add(30 * time.Second)
	if err := service.metadata.UpsertTrivyRuntimeState(context.Background(), "tenant-a", ports.TrivyRuntimeState{
		Status:            ports.TrivyRuntimeStatusReady,
		ActiveVersion:     "0.57.1",
		PreviousVersion:   "0.56.2",
		ActiveBinaryPath:  filepath.Join(t.TempDir(), "features", "trivy", "bin", "active", "trivy"),
		CacheDir:          filepath.Join(t.TempDir(), "features", "trivy", "trivy-cache"),
		ReceiptPath:       filepath.Join(t.TempDir(), "features", "trivy", "receipts", "0.57.1.json"),
		LastVerifiedAt:    &verifiedAt,
		LastHealthCheckAt: &healthAt,
		UpdatedAt:         healthAt,
	}); err != nil {
		t.Fatalf("UpsertTrivyRuntimeState() error = %v", err)
	}

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if status.Runtime.Mode != ports.FeatureRuntimeModeManaged || status.Runtime.Status != string(ports.TrivyRuntimeStatusReady) {
		t.Fatalf("status.Runtime = %#v, want managed ready runtime projection", status.Runtime)
	}
	if status.Runtime.Version != "0.57.1" || !status.Runtime.RollbackAvailable {
		t.Fatalf("status.Runtime = %#v, want active version and rollback availability", status.Runtime)
	}
	if status.ServiceURL != "" || status.BinaryPath != "" || status.CacheDir != "" {
		t.Fatalf("status = %#v, want runtime lifecycle separated from feature intent fields", status)
	}
	if status.RegistryReachableURL != "https://registry.internal:5443" || status.Interval != 6*time.Hour || status.Timeout != 10*time.Minute {
		t.Fatalf("status = %#v, want operator intent preserved", status)
	}
}

func TestServiceGetFeatureStatusReportsLegacyRuntimeMigrationRequired(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", ports.ScanSettings{
		Enabled:              true,
		ScheduleEnabled:      true,
		Interval:             3 * time.Hour,
		Timeout:              17 * time.Minute,
		RegistryReachableURL: "https://registry.internal",
		LegacyCacheDir:       "/var/cache/trivy",
		LegacyBinaryPath:     "/tmp/README.sh",
		ServiceURL:           "https://scanner.example.com",
		MaxConcurrency:       2,
		UpdatedAt:            time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if status.Runtime.Status != string(ports.TrivyRuntimeStatusMigrationRequired) || status.Runtime.Health != "migration-required" {
		t.Fatalf("status.Runtime = %#v, want migration-required runtime", status.Runtime)
	}
	if !strings.Contains(status.Runtime.Detail, "/tmp/README.sh") {
		t.Fatalf("status.Runtime.Detail = %q, want legacy binary evidence", status.Runtime.Detail)
	}
	if status.Runtime.Mode != ports.FeatureRuntimeModeManaged {
		t.Fatalf("status.Runtime.Mode = %q, want managed runtime truth even for migration", status.Runtime.Mode)
	}
}

func TestServiceManualAndScheduledScansUseManagedRuntimeStateAndIgnoreLegacyPaths(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", ports.ScanSettings{
		Enabled:              true,
		ScheduleEnabled:      true,
		Interval:             time.Hour,
		Timeout:              time.Minute,
		RegistryReachableURL: "https://registry.internal",
		LegacyBinaryPath:     "/tmp/README.sh",
		MaxConcurrency:       1,
		UpdatedAt:            time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}
	if err := service.metadata.UpsertTrivyRuntimeState(context.Background(), "tenant-a", ports.TrivyRuntimeState{
		Status:           ports.TrivyRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         "/var/lib/regixtry/features/trivy/trivy-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTrivyRuntimeState() error = %v", err)
	}
	runner := &capturingScanRunner{result: ports.ScanResult{TrivyVersion: "0.57.1"}}
	service.SetScanRunner(runner)

	manual, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	waitForScanCompletion(t, service, manual.ID)
	if len(runner.settings) == 0 || runner.settings[0].BinaryPath != "/var/lib/regixtry/features/trivy/bin/active/trivy" {
		t.Fatalf("runner settings = %#v, want managed binary path", runner.settings)
	}
	if runner.settings[0].LegacyBinaryPath != "/tmp/README.sh" {
		t.Fatalf("runner settings = %#v, want legacy evidence preserved but not executed", runner.settings)
	}

	if err := service.RunScheduledScans(context.Background()); err != nil {
		t.Fatalf("RunScheduledScans() error = %v", err)
	}
	waitForRunnerTargets(t, runner, 2)
	if len(runner.targets) < 2 {
		t.Fatalf("targets = %#v, want manual and scheduled scans through managed runtime", runner.targets)
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
		Enabled:              boolPtr(true),
		ScheduleEnabled:      boolPtr(false),
		Interval:             durationPtr(24 * time.Hour),
		Timeout:              durationPtr(15 * time.Minute),
		ServiceURL:           stringPtr("https://scanner.example.com"),
		RegistryReachableURL: stringPtr("https://registry.internal"),
		MaxConcurrency:       intPtr(1),
	})
	if err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}

	imported, err := service.ImportLegacyFeatureConfigIfMissing(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:              boolPtr(false),
		ScheduleEnabled:      boolPtr(true),
		Interval:             durationPtr(3 * time.Hour),
		Timeout:              durationPtr(20 * time.Minute),
		ServiceURL:           stringPtr("https://legacy-scanner.example.com"),
		RegistryReachableURL: stringPtr("https://legacy-registry.internal"),
		MaxConcurrency:       intPtr(4),
	})
	if err != nil {
		t.Fatalf("ImportLegacyFeatureConfigIfMissing() error = %v", err)
	}

	if imported != initial {
		t.Fatalf("ImportLegacyFeatureConfigIfMissing() = %#v, want preserved existing state %#v", imported, initial)
	}
}

func TestServicePublishRemainsAvailableWhileScanRunsExist(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/base")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
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
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/base")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
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

type capturingScanRunner struct {
	mu       sync.Mutex
	result   ports.ScanResult
	settings []ports.ScanSettings
	targets  []string
}

func (c *capturingScanRunner) Run(_ context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = append(c.targets, imageRef)
	c.settings = append(c.settings, settings)
	return c.result, nil
}

func waitForScanCompletion(t *testing.T, service *Service, runID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := service.metadata.GetScanRun(context.Background(), "tenant-a", runID)
		if err == nil && run.Status == ports.ScanRunStatusCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("scan run %s did not complete before deadline", runID)
}

func waitForRunnerTargets(t *testing.T, runner *capturingScanRunner, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runner.mu.Lock()
		count := len(runner.targets)
		runner.mu.Unlock()
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runner targets did not reach %d before deadline", want)
}

func seedManagedRuntimeState(t *testing.T, service *Service, version string) {
	t.Helper()
	if err := service.metadata.UpsertTrivyRuntimeState(context.Background(), "tenant-a", ports.TrivyRuntimeState{
		Status:           ports.TrivyRuntimeStatusReady,
		ActiveVersion:    version,
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         "/var/lib/regixtry/features/trivy/trivy-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTrivyRuntimeState() error = %v", err)
	}
}

func newTestTrivyRunner(t *testing.T, wantToken string) (*trivy.Runner, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); wantToken != "" && got != "Bearer "+wantToken {
			http.Error(w, fmt.Sprintf("unexpected auth header %q", got), http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			_, _ = w.Write([]byte(`{"Version":"0.57.1"}`))
		default:
			http.NotFound(w, r)
		}
	}))

	return trivy.New(trivy.RunnerConfig{}), server
}

func newFailingVersionTrivyRunner(t *testing.T) (*trivy.Runner, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			http.Error(w, "version probe failed", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))

	return trivy.New(trivy.RunnerConfig{}), server
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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := service.metadata.GetScanRun(context.Background(), "tenant-a", runID)
		if err != nil {
			if strings.Contains(err.Error(), "database is locked") {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			t.Fatalf("GetScanRun() error = %v", err)
		}
		if run.Repository == repository && run.Status == wantStatus {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %q did not reach status %q", runID, wantStatus)
	return ports.ScanRun{}
}

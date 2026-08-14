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

// TestServiceDeleteManifestRejectsUnauthorizedCallerRegardlessOfFlag pins
// design.md Decision 2's ordering: authorization is checked BEFORE the
// deleteEnabled flag. A caller who lacks delete access (writer scope
// requesting only pull,push -- TestPrincipalHasDeleteAccess already pins
// this as a failure) must be rejected with domain.ErrorCodeUnauthorized
// whether the flag is on or off, so the flag's state never leaks to a
// caller who was never entitled to delete in the first place.
func TestServiceDeleteManifestRejectsUnauthorizedCallerRegardlessOfFlag(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		deleteEnabled bool
	}{
		{name: "flag off", deleteEnabled: false},
		{name: "flag on", deleteEnabled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "regixtry", Service: "regixtry"}))
			defer cleanup()

			service.SetDeleteEnabled(tt.deleteEnabled)

			writerCtx := ports.ContextWithPrincipal(context.Background(), principalForGrants("team/app", domainauth.RepoRoleWriter, []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}}))

			if _, err := service.DeleteManifest(writerCtx, "team/app", "latest"); err == nil {
				t.Fatal("expected unauthorized error for a pull,push-only token")
			} else if !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
				t.Fatalf("DeleteManifest() error = %v, want ErrorCodeUnauthorized", err)
			}
		})
	}
}

// TestServiceDeleteManifestRefusesWithValidationErrorWhenFlagOffForAuthorizedCaller
// pins design.md Decision 2: an authorized caller is refused with
// domain.ErrorCodeValidation when the flag is off (the OCI UNSUPPORTED wire
// mapping is a router-layer concern, Phase 4, not this service). The store
// must never be reached: proven here by confirming the manifest is still
// resolvable afterward.
func TestServiceDeleteManifestRefusesWithValidationErrorWhenFlagOffForAuthorizedCaller(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()
	published := publishManifestWithTags(t, service, ctx, "team/app", "latest")

	// deleteEnabled defaults to false -- SetDeleteEnabled is never called.
	if _, err := service.DeleteManifest(ctx, "team/app", "latest"); err == nil {
		t.Fatal("expected validation error when the delete flag is off")
	} else if !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("DeleteManifest() error = %v, want ErrorCodeValidation", err)
	}

	resolved, err := service.ResolveManifest(ctx, "team/app", "latest")
	if err != nil {
		t.Fatalf("ResolveManifest() after refused delete error = %v, want the manifest untouched", err)
	}
	if resolved.Digest != published.Digest {
		t.Fatalf("resolved.Digest = %q, want %q -- store must never be reached when the flag is off", resolved.Digest, published.Digest)
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

func TestServiceQueuePushScanCreatesQueuedRunWhenScanSettingsEnabled(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	digest := "sha256:" + strings.Repeat("a", 64)
	service.queuePushScan(context.Background(), "tenant-a", "library/alpine", "latest", digest)

	run, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/alpine", digest)
	if err != nil {
		t.Fatalf("GetActiveScanRunByDigest() error = %v", err)
	}
	if run.Status != ports.ScanRunStatusQueued || run.Trigger != ports.ScanTriggerPush {
		t.Fatalf("run = %#v, want queued/push", run)
	}
}

func TestServiceQueuePushScanCreatesNothingWhenScanSettingsDisabled(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: false, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	digest := "sha256:" + strings.Repeat("b", 64)
	service.queuePushScan(context.Background(), "tenant-a", "library/alpine", "latest", digest)

	if _, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/alpine", digest); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetActiveScanRunByDigest() error = %v, want ErrorCodeNotFound (no run queued)", err)
	}
}

func TestServiceQueuePushScanDoesNotDuplicateAnInFlightRun(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	digest := "sha256:" + strings.Repeat("c", 64)
	now := time.Now().UTC()
	existing := ports.ScanRun{ID: "run-existing", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusRunning, Trigger: ports.ScanTriggerManual, CreatedAt: now, UpdatedAt: now}
	if err := service.metadata.UpsertScanRun(context.Background(), "tenant-a", existing); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}

	service.queuePushScan(context.Background(), "tenant-a", "library/alpine", "latest", digest)

	run, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/alpine", digest)
	if err != nil {
		t.Fatalf("GetActiveScanRunByDigest() error = %v", err)
	}
	if run.ID != existing.ID {
		t.Fatalf("run.ID = %q, want unchanged existing run %q (no duplicate queued)", run.ID, existing.ID)
	}
}

func TestServicePublishManifestReturnsBeforeScanCompletes(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	slowRunner := &blockingScanRunner{release: make(chan struct{})}
	defer close(slowRunner.release)
	service.SetScanRunner(slowRunner)

	upload, err := service.BeginUpload(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(context.Background(), "library/alpine", upload.ID, digestForTest([]byte("layer-one")), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)

	published, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload)
	if err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	if published.Digest == "" {
		t.Fatalf("published = %#v, want a resolved digest", published)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/alpine", published.Digest); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected a queued push scan run to exist for the published digest")
}

// blockingScanRunner blocks until release is closed, proving PublishManifest
// does not wait on scan completion — the queued push scan must be
// observable while the runner is still blocked.
type blockingScanRunner struct {
	release chan struct{}
}

func (b *blockingScanRunner) Run(ctx context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return ports.ScanResult{}, nil
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

func TestServiceGetScanPolicySettingsDefaultsToEnabledCriticalWithNoRowWritten(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	settings, err := service.GetScanPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("GetScanPolicySettings() error = %v", err)
	}
	if !settings.Enabled || settings.SeverityThreshold != ports.ScanPolicyThresholdCritical {
		t.Fatalf("settings = %#v, want enabled=true threshold=critical with zero rows written", settings)
	}
}

func TestServiceUpdateScanPolicySettingsPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	updated, err := service.UpdateScanPolicySettings(context.Background(), ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh})
	if err != nil {
		t.Fatalf("UpdateScanPolicySettings() error = %v", err)
	}
	if updated.Enabled || updated.SeverityThreshold != ports.ScanPolicyThresholdCriticalHigh {
		t.Fatalf("updated = %#v, want disabled/critical_high", updated)
	}

	stored, err := service.GetScanPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("GetScanPolicySettings() error = %v", err)
	}
	if stored.Enabled || stored.SeverityThreshold != ports.ScanPolicyThresholdCriticalHigh {
		t.Fatalf("stored = %#v, want disabled/critical_high", stored)
	}
}

func TestServiceOpenManifestBlocksPullOnCompletedViolatingScan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threshold string
		critical  int
		high      int
	}{
		{name: "critical at CRITICAL threshold", threshold: ports.ScanPolicyThresholdCritical, critical: 3, high: 0},
		{name: "high-only at CRITICAL_HIGH threshold", threshold: ports.ScanPolicyThresholdCriticalHigh, critical: 0, high: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			seedRepository(t, service, context.Background(), "library/alpine")
			digest := seededManifestDigest(t, service, "library/alpine")
			seedScanPolicyThreshold(t, service, tt.threshold)
			seedCompletedScanRun(t, service, "library/alpine", digest, tt.critical, tt.high)

			if _, err := service.OpenManifest(context.Background(), "library/alpine", "latest"); err == nil {
				t.Fatal("OpenManifest() error = nil, want policy violation")
			} else if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
				t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
			}
		})
	}
}

func TestServiceOpenManifestAllowsPullOnNonBlockingScanStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seed    func(t *testing.T, service *Service, digest string)
		enabled bool
	}{
		{name: "no scan run", seed: func(t *testing.T, service *Service, digest string) {}, enabled: true},
		{name: "queued", seed: func(t *testing.T, service *Service, digest string) {
			seedScanRunWithStatus(t, service, "library/alpine", digest, ports.ScanRunStatusQueued, 5, 0)
		}, enabled: true},
		{name: "running", seed: func(t *testing.T, service *Service, digest string) {
			seedScanRunWithStatus(t, service, "library/alpine", digest, ports.ScanRunStatusRunning, 5, 0)
		}, enabled: true},
		{name: "failed", seed: func(t *testing.T, service *Service, digest string) {
			seedScanRunWithStatus(t, service, "library/alpine", digest, ports.ScanRunStatusFailed, 5, 0)
		}, enabled: true},
		{name: "policy disabled with a completed violating scan", seed: func(t *testing.T, service *Service, digest string) {
			seedScanRunWithStatus(t, service, "library/alpine", digest, ports.ScanRunStatusCompleted, 5, 0)
		}, enabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			seedRepository(t, service, context.Background(), "library/alpine")
			digest := seededManifestDigest(t, service, "library/alpine")
			if !tt.enabled {
				seedScanPolicyEnabled(t, service, false)
			}
			tt.seed(t, service, digest)

			if _, err := service.OpenManifest(context.Background(), "library/alpine", "latest"); err != nil {
				t.Fatalf("OpenManifest() error = %v, want allowed pull", err)
			}
		})
	}
}

// TestServiceResolveManifestIgnoresPolicyGate proves the TUI browse path
// (ResolveManifest) is untouched by the pull gate — the same digest the
// gate blocks in OpenManifest must still resolve successfully.
func TestServiceResolveManifestIgnoresPolicyGate(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	digest := seededManifestDigest(t, service, "library/alpine")
	seedCompletedScanRun(t, service, "library/alpine", digest, 4, 0)

	if _, err := service.OpenManifest(context.Background(), "library/alpine", "latest"); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}

	if _, err := service.ResolveManifest(context.Background(), "library/alpine", "latest"); err != nil {
		t.Fatalf("ResolveManifest() error = %v, want the browse path unaffected by the pull gate", err)
	}
}

// TestServiceOpenManifestSigningPolicyDisabledIsByteIdenticalToScanOnlyBehavior
// is the Phase 5 RED test (tasks.md 5.1): a disabled signing policy must not
// change OpenManifest's existing behavior for either a signed or an unsigned
// digest (proposal Success Criterion 2).
func TestServiceOpenManifestSigningPolicyDisabledIsByteIdenticalToScanOnlyBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seed func(t *testing.T, service *Service, repository string)
	}{
		{
			name: "a signed digest",
			seed: func(t *testing.T, service *Service, repository string) {
				seedFixtureImageManifest(t, service, repository)
				seedFixtureSignatureArtifact(t, service, repository)
			},
		},
		{
			name: "an unsigned digest",
			seed: func(t *testing.T, service *Service, repository string) {
				seedFixtureImageManifest(t, service, repository)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			repository := "library/alpine"
			tt.seed(t, service, repository)
			// Signing policy left at its default {Enabled: false}.

			manifest, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest)
			if err != nil {
				t.Fatalf("OpenManifest() error = %v, want the pull unaffected by a disabled signing policy", err)
			}
			if manifest.Digest.String() != fixtureImageDigest {
				t.Fatalf("OpenManifest().Digest = %s, want %s", manifest.Digest.String(), fixtureImageDigest)
			}
		})
	}
}

// TestServiceOpenManifestAllowsPullWithSigningPolicyEnabledAndValidSignature
// is the Phase 5 RED test (tasks.md 5.2).
func TestServiceOpenManifestAllowsPullWithSigningPolicyEnabledAndValidSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedFixtureSignatureArtifact(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	if _, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest); err != nil {
		t.Fatalf("OpenManifest() error = %v, want the pull allowed", err)
	}
}

// TestServiceOpenManifestBlocksPullWithSigningPolicyEnabledAndNoSignature is
// the Phase 5 RED test (tasks.md 5.3): proves enforceSigningPolicy runs
// after enforceScanPolicy without either masking the other — the
// vulnerability gate has nothing to say here (no scan run at all, its
// fail-open default), so a block can only come from the signing gate.
func TestServiceOpenManifestBlocksPullWithSigningPolicyEnabledAndNoSignature(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	_, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("OpenManifest() error = nil, want a policy violation from the signing gate")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}
}

// TestServiceOpenManifestContrastsFailOpenScanGateWithFailClosedSigningGate
// is the Phase 5 RED test (tasks.md 5.4) — the spec's explicit scenario
// (image-signature-verification/spec.md "Contrast with the vulnerability
// gate's fail-open default"): a digest with neither a completed scan nor a
// verifiable signature, both gates enabled — the vulnerability gate must NOT
// block (fail-open, existing behavior unchanged) while the signing gate MUST
// block (fail-closed, the new behavior), exercising OpenManifest once.
func TestServiceOpenManifestContrastsFailOpenScanGateWithFailClosedSigningGate(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	// Vulnerability policy enabled, no scan run of any status seeded for
	// this digest — enforceScanPolicy's fail-open default (NotFound is not
	// an error, service_scanning.go:99-104).
	seedScanPolicyEnabled(t, service, true)
	// Signing policy enabled, no signature artifact seeded for this
	// digest — enforceSigningPolicy's fail-closed default.
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	_, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest)
	if err == nil {
		t.Fatal("OpenManifest() error = nil, want the signing gate to block despite the vulnerability gate allowing (fail-open vs. fail-closed contrast)")
	}
	if !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation from the signing gate", err)
	}
}

// TestServiceResolveManifestIgnoresSigningPolicyGate is the Phase 4/5 proof
// (tasks.md 4.11) that ResolveManifest — the browse path — is never gated by
// enforceSigningPolicy, mirroring TestServiceResolveManifestIgnoresPolicyGate
// for the vulnerability gate above. It necessarily lives here rather than in
// service_signing_test.go: enforceSigningPolicy is unreachable from any call
// site until this phase wires it into OpenManifest.
func TestServiceResolveManifestIgnoresSigningPolicyGate(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	seedFixtureImageManifest(t, service, repository)
	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	if _, err := service.OpenManifest(context.Background(), repository, fixtureImageDigest); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
		t.Fatalf("OpenManifest() error = %v, want ErrorCodePolicyViolation", err)
	}

	if _, err := service.ResolveManifest(context.Background(), repository, fixtureImageDigest); err != nil {
		t.Fatalf("ResolveManifest() error = %v, want the browse path unaffected by the signing gate", err)
	}
}

// overrideAwareScanRunner is a ports.ScanRunner test double whose Run()
// output depends on the settings it is called with, so tests can prove the
// real chain from a repository override through to the pull gate without a
// real Trivy binary: an IgnoreFilePath set on the settings simulates the
// ignore file excluding the CVE this fake would otherwise report (design.md
// Decision 9 steps 3-4, "suppressed vulnerabilities are absent from
// payload.Results").
type overrideAwareScanRunner struct {
	mu    sync.Mutex
	calls []ports.ScanSettings
}

func (r *overrideAwareScanRunner) Run(_ context.Context, _ string, settings ports.ScanSettings) (ports.ScanResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, settings)
	r.mu.Unlock()

	updatedAt := time.Now().UTC()
	if strings.TrimSpace(settings.IgnoreFilePath) != "" {
		return ports.ScanResult{TrivyVersion: "0.58.1", DBUpdatedAt: &updatedAt, Critical: 0, High: 0}, nil
	}
	return ports.ScanResult{TrivyVersion: "0.58.1", DBUpdatedAt: &updatedAt, Critical: 3, High: 0}, nil
}

func (r *overrideAwareScanRunner) Calls() []ports.ScanSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ports.ScanSettings(nil), r.calls...)
}

// TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository
// is the Phase 9 tasks 9.1/9.2 RED/integration test for
// repository-vulnerability-scans/spec.md's "Per-Repository Trivy Override
// Changes Are Isolated To That Repository's Pull-Gate Outcome" requirement
// (proposal's last Success Criterion; design.md Decision 9). It exercises
// the real chain end to end: SetRepositoryOverride -> applyRepositoryOverride
// (queue time) -> scanRunner.Run(settings) -> run.Critical/High ->
// GetLatestScanRunByDigest -> scanPolicyViolated -> OpenManifest — never
// seeding a ScanRun row directly.
func TestServiceRepositoryOverridePolicyCouplingIsolatesGateOutcomePerRepository(t *testing.T) {
	t.Parallel()

	t.Run("an ignore-file override changes the pull-gate outcome only after a rescan (9.1)", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		seedRepository(t, service, context.Background(), "library/alpine")
		if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
			t.Fatalf("EnsureScanSettings() error = %v", err)
		}
		seedManagedRuntimeState(t, service, "0.58.1")
		runner := &overrideAwareScanRunner{}
		service.SetScanRunner(runner)

		queued, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
		if err != nil {
			t.Fatalf("QueueManualScan() error = %v", err)
		}
		waitForScanRunStatus(t, service, "library/alpine", queued.ID, ports.ScanRunStatusCompleted)

		if _, err := service.OpenManifest(context.Background(), "library/alpine", "latest"); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
			t.Fatalf("OpenManifest() before override = %v, want ErrorCodePolicyViolation", err)
		}

		raw := []byte(`{"enabled":true,"ignore_file_path":"/etc/trivy/ignore-cve.yaml"}`)
		if _, err := service.SetRepositoryOverride(context.Background(), "library/alpine", "trivy", raw); err != nil {
			t.Fatalf("SetRepositoryOverride() error = %v", err)
		}

		// Only after a rescan (design.md Decision 9's first consequence):
		// setting the override alone does not re-evaluate the
		// already-completed run, so the digest already blocked stays
		// blocked.
		if _, err := service.OpenManifest(context.Background(), "library/alpine", "latest"); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
			t.Fatalf("OpenManifest() right after SetRepositoryOverride, before any rescan, = %v, want still ErrorCodePolicyViolation", err)
		}

		rescanned, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
		if err != nil {
			t.Fatalf("QueueManualScan() rescan error = %v", err)
		}
		waitForScanRunStatus(t, service, "library/alpine", rescanned.ID, ports.ScanRunStatusCompleted)

		if _, err := service.OpenManifest(context.Background(), "library/alpine", "latest"); err != nil {
			t.Fatalf("OpenManifest() after rescan under the override = %v, want the pull allowed (ignored finding no longer counted)", err)
		}
	})

	t.Run("the override affects only its own repository, a second repository stays byte-identical (9.2)", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		seedRepository(t, service, context.Background(), "library/alpine")
		seedRepository(t, service, context.Background(), "library/base")
		if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
			t.Fatalf("EnsureScanSettings() error = %v", err)
		}
		seedManagedRuntimeState(t, service, "0.58.1")
		runner := &overrideAwareScanRunner{}
		service.SetScanRunner(runner)

		if _, err := service.SetRepositoryOverride(context.Background(), "library/alpine", "trivy", []byte(`{"enabled":false}`)); err != nil {
			t.Fatalf("SetRepositoryOverride() error = %v", err)
		}

		if _, err := service.QueueManualScan(context.Background(), "library/alpine", "latest"); err == nil {
			t.Fatal("QueueManualScan(overridden, disabled) error = nil, want validation error")
		} else if !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Fatalf("QueueManualScan(overridden, disabled) error = %v, want ErrorCodeValidation", err)
		}

		queued, err := service.QueueManualScan(context.Background(), "library/base", "latest")
		if err != nil {
			t.Fatalf("QueueManualScan(non-overridden) error = %v", err)
		}
		waitForScanRunStatus(t, service, "library/base", queued.ID, ports.ScanRunStatusCompleted)

		if _, err := service.OpenManifest(context.Background(), "library/base", "latest"); !domain.IsCode(err, domain.ErrorCodePolicyViolation) {
			t.Fatalf("OpenManifest(library/base) error = %v, want ErrorCodePolicyViolation -- the other repository's finding counts and pull-gate outcome must remain unaffected by library/alpine's override", err)
		}

		calls := runner.Calls()
		if len(calls) != 1 {
			t.Fatalf("runner calls = %d, want exactly 1 -- the disabled overridden repository must never reach the runner", len(calls))
		}
		if calls[0].IgnoreFilePath != "" || calls[0].IgnorePolicyPath != "" {
			t.Fatalf("runner settings for library/base = %#v, want byte-identical to the no-override case (no override fields populated)", calls[0])
		}
	})
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

func TestServiceSetFeatureEnabledDoesNotLeakIntoAnotherFeaturesScanSettingsRow(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	originalFeatures := builtInFeatures
	builtInFeatures = append(append([]featureDescriptor{}, originalFeatures...), featureDescriptor{name: "gitleaks", kind: ports.FeatureKindBuiltin})
	defer func() { builtInFeatures = originalFeatures }()

	if _, err := service.SetFeatureEnabled(context.Background(), "trivy", false); err != nil {
		t.Fatalf("SetFeatureEnabled(trivy) error = %v", err)
	}
	if _, err := service.SetFeatureEnabled(context.Background(), "gitleaks", true); err != nil {
		t.Fatalf("SetFeatureEnabled(gitleaks) error = %v", err)
	}

	trivySettings, err := service.metadata.GetScanSettings(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("GetScanSettings(trivy) error = %v", err)
	}
	if trivySettings.Enabled {
		t.Fatalf("trivySettings.Enabled = true, want enabling gitleaks to leave trivy's own row untouched")
	}

	gitleaksSettings, err := service.metadata.GetScanSettings(context.Background(), "tenant-a", "gitleaks")
	if err != nil {
		t.Fatalf("GetScanSettings(gitleaks) error = %v", err)
	}
	if !gitleaksSettings.Enabled {
		t.Fatalf("gitleaksSettings.Enabled = false, want gitleaks' own row to reflect its enable call")
	}
}

func TestServiceProjectFeatureRuntimeReflectsOnlyEachFeaturesOwnManagerState(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	service.SetFeatureRuntimeManager("trivy", fakeFeatureRuntimeManager{statusState: ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady, ActiveVersion: "0.57.1"}, latestVersion: "0.58.0"})
	service.SetFeatureRuntimeManager("gitleaks", fakeFeatureRuntimeManager{statusState: ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusInstalling, ActiveVersion: "8.24.0"}, latestVersion: "8.25.0"})

	trivyRuntime := service.projectFeatureRuntime(context.Background(), "trivy")
	gitleaksRuntime := service.projectFeatureRuntime(context.Background(), "gitleaks")

	if trivyRuntime.Version != "0.57.1" || trivyRuntime.Status != string(ports.FeatureRuntimeStatusReady) {
		t.Fatalf("trivyRuntime = %#v, want trivy's own reported manager state", trivyRuntime)
	}
	if gitleaksRuntime.Version != "8.24.0" || gitleaksRuntime.Status != string(ports.FeatureRuntimeStatusInstalling) {
		t.Fatalf("gitleaksRuntime = %#v, want gitleaks' own reported manager state", gitleaksRuntime)
	}
	if trivyRuntime.Version == gitleaksRuntime.Version || trivyRuntime.Status == gitleaksRuntime.Status {
		t.Fatalf("trivyRuntime = %#v, gitleaksRuntime = %#v, want each feature's projection independent of the other's manager", trivyRuntime, gitleaksRuntime)
	}
}

func TestServiceListFeaturesReturnsBuiltinTrivyInventory(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetFeatureRuntimeManager("trivy", fakeFeatureRuntimeManager{statusState: ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady, ActiveVersion: "0.57.1", PreviousVersion: "0.56.2"}, latestVersion: "0.58.0"})

	features, err := service.ListFeatures(context.Background())
	if err != nil {
		t.Fatalf("ListFeatures() error = %v", err)
	}

	want := []ports.FeatureSummary{{
		Name:           "trivy",
		Kind:           ports.FeatureKindBuiltin,
		Enabled:        false,
		Configured:     false,
		CurrentVersion: "0.57.1",
		LatestVersion:  "0.58.0",
		UpdateStatus:   "available",
	}, {
		Name:           "gitleaks",
		Kind:           ports.FeatureKindBuiltin,
		Enabled:        false,
		Configured:     false,
		CurrentVersion: "",
		LatestVersion:  "unknown",
		UpdateStatus:   "unknown",
	}, {
		// signing (design.md Decision 10, tasks.md Phase 6): a builtin
		// feature with no managed runtime at all, not merely an uninstalled
		// one — CurrentVersion/LatestVersion/UpdateStatus stay at their
		// unmanaged zero/"unknown" projection, the same shape gitleaks has
		// here before any manager is registered for it.
		Name:           "signing",
		Kind:           ports.FeatureKindBuiltin,
		Enabled:        false,
		Configured:     false,
		CurrentVersion: "",
		LatestVersion:  "unknown",
		UpdateStatus:   "unknown",
	}}
	if !reflect.DeepEqual(features, want) {
		t.Fatalf("ListFeatures() = %#v, want %#v", features, want)
	}
}

func TestBuildFeaturePageKeepsMinimalNonTrivyPagesLightweight(t *testing.T) {
	t.Parallel()

	summary := ports.FeatureSummary{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: true, Configured: true}
	details := ports.FeatureDetails{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: true, Configured: true}

	page := buildFeaturePage(summary, details, nil)

	if page.Summary != summary {
		t.Fatalf("page.Summary = %#v, want %#v", page.Summary, summary)
	}
	if len(page.Header) == 0 {
		t.Fatalf("page.Header = %#v, want lightweight header fields", page.Header)
	}
	if len(page.Sections) != 0 {
		t.Fatalf("page.Sections = %#v, want no Trivy-only sections for minimal feature page", page.Sections)
	}
	if len(page.Actions) != 0 {
		t.Fatalf("page.Actions = %#v, want no implicit actions for minimal feature page", page.Actions)
	}
}

func TestServiceGetFeaturePageBuildsRuntimeOnlyTrivySectionsAndDeclaredActions(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.ConfigureFeature(context.Background(), "trivy", ports.FeatureConfigureInput{
		Enabled:              boolPtr(true),
		ScheduleEnabled:      boolPtr(true),
		Interval:             durationPtr(6 * time.Hour),
		Timeout:              durationPtr(10 * time.Minute),
		RegistryReachableURL: stringPtr("https://registry.internal:5443"),
		MaxConcurrency:       intPtr(2),
	}); err != nil {
		t.Fatalf("ConfigureFeature() error = %v", err)
	}
	verifiedAt := time.Now().UTC().Add(-time.Minute)
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		PreviousVersion:  "0.56.2",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		ReceiptPath:      "/var/lib/regixtry/features/trivy/receipts/0.57.1.json",
		LastVerifiedAt:   &verifiedAt,
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
	}
	service.SetFeatureRuntimeManager("trivy", fakeFeatureRuntimeManager{statusState: ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady, ActiveVersion: "0.57.1", PreviousVersion: "0.56.2"}, latestVersion: "0.58.0"})

	page, err := service.GetFeaturePage(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeaturePage() error = %v", err)
	}

	if got, want := sectionIDs(page.Sections), []string{"config", "runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("section IDs = %#v, want %#v", got, want)
	}
	if got, want := actionIDs(page.Actions), []string{"refresh", "disable", "upgrade-runtime", "rollback-runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("action IDs = %#v, want %#v", got, want)
	}
	if len(page.Sections) != 2 {
		t.Fatalf("page.Sections = %#v, want runtime-only page shape", page.Sections)
	}
}

// TestServiceGetFeaturePageBuildsRuntimeSectionsForGitleaksIndependentlyFromTrivy
// is the Phase 6 RED test (tasks.md 6.2/operator-admin-tui "Per-Feature
// Runtime Status Surface"): buildFeaturePage must project Config+Runtime
// sections for gitleaks too (not just Trivy, task 6.1's generalization),
// and each feature's own (tenant, feature)-keyed runtime state must be
// shown independently — a Gitleaks page must never leak Trivy's version
// or vice versa.
func TestServiceGetFeaturePageBuildsRuntimeSectionsForGitleaksIndependentlyFromTrivy(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedManagedRuntimeState(t, service, "0.57.1")
	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "gitleaks", ports.ScanSettings{
		Enabled:        true,
		Interval:       time.Hour,
		Timeout:        time.Minute,
		MaxConcurrency: 1,
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings(gitleaks) error = %v", err)
	}
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "gitleaks", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "8.27.0",
		PreviousVersion:  "8.26.0",
		ActiveBinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(gitleaks) error = %v", err)
	}

	trivyPage, err := service.GetFeaturePage(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeaturePage(trivy) error = %v", err)
	}
	gitleaksPage, err := service.GetFeaturePage(context.Background(), "gitleaks")
	if err != nil {
		t.Fatalf("GetFeaturePage(gitleaks) error = %v", err)
	}

	if got, want := sectionIDs(gitleaksPage.Sections), []string{"config", "runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("gitleaks section IDs = %#v, want %#v", got, want)
	}

	trivyVersion := featureFieldValue(trivyPage.Sections, "runtime", "Version")
	gitleaksVersion := featureFieldValue(gitleaksPage.Sections, "runtime", "Version")
	if trivyVersion != "0.57.1" || gitleaksVersion != "8.27.0" {
		t.Fatalf("trivy version = %q, gitleaks version = %q, want each feature's own independent runtime state", trivyVersion, gitleaksVersion)
	}
	if got := featureFieldValue(gitleaksPage.Sections, "runtime", "Rollback Available"); got != "true" {
		t.Fatalf("gitleaks rollback available = %q, want %q", got, "true")
	}
}

func featureFieldValue(sections []ports.FeatureSection, sectionID string, label string) string {
	for _, section := range sections {
		if section.ID != sectionID {
			continue
		}
		for _, field := range section.Fields {
			if field.Label == label {
				return field.Value
			}
		}
	}
	return ""
}

func TestServiceGetFeatureStatusFallsBackToUnknownLatestVersion(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetFeatureRuntimeManager("trivy", fakeFeatureRuntimeManager{statusState: ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady, ActiveVersion: "0.57.1", PreviousVersion: "0.56.2"}, latestErr: errors.New("lookup failed")})

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if got, want := status.Runtime.Version, "0.57.1"; got != want {
		t.Fatalf("status.Runtime.Version = %q, want %q", got, want)
	}
	if got, want := status.Runtime.LatestVersion, "unknown"; got != want {
		t.Fatalf("status.Runtime.LatestVersion = %q, want %q", got, want)
	}
	if got, want := status.Runtime.UpdateStatus, "unknown"; got != want {
		t.Fatalf("status.Runtime.UpdateStatus = %q, want %q", got, want)
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
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		PreviousVersion:  "0.56.2",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         "/var/lib/regixtry/features/trivy/trivy-cache",
		ReceiptPath:      "/var/lib/regixtry/features/trivy/receipts/0.57.1.json",
		LastVerifiedAt:   &verifiedAt,
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
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
		Enabled:              boolPtr(true),
		ScheduleEnabled:      boolPtr(true),
		Interval:             durationPtr(6 * time.Hour),
		Timeout:              durationPtr(10 * time.Minute),
		RegistryReachableURL: stringPtr("https://registry.example.com"),
		MaxConcurrency:       intPtr(2),
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
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status:            ports.FeatureRuntimeStatusReady,
		ActiveVersion:     "0.57.1",
		PreviousVersion:   "0.56.2",
		ActiveBinaryPath:  filepath.Join(t.TempDir(), "features", "trivy", "bin", "active", "trivy"),
		CacheDir:          filepath.Join(t.TempDir(), "features", "trivy", "trivy-cache"),
		ReceiptPath:       filepath.Join(t.TempDir(), "features", "trivy", "receipts", "0.57.1.json"),
		LastVerifiedAt:    &verifiedAt,
		LastHealthCheckAt: &healthAt,
		UpdatedAt:         healthAt,
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
	}

	status, err := service.GetFeatureStatus(context.Background(), "trivy")
	if err != nil {
		t.Fatalf("GetFeatureStatus() error = %v", err)
	}
	if status.Runtime.Mode != ports.FeatureRuntimeModeManaged || status.Runtime.Status != string(ports.FeatureRuntimeStatusReady) {
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
	service.SetFeatureRuntimeManager("trivy", fakeFeatureRuntimeManager{statusState: ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusMigrationRequired, MigrationHint: `legacy binary_path "/tmp/README.sh" requires managed reinstall and will never be executed`}, latestErr: errors.New("lookup failed")})

	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "trivy", ports.ScanSettings{
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
	if status.Runtime.Status != string(ports.FeatureRuntimeStatusMigrationRequired) || status.Runtime.Health != "migration-required" {
		t.Fatalf("status.Runtime = %#v, want migration-required runtime", status.Runtime)
	}
	if !strings.Contains(status.Runtime.Detail, "/tmp/README.sh") {
		t.Fatalf("status.Runtime.Detail = %q, want legacy binary evidence", status.Runtime.Detail)
	}
	if status.Runtime.Mode != ports.FeatureRuntimeModeManaged {
		t.Fatalf("status.Runtime.Mode = %q, want managed runtime truth even for migration", status.Runtime.Mode)
	}
	if got, want := status.Runtime.LatestVersion, "unknown"; got != want {
		t.Fatalf("status.Runtime.LatestVersion = %q, want %q", got, want)
	}
	if got, want := status.Runtime.UpdateStatus, "unknown"; got != want {
		t.Fatalf("status.Runtime.UpdateStatus = %q, want %q", got, want)
	}
}

func TestServiceManualAndScheduledScansUseManagedRuntimeStateAndIgnoreLegacyPaths(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "trivy", ports.ScanSettings{
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
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         "/var/lib/regixtry/features/trivy/trivy-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
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

func TestServiceExecuteScanRunRunsSecretScanLegAlongsideTrivyLegWhenGitleaksEnabledAndReady(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "trivy", ports.ScanSettings{
		Enabled:              true,
		Interval:             time.Hour,
		Timeout:              time.Minute,
		RegistryReachableURL: "https://registry.internal",
		MaxConcurrency:       1,
		UpdatedAt:            time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings(trivy) error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")

	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "gitleaks", ports.ScanSettings{
		Enabled:        true,
		Interval:       time.Hour,
		Timeout:        time.Minute,
		MaxConcurrency: 1,
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings(gitleaks) error = %v", err)
	}
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "gitleaks", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "8.27.0",
		ActiveBinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks",
		CacheDir:         "/var/lib/regixtry/features/gitleaks/gitleaks-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(gitleaks) error = %v", err)
	}

	trivyRunner := &capturingScanRunner{result: ports.ScanResult{TrivyVersion: "0.57.1"}}
	service.SetScanRunner(trivyRunner)
	secretRunner := &capturingSecretScanRunner{result: ports.SecretScanResult{
		GitleaksVersion: "8.27.0",
		Findings:        []ports.SecretFinding{{RuleID: "aws-access-token", BlobDigest: "sha256:" + strings.Repeat("a", 64), Path: "config.json", StartLine: 1, EndLine: 1}},
	}}
	service.SetSecretScanRunner(secretRunner)

	manual, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	waitForScanCompletion(t, service, manual.ID)
	waitForSecretScanRunnerCalls(t, secretRunner, 1)

	secretRunner.mu.Lock()
	targets := append([]ports.SecretScanTarget(nil), secretRunner.targets...)
	settingsSeen := append([]ports.ScanSettings(nil), secretRunner.settings...)
	secretRunner.mu.Unlock()
	if len(targets) != 1 {
		t.Fatalf("secret runner targets = %#v, want exactly one call alongside the Trivy leg", targets)
	}
	if targets[0].Repository != "library/alpine" || targets[0].Digest != manual.Digest {
		t.Fatalf("secret target = %#v, want repository/digest matching the scan run", targets[0])
	}
	if len(targets[0].Blobs) == 0 {
		t.Fatalf("secret target.Blobs = %#v, want manifest config+layer blobs", targets[0].Blobs)
	}
	if settingsSeen[0].BinaryPath != "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks" {
		t.Fatalf("secret settings = %#v, want managed gitleaks binary path", settingsSeen[0])
	}

	waitForSecretScanRunCompletion(t, service, "library/alpine")
	runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].Status != ports.SecretScanRunStatusCompleted {
		t.Fatalf("secret scan runs = %#v, want exactly one completed run", runs)
	}
	detail, err := service.metadata.GetSecretScanRunDetail(context.Background(), "tenant-a", runs[0].ID)
	if err != nil {
		t.Fatalf("GetSecretScanRunDetail() error = %v", err)
	}
	if len(detail.Findings) != 1 || detail.Findings[0].RuleID != "aws-access-token" {
		t.Fatalf("secret findings = %#v, want persisted redacted finding", detail.Findings)
	}
}

// TestServiceGetSecretScanFindingsReturnsPersistedFindingsForImageAndNotFoundOtherwise
// is the Phase 6 RED test backing the secret-findings-by-image endpoint
// (tasks.md 6.1/6.2): findings must be attributable to one image
// (repository@digest) and a repository with no secret scan run yet must
// report not-found rather than an empty/misleading success.
func TestServiceGetSecretScanFindingsReturnsPersistedFindingsForImageAndNotFoundOtherwise(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if err := service.metadata.UpsertSecretScanRunDetail(context.Background(), "tenant-a", ports.SecretScanRunDetail{
		Run: ports.SecretScanRun{
			ID:              "secret-run-1",
			Repository:      "library/alpine",
			Digest:          "sha256:" + strings.Repeat("a", 64),
			Status:          ports.SecretScanRunStatusCompleted,
			Trigger:         ports.ScanTriggerManual,
			GitleaksVersion: "8.27.0",
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
		Findings: []ports.SecretFinding{{RuleID: "aws-access-token", BlobDigest: "sha256:" + strings.Repeat("b", 64), Path: "config.json", StartLine: 3, EndLine: 3}},
	}); err != nil {
		t.Fatalf("UpsertSecretScanRunDetail() error = %v", err)
	}

	detail, err := service.GetSecretScanFindings(context.Background(), "library/alpine", "sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("GetSecretScanFindings() error = %v", err)
	}
	if len(detail.Findings) != 1 || detail.Findings[0].RuleID != "aws-access-token" {
		t.Fatalf("detail.Findings = %#v, want the persisted redacted finding", detail.Findings)
	}

	if _, err := service.GetSecretScanFindings(context.Background(), "library/alpine", "sha256:"+strings.Repeat("c", 64)); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetSecretScanFindings(unknown digest) error = %v, want not-found", err)
	}
	if _, err := service.GetSecretScanFindings(context.Background(), "library/other", "sha256:"+strings.Repeat("a", 64)); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetSecretScanFindings(unknown repository) error = %v, want not-found", err)
	}
}

func TestServiceExecuteScanRunSkipsSecretScanLegWithoutBlockingTrivyWhenGitleaksDisabledOrNotReady(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "trivy", ports.ScanSettings{
		Enabled:              true,
		Interval:             time.Hour,
		Timeout:              time.Minute,
		RegistryReachableURL: "https://registry.internal",
		MaxConcurrency:       1,
		UpdatedAt:            time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings(trivy) error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	// gitleaks intentionally left unconfigured (disabled/uninstalled by default).

	trivyRunner := &capturingScanRunner{result: ports.ScanResult{TrivyVersion: "0.57.1"}}
	service.SetScanRunner(trivyRunner)
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)

	manual, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("QueueManualScan() error = %v", err)
	}
	waitForScanCompletion(t, service, manual.ID)

	trivyRun, err := service.metadata.GetScanRun(context.Background(), "tenant-a", manual.ID)
	if err != nil {
		t.Fatalf("GetScanRun() error = %v", err)
	}
	if trivyRun.Status != ports.ScanRunStatusCompleted {
		t.Fatalf("trivy run status = %q, want completed regardless of gitleaks state", trivyRun.Status)
	}

	// Give the secret leg goroutine time to run and confirm it did nothing:
	// an absent/disabled gitleaks feature must never fail or block the Trivy leg.
	time.Sleep(50 * time.Millisecond)
	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("secret runner calls = %d, want 0 when gitleaks is not enabled/ready", calls)
	}
	runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("secret scan runs = %#v, want none persisted when gitleaks is not enabled/ready", runs)
	}
}

// TestServicePublishManifestDoesNotTriggerSecretScan is the regression test
// for spec.md "Reused Rescan Trigger, No Push-Time Path": secret scans MUST
// only run through the existing manual/scheduled rescan orchestration
// (executeScanRun), never as a direct consequence of PublishManifest. This
// verifies the actual code path — PublishManifest never calls
// executeScanRun, QueueManualScan, or RunScheduledScans — rather than
// assuming it from the design doc.
func TestServicePublishManifestDoesNotTriggerSecretScan(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", "gitleaks", ports.ScanSettings{
		Enabled:        true,
		Interval:       time.Hour,
		Timeout:        time.Minute,
		MaxConcurrency: 1,
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings(gitleaks) error = %v", err)
	}
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "gitleaks", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "8.27.0",
		ActiveBinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks",
		CacheDir:         "/var/lib/regixtry/features/gitleaks/gitleaks-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(gitleaks) error = %v", err)
	}
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)

	// Only publish the manifest (the push path) — never call QueueManualScan
	// or RunScheduledScans.
	seedRepository(t, service, context.Background(), "library/pushed-only")

	// Give any accidental async trigger time to run before asserting absence.
	time.Sleep(50 * time.Millisecond)
	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("secret runner calls = %d, want 0: an image push MUST NOT trigger a secret scan directly", calls)
	}
	runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", "library/pushed-only", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("secret scan runs = %#v, want none: push alone must not create a secret scan run", runs)
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

func TestServiceGetScanRunDetailPreservesDigestTruthAndDerivesReferenceFreshness(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	resolved, err := service.ResolveManifest(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("ResolveManifest(initial) error = %v", err)
	}

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	if err := service.metadata.UpsertScanRunDetail(context.Background(), "tenant-a", ports.ScanRunDetail{
		Run:         ports.ScanRun{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Digest: resolved.Digest, Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: now, UpdatedAt: now, Critical: 1, TrivyVersion: "0.58.1"},
		Findings:    []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-0001", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}},
		DBFreshness: ports.ScanRunDBFreshness{FreshnessState: ports.ScanRunDBFreshnessStateFresh},
	}); err != nil {
		t.Fatalf("UpsertScanRunDetail() error = %v", err)
	}

	publishRepositoryTag(t, service, context.Background(), "library/alpine", "latest", "layer-two")

	detail, err := service.GetScanRunDetail(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("GetScanRunDetail() error = %v", err)
	}
	if got, want := detail.Run.Digest, resolved.Digest; got != want {
		t.Fatalf("detail.Run.Digest = %q, want %q", got, want)
	}
	if got, want := detail.ReferenceFreshness, ports.ScanReferenceFreshnessMoved; got != want {
		t.Fatalf("ReferenceFreshness = %q, want %q", got, want)
	}
	if got, want := len(detail.Findings), 1; got != want {
		t.Fatalf("len(detail.Findings) = %d, want %d", got, want)
	}
	if got, want := detail.Findings[0].VulnerabilityID, "CVE-2026-0001"; got != want {
		t.Fatalf("finding vulnerability = %q, want %q", got, want)
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
		// PublishManifest fires a background queuePushScan goroutine on
		// every call; draining it here before closing the store avoids a
		// goroutine racing t.TempDir()'s cleanup after the test returns.
		service.WaitForBackgroundWork()
		_ = metadataStore.Close()
	}
}

type allowAllAccessController struct{}

type fakeFeatureRuntimeManager struct {
	statusState   ports.FeatureRuntimeState
	latestVersion string
	latestErr     error
}

func (f fakeFeatureRuntimeManager) Install(context.Context, string, func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	return ports.FeatureRuntimeState{}, nil
}

func (f fakeFeatureRuntimeManager) Upgrade(context.Context, string, func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	return ports.FeatureRuntimeState{}, nil
}

func (f fakeFeatureRuntimeManager) Rollback(context.Context) (ports.FeatureRuntimeState, error) {
	return ports.FeatureRuntimeState{}, nil
}

func (f fakeFeatureRuntimeManager) Status(context.Context) (ports.FeatureRuntimeState, error) {
	return f.statusState, nil
}

func (f fakeFeatureRuntimeManager) LatestVersion(context.Context) (string, error) {
	return f.latestVersion, f.latestErr
}

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
	// Drain PublishManifest's fire-and-forget queuePushScan goroutine before
	// returning: at this point no scan settings are configured yet, so it
	// resolves to a no-op, but without draining it here its unpredictable
	// scheduling could otherwise race a caller that configures scan
	// settings and triggers its own scan immediately afterward.
	service.WaitForBackgroundWork()
}

// publishManifestWithTags publishes one manifest and tags it with each name
// in tags, all pointing at the same digest -- shared by the delete tests:
// three tags on one digest exercises the digest-delete cascade, two tags on
// one digest exercises tag-delete sibling isolation. Returns the details
// from the last PublishManifest call; the digest is identical across calls
// since every tag reuses the same blob/manifest payload.
func publishManifestWithTags(t *testing.T, service *Service, ctx context.Context, repository string, tags ...string) ManifestDetails {
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

	var published ManifestDetails
	for _, tag := range tags {
		published, err = service.PublishManifest(ctx, repository, tag, "application/vnd.oci.image.manifest.v1+json", manifestPayload)
		if err != nil {
			t.Fatalf("PublishManifest(%q, %q) error = %v", repository, tag, err)
		}
	}
	service.WaitForBackgroundWork()

	return published
}

// seededManifestDigest resolves the digest published by seedRepository for
// the "latest" tag, so gate tests can seed scan runs keyed to the exact
// digest OpenManifest will resolve.
func seededManifestDigest(t *testing.T, service *Service, repository string) string {
	t.Helper()

	details, err := service.ResolveManifest(context.Background(), repository, "latest")
	if err != nil {
		t.Fatalf("ResolveManifest(%q) error = %v", repository, err)
	}
	return details.Digest
}

func seedScanPolicyThreshold(t *testing.T, service *Service, threshold string) {
	t.Helper()

	if err := service.metadata.UpsertScanPolicySettings(context.Background(), "tenant-a", ports.ScanPolicySettings{Enabled: true, SeverityThreshold: threshold, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanPolicySettings() error = %v", err)
	}
}

func seedScanPolicyEnabled(t *testing.T, service *Service, enabled bool) {
	t.Helper()

	if err := service.metadata.UpsertScanPolicySettings(context.Background(), "tenant-a", ports.ScanPolicySettings{Enabled: enabled, SeverityThreshold: ports.ScanPolicyThresholdCritical, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("UpsertScanPolicySettings() error = %v", err)
	}
}

func seedCompletedScanRun(t *testing.T, service *Service, repository string, digest string, critical int, high int) {
	t.Helper()
	seedScanRunWithStatus(t, service, repository, digest, ports.ScanRunStatusCompleted, critical, high)
}

func seedScanRunWithStatus(t *testing.T, service *Service, repository string, digest string, status string, critical int, high int) {
	t.Helper()

	now := time.Now().UTC()
	run := ports.ScanRun{ID: "run-" + status + "-" + digest, Repository: repository, RequestedRef: "latest", Digest: digest, Status: status, Trigger: ports.ScanTriggerManual, CreatedAt: now, UpdatedAt: now, Critical: critical, High: high}
	if err := service.metadata.UpsertScanRun(context.Background(), "tenant-a", run); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }

func durationPtr(value time.Duration) *time.Duration { return &value }

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

func sectionIDs(sections []ports.FeatureSection) []string {
	ids := make([]string, 0, len(sections))
	for _, section := range sections {
		ids = append(ids, section.ID)
	}
	return ids
}

func actionIDs(actions []ports.FeatureAction) []string {
	ids := make([]string, 0, len(actions))
	for _, action := range actions {
		ids = append(ids, action.ID)
	}
	return ids
}

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

type capturingSecretScanRunner struct {
	mu       sync.Mutex
	result   ports.SecretScanResult
	err      error
	settings []ports.ScanSettings
	targets  []ports.SecretScanTarget
}

func (c *capturingSecretScanRunner) Run(_ context.Context, target ports.SecretScanTarget, settings ports.ScanSettings) (ports.SecretScanResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = append(c.targets, target)
	c.settings = append(c.settings, settings)
	return c.result, c.err
}

func waitForSecretScanRunnerCalls(t *testing.T, runner *capturingSecretScanRunner, want int) {
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
	t.Fatalf("secret scan runner calls did not reach %d before deadline", want)
}

func waitForSecretScanRunCompletion(t *testing.T, service *Service, repository string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", repository, 10)
		if err == nil {
			for _, run := range runs {
				if run.Status == ports.SecretScanRunStatusCompleted || run.Status == ports.SecretScanRunStatusFailed {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("secret scan run for %s did not reach a terminal status before deadline", repository)
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
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    version,
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:         "/var/lib/regixtry/features/trivy/trivy-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
	}
}

func publishRepositoryTag(t *testing.T, service *Service, ctx context.Context, repository string, tag string, layer string) {
	t.Helper()

	upload, err := service.BeginUpload(ctx, repository)
	if err != nil {
		t.Fatalf("BeginUpload(%q) error = %v", repository, err)
	}
	if _, err := service.AppendUpload(ctx, repository, upload.ID, strings.NewReader(layer)); err != nil {
		t.Fatalf("AppendUpload(%q) error = %v", repository, err)
	}
	blobPayload := []byte(layer)
	blob, err := service.CompleteUpload(ctx, repository, upload.ID, digestForTest(blobPayload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload(%q) error = %v", repository, err)
	}
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)
	if _, err := service.PublishManifest(ctx, repository, tag, "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest(%q) error = %v", repository, err)
	}
	// See seedRepository: drain the fire-and-forget push-scan goroutine so
	// its unpredictable scheduling cannot race a caller that configures
	// scan settings right after publishing.
	service.WaitForBackgroundWork()
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

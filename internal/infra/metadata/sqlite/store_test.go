package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func TestStorePublishResolveCatalogAndTags(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")
	blobs := []domain.Descriptor{
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer-1")), Size: int64(len("layer-1"))},
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer-2")), Size: int64(len("layer-2"))},
	}

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "latest", manifest, blobs); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	resolvedByTag, err := store.ResolveManifest(context.Background(), "tenant-a", repo, "latest")
	if err != nil {
		t.Fatalf("ResolveManifest(tag) error = %v", err)
	}

	if resolvedByTag.Digest != manifest.Digest {
		t.Fatalf("resolvedByTag.Digest = %s, want %s", resolvedByTag.Digest, manifest.Digest)
	}

	resolvedByDigest, err := store.ResolveManifest(context.Background(), "tenant-a", repo, manifest.Digest.String())
	if err != nil {
		t.Fatalf("ResolveManifest(digest) error = %v", err)
	}

	if resolvedByDigest.Digest != manifest.Digest {
		t.Fatalf("resolvedByDigest.Digest = %s, want %s", resolvedByDigest.Digest, manifest.Digest)
	}

	catalog, err := store.Catalog(context.Background(), "tenant-a", 10, "")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	if len(catalog) != 1 || catalog[0].String() != repo.String() {
		t.Fatalf("catalog = %#v, want [%s]", catalog, repo)
	}

	tags, err := store.ListTags(context.Background(), "tenant-a", repo, 10, "")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}

	if len(tags) != 1 || tags[0] != "latest" {
		t.Fatalf("tags = %#v, want [latest]", tags)
	}

	linkedBlobs, err := store.ListManifestBlobs(context.Background(), "tenant-a", repo, manifest.Digest)
	if err != nil {
		t.Fatalf("ListManifestBlobs() error = %v", err)
	}

	if len(linkedBlobs) != len(blobs) {
		t.Fatalf("len(linkedBlobs) = %d, want %d", len(linkedBlobs), len(blobs))
	}
}

func TestStorePersistsUploadMetadataAcrossReopen(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	upload := domain.UploadState{
		ID:         "upload-1",
		Repository: domain.MustParseRepositoryRef("library/alpine"),
		Status:     domain.UploadStatusActive,
		Size:       42,
		StartedAt:  time.Now().UTC().Add(-time.Minute),
		UpdatedAt:  time.Now().UTC(),
		Location:   "/tmp/uploads/upload-1/data",
	}

	if err := store.SaveUpload(context.Background(), "tenant-a", upload); err != nil {
		t.Fatalf("SaveUpload() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	storedUpload, err := reopened.GetUpload(context.Background(), "tenant-a", upload.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}

	if storedUpload.Repository.String() != upload.Repository.String() || storedUpload.Size != upload.Size || storedUpload.Location != upload.Location {
		t.Fatalf("stored upload = %#v, want %#v", storedUpload, upload)
	}
}

func TestStoreEnablesSQLiteWALAndBusyTimeout(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRowContext(context.Background(), `PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode error = %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRowContext(context.Background(), `PRAGMA busy_timeout;`).Scan(&busyTimeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout error = %v", err)
	}
	if busyTimeout != sqliteBusyTimeoutMillis {
		t.Fatalf("busy_timeout = %d, want %d", busyTimeout, sqliteBusyTimeoutMillis)
	}
}

func TestStorePersistsDefaultDisabledScanSettings(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	settings := ports.ScanSettings{
		Enabled:               false,
		ScheduleEnabled:       false,
		Interval:              24 * time.Hour,
		Timeout:               15 * time.Minute,
		ServiceURL:            "https://scanner.example.com",
		RegistryReachableURL:  "https://registry.internal:5443",
		AuthToken:             "secret-token",
		TLSCACertPath:         "/etc/regixtry/trivy-ca.pem",
		TLSInsecureSkipVerify: true,
		MaxConcurrency:        1,
	}
	if err := store.UpsertScanSettings(context.Background(), "tenant-a", "trivy", settings); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}

	stored, err := store.GetScanSettings(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if stored.Enabled || stored.ScheduleEnabled {
		t.Fatalf("stored = %#v, want disabled defaults", stored)
	}
	if stored.ServiceURL != settings.ServiceURL || stored.RegistryReachableURL != settings.RegistryReachableURL || stored.AuthToken != settings.AuthToken || stored.TLSCACertPath != settings.TLSCACertPath || stored.TLSInsecureSkipVerify != settings.TLSInsecureSkipVerify || stored.MaxConcurrency != settings.MaxConcurrency {
		t.Fatalf("stored = %#v, want %#v", stored, settings)
	}
}

func TestStoreGetScanPolicySettingsReturnsNotFoundWithNoRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetScanPolicySettings(context.Background(), "tenant-a")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetScanPolicySettings() error = %v, want ErrorCodeNotFound", err)
	}
}

func TestStoreUpsertScanPolicySettingsRoundTripsEnabledAndThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		enabled   bool
		threshold string
	}{
		{name: "enabled critical", enabled: true, threshold: ports.ScanPolicyThresholdCritical},
		{name: "disabled critical_high", enabled: false, threshold: ports.ScanPolicyThresholdCriticalHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			defer store.Close()

			settings := ports.ScanPolicySettings{Enabled: tt.enabled, SeverityThreshold: tt.threshold, UpdatedAt: time.Now().UTC()}
			if err := store.UpsertScanPolicySettings(context.Background(), "tenant-a", settings); err != nil {
				t.Fatalf("UpsertScanPolicySettings() error = %v", err)
			}

			stored, err := store.GetScanPolicySettings(context.Background(), "tenant-a")
			if err != nil {
				t.Fatalf("GetScanPolicySettings() error = %v", err)
			}
			if stored.Enabled != tt.enabled || stored.SeverityThreshold != tt.threshold {
				t.Fatalf("stored = %#v, want enabled=%v threshold=%s", stored, tt.enabled, tt.threshold)
			}

			settings.Enabled = !tt.enabled
			if err := store.UpsertScanPolicySettings(context.Background(), "tenant-a", settings); err != nil {
				t.Fatalf("UpsertScanPolicySettings(update) error = %v", err)
			}
			updated, err := store.GetScanPolicySettings(context.Background(), "tenant-a")
			if err != nil {
				t.Fatalf("GetScanPolicySettings(update) error = %v", err)
			}
			if updated.Enabled == stored.Enabled {
				t.Fatalf("updated.Enabled = %v, want flipped from %v", updated.Enabled, stored.Enabled)
			}
		})
	}
}

func TestStoreGetRepositoryFeatureOverrideReturnsNotFoundWithNoRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryFeatureOverride() error = %v, want ErrorCodeNotFound", err)
	}
}

func TestStoreUpsertRepositoryFeatureOverrideRoundTripsPayloadAndUpdatedAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	payload := []byte(`{"enabled":false,"ignore_file_path":"/etc/regixtry/ignore.txt"}`)
	updatedAt := time.Now().UTC()

	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", payload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
	}

	stored, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride() error = %v", err)
	}
	if !reflect.DeepEqual(stored, payload) {
		t.Fatalf("stored payload = %s, want %s", stored, payload)
	}

	overrides, err := store.ListRepositoryFeatureOverrides(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("ListRepositoryFeatureOverrides() error = %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("len(overrides) = %d, want 1", len(overrides))
	}
	if overrides[0].Repository != "library/alpine" || overrides[0].Feature != "trivy" {
		t.Fatalf("overrides[0] = %#v, want repository=library/alpine feature=trivy", overrides[0])
	}
	if !reflect.DeepEqual([]byte(overrides[0].Payload), payload) {
		t.Fatalf("overrides[0].Payload = %s, want %s", overrides[0].Payload, payload)
	}
	if overrides[0].UpdatedAt.Before(updatedAt.Add(-time.Minute)) || overrides[0].UpdatedAt.After(updatedAt.Add(time.Minute)) {
		t.Fatalf("overrides[0].UpdatedAt = %v, want close to %v", overrides[0].UpdatedAt, updatedAt)
	}

	// Upsert again with a different payload -> same row updates in place.
	updatedPayload := []byte(`{"enabled":true}`)
	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", updatedPayload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(update) error = %v", err)
	}
	updatedStored, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride(update) error = %v", err)
	}
	if !reflect.DeepEqual(updatedStored, updatedPayload) {
		t.Fatalf("updatedStored = %s, want %s", updatedStored, updatedPayload)
	}
}

func TestStoreRepositoryFeatureOverrideIsolatesEachFeatureNameAsIndependentRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	trivyPayload := []byte(`{"enabled":false}`)
	gitleaksPayload := []byte(`{"enabled":true,"config_path":"/etc/regixtry/gitleaks.toml"}`)
	// A third, entirely fabricated feature name must round-trip with no
	// migration — the table is feature-agnostic (design.md Decision 1).
	imageSigningPayload := []byte(`{"enabled":true,"key_path":"/etc/regixtry/cosign.pub"}`)

	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", trivyPayload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(trivy) error = %v", err)
	}
	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "gitleaks", gitleaksPayload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(gitleaks) error = %v", err)
	}
	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "image-signing", imageSigningPayload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(image-signing) error = %v", err)
	}

	storedTrivy, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride(trivy) error = %v", err)
	}
	if !reflect.DeepEqual(storedTrivy, trivyPayload) {
		t.Fatalf("storedTrivy = %s, want %s", storedTrivy, trivyPayload)
	}

	storedGitleaks, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "gitleaks")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride(gitleaks) error = %v", err)
	}
	if !reflect.DeepEqual(storedGitleaks, gitleaksPayload) {
		t.Fatalf("storedGitleaks = %s, want %s", storedGitleaks, gitleaksPayload)
	}

	storedImageSigning, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "image-signing")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride(image-signing) error = %v", err)
	}
	if !reflect.DeepEqual(storedImageSigning, imageSigningPayload) {
		t.Fatalf("storedImageSigning = %s, want %s", storedImageSigning, imageSigningPayload)
	}
}

func TestStoreDeleteRepositoryFeatureOverride(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	err := store.DeleteRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteRepositoryFeatureOverride(absent) error = %v, want ErrorCodeNotFound", err)
	}

	payload := []byte(`{"enabled":false}`)
	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", payload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
	}

	if err := store.DeleteRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy"); err != nil {
		t.Fatalf("DeleteRepositoryFeatureOverride() error = %v", err)
	}

	_, err = store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryFeatureOverride(after delete) error = %v, want ErrorCodeNotFound", err)
	}

	overrides, err := store.ListRepositoryFeatureOverrides(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("ListRepositoryFeatureOverrides(after delete) error = %v", err)
	}
	if len(overrides) != 0 {
		t.Fatalf("overrides = %#v, want empty after delete", overrides)
	}
}

func TestStoreBridgesLegacyBinaryColumnsWhenServiceFieldsAreMissing(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.db.ExecContext(context.Background(), `
		INSERT INTO scan_settings (tenant, enabled, schedule_enabled, interval, timeout, cache_dir, binary_path, max_concurrency, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "tenant-a", true, true, (3 * time.Hour).String(), (17 * time.Minute).String(), "/var/cache/trivy", "/tmp/README.sh", 4, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert legacy row error = %v", err)
	}

	stored, err := store.GetScanSettings(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if stored.ServiceURL != "" || stored.RegistryReachableURL != "" {
		t.Fatalf("stored = %#v, want service fields empty for legacy bridge row", stored)
	}
	if stored.Interval != 3*time.Hour || stored.Timeout != 17*time.Minute || stored.MaxConcurrency != 4 || !stored.Enabled || !stored.ScheduleEnabled {
		t.Fatalf("stored = %#v, want shared knobs preserved from legacy row", stored)
	}
}

func TestStorePersistsScanRunsAndSchedulerStateAcrossReopen(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	finishedAt := time.Now().UTC()
	dbUpdatedAt := finishedAt.Add(-time.Hour)
	runs := []ports.ScanRun{
		{ID: "run-completed", Repository: "library/alpine", RequestedRef: "latest", Digest: domain.DigestFromBytes([]byte("manifest-1")).String(), Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, StartedAt: &finishedAt, FinishedAt: &finishedAt, Critical: 1, High: 2, Medium: 3, Low: 4, TrivyVersion: "0.54.0", DBUpdatedAt: &dbUpdatedAt},
		{ID: "run-failed", Repository: "library/alpine", RequestedRef: "1.0", Digest: domain.DigestFromBytes([]byte("manifest-2")).String(), Status: ports.ScanRunStatusFailed, Trigger: ports.ScanTriggerManual, FinishedAt: &finishedAt, Error: "boom"},
	}
	for _, run := range runs {
		if err := store.UpsertScanRun(context.Background(), "tenant-a", run); err != nil {
			t.Fatalf("UpsertScanRun(%s) error = %v", run.ID, err)
		}
	}

	state := ports.ScanSchedulerState{OwnerID: "node-a", LeaseExpiresAt: finishedAt.Add(time.Minute), LastHeartbeatAt: finishedAt, BatchStartedAt: &finishedAt}
	if err := store.UpsertScanSchedulerState(context.Background(), "tenant-a", state); err != nil {
		t.Fatalf("UpsertScanSchedulerState() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	storedRuns, err := reopened.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	if len(storedRuns) != 2 {
		t.Fatalf("len(storedRuns) = %d, want 2", len(storedRuns))
	}
	if storedRuns[0].Status != ports.ScanRunStatusCompleted || storedRuns[1].Status != ports.ScanRunStatusFailed {
		t.Fatalf("storedRuns = %#v, want severity-first ordering before failed summaries", storedRuns)
	}

	storedState, err := reopened.GetScanSchedulerState(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetScanSchedulerState() error = %v", err)
	}
	if storedState.OwnerID != state.OwnerID || storedState.BatchStartedAt == nil {
		t.Fatalf("storedState = %#v, want %#v", storedState, state)
	}
}

func TestStorePersistsFeatureRuntimeStateAcrossReopenAndDerivesLegacyMigrationState(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	verifiedAt := time.Now().UTC().Add(-2 * time.Minute)
	healthAt := verifiedAt.Add(time.Minute)
	dbUpdatedAt := healthAt.Add(-30 * time.Second)
	state := ports.FeatureRuntimeState{
		Status:            ports.FeatureRuntimeStatusReady,
		ActiveVersion:     "0.57.1",
		PreviousVersion:   "0.56.2",
		ActiveBinaryPath:  "/var/lib/regixtry/features/trivy/bin/active/trivy",
		CacheDir:          "/var/lib/regixtry/features/trivy/trivy-cache",
		ReceiptPath:       "/var/lib/regixtry/features/trivy/receipts/0.57.1.json",
		LastVerifiedAt:    &verifiedAt,
		LastHealthCheckAt: &healthAt,
		LastDBUpdatedAt:   &dbUpdatedAt,
		UpdatedAt:         healthAt,
	}
	if err := store.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", state); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	stored, err := reopened.GetFeatureRuntimeState(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState() error = %v", err)
	}
	if stored.Status != state.Status || stored.ActiveVersion != state.ActiveVersion || stored.PreviousVersion != state.PreviousVersion {
		t.Fatalf("stored = %#v, want persisted runtime identity %#v", stored, state)
	}
	if stored.ActiveBinaryPath != state.ActiveBinaryPath || stored.CacheDir != state.CacheDir || stored.ReceiptPath != state.ReceiptPath {
		t.Fatalf("stored = %#v, want persisted managed paths %#v", stored, state)
	}
	if stored.LastVerifiedAt == nil || !stored.LastVerifiedAt.Equal(verifiedAt) || stored.LastHealthCheckAt == nil || !stored.LastHealthCheckAt.Equal(healthAt) {
		t.Fatalf("stored = %#v, want persisted verification timestamps", stored)
	}

	legacyStore := newTestStore(t)
	defer legacyStore.Close()
	_, err = legacyStore.db.ExecContext(context.Background(), `
		INSERT INTO scan_settings (tenant, enabled, schedule_enabled, interval, timeout, cache_dir, binary_path, service_url, registry_reachable_url, max_concurrency, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "tenant-b", true, true, (6 * time.Hour).String(), (10 * time.Minute).String(), "/var/cache/trivy", "/tmp/README.sh", "https://scanner.example.com", "https://registry.internal", 2, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert legacy scan settings error = %v", err)
	}

	legacyState, err := legacyStore.GetFeatureRuntimeState(context.Background(), "tenant-b", "trivy")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState(legacy) error = %v", err)
	}
	if legacyState.Status != ports.FeatureRuntimeStatusMigrationRequired {
		t.Fatalf("legacyState.Status = %q, want migration-required", legacyState.Status)
	}
	if legacyState.ActiveBinaryPath != "" {
		t.Fatalf("legacyState.ActiveBinaryPath = %q, want empty because legacy paths must not be executable runtime authority", legacyState.ActiveBinaryPath)
	}
	if !strings.Contains(legacyState.MigrationHint, "/tmp/README.sh") {
		t.Fatalf("legacyState.MigrationHint = %q, want legacy binary evidence", legacyState.MigrationHint)
	}
}

func TestStoreFeatureRuntimeStateIsolatesEachFeaturesOwnRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	trivyState := ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "0.57.1",
		ActiveBinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy",
		UpdatedAt:        time.Now().UTC(),
	}
	if err := store.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "trivy", trivyState); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(trivy) error = %v", err)
	}

	gitleaksState := ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusInstalling,
		ActiveVersion:    "8.24.0",
		ActiveBinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks",
		UpdatedAt:        time.Now().UTC(),
	}
	if err := store.UpsertFeatureRuntimeState(context.Background(), "tenant-a", "gitleaks", gitleaksState); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(gitleaks) error = %v", err)
	}

	storedTrivy, err := store.GetFeatureRuntimeState(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState(trivy) error = %v", err)
	}
	if storedTrivy.Status != ports.FeatureRuntimeStatusReady || storedTrivy.ActiveVersion != "0.57.1" || storedTrivy.ActiveBinaryPath != trivyState.ActiveBinaryPath {
		t.Fatalf("storedTrivy = %#v, want trivy's own row untouched by gitleaks writes", storedTrivy)
	}

	storedGitleaks, err := store.GetFeatureRuntimeState(context.Background(), "tenant-a", "gitleaks")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState(gitleaks) error = %v", err)
	}
	if storedGitleaks.Status != ports.FeatureRuntimeStatusInstalling || storedGitleaks.ActiveVersion != "8.24.0" || storedGitleaks.ActiveBinaryPath != gitleaksState.ActiveBinaryPath {
		t.Fatalf("storedGitleaks = %#v, want gitleaks' own row untouched by trivy writes", storedGitleaks)
	}
}

func TestStorePersistsScanRunDetailAcrossReopenAndOrdersBySeverityThenFixability(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	details := []ports.ScanRunDetail{
		{
			Run:         ports.ScanRun{ID: "run-fixable-critical", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:111", Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: now, UpdatedAt: now, Critical: 1, High: 0, Medium: 0, Low: 0, TrivyVersion: "0.58.1"},
			Findings:    []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-1", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}},
			DBFreshness: ports.ScanRunDBFreshness{TrivyVersion: "0.58.1", DBVersion: 7, FreshnessState: ports.ScanRunDBFreshnessStateFresh},
		},
		{
			Run:      ports.ScanRun{ID: "run-unfixable-critical", Repository: "library/alpine", RequestedRef: "1.0", Digest: "sha256:222", Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute), Critical: 1, High: 0, Medium: 0, Low: 0, TrivyVersion: "0.58.1"},
			Findings: []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2", PackageName: "busybox", InstalledVersion: "1.0.0", Fixable: false}},
		},
		{
			Run:      ports.ScanRun{ID: "run-high-fixable", Repository: "library/alpine", RequestedRef: "2.0", Digest: "sha256:333", Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute), Critical: 0, High: 1, Medium: 0, Low: 0, TrivyVersion: "0.58.1"},
			Findings: []ports.ScanRunFinding{{Severity: "HIGH", VulnerabilityID: "CVE-3", PackageName: "curl", InstalledVersion: "8.0.0", FixedVersion: "8.0.1", Fixable: true}},
		},
	}
	for _, detail := range details {
		if err := store.UpsertScanRunDetail(context.Background(), "tenant-a", detail); err != nil {
			t.Fatalf("UpsertScanRunDetail(%s) error = %v", detail.Run.ID, err)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	runs, err := reopened.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	if got, want := []string{runs[0].ID, runs[1].ID, runs[2].ID}, []string{"run-fixable-critical", "run-unfixable-critical", "run-high-fixable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("run order = %#v, want %#v", got, want)
	}

	detail, err := reopened.GetScanRunDetail(context.Background(), "tenant-a", "run-unfixable-critical")
	if err != nil {
		t.Fatalf("GetScanRunDetail() error = %v", err)
	}
	if got, want := len(detail.Findings), 1; got != want {
		t.Fatalf("len(detail.Findings) = %d, want %d", got, want)
	}
	if detail.Findings[0].Fixable {
		t.Fatalf("finding = %#v, want non-fixable finding to remain visible", detail.Findings[0])
	}
	if got, want := detail.DBFreshness.FreshnessState, ports.ScanRunDBFreshnessStateUnknown; got != want {
		t.Fatalf("FreshnessState = %q, want %q when no row was stored", got, want)
	}
}

func TestStorePersistsSecretScanRunDetailAcrossReopenWithoutSecretMaterial(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "registry.db")
	store, err := New(databasePath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	detail := ports.SecretScanRunDetail{
		Run: ports.SecretScanRun{ID: "secret-run-1", Repository: "library/alpine", Digest: "sha256:abc", Status: ports.SecretScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, StartedAt: &now, FinishedAt: &now, CreatedAt: now, UpdatedAt: now, GitleaksVersion: "8.27.0"},
		Findings: []ports.SecretFinding{
			{RuleID: "aws-access-token", Description: "AWS Access Token", BlobDigest: "sha256:layerdigest", Path: "fake-secrets.txt", StartLine: 1, EndLine: 1, Tags: []string{"aws"}},
		},
	}
	if err := store.UpsertSecretScanRunDetail(context.Background(), "tenant-a", detail); err != nil {
		t.Fatalf("UpsertSecretScanRunDetail() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(databasePath)
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	defer reopened.Close()

	storedDetail, err := reopened.GetSecretScanRunDetail(context.Background(), "tenant-a", "secret-run-1")
	if err != nil {
		t.Fatalf("GetSecretScanRunDetail() error = %v", err)
	}
	if storedDetail.Run.Status != ports.SecretScanRunStatusCompleted || storedDetail.Run.GitleaksVersion != "8.27.0" {
		t.Fatalf("storedDetail.Run = %#v, want persisted run identity", storedDetail.Run)
	}
	if storedDetail.Run.FindingCount != 1 {
		t.Fatalf("storedDetail.Run.FindingCount = %d, want 1", storedDetail.Run.FindingCount)
	}
	if got, want := len(storedDetail.Findings), 1; got != want {
		t.Fatalf("len(storedDetail.Findings) = %d, want %d", got, want)
	}
	finding := storedDetail.Findings[0]
	if finding.RuleID != "aws-access-token" || finding.BlobDigest != "sha256:layerdigest" || finding.Path != "fake-secrets.txt" {
		t.Fatalf("finding = %#v, want persisted rule/location fields", finding)
	}
	if len(finding.Tags) != 1 || finding.Tags[0] != "aws" {
		t.Fatalf("finding.Tags = %#v, want [aws]", finding.Tags)
	}

	runs, err := reopened.ListSecretScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "secret-run-1" || runs[0].FindingCount != 1 {
		t.Fatalf("runs = %#v, want one listed run with FindingCount 1", runs)
	}
}

func TestStoreGetLatestScanRunByDigestReturnsNewestRunRegardlessOfStatus(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	digest := domain.DigestFromBytes([]byte("manifest-latest")).String()
	base := time.Now().UTC().Add(-time.Hour)
	queued := ports.ScanRun{ID: "run-queued", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusQueued, Trigger: ports.ScanTriggerManual, CreatedAt: base, UpdatedAt: base}
	completed := ports.ScanRun{ID: "run-completed", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute), Critical: 2}
	failed := ports.ScanRun{ID: "run-failed", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusFailed, Trigger: ports.ScanTriggerManual, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)}

	for _, run := range []ports.ScanRun{queued, completed, failed} {
		if err := store.UpsertScanRun(context.Background(), "tenant-a", run); err != nil {
			t.Fatalf("UpsertScanRun(%s) error = %v", run.ID, err)
		}
	}

	latest, err := store.GetLatestScanRunByDigest(context.Background(), "tenant-a", "library/alpine", digest)
	if err != nil {
		t.Fatalf("GetLatestScanRunByDigest() error = %v", err)
	}
	if latest.ID != failed.ID {
		t.Fatalf("latest.ID = %q, want %q (newest run regardless of status, including a completed run in between)", latest.ID, failed.ID)
	}
}

// TestStoreGetLatestScanRunByDigestBreaksCreatedAtTiesByInsertOrder is
// sdd-apply's live-DB confirmation from design.md's Open Questions:
// ORDER BY created_at DESC alone is not stable when two runs share an
// identical created_at value to nanosecond precision — SQLite falls back to
// an implementation-specific tiebreak (observed: the first-inserted row),
// which can return a stale run instead of the actually-latest one. rowid
// DESC as a secondary sort key makes "latest insert wins" deterministic and
// correct, since rowid strictly increases with each insert regardless of
// timestamp collisions.
func TestStoreGetLatestScanRunByDigestBreaksCreatedAtTiesByInsertOrder(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	tied := time.Now().UTC()
	older := ports.ScanRun{ID: "run-tied-older", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:tied-digest", Status: ports.ScanRunStatusFailed, Trigger: ports.ScanTriggerManual, CreatedAt: tied, UpdatedAt: tied}
	newer := ports.ScanRun{ID: "run-tied-newer", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:tied-digest", Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: tied, UpdatedAt: tied, Critical: 5}

	if err := store.UpsertScanRun(context.Background(), "tenant-a", older); err != nil {
		t.Fatalf("UpsertScanRun(older) error = %v", err)
	}
	if err := store.UpsertScanRun(context.Background(), "tenant-a", newer); err != nil {
		t.Fatalf("UpsertScanRun(newer) error = %v", err)
	}

	got, err := store.GetLatestScanRunByDigest(context.Background(), "tenant-a", "library/alpine", "sha256:tied-digest")
	if err != nil {
		t.Fatalf("GetLatestScanRunByDigest() error = %v", err)
	}
	if got.ID != newer.ID {
		t.Fatalf("GetLatestScanRunByDigest() = %#v, want the more-recently-inserted run %#v when created_at ties", got, newer)
	}
}

func TestStoreGetLatestScanRunByDigestReturnsTypedNotFoundWithNoRun(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetLatestScanRunByDigest(context.Background(), "tenant-a", "library/alpine", "sha256:0000000000000000000000000000000000000000000000000000000000aa")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetLatestScanRunByDigest() error = %v, want ErrorCodeNotFound", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return store
}

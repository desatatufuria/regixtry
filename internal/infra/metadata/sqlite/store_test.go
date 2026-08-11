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

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return store
}

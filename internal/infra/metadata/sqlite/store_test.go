package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
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

// TestStoreDeleteManifestByDigestCascadesTagsAndManifestBlobs covers
// manifest-deletion/spec.md's "Delete By Digest Cascades To Tags And
// Manifest Blobs" requirement: deleting a digest with three tags removes the
// manifest row, all three tags, and its manifest_blobs rows, and returns
// exactly the three removed tag names (selected inside the same transaction,
// before the delete, per design.md Decision 1/3).
func TestStoreDeleteManifestByDigestCascadesTagsAndManifestBlobs(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")
	blobs := []domain.Descriptor{
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer-1")), Size: int64(len("layer-1"))},
	}
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	tagNames := []string{"latest", "v1", "v2"}
	for _, tag := range tagNames {
		if err := store.PublishManifest(ctx, "tenant-a", repo, tag, manifest, blobs); err != nil {
			t.Fatalf("PublishManifest(%s) error = %v", tag, err)
		}
	}

	removedTags, err := store.DeleteManifestByDigest(ctx, "tenant-a", repo, manifest.Digest)
	if err != nil {
		t.Fatalf("DeleteManifestByDigest() error = %v", err)
	}

	sort.Strings(removedTags)
	if !reflect.DeepEqual(removedTags, tagNames) {
		t.Fatalf("removedTags = %#v, want %#v", removedTags, tagNames)
	}

	if _, err := store.ResolveManifest(ctx, "tenant-a", repo, manifest.Digest.String()); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("ResolveManifest(by digest, after delete) error = %v, want ErrorCodeNotFound", err)
	}

	for _, tag := range tagNames {
		if _, err := store.ResolveManifest(ctx, "tenant-a", repo, tag); !domain.IsCode(err, domain.ErrorCodeNotFound) {
			t.Fatalf("ResolveManifest(tag %s, after delete) error = %v, want ErrorCodeNotFound", tag, err)
		}
	}

	remainingBlobs, err := store.ListManifestBlobs(ctx, "tenant-a", repo, manifest.Digest)
	if err != nil {
		t.Fatalf("ListManifestBlobs(after delete) error = %v", err)
	}
	if len(remainingBlobs) != 0 {
		t.Fatalf("remainingBlobs = %#v, want empty (manifest_blobs cascade-deleted)", remainingBlobs)
	}
}

// TestStoreDeleteManifestByDigestReturnsNotFoundWithNoManifest covers
// manifest-deletion/spec.md's "Unknown digest returns MANIFEST_UNKNOWN"
// scenario at the store layer: zero rows affected surfaces as a typed
// domain.ErrorCodeNotFound, mirroring DeleteUpload/
// DeleteRepositoryFeatureOverride.
func TestStoreDeleteManifestByDigestReturnsNotFoundWithNoManifest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")
	_, err := store.DeleteManifestByDigest(context.Background(), "tenant-a", repo, domain.DigestFromBytes([]byte("absent-digest")))
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteManifestByDigest(absent) error = %v, want ErrorCodeNotFound", err)
	}
}

// TestStoreDeleteTagRemovesOnlyNamedTagLeavingManifestAndSiblingsIntact
// covers manifest-deletion/spec.md's "Delete By Tag Untags Without Touching
// The Manifest" requirement: deleting one tag removes only that tags row;
// the manifest and every other tag pointing at it must still resolve.
func TestStoreDeleteTagRemovesOnlyNamedTagLeavingManifestAndSiblingsIntact(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")
	blobs := []domain.Descriptor{
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: domain.DigestFromBytes([]byte("layer-1")), Size: int64(len("layer-1"))},
	}
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	for _, tag := range []string{"a", "b"} {
		if err := store.PublishManifest(ctx, "tenant-a", repo, tag, manifest, blobs); err != nil {
			t.Fatalf("PublishManifest(%s) error = %v", tag, err)
		}
	}

	if err := store.DeleteTag(ctx, "tenant-a", repo, "a"); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}

	if _, err := store.ResolveManifest(ctx, "tenant-a", repo, "a"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("ResolveManifest(a, after delete) error = %v, want ErrorCodeNotFound", err)
	}

	resolvedByTag, err := store.ResolveManifest(ctx, "tenant-a", repo, "b")
	if err != nil {
		t.Fatalf("ResolveManifest(b) error = %v, want success (sibling tag survives)", err)
	}
	if resolvedByTag.Digest != manifest.Digest {
		t.Fatalf("resolvedByTag.Digest = %s, want %s", resolvedByTag.Digest, manifest.Digest)
	}

	resolvedByDigest, err := store.ResolveManifest(ctx, "tenant-a", repo, manifest.Digest.String())
	if err != nil {
		t.Fatalf("ResolveManifest(digest) error = %v, want success (manifest survives)", err)
	}
	if resolvedByDigest.Digest != manifest.Digest {
		t.Fatalf("resolvedByDigest.Digest = %s, want %s", resolvedByDigest.Digest, manifest.Digest)
	}
}

// TestStoreDeleteTagReturnsNotFoundWithNoSuchTag covers manifest-deletion/
// spec.md's "Unknown tag returns MANIFEST_UNKNOWN" scenario at the store
// layer.
func TestStoreDeleteTagReturnsNotFoundWithNoSuchTag(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")
	err := store.DeleteTag(context.Background(), "tenant-a", repo, "absent")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteTag(absent) error = %v, want ErrorCodeNotFound", err)
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

// TestStoreEnablesSQLiteForeignKeyEnforcement pins the cascade-delete premise
// (design.md's DeleteManifestByDigest/DeleteTag rely on the manifests/tags/
// manifest_blobs ON DELETE CASCADE FKs actually firing) the same way
// TestStoreEnablesSQLiteWALAndBusyTimeout pins journal_mode/busy_timeout:
// `_pragma=foreign_keys(1)` is already set in sqliteDSN (store.go:42), so
// this is an already-GREEN guard against that premise silently regressing,
// not a state that must flip.
func TestStoreEnablesSQLiteForeignKeyEnforcement(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	var foreignKeys int
	if err := store.db.QueryRowContext(context.Background(), `PRAGMA foreign_keys;`).Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
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

// TestStoreGetSigningPolicySettingsReturnsNotFoundWithNoRow is the Phase 2
// RED test (tasks.md 2.3): an absent signing_policy_settings row is a typed
// domain.ErrorCodeNotFound, mirroring GetScanPolicySettings's own row-absence
// behavior — the fail-closed default itself is applied one layer up, at the
// service (design.md Decision 4).
func TestStoreGetSigningPolicySettingsReturnsNotFoundWithNoRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetSigningPolicySettings(context.Background(), "tenant-a")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetSigningPolicySettings() error = %v, want ErrorCodeNotFound", err)
	}
}

// TestStoreUpsertSigningPolicySettingsRoundTripsEnabledKeysAndUpdatedAt is the
// Phase 2 RED test (tasks.md 2.4): Enabled, TrustedPublicKeys (order
// preserved), and UpdatedAt (time.RFC3339Nano) must round-trip through
// Upsert/Get exactly, mirroring TestStoreUpsertScanPolicySettingsRoundTripsEnabledAndThreshold.
func TestStoreUpsertSigningPolicySettingsRoundTripsEnabledKeysAndUpdatedAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	updatedAt := time.Now().UTC()
	settings := ports.SigningPolicySettings{
		Enabled:           true,
		TrustedPublicKeys: []string{"-----BEGIN PUBLIC KEY-----\nkey-one\n-----END PUBLIC KEY-----", "-----BEGIN PUBLIC KEY-----\nkey-two\n-----END PUBLIC KEY-----"},
		UpdatedAt:         updatedAt,
	}
	if err := store.UpsertSigningPolicySettings(context.Background(), "tenant-a", settings); err != nil {
		t.Fatalf("UpsertSigningPolicySettings() error = %v", err)
	}

	stored, err := store.GetSigningPolicySettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetSigningPolicySettings() error = %v", err)
	}
	if !stored.Enabled {
		t.Fatalf("stored.Enabled = %v, want true", stored.Enabled)
	}
	if !reflect.DeepEqual(stored.TrustedPublicKeys, settings.TrustedPublicKeys) {
		t.Fatalf("stored.TrustedPublicKeys = %#v, want %#v (order preserved)", stored.TrustedPublicKeys, settings.TrustedPublicKeys)
	}
	if !stored.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("stored.UpdatedAt = %v, want %v", stored.UpdatedAt, updatedAt)
	}

	settings.Enabled = false
	settings.TrustedPublicKeys = nil
	settings.UpdatedAt = time.Now().UTC()
	if err := store.UpsertSigningPolicySettings(context.Background(), "tenant-a", settings); err != nil {
		t.Fatalf("UpsertSigningPolicySettings(update) error = %v", err)
	}
	updated, err := store.GetSigningPolicySettings(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetSigningPolicySettings(update) error = %v", err)
	}
	if updated.Enabled {
		t.Fatalf("updated.Enabled = %v, want false", updated.Enabled)
	}
	if len(updated.TrustedPublicKeys) != 0 {
		t.Fatalf("updated.TrustedPublicKeys = %#v, want empty", updated.TrustedPublicKeys)
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

// TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage
// covers repository-config-overrides/spec.md's "Override Rows Are Not
// Cascade-Deleted On Repository Lifecycle Changes" requirement — proxy
// coverage given no repository deletion/rename operation exists in this
// codebase yet. Since no delete/rename API exists to directly exercise "the
// old repository is gone", the closest testable proxy is proving resolution
// is a strict exact-name lookup: an override upserted for one repository
// name MUST NOT be observable when querying a different repository name
// (simulating the old name's row going inert after a hypothetical
// delete/rename), with no fuzzy/prefix/fallback matching that could leak an
// old override onto an unrelated or renamed-to repository.
func TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	oldPayload := []byte(`{"enabled":false}`)
	if err := store.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy", oldPayload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(old name) error = %v", err)
	}

	// A different repository name — simulating the repository having been
	// renamed or deleted and replaced — must resolve as NotFound, never as
	// the old override, even though "library/alpine-new" shares a prefix
	// with the stored key.
	_, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine-new", "trivy")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetRepositoryFeatureOverride(renamed-to name) error = %v, want ErrorCodeNotFound (no orphan leakage)", err)
	}

	// The old row remains untouched and still resolves under its exact
	// original name — it is inert, not cleaned up, per the requirement.
	stored, err := store.GetRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", "trivy")
	if err != nil {
		t.Fatalf("GetRepositoryFeatureOverride(old name) error = %v", err)
	}
	if !reflect.DeepEqual(stored, oldPayload) {
		t.Fatalf("stored = %s, want %s (old row must remain inert, not cascade-deleted)", stored, oldPayload)
	}

	// List is scoped by exact match only — the "renamed" repository must not
	// appear in the listing derived from the old row.
	overrides, err := store.ListRepositoryFeatureOverrides(context.Background(), "tenant-a", "trivy")
	if err != nil {
		t.Fatalf("ListRepositoryFeatureOverrides() error = %v", err)
	}
	for _, o := range overrides {
		if o.Repository == "library/alpine-new" {
			t.Fatalf("overrides = %#v, want no entry for the renamed-to repository (orphan leakage)", overrides)
		}
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

// seedHeavilyRescannedRepository inserts count distinct, strictly increasing
// (by CreatedAt) completed scan runs for repository, each with a real
// critical finding so it dominates ListScanRuns' severity-first ordering.
// Returns the ID of the LAST (most recently created) run inserted.
func seedHeavilyRescannedRepository(t *testing.T, store *Store, ctx context.Context, repository string, base time.Time, count int) string {
	t.Helper()
	var lastID string
	for i := 0; i < count; i++ {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		lastID = fmt.Sprintf("%s-run-%02d", strings.ReplaceAll(repository, "/", "-"), i)
		run := ports.ScanRun{
			ID:           lastID,
			Repository:   repository,
			RequestedRef: "latest",
			Digest:       domain.DigestFromBytes([]byte(lastID)).String(),
			Status:       ports.ScanRunStatusCompleted,
			Trigger:      ports.ScanTriggerManual,
			CreatedAt:    createdAt,
			FinishedAt:   &createdAt,
			Critical:     1,
		}
		if err := store.UpsertScanRun(ctx, "tenant-a", run); err != nil {
			t.Fatalf("UpsertScanRun(%s) error = %v", lastID, err)
		}
	}
	return lastID
}

// TestStoreListLatestScanRunPerRepositoryCollapsesBeforeLimitSoNoRepositoryIsCrowdedOut
// is the RED test reproducing the Repository Alerts crowd-out bug (found
// live: a 25-row LIMIT on raw scan_runs, ordered severity-first, let 2-3
// heavily-rescanned high-severity repositories fill the entire window and
// hide every OTHER repository's rows entirely, even ones with real,
// completed, 0-critical/0-high scans). ListLatestScanRunPerRepository must
// collapse to one row per repository BEFORE applying limit, so a quiet
// repository with a single clean scan can never be crowded out by a hot
// repository's rescans.
func TestStoreListLatestScanRunPerRepositoryCollapsesBeforeLimitSoNoRepositoryIsCrowdedOut(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)

	// Two "hot" repositories, 5 rescans each (10 raw scan_runs), all
	// critical -- exactly the shape that used to fill a small LIMIT window
	// on its own.
	hotALatestID := seedHeavilyRescannedRepository(t, store, ctx, "team/hot-a", base, 5)
	hotBLatestID := seedHeavilyRescannedRepository(t, store, ctx, "team/hot-b", base.Add(time.Hour), 5)

	// One "quiet" repository: a single, real, completed, 0-critical/0-high
	// scan -- created after both hot repositories' runs, so a naive
	// created_at-DESC LIMIT over raw rows would also have buried it.
	quietFinishedAt := base.Add(3 * time.Hour)
	quietRun := ports.ScanRun{
		ID: "quiet-run", Repository: "team/quiet", RequestedRef: "latest",
		Digest: domain.DigestFromBytes([]byte("quiet")).String(),
		Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual,
		CreatedAt: quietFinishedAt, FinishedAt: &quietFinishedAt,
	}
	if err := store.UpsertScanRun(ctx, "tenant-a", quietRun); err != nil {
		t.Fatalf("UpsertScanRun(quiet-run) error = %v", err)
	}

	// A LIMIT well below the 10 raw scan_runs rows, but >= the 3 distinct
	// repositories -- proves collapsing happens before limiting, not after.
	summaries, err := store.ListLatestScanRunPerRepository(ctx, "tenant-a", 3)
	if err != nil {
		t.Fatalf("ListLatestScanRunPerRepository() error = %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("len(summaries) = %d, want 3 (one row per repository, none crowded out)", len(summaries))
	}

	byRepo := make(map[string]ports.RepositoryScanSummary, len(summaries))
	for _, summary := range summaries {
		byRepo[summary.Run.Repository] = summary
	}

	quiet, ok := byRepo["team/quiet"]
	if !ok {
		t.Fatalf("summaries = %#v, want team/quiet present despite team/hot-a and team/hot-b's rescans", summaries)
	}
	if quiet.Run.ID != "quiet-run" || quiet.RunCount != 1 {
		t.Fatalf("quiet summary = %#v, want its single run with RunCount 1", quiet)
	}

	hotA, ok := byRepo["team/hot-a"]
	if !ok {
		t.Fatal("summaries missing team/hot-a")
	}
	if hotA.RunCount != 5 {
		t.Fatalf("team/hot-a RunCount = %d, want 5 (every rescan counted, only the latest run shown)", hotA.RunCount)
	}
	if hotA.Run.ID != hotALatestID {
		t.Fatalf("team/hot-a Run.ID = %q, want the most recently created run %q, not an older rescan", hotA.Run.ID, hotALatestID)
	}

	hotB, ok := byRepo["team/hot-b"]
	if !ok {
		t.Fatal("summaries missing team/hot-b")
	}
	if hotB.Run.ID != hotBLatestID {
		t.Fatalf("team/hot-b Run.ID = %q, want the most recently created run %q", hotB.Run.ID, hotBLatestID)
	}

	// Severity-first ordering survives collapsing: both critical repos sort
	// ahead of the quiet, 0-critical one.
	if summaries[len(summaries)-1].Run.Repository != "team/quiet" {
		t.Fatalf("summaries = %#v, want the 0-critical repository ordered last", summaries)
	}
}

// TestStoreListLatestScanRunPerRepositoryHonorsLimitAcrossManyRepositories is
// a companion RED test: with more distinct repositories than the limit,
// exactly `limit` distinct repositories come back, worst-severity first.
func TestStoreListLatestScanRunPerRepositoryHonorsLimitAcrossManyRepositories(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		id := fmt.Sprintf("run-%d", i)
		run := ports.ScanRun{
			ID: id, Repository: fmt.Sprintf("team/repo-%d", i), RequestedRef: "latest",
			Digest: domain.DigestFromBytes([]byte(id)).String(),
			Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual,
			CreatedAt: createdAt, FinishedAt: &createdAt, Critical: i, // repo-4 most severe
		}
		if err := store.UpsertScanRun(ctx, "tenant-a", run); err != nil {
			t.Fatalf("UpsertScanRun(%s) error = %v", id, err)
		}
	}

	summaries, err := store.ListLatestScanRunPerRepository(ctx, "tenant-a", 2)
	if err != nil {
		t.Fatalf("ListLatestScanRunPerRepository() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2 (limit honored across distinct repositories)", len(summaries))
	}
	if summaries[0].Run.Repository != "team/repo-4" || summaries[1].Run.Repository != "team/repo-3" {
		t.Fatalf("summaries = %#v, want the two most severe repositories first", summaries)
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

// TestStoreScanRunNotFoundErrorsIncludeTheLookupKey is the RED test for the
// scanRunRow/secretScanRunRow not-found message bug found while live-testing
// the stale-scan-run-dedup fix: both helpers always built
// domain.NewNotFoundError(kind, "") with a hardcoded empty second argument,
// so every not-found error read as `scan_run "" was not found` regardless of
// which run/digest was actually looked up. Each lookup below must surface
// the key it was actually searched by.
func TestStoreScanRunNotFoundErrorsIncludeTheLookupKey(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	const missingDigest = "sha256:0000000000000000000000000000000000000000000000000000000000aa"

	if _, err := store.GetScanRun(ctx, "tenant-a", "missing-run-id"); !strings.Contains(err.Error(), "missing-run-id") {
		t.Fatalf("GetScanRun(missing) error = %v, want it to mention the run ID", err)
	}
	if _, err := store.GetActiveScanRunByDigest(ctx, "tenant-a", "library/alpine", missingDigest); !strings.Contains(err.Error(), missingDigest) {
		t.Fatalf("GetActiveScanRunByDigest(missing) error = %v, want it to mention the digest", err)
	}
	if _, err := store.GetLatestScanRunByDigest(ctx, "tenant-a", "library/alpine", missingDigest); !strings.Contains(err.Error(), missingDigest) {
		t.Fatalf("GetLatestScanRunByDigest(missing) error = %v, want it to mention the digest", err)
	}
	if _, err := store.GetSecretScanRun(ctx, "tenant-a", "missing-secret-run-id"); !strings.Contains(err.Error(), "missing-secret-run-id") {
		t.Fatalf("GetSecretScanRun(missing) error = %v, want it to mention the run ID", err)
	}
	if _, err := store.GetActiveSecretScanRunByDigest(ctx, "tenant-a", "library/alpine", missingDigest); !strings.Contains(err.Error(), missingDigest) {
		t.Fatalf("GetActiveSecretScanRunByDigest(missing) error = %v, want it to mention the digest", err)
	}
}

// TestStoreListTagsWithCreatedAtReturnsEachTagsManifestCreatedAt is the RED
// test for the console-tags-table change: ListTagsWithCreatedAt joins tags
// to their manifest row and returns each tag's manifest created_at (the
// timestamp of when that manifest was pushed), not the tag row's own
// created_at -- retagging an existing digest under a second name must still
// report the manifest's original push time for both tags.
func TestStoreListTagsWithCreatedAtReturnsEachTagsManifestCreatedAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}

	before := time.Now().UTC()
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "latest", manifest, nil); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	// Retagging the same digest under a second tag must not mint a second
	// manifest row -- both tags should report the same original created_at.
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "stable", manifest, nil); err != nil {
		t.Fatalf("PublishManifest(second tag) error = %v", err)
	}
	after := time.Now().UTC()

	tags, err := store.ListTagsWithCreatedAt(context.Background(), "tenant-a", repo, 10, "")
	if err != nil {
		t.Fatalf("ListTagsWithCreatedAt() error = %v", err)
	}

	if len(tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2: %#v", len(tags), tags)
	}
	if tags[0].Name != "latest" || tags[1].Name != "stable" {
		t.Fatalf("tag names = [%s, %s], want [latest, stable] (ORDER BY name ASC)", tags[0].Name, tags[1].Name)
	}
	for _, tag := range tags {
		if tag.CreatedAt.Before(before) || tag.CreatedAt.After(after) {
			t.Fatalf("tag %q CreatedAt = %s, want between %s and %s", tag.Name, tag.CreatedAt, before, after)
		}
	}
	if !tags[0].CreatedAt.Equal(tags[1].CreatedAt) {
		t.Fatalf("tags[0].CreatedAt = %s, tags[1].CreatedAt = %s, want equal (both tags point at the same manifest)", tags[0].CreatedAt, tags[1].CreatedAt)
	}
}

// TestStoreListRepositoriesWithSummaryReturnsTagCountAndMostRecentPush is
// the RED test for the console-repositories-table change: a single
// aggregate query (COUNT(tags) / MAX(manifests.created_at) per repository)
// backs the Console TUI's top-level Repositories table's Tags and Last
// Pushed columns. Two tags in the same repository point at two DIFFERENT
// manifests published at different times, proving MAX picks the later push
// time rather than an arbitrary row, and TagCount reflects both tags.
func TestStoreListRepositoriesWithSummaryReturnsTagCountAndMostRecentPush(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")

	olderManifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2,"v":1}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "v1", olderManifest, nil); err != nil {
		t.Fatalf("PublishManifest(v1) error = %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	newerManifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2,"v":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	before := time.Now().UTC()
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "v2", newerManifest, nil); err != nil {
		t.Fatalf("PublishManifest(v2) error = %v", err)
	}
	after := time.Now().UTC()

	summaries, err := store.ListRepositoriesWithSummary(context.Background(), "tenant-a", 10, "")
	if err != nil {
		t.Fatalf("ListRepositoriesWithSummary() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1: %#v", len(summaries), summaries)
	}

	summary := summaries[0]
	if summary.Name != repo.String() {
		t.Fatalf("summary.Name = %q, want %q", summary.Name, repo.String())
	}
	if summary.TagCount != 2 {
		t.Fatalf("summary.TagCount = %d, want 2", summary.TagCount)
	}
	if summary.LastPushed.Before(before) || summary.LastPushed.After(after) {
		t.Fatalf("summary.LastPushed = %s, want between %s and %s (the LATER tag's push time, not the first/arbitrary one)", summary.LastPushed, before, after)
	}
}

// TestStoreListRepositoriesWithSummaryIncludesZeroTagRepositories proves a
// repository row with no tags at all (a manifest published with an empty
// tag, e.g. digest-only push) still comes back with TagCount=0 and a zero
// LastPushed rather than being silently dropped by the aggregate join.
func TestStoreListRepositoriesWithSummaryIncludesZeroTagRepositories(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/untagged")
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "", manifest, nil); err != nil {
		t.Fatalf("PublishManifest(no tag) error = %v", err)
	}

	summaries, err := store.ListRepositoriesWithSummary(context.Background(), "tenant-a", 10, "")
	if err != nil {
		t.Fatalf("ListRepositoriesWithSummary() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1: %#v", len(summaries), summaries)
	}
	if summaries[0].TagCount != 0 {
		t.Fatalf("summaries[0].TagCount = %d, want 0", summaries[0].TagCount)
	}
	if !summaries[0].LastPushed.IsZero() {
		t.Fatalf("summaries[0].LastPushed = %s, want zero (no tags to aggregate over)", summaries[0].LastPushed)
	}
}

// TestStoreListRepositoriesWithSummaryOrdersByNameAscAndRespectsLimitAfter
// mirrors Catalog's own ordering/pagination contract (ORDER BY name ASC,
// after cursor, limit) -- the aggregate query must not silently drop that
// contract while adding TagCount/LastPushed.
func TestStoreListRepositoriesWithSummaryOrdersByNameAscAndRespectsLimitAfter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	for _, name := range []string{"team/charlie", "team/alpha", "team/bravo"} {
		repo := domain.MustParseRepositoryRef(name)
		if err := store.PublishManifest(context.Background(), "tenant-a", repo, "", manifest, nil); err != nil {
			t.Fatalf("PublishManifest(%q) error = %v", name, err)
		}
	}

	summaries, err := store.ListRepositoriesWithSummary(context.Background(), "tenant-a", 10, "")
	if err != nil {
		t.Fatalf("ListRepositoriesWithSummary() error = %v", err)
	}
	if len(summaries) != 3 {
		t.Fatalf("len(summaries) = %d, want 3: %#v", len(summaries), summaries)
	}
	names := []string{summaries[0].Name, summaries[1].Name, summaries[2].Name}
	if names[0] != "team/alpha" || names[1] != "team/bravo" || names[2] != "team/charlie" {
		t.Fatalf("names = %#v, want alphabetical order", names)
	}

	after, err := store.ListRepositoriesWithSummary(context.Background(), "tenant-a", 10, "team/alpha")
	if err != nil {
		t.Fatalf("ListRepositoriesWithSummary(after=team/alpha) error = %v", err)
	}
	if len(after) != 2 || after[0].Name != "team/bravo" {
		t.Fatalf("after = %#v, want [team/bravo, team/charlie]", after)
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

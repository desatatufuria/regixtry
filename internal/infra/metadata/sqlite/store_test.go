package sqlite

import (
	"context"
	"database/sql"
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

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
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
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
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
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, blobs, nil, nil)
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

// TestStoreGetUpdateChannelReturnsNotFoundWithNoRow mirrors
// TestStoreGetScanPolicySettingsReturnsNotFoundWithNoRow's own row-absence
// behavior: the code-level default ("stable") is applied one layer up, at
// the service, never at the store.
func TestStoreGetUpdateChannelReturnsNotFoundWithNoRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetUpdateChannel(context.Background(), "tenant-a")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetUpdateChannel() error = %v, want ErrorCodeNotFound", err)
	}
}

// TestStoreUpsertUpdateChannelRoundTripsChannelAndUpdatedAt mirrors
// TestStoreUpsertScanPolicySettingsRoundTripsEnabledAndThreshold's
// round-trip-then-flip shape.
func TestStoreUpsertUpdateChannelRoundTripsChannelAndUpdatedAt(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	updatedAt := time.Now().UTC()
	settings := ports.UpdateChannelSettings{Channel: ports.UpdateChannelStable, UpdatedAt: updatedAt}
	if err := store.UpsertUpdateChannel(context.Background(), "tenant-a", settings); err != nil {
		t.Fatalf("UpsertUpdateChannel() error = %v", err)
	}

	stored, err := store.GetUpdateChannel(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetUpdateChannel() error = %v", err)
	}
	if stored.Channel != ports.UpdateChannelStable {
		t.Fatalf("stored.Channel = %q, want %q", stored.Channel, ports.UpdateChannelStable)
	}
	if !stored.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("stored.UpdatedAt = %v, want %v", stored.UpdatedAt, updatedAt)
	}

	settings.Channel = ports.UpdateChannelInsider
	settings.UpdatedAt = time.Now().UTC()
	if err := store.UpsertUpdateChannel(context.Background(), "tenant-a", settings); err != nil {
		t.Fatalf("UpsertUpdateChannel(update) error = %v", err)
	}
	updated, err := store.GetUpdateChannel(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("GetUpdateChannel(update) error = %v", err)
	}
	if updated.Channel != ports.UpdateChannelInsider {
		t.Fatalf("updated.Channel = %q, want %q", updated.Channel, ports.UpdateChannelInsider)
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
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
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

// TestStoreListTagsWithCreatedAtReturnsPushedBy is the RED test for the
// console-tags-pushed-by change: ListTagsWithCreatedAt carries each tag's
// manifest.pushed_by column through to ports.TagSummary.PushedBy, and a
// legacy/unknown manifest (empty pushed_by, the column's own default)
// reports "" rather than erroring.
func TestStoreListTagsWithCreatedAtReturnsPushedBy(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	repo := domain.MustParseRepositoryRef("library/alpine")

	pushed, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	pushed.PushedBy = "user-abc-123"
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "known-pusher", pushed, nil); err != nil {
		t.Fatalf("PublishManifest(known-pusher) error = %v", err)
	}

	legacy, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"annotations":{"legacy":"true"}}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "unknown-pusher", legacy, nil); err != nil {
		t.Fatalf("PublishManifest(unknown-pusher) error = %v", err)
	}

	tags, err := store.ListTagsWithCreatedAt(context.Background(), "tenant-a", repo, 10, "")
	if err != nil {
		t.Fatalf("ListTagsWithCreatedAt() error = %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2: %#v", len(tags), tags)
	}

	byName := make(map[string]ports.TagSummary, len(tags))
	for _, tag := range tags {
		byName[tag.Name] = tag
	}
	if got, want := byName["known-pusher"].PushedBy, "user-abc-123"; got != want {
		t.Fatalf("known-pusher.PushedBy = %q, want %q", got, want)
	}
	if got, want := byName["unknown-pusher"].PushedBy, ""; got != want {
		t.Fatalf("unknown-pusher.PushedBy = %q, want %q (legacy/unknown default)", got, want)
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

	olderManifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"v":1}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-a", repo, "v1", olderManifest, nil); err != nil {
		t.Fatalf("PublishManifest(v1) error = %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	newerManifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"v":2}`), nil, nil, nil, nil)
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
	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
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

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
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

// TestListReferencedBlobDigestsIsGlobalAcrossTenants is T7 (design.md
// Testing Strategy): the mark query has no tenant argument at all (a
// compile-time proof by itself) and two tenants sharing the same digest
// must collapse to exactly one row -- SELECT DISTINCT digest FROM
// manifest_blobs, Decision C's deliberate asymmetry on MetadataStore.
func TestListReferencedBlobDigestsIsGlobalAcrossTenants(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	shared := domain.DigestFromBytes([]byte("shared-layer"))
	blobs := []domain.Descriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: shared, Size: int64(len("shared-layer"))}}

	manifestA, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"who":"a"}`), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-a", domain.MustParseRepositoryRef("library/alpine"), "latest", manifestA, blobs); err != nil {
		t.Fatalf("PublishManifest(tenant-a) error = %v", err)
	}

	manifestB, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"who":"b"}`), nil, blobs, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(context.Background(), "tenant-b", domain.MustParseRepositoryRef("library/alpine"), "latest", manifestB, blobs); err != nil {
		t.Fatalf("PublishManifest(tenant-b) error = %v", err)
	}

	digests, err := store.ListReferencedBlobDigests(context.Background())
	if err != nil {
		t.Fatalf("ListReferencedBlobDigests() error = %v", err)
	}

	if len(digests) != 1 {
		t.Fatalf("ListReferencedBlobDigests() = %#v, want exactly one row for a digest shared by two tenants", digests)
	}
	if digests[0] != shared.String() {
		t.Fatalf("digests[0] = %s, want %s", digests[0], shared)
	}
}

// TestCreateGCReportPersistsReportAndCandidatesInOneTransaction pins
// tasks.md 3.3: a report plus its candidates round-trip through
// CreateGCReport/GetGCReport with stored position order preserved.
func TestCreateGCReportPersistsReportAndCandidatesInOneTransaction(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	now := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	report := ports.GCReport{
		ID:                "report-1",
		Status:            ports.GCReportStatusReported,
		ComputedAt:        now,
		ExpiresAt:         now.Add(24 * time.Hour),
		GraceCutoff:       now.Add(-24 * time.Hour),
		CandidateCount:    2,
		CandidateBytes:    300,
		DurationMillis:    42,
		RequestedBy:       "usr_01",
		TriggeredInTenant: "tenant-a",
	}
	candidates := []ports.GCReportCandidate{
		{Digest: "sha256:bb00000000000000000000000000000000000000000000000000000000000000", Size: 200, ModTime: now.Add(-48 * time.Hour)},
		{Digest: "sha256:aa00000000000000000000000000000000000000000000000000000000000000", Size: 100, ModTime: now.Add(-72 * time.Hour)},
	}

	if err := store.CreateGCReport(context.Background(), report, candidates); err != nil {
		t.Fatalf("CreateGCReport() error = %v", err)
	}

	detail, err := store.GetGCReport(context.Background(), "report-1")
	if err != nil {
		t.Fatalf("GetGCReport() error = %v", err)
	}

	if detail.Report.ID != "report-1" || detail.Report.CandidateCount != 2 || detail.Report.CandidateBytes != 300 {
		t.Fatalf("detail.Report = %#v, want ID=report-1 CandidateCount=2 CandidateBytes=300", detail.Report)
	}
	if detail.Report.RequestedBy != "usr_01" || detail.Report.TriggeredInTenant != "tenant-a" {
		t.Fatalf("detail.Report = %#v, want RequestedBy=usr_01 TriggeredInTenant=tenant-a", detail.Report)
	}
	if len(detail.Candidates) != 2 {
		t.Fatalf("len(detail.Candidates) = %d, want 2", len(detail.Candidates))
	}
	if detail.Candidates[0].Digest != candidates[0].Digest || detail.Candidates[1].Digest != candidates[1].Digest {
		t.Fatalf("detail.Candidates = %#v, want stored position order [%s, %s]", detail.Candidates, candidates[0].Digest, candidates[1].Digest)
	}
}

// TestGetGCReportReturnsNotFoundForUnknownID pins tasks.md 3.4.
func TestGetGCReportReturnsNotFoundForUnknownID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetGCReport(context.Background(), "does-not-exist")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetGCReport(unknown) error = %v, want ErrorCodeNotFound", err)
	}
}

// TestPruneExpiredGCReportsRemovesExpiredReportedButKeepsDeleted is T9's
// prune half: an expired "reported" row is pruned on the next report, while
// a "deleted"-state row is retained regardless of age (design.md D8 -- this
// is hygiene, not the delete-safety mechanism).
func TestPruneExpiredGCReportsRemovesExpiredReportedButKeepsDeleted(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	now := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)

	expiredReported := ports.GCReport{
		ID:          "expired-reported",
		Status:      ports.GCReportStatusReported,
		ComputedAt:  now.Add(-48 * time.Hour),
		ExpiresAt:   now.Add(-24 * time.Hour),
		GraceCutoff: now.Add(-72 * time.Hour),
	}
	if err := store.CreateGCReport(context.Background(), expiredReported, nil); err != nil {
		t.Fatalf("CreateGCReport(expiredReported) error = %v", err)
	}

	stillLive := ports.GCReport{
		ID:          "still-live",
		Status:      ports.GCReportStatusReported,
		ComputedAt:  now,
		ExpiresAt:   now.Add(24 * time.Hour),
		GraceCutoff: now.Add(-24 * time.Hour),
	}
	if err := store.CreateGCReport(context.Background(), stillLive, nil); err != nil {
		t.Fatalf("CreateGCReport(stillLive) error = %v", err)
	}

	expiredDeleted := ports.GCReport{
		ID:          "expired-deleted",
		Status:      ports.GCReportStatusDeleted,
		ComputedAt:  now.Add(-48 * time.Hour),
		ExpiresAt:   now.Add(-24 * time.Hour),
		GraceCutoff: now.Add(-72 * time.Hour),
	}
	if err := store.CreateGCReport(context.Background(), expiredDeleted, nil); err != nil {
		t.Fatalf("CreateGCReport(expiredDeleted) error = %v", err)
	}

	pruned, err := store.PruneExpiredGCReports(context.Background(), now)
	if err != nil {
		t.Fatalf("PruneExpiredGCReports() error = %v", err)
	}
	if pruned != 1 {
		t.Fatalf("PruneExpiredGCReports() = %d, want 1", pruned)
	}

	if _, err := store.GetGCReport(context.Background(), "expired-reported"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetGCReport(expired-reported) after prune error = %v, want ErrorCodeNotFound", err)
	}
	if _, err := store.GetGCReport(context.Background(), "still-live"); err != nil {
		t.Fatalf("GetGCReport(still-live) after prune error = %v, want it to survive", err)
	}
	if _, err := store.GetGCReport(context.Background(), "expired-deleted"); err != nil {
		t.Fatalf("GetGCReport(expired-deleted) after prune error = %v, want deleted-state rows to be retained indefinitely", err)
	}
}

// TestMarkGCReportDeletedRejectsNonReportedRow is T5's store half (design.md
// Decision E): the SQL WHERE id = ? AND status = 'reported' clause is the
// actual single-use guard, not just a Go-side status check made in advance.
// A second UPDATE against an already-"deleted" row affects zero rows and
// must surface a typed domain.ErrorCodeConflict.
func TestMarkGCReportDeletedRejectsNonReportedRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	now := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	report := ports.GCReport{
		ID:          "report-single-use",
		Status:      ports.GCReportStatusReported,
		ComputedAt:  now,
		ExpiresAt:   now.Add(24 * time.Hour),
		GraceCutoff: now.Add(-24 * time.Hour),
	}
	candidates := []ports.GCReportCandidate{
		{Digest: "sha256:cc00000000000000000000000000000000000000000000000000000000000000", Size: 10, ModTime: now.Add(-48 * time.Hour)},
	}
	if err := store.CreateGCReport(context.Background(), report, candidates); err != nil {
		t.Fatalf("CreateGCReport() error = %v", err)
	}

	firstOutcome := ports.GCDeleteOutcome{
		DeletedAt:      now,
		DeletedBy:      "usr_01",
		DeletedCount:   1,
		BytesReclaimed: 10,
		Candidates: []ports.GCCandidateOutcome{
			{Digest: candidates[0].Digest, Outcome: ports.GCCandidateOutcomeDeleted},
		},
	}
	if err := store.MarkGCReportDeleted(context.Background(), report.ID, firstOutcome); err != nil {
		t.Fatalf("MarkGCReportDeleted() (first) error = %v", err)
	}

	secondOutcome := firstOutcome
	err := store.MarkGCReportDeleted(context.Background(), report.ID, secondOutcome)
	if !domain.IsCode(err, domain.ErrorCodeConflict) {
		t.Fatalf("MarkGCReportDeleted() (second) error = %v, want ErrorCodeConflict", err)
	}

	detail, err := store.GetGCReport(context.Background(), report.ID)
	if err != nil {
		t.Fatalf("GetGCReport() error = %v", err)
	}
	if detail.Report.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1 (second call must not double-count)", detail.Report.DeletedCount)
	}
}

// TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex confirms
// tasks.md 10.6's chosen index decision (a): the per-candidate outcome
// UPDATE inside MarkGCReportDeleted (WHERE report_id = ? AND digest = ?)
// uses idx_gc_report_candidates_report_digest via EXPLAIN QUERY PLAN,
// rather than a linear scan of every candidate row for the report.
func TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	rows, err := store.db.Query(`EXPLAIN QUERY PLAN UPDATE gc_report_candidates SET outcome = ?, error = ? WHERE report_id = ? AND digest = ?`, "deleted", "", "some-report-id", "sha256:aaaa")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("; ")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}

	if !strings.Contains(plan.String(), "idx_gc_report_candidates_report_digest") {
		t.Fatalf("query plan = %q, want it to use idx_gc_report_candidates_report_digest", plan.String())
	}
}

// TestStorePublishManifestWritesSubjectDigest covers oci-referrers-api
// design.md Decision 2/3, Phase 3: PublishManifest persists
// manifests.subject_digest from manifest.Subject, so ListReferrers (Phase 5,
// out of scope here) can query it directly without re-parsing payload JSON
// on every read.
func TestStorePublishManifestWritesSubjectDigest(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")

	target, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"target":true}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest(target) error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "target", target, nil); err != nil {
		t.Fatalf("PublishManifest(target) error = %v", err)
	}

	subject := &domain.Descriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    target.Digest,
		Size:      target.Size,
	}
	referrer, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.sbom.v1+json", []byte(`{"schemaVersion":2,"referrer":true}`), nil, nil, subject, nil)
	if err != nil {
		t.Fatalf("NewManifest(referrer) error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "", referrer, nil); err != nil {
		t.Fatalf("PublishManifest(referrer) error = %v", err)
	}

	got := querySubjectDigest(t, store, "tenant-a", "library/alpine", referrer.Digest.String())
	if got != target.Digest.String() {
		t.Fatalf("subject_digest = %q, want %q", got, target.Digest.String())
	}
}

// TestStorePublishManifestNoSubjectStoresEmptyString triangulates the above:
// an ordinary manifest with no Subject (the overwhelming majority of pushes)
// must store an empty string -- the value idx_manifests_subject's partial predicate
// excludes.
func TestStorePublishManifestNoSubjectStoresEmptyString(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")

	manifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "latest", manifest, nil); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	got := querySubjectDigest(t, store, "tenant-a", "library/alpine", manifest.Digest.String())
	if got != "" {
		t.Fatalf("subject_digest = %q, want empty string", got)
	}
}

// TestStorePublishManifestRepushWithDifferentSubjectUpdatesStoredValue
// covers design.md's risk note: unlike pushed_by (deliberately insert-only),
// subject_digest IS in DO UPDATE SET. Two domain.Manifest values sharing one
// byte-identical payload (hence one digest, since Subject/ArtifactType/
// PushedBy do not participate in digest computation -- manifest.go) but
// different Subject descriptors model a repush at the store's own API
// boundary; the second PublishManifest call must overwrite, not preserve,
// the first subject_digest.
func TestStorePublishManifestRepushWithDifferentSubjectUpdatesStoredValue(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")
	payload := []byte(`{"schemaVersion":2,"repush":true}`)

	firstSubject := &domain.Descriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    domain.DigestFromBytes([]byte("subject-one")),
		Size:      int64(len("subject-one")),
	}
	firstManifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", payload, nil, nil, firstSubject, nil)
	if err != nil {
		t.Fatalf("NewManifest(first) error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "", firstManifest, nil); err != nil {
		t.Fatalf("PublishManifest(first) error = %v", err)
	}

	if got := querySubjectDigest(t, store, "tenant-a", "library/alpine", firstManifest.Digest.String()); got != firstSubject.Digest.String() {
		t.Fatalf("subject_digest (first) = %q, want %q", got, firstSubject.Digest.String())
	}

	secondSubject := &domain.Descriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    domain.DigestFromBytes([]byte("subject-two")),
		Size:      int64(len("subject-two")),
	}
	secondManifest, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", payload, nil, nil, secondSubject, nil)
	if err != nil {
		t.Fatalf("NewManifest(second) error = %v", err)
	}
	if secondManifest.Digest != firstManifest.Digest {
		t.Fatalf("secondManifest.Digest = %s, want equal to firstManifest.Digest %s (repush requires byte-identical payload)", secondManifest.Digest, firstManifest.Digest)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "", secondManifest, nil); err != nil {
		t.Fatalf("PublishManifest(second/repush) error = %v", err)
	}

	if got := querySubjectDigest(t, store, "tenant-a", "library/alpine", firstManifest.Digest.String()); got != secondSubject.Digest.String() {
		t.Fatalf("subject_digest (after repush) = %q, want %q (must update, not stay stale)", got, secondSubject.Digest.String())
	}
}

// TestStoreCreatesPartialIndexOnSubjectDigestUsableByLiteralPredicate covers
// design.md Decision 2's load-bearing detail: SQLite only uses a partial
// index when the query's WHERE clause provably implies the index's own
// predicate. ListReferrers itself is Phase 5 (out of scope for this PR), so
// this issues the equivalent SELECT directly, following
// TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex's EXPLAIN
// QUERY PLAN precedent above.
func TestStoreCreatesPartialIndexOnSubjectDigestUsableByLiteralPredicate(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	plan := explainQueryPlan(t, store, `
		SELECT m.digest, m.media_type, m.size, m.payload
		FROM manifests m
		JOIN repositories r ON r.id = m.repository_id
		WHERE m.tenant = ? AND r.tenant = ? AND r.name = ?
		  AND m.subject_digest = ?
		  AND m.subject_digest != ''
		ORDER BY m.digest ASC
	`, "tenant-a", "tenant-a", "library/alpine", "sha256:aaaa")

	if !strings.Contains(plan, "idx_manifests_subject") {
		t.Fatalf("query plan = %q, want it to use idx_manifests_subject (literal subject_digest != '' predicate required for SQLite to prove partial-index applicability)", plan)
	}
}

// TestStorePartialIndexNotUsedWithoutTheLiteralPredicate triangulates the
// above: a query with only "subject_digest = ?" (a bound parameter, no
// literal empty-string comparison) does not provably imply the partial index's own
// predicate, so SQLite must NOT resolve it to idx_manifests_subject -- this
// proves Decision 2's literal-predicate requirement is load-bearing, not
// decorative.
func TestStorePartialIndexNotUsedWithoutTheLiteralPredicate(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	plan := explainQueryPlan(t, store, `
		SELECT m.digest, m.media_type, m.size, m.payload
		FROM manifests m
		JOIN repositories r ON r.id = m.repository_id
		WHERE m.tenant = ? AND r.tenant = ? AND r.name = ?
		  AND m.subject_digest = ?
		ORDER BY m.digest ASC
	`, "tenant-a", "tenant-a", "library/alpine", "sha256:aaaa")

	if strings.Contains(plan, "idx_manifests_subject") {
		t.Fatalf("query plan = %q, must NOT use idx_manifests_subject without the literal subject_digest != '' predicate", plan)
	}
}

// TestStoreBackfillsPreExistingRowsSubjectDigestOnNewIdempotently covers
// oci-referrers-api design.md Decision 1, Phase 4: a row written before this
// column existed (subject_digest equal to an empty string, simulated here via
// insertRawManifestRow against a schema-only store, bypassing both
// PublishManifest and the very first backfill pass) is backfilled the
// moment New() is called against its database file, and a second New()
// changes no row and writes no second schema_backfills marker.
func TestStoreBackfillsPreExistingRowsSubjectDigestOnNewIdempotently(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "registry.db")

	store := newSchemaOnlyStore(t, path)

	subjectDigest := domain.DigestFromBytes([]byte("pre-existing-subject")).String()
	payload := []byte(fmt.Sprintf(`{"schemaVersion":2,"subject":{"digest":%q}}`, subjectDigest))
	referrerDigest := domain.DigestFromBytes(payload).String()

	insertRawManifestRow(t, store, "tenant-a", "library/alpine", referrerDigest, payload, "")

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(path)
	if err != nil {
		t.Fatalf("New() (reopen, triggers backfill) error = %v", err)
	}
	defer reopened.Close()

	if got := querySubjectDigest(t, reopened, "tenant-a", "library/alpine", referrerDigest); got != subjectDigest {
		t.Fatalf("subject_digest after backfill = %q, want %q", got, subjectDigest)
	}

	completedAtFirst := queryBackfillMarker(t, reopened)
	if completedAtFirst == "" {
		t.Fatalf("schema_backfills marker missing after backfill")
	}

	if err := reopened.Close(); err != nil {
		t.Fatalf("Close() (before idempotency reopen) error = %v", err)
	}

	secondReopen, err := New(path)
	if err != nil {
		t.Fatalf("New() (second reopen, idempotency) error = %v", err)
	}
	defer secondReopen.Close()

	if got := querySubjectDigest(t, secondReopen, "tenant-a", "library/alpine", referrerDigest); got != subjectDigest {
		t.Fatalf("subject_digest after second New() = %q, want unchanged %q", got, subjectDigest)
	}

	if completedAtSecond := queryBackfillMarker(t, secondReopen); completedAtSecond != completedAtFirst {
		t.Fatalf("schema_backfills.completed_at changed across idempotent reopen: first=%q second=%q, want unchanged", completedAtFirst, completedAtSecond)
	}

	if count := queryBackfillMarkerCount(t, secondReopen); count != 1 {
		t.Fatalf("schema_backfills row count = %d, want exactly 1 (INSERT OR IGNORE must not create a second marker)", count)
	}
}

// TestStoreBackfillLeavesUnparseablePayloadEmptyAndNewStillSucceeds covers
// the threat matrix's "boot-time migration availability" row: a row whose
// payload is not JSON, and a row whose subject.digest fails
// domain.ParseDigest, must both be left at subject_digest equal to an empty string -- exactly
// the pre-change value -- and New() must still succeed rather than fail
// boot. A third, valid control row is seeded alongside the two corrupt ones
// and asserted non-empty, proving the backfill actually ran across all three
// rows rather than this test accidentally passing because no backfill ran
// at all.
func TestStoreBackfillLeavesUnparseablePayloadEmptyAndNewStillSucceeds(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "registry.db")

	store := newSchemaOnlyStore(t, path)

	notJSONPayload := []byte("not valid json at all")
	notJSONDigest := domain.DigestFromBytes(notJSONPayload).String()
	insertRawManifestRow(t, store, "tenant-a", "library/alpine", notJSONDigest, notJSONPayload, "")

	malformedSubjectPayload := []byte(`{"schemaVersion":2,"subject":{"digest":"not-a-valid-digest"}}`)
	malformedDigest := domain.DigestFromBytes(malformedSubjectPayload).String()
	insertRawManifestRow(t, store, "tenant-a", "library/alpine", malformedDigest, malformedSubjectPayload, "")

	validSubjectDigest := domain.DigestFromBytes([]byte("control-row-subject")).String()
	validPayload := []byte(fmt.Sprintf(`{"schemaVersion":2,"subject":{"digest":%q}}`, validSubjectDigest))
	validDigest := domain.DigestFromBytes(validPayload).String()
	insertRawManifestRow(t, store, "tenant-a", "library/alpine", validDigest, validPayload, "")

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(path)
	if err != nil {
		t.Fatalf("New() (reopen, backfill over corrupt rows) error = %v, want success -- a corrupt row must not block boot", err)
	}
	defer reopened.Close()

	if got := querySubjectDigest(t, reopened, "tenant-a", "library/alpine", notJSONDigest); got != "" {
		t.Fatalf("subject_digest (unparseable JSON payload) = %q, want empty string", got)
	}
	if got := querySubjectDigest(t, reopened, "tenant-a", "library/alpine", malformedDigest); got != "" {
		t.Fatalf("subject_digest (malformed subject.digest) = %q, want empty string", got)
	}
	if got := querySubjectDigest(t, reopened, "tenant-a", "library/alpine", validDigest); got != validSubjectDigest {
		t.Fatalf("subject_digest (control row, valid subject) = %q, want %q -- proves the backfill actually ran, not merely that corrupt rows are untouched", got, validSubjectDigest)
	}
}

// TestStoreBackfillRowUpdateAndMarkerCommitTogether is the single-transaction
// probe for design.md Decision 1: the row UPDATE(s) and the schema_backfills
// INSERT OR IGNORE land in the SAME transaction, so one read joining both
// tables must see them landed together, never one without the other.
// Fault-injected mid-transaction atomicity is out of reach for a black-box
// database/sql test; this proves the observable invariant the atomicity
// exists to guarantee.
func TestStoreBackfillRowUpdateAndMarkerCommitTogether(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "registry.db")

	store := newSchemaOnlyStore(t, path)

	subjectDigest := domain.DigestFromBytes([]byte("atomic-probe-subject")).String()
	payload := []byte(fmt.Sprintf(`{"schemaVersion":2,"subject":{"digest":%q}}`, subjectDigest))
	referrerDigest := domain.DigestFromBytes(payload).String()
	insertRawManifestRow(t, store, "tenant-a", "library/alpine", referrerDigest, payload, "")

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := New(path)
	if err != nil {
		t.Fatalf("New() (reopen, backfill) error = %v", err)
	}
	defer reopened.Close()

	var (
		gotSubjectDigest string
		markerCount      int
	)
	err = reopened.db.QueryRow(`
		SELECT
			(SELECT m.subject_digest FROM manifests m
			 JOIN repositories r ON r.id = m.repository_id
			 WHERE r.tenant = ? AND r.name = ? AND m.digest = ?),
			(SELECT COUNT(*) FROM schema_backfills WHERE name = ?)
	`, "tenant-a", "library/alpine", referrerDigest, "manifests.subject_digest.v1").Scan(&gotSubjectDigest, &markerCount)
	if err != nil {
		t.Fatalf("single-transaction probe query error = %v", err)
	}

	if gotSubjectDigest != subjectDigest || markerCount != 1 {
		t.Fatalf("probe = (subject_digest=%q, markerCount=%d), want (%q, 1) -- row update and marker must land together", gotSubjectDigest, markerCount, subjectDigest)
	}
}

// TestStoreListReferrersCrossTenantReturnsEmpty covers the threat matrix's
// highest-severity row (design.md Decision 3 / Threat Matrix
// "Cross-tenant / cross-repository leakage"): a referrer seeded under
// tenant-a must never be visible to tenant-b's identically-named repository,
// asserted directly against seeded rows -- never inferred from the HTTP
// layer, which does not exist yet (Phase 7).
func TestStoreListReferrersCrossTenantReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")
	subject := &domain.Descriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    domain.DigestFromBytes([]byte("cross-tenant-subject")),
		Size:      int64(len("cross-tenant-subject")),
	}
	referrer, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.sbom.v1+json", []byte(`{"schemaVersion":2,"crossTenant":true}`), nil, nil, subject, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "", referrer, nil); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	rows, err := store.ListReferrers(ctx, "tenant-b", repo, subject.Digest)
	if err != nil {
		t.Fatalf("ListReferrers() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListReferrers() rows = %d, want 0 -- tenant-b must never see tenant-a's referrer, even under the identically-named repository %q", len(rows), repo.String())
	}
}

// TestStoreListReferrersCrossRepositoryReturnsEmpty triangulates the above:
// same tenant, but a different repository name -- ListReferrers is scoped
// exactly like ResolveManifest/ListTags (tenant AND repository), never like
// ListReferencedBlobDigests' deliberate global scoping (design.md Decision
// 3's own doc comment names that method as the anti-pattern).
func TestStoreListReferrersCrossRepositoryReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")
	otherRepo := domain.MustParseRepositoryRef("library/other")
	subject := &domain.Descriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    domain.DigestFromBytes([]byte("cross-repository-subject")),
		Size:      int64(len("cross-repository-subject")),
	}
	referrer, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "application/vnd.example.sbom.v1+json", []byte(`{"schemaVersion":2,"crossRepository":true}`), nil, nil, subject, nil)
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "", referrer, nil); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	rows, err := store.ListReferrers(ctx, "tenant-a", otherRepo, subject.Digest)
	if err != nil {
		t.Fatalf("ListReferrers() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListReferrers() rows = %d, want 0 -- library/other must never see library/alpine's referrer under the same tenant", len(rows))
	}
}

// TestStoreListReferrersMatchesExactSubjectAndOrdersByDigestAscRegardlessOfInsertionOrder
// covers design.md Decision 4: three referrers of one subject, pushed in an
// order that is NOT digest-ascending, must come back ORDER BY digest ASC.
// An ordinary manifest (no Subject, so subject_digest stays an empty
// string) is seeded alongside them and asserted absent from the matched
// set -- the "empty string never matches" property from tasks.md 5.2.
func TestStoreListReferrersMatchesExactSubjectAndOrdersByDigestAscRegardlessOfInsertionOrder(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")
	subject := &domain.Descriptor{
		MediaType: "application/vnd.oci.image.manifest.v1+json",
		Digest:    domain.DigestFromBytes([]byte("ordering-subject")),
		Size:      int64(len("ordering-subject")),
	}

	ordinary, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"ordinary":true}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest(ordinary) error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "latest", ordinary, nil); err != nil {
		t.Fatalf("PublishManifest(ordinary) error = %v", err)
	}

	payloads := [][]byte{
		[]byte(`{"schemaVersion":2,"referrer":"first-pushed"}`),
		[]byte(`{"schemaVersion":2,"referrer":"second-pushed"}`),
		[]byte(`{"schemaVersion":2,"referrer":"third-pushed"}`),
	}
	var referrerDigests []string
	for _, payload := range payloads {
		referrer, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", payload, nil, nil, subject, nil)
		if err != nil {
			t.Fatalf("NewManifest(referrer) error = %v", err)
		}
		if err := store.PublishManifest(ctx, "tenant-a", repo, "", referrer, nil); err != nil {
			t.Fatalf("PublishManifest(referrer) error = %v", err)
		}
		referrerDigests = append(referrerDigests, referrer.Digest.String())
	}

	rows, err := store.ListReferrers(ctx, "tenant-a", repo, subject.Digest)
	if err != nil {
		t.Fatalf("ListReferrers() error = %v", err)
	}
	if len(rows) != len(referrerDigests) {
		t.Fatalf("ListReferrers() rows = %d, want %d -- must exclude the ordinary manifest, whose subject_digest is ''", len(rows), len(referrerDigests))
	}

	want := append([]string(nil), referrerDigests...)
	sort.Strings(want)
	for i, row := range rows {
		if row.Digest.String() != want[i] {
			t.Fatalf("rows[%d].Digest = %q, want %q -- must be ORDER BY digest ASC regardless of insertion order", i, row.Digest.String(), want[i])
		}
	}
}

// TestStoreListReferrersUnknownDigestReturnsEmptySliceNotError covers
// tasks.md 5.2's third clause: a subject digest that was never pushed as
// anyone's subject returns an empty slice and a nil error, never a
// domain.ErrorCodeNotFound -- ListReferrers is a list query, not a
// single-row lookup like ResolveManifest.
func TestStoreListReferrersUnknownDigestReturnsEmptySliceNotError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	repo := domain.MustParseRepositoryRef("library/alpine")

	ordinary, err := domain.NewManifest("application/vnd.oci.image.manifest.v1+json", "", []byte(`{"schemaVersion":2,"ordinary":true}`), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewManifest(ordinary) error = %v", err)
	}
	if err := store.PublishManifest(ctx, "tenant-a", repo, "latest", ordinary, nil); err != nil {
		t.Fatalf("PublishManifest(ordinary) error = %v", err)
	}

	neverPushed := domain.DigestFromBytes([]byte("never-pushed-subject"))
	rows, err := store.ListReferrers(ctx, "tenant-a", repo, neverPushed)
	if err != nil {
		t.Fatalf("ListReferrers() error = %v, want nil -- an absent subject digest is an empty slice, never a not-found error (design.md Decision 3)", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListReferrers() rows = %d, want 0", len(rows))
	}
}

// TestStoreListReferrersUsesPartialIndexGivenLiteralPredicate re-confirms,
// against ListReferrers' own shipped query text, what
// TestStoreCreatesPartialIndexOnSubjectDigestUsableByLiteralPredicate (PR 2,
// above) already proved against an identical literal query before
// ListReferrers existed: SQLite only resolves idx_manifests_subject when the
// WHERE clause literally repeats the "not equal to empty string"
// subject_digest predicate (design.md Decision 2's load-bearing detail).
// This literal must be kept in lockstep with
// ListReferrers' own SQL by hand -- EXPLAIN QUERY PLAN cannot introspect an
// arbitrary Go method -- following
// TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex's
// precedent.
func TestStoreListReferrersUsesPartialIndexGivenLiteralPredicate(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	plan := explainQueryPlan(t, store, `
		SELECT m.digest, m.media_type, m.size, m.payload
		FROM manifests m
		JOIN repositories r ON r.id = m.repository_id
		WHERE m.tenant = ? AND r.tenant = ? AND r.name = ?
		  AND m.subject_digest = ?
		  AND m.subject_digest != ''
		ORDER BY m.digest ASC
	`, "tenant-a", "tenant-a", "library/alpine", "sha256:aaaa")

	if !strings.Contains(plan, "idx_manifests_subject") {
		t.Fatalf("query plan = %q, want it to use idx_manifests_subject (ListReferrers' own query must repeat the literal subject_digest != '' predicate)", plan)
	}
}

// newSchemaOnlyStore opens a store at path with init() run (so the
// manifests/schema_backfills tables and subject_digest column all exist)
// but WITHOUT running backfillSubjectDigests -- letting a test seed
// "pre-existing" rows before the very first backfill pass ever executes,
// matching how a real upgrade encounters rows written before this
// migration shipped. A plain New(path) call always runs both init() and
// the backfill together, so it cannot be used to seed pre-backfill state.
func newSchemaOnlyStore(t *testing.T, path string) *Store {
	t.Helper()

	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		t.Fatalf("init() error = %v", err)
	}

	return store
}

// querySubjectDigest reads manifests.subject_digest directly -- no port
// exposes it yet (ListReferrers is Phase 5, out of scope for this PR) --
// using the same tenant/repository join predicates ResolveManifest/ListTags
// use.
func querySubjectDigest(t *testing.T, store *Store, tenant string, repository string, digest string) string {
	t.Helper()

	var subjectDigest string
	err := store.db.QueryRow(`
		SELECT m.subject_digest
		FROM manifests m
		JOIN repositories r ON r.id = m.repository_id
		WHERE m.tenant = ? AND r.tenant = ? AND r.name = ? AND m.digest = ?
	`, tenant, tenant, repository, digest).Scan(&subjectDigest)
	if err != nil {
		t.Fatalf("query subject_digest error = %v", err)
	}

	return subjectDigest
}

// queryBackfillMarker reads schema_backfills.completed_at for the
// subject_digest backfill marker, or "" if absent.
func queryBackfillMarker(t *testing.T, store *Store) string {
	t.Helper()

	var completedAt string
	err := store.db.QueryRow(`SELECT completed_at FROM schema_backfills WHERE name = ?`, "manifests.subject_digest.v1").Scan(&completedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return ""
		}
		t.Fatalf("query schema_backfills error = %v", err)
	}

	return completedAt
}

// queryBackfillMarkerCount reads how many schema_backfills rows carry the
// subject_digest backfill marker name -- must never exceed 1.
func queryBackfillMarkerCount(t *testing.T, store *Store) int {
	t.Helper()

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM schema_backfills WHERE name = ?`, "manifests.subject_digest.v1").Scan(&count); err != nil {
		t.Fatalf("count schema_backfills error = %v", err)
	}

	return count
}

// insertRawManifestRow directly inserts a manifest row (bypassing
// PublishManifest/domain.NewManifest validation entirely) to simulate a row
// written by a build that predates this migration -- exactly what
// backfillSubjectDigests must repair. subjectDigest is the column's
// pre-migration value (an empty string for every row before the backfill runs).
func insertRawManifestRow(t *testing.T, store *Store, tenant string, repository string, digest string, payload []byte, subjectDigest string) {
	t.Helper()

	var repositoryID int64
	err := store.db.QueryRow(`SELECT id FROM repositories WHERE tenant = ? AND name = ?`, tenant, repository).Scan(&repositoryID)
	if err != nil {
		if err != sql.ErrNoRows {
			t.Fatalf("lookup repository error = %v", err)
		}

		result, insertErr := store.db.Exec(`INSERT INTO repositories (tenant, name, created_at) VALUES (?, ?, ?)`, tenant, repository, time.Now().UTC().Format(time.RFC3339Nano))
		if insertErr != nil {
			t.Fatalf("insert repository error = %v", insertErr)
		}
		repositoryID, err = result.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId() error = %v", err)
		}
	}

	_, err = store.db.Exec(`
		INSERT INTO manifests (tenant, repository_id, digest, media_type, size, payload, created_at, pushed_by, subject_digest)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)
	`, tenant, repositoryID, digest, "application/vnd.oci.image.manifest.v1+json", int64(len(payload)), payload, time.Now().UTC().Format(time.RFC3339Nano), subjectDigest)
	if err != nil {
		t.Fatalf("insert manifest row error = %v", err)
	}
}

// explainQueryPlan runs EXPLAIN QUERY PLAN for the given query and returns
// the concatenated "detail" column, following
// TestMarkGCReportDeletedCandidateUpdateUsesTheReportDigestIndex's pattern
// above.
func explainQueryPlan(t *testing.T, store *Store, query string, args ...any) string {
	t.Helper()

	rows, err := store.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("; ")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}

	return plan.String()
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return store
}

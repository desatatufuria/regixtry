package gitleaks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

func TestRuntimeManagerInstallAbortsWhenChecksumMismatches(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := metadata.New(filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()

	manager := NewRuntimeManager(RuntimeManagerConfig{
		StorageRoot:   root,
		Store:         store,
		ReleaseClient: fakeReleaseClient{}.withMismatchedChecksum(t, "8.27.0", "binary-8.27.0"),
		Prober:        fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "8.27.0"}},
		Now:           func() time.Time { return time.Date(2026, time.August, 11, 20, 0, 0, 0, time.UTC) },
	})

	if _, err := manager.Install(context.Background(), "8.27.0", nil); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Install() error = %v, want checksum mismatch rejection", err)
	}

	state, err := store.GetFeatureRuntimeState(context.Background(), ports.DefaultTenant, "gitleaks")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState() error = %v", err)
	}
	if state.Status == ports.FeatureRuntimeStatusReady {
		t.Fatalf("state = %#v, want mismatched checksum to never reach ready status", state)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "features", "gitleaks", "bin", "active")); statErr == nil {
		t.Fatalf("active binary link exists after checksum mismatch, want no activation")
	}
}

func TestRuntimeManagerInstallStagesActivationAndRetainsRollbackTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := metadata.New(filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()

	manager := NewRuntimeManager(RuntimeManagerConfig{
		StorageRoot:   root,
		Store:         store,
		ReleaseClient: fakeReleaseClient{}.withArchive(t, "8.27.0", "binary-8.27.0"),
		Prober:        fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "8.27.0"}},
		Now:           func() time.Time { return time.Date(2026, time.August, 11, 20, 0, 0, 0, time.UTC) },
	})

	installed, err := manager.Install(context.Background(), "8.27.0", nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if installed.Status != ports.FeatureRuntimeStatusReady || installed.ActiveVersion != "8.27.0" {
		t.Fatalf("installed = %#v, want ready active version", installed)
	}
	if _, err := os.Stat(installed.ActiveBinaryPath); err != nil {
		t.Fatalf("Stat(active binary) error = %v", err)
	}

	manager.releaseClient = fakeReleaseClient{}.withArchive(t, "8.28.0", "binary-8.28.0")
	manager.prober = fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "8.28.0"}}
	upgraded, err := manager.Upgrade(context.Background(), "8.28.0", nil)
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if upgraded.ActiveVersion != "8.28.0" || upgraded.PreviousVersion != "8.27.0" {
		t.Fatalf("upgraded = %#v, want retained rollback target", upgraded)
	}

	rolledBack, err := manager.Rollback(context.Background())
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if rolledBack.ActiveVersion != "8.27.0" {
		t.Fatalf("rolledBack = %#v, want previous version restored", rolledBack)
	}
}

func TestRuntimeManagerRestoresPreviousRuntimeWhenActivationProbeFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := metadata.New(filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()

	manager := NewRuntimeManager(RuntimeManagerConfig{
		StorageRoot:   root,
		Store:         store,
		ReleaseClient: fakeReleaseClient{}.withArchive(t, "8.27.0", "binary-8.27.0"),
		Prober:        fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "8.27.0"}},
		Now:           func() time.Time { return time.Date(2026, time.August, 11, 20, 0, 0, 0, time.UTC) },
	})
	if _, err := manager.Install(context.Background(), "8.27.0", nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	manager.releaseClient = fakeReleaseClient{}.withArchive(t, "8.28.0", "binary-8.28.0")
	manager.prober = fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "degraded"}, err: errors.New("probe failed")}
	if _, err := manager.Upgrade(context.Background(), "8.28.0", nil); err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("Upgrade() error = %v, want probe failure", err)
	}

	state, err := store.GetFeatureRuntimeState(context.Background(), ports.DefaultTenant, "gitleaks")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState() error = %v", err)
	}
	if state.ActiveVersion != "8.27.0" {
		t.Fatalf("state = %#v, want previous version restored after failed activation", state)
	}
}

type fakeRuntimeProber struct {
	runtime ports.FeatureRuntime
	err     error
}

func (f fakeRuntimeProber) Probe(context.Context, ports.ScanSettings) (ports.FeatureRuntime, error) {
	return f.runtime, f.err
}

type fakeReleaseClient struct {
	asset      releaseAsset
	payloads   map[string][]byte
	resolveErr error
}

func (f fakeReleaseClient) ResolveRelease(context.Context, string) (releaseAsset, error) {
	if f.resolveErr != nil {
		return releaseAsset{}, f.resolveErr
	}
	return f.asset, nil
}

func (f fakeReleaseClient) DownloadReleaseAsset(context.Context, string) ([]byte, error) {
	return f.payloads[f.asset.ArchiveURL], nil
}

func (f fakeReleaseClient) DownloadChecksums(context.Context, string) ([]byte, error) {
	return f.payloads[f.asset.ChecksumsURL], nil
}

func (f fakeReleaseClient) withArchive(t *testing.T, version string, binaryBody string) fakeReleaseClient {
	t.Helper()
	archiveName := "gitleaks_" + version + "_linux_x64.tar.gz"
	archiveBody := makeGitleaksArchive(t, binaryBody)
	checksum := sha256.Sum256(archiveBody)
	return fakeReleaseClient{
		asset: releaseAsset{Version: version, ArchiveName: archiveName, ArchiveURL: "archive://" + version, ChecksumsURL: "checksums://" + version},
		payloads: map[string][]byte{
			"archive://" + version:   archiveBody,
			"checksums://" + version: []byte(hex.EncodeToString(checksum[:]) + "  " + archiveName + "\n"),
		},
	}
}

// withMismatchedChecksum returns a fake release client whose checksums file
// does not match the archive body, exercising the checksum-mismatch-aborts-
// install threat-matrix case (binary provenance).
func (f fakeReleaseClient) withMismatchedChecksum(t *testing.T, version string, binaryBody string) fakeReleaseClient {
	t.Helper()
	archiveName := "gitleaks_" + version + "_linux_x64.tar.gz"
	archiveBody := makeGitleaksArchive(t, binaryBody)
	wrongChecksum := sha256.Sum256([]byte("not-the-archive-body"))
	return fakeReleaseClient{
		asset: releaseAsset{Version: version, ArchiveName: archiveName, ArchiveURL: "archive://" + version, ChecksumsURL: "checksums://" + version},
		payloads: map[string][]byte{
			"archive://" + version:   archiveBody,
			"checksums://" + version: []byte(hex.EncodeToString(wrongChecksum[:]) + "  " + archiveName + "\n"),
		},
	}
}

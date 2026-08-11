package trivy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

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
		ReleaseClient: fakeReleaseClient{}.withArchive(t, "0.57.1", "binary-0.57.1"),
		Prober:        fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "0.57.1"}},
		Now:           func() time.Time { return time.Date(2026, time.August, 10, 20, 0, 0, 0, time.UTC) },
	})

	installed, err := manager.Install(context.Background(), "0.57.1", nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if installed.Status != ports.FeatureRuntimeStatusReady || installed.ActiveVersion != "0.57.1" {
		t.Fatalf("installed = %#v, want ready active version", installed)
	}
	if installed.ActiveBinaryPath == "" || installed.CacheDir == "" || installed.ReceiptPath == "" {
		t.Fatalf("installed = %#v, want managed runtime paths", installed)
	}
	if _, err := os.Stat(installed.ActiveBinaryPath); err != nil {
		t.Fatalf("Stat(active binary) error = %v", err)
	}

	manager.releaseClient = fakeReleaseClient{}.withArchive(t, "0.58.0", "binary-0.58.0")
	manager.prober = fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "0.58.0"}}
	upgraded, err := manager.Upgrade(context.Background(), "0.58.0", nil)
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if upgraded.ActiveVersion != "0.58.0" || upgraded.PreviousVersion != "0.57.1" {
		t.Fatalf("upgraded = %#v, want retained rollback target", upgraded)
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
		ReleaseClient: fakeReleaseClient{}.withArchive(t, "0.57.1", "binary-0.57.1"),
		Prober:        fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "0.57.1"}},
		Now:           func() time.Time { return time.Date(2026, time.August, 10, 20, 0, 0, 0, time.UTC) },
	})
	if _, err := manager.Install(context.Background(), "0.57.1", nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	manager.releaseClient = fakeReleaseClient{}.withArchive(t, "0.58.0", "binary-0.58.0")
	manager.prober = fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "degraded"}, err: errors.New("probe failed")}
	if _, err := manager.Upgrade(context.Background(), "0.58.0", nil); err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("Upgrade() error = %v, want probe failure", err)
	}

	state, err := store.GetFeatureRuntimeState(context.Background(), ports.DefaultTenant, "trivy")
	if err != nil {
		t.Fatalf("GetFeatureRuntimeState() error = %v", err)
	}
	if state.ActiveVersion != "0.57.1" {
		t.Fatalf("state = %#v, want previous version restored after failed activation", state)
	}
}

func TestRuntimeManagerInstallAndUpgradeEmitProgressStages(t *testing.T) {
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
		ReleaseClient: fakeReleaseClient{}.withArchive(t, "0.57.1", "binary-0.57.1"),
		Prober:        fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "0.57.1"}},
		Now:           func() time.Time { return time.Date(2026, time.August, 10, 20, 0, 0, 0, time.UTC) },
	})

	var installStages []string
	if _, err := manager.Install(context.Background(), "0.57.1", func(progress ports.FeatureRuntimeProgress) {
		installStages = append(installStages, progress.Stage)
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got, want := installStages, []string{"resolve", "download", "verify", "extract", "activate", "probe", "complete"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("install stages = %#v, want %#v", got, want)
	}

	manager.releaseClient = fakeReleaseClient{}.withArchive(t, "0.58.0", "binary-0.58.0")
	manager.prober = fakeRuntimeProber{runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Health: "ready", Version: "0.58.0"}}
	var upgradeStages []string
	if _, err := manager.Upgrade(context.Background(), "0.58.0", func(progress ports.FeatureRuntimeProgress) {
		upgradeStages = append(upgradeStages, progress.Stage)
	}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if got, want := upgradeStages, []string{"resolve", "download", "verify", "extract", "activate", "probe", "complete"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade stages = %#v, want %#v", got, want)
	}
}

func TestRuntimeManagerLatestVersionUsesReleaseLookupAndPropagatesFailure(t *testing.T) {
	t.Parallel()

	manager := NewRuntimeManager(RuntimeManagerConfig{ReleaseClient: fakeReleaseClient{asset: releaseAsset{Version: "0.58.0"}}})
	latest, err := manager.LatestVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestVersion() error = %v", err)
	}
	if got, want := latest, "0.58.0"; got != want {
		t.Fatalf("LatestVersion() = %q, want %q", got, want)
	}

	manager.releaseClient = fakeReleaseClient{resolveErr: errors.New("lookup failed")}
	if _, err := manager.LatestVersion(context.Background()); err == nil || !strings.Contains(err.Error(), "lookup failed") {
		t.Fatalf("LatestVersion() error = %v, want lookup failure", err)
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
	archiveName := "trivy_" + version + "_Linux-64bit.tar.gz"
	archiveBody := makeTrivyArchive(t, binaryBody)
	checksum := sha256.Sum256(archiveBody)
	return fakeReleaseClient{
		asset: releaseAsset{Version: version, ArchiveName: archiveName, ArchiveURL: "archive://" + version, ChecksumsURL: "checksums://" + version},
		payloads: map[string][]byte{
			"archive://" + version:   archiveBody,
			"checksums://" + version: []byte(hex.EncodeToString(checksum[:]) + "  " + archiveName + "\n"),
		},
	}
}

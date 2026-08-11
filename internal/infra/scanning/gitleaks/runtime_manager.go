package gitleaks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"regixtry/internal/infra/release"
	"regixtry/internal/ports"
)

const gitleaksFeatureName = "gitleaks"

type runtimeProber interface {
	Probe(ctx context.Context, settings ports.ScanSettings) (ports.FeatureRuntime, error)
}

type RuntimeManagerConfig struct {
	StorageRoot   string
	Store         ports.MetadataStore
	ReleaseClient releaseClient
	Prober        runtimeProber
	Now           func() time.Time
}

type RuntimeManager struct {
	storageRoot   string
	store         ports.MetadataStore
	releaseClient releaseClient
	prober        runtimeProber
	now           func() time.Time
}

func NewRuntimeManager(cfg RuntimeManagerConfig) *RuntimeManager {
	releaseClient := cfg.ReleaseClient
	if releaseClient == nil {
		releaseClient = newGitHubReleaseClient()
	}
	prober := cfg.Prober
	if prober == nil {
		prober = New(RunnerConfig{})
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &RuntimeManager{storageRoot: cfg.StorageRoot, store: cfg.Store, releaseClient: releaseClient, prober: prober, now: now}
}

func (m *RuntimeManager) Install(ctx context.Context, version string, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	return m.activate(ctx, version, false, progress)
}

func (m *RuntimeManager) Upgrade(ctx context.Context, version string, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	return m.activate(ctx, version, true, progress)
}

func (m *RuntimeManager) LatestVersion(ctx context.Context) (string, error) {
	asset, err := m.releaseClient.ResolveRelease(ctx, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(asset.Version), nil
}

func (m *RuntimeManager) Rollback(ctx context.Context) (ports.FeatureRuntimeState, error) {
	current, err := m.currentState(ctx)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	if strings.TrimSpace(current.PreviousVersion) == "" {
		return ports.FeatureRuntimeState{}, fmt.Errorf("rollback target is unavailable")
	}
	rollbackVersion := current.PreviousVersion
	previousVersion := current.ActiveVersion
	if err := m.activateVersionLink(rollbackVersion); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	runtime, probeErr := m.probeActiveBinary(ctx)
	if probeErr != nil {
		_ = m.activateVersionLink(previousVersion)
		return ports.FeatureRuntimeState{}, probeErr
	}
	state := ports.FeatureRuntimeState{
		Status:            ports.FeatureRuntimeStatusReady,
		ActiveVersion:     rollbackVersion,
		PreviousVersion:   previousVersion,
		ActiveBinaryPath:  m.activeBinaryPath(),
		CacheDir:          m.cacheDir(),
		ReceiptPath:       m.receiptPath(rollbackVersion),
		LastVerifiedAt:    ptrTime(m.now()),
		LastHealthCheckAt: ptrTime(m.now()),
		LastError:         "",
		UpdatedAt:         m.now(),
	}
	if runtime.Version != "" {
		state.ActiveVersion = runtime.Version
		state.PreviousVersion = previousVersion
	}
	if err := m.store.UpsertFeatureRuntimeState(ctx, ports.DefaultTenant, gitleaksFeatureName, state); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	return state, nil
}

func (m *RuntimeManager) Status(ctx context.Context) (ports.FeatureRuntimeState, error) {
	state, err := m.currentState(ctx)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	if state.Status == ports.FeatureRuntimeStatusReady || state.Status == ports.FeatureRuntimeStatusDegraded {
		runtime, probeErr := m.probeActiveBinary(ctx)
		now := m.now()
		state.LastHealthCheckAt = &now
		if probeErr != nil {
			state.Status = ports.FeatureRuntimeStatusDegraded
			state.LastError = probeErr.Error()
		} else {
			state.Status = ports.FeatureRuntimeStatusReady
			state.LastError = ""
			if strings.TrimSpace(runtime.Version) != "" {
				state.ActiveVersion = runtime.Version
			}
		}
		state.UpdatedAt = now
		if err := m.store.UpsertFeatureRuntimeState(ctx, ports.DefaultTenant, gitleaksFeatureName, state); err != nil {
			return ports.FeatureRuntimeState{}, err
		}
	}
	return state, nil
}

func (m *RuntimeManager) activate(ctx context.Context, version string, requireCurrent bool, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	current, currentErr := m.currentState(ctx)
	if requireCurrent && currentErr != nil {
		return ports.FeatureRuntimeState{}, currentErr
	}
	emitFeatureRuntimeProgress(progress, "resolve", "Resolve release")
	asset, err := m.releaseClient.ResolveRelease(ctx, version)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	if err := m.ensureLayout(); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	now := m.now()
	installing := ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusInstalling, ActiveVersion: current.ActiveVersion, PreviousVersion: current.PreviousVersion, UpdatedAt: now, LastError: ""}
	if err := m.store.UpsertFeatureRuntimeState(ctx, ports.DefaultTenant, gitleaksFeatureName, installing); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	emitFeatureRuntimeProgress(progress, "download", "Download archive")
	archiveBody, err := m.releaseClient.DownloadReleaseAsset(ctx, asset.ArchiveURL)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	checksumsBody, err := m.releaseClient.DownloadChecksums(ctx, asset.ChecksumsURL)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	archivePath := filepath.Join(m.downloadsDir(), asset.ArchiveName)
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	emitFeatureRuntimeProgress(progress, "verify", "Verify checksum")
	if err := release.VerifyChecksum(archivePath, asset.ArchiveName, checksumsBody); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	versionDir := m.versionDir(asset.Version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	emitFeatureRuntimeProgress(progress, "extract", "Extract binary")
	binaryPath, err := release.ExtractBinary(ctx, archivePath, versionDir, "gitleaks")
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	receiptPath := m.receiptPath(asset.Version)
	if err := m.writeReceipt(receiptPath, asset, current.ActiveVersion); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	emitFeatureRuntimeProgress(progress, "activate", "Activate runtime")
	if err := m.activateVersionLink(asset.Version); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	emitFeatureRuntimeProgress(progress, "probe", "Probe runtime")
	runtime, probeErr := m.prober.Probe(ctx, ports.ScanSettings{BinaryPath: binaryPath, CacheDir: m.cacheDir(), Timeout: 30 * time.Second, MaxConcurrency: 1})
	if probeErr != nil {
		if strings.TrimSpace(current.ActiveVersion) != "" {
			_ = m.activateVersionLink(current.ActiveVersion)
		}
		return ports.FeatureRuntimeState{}, probeErr
	}
	now = m.now()
	state := ports.FeatureRuntimeState{
		Status:            ports.FeatureRuntimeStatusReady,
		ActiveVersion:     firstNonBlank(runtime.Version, asset.Version),
		PreviousVersion:   strings.TrimSpace(current.ActiveVersion),
		ActiveBinaryPath:  m.activeBinaryPath(),
		CacheDir:          m.cacheDir(),
		ReceiptPath:       receiptPath,
		LastVerifiedAt:    &now,
		LastHealthCheckAt: &now,
		LastError:         "",
		UpdatedAt:         now,
	}
	if err := m.store.UpsertFeatureRuntimeState(ctx, ports.DefaultTenant, gitleaksFeatureName, state); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	emitFeatureRuntimeProgress(progress, "complete", "Runtime ready")
	return state, nil
}

func emitFeatureRuntimeProgress(progress func(ports.FeatureRuntimeProgress), stage string, detail string) {
	if progress == nil {
		return
	}
	progress(ports.FeatureRuntimeProgress{Stage: strings.TrimSpace(stage), Detail: strings.TrimSpace(detail)})
}

func (m *RuntimeManager) currentState(ctx context.Context) (ports.FeatureRuntimeState, error) {
	if m == nil || m.store == nil {
		return ports.FeatureRuntimeState{}, fmt.Errorf("gitleaks runtime manager is not configured")
	}
	return m.store.GetFeatureRuntimeState(ctx, ports.DefaultTenant, gitleaksFeatureName)
}

func (m *RuntimeManager) ensureLayout() error {
	for _, path := range []string{m.versionsDir(), m.downloadsDir(), m.receiptsDir(), m.cacheDir()} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (m *RuntimeManager) activateVersionLink(version string) error {
	activeLink := m.activeLinkPath()
	tempLink := activeLink + ".next"
	_ = os.Remove(tempLink)
	if err := os.Symlink(m.versionDir(version), tempLink); err != nil {
		return err
	}
	if err := os.Rename(tempLink, activeLink); err != nil {
		_ = os.Remove(tempLink)
		return err
	}
	return nil
}

func (m *RuntimeManager) probeActiveBinary(ctx context.Context) (ports.FeatureRuntime, error) {
	return m.prober.Probe(ctx, ports.ScanSettings{BinaryPath: m.activeBinaryPath(), CacheDir: m.cacheDir(), Timeout: 30 * time.Second, MaxConcurrency: 1})
}

func (m *RuntimeManager) writeReceipt(path string, asset releaseAsset, previousVersion string) error {
	body, err := json.MarshalIndent(map[string]string{
		"version":          asset.Version,
		"archive_name":     asset.ArchiveName,
		"archive_url":      asset.ArchiveURL,
		"checksums_url":    asset.ChecksumsURL,
		"previous_version": previousVersion,
		"installed_at":     m.now().Format(time.RFC3339Nano),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func (m *RuntimeManager) runtimeRoot() string {
	return filepath.Join(strings.TrimSpace(m.storageRoot), "features", "gitleaks")
}
func (m *RuntimeManager) versionsDir() string {
	return filepath.Join(m.runtimeRoot(), "bin", "versions")
}
func (m *RuntimeManager) versionDir(version string) string {
	return filepath.Join(m.versionsDir(), strings.TrimSpace(version))
}
func (m *RuntimeManager) activeLinkPath() string {
	return filepath.Join(m.runtimeRoot(), "bin", "active")
}
func (m *RuntimeManager) activeBinaryPath() string {
	return filepath.Join(m.activeLinkPath(), "gitleaks")
}
func (m *RuntimeManager) cacheDir() string     { return filepath.Join(m.runtimeRoot(), "gitleaks-cache") }
func (m *RuntimeManager) downloadsDir() string { return filepath.Join(m.runtimeRoot(), "downloads") }
func (m *RuntimeManager) receiptsDir() string  { return filepath.Join(m.runtimeRoot(), "receipts") }
func (m *RuntimeManager) receiptPath(version string) string {
	return filepath.Join(m.receiptsDir(), strings.TrimSpace(version)+".json")
}

func ptrTime(value time.Time) *time.Time { return &value }

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

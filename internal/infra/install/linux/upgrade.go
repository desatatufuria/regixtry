package linux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/infra/install/releases"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

type UpgradeConfig struct {
	Ref            string
	ProvenancePath string
	AssumeYes      bool
	Preflight      func(UpgradePreflight) error
	Confirm        func(UpgradePreflight) error
	Progress       func(UpgradeProgress)
}

type UpgradePreflight struct {
	InstalledRef     string
	InstalledVersion string
	TargetRef        string
	TargetVersion    string
	UpToDate         bool
}

type UpgradeResult struct {
	FromRef        string
	FromVersion    string
	ToVersion      string
	TargetRef      string
	ProvenancePath string
	UpToDate       bool
}

type UpgradeProgress struct {
	Stage       string
	Detail      string
	FromRef     string
	FromVersion string
	ToRef       string
	ToVersion   string
}

type releaseClient interface {
	Resolve(context.Context, string, string, string) (releases.ReleaseAsset, error)
	DownloadVerifiedBinary(context.Context, releases.ReleaseAsset, string, func(releases.DownloadProgress)) (string, error)
}

var newReleaseClient = func() releaseClient {
	return releases.NewGitHubClient(os.Getenv("REGISTRY_INSTALL_RELEASES_API_URL"))
}

type managedRuntimeArtifactBackup struct {
	Path       string
	Body       []byte
	Mode       os.FileMode
	BackupPath string
}

type managedRuntimeBackup struct {
	Binary           managedRuntimeArtifactBackup
	Env              managedRuntimeArtifactBackup
	Unit             managedRuntimeArtifactBackup
	BootstrapReceipt managedRuntimeArtifactBackup
	Provenance       managedRuntimeArtifactBackup
	Plan             BootstrapPlan
}

func (b *Bootstrapper) Upgrade(ctx context.Context, cfg UpgradeConfig) (UpgradeResult, error) {
	provenancePath := strings.TrimSpace(cfg.ProvenancePath)
	if provenancePath == "" {
		provenancePath = LifecycleProvenancePath("/etc/regixtry/bootstrap-state.json")
	}
	provenance, err := b.readLifecycleProvenance(provenancePath)
	if err != nil {
		return UpgradeResult{}, err
	}
	envValues, err := b.readManagedEnv(intentEnvPath(provenance))
	if err != nil {
		return UpgradeResult{}, err
	}
	intent, err := loadInstalledIntent(provenance, envValues)
	if err != nil {
		return UpgradeResult{}, err
	}
	if shouldImportLegacyTrivySettings(provenance, envValues) {
		if err := importLegacyTrivySettingsIfMissing(ctx, intent); err != nil {
			return UpgradeResult{}, err
		}
	}
	plan := buildPlanFromInstalledIntent(intent)
	arch, err := releases.CurrentLinuxArch()
	if err != nil {
		return UpgradeResult{}, err
	}
	asset, err := newReleaseClient().Resolve(ctx, cfg.Ref, "linux", arch)
	if err != nil {
		return UpgradeResult{}, err
	}
	preflight := UpgradePreflight{
		InstalledRef:     provenance.InstalledRef,
		InstalledVersion: intent.InstalledVersion,
		TargetRef:        asset.Tag,
		TargetVersion:    asset.Version,
		UpToDate:         sameUpgradeVersion(intent.InstalledVersion, asset.Version, provenance.InstalledRef, asset.Tag),
	}
	if err := reportUpgradePreflight(cfg.Preflight, preflight); err != nil {
		return UpgradeResult{}, err
	}
	if preflight.UpToDate {
		return UpgradeResult{
			FromRef:        provenance.InstalledRef,
			FromVersion:    intent.InstalledVersion,
			ToVersion:      asset.Version,
			TargetRef:      asset.Tag,
			ProvenancePath: provenance.StatePath,
			UpToDate:       true,
		}, nil
	}
	if err := confirmUpgrade(preflight, cfg.AssumeYes, cfg.Confirm); err != nil {
		return UpgradeResult{}, err
	}
	reportUpgradeProgress(cfg.Progress, UpgradeProgress{
		Stage:       "resolve",
		Detail:      fmt.Sprintf("Upgrading from %s to %s", formatUpgradeIdentity(provenance.InstalledRef, intent.InstalledVersion), formatUpgradeIdentity(asset.Tag, asset.Version)),
		FromRef:     provenance.InstalledRef,
		FromVersion: intent.InstalledVersion,
		ToRef:       asset.Tag,
		ToVersion:   asset.Version,
	})

	stageDir, err := os.MkdirTemp("", "regixtry-upgrade-*")
	if err != nil {
		return UpgradeResult{}, err
	}
	defer os.RemoveAll(stageDir)

	stagedBinaryPath, err := newReleaseClient().DownloadVerifiedBinary(ctx, asset, stageDir, func(progress releases.DownloadProgress) {
		reportUpgradeProgress(cfg.Progress, UpgradeProgress{
			Stage:       progress.Stage,
			Detail:      progress.Detail,
			FromRef:     provenance.InstalledRef,
			FromVersion: intent.InstalledVersion,
			ToRef:       asset.Tag,
			ToVersion:   asset.Version,
		})
	})
	if err != nil {
		return UpgradeResult{}, err
	}
	if _, err := b.detector.Detect(); err != nil {
		return UpgradeResult{}, err
	}

	backup, err := b.captureManagedRuntimeBackup(plan, provenancePath)
	if err != nil {
		return UpgradeResult{}, err
	}
	reportUpgradeProgress(cfg.Progress, UpgradeProgress{Stage: "stop", Detail: fmt.Sprintf("Stopping %s.service", plan.ServiceName), FromRef: provenance.InstalledRef, FromVersion: intent.InstalledVersion, ToRef: asset.Tag, ToVersion: asset.Version})

	if err := b.runCommand(ctx, "systemctl", "disable", "--now", plan.ServiceName+".service"); err != nil {
		return UpgradeResult{}, fmt.Errorf("systemctl disable --now %s.service: %w", plan.ServiceName, err)
	}

	rollback := func(runErr error) error {
		reportUpgradeProgress(cfg.Progress, UpgradeProgress{Stage: "rollback", Detail: "Restoring previous installation", FromRef: provenance.InstalledRef, FromVersion: intent.InstalledVersion, ToRef: asset.Tag, ToVersion: asset.Version})
		if rollbackErr := b.restoreManagedRuntimeBackup(ctx, backup); rollbackErr != nil {
			return errors.Join(runErr, rollbackErr)
		}
		return runErr
	}

	reportUpgradeProgress(cfg.Progress, UpgradeProgress{Stage: "swap", Detail: fmt.Sprintf("Swapping installed binary at %s", plan.BinaryPath), FromRef: provenance.InstalledRef, FromVersion: intent.InstalledVersion, ToRef: asset.Tag, ToVersion: asset.Version})
	binaryBackupPath, err := b.swapInstalledBinary(plan.BinaryPath, stagedBinaryPath, backup.Binary.Mode)
	if err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("replace installed binary: %w", err))
	}
	backup.Binary.BackupPath = binaryBackupPath
	if err := b.writeManagedArtifacts(plan, bootstrapReceiptFromPlan(plan), false); err != nil {
		return UpgradeResult{}, rollback(err)
	}
	reportUpgradeProgress(cfg.Progress, UpgradeProgress{Stage: "restart", Detail: fmt.Sprintf("Restarting %s.service", plan.ServiceName), FromRef: provenance.InstalledRef, FromVersion: intent.InstalledVersion, ToRef: asset.Tag, ToVersion: asset.Version})
	if err := b.runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("systemctl daemon-reload: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "enable", "--now", plan.ServiceName+".service"); err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("systemctl enable --now %s.service: %w", plan.ServiceName, err))
	}
	reportUpgradeProgress(cfg.Progress, UpgradeProgress{Stage: "health-check", Detail: fmt.Sprintf("Waiting for %s health check", plan.ServiceName), FromRef: provenance.InstalledRef, FromVersion: intent.InstalledVersion, ToRef: asset.Tag, ToVersion: asset.Version})
	if err := b.waitUntilReachable(ctx, plan); err != nil {
		return UpgradeResult{}, rollback(err)
	}

	provenance = lifecycleProvenanceFromPlan(plan, bootstrapReceiptFromPlan(plan))
	provenance.StatePath = provenancePath
	provenance.InstalledRef = asset.Tag
	provenance.InstalledVersion = asset.Version
	if err := b.writeLifecycleProvenance(provenance); err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("write lifecycle provenance: %w", err))
	}
	if backup.Binary.BackupPath != "" {
		if err := b.removeAll(backup.Binary.BackupPath); err != nil {
			return UpgradeResult{}, rollback(fmt.Errorf("remove binary backup: %w", err))
		}
		backup.Binary.BackupPath = ""
	}

	return UpgradeResult{
		FromRef:        preflight.InstalledRef,
		FromVersion:    intent.InstalledVersion,
		ToVersion:      asset.Version,
		TargetRef:      asset.Tag,
		ProvenancePath: provenance.StatePath,
	}, nil
}

func reportUpgradePreflight(preflight func(UpgradePreflight) error, event UpgradePreflight) error {
	if preflight == nil {
		return nil
	}
	return preflight(event)
}

func reportUpgradeProgress(progress func(UpgradeProgress), event UpgradeProgress) {
	if progress == nil {
		return
	}
	progress(event)
}

func formatUpgradeIdentity(ref string, version string) string {
	trimmedRef := strings.TrimSpace(ref)
	trimmedVersion := strings.TrimSpace(version)
	if trimmedRef != "" && trimmedVersion != "" {
		return fmt.Sprintf("%s (%s)", trimmedRef, trimmedVersion)
	}
	if trimmedRef != "" {
		return trimmedRef
	}
	if trimmedVersion != "" {
		return trimmedVersion
	}
	return "unknown version"
}

func sameUpgradeVersion(installedVersion string, targetVersion string, installedRef string, targetRef string) bool {
	trimmedInstalledVersion := strings.TrimSpace(installedVersion)
	trimmedTargetVersion := strings.TrimSpace(targetVersion)
	if trimmedInstalledVersion != "" && trimmedTargetVersion != "" {
		return trimmedInstalledVersion == trimmedTargetVersion
	}
	trimmedInstalledRef := strings.TrimSpace(installedRef)
	trimmedTargetRef := strings.TrimSpace(targetRef)
	return trimmedInstalledRef != "" && trimmedInstalledRef == trimmedTargetRef
}

func (b *Bootstrapper) swapInstalledBinary(installedPath string, stagedBinaryPath string, mode os.FileMode) (string, error) {
	dir := filepath.Dir(installedPath)
	base := filepath.Base(installedPath)
	rename := b.rename
	if rename == nil {
		rename = os.Rename
	}
	removeAll := b.removeAll
	if removeAll == nil {
		removeAll = os.RemoveAll
	}

	stagedSwapFile, err := os.CreateTemp(dir, "."+base+".upgrade-*")
	if err != nil {
		return "", fmt.Errorf("create staged swap file: %w", err)
	}
	stagedSwapPath := stagedSwapFile.Name()
	defer func() {
		if stagedSwapPath != "" {
			_ = removeAll(stagedSwapPath)
		}
	}()

	stagedBinary, err := os.Open(stagedBinaryPath)
	if err != nil {
		return "", fmt.Errorf("open staged binary: %w", err)
	}
	defer stagedBinary.Close()

	if _, err := io.Copy(stagedSwapFile, stagedBinary); err != nil {
		return "", fmt.Errorf("copy staged binary into swap file: %w", err)
	}
	if err := stagedSwapFile.Chmod(mode); err != nil {
		return "", fmt.Errorf("chmod staged swap file: %w", err)
	}
	if err := stagedSwapFile.Close(); err != nil {
		return "", fmt.Errorf("close staged swap file: %w", err)
	}

	backupFile, err := os.CreateTemp(dir, "."+base+".backup-*")
	if err != nil {
		return "", fmt.Errorf("create binary backup path: %w", err)
	}
	backupPath := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		return "", fmt.Errorf("close binary backup path: %w", err)
	}
	if err := removeAll(backupPath); err != nil {
		return "", fmt.Errorf("prepare binary backup path: %w", err)
	}

	if err := rename(installedPath, backupPath); err != nil {
		return "", fmt.Errorf("backup installed binary: %w", err)
	}
	if err := rename(stagedSwapPath, installedPath); err != nil {
		restoreErr := rename(backupPath, installedPath)
		if restoreErr != nil {
			return "", errors.Join(fmt.Errorf("activate staged binary: %w", err), fmt.Errorf("restore installed binary after failed swap: %w", restoreErr))
		}
		return "", fmt.Errorf("activate staged binary: %w", err)
	}

	stagedSwapPath = ""
	return backupPath, nil
}

func importLegacyTrivySettingsIfMissing(ctx context.Context, intent InstalledIntent) error {
	store, err := metadata.New(intent.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	if _, err := store.GetScanSettings(ctx, ports.DefaultTenant); err == nil {
		return nil
	} else if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return err
	}

	cacheDir := strings.TrimSpace(intent.TrivyCacheDir)
	if cacheDir == "" {
		if strings.TrimSpace(intent.StorageRoot) != "" {
			cacheDir = filepath.Join(intent.StorageRoot, "trivy-cache")
		} else {
			cacheDir = filepath.Join(".", "trivy-cache")
		}
	}
	settings := ports.ScanSettings{
		Enabled:         intent.TrivyEnabled,
		ScheduleEnabled: intent.TrivyScheduleEnabled,
		Interval:        firstPositiveDuration(intent.TrivyInterval, 24*time.Hour),
		Timeout:         firstPositiveDuration(intent.TrivyTimeout, 15*time.Minute),
		CacheDir:        cacheDir,
		BinaryPath:      firstNonBlank(intent.TrivyBinaryPath, "trivy"),
		MaxConcurrency:  firstPositiveInt(intent.TrivyMaxConcurrency, 1),
		UpdatedAt:       time.Now().UTC(),
	}
	return store.UpsertScanSettings(ctx, ports.DefaultTenant, settings)
}

func shouldImportLegacyTrivySettings(provenance LifecycleProvenance, envValues map[string]string) bool {
	if provenance.Intent.TrivyEnabled || provenance.Intent.TrivyScheduleEnabled || strings.TrimSpace(provenance.Intent.TrivyInterval) != "" || strings.TrimSpace(provenance.Intent.TrivyTimeout) != "" || strings.TrimSpace(provenance.Intent.TrivyCacheDir) != "" || strings.TrimSpace(provenance.Intent.TrivyBinaryPath) != "" || provenance.Intent.TrivyMaxConcurrency > 0 {
		return true
	}
	for _, key := range []string{
		"REGISTRY_TRIVY_ENABLED",
		"REGISTRY_TRIVY_SCHEDULE_ENABLED",
		"REGISTRY_TRIVY_INTERVAL",
		"REGISTRY_TRIVY_TIMEOUT",
		"REGISTRY_TRIVY_CACHE_DIR",
		"REGISTRY_TRIVY_BINARY_PATH",
		"REGISTRY_TRIVY_MAX_CONCURRENCY",
	} {
		if strings.TrimSpace(envValues[key]) != "" {
			return true
		}
	}
	return false
}

func firstPositiveDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func firstPositiveInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func intentEnvPath(provenance LifecycleProvenance) string {
	return firstNonBlank(provenance.Intent.EnvPath, managedPathMatch(provenance.ManagedPaths, func(path string) bool {
		return filepath.Base(path) == "regixtry.env"
	}), filepath.Join(filepath.Dir(provenance.StatePath), "regixtry.env"))
}

func (b *Bootstrapper) readManagedEnv(path string) (map[string]string, error) {
	body, err := b.readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read managed runtime env: %w", err)
	}
	return parseManagedEnvFile(body)
}

func confirmUpgrade(preflight UpgradePreflight, assumeYes bool, confirm func(UpgradePreflight) error) error {
	if assumeYes {
		return nil
	}
	if confirm != nil {
		return confirm(preflight)
	}
	return requireUpgradeConfirmation(preflight)
}

func requireUpgradeConfirmation(preflight UpgradePreflight) error {
	if preflight.InstalledVersion == "" {
		return nil
	}
	currentMajor := semanticMajor(preflight.InstalledVersion)
	targetMajor := semanticMajor(preflight.TargetVersion)
	if currentMajor == 0 || targetMajor == 0 || currentMajor == targetMajor {
		return nil
	}
	return fmt.Errorf("major-version upgrade from %s to %s requires --yes", preflight.InstalledVersion, preflight.TargetVersion)
}

func semanticMajor(version string) int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	majorPart, _, _ := strings.Cut(trimmed, ".")
	major, _ := strconv.Atoi(majorPart)
	return major
}

func (b *Bootstrapper) captureManagedRuntimeBackup(plan BootstrapPlan, provenancePath string) (managedRuntimeBackup, error) {
	binary, err := b.captureManagedRuntimeArtifact(plan.BinaryPath, "read installed binary", "stat installed binary")
	if err != nil {
		return managedRuntimeBackup{}, err
	}
	env, err := b.captureManagedRuntimeArtifact(plan.EnvPath, "read env file", "stat env file")
	if err != nil {
		return managedRuntimeBackup{}, err
	}
	unit, err := b.captureManagedRuntimeArtifact(plan.UnitPath, "read service unit", "stat service unit")
	if err != nil {
		return managedRuntimeBackup{}, err
	}
	receipt, err := b.captureManagedRuntimeArtifact(plan.StatePath, "read bootstrap receipt", "stat bootstrap receipt")
	if err != nil {
		return managedRuntimeBackup{}, err
	}
	provenance, err := b.captureManagedRuntimeArtifact(provenancePath, "read lifecycle provenance", "stat lifecycle provenance")
	if err != nil {
		return managedRuntimeBackup{}, err
	}
	return managedRuntimeBackup{
		Binary:           binary,
		Env:              env,
		Unit:             unit,
		BootstrapReceipt: receipt,
		Provenance:       provenance,
		Plan:             plan,
	}, nil
}

func (b *Bootstrapper) captureManagedRuntimeArtifact(path string, readErr string, statErr string) (managedRuntimeArtifactBackup, error) {
	body, err := b.readFile(path)
	if err != nil {
		return managedRuntimeArtifactBackup{}, fmt.Errorf("%s: %w", readErr, err)
	}
	info, err := b.stat(path)
	if err != nil {
		return managedRuntimeArtifactBackup{}, fmt.Errorf("%s: %w", statErr, err)
	}
	return managedRuntimeArtifactBackup{Path: path, Body: body, Mode: info.Mode()}, nil
}

func (b *Bootstrapper) restoreManagedRuntimeBackup(ctx context.Context, backup managedRuntimeBackup) error {
	var errs []error
	rename := b.rename
	if rename == nil {
		rename = os.Rename
	}
	removeAll := b.removeAll
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	if backup.Binary.BackupPath != "" {
		rollbackBinaryPath := backup.Binary.BackupPath + ".rollback"
		if err := rename(backup.Binary.Path, rollbackBinaryPath); err != nil {
			errs = append(errs, fmt.Errorf("move upgraded binary aside: %w", err))
		} else {
			if err := rename(backup.Binary.BackupPath, backup.Binary.Path); err != nil {
				errs = append(errs, fmt.Errorf("restore installed binary from backup: %w", err))
				if moveBackErr := rename(rollbackBinaryPath, backup.Binary.Path); moveBackErr != nil {
					errs = append(errs, fmt.Errorf("restore upgraded binary after failed rollback: %w", moveBackErr))
				}
			} else if err := removeAll(rollbackBinaryPath); err != nil {
				errs = append(errs, fmt.Errorf("cleanup upgraded binary after rollback: %w", err))
			}
		}
	} else if err := b.writeFile(backup.Binary.Path, backup.Binary.Body, backup.Binary.Mode); err != nil {
		errs = append(errs, fmt.Errorf("restore installed binary: %w", err))
	}
	if err := b.writeFile(backup.Env.Path, backup.Env.Body, backup.Env.Mode); err != nil {
		errs = append(errs, fmt.Errorf("restore env file: %w", err))
	}
	if err := b.writeFile(backup.Unit.Path, backup.Unit.Body, backup.Unit.Mode); err != nil {
		errs = append(errs, fmt.Errorf("restore service unit: %w", err))
	}
	if err := b.writeFile(backup.BootstrapReceipt.Path, backup.BootstrapReceipt.Body, backup.BootstrapReceipt.Mode); err != nil {
		errs = append(errs, fmt.Errorf("restore bootstrap receipt: %w", err))
	}
	if err := b.writeFile(backup.Provenance.Path, backup.Provenance.Body, backup.Provenance.Mode); err != nil {
		errs = append(errs, fmt.Errorf("restore lifecycle provenance: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		errs = append(errs, fmt.Errorf("systemctl daemon-reload during rollback: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "enable", "--now", backup.Plan.ServiceName+".service"); err != nil {
		errs = append(errs, fmt.Errorf("systemctl enable --now %s.service during rollback: %w", backup.Plan.ServiceName, err))
	}
	if err := b.waitUntilReachable(ctx, backup.Plan); err != nil {
		errs = append(errs, fmt.Errorf("rollback readiness validation: %w", err))
	}
	return errors.Join(errs...)
}

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

	"regixtry/internal/infra/install/releases"
)

type UpgradeConfig struct {
	Ref            string
	ProvenancePath string
	AssumeYes      bool
}

type UpgradeResult struct {
	FromVersion    string
	ToVersion      string
	TargetRef      string
	ProvenancePath string
}

type releaseClient interface {
	Resolve(context.Context, string, string, string) (releases.ReleaseAsset, error)
	DownloadVerifiedBinary(context.Context, releases.ReleaseAsset, string) (string, error)
}

var newReleaseClient = func() releaseClient {
	return releases.NewGitHubClient(os.Getenv("REGISTRY_INSTALL_RELEASES_API_URL"))
}

type upgradeSnapshot struct {
	BinaryBody       []byte
	BinaryBackupPath string
	BinaryMode       os.FileMode
	EnvBody          []byte
	UnitBody         []byte
	BootstrapReceipt []byte
	ProvenanceBody   []byte
	ProvenanceMode   os.FileMode
	Plan             BootstrapPlan
	ProvenancePath   string
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
	plan := buildPlanFromInstalledIntent(intent)

	arch, err := releases.CurrentLinuxArch()
	if err != nil {
		return UpgradeResult{}, err
	}
	asset, err := newReleaseClient().Resolve(ctx, cfg.Ref, "linux", arch)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := requireUpgradeConfirmation(intent, asset, cfg.AssumeYes); err != nil {
		return UpgradeResult{}, err
	}

	stageDir, err := os.MkdirTemp("", "regixtry-upgrade-*")
	if err != nil {
		return UpgradeResult{}, err
	}
	defer os.RemoveAll(stageDir)

	stagedBinaryPath, err := newReleaseClient().DownloadVerifiedBinary(ctx, asset, stageDir)
	if err != nil {
		return UpgradeResult{}, err
	}
	if _, err := b.detector.Detect(); err != nil {
		return UpgradeResult{}, err
	}

	snapshot, err := b.captureUpgradeSnapshot(plan, provenancePath)
	if err != nil {
		return UpgradeResult{}, err
	}

	if err := b.runCommand(ctx, "systemctl", "disable", "--now", plan.ServiceName+".service"); err != nil {
		return UpgradeResult{}, fmt.Errorf("systemctl disable --now %s.service: %w", plan.ServiceName, err)
	}

	rollback := func(runErr error) error {
		if rollbackErr := b.restoreUpgradeSnapshot(ctx, snapshot); rollbackErr != nil {
			return errors.Join(runErr, rollbackErr)
		}
		return runErr
	}

	binaryBackupPath, err := b.swapInstalledBinary(plan.BinaryPath, stagedBinaryPath, snapshot.BinaryMode)
	if err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("replace installed binary: %w", err))
	}
	snapshot.BinaryBackupPath = binaryBackupPath
	if err := b.writeManagedArtifacts(plan, bootstrapReceiptFromPlan(plan), false); err != nil {
		return UpgradeResult{}, rollback(err)
	}
	if err := b.runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("systemctl daemon-reload: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "enable", "--now", plan.ServiceName+".service"); err != nil {
		return UpgradeResult{}, rollback(fmt.Errorf("systemctl enable --now %s.service: %w", plan.ServiceName, err))
	}
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
	if snapshot.BinaryBackupPath != "" {
		if err := b.removeAll(snapshot.BinaryBackupPath); err != nil {
			return UpgradeResult{}, rollback(fmt.Errorf("remove binary backup: %w", err))
		}
		snapshot.BinaryBackupPath = ""
	}

	return UpgradeResult{
		FromVersion:    intent.InstalledVersion,
		ToVersion:      asset.Version,
		TargetRef:      asset.Tag,
		ProvenancePath: provenance.StatePath,
	}, nil
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

func requireUpgradeConfirmation(intent InstalledIntent, asset releases.ReleaseAsset, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	if intent.InstalledVersion == "" {
		return nil
	}
	currentMajor := semanticMajor(intent.InstalledVersion)
	targetMajor := semanticMajor(asset.Version)
	if currentMajor == 0 || targetMajor == 0 || currentMajor == targetMajor {
		return nil
	}
	return fmt.Errorf("major-version upgrade from %s to %s requires --yes", intent.InstalledVersion, asset.Version)
}

func semanticMajor(version string) int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	majorPart, _, _ := strings.Cut(trimmed, ".")
	major, _ := strconv.Atoi(majorPart)
	return major
}

func (b *Bootstrapper) captureUpgradeSnapshot(plan BootstrapPlan, provenancePath string) (upgradeSnapshot, error) {
	binaryBody, err := b.readFile(plan.BinaryPath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("read installed binary: %w", err)
	}
	envBody, err := b.readFile(plan.EnvPath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("read env file: %w", err)
	}
	unitBody, err := b.readFile(plan.UnitPath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("read service unit: %w", err)
	}
	receiptBody, err := b.readFile(plan.StatePath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("read bootstrap receipt: %w", err)
	}
	provenanceBody, err := b.readFile(provenancePath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("read lifecycle provenance: %w", err)
	}
	info, err := b.stat(plan.BinaryPath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("stat installed binary: %w", err)
	}
	provenanceInfo, err := b.stat(provenancePath)
	if err != nil {
		return upgradeSnapshot{}, fmt.Errorf("stat lifecycle provenance: %w", err)
	}
	return upgradeSnapshot{
		BinaryBody:       binaryBody,
		BinaryMode:       info.Mode(),
		EnvBody:          envBody,
		UnitBody:         unitBody,
		BootstrapReceipt: receiptBody,
		ProvenanceBody:   provenanceBody,
		ProvenanceMode:   provenanceInfo.Mode(),
		Plan:             plan,
		ProvenancePath:   provenancePath,
	}, nil
}

func (b *Bootstrapper) restoreUpgradeSnapshot(ctx context.Context, snapshot upgradeSnapshot) error {
	var errs []error
	rename := b.rename
	if rename == nil {
		rename = os.Rename
	}
	removeAll := b.removeAll
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	if snapshot.BinaryBackupPath != "" {
		rollbackBinaryPath := snapshot.BinaryBackupPath + ".rollback"
		if err := rename(snapshot.Plan.BinaryPath, rollbackBinaryPath); err != nil {
			errs = append(errs, fmt.Errorf("move upgraded binary aside: %w", err))
		} else {
			if err := rename(snapshot.BinaryBackupPath, snapshot.Plan.BinaryPath); err != nil {
				errs = append(errs, fmt.Errorf("restore installed binary from backup: %w", err))
				if moveBackErr := rename(rollbackBinaryPath, snapshot.Plan.BinaryPath); moveBackErr != nil {
					errs = append(errs, fmt.Errorf("restore upgraded binary after failed rollback: %w", moveBackErr))
				}
			} else if err := removeAll(rollbackBinaryPath); err != nil {
				errs = append(errs, fmt.Errorf("cleanup upgraded binary after rollback: %w", err))
			}
		}
	} else if err := b.writeFile(snapshot.Plan.BinaryPath, snapshot.BinaryBody, snapshot.BinaryMode); err != nil {
		errs = append(errs, fmt.Errorf("restore installed binary: %w", err))
	}
	if err := b.writeFile(snapshot.Plan.EnvPath, snapshot.EnvBody, 0o644); err != nil {
		errs = append(errs, fmt.Errorf("restore env file: %w", err))
	}
	if err := b.writeFile(snapshot.Plan.UnitPath, snapshot.UnitBody, 0o644); err != nil {
		errs = append(errs, fmt.Errorf("restore service unit: %w", err))
	}
	if err := b.writeFile(snapshot.Plan.StatePath, snapshot.BootstrapReceipt, 0o644); err != nil {
		errs = append(errs, fmt.Errorf("restore bootstrap receipt: %w", err))
	}
	if err := b.writeFile(snapshot.ProvenancePath, snapshot.ProvenanceBody, snapshot.ProvenanceMode); err != nil {
		errs = append(errs, fmt.Errorf("restore lifecycle provenance: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		errs = append(errs, fmt.Errorf("systemctl daemon-reload during rollback: %w", err))
	}
	if err := b.runCommand(ctx, "systemctl", "enable", "--now", snapshot.Plan.ServiceName+".service"); err != nil {
		errs = append(errs, fmt.Errorf("systemctl enable --now %s.service during rollback: %w", snapshot.Plan.ServiceName, err))
	}
	if err := b.waitUntilReachable(ctx, snapshot.Plan); err != nil {
		errs = append(errs, fmt.Errorf("rollback readiness validation: %w", err))
	}
	return errors.Join(errs...)
}

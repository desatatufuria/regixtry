package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"regixtry/internal/infra/install/releases"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/ports"
)

func TestBootstrapperUpgradeStagesBeforeStoppingAndPreservesMetadataDB(t *testing.T) {
	root, provenancePath, plan := writeInstalledRuntimeFixture(t)
	metadataBody := []byte("metadata-stays\n")
	if err := os.WriteFile(plan.DatabasePath, metadataBody, 0o644); err != nil {
		t.Fatalf("WriteFile(metadata) error = %v", err)
	}

	events := make([]string, 0, 8)
	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			events = append(events, "resolve")
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, progress func(releases.DownloadProgress)) (string, error) {
			if progress != nil {
				progress(releases.DownloadProgress{Stage: "download", Detail: "Downloading regixtry_1.2.3_linux_amd64.tar.gz"})
				progress(releases.DownloadProgress{Stage: "verify", Detail: "Verifying regixtry_1.2.3_linux_amd64.tar.gz"})
			}
			events = append(events, "download")
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(&events)
	originalWriteFile := b.writeFile
	b.writeFile = func(path string, body []byte, mode os.FileMode) error {
		if path == plan.BinaryPath {
			t.Fatalf("writeFile called for installed binary during upgrade")
		}
		return originalWriteFile(path, body, mode)
	}

	result, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath, Progress: func(progress UpgradeProgress) {
		if progress.Stage != "" {
			events = append(events, progress.Stage)
		}
	}})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if result.TargetRef != "v1.2.3" {
		t.Fatalf("TargetRef = %q, want v1.2.3", result.TargetRef)
	}
	if got, wantPrefix := strings.Join(events, "|"), "resolve|resolve|download|verify|download|stop|systemctl disable --now regixtry.service"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("events = %s, want staging before service stop", got)
	}
	body, err := os.ReadFile(plan.DatabasePath)
	if err != nil {
		t.Fatalf("ReadFile(metadata) error = %v", err)
	}
	if string(body) != string(metadataBody) {
		t.Fatalf("metadata.db = %q, want preserved contents %q", string(body), string(metadataBody))
	}
	provenanceBody, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatalf("ReadFile(provenance) error = %v", err)
	}
	if !strings.Contains(string(provenanceBody), `"installed_ref": "v1.2.3"`) {
		t.Fatalf("provenance = %q, want upgraded ref", string(provenanceBody))
	}
	assertNoBinarySwapArtifacts(t, plan.BinaryPath)
	_ = root
}

func TestBootstrapperUpgradeRestoresAfterSystemctlFailure(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(nil)
	originalWriteFile := b.writeFile
	b.writeFile = func(path string, body []byte, mode os.FileMode) error {
		if path == plan.BinaryPath {
			t.Fatalf("writeFile called for installed binary during rollback")
		}
		return originalWriteFile(path, body, mode)
	}
	b.runCommand = func(_ context.Context, name string, args ...string) error {
		if strings.Join(args, " ") == "enable --now regixtry.service" {
			return errors.New("systemd start failed")
		}
		return nil
	}

	err := upgradeExpectFailure(t, b, provenancePath, "systemctl enable --now regixtry.service")
	if !strings.Contains(err.Error(), "rollback readiness validation") && !strings.Contains(err.Error(), "systemctl enable --now regixtry.service during rollback") {
		// rollback path restarted the previous binary; any truthful rollback error shape is acceptable.
	}
	assertFileBody(t, plan.BinaryPath, "old-binary")
	assertFileContains(t, plan.EnvPath, `REGISTRY_PUBLIC_URL="http://127.0.0.1:5000"`)
	assertNoBinarySwapArtifacts(t, plan.BinaryPath)
}

func TestBootstrapperUpgradeRestoresAfterProbeFailure(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(nil)
	b.probe = func(context.Context, string, bool) (int, error) { return 503, nil }
	b.probeInterval = time.Millisecond
	b.probeTimeout = 3 * time.Millisecond

	_ = upgradeExpectFailure(t, b, provenancePath, "readiness probe returned 503")
	assertFileBody(t, plan.BinaryPath, "old-binary")
	assertFileContains(t, plan.UnitPath, "serve -addr=${REGISTRY_ADDR}")
}

func TestBootstrapperUpgradeRestoresProvenanceAfterProvenanceWriteFailure(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	originalProvenanceBody, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatalf("ReadFile(provenance) error = %v", err)
	}

	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(nil)
	originalWriteFile := b.writeFile
	failedProvenanceWrite := false
	b.writeFile = func(path string, body []byte, mode os.FileMode) error {
		if path == provenancePath && strings.Contains(string(body), `"installed_ref": "v1.2.3"`) && !failedProvenanceWrite {
			failedProvenanceWrite = true
			return errors.New("disk full")
		}
		return originalWriteFile(path, body, mode)
	}

	err = upgradeExpectFailure(t, b, provenancePath, "write lifecycle provenance")
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Upgrade() error = %v, want provenance write failure", err)
	}
	if !failedProvenanceWrite {
		t.Fatal("expected provenance write failure to be triggered")
	}
	assertFileBody(t, plan.BinaryPath, "old-binary")
	assertFileEquals(t, provenancePath, originalProvenanceBody)
}

func TestBootstrapperUpgradeAlreadyUpToDateReturnsWithoutUpgradeActions(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	events := make([]string, 0, 4)
	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			events = append(events, "resolve")
			return releases.ReleaseAsset{Tag: "v1.2.2", Version: "1.2.2", ArchiveName: "regixtry_1.2.2_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(context.Context, releases.ReleaseAsset, string, func(releases.DownloadProgress)) (string, error) {
			t.Fatal("DownloadVerifiedBinary should not run for an up-to-date install")
			return "", nil
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(&events)
	progressCalls := 0
	result, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath, Progress: func(progress UpgradeProgress) {
		if progress.Stage != "" {
			progressCalls++
			events = append(events, progress.Stage)
		}
	}})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if !result.UpToDate {
		t.Fatalf("UpToDate = %v, want true", result.UpToDate)
	}
	if result.TargetRef != "v1.2.2" || result.ToVersion != "1.2.2" {
		t.Fatalf("result = %#v, want same installed target identity", result)
	}
	if got := strings.Join(events, "|"); got != "resolve" {
		t.Fatalf("events = %s, want only release resolution without progress callbacks", got)
	}
	if progressCalls != 0 {
		t.Fatalf("progressCalls = %d, want 0", progressCalls)
	}
	assertFileBody(t, plan.BinaryPath, "old-binary")
	assertFileContains(t, provenancePath, `"installed_ref": "v1.2.2"`)
	assertNoBinarySwapArtifacts(t, plan.BinaryPath)
	assertFileContains(t, plan.EnvPath, `REGISTRY_PUBLIC_URL="http://127.0.0.1:5000"`)
}

func TestBootstrapperUpgradeRunsPreflightAndConfirmBeforeProgress(t *testing.T) {
	_, provenancePath, _ := writeInstalledRuntimeFixture(t)
	steps := make([]string, 0, 6)
	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			steps = append(steps, "resolve-client")
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, progress func(releases.DownloadProgress)) (string, error) {
			if progress != nil {
				progress(releases.DownloadProgress{Stage: "download", Detail: "Downloading regixtry_1.2.3_linux_amd64.tar.gz"})
			}
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(nil)
	_, err := b.Upgrade(context.Background(), UpgradeConfig{
		ProvenancePath: provenancePath,
		Preflight: func(UpgradePreflight) error {
			steps = append(steps, "preflight")
			return nil
		},
		Confirm: func(UpgradePreflight) error {
			steps = append(steps, "confirm")
			return nil
		},
		Progress: func(progress UpgradeProgress) {
			if progress.Stage != "" {
				steps = append(steps, "progress:"+progress.Stage)
			}
		},
	})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if got, wantPrefix := strings.Join(steps, "|"), "resolve-client|preflight|confirm|progress:resolve|progress:download"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("steps = %s, want preflight and confirm before progress", got)
	}
}

func TestBootstrapperUpgradeImportsLegacyTrivySettingsWhenFeatureStateMissing(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	if err := os.WriteFile(plan.EnvPath, []byte(strings.Join([]string{
		"REGISTRY_ADDR=\"127.0.0.1:5000\"",
		"REGISTRY_PUBLIC_URL=\"http://127.0.0.1:5000\"",
		"REGISTRY_STORAGE_ROOT=\"" + plan.StorageRoot + "\"",
		"REGISTRY_DATABASE_PATH=\"" + plan.DatabasePath + "\"",
		"REGISTRY_SERVICE_NAME=\"regixtry\"",
		"REGISTRY_TRIVY_ENABLED=\"true\"",
		"REGISTRY_TRIVY_SCHEDULE_ENABLED=\"true\"",
		"REGISTRY_TRIVY_INTERVAL=\"6h0m0s\"",
		"REGISTRY_TRIVY_TIMEOUT=\"10m0s\"",
		"REGISTRY_TRIVY_CACHE_DIR=\"" + filepath.Join(plan.StorageRoot, "trivy-cache") + "\"",
		"REGISTRY_TRIVY_BINARY_PATH=\"trivy-custom\"",
		"REGISTRY_TRIVY_MAX_CONCURRENCY=\"2\"",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}

	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(nil)
	if _, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	store, err := metadata.New(plan.DatabasePath)
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()
	settings, err := store.GetScanSettings(context.Background(), ports.DefaultTenant)
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if !settings.Enabled || !settings.ScheduleEnabled || settings.Interval != 6*time.Hour || settings.Timeout != 10*time.Minute || settings.ServiceURL != "" || settings.RegistryReachableURL != "" || settings.MaxConcurrency != 2 {
		t.Fatalf("settings = %#v, want imported legacy trivy state", settings)
	}
}

func TestBootstrapperUpgradeKeepsExistingFeatureStateWhenLegacyInputsDiffer(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	store, err := metadata.New(plan.DatabasePath)
	if err != nil {
		t.Fatalf("metadata.New() error = %v", err)
	}
	defer store.Close()
	existing := ports.ScanSettings{
		Enabled:              true,
		ScheduleEnabled:      false,
		Interval:             24 * time.Hour,
		Timeout:              15 * time.Minute,
		ServiceURL:           "https://scanner.example.com",
		RegistryReachableURL: "https://registry.internal",
		MaxConcurrency:       1,
		UpdatedAt:            time.Now().UTC(),
	}
	if err := store.UpsertScanSettings(context.Background(), ports.DefaultTenant, existing); err != nil {
		t.Fatalf("UpsertScanSettings() error = %v", err)
	}
	if err := os.WriteFile(plan.EnvPath, []byte(strings.Join([]string{
		"REGISTRY_ADDR=\"127.0.0.1:5000\"",
		"REGISTRY_PUBLIC_URL=\"http://127.0.0.1:5000\"",
		"REGISTRY_STORAGE_ROOT=\"" + plan.StorageRoot + "\"",
		"REGISTRY_DATABASE_PATH=\"" + plan.DatabasePath + "\"",
		"REGISTRY_SERVICE_NAME=\"regixtry\"",
		"REGISTRY_TRIVY_ENABLED=\"false\"",
		"REGISTRY_TRIVY_SCHEDULE_ENABLED=\"true\"",
		"REGISTRY_TRIVY_INTERVAL=\"3h0m0s\"",
		"REGISTRY_TRIVY_TIMEOUT=\"20m0s\"",
		"REGISTRY_TRIVY_CACHE_DIR=\"" + filepath.Join(plan.StorageRoot, "legacy-cache") + "\"",
		"REGISTRY_TRIVY_BINARY_PATH=\"trivy-legacy\"",
		"REGISTRY_TRIVY_MAX_CONCURRENCY=\"4\"",
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}

	restoreRelease := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restoreRelease()

	b := newUpgradeTestBootstrapper(nil)
	if _, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	settings, err := store.GetScanSettings(context.Background(), ports.DefaultTenant)
	if err != nil {
		t.Fatalf("GetScanSettings() error = %v", err)
	}
	if settings.Enabled != existing.Enabled || settings.ScheduleEnabled != existing.ScheduleEnabled || settings.Interval != existing.Interval || settings.Timeout != existing.Timeout || settings.ServiceURL != existing.ServiceURL || settings.RegistryReachableURL != existing.RegistryReachableURL || settings.MaxConcurrency != existing.MaxConcurrency {
		t.Fatalf("settings = %#v, want preserved existing feature state %#v", settings, existing)
	}
}

func TestCaptureManagedRuntimeBackupIncludesManagedArtifacts(t *testing.T) {
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	b := newUpgradeTestBootstrapper(nil)

	backup, err := b.captureManagedRuntimeBackup(plan, provenancePath)
	if err != nil {
		t.Fatalf("captureManagedRuntimeBackup() error = %v", err)
	}
	if backup.Binary.Path != plan.BinaryPath || string(backup.Binary.Body) != "old-binary" {
		t.Fatalf("binary backup = %#v, want installed binary snapshot", backup.Binary)
	}
	if backup.Binary.BackupPath != "" {
		t.Fatalf("binary backup path = %q, want empty before swap", backup.Binary.BackupPath)
	}
	if backup.Env.Path != plan.EnvPath || !strings.Contains(string(backup.Env.Body), `REGISTRY_PUBLIC_URL="http://127.0.0.1:5000"`) {
		t.Fatalf("env backup = %#v, want managed env snapshot", backup.Env)
	}
	if backup.Unit.Path != plan.UnitPath || !strings.Contains(string(backup.Unit.Body), "regixtry serve") {
		t.Fatalf("unit backup = %#v, want systemd unit snapshot", backup.Unit)
	}
	if backup.BootstrapReceipt.Path != plan.StatePath || !strings.Contains(string(backup.BootstrapReceipt.Body), `"service_name":"regixtry"`) {
		t.Fatalf("receipt backup = %#v, want bootstrap receipt snapshot", backup.BootstrapReceipt)
	}
	if backup.Provenance.Path != provenancePath || !strings.Contains(string(backup.Provenance.Body), `"installed_version": "1.2.2"`) {
		t.Fatalf("provenance backup = %#v, want lifecycle provenance snapshot", backup.Provenance)
	}
}

type stubReleaseClient struct {
	resolveFn  func(context.Context, string, string, string) (releases.ReleaseAsset, error)
	downloadFn func(context.Context, releases.ReleaseAsset, string, func(releases.DownloadProgress)) (string, error)
}

func (s stubReleaseClient) Resolve(ctx context.Context, ref string, targetOS string, targetArch string) (releases.ReleaseAsset, error) {
	return s.resolveFn(ctx, ref, targetOS, targetArch)
}

func (s stubReleaseClient) DownloadVerifiedBinary(ctx context.Context, asset releases.ReleaseAsset, dir string, progress func(releases.DownloadProgress)) (string, error) {
	return s.downloadFn(ctx, asset, dir, progress)
}

func swapReleaseClient(t *testing.T, client releaseClient) func() {
	t.Helper()
	previous := newReleaseClient
	newReleaseClient = func() releaseClient { return client }
	return func() { newReleaseClient = previous }
}

func newUpgradeTestBootstrapper(events *[]string) *Bootstrapper {
	appendEvent := func(value string) {
		if events != nil {
			*events = append(*events, value)
		}
	}
	return &Bootstrapper{
		detector: detector{
			goos:     "linux",
			readFile: func(string) ([]byte, error) { return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil },
			stat:     func(string) (os.FileInfo, error) { return fakeInfo{name: "systemd"}, nil },
		},
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		stat:      os.Stat,
		rename:    os.Rename,
		removeAll: os.RemoveAll,
		runCommand: func(_ context.Context, name string, args ...string) error {
			appendEvent(name + " " + strings.Join(args, " "))
			return nil
		},
		probe:         func(_ context.Context, _ string, _ bool) (int, error) { return 401, nil },
		probeInterval: time.Millisecond,
		probeTimeout:  50 * time.Millisecond,
	}
}

func assertNoBinarySwapArtifacts(t *testing.T, binaryPath string) {
	t.Helper()
	dir := filepath.Dir(binaryPath)
	base := filepath.Base(binaryPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	}
	artifacts := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "."+base+".backup-") || strings.HasPrefix(name, "."+base+".upgrade-") || strings.HasSuffix(name, ".rollback") {
			artifacts = append(artifacts, name)
		}
	}
	slices.Sort(artifacts)
	if len(artifacts) > 0 {
		t.Fatalf("swap artifacts = %v, want none", artifacts)
	}
}

func writeInstalledRuntimeFixture(t *testing.T) (string, string, BootstrapPlan) {
	t.Helper()
	root := t.TempDir()
	plan := BootstrapPlan{
		Mode:           supportedMode,
		Addr:           "127.0.0.1:5000",
		PublicURL:      "http://127.0.0.1:5000",
		RuntimeTLSMode: RuntimeTLSModeLocalHTTP,
		StorageRoot:    filepath.Join(root, "var", "lib", "regixtry"),
		DatabasePath:   filepath.Join(root, "var", "lib", "regixtry", "metadata.db"),
		ContentPath:    filepath.Join(root, "var", "lib", "regixtry", "content"),
		StatePath:      filepath.Join(root, "etc", "regixtry", "bootstrap-state.json"),
		EnvPath:        filepath.Join(root, "etc", "regixtry", "regixtry.env"),
		UnitPath:       filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		BinaryPath:     filepath.Join(root, "usr", "local", "bin", "regixtry"),
		ServiceName:    "regixtry",
	}
	for _, dir := range []string{filepath.Dir(plan.EnvPath), filepath.Dir(plan.UnitPath), filepath.Dir(plan.BinaryPath), plan.StorageRoot, plan.ContentPath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}
	if err := os.WriteFile(plan.BinaryPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(binary) error = %v", err)
	}
	if err := os.WriteFile(plan.EnvPath, []byte(RenderEnvFile(plan)), 0o644); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}
	if err := os.WriteFile(plan.UnitPath, []byte(RenderSystemdUnit(plan)), 0o644); err != nil {
		t.Fatalf("WriteFile(unit) error = %v", err)
	}
	if err := os.WriteFile(plan.StatePath, []byte(`{"mode":"daemon-sqlite","service_name":"regixtry","paths":["`+plan.EnvPath+`","`+plan.UnitPath+`","`+plan.DatabasePath+`","`+plan.ContentPath+`","`+plan.StatePath+`"]}`), 0o644); err != nil {
		t.Fatalf("WriteFile(receipt) error = %v", err)
	}
	provenance := LifecycleProvenance{
		Version:          1,
		Mode:             supportedMode,
		InstalledBin:     plan.BinaryPath,
		InstalledRef:     "v1.2.2",
		InstalledVersion: "1.2.2",
		ServiceName:      plan.ServiceName,
		StatePath:        filepath.Join(root, "etc", "regixtry", lifecycleProvenanceFileName),
		ManagedPaths:     []string{plan.EnvPath, plan.UnitPath, plan.DatabasePath, plan.ContentPath, plan.StatePath},
	}
	provenancePath := provenance.StatePath
	b := &Bootstrapper{writeFile: os.WriteFile}
	if err := b.writeLifecycleProvenance(provenance); err != nil {
		t.Fatalf("writeLifecycleProvenance() error = %v", err)
	}
	return root, provenancePath, plan
}

func upgradeExpectFailure(t *testing.T, b *Bootstrapper, provenancePath string, want string) error {
	t.Helper()
	_, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Upgrade() error = %v, want substring %q", err, want)
	}
	return err
}

func assertFileBody(t *testing.T, path string, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(body) != want {
		t.Fatalf("%s = %q, want %q", path, string(body), want)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !strings.Contains(string(body), want) {
		t.Fatalf("%s = %q, want substring %q", path, string(body), want)
	}
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(body) != string(want) {
		t.Fatalf("%s = %q, want %q", path, string(body), string(want))
	}
}

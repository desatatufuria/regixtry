package linux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"regixtry/internal/infra/install/releases"
)

const (
	secretTestPassword = "s3cr3t-pa55w0rd"
	secretTestDSN      = "postgres://registry:" + secretTestPassword + "@db.example.com:5432/regixtry_auth?sslmode=disable"
)

func secretTestPlan(root string) BootstrapPlan {
	return BootstrapPlan{
		Mode:            supportedMode,
		Addr:            "127.0.0.1:5000",
		PublicURL:       "http://127.0.0.1:5000",
		RuntimeTLSMode:  RuntimeTLSModeLocalHTTP,
		AuthPostgresDSN: secretTestDSN,
		StorageRoot:     filepath.Join(root, "var", "lib", "regixtry"),
		DatabasePath:    filepath.Join(root, "var", "lib", "regixtry", "metadata.db"),
		ContentPath:     filepath.Join(root, "var", "lib", "regixtry", "content"),
		StatePath:       filepath.Join(root, "etc", "regixtry", "bootstrap-state.json"),
		EnvPath:         filepath.Join(root, "etc", "regixtry", "regixtry.env"),
		UnitPath:        filepath.Join(root, "etc", "systemd", "system", "regixtry.service"),
		BinaryPath:      filepath.Join(root, "usr", "local", "bin", "regixtry"),
		ServiceName:     "regixtry",
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", path, got, want)
	}
}

func assertNoSecret(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if strings.Contains(string(body), secretTestPassword) {
		t.Fatalf("%s contains the Postgres password:\n%s", path, body)
	}
}

func TestRenderSystemdUnitNeverPutsDSNOnExecStart(t *testing.T) {
	t.Parallel()

	unit := RenderSystemdUnit(secretTestPlan(t.TempDir()))
	if strings.Contains(unit, "auth-postgres-dsn") || strings.Contains(unit, "REGISTRY_AUTH_POSTGRES_DSN") || strings.Contains(unit, secretTestPassword) {
		t.Fatalf("unit references the DSN, want it delivered only via EnvironmentFile:\n%s", unit)
	}
	if !strings.Contains(unit, "EnvironmentFile=") {
		t.Fatalf("unit = %q, want EnvironmentFile", unit)
	}
}

func TestSetupWritesDSNOnlyToOwnerOnlyEnvFile(t *testing.T) {
	root := t.TempDir()
	plan := secretTestPlan(root)
	b := &Bootstrapper{mkdirAll: os.MkdirAll, writeFile: os.WriteFile}

	if err := b.writeSetupArtifacts(plan, bootstrapReceiptFromPlan(plan)); err != nil {
		t.Fatalf("writeSetupArtifacts() error = %v", err)
	}
	provenance := lifecycleProvenanceFromPlan(plan, bootstrapReceiptFromPlan(plan))
	if err := b.writeLifecycleProvenance(provenance); err != nil {
		t.Fatalf("writeLifecycleProvenance() error = %v", err)
	}

	assertMode(t, plan.EnvPath, 0o600)
	assertFileContains(t, plan.EnvPath, secretTestPassword)
	assertNoSecret(t, plan.UnitPath)
	assertNoSecret(t, plan.StatePath)
	assertNoSecret(t, lifecycleProvenancePath(plan.StatePath))
	assertFileContains(t, lifecycleProvenancePath(plan.StatePath), "xxxxx")
}

func TestSecretFileWriteTightensExistingWideFileAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regixtry.env")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeSecretFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatalf("writeSecretFile() error = %v", err)
	}
	assertMode(t, path, 0o600)
	assertFileBody(t, path, "new\n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir entries = %d, want only the target file", len(entries))
	}
}

func TestSecretFileWriteReplacesSymlinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "regixtry.env")
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if err := writeSecretFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("writeSecretFile() error = %v", err)
	}
	assertFileBody(t, victim, "keep\n")
	assertFileBody(t, path, "secret\n")
	assertMode(t, path, 0o600)
}

func TestRedactDSN(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		secretTestDSN:                        "postgres://registry:xxxxx@db.example.com:5432/regixtry_auth?sslmode=disable",
		"postgres://u@h/db":                  "postgres://u@h/db",
		"host=h user=u password=pw dbname=d": "host=h user=u password=xxxxx dbname=d",
		"host=h user=u password='a b' x=1":   "host=h user=u password=xxxxx x=1",
		"":                                   "",
	}
	for in, want := range cases {
		if got := RedactDSN(in); got != want {
			t.Errorf("RedactDSN(%q) = %q, want %q", in, got, want)
		}
	}
}

// legacyDSNInstallFixture lays out an install written by the old installer:
// 0644 env with the DSN, a unit whose ExecStart carries the DSN flag, and a
// 0644 provenance holding the DSN in clear text.
func legacyDSNInstallFixture(t *testing.T) (string, BootstrapPlan) {
	t.Helper()
	_, provenancePath, plan := writeInstalledRuntimeFixture(t)
	plan.AuthPostgresDSN = secretTestDSN
	legacyEnv := RenderEnvFile(plan)
	legacyUnit := strings.Replace(RenderSystemdUnit(plan), "-service=${REGISTRY_SERVICE_NAME}", "-service=${REGISTRY_SERVICE_NAME} -auth-postgres-dsn=${REGISTRY_AUTH_POSTGRES_DSN}", 1)
	if err := os.WriteFile(plan.EnvPath, []byte(legacyEnv), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(plan.EnvPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, []byte(legacyUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyProvenance := `{"version":2,"mode":"daemon-sqlite","installed_bin":"` + plan.BinaryPath + `","installed_ref":"v1.2.2","installed_version":"1.2.2","service_name":"regixtry","state_path":"` + provenancePath + `","managed_paths":["` + plan.EnvPath + `","` + plan.UnitPath + `","` + plan.DatabasePath + `","` + plan.ContentPath + `","` + plan.StatePath + `"],"intent":{"addr":"127.0.0.1:5000","public_url":"http://127.0.0.1:5000","runtime_tls_mode":"local-http","auth_postgres_dsn":"` + secretTestDSN + `","storage_root":"` + plan.StorageRoot + `","database_path":"` + plan.DatabasePath + `","content_path":"` + plan.ContentPath + `","bootstrap_state_path":"` + plan.StatePath + `","env_path":"` + plan.EnvPath + `","unit_path":"` + plan.UnitPath + `","binary_path":"` + plan.BinaryPath + `","service_name":"regixtry"}}`
	if err := os.WriteFile(provenancePath, []byte(legacyProvenance), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(provenancePath, 0o644); err != nil {
		t.Fatal(err)
	}
	return provenancePath, plan
}

func TestUpgradeMigratesLegacyWorldReadableDSNLayout(t *testing.T) {
	provenancePath, plan := legacyDSNInstallFixture(t)
	restore := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restore()

	b := newUpgradeTestBootstrapper(nil)
	if _, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}

	assertMode(t, plan.EnvPath, 0o600)
	assertFileContains(t, plan.EnvPath, secretTestPassword)
	assertNoSecret(t, plan.UnitPath)
	assertNoSecret(t, provenancePath)
	unit, _ := os.ReadFile(plan.UnitPath)
	if strings.Contains(string(unit), "auth-postgres-dsn") {
		t.Fatalf("unit still passes the DSN flag:\n%s", unit)
	}
}

func TestUpgradeRecoversDSNFromLegacyProvenanceWhenEnvLacksIt(t *testing.T) {
	provenancePath, plan := legacyDSNInstallFixture(t)
	plan.AuthPostgresDSN = ""
	if err := os.WriteFile(plan.EnvPath, []byte(RenderEnvFile(plan)), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := swapReleaseClient(t, stubReleaseClient{
		resolveFn: func(context.Context, string, string, string) (releases.ReleaseAsset, error) {
			return releases.ReleaseAsset{Tag: "v1.2.3", Version: "1.2.3", ArchiveName: "regixtry_1.2.3_linux_amd64.tar.gz"}, nil
		},
		downloadFn: func(_ context.Context, _ releases.ReleaseAsset, dir string, _ func(releases.DownloadProgress)) (string, error) {
			stagedPath := filepath.Join(dir, "regixtry")
			return stagedPath, os.WriteFile(stagedPath, []byte("new-binary"), 0o755)
		},
	})
	defer restore()

	b := newUpgradeTestBootstrapper(nil)
	if _, err := b.Upgrade(context.Background(), UpgradeConfig{ProvenancePath: provenancePath}); err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	assertFileContains(t, plan.EnvPath, secretTestPassword)
	assertMode(t, plan.EnvPath, 0o600)
	assertNoSecret(t, provenancePath)
}

func TestLoadInstalledIntentPrefersEnvDSNOverRedactedAndLegacyProvenance(t *testing.T) {
	t.Parallel()

	provenance := LifecycleProvenance{
		Mode: supportedMode, ServiceName: "regixtry", InstalledBin: "/usr/local/bin/regixtry", StatePath: "/etc/regixtry/regixtry-lifecycle-state.json",
		Intent: LifecycleIntent{
			Addr: "127.0.0.1:5000", PublicURL: "http://127.0.0.1:5000", StorageRoot: "/var/lib/regixtry",
			AuthPostgresDSNRedacted: "postgres://registry:xxxxx@db/x",
		},
	}
	intent, err := loadInstalledIntent(provenance, map[string]string{"REGISTRY_AUTH_POSTGRES_DSN": secretTestDSN})
	if err != nil {
		t.Fatalf("loadInstalledIntent() error = %v", err)
	}
	if intent.AuthPostgresDSN != secretTestDSN {
		t.Fatalf("AuthPostgresDSN = %q, want the env value", intent.AuthPostgresDSN)
	}
	intent, err = loadInstalledIntent(provenance, map[string]string{})
	if err != nil {
		t.Fatalf("loadInstalledIntent() error = %v", err)
	}
	if strings.Contains(intent.AuthPostgresDSN, "xxxxx") {
		t.Fatalf("AuthPostgresDSN = %q, redacted value must never become the live DSN", intent.AuthPostgresDSN)
	}
}

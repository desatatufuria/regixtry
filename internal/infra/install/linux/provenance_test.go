package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadLifecycleProvenanceAcceptsV1AndV2(t *testing.T) {
	t.Parallel()

	b := &Bootstrapper{readFile: func(path string) ([]byte, error) {
		switch filepath.Base(path) {
		case "v1.json":
			return []byte(`{"version":1,"mode":"daemon-sqlite","installed_bin":"/usr/local/bin/regixtry","service_name":"regixtry","state_path":"/etc/regixtry/regixtry-lifecycle-state.json","managed_paths":["/etc/regixtry/regixtry.env"]}`), nil
		case "v2.json":
			return []byte(`{"version":2,"mode":"daemon-sqlite","installed_bin":"/usr/local/bin/regixtry","installed_ref":"v1.2.3","installed_version":"1.2.3","service_name":"regixtry","state_path":"/etc/regixtry/regixtry-lifecycle-state.json","managed_paths":["/etc/regixtry/regixtry.env"],"intent":{"public_url":"http://127.0.0.1:5000"}}`), nil
		default:
			return nil, os.ErrNotExist
		}
	}}

	v1, err := b.readLifecycleProvenance("/tmp/v1.json")
	if err != nil {
		t.Fatalf("readLifecycleProvenance(v1) error = %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("v1.Version = %d, want 1", v1.Version)
	}

	v2, err := b.readLifecycleProvenance("/tmp/v2.json")
	if err != nil {
		t.Fatalf("readLifecycleProvenance(v2) error = %v", err)
	}
	if v2.Version != 2 || v2.InstalledRef != "v1.2.3" || v2.Intent.PublicURL != "http://127.0.0.1:5000" {
		t.Fatalf("v2 = %#v, want preserved v2 provenance", v2)
	}
}

func TestLoadInstalledIntentRecoversFromManagedEnv(t *testing.T) {
	t.Parallel()

	provenance := LifecycleProvenance{
		Version:      1,
		Mode:         supportedMode,
		InstalledBin: "/usr/local/bin/regixtry",
		ServiceName:  "regixtry",
		StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		ManagedPaths: []string{"/etc/regixtry/regixtry.env", "/etc/systemd/system/regixtry.service", "/var/lib/regixtry/metadata.db", "/var/lib/regixtry/content", "/etc/regixtry/bootstrap-state.json"},
	}
	envValues := map[string]string{
		"REGISTRY_ADDR":              "127.0.0.1:5000",
		"REGISTRY_PUBLIC_URL":        "http://127.0.0.1:5000",
		"REGISTRY_STORAGE_ROOT":      "/var/lib/regixtry",
		"REGISTRY_DATABASE_PATH":     "/var/lib/regixtry/metadata.db",
		"REGISTRY_SERVICE_NAME":      "regixtry",
		"REGISTRY_AUTH_POSTGRES_DSN": "postgres://regixtry:secret@127.0.0.1:5432/regixtry_auth?sslmode=disable",
	}

	intent, err := loadInstalledIntent(provenance, envValues)
	if err != nil {
		t.Fatalf("loadInstalledIntent() error = %v", err)
	}
	want := InstalledIntent{
		Mode:                supportedMode,
		Addr:                "127.0.0.1:5000",
		PublicURL:           "http://127.0.0.1:5000",
		RuntimeTLSMode:      RuntimeTLSModeLocalHTTP,
		AuthPostgresDSN:     envValues["REGISTRY_AUTH_POSTGRES_DSN"],
		StorageRoot:         "/var/lib/regixtry",
		DatabasePath:        "/var/lib/regixtry/metadata.db",
		ContentPath:         "/var/lib/regixtry/content",
		BootstrapStatePath:  "/etc/regixtry/bootstrap-state.json",
		EnvPath:             "/etc/regixtry/regixtry.env",
		UnitPath:            "/etc/systemd/system/regixtry.service",
		BinaryPath:          "/usr/local/bin/regixtry",
		ServiceName:         "regixtry",
		TrivyCacheDir:       "/var/lib/regixtry/trivy-cache",
		TrivyBinaryPath:     "trivy",
		TrivyTimeout:        15 * time.Minute,
		TrivyInterval:       24 * time.Hour,
		TrivyMaxConcurrency: 1,
	}
	if !reflect.DeepEqual(intent, want) {
		t.Fatalf("intent = %#v, want %#v", intent, want)
	}
}

func TestLoadInstalledIntentRecoversTrivyManagedSettings(t *testing.T) {
	t.Parallel()

	provenance := LifecycleProvenance{
		Version:      lifecycleProvenanceVersion,
		Mode:         supportedMode,
		InstalledBin: "/usr/local/bin/regixtry",
		ServiceName:  "regixtry",
		StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		ManagedPaths: []string{"/etc/regixtry/regixtry.env"},
		Intent: LifecycleIntent{
			TrivyCacheDir:        "/var/lib/regixtry/trivy-cache",
			TrivyBinaryPath:      "trivy-custom",
			TrivyEnabled:         true,
			TrivyScheduleEnabled: true,
			TrivyTimeout:         "10m0s",
			TrivyInterval:        "6h0m0s",
			TrivyMaxConcurrency:  2,
		},
	}

	intent, err := loadInstalledIntent(provenance, map[string]string{
		"REGISTRY_ADDR":                   "127.0.0.1:5000",
		"REGISTRY_PUBLIC_URL":             "http://127.0.0.1:5000",
		"REGISTRY_STORAGE_ROOT":           "/var/lib/regixtry",
		"REGISTRY_DATABASE_PATH":          "/var/lib/regixtry/metadata.db",
		"REGISTRY_SERVICE_NAME":           "regixtry",
		"REGISTRY_TRIVY_CACHE_DIR":        "/var/lib/regixtry/trivy-cache",
		"REGISTRY_TRIVY_BINARY_PATH":      "trivy-custom",
		"REGISTRY_TRIVY_ENABLED":          "true",
		"REGISTRY_TRIVY_SCHEDULE_ENABLED": "true",
		"REGISTRY_TRIVY_TIMEOUT":          "10m0s",
		"REGISTRY_TRIVY_INTERVAL":         "6h0m0s",
		"REGISTRY_TRIVY_MAX_CONCURRENCY":  "2",
	})
	if err != nil {
		t.Fatalf("loadInstalledIntent() error = %v", err)
	}
	if !intent.TrivyEnabled || !intent.TrivyScheduleEnabled {
		t.Fatalf("intent = %#v, want trivy flags restored", intent)
	}
	if intent.TrivyCacheDir != "/var/lib/regixtry/trivy-cache" || intent.TrivyBinaryPath != "trivy-custom" {
		t.Fatalf("intent = %#v, want trivy cache/binary restored", intent)
	}
	if intent.TrivyTimeout != 10*time.Minute || intent.TrivyInterval != 6*time.Hour || intent.TrivyMaxConcurrency != 2 {
		t.Fatalf("intent = %#v, want trivy duration/concurrency restored", intent)
	}
}

func TestLoadInstalledIntentReportsMissingLifecycleCriticalValues(t *testing.T) {
	t.Parallel()

	_, err := loadInstalledIntent(LifecycleProvenance{
		Version:      1,
		Mode:         supportedMode,
		InstalledBin: "/usr/local/bin/regixtry",
		ServiceName:  "regixtry",
		StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		ManagedPaths: []string{"/etc/regixtry/regixtry.env"},
	}, map[string]string{"REGISTRY_STORAGE_ROOT": "/var/lib/regixtry"})
	if err == nil {
		t.Fatal("loadInstalledIntent() error = nil, want missing intent guidance")
	}
	var missingErr MissingIntentError
	if !errors.As(err, &missingErr) {
		t.Fatalf("loadInstalledIntent() error = %v, want MissingIntentError", err)
	}
	if !strings.Contains(err.Error(), "public_url") || !strings.Contains(err.Error(), "addr") {
		t.Fatalf("loadInstalledIntent() error = %v, want named missing fields", err)
	}
}

func TestBootstrapperUninstallFailsWhenProvenanceIsMissing(t *testing.T) {
	t.Parallel()

	b := &Bootstrapper{readFile: os.ReadFile}
	_, err := b.Uninstall(context.Background(), filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "read lifecycle provenance") {
		t.Fatalf("Uninstall() error = %v, want missing provenance error", err)
	}
}

func TestBootstrapperUninstallReportsDriftTruthfully(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bootstrapStatePath := filepath.Join(root, "etc", "regixtry", "bootstrap-state.json")
	lifecycleStatePath := filepath.Join(root, "etc", "regixtry", lifecycleProvenanceFileName)
	managedExisting := filepath.Join(root, "var", "lib", "regixtry", "content")
	managedMissing := filepath.Join(root, "var", "lib", "regixtry", "metadata.db")
	installedBin := filepath.Join(root, "usr", "local", "bin", "regixtry")

	for _, path := range []string{filepath.Dir(lifecycleStatePath), filepath.Dir(installedBin)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
	}
	if err := os.MkdirAll(managedExisting, 0o755); err != nil {
		t.Fatalf("MkdirAll(managedExisting) error = %v", err)
	}
	if err := os.WriteFile(installedBin, []byte("binary\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(installedBin) error = %v", err)
	}

	provenance := LifecycleProvenance{
		Version:      lifecycleProvenanceVersion,
		Mode:         supportedMode,
		InstalledBin: installedBin,
		ServiceName:  "regixtry",
		StatePath:    lifecycleStatePath,
		ManagedPaths: []string{managedExisting, managedMissing, bootstrapStatePath},
	}
	b := &Bootstrapper{
		writeFile: os.WriteFile,
		readFile:  os.ReadFile,
		stat:      os.Stat,
		removeAll: os.RemoveAll,
		runCommand: func(_ context.Context, _ string, _ ...string) error {
			return nil
		},
	}
	if err := b.writeLifecycleProvenance(provenance); err != nil {
		t.Fatalf("writeLifecycleProvenance() error = %v", err)
	}

	report, err := b.Uninstall(context.Background(), lifecycleStatePath)
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if report.Service.Status != CleanupStatusRemoved {
		t.Fatalf("service status = %q, want %q", report.Service.Status, CleanupStatusRemoved)
	}
	if len(report.Items) != 5 {
		t.Fatalf("items = %#v, want 5 cleanup items", report.Items)
	}

	statuses := map[string]string{}
	for _, item := range report.Items {
		statuses[item.Path] = item.Status
	}
	if statuses[managedExisting] != CleanupStatusRemoved {
		t.Fatalf("managedExisting status = %q, want removed", statuses[managedExisting])
	}
	if statuses[managedMissing] != CleanupStatusMissing {
		t.Fatalf("managedMissing status = %q, want missing", statuses[managedMissing])
	}
	if statuses[lifecycleStatePath] != CleanupStatusRemoved {
		t.Fatalf("lifecycle state status = %q, want removed", statuses[lifecycleStatePath])
	}
	if statuses[installedBin] != CleanupStatusRemoved {
		t.Fatalf("installedBin status = %q, want removed", statuses[installedBin])
	}
}

func TestUninstallCleanupTargetsRemovesInstalledBinaryLast(t *testing.T) {
	t.Parallel()

	installedBin := "/usr/local/bin/regixtry"
	got := uninstallCleanupTargets(LifecycleProvenance{
		InstalledBin: installedBin,
		StatePath:    "/etc/regixtry/regixtry-lifecycle-state.json",
		ManagedPaths: []string{
			"/etc/regixtry/regixtry.env",
			installedBin,
			"/var/lib/regixtry/content",
		},
	})
	if len(got) == 0 {
		t.Fatal("uninstallCleanupTargets() = empty, want cleanup order")
	}
	if got[len(got)-1] != installedBin {
		t.Fatalf("last cleanup target = %q, want %q", got[len(got)-1], installedBin)
	}
	for _, target := range got[:len(got)-1] {
		if target == installedBin {
			t.Fatalf("cleanup order = %v, want installed binary only once at the end", got)
		}
	}
}

func TestBootstrapperUninstallReturnsFailuresInReport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	managedPath := filepath.Join(root, "var", "lib", "regixtry", "content")
	provenancePath := filepath.Join(root, "etc", "regixtry", lifecycleProvenanceFileName)
	if err := os.MkdirAll(filepath.Dir(provenancePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(provenance) error = %v", err)
	}
	provenance := LifecycleProvenance{
		Version:      lifecycleProvenanceVersion,
		Mode:         supportedMode,
		InstalledBin: filepath.Join(root, "usr", "local", "bin", "regixtry"),
		ServiceName:  "regixtry",
		StatePath:    provenancePath,
		ManagedPaths: []string{managedPath},
	}

	b := &Bootstrapper{
		readFile: func(string) ([]byte, error) {
			return []byte(`{"version":1,"mode":"daemon-sqlite","installed_bin":"` + provenance.InstalledBin + `","service_name":"regixtry","state_path":"` + provenancePath + `","managed_paths":["` + managedPath + `"]}`), nil
		},
		stat: func(path string) (os.FileInfo, error) {
			return fakeInfo{name: filepath.Base(path)}, nil
		},
		removeAll: func(path string) error {
			if path == managedPath {
				return errors.New("permission denied")
			}
			return nil
		},
		runCommand: func(_ context.Context, _ string, _ ...string) error {
			return errors.New("systemd unavailable")
		},
	}

	report, err := b.Uninstall(context.Background(), provenancePath)
	if err == nil {
		t.Fatal("Uninstall() error = nil, want aggregated failure")
	}
	if report.Service.Status != CleanupStatusFailed {
		t.Fatalf("service status = %q, want failed", report.Service.Status)
	}
	failedPaths := map[string]string{}
	for _, item := range report.Items {
		if item.Status == CleanupStatusFailed {
			failedPaths[item.Path] = item.Detail
		}
	}
	if _, ok := failedPaths[managedPath]; !ok {
		t.Fatalf("items = %#v, want managed path failure recorded", report.Items)
	}
}

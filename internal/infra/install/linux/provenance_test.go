package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

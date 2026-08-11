package trivy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func TestRunnerProbesManagedVersionAndExecutesScan(t *testing.T) {
	t.Parallel()

	cacheDir := filepath.Join(t.TempDir(), "trivy-cache")
	var commands [][]string
	runner := New(RunnerConfig{Exec: func(_ context.Context, binaryPath string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{binaryPath}, args...))
		if len(args) >= 1 && args[0] == "version" {
			return []byte(`{"Version":"0.57.1"}`), nil
		}
		return []byte(`{"Metadata":{"DBUpdatedAt":"2026-08-10T12:00:00Z"},"Results":[{"Vulnerabilities":[{"Severity":"CRITICAL"},{"Severity":"HIGH"},{"Severity":"LOW"}]}]}`), nil
	}})
	settings := ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: cacheDir, Timeout: time.Minute, RegistryReachableURL: "https://registry.internal:5443"}

	runtime, err := runner.Probe(context.Background(), settings)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if runtime.Health != "ready" || runtime.Version != "0.57.1" || runtime.Mode != ports.FeatureRuntimeModeManaged {
		t.Fatalf("runtime = %#v, want ready managed runtime with version", runtime)
	}

	result, err := runner.Run(context.Background(), "registry.internal:5443/library/alpine@sha256:abc", settings)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Critical != 1 || result.High != 1 || result.Low != 1 || result.TrivyVersion != "0.57.1" {
		t.Fatalf("result = %#v, want severity counts from managed runtime", result)
	}
	if len(commands) != 3 || commands[2][0] != settings.BinaryPath {
		t.Fatalf("commands = %#v, want managed binary execution", commands)
	}
	if !containsArgPair(commands[2][1:], "--cache-dir", settings.CacheDir) {
		t.Fatalf("scan args = %#v, want managed cache dir", commands[2])
	}
}

func TestRunnerRejectsLegacyPathsAndExecFailures(t *testing.T) {
	t.Parallel()

	runner := New(RunnerConfig{Exec: func(ctx context.Context, binaryPath string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "version" {
			return nil, fmt.Errorf("boom version")
		}
		return nil, ctx.Err()
	}})
	if _, err := runner.Probe(context.Background(), ports.ScanSettings{BinaryPath: "/tmp/README.sh", CacheDir: "/var/lib/regixtry/features/trivy/trivy-cache", Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "managed trivy runtime") {
		t.Fatalf("Probe() error = %v, want managed runtime path rejection", err)
	}
	if _, err := runner.Probe(context.Background(), ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: "/var/lib/regixtry/features/trivy/trivy-cache", Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "boom version") {
		t.Fatalf("Probe() error = %v, want exec failure", err)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelRunner := New(RunnerConfig{Exec: func(ctx context.Context, binaryPath string, args ...string) ([]byte, error) {
		return nil, ctx.Err()
	}})
	if _, err := cancelRunner.Run(canceledCtx, "registry.internal/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: "/var/lib/regixtry/features/trivy/trivy-cache", Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
}

func TestRunnerRejectsMalformedScanJSON(t *testing.T) {
	t.Parallel()

	runner := New(RunnerConfig{Exec: func(_ context.Context, binaryPath string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "version" {
			return []byte(`{"Version":"0.58.0"}`), nil
		}
		return []byte("{"), nil
	}})
	if _, err := runner.Run(context.Background(), "registry.internal/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: "/var/lib/regixtry/features/trivy/trivy-cache", Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "decode trivy output") {
		t.Fatalf("Run() error = %v, want malformed JSON rejection", err)
	}
}

func TestRunnerExtractsFindingsAndFreshnessMetadata(t *testing.T) {
	t.Parallel()

	runner := New(RunnerConfig{Exec: func(_ context.Context, binaryPath string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "version" {
			return []byte(`{"Version":"0.58.1","VulnerabilityDB":{"Version":7,"UpdatedAt":"2026-08-10T10:00:00Z","NextUpdate":"2026-08-10T11:00:00Z","DownloadedAt":"2026-08-10T10:05:00Z"}}`), nil
		}
		return []byte(`{"SchemaVersion":2,"CreatedAt":"2026-08-10T12:00:00Z","Metadata":{"DBUpdatedAt":"2026-08-10T10:00:00Z"},"Results":[{"Target":"alpine:3.20","Class":"os-pkgs","Type":"alpine","Vulnerabilities":[{"VulnerabilityID":"CVE-2026-0001","PkgName":"openssl","InstalledVersion":"3.0.0-r0","FixedVersion":"3.0.1-r0","Title":"openssl fix available","PrimaryURL":"https://example.test/CVE-2026-0001","Severity":"CRITICAL","Status":"fixed","PublishedDate":"2026-08-01T10:00:00Z","LastModifiedDate":"2026-08-02T10:00:00Z","DataSource":{"Name":"alpine","URL":"https://example.test/alpine"}},{"VulnerabilityID":"CVE-2026-0002","PkgName":"busybox","InstalledVersion":"1.36.0-r0","Title":"busybox no fix yet","PrimaryURL":"https://example.test/CVE-2026-0002","Severity":"HIGH","Status":"affected","DataSource":{"Name":"alpine","URL":"https://example.test/alpine"}}]}]}`), nil
	}})

	result, err := runner.Run(context.Background(), "registry.internal/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: "/var/lib/regixtry/features/trivy/trivy-cache", Timeout: time.Minute})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := len(result.Findings), 2; got != want {
		t.Fatalf("len(result.Findings) = %d, want %d", got, want)
	}
	if result.Findings[0].Target != "alpine:3.20" || result.Findings[0].Class != "os-pkgs" || result.Findings[0].Type != "alpine" {
		t.Fatalf("first finding = %#v, want target/class/type metadata", result.Findings[0])
	}
	if !result.Findings[0].Fixable || result.Findings[1].Fixable {
		t.Fatalf("findings = %#v, want fixable then non-fixable findings", result.Findings)
	}
	if got, want := result.DBFreshness.ReportSchemaVersion, 2; got != want {
		t.Fatalf("ReportSchemaVersion = %d, want %d", got, want)
	}
	if result.DBFreshness.ReportCreatedAt == nil || result.DBFreshness.DBUpdatedAt == nil || result.DBFreshness.DBDownloadedAt == nil || result.DBFreshness.DBNextUpdateAt == nil {
		t.Fatalf("DBFreshness = %#v, want captured timestamps", result.DBFreshness)
	}
	if got, want := result.DBFreshness.DBVersion, 7; got != want {
		t.Fatalf("DBVersion = %d, want %d", got, want)
	}
	if got, want := result.DBFreshness.FreshnessState, ports.ScanRunDBFreshnessStateStale; got != want {
		t.Fatalf("FreshnessState = %q, want %q", got, want)
	}
}

func TestRunnerRetainsPartialFreshnessMetadata(t *testing.T) {
	t.Parallel()

	runner := New(RunnerConfig{Exec: func(_ context.Context, binaryPath string, args ...string) ([]byte, error) {
		if len(args) >= 1 && args[0] == "version" {
			return []byte(`{"Version":"0.58.1"}`), nil
		}
		return []byte(`{"SchemaVersion":2,"Results":[{"Target":"alpine:3.20","Vulnerabilities":[{"VulnerabilityID":"CVE-2026-0003","PkgName":"apk-tools","InstalledVersion":"1.0.0-r0","PrimaryURL":"https://example.test/CVE-2026-0003","Severity":"MEDIUM"}]}]}`), nil
	}})

	result, err := runner.Run(context.Background(), "registry.internal/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/trivy/bin/active/trivy", CacheDir: "/var/lib/regixtry/features/trivy/trivy-cache", Timeout: time.Minute})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := len(result.Findings), 1; got != want {
		t.Fatalf("len(result.Findings) = %d, want %d", got, want)
	}
	if got, want := result.DBFreshness.FreshnessState, ports.ScanRunDBFreshnessStateUnknown; got != want {
		t.Fatalf("FreshnessState = %q, want %q", got, want)
	}
	if result.DBFreshness.DBUpdatedAt != nil || result.DBFreshness.DBDownloadedAt != nil || result.DBFreshness.DBNextUpdateAt != nil {
		t.Fatalf("DBFreshness = %#v, want omitted optional timestamps to stay nil", result.DBFreshness)
	}
}

func containsArgPair(args []string, name string, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

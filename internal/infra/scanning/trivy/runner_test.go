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

func containsArgPair(args []string, name string, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

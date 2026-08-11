package gitleaks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func TestRunnerProbesManagedVersionAndReportsReady(t *testing.T) {
	t.Parallel()

	var commands [][]string
	runner := New(RunnerConfig{Exec: func(_ context.Context, binaryPath string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{binaryPath}, args...))
		return []byte("8.27.0\n"), nil
	}})
	settings := ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks", Timeout: time.Minute}

	runtime, err := runner.Probe(context.Background(), settings)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if runtime.Health != "ready" || runtime.Version != "8.27.0" || runtime.Mode != ports.FeatureRuntimeModeManaged {
		t.Fatalf("runtime = %#v, want ready managed runtime with version", runtime)
	}
	if len(commands) != 1 || commands[0][0] != settings.BinaryPath || commands[0][1] != "version" {
		t.Fatalf("commands = %#v, want managed binary version execution", commands)
	}
}

// TestRunnerRefusesToProbeBinaryOutsideManagedLayout is the binary-provenance
// threat-matrix RED test: a binary path outside features/gitleaks/ must never
// be executed, even if the caller-supplied settings point at it.
func TestRunnerRefusesToProbeBinaryOutsideManagedLayout(t *testing.T) {
	t.Parallel()

	executed := false
	runner := New(RunnerConfig{Exec: func(context.Context, string, ...string) ([]byte, error) {
		executed = true
		return []byte("8.27.0\n"), nil
	}})

	if _, err := runner.Probe(context.Background(), ports.ScanSettings{BinaryPath: "/tmp/gitleaks", Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "features/gitleaks") {
		t.Fatalf("Probe() error = %v, want managed gitleaks layout rejection", err)
	}
	if executed {
		t.Fatalf("exec was invoked for a binary path outside the managed features/gitleaks layout")
	}
}

func TestRunnerPropagatesExecFailures(t *testing.T) {
	t.Parallel()

	runner := New(RunnerConfig{Exec: func(context.Context, string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("boom version")
	}})
	if _, err := runner.Probe(context.Background(), ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks", Timeout: time.Minute}); err == nil || !strings.Contains(err.Error(), "boom version") {
		t.Fatalf("Probe() error = %v, want exec failure", err)
	}
}

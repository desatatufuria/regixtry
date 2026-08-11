package gitleaks

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"regixtry/internal/ports"
)

type execRunner func(ctx context.Context, binaryPath string, args ...string) ([]byte, error)

type RunnerConfig struct {
	Exec execRunner
}

type Runner struct {
	exec execRunner
}

func New(cfg RunnerConfig) *Runner {
	run := cfg.Exec
	if run == nil {
		run = func(ctx context.Context, binaryPath string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, binaryPath, args...)
			return cmd.Output()
		}
	}
	return &Runner{exec: run}
}

// Probe runs a lightweight "gitleaks version" health check against the
// activated binary, mirroring Trivy's runtime prober: no scan target is
// touched, only the managed binary itself is exercised, and readiness is
// reported only once the binary actually executes and reports a version at
// or above minimumGitleaksVersion.
func (r *Runner) Probe(ctx context.Context, settings ports.ScanSettings) (ports.FeatureRuntime, error) {
	if r == nil || r.exec == nil {
		return ports.FeatureRuntime{}, fmt.Errorf("gitleaks runner is not configured")
	}
	binaryPath := strings.TrimSpace(settings.BinaryPath)
	if binaryPath == "" {
		err := fmt.Errorf("managed gitleaks runtime is not installed")
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}, err
	}
	output, err := r.exec(ctx, binaryPath, "version")
	if err != nil {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}, err
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		err := fmt.Errorf("gitleaks version output was empty")
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}, err
	}
	if err := requireMinimumVersion(version); err != nil {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Version: version, Detail: err.Error(), LastError: err.Error()}, err
	}
	return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusReady), Health: string(ports.FeatureRuntimeStatusReady), Version: version, ActiveBinaryPath: binaryPath}, nil
}

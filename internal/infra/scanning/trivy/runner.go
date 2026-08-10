package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

func (r *Runner) Probe(ctx context.Context, settings ports.ScanSettings) (ports.FeatureRuntime, error) {
	if r == nil || r.exec == nil {
		return ports.FeatureRuntime{}, fmt.Errorf("trivy runner is not configured")
	}
	binaryPath, err := managedBinaryPath(settings.BinaryPath)
	if err != nil {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusDegraded), Health: string(ports.TrivyRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}, err
	}
	output, err := r.exec(ctx, binaryPath, "version", "--format", "json")
	if err != nil {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusDegraded), Health: string(ports.TrivyRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}, err
	}
	var payload struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusDegraded), Health: string(ports.TrivyRuntimeStatusDegraded), Detail: fmt.Sprintf("decode version response: %v", err), LastError: fmt.Sprintf("decode version response: %v", err)}, fmt.Errorf("decode version response: %w", err)
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusDegraded), Health: string(ports.TrivyRuntimeStatusDegraded), Detail: "version response did not include Version", LastError: "version response did not include Version"}, fmt.Errorf("version response did not include Version")
	}
	return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusReady), Health: string(ports.TrivyRuntimeStatusReady), Version: version, ActiveBinaryPath: binaryPath}, nil
}

func (r *Runner) Run(ctx context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	if r == nil || r.exec == nil {
		return ports.ScanResult{}, fmt.Errorf("trivy runner is not configured")
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	runtime, err := r.Probe(runCtx, settings)
	if err != nil {
		return ports.ScanResult{}, err
	}
	binaryPath, err := managedBinaryPath(settings.BinaryPath)
	if err != nil {
		return ports.ScanResult{}, err
	}
	args := []string{"image", "--format", "json"}
	if cacheDir := strings.TrimSpace(settings.CacheDir); cacheDir != "" {
		args = append(args, "--cache-dir", cacheDir)
	}
	args = append(args, strings.TrimSpace(imageRef))
	output, err := r.exec(runCtx, binaryPath, args...)
	if err != nil {
		return ports.ScanResult{}, err
	}
	var payload struct {
		Metadata struct {
			DBUpdatedAt *time.Time `json:"DBUpdatedAt"`
		} `json:"Metadata"`
		Results []struct {
			Vulnerabilities []struct {
				Severity string `json:"Severity"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return ports.ScanResult{}, fmt.Errorf("decode trivy output: %w", err)
	}
	result := ports.ScanResult{TrivyVersion: runtime.Version, DBUpdatedAt: payload.Metadata.DBUpdatedAt}
	for _, section := range payload.Results {
		for _, vulnerability := range section.Vulnerabilities {
			switch strings.ToUpper(strings.TrimSpace(vulnerability.Severity)) {
			case "CRITICAL":
				result.Critical++
			case "HIGH":
				result.High++
			case "MEDIUM":
				result.Medium++
			case "LOW":
				result.Low++
			}
		}
	}
	return result, nil
}

func managedBinaryPath(binaryPath string) (string, error) {
	trimmed := strings.TrimSpace(binaryPath)
	if trimmed == "" {
		return "", fmt.Errorf("managed trivy runtime is not installed")
	}
	clean := filepath.Clean(trimmed)
	if !strings.Contains(clean, string(filepath.Separator)+"features"+string(filepath.Separator)+"trivy"+string(filepath.Separator)) {
		return "", fmt.Errorf("managed trivy runtime must execute only from the Regixtry-owned features/trivy layout")
	}
	return clean, nil
}

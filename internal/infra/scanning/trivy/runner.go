package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"regixtry/internal/ports"
)

type ExecCommand func(ctx context.Context, name string, args ...string) ([]byte, []byte, error)

type RunnerConfig struct {
	ExecCommand ExecCommand
	Env         []string
}

type Runner struct {
	execCommand ExecCommand
	env         []string
}

func New(cfg RunnerConfig) *Runner {
	execCommand := cfg.ExecCommand
	if execCommand == nil {
		execCommand = func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			if len(cfg.Env) > 0 {
				cmd.Env = append([]string(nil), cfg.Env...)
			}
			stdout, err := cmd.Output()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					return stdout, exitErr.Stderr, err
				}
				return stdout, nil, err
			}
			return stdout, nil, nil
		}
	}
	return &Runner{execCommand: execCommand, env: append([]string(nil), cfg.Env...)}
}

func (r *Runner) Run(ctx context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	if r == nil {
		return ports.ScanResult{}, fmt.Errorf("trivy runner is not configured")
	}
	if strings.TrimSpace(settings.BinaryPath) == "" {
		settings.BinaryPath = "trivy"
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr, err := r.execCommand(runCtx, settings.BinaryPath, "image", "--quiet", "--format", "json", "--cache-dir", settings.CacheDir, imageRef)
	if err != nil {
		if runCtx.Err() != nil {
			return ports.ScanResult{}, runCtx.Err()
		}
		return ports.ScanResult{}, fmt.Errorf("run trivy: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	var payload struct {
		Metadata struct {
			DBUpdatedAt *time.Time `json:"DBUpdatedAt"`
		} `json:"Metadata"`
		ArtifactName string `json:"ArtifactName"`
		Results      []struct {
			Vulnerabilities []struct {
				Severity string `json:"Severity"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(stdout, &payload); err != nil {
		return ports.ScanResult{}, fmt.Errorf("decode trivy output: %w", err)
	}
	result := ports.ScanResult{TrivyVersion: settings.BinaryPath, DBUpdatedAt: payload.Metadata.DBUpdatedAt}
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

package gitleaks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"regixtry/internal/ports"
)

type execRunner func(ctx context.Context, binaryPath string, args ...string) ([]byte, error)

type RunnerConfig struct {
	Exec  execRunner
	Blobs ports.BlobStore
}

type Runner struct {
	exec  execRunner
	blobs ports.BlobStore
}

func New(cfg RunnerConfig) *Runner {
	run := cfg.Exec
	if run == nil {
		run = func(ctx context.Context, binaryPath string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, binaryPath, args...)
			return cmd.Output()
		}
	}
	return &Runner{exec: run, blobs: cfg.Blobs}
}

// Run stages target's blobs (config + layers, per design.md decision 7),
// invokes the managed gitleaks binary in "dir" mode against the staged
// directory (Exec Surface in design.md, single source of truth for the
// argv), and decodes the resulting report through the redacting decoder in
// report.go. The argv is a fixed literal slice plus adapter-generated paths
// only — no finding value, secret, or other user-controlled string ever
// reaches argv or an error message.
func (r *Runner) Run(ctx context.Context, target ports.SecretScanTarget, settings ports.ScanSettings) (ports.SecretScanResult, error) {
	if r == nil || r.exec == nil {
		return ports.SecretScanResult{}, fmt.Errorf("gitleaks runner is not configured")
	}
	if r.blobs == nil {
		return ports.SecretScanResult{}, fmt.Errorf("gitleaks runner blob store is not configured")
	}
	binaryPath, err := managedBinaryPath(settings.BinaryPath)
	if err != nil {
		return ports.SecretScanResult{}, err
	}

	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workRoot := filepath.Join(strings.TrimSpace(settings.CacheDir), "work")
	staged, cleanup, err := stageManifestBlobs(runCtx, r.blobs, workRoot, target.Blobs)
	defer cleanup()
	if err != nil {
		return ports.SecretScanResult{}, err
	}

	reportPath := filepath.Join(staged.RunDir, "report.json")
	args := []string{
		"dir", staged.ScanDir,
		"--report-format", "json",
		"--report-path", reportPath,
		"--no-banner",
		"--redact",
		"--exit-code", "0",
		"--max-archive-depth", "2",
	}
	if _, err := r.exec(runCtx, binaryPath, args...); err != nil {
		return ports.SecretScanResult{}, fmt.Errorf("gitleaks scan execution failed")
	}

	reportBody, err := os.ReadFile(reportPath)
	if err != nil {
		return ports.SecretScanResult{}, fmt.Errorf("gitleaks report was not produced")
	}
	entries, err := decodeReport(reportBody)
	if err != nil {
		return ports.SecretScanResult{}, err
	}

	findings := make([]ports.SecretFinding, 0, len(entries))
	for _, entry := range entries {
		findings = append(findings, attributeFinding(entry, staged.Staged, staged.ScanDir))
	}

	skipped := make([]string, 0, len(staged.Skipped))
	for _, blob := range staged.Skipped {
		skipped = append(skipped, blob.Digest.String())
	}

	return ports.SecretScanResult{Findings: findings, SkippedBlobs: skipped}, nil
}

// attributeFinding maps one decoded report entry (which only ever carries
// RuleID/Description/File/StartLine/EndLine/Tags, per report.go) to a
// ports.SecretFinding by matching its File against the staged blob paths:
// the longest matching staged path (relative to the scanned directory)
// identifies the source blob, and BlobDigest/Path never carry the matched
// secret text itself — only rule ID and location, per design.md decision 10.
//
// When no staged path matches (e.g. gitleaks reports a member inside a
// nested archive, per --max-archive-depth, whose prefix does not line up
// with a staged blob), BlobDigest stays empty but Path still carries the
// raw File gitleaks reported — an operator can act on that relative path
// even without a resolved blob, instead of seeing an unattributed "unknown".
func attributeFinding(entry reportEntry, staged []stagedBlob, scanDir string) ports.SecretFinding {
	finding := ports.SecretFinding{
		RuleID:      entry.RuleID,
		Description: entry.Description,
		StartLine:   entry.StartLine,
		EndLine:     entry.EndLine,
		Tags:        entry.Tags,
	}
	file := filepath.ToSlash(strings.TrimSpace(entry.File))
	bestPrefix := ""
	var bestBlob stagedBlob
	for _, blob := range staged {
		relative, err := filepath.Rel(scanDir, blob.Path)
		if err != nil {
			continue
		}
		relative = filepath.ToSlash(relative)
		matches := file == relative || strings.HasPrefix(file, relative+"!") || strings.HasPrefix(file, relative+":")
		if matches && len(relative) > len(bestPrefix) {
			bestPrefix = relative
			bestBlob = blob
		}
	}
	if bestPrefix == "" {
		finding.Path = file
		return finding
	}
	finding.BlobDigest = bestBlob.Descriptor.Digest.String()
	remainder := strings.TrimPrefix(file, bestPrefix)
	remainder = strings.TrimPrefix(remainder, "!")
	remainder = strings.TrimPrefix(remainder, ":")
	finding.Path = remainder
	return finding
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
	binaryPath, err := managedBinaryPath(settings.BinaryPath)
	if err != nil {
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

// managedBinaryPath is the binary-provenance execution-path guard: only a
// path resolving under the Regixtry-owned features/gitleaks layout may ever
// be executed. This mirrors Trivy's managedBinaryPath guard and rejects any
// attacker-supplied or misconfigured binary_path before exec is reached.
func managedBinaryPath(binaryPath string) (string, error) {
	trimmed := strings.TrimSpace(binaryPath)
	if trimmed == "" {
		return "", fmt.Errorf("managed gitleaks runtime is not installed")
	}
	clean := filepath.Clean(trimmed)
	if !strings.Contains(clean, string(filepath.Separator)+"features"+string(filepath.Separator)+"gitleaks"+string(filepath.Separator)) {
		return "", fmt.Errorf("managed gitleaks runtime must execute only from the Regixtry-owned features/gitleaks layout")
	}
	return clean, nil
}

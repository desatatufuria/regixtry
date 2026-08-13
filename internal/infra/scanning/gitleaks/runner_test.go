package gitleaks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
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

// TestRunnerRunInvokesGitleaksWithExactLiteralArgv is the argv-snapshot RED
// test (tasks.md 4.6): the "dir" invocation must match design.md's Exec
// Surface exactly (fixed literal flags plus only adapter-generated paths),
// and no finding value or secret-shaped string may ever reach argv.
func TestRunnerRunInvokesGitleaksWithExactLiteralArgv(t *testing.T) {
	t.Parallel()

	configDigest := domain.DigestFromBytes([]byte("config-body"))
	layerDigest := domain.DigestFromBytes([]byte("layer-body"))
	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{
		configDigest: []byte("config-body"),
		layerDigest:  []byte("layer-body"),
	}}

	var capturedBinary string
	var capturedArgs []string
	runner := New(RunnerConfig{Blobs: store, Exec: func(_ context.Context, binaryPath string, args ...string) ([]byte, error) {
		capturedBinary = binaryPath
		capturedArgs = append([]string{}, args...)
		reportPath := argValue(args, "--report-path")
		if reportPath == "" {
			return nil, fmt.Errorf("fake exec: --report-path not found in argv")
		}
		if err := os.WriteFile(reportPath, []byte(`[]`), 0o600); err != nil {
			return nil, err
		}
		return nil, nil
	}})

	target := ports.SecretScanTarget{
		Repository: "library/alpine",
		Digest:     "sha256:deadbeef",
		Blobs: []domain.Descriptor{
			{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: 11},
			{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerDigest, Size: 10},
		},
	}
	settings := ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks", CacheDir: t.TempDir(), Timeout: time.Minute}

	result, err := runner.Run(context.Background(), target, settings)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("result.Findings = %#v, want none for an empty report", result.Findings)
	}
	if capturedBinary != settings.BinaryPath {
		t.Fatalf("captured binary = %q, want %q", capturedBinary, settings.BinaryPath)
	}
	if len(capturedArgs) == 0 || capturedArgs[0] != "dir" {
		t.Fatalf("capturedArgs = %#v, want the \"dir\" subcommand first", capturedArgs)
	}
	scanDir := capturedArgs[1]
	if !strings.Contains(scanDir, "scan") {
		t.Fatalf("scanDir = %q, want it to point at the staged scan directory", scanDir)
	}
	wantFixedFlags := []string{"--report-format", "json", "--no-banner", "--redact", "--exit-code", "0", "--max-archive-depth", "2"}
	for _, flag := range wantFixedFlags {
		found := false
		for _, arg := range capturedArgs {
			if arg == flag {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("capturedArgs = %#v, want to contain fixed flag %q from design.md's Exec Surface", capturedArgs, flag)
		}
	}
	for _, arg := range capturedArgs {
		if strings.Contains(arg, "AKIA") || strings.Contains(strings.ToLower(arg), "secret") {
			t.Fatalf("capturedArgs = %#v, want no finding value or secret-shaped string in argv", capturedArgs)
		}
	}
}

// TestRunnerRunArgvUnchangedWithoutConfigOverride is the argv regression pin
// (tasks.md 6.5): with no ConfigPath override set, the argv stays the exact
// 10-element fixed literal slice from today, with no trailing --config flag.
func TestRunnerRunArgvUnchangedWithoutConfigOverride(t *testing.T) {
	t.Parallel()

	configDigest := domain.DigestFromBytes([]byte("config-body"))
	layerDigest := domain.DigestFromBytes([]byte("layer-body"))
	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{
		configDigest: []byte("config-body"),
		layerDigest:  []byte("layer-body"),
	}}

	var capturedArgs []string
	runner := New(RunnerConfig{Blobs: store, Exec: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		capturedArgs = append([]string{}, args...)
		reportPath := argValue(args, "--report-path")
		if reportPath == "" {
			return nil, fmt.Errorf("fake exec: --report-path not found in argv")
		}
		return nil, os.WriteFile(reportPath, []byte(`[]`), 0o600)
	}})

	target := ports.SecretScanTarget{
		Repository: "library/alpine",
		Digest:     "sha256:deadbeef",
		Blobs: []domain.Descriptor{
			{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: 11},
			{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerDigest, Size: 10},
		},
	}
	settings := ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks", CacheDir: t.TempDir(), Timeout: time.Minute}

	if _, err := runner.Run(context.Background(), target, settings); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(capturedArgs) != 12 {
		t.Fatalf("capturedArgs = %#v, want 12 literal elements without a --config override", capturedArgs)
	}
	for _, arg := range capturedArgs {
		if arg == "--config" {
			t.Fatalf("capturedArgs = %#v, want no --config flag when ConfigPath is empty", capturedArgs)
		}
	}
}

// TestRunnerRunAppendsConfigFlagWhenOverridePathSet covers tasks.md 6.6:
// ConfigPath appends --config after the existing fixed literal slice
// (design.md Decision 5's Gitleaks Exec Surface).
func TestRunnerRunAppendsConfigFlagWhenOverridePathSet(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "alpine.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile(configPath) error = %v", err)
	}

	configDigest := domain.DigestFromBytes([]byte("config-body"))
	layerDigest := domain.DigestFromBytes([]byte("layer-body"))
	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{
		configDigest: []byte("config-body"),
		layerDigest:  []byte("layer-body"),
	}}

	var capturedArgs []string
	runner := New(RunnerConfig{Blobs: store, Exec: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		capturedArgs = append([]string{}, args...)
		reportPath := argValue(args, "--report-path")
		if reportPath == "" {
			return nil, fmt.Errorf("fake exec: --report-path not found in argv")
		}
		return nil, os.WriteFile(reportPath, []byte(`[]`), 0o600)
	}})

	target := ports.SecretScanTarget{
		Repository: "library/alpine",
		Digest:     "sha256:deadbeef",
		Blobs: []domain.Descriptor{
			{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: 11},
			{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerDigest, Size: 10},
		},
	}
	settings := ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks", CacheDir: t.TempDir(), Timeout: time.Minute, ConfigPath: configPath}

	if _, err := runner.Run(context.Background(), target, settings); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(capturedArgs) < 2 || capturedArgs[len(capturedArgs)-2] != "--config" || capturedArgs[len(capturedArgs)-1] != configPath {
		t.Fatalf("capturedArgs = %#v, want --config %q appended after the fixed literal slice", capturedArgs, configPath)
	}
}

// TestRunnerRunFailsPreflightOnUnreadableConfigPath is the fail-open
// threat-matrix RED test (tasks.md 6.7): a missing or unreadable ConfigPath
// must fail the run before r.exec is ever invoked (design.md Decision 6 —
// gitleaks fatals on a bad --config, so the pre-flight check must beat it).
func TestRunnerRunFailsPreflightOnUnreadableConfigPath(t *testing.T) {
	t.Parallel()

	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{}}
	executed := false
	runner := New(RunnerConfig{Blobs: store, Exec: func(context.Context, string, ...string) ([]byte, error) {
		executed = true
		return nil, fmt.Errorf("exec should not have been invoked")
	}})

	target := ports.SecretScanTarget{Repository: "library/alpine", Digest: "sha256:deadbeef"}
	settings := ports.ScanSettings{
		BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks",
		CacheDir:   t.TempDir(),
		Timeout:    time.Minute,
		ConfigPath: filepath.Join(t.TempDir(), "missing.toml"),
	}

	if _, err := runner.Run(context.Background(), target, settings); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("Run() error = %v, want config path readability rejection", err)
	}
	if executed {
		t.Fatalf("exec was invoked despite an unreadable/missing config override path")
	}
}

// argValue returns the value following the given flag in args, or "" if the
// flag is absent.
func argValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// TestRunnerRunIntegrationAttributesFindingToBlobWithoutSecretMaterial is the
// integration test (tasks.md 4.11): a tar.gz fixture containing a known,
// obviously-fake test secret pattern is staged and scanned through a
// fake/stubbed gitleaks exec that emits a realistic report JSON shape. The
// resulting SecretFinding must be attributed to the right blob/location and
// must not carry the secret material anywhere in the result.
func TestRunnerRunIntegrationAttributesFindingToBlobWithoutSecretMaterial(t *testing.T) {
	t.Parallel()

	const testSecret = "AKIAFAKEFAKEFAKEFAKE"
	archiveBody := buildTarGzFixture(t, "fake-secrets.txt", "AWS_ACCESS_KEY_ID="+testSecret+"\n")
	layerDigest := domain.DigestFromBytes(archiveBody)
	configBody := []byte(`{"config":{"Env":["PATH=/usr/bin"]}}`)
	configDigest := domain.DigestFromBytes(configBody)

	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{
		layerDigest:  archiveBody,
		configDigest: configBody,
	}}

	runner := New(RunnerConfig{Blobs: store, Exec: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		reportPath := argValue(args, "--report-path")
		if reportPath == "" {
			return nil, fmt.Errorf("fake exec: --report-path not found in argv")
		}
		stagedLayerRelative := filepath.ToSlash(filepath.Join("layers", fmt.Sprintf("001-%s.tar.gz", digest12(layerDigest))))
		report := fmt.Sprintf(`[
  {
    "RuleID": "aws-access-token",
    "Description": "AWS Access Token",
    "StartLine": 1,
    "EndLine": 1,
    "File": "%s!fake-secrets.txt",
    "Secret": "%s",
    "Match": "%s",
    "Fingerprint": "%s!fake-secrets.txt:aws-access-token:1",
    "Entropy": 3.7,
    "Tags": ["aws"]
  }
]`, stagedLayerRelative, testSecret, testSecret, stagedLayerRelative)
		return nil, os.WriteFile(reportPath, []byte(report), 0o600)
	}})

	target := ports.SecretScanTarget{
		Repository: "library/alpine",
		Digest:     "sha256:deadbeef",
		Blobs: []domain.Descriptor{
			{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: int64(len(configBody))},
			{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: layerDigest, Size: int64(len(archiveBody))},
		},
	}
	settings := ports.ScanSettings{BinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks", CacheDir: t.TempDir(), Timeout: time.Minute}

	result, err := runner.Run(context.Background(), target, settings)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("len(result.Findings) = %d, want 1", len(result.Findings))
	}
	finding := result.Findings[0]
	if finding.RuleID != "aws-access-token" {
		t.Fatalf("finding.RuleID = %q, want aws-access-token", finding.RuleID)
	}
	if finding.BlobDigest != layerDigest.String() {
		t.Fatalf("finding.BlobDigest = %q, want %q (attributed to the layer that actually contains the secret)", finding.BlobDigest, layerDigest.String())
	}
	if finding.Path != "fake-secrets.txt" {
		t.Fatalf("finding.Path = %q, want the inner archive path", finding.Path)
	}

	marshaled, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("json.Marshal(finding) error = %v", err)
	}
	if strings.Contains(string(marshaled), testSecret) {
		t.Fatalf("marshaled finding = %s, want no secret material anywhere in the result", marshaled)
	}
	if strings.Contains(string(marshaled), "Fingerprint") || strings.Contains(string(marshaled), "Entropy") {
		t.Fatalf("marshaled finding = %s, want no fingerprint/entropy field on SecretFinding", marshaled)
	}
}

func buildTarGzFixture(t *testing.T, filename string, body string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gzw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: filename, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("WriteHeader(%s) error = %v", filename, err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatalf("Write(%s) error = %v", filename, err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	return raw.Bytes()
}

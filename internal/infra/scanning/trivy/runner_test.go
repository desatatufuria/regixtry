package trivy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func TestRunnerBuildsExplicitArgv(t *testing.T) {
	t.Parallel()

	runner := New(RunnerConfig{ExecCommand: func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte(`{"Results":[{"Vulnerabilities":[{"Severity":"CRITICAL"},{"Severity":"HIGH"}]}]}`), nil, nil
	}})

	result, err := runner.Run(context.Background(), "registry.example.com/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: "trivy", CacheDir: "/tmp/trivy-cache", Timeout: time.Minute})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Critical != 1 || result.High != 1 {
		t.Fatalf("result = %#v, want severity counts", result)
	}
}

func TestRunnerRejectsNonZeroExitMalformedJSONAndCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		exec func(context.Context, string, ...string) ([]byte, []byte, error)
		ctx  func() (context.Context, context.CancelFunc)
		want string
	}{
		{name: "non-zero exit", exec: func(context.Context, string, ...string) ([]byte, []byte, error) {
			return nil, []byte("boom"), errors.New("exit 1")
		}, ctx: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, want: "exit 1"},
		{name: "malformed json", exec: func(context.Context, string, ...string) ([]byte, []byte, error) { return []byte("{"), nil, nil }, ctx: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, want: "decode trivy output"},
		{name: "canceled", exec: func(ctx context.Context, _ string, _ ...string) ([]byte, []byte, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		}, ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}, want: "context canceled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.ctx()
			defer cancel()
			runner := New(RunnerConfig{ExecCommand: tt.exec})
			_, err := runner.Run(ctx, "registry.example.com/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: "trivy", CacheDir: "/tmp/trivy-cache", Timeout: time.Minute})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRunnerUsesCommandContextWithoutShell(t *testing.T) {
	t.Parallel()

	scriptDir := t.TempDir()
	out := filepath.Join(scriptDir, "argv.txt")
	scriptPath := filepath.Join(scriptDir, "trivy")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '%s\n' \"$0\" \"$@\" > \""+out+"\"\nprintf '{\"Results\":[]}'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	runner := New(RunnerConfig{})
	if _, err := runner.Run(context.Background(), "registry.example.com/library/alpine@sha256:abc", ports.ScanSettings{BinaryPath: scriptPath, CacheDir: "/tmp/trivy-cache", Timeout: time.Minute}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile(argv) error = %v", err)
	}
	if strings.Contains(string(body), "sh -c") {
		t.Fatalf("argv = %q, did not expect shell execution", string(body))
	}
}

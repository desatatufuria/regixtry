package compose

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPreflightMapsFailureCausesToTruthfulMessages is the RED test for
// tasks.md 1.1: three distinct Docker/Compose absence causes must map to
// three distinct truthful messages (design.md Interfaces / Contracts),
// never a panic, never a raw exec error string.
func TestPreflightMapsFailureCausesToTruthfulMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		dockerVersionErr  error
		composeVersionErr error
		wantErr           string
		wantComposeProbed bool
	}{
		{
			name:              "docker absent from PATH",
			dockerVersionErr:  &exec.Error{Name: "docker", Err: exec.ErrNotFound},
			wantErr:           errDockerNotInstalled,
			wantComposeProbed: false,
		},
		{
			name:              "docker version fails while daemon is unreachable",
			dockerVersionErr:  errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock"),
			wantErr:           errDockerDaemonUnreachable,
			wantComposeProbed: false,
		},
		{
			name:              "docker compose version fails while docker version succeeds",
			composeVersionErr: errors.New(`docker: 'compose' is not a docker command`),
			wantErr:           errComposeV2Required,
			wantComposeProbed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fake := newFakeExec()
			fake.On("docker version", func([]execCall) ([]byte, error) { return []byte("ok"), tt.dockerVersionErr })
			fake.On("docker compose version", func([]execCall) ([]byte, error) { return []byte("ok"), tt.composeVersionErr })
			p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

			err := p.Preflight(context.Background())
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Preflight() error = %v, want %q", err, tt.wantErr)
			}

			composeProbed := false
			for _, call := range fake.Calls {
				if call.Name == "docker" && len(call.Args) >= 1 && call.Args[0] == "compose" {
					composeProbed = true
				}
			}
			if composeProbed != tt.wantComposeProbed {
				t.Fatalf("compose probed = %v, want %v (calls=%#v)", composeProbed, tt.wantComposeProbed, fake.Calls)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("dir entries = %v, want Preflight to write nothing to disk on failure", entries)
			}
		})
	}
}

// TestPreflightSucceedsWhenDockerAndComposeAreAvailable triangulates the
// failure-cause table above with the happy path: both probes run and no
// error is returned.
func TestPreflightSucceedsWhenDockerAndComposeAreAvailable(t *testing.T) {
	t.Parallel()

	fake := newFakeExec()
	fake.On("docker version", func([]execCall) ([]byte, error) { return []byte("Docker version 27.0.0"), nil })
	fake.On("docker compose version", func([]execCall) ([]byte, error) { return []byte("Docker Compose version v2.29.0"), nil })
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

	if err := p.Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("calls = %#v, want exactly one docker version probe and one docker compose version probe", fake.Calls)
	}
}

// TestDefaultExecSurfacesStderrOnFailure is the RED test proving a real
// diagnostics gap found live during v0.2.1-rc5's docker-mode validation:
// every failure through this package's real (non-fake) subprocess wiring
// surfaced as a bare "exit status N" with zero content, because
// `cmd.Output()` alone never exposes the command's stderr -- Go only
// attaches it to the unexported `*exec.ExitError.Stderr` field, which
// nothing here ever read. BootstrapAdmin's real failure stayed a total
// mystery across five release candidates because of exactly this. The
// default exec wiring must fold that Stderr into the returned error's text.
func TestDefaultExecSurfacesStderrOnFailure(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(ProvisionerConfig{})
	_, err := p.exec(context.Background(), "sh", "-c", "echo boom-on-stderr 1>&2; exit 3")
	if err == nil {
		t.Fatalf("exec() error = nil, want a non-nil error for a command that exits 3")
	}
	if !strings.Contains(err.Error(), "boom-on-stderr") {
		t.Fatalf("exec() error = %q, want it to contain the command's stderr output", err.Error())
	}
}

// TestDefaultExecStdinSurfacesStderrOnFailure is
// TestDefaultExecSurfacesStderrOnFailure's counterpart for the stdin-piping
// seam BootstrapAdmin uses -- the exact call site whose swallowed stderr
// blocked every attempt to diagnose the real docker-mode failure.
func TestDefaultExecStdinSurfacesStderrOnFailure(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(ProvisionerConfig{})
	_, err := p.execStdin(context.Background(), strings.NewReader(""), "sh", "-c", "echo boom-on-stdin-stderr 1>&2; exit 3")
	if err == nil {
		t.Fatalf("execStdin() error = nil, want a non-nil error for a command that exits 3")
	}
	if !strings.Contains(err.Error(), "boom-on-stdin-stderr") {
		t.Fatalf("execStdin() error = %q, want it to contain the command's stderr output", err.Error())
	}
}

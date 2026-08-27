package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestDownTearsDownStackAndRemovesProjectFiles is the RED test for tasks.md
// 6.6: Down runs `docker compose ... down --volumes` (composeArgs' shared
// --project-name/--file/--env-file prefix, same as every other compose
// invocation in this package -- PR #2's apply-progress recorded why
// --env-file must always be explicit) and removes the generated project
// directory, taking the compose file and env file with it -- the compose
// analogue of rollbackSetupFailure (design.md Data Flow).
func TestDownTearsDownStackAndRemovesProjectFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	proj := testProject(true)
	proj.Dir = dir
	proj.ComposeFilePath = filepath.Join(dir, "docker-compose.yml")
	proj.EnvFilePath = filepath.Join(dir, "regixtry.env")
	if err := os.WriteFile(proj.ComposeFilePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(proj.EnvFilePath, []byte("REGIXTRY_PORT=5000\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}

	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "down", "--volumes")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

	if err := p.Down(context.Background(), proj); err != nil {
		t.Fatalf("Down() error = %v, want nil", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one `down --volumes` call", fake.Calls)
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat(dir) error = %v, want the project directory (and everything under it) removed", err)
	}
}

// TestDownStillRemovesFilesWhenComposeDownFails triangulates 6.6/6.7: even
// when the `docker compose down` subprocess itself fails, Down still
// attempts to remove the generated project files -- the same
// "cleanup best-effort, join errors" shape rollbackWithReceipt already uses
// in internal/infra/install/linux/bootstrap.go -- and reports the compose
// failure rather than silently swallowing it.
func TestDownStillRemovesFilesWhenComposeDownFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	proj := testProject(true)
	proj.Dir = dir
	proj.ComposeFilePath = filepath.Join(dir, "docker-compose.yml")
	proj.EnvFilePath = filepath.Join(dir, "regixtry.env")
	if err := os.WriteFile(proj.ComposeFilePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	fake := newFakeExec()
	downErr := errors.New("fake: compose down failed")
	fake.On("docker "+joinArgs(composeArgs(proj, "down", "--volumes")), func([]execCall) ([]byte, error) {
		return nil, downErr
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

	err := p.Down(context.Background(), proj)
	if err == nil {
		t.Fatalf("Down() error = nil, want the compose down failure surfaced")
	}

	if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Stat(dir) error = %v, want the project directory still removed despite the compose down failure", statErr)
	}
}

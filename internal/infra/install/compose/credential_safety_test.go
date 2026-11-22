package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func noopSleep(time.Duration) {}

// credentialSafetyFlow runs WriteProject -> StartDatabase -> BootstrapAdmin
// end to end against a fake exec seam (no Docker daemon) and returns the
// generated bundled Postgres password plus every recorded exec call, so the
// tests below can assert the password's full disclosure surface across the
// whole PR #2 orchestration, not just one method in isolation.
func credentialSafetyFlow(t *testing.T, adminPassword string) (Project, string, *fakeExec) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "project")
	fake := newFakeExec()
	fake.Default = func(execCall) ([]byte, error) { return []byte("ok"), nil }
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run, ExecStdin: fake.RunStdin, Sleep: noopSleep})

	project, err := p.WriteProject(ProjectConfig{
		Dir:   dir,
		Name:  "regixtry",
		Image: "ghcr.io/desatatufuria/regixtry:latest",
		Port:  "5000",
	})
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}
	// waitPostgresReady (database.go) reads compose logs looking for the
	// real postgres image's own readiness marker, not just a successful
	// exec -- the blanket "ok" Default above doesn't satisfy that, so this
	// flow needs its own handler for exactly that call, same as any other
	// real environment's postgres would eventually log.
	fake.On("docker "+joinArgs(composeArgs(project, "logs", "--no-color", "postgres")), func([]execCall) ([]byte, error) {
		return []byte("database system is ready to accept connections"), nil
	})

	if err := p.StartDatabase(context.Background(), project); err != nil {
		t.Fatalf("StartDatabase() error = %v", err)
	}
	if err := p.BootstrapAdmin(context.Background(), project, "admin", adminPassword); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}

	envBody, err := os.ReadFile(project.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	dbPassword := extractEnvValue(t, string(envBody), "REGIXTRY_POSTGRES_PASSWORD")
	if dbPassword == "" {
		t.Fatalf("REGIXTRY_POSTGRES_PASSWORD was empty in %q", envBody)
	}

	return project, dbPassword, fake
}

// TestCredentialSafetyBundledPasswordNeverReachesArgvOrStdin proves property
// 1 of the orchestrator-mandated security coverage: the generated bundled
// Postgres password must never appear in any argv element, and must never
// appear on any call's stdin either -- only the operator-supplied admin
// password may travel on stdin (BootstrapAdmin's single stdin-bearing
// call), and the bundled password travels through neither channel.
func TestCredentialSafetyBundledPasswordNeverReachesArgvOrStdin(t *testing.T) {
	t.Parallel()

	const adminPassword = "operator-supplied-admin-pw"
	_, dbPassword, fake := credentialSafetyFlow(t, adminPassword)

	for _, call := range fake.Calls {
		for _, arg := range call.Args {
			if strings.Contains(arg, dbPassword) {
				t.Fatalf("argv element %q contains the generated bundled Postgres password; it must never reach argv", arg)
			}
		}
		if strings.Contains(call.Stdin, dbPassword) {
			t.Fatalf("call %+v carries the bundled Postgres password on stdin; only the admin password may travel via stdin", call)
		}
	}
}

// TestCredentialSafetyBundledPasswordAbsentFromComposeBytes proves property
// 2: after the full StartDatabase/BootstrapAdmin flow runs against the
// materialized project, the on-disk compose file still carries only
// ${...} interpolation for the password, never the literal secret --
// re-confirms PR #1's WriteProject guarantee holds through PR #2's
// additional orchestration, not just immediately after WriteProject.
func TestCredentialSafetyBundledPasswordAbsentFromComposeBytes(t *testing.T) {
	t.Parallel()

	project, dbPassword, _ := credentialSafetyFlow(t, "another-admin-pw")

	composeBody, err := os.ReadFile(project.ComposeFilePath)
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	if strings.Contains(string(composeBody), dbPassword) {
		t.Fatalf("compose file contains the generated bundled Postgres password; it must only ever carry ${...} interpolation")
	}
	if !strings.Contains(string(composeBody), "${REGIXTRY_POSTGRES_PASSWORD") {
		t.Fatalf("compose file = %q, want the mandatory ${REGIXTRY_POSTGRES_PASSWORD:?...} interpolation to still be present", composeBody)
	}
}

// TestCredentialSafetyErrorOutputNeverContainsBundledPassword proves
// property 3: when StartDatabase or BootstrapAdmin fail, the returned
// error's text must never embed the generated bundled Postgres password --
// a truthful failure message is not an excuse to leak a secret into logs.
func TestCredentialSafetyErrorOutputNeverContainsBundledPassword(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "project")
	fake := newFakeExec()
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run, ExecStdin: fake.RunStdin, Sleep: noopSleep})

	project, err := p.WriteProject(ProjectConfig{Dir: dir, Name: "regixtry", Image: "img", Port: "5000"})
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}
	envBody, err := os.ReadFile(project.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	dbPassword := extractEnvValue(t, string(envBody), "REGIXTRY_POSTGRES_PASSWORD")

	fake.Default = func(execCall) ([]byte, error) { return nil, errFakePgNotReady }

	dbErr := p.StartDatabase(context.Background(), project)
	if dbErr == nil {
		t.Fatalf("StartDatabase() error = nil, want the bounded-poll failure to surface")
	}
	if strings.Contains(dbErr.Error(), dbPassword) {
		t.Fatalf("StartDatabase error = %v, must never contain the bundled Postgres password", dbErr)
	}

	adminErr := p.BootstrapAdmin(context.Background(), project, "admin", "admin-pw")
	if adminErr == nil {
		t.Fatalf("BootstrapAdmin() error = nil, want the fake failure to surface")
	}
	if strings.Contains(adminErr.Error(), dbPassword) {
		t.Fatalf("BootstrapAdmin error = %v, must never contain the bundled Postgres password", adminErr)
	}
}

// TestCredentialSafetyEnvFileIsSoleChannelWith0600Mode proves property 4:
// after the full flow runs, the generated bundled Postgres password is
// readable only from the 0600 env file -- the file mode has not regressed
// and no sibling file at the project directory carries the secret.
func TestCredentialSafetyEnvFileIsSoleChannelWith0600Mode(t *testing.T) {
	t.Parallel()

	project, dbPassword, _ := credentialSafetyFlow(t, "third-admin-pw")

	info, err := os.Stat(project.EnvFilePath)
	if err != nil {
		t.Fatalf("Stat(env) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("env file mode = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(project.Dir)
	if err != nil {
		t.Fatalf("ReadDir(project dir) error = %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == filepath.Base(project.EnvFilePath) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(project.Dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", entry.Name(), err)
		}
		if strings.Contains(string(body), dbPassword) {
			t.Fatalf("sibling file %s contains the bundled Postgres password; the 0600 env file must be the sole channel", entry.Name())
		}
	}
}

func TestWriteOwnerOnlyFileTightensWiderExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regixtry.env")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOwnerOnlyFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v, want 0600", info.Mode().Perm(), err)
	}
}

func TestWriteOwnerOnlyFileFailureKeepsOldFileAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regixtry.env")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	defer func() { renameFile = previous }()

	if err := writeOwnerOnlyFile(path, []byte("new")); err == nil {
		t.Fatal("writeOwnerOnlyFile() error = nil, want injected failure")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "old" {
		t.Fatalf("old env file = %q, %v, want intact", body, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir entries = %d, want only the original file (temp removed)", len(entries))
	}
}

func TestWriteOwnerOnlyFileReplacesSymlinkInsteadOfFollowing(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "regixtry.env")
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if err := writeOwnerOnlyFile(path, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(victim); string(body) != "keep" {
		t.Fatalf("symlink target modified: %q", body)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("env path = %v, %v, want regular 0600 file", info.Mode(), err)
	}
}

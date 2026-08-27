package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestProjectConfig(dir string) ProjectConfig {
	return ProjectConfig{
		Dir:   dir,
		Name:  "regixtry",
		Image: "ghcr.io/desatatufuria/regixtry:latest",
		Port:  "5000",
	}
}

// TestWriteProjectRefusesPreExistingProjectDirectory is the RED test for
// tasks.md 2.5: a compose project of that name with existing containers is
// refused, never adopted (design.md Interfaces / Contracts).
func TestWriteProjectRefusesPreExistingProjectDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	p := NewProvisioner(ProvisionerConfig{})
	_, err := p.WriteProject(newTestProjectConfig(dir))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("WriteProject() error = %v, want refusal of a pre-existing project directory", err)
	}
}

// TestWriteProjectBuildsPathsWithFilepathJoinAndPreservesLiteralValues is
// the second half of tasks.md 2.5: project/env-file paths are built only
// via filepath.Join, and operator-supplied strings containing `;`, `$(...)`,
// spaces, or a leading `-` reach the written files as one literal value --
// nothing here ever goes through a shell.
func TestWriteProjectBuildsPathsWithFilepathJoinAndPreservesLiteralValues(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "project")
	weirdImage := "ghcr.io/desatatufuria/regixtry:latest; rm -rf / $(whoami) -x"
	weirdPublicURL := "http://127.0.0.1:5000; echo pwned"

	p := NewProvisioner(ProvisionerConfig{})
	cfg := newTestProjectConfig(dir)
	cfg.Image = weirdImage
	cfg.PublicURL = weirdPublicURL
	project, err := p.WriteProject(cfg)
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}

	if project.ComposeFilePath != filepath.Join(dir, "docker-compose.yml") {
		t.Fatalf("ComposeFilePath = %q, want filepath.Join(dir, %q)", project.ComposeFilePath, "docker-compose.yml")
	}
	if project.EnvFilePath != filepath.Join(dir, "regixtry.env") {
		t.Fatalf("EnvFilePath = %q, want filepath.Join(dir, %q)", project.EnvFilePath, "regixtry.env")
	}
	if project.Image != weirdImage {
		t.Fatalf("Image = %q, want the literal operator-supplied value untouched by any shell", project.Image)
	}
	if project.PublicURL != weirdPublicURL {
		t.Fatalf("PublicURL = %q, want the literal operator-supplied value untouched by any shell", project.PublicURL)
	}

	envBody, err := os.ReadFile(project.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	if !strings.Contains(string(envBody), "REGIXTRY_IMAGE="+weirdImage) {
		t.Fatalf("env file = %q, want the literal image value written verbatim, not shell-interpreted", envBody)
	}
}

// TestWriteProjectGeneratesPasswordAbsentFromComposeAndEnvKeys is the RED
// test for tasks.md 2.6: the generated crypto/rand bundled password must be
// absent from the copied compose bytes and from the env file's own key
// names, and the env file must be written with mode 0600. Triangulated by
// asserting a second WriteProject call produces an independently generated
// (different) password.
func TestWriteProjectGeneratesPasswordAbsentFromComposeAndEnvKeys(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(ProvisionerConfig{})

	dir1 := filepath.Join(t.TempDir(), "project")
	project1, err := p.WriteProject(newTestProjectConfig(dir1))
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}

	composeBody, err := os.ReadFile(project1.ComposeFilePath)
	if err != nil {
		t.Fatalf("ReadFile(compose) error = %v", err)
	}
	envBody, err := os.ReadFile(project1.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}

	password1 := extractEnvValue(t, string(envBody), "REGIXTRY_POSTGRES_PASSWORD")
	if password1 == "" {
		t.Fatalf("REGIXTRY_POSTGRES_PASSWORD was empty, want a generated crypto/rand password")
	}
	if strings.Contains(string(composeBody), password1) {
		t.Fatalf("compose file contains the generated password; it must only ever carry ${...} interpolation")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(envBody)), "\n") {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.Contains(key, password1) {
			t.Fatalf("env file key %q contains the generated password", key)
		}
	}

	info, err := os.Stat(project1.EnvFilePath)
	if err != nil {
		t.Fatalf("Stat(env) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("env file mode = %v, want 0600", info.Mode().Perm())
	}

	dir2 := filepath.Join(t.TempDir(), "project2")
	project2, err := p.WriteProject(newTestProjectConfig(dir2))
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}
	envBody2, err := os.ReadFile(project2.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env2) error = %v", err)
	}
	password2 := extractEnvValue(t, string(envBody2), "REGIXTRY_POSTGRES_PASSWORD")
	if password2 == "" {
		t.Fatalf("second REGIXTRY_POSTGRES_PASSWORD was empty, want a generated crypto/rand password")
	}
	if password2 == password1 {
		t.Fatalf("two WriteProject calls produced the identical password %q, want independent crypto/rand generation", password1)
	}
}

// TestWriteProjectBuildsBundledAuthDSNFromGeneratedPassword triangulates
// GREEN/2.7: with no ExternalPostgresDSN, WriteProject is bundled and
// assembles REGIXTRY_AUTH_POSTGRES_DSN from the generated password.
func TestWriteProjectBuildsBundledAuthDSNFromGeneratedPassword(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "project")
	p := NewProvisioner(ProvisionerConfig{})
	project, err := p.WriteProject(newTestProjectConfig(dir))
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}
	if !project.BundledPostgres {
		t.Fatalf("BundledPostgres = false, want true when ExternalPostgresDSN is empty")
	}

	envBody, err := os.ReadFile(project.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	password := extractEnvValue(t, string(envBody), "REGIXTRY_POSTGRES_PASSWORD")
	dsn := extractEnvValue(t, string(envBody), "REGIXTRY_AUTH_POSTGRES_DSN")
	wantDSN := fmt.Sprintf("postgres://registry:%s@postgres:5432/regixtry_auth?sslmode=disable", password)
	if dsn != wantDSN {
		t.Fatalf("REGIXTRY_AUTH_POSTGRES_DSN = %q, want %q", dsn, wantDSN)
	}
}

// TestWriteProjectPassesThroughExternalDSNWithoutBundlingPostgres is the
// second triangulation case: a supplied ExternalPostgresDSN is external
// mode and is written through verbatim, never rebuilt from the generated
// password (design.md "Bundled vs external selection" decision).
func TestWriteProjectPassesThroughExternalDSNWithoutBundlingPostgres(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "project")
	externalDSN := "postgres://ops:s3cr3t@db.internal:5432/regixtry_auth?sslmode=require"
	p := NewProvisioner(ProvisionerConfig{})
	cfg := newTestProjectConfig(dir)
	cfg.ExternalPostgresDSN = externalDSN
	project, err := p.WriteProject(cfg)
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}
	if project.BundledPostgres {
		t.Fatalf("BundledPostgres = true, want false when ExternalPostgresDSN is set")
	}

	envBody, err := os.ReadFile(project.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	dsn := extractEnvValue(t, string(envBody), "REGIXTRY_AUTH_POSTGRES_DSN")
	if dsn != externalDSN {
		t.Fatalf("REGIXTRY_AUTH_POSTGRES_DSN = %q, want the external DSN passed through verbatim", dsn)
	}
}

// TestWriteProjectRequiresDir is a minimal validation-branch triangulation:
// an empty Dir is rejected before any filesystem write is attempted.
func TestWriteProjectRequiresDir(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(ProvisionerConfig{})
	cfg := newTestProjectConfig("")
	if _, err := p.WriteProject(cfg); err == nil {
		t.Fatalf("WriteProject() error = nil, want an error for an empty project directory")
	}
}

func extractEnvValue(t *testing.T, body string, key string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && k == key {
			return v
		}
	}
	return ""
}

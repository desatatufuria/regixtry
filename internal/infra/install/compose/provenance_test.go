package compose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveProvenanceWritesComposeStateFile is the RED test for tasks.md 6.4:
// regixtry-compose-state.json (0600) must contain mode: "docker", the
// compose project name/dir/file path, env file path, pinned image,
// service/volume names, bundled_postgres, and the public URL -- and it must
// use a filename distinct from regixtry-lifecycle-state.json (design.md
// "Docker provenance file" decision), so today's systemd `uninstall` never
// mistakes a compose stack for a systemd install.
func TestSaveProvenanceWritesComposeStateFile(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	proj.Dir = t.TempDir()
	proj.ComposeFilePath = filepath.Join(proj.Dir, "docker-compose.yml")
	proj.EnvFilePath = filepath.Join(proj.Dir, "regixtry.env")

	p := NewProvisioner(ProvisionerConfig{})
	if err := p.SaveProvenance(proj); err != nil {
		t.Fatalf("SaveProvenance() error = %v, want nil", err)
	}

	provenancePath := filepath.Join(proj.Dir, "regixtry-compose-state.json")
	if provenancePath == filepath.Join(proj.Dir, "regixtry-lifecycle-state.json") {
		t.Fatalf("provenance filename must not equal the systemd lifecycle provenance filename")
	}

	info, err := os.Stat(provenancePath)
	if err != nil {
		t.Fatalf("Stat(provenance) error = %v, want a regixtry-compose-state.json file", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("provenance file mode = %v, want 0600", info.Mode().Perm())
	}

	body, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatalf("ReadFile(provenance) error = %v", err)
	}

	var decoded Provenance
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(provenance) error = %v", err)
	}
	if decoded.Mode != "docker" {
		t.Fatalf("Mode = %q, want %q", decoded.Mode, "docker")
	}
	if decoded.ProjectName != proj.Name {
		t.Fatalf("ProjectName = %q, want %q", decoded.ProjectName, proj.Name)
	}
	if decoded.ProjectDir != proj.Dir {
		t.Fatalf("ProjectDir = %q, want %q", decoded.ProjectDir, proj.Dir)
	}
	if decoded.ComposeFilePath != proj.ComposeFilePath {
		t.Fatalf("ComposeFilePath = %q, want %q", decoded.ComposeFilePath, proj.ComposeFilePath)
	}
	if decoded.EnvFilePath != proj.EnvFilePath {
		t.Fatalf("EnvFilePath = %q, want %q", decoded.EnvFilePath, proj.EnvFilePath)
	}
	if decoded.Image != proj.Image {
		t.Fatalf("Image = %q, want %q", decoded.Image, proj.Image)
	}
	if len(decoded.ServiceNames) != len(proj.ServiceNames) {
		t.Fatalf("ServiceNames = %#v, want %#v", decoded.ServiceNames, proj.ServiceNames)
	}
	if len(decoded.VolumeNames) != len(proj.VolumeNames) {
		t.Fatalf("VolumeNames = %#v, want %#v", decoded.VolumeNames, proj.VolumeNames)
	}
	if !decoded.BundledPostgres {
		t.Fatalf("BundledPostgres = false, want true (proj was bundled)")
	}
	if decoded.PublicURL != proj.PublicURL {
		t.Fatalf("PublicURL = %q, want %q", decoded.PublicURL, proj.PublicURL)
	}
}

// TestSaveProvenanceContainsNoSecret triangulates 6.4: a project produced by
// WriteProject (which carries a real generated password internally, in the
// env file it wrote) must never leak that password through SaveProvenance --
// Project itself carries no password field, so this proves the absence
// structurally by round-tripping through the real WriteProject -> SaveProvenance
// flow, not just by inspecting a hand-built Project value.
func TestSaveProvenanceContainsNoSecret(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(ProvisionerConfig{})
	dir := filepath.Join(t.TempDir(), "project")
	proj, err := p.WriteProject(newTestProjectConfig(dir))
	if err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}

	envBody, err := os.ReadFile(proj.EnvFilePath)
	if err != nil {
		t.Fatalf("ReadFile(env) error = %v", err)
	}
	password := extractEnvValue(t, string(envBody), "REGIXTRY_POSTGRES_PASSWORD")
	if password == "" {
		t.Fatalf("generated password was empty, test setup is broken")
	}

	if err := p.SaveProvenance(proj); err != nil {
		t.Fatalf("SaveProvenance() error = %v, want nil", err)
	}

	provenanceBody, err := os.ReadFile(filepath.Join(proj.Dir, "regixtry-compose-state.json"))
	if err != nil {
		t.Fatalf("ReadFile(provenance) error = %v", err)
	}
	if strings.Contains(string(provenanceBody), password) {
		t.Fatalf("provenance file contains the generated bundled password; it must never appear there")
	}
}

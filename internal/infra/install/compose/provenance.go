package compose

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// composeProvenanceFileName is deliberately distinct from
// installlinux's "regixtry-lifecycle-state.json" (design.md "Docker
// provenance file" decision): normalizeLifecycleProvenance accepts any
// non-empty Mode, and uninstallWithProvenance proceeds to systemd teardown
// unconditionally, so reusing the systemd path would make a future
// `uninstall` run `systemctl stop` against a compose stack. This package
// never reads or writes regixtry-lifecycle-state.json.
const (
	composeProvenanceVersion  = 1
	composeProvenanceFileName = "regixtry-compose-state.json"
)

// Provenance is the on-disk shape of regixtry-compose-state.json
// (design.md Interfaces / Contracts): everything a later `docker compose
// down --volumes` needs to tear the stack down without re-deriving
// anything, and nothing secret -- Project carries no password field, so
// Provenance structurally cannot leak the generated bundled Postgres
// credential.
type Provenance struct {
	Version         int      `json:"version"`
	Mode            string   `json:"mode"`
	ProjectName     string   `json:"project_name"`
	ProjectDir      string   `json:"project_dir"`
	ComposeFilePath string   `json:"compose_file_path"`
	EnvFilePath     string   `json:"env_file_path"`
	Image           string   `json:"image"`
	ServiceNames    []string `json:"service_names"`
	VolumeNames     []string `json:"volume_names"`
	BundledPostgres bool     `json:"bundled_postgres"`
	PublicURL       string   `json:"public_url"`
}

func provenanceFromProject(proj Project) Provenance {
	return Provenance{
		Version:         composeProvenanceVersion,
		Mode:            "docker",
		ProjectName:     proj.Name,
		ProjectDir:      proj.Dir,
		ComposeFilePath: proj.ComposeFilePath,
		EnvFilePath:     proj.EnvFilePath,
		Image:           proj.Image,
		ServiceNames:    append([]string{}, proj.ServiceNames...),
		VolumeNames:     append([]string{}, proj.VolumeNames...),
		BundledPostgres: proj.BundledPostgres,
		PublicURL:       proj.PublicURL,
	}
}

// SaveProvenance writes regixtry-compose-state.json (0600) into the compose
// project directory (tasks.md 6.4/6.5). It never invokes a subprocess and
// never touches the systemd provenance path.
func (p *Provisioner) SaveProvenance(proj Project) error {
	provenance := provenanceFromProject(proj)
	body, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal compose provenance: %w", err)
	}

	path := filepath.Join(proj.Dir, composeProvenanceFileName)
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("write compose provenance: %w", err)
	}

	return nil
}

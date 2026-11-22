package compose

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectConfig is the caller-supplied intent for one `docker` setup-mode
// project (design.md Data Flow). ExternalPostgresDSN empty means bundled
// Postgres; non-empty means external -- Postgres selection itself is
// decided by the caller (PR #4's promptSetupDockerConfig), not by this
// package.
type ProjectConfig struct {
	Dir                 string
	Name                string
	Image               string
	Port                string
	PublicURL           string
	ExternalPostgresDSN string
}

// Project is the materialized result of WriteProject: everything the rest
// of the composeRunner interface (StartDatabase/BootstrapAdmin in PR #2,
// StartRegistry/WaitReachable/SaveProvenance/Down in PR #3) and provenance
// need to act on this project without re-deriving anything (design.md
// Interfaces / Contracts).
type Project struct {
	Name            string
	Dir             string
	ComposeFilePath string
	EnvFilePath     string
	Image           string
	ServiceNames    []string
	VolumeNames     []string
	BundledPostgres bool
	PublicURL       string
}

const (
	composeFileName     = "docker-compose.yml"
	envFileName         = "regixtry.env"
	bundledPostgresUser = "registry"
	bundledPostgresDB   = "regixtry_auth"
)

// composeServiceNames/composeVolumeNames mirror the embedded compose
// asset's service and volume names (assets/docker-compose.yml) -- kept as
// package-level data here rather than parsed from the asset, since the
// asset is a reviewed, drift-locked file, not a machine-generated one.
var (
	composeServiceNames = []string{"postgres", "regixtry"}
	composeVolumeNames  = []string{"postgres-data", "registry-data"}
)

// WriteProject materializes one compose project directory: a byte-identical
// copy of the embedded compose asset, plus a 0600 env file carrying a
// freshly generated bundled Postgres password and the resolved auth DSN.
// A pre-existing directory at cfg.Dir is refused, never adopted (design.md
// "Filesystem target selection" threat). Every path is built with
// filepath.Join; every value (image, public URL, DSN) is written to the
// files verbatim -- WriteProject never invokes a subprocess, so nothing
// here can be shell-interpreted.
func (p *Provisioner) WriteProject(cfg ProjectConfig) (Project, error) {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		return Project{}, errors.New("compose project directory is required")
	}

	if _, err := os.Stat(dir); err == nil {
		return Project{}, fmt.Errorf("a compose project already exists at %s; refusing to adopt it", dir)
	} else if !os.IsNotExist(err) {
		return Project{}, fmt.Errorf("stat compose project directory: %w", err)
	}

	password, err := generateBundledPassword()
	if err != nil {
		return Project{}, err
	}

	bundled := strings.TrimSpace(cfg.ExternalPostgresDSN) == ""
	authDSN := strings.TrimSpace(cfg.ExternalPostgresDSN)
	if bundled {
		authDSN = fmt.Sprintf("postgres://%s:%s@postgres:5432/%s?sslmode=disable", bundledPostgresUser, password, bundledPostgresDB)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Project{}, fmt.Errorf("create compose project directory: %w", err)
	}

	composeFilePath := filepath.Join(dir, composeFileName)
	if err := os.WriteFile(composeFilePath, ComposeAsset, 0o644); err != nil {
		return Project{}, fmt.Errorf("write compose project file: %w", err)
	}

	envFilePath := filepath.Join(dir, envFileName)
	envBody := renderEnvFile(envFileData{
		Image:            cfg.Image,
		Port:             cfg.Port,
		PostgresPassword: password,
		AuthPostgresDSN:  authDSN,
	})
	if err := writeOwnerOnlyFile(envFilePath, []byte(envBody)); err != nil {
		return Project{}, fmt.Errorf("write compose project env file: %w", err)
	}

	return Project{
		Name:            strings.TrimSpace(cfg.Name),
		Dir:             dir,
		ComposeFilePath: composeFilePath,
		EnvFilePath:     envFilePath,
		Image:           cfg.Image,
		ServiceNames:    append([]string{}, composeServiceNames...),
		VolumeNames:     append([]string{}, composeVolumeNames...),
		BundledPostgres: bundled,
		PublicURL:       cfg.PublicURL,
	}, nil
}

func generateBundledPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate bundled postgres password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// envFileData is the golden-text input for renderEnvFile, reused by PR #2's
// secret-channel tests (tasks.md 2.8 refactor note).
type envFileData struct {
	Image            string
	Port             string
	PostgresPassword string
	AuthPostgresDSN  string
}

// renderEnvFile is the single place that decides the on-disk env-file
// shape; WriteProject and future secret-channel golden tests both go
// through it so the format only needs to change in one place.
func renderEnvFile(data envFileData) string {
	lines := []string{
		fmt.Sprintf("REGIXTRY_IMAGE=%s", data.Image),
		fmt.Sprintf("REGIXTRY_PORT=%s", data.Port),
		fmt.Sprintf("REGIXTRY_POSTGRES_PASSWORD=%s", data.PostgresPassword),
		fmt.Sprintf("REGIXTRY_AUTH_POSTGRES_DSN=%s", data.AuthPostgresDSN),
	}
	return strings.Join(lines, "\n") + "\n"
}

// renameFile is the final step of writeOwnerOnlyFile; a variable so tests can
// inject a failure after the temp file exists.
var renameFile = os.Rename

// writeOwnerOnlyFile writes a secret-bearing file atomically with mode 0600
// from the moment it exists: the bytes go to a temp file in the same directory
// opened O_EXCL with 0600, are fsynced, then renamed over the target. A wider
// pre-existing file is replaced (not chmod-ed after the fact), a symlink at the
// target is replaced rather than followed, and the old file stays intact until
// the rename succeeds. The temp file is removed on any failure.
func writeOwnerOnlyFile(path string, body []byte) error {
	dir := filepath.Dir(path)
	for attempt := 0; attempt < 8; attempt++ {
		tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.%d.%d.tmp", filepath.Base(path), os.Getpid(), attempt, time.Now().UnixNano()))
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := f.Write(body)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = renameFile(tmp, path)
		}
		if werr != nil {
			_ = os.Remove(tmp)
			return werr
		}
		return nil
	}
	return fmt.Errorf("create temporary file next to %s: too many collisions", path)
}

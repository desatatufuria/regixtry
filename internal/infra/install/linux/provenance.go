package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	lifecycleProvenanceVersion  = 2
	lifecycleProvenanceFileName = "regixtry-lifecycle-state.json"
)

const (
	CleanupStatusRemoved = "removed"
	CleanupStatusMissing = "missing"
	CleanupStatusSkipped = "skipped"
	CleanupStatusFailed  = "failed"
)

type LifecycleProvenance struct {
	Version          int             `json:"version"`
	Mode             string          `json:"mode"`
	InstalledBin     string          `json:"installed_bin"`
	InstalledRef     string          `json:"installed_ref,omitempty"`
	InstalledVersion string          `json:"installed_version,omitempty"`
	ServiceName      string          `json:"service_name"`
	StatePath        string          `json:"state_path"`
	ManagedPaths     []string        `json:"managed_paths"`
	Intent           LifecycleIntent `json:"intent,omitempty"`
}

type LifecycleIntent struct {
	Addr                 string `json:"addr,omitempty"`
	PublicURL            string `json:"public_url,omitempty"`
	RuntimeTLSMode       string `json:"runtime_tls_mode,omitempty"`
	TLSCertFile          string `json:"tls_cert_file,omitempty"`
	TLSKeyFile           string `json:"tls_key_file,omitempty"`
	AuthPostgresDSN      string `json:"auth_postgres_dsn,omitempty"`
	StorageRoot          string `json:"storage_root,omitempty"`
	DatabasePath         string `json:"database_path,omitempty"`
	ContentPath          string `json:"content_path,omitempty"`
	BootstrapStatePath   string `json:"bootstrap_state_path,omitempty"`
	EnvPath              string `json:"env_path,omitempty"`
	UnitPath             string `json:"unit_path,omitempty"`
	BinaryPath           string `json:"binary_path,omitempty"`
	ServiceName          string `json:"service_name,omitempty"`
	TrivyEnabled         bool   `json:"trivy_enabled,omitempty"`
	TrivyScheduleEnabled bool   `json:"trivy_schedule_enabled,omitempty"`
	TrivyInterval        string `json:"trivy_interval,omitempty"`
	TrivyTimeout         string `json:"trivy_timeout,omitempty"`
	TrivyCacheDir        string `json:"trivy_cache_dir,omitempty"`
	TrivyBinaryPath      string `json:"trivy_binary_path,omitempty"`
	TrivyMaxConcurrency  int    `json:"trivy_max_concurrency,omitempty"`
}

type CleanupItem struct {
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type UninstallReport struct {
	Mode        string        `json:"mode"`
	ServiceName string        `json:"service_name"`
	Service     CleanupItem   `json:"service"`
	Items       []CleanupItem `json:"items"`
}

func lifecycleProvenancePath(bootstrapStatePath string) string {
	trimmed := strings.TrimSpace(bootstrapStatePath)
	if trimmed == "" {
		return lifecycleProvenanceFileName
	}
	return filepath.Join(filepath.Dir(trimmed), lifecycleProvenanceFileName)
}

func LifecycleProvenancePath(bootstrapStatePath string) string {
	return lifecycleProvenancePath(bootstrapStatePath)
}

func lifecycleProvenanceFromPlan(plan BootstrapPlan, receipt BootstrapReceipt) LifecycleProvenance {
	return LifecycleProvenance{
		Version:      lifecycleProvenanceVersion,
		Mode:         plan.Mode,
		InstalledBin: plan.BinaryPath,
		ServiceName:  plan.ServiceName,
		StatePath:    lifecycleProvenancePath(plan.StatePath),
		ManagedPaths: append([]string(nil), receipt.Paths...),
		Intent: LifecycleIntent{
			Addr:                 plan.Addr,
			PublicURL:            plan.PublicURL,
			RuntimeTLSMode:       plan.RuntimeTLSMode,
			TLSCertFile:          plan.TLSCertFile,
			TLSKeyFile:           plan.TLSKeyFile,
			AuthPostgresDSN:      plan.AuthPostgresDSN,
			StorageRoot:          plan.StorageRoot,
			DatabasePath:         plan.DatabasePath,
			ContentPath:          plan.ContentPath,
			BootstrapStatePath:   plan.StatePath,
			EnvPath:              plan.EnvPath,
			UnitPath:             plan.UnitPath,
			BinaryPath:           plan.BinaryPath,
			ServiceName:          plan.ServiceName,
			TrivyEnabled:         plan.TrivyEnabled,
			TrivyScheduleEnabled: plan.TrivyScheduleEnabled,
			TrivyInterval:        plan.TrivyInterval.String(),
			TrivyTimeout:         plan.TrivyTimeout.String(),
			TrivyCacheDir:        plan.TrivyCacheDir,
			TrivyBinaryPath:      plan.TrivyBinaryPath,
			TrivyMaxConcurrency:  plan.TrivyMaxConcurrency,
		},
	}
}

func (b *Bootstrapper) writeLifecycleProvenance(provenance LifecycleProvenance) error {
	normalized, err := normalizeLifecycleProvenance(provenance)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lifecycle provenance: %w", err)
	}
	if err := b.writeFile(normalized.StatePath, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write lifecycle provenance: %w", err)
	}
	return nil
}

func (b *Bootstrapper) SaveLifecycleProvenance(provenance LifecycleProvenance) error {
	return b.writeLifecycleProvenance(provenance)
}

func (b *Bootstrapper) readLifecycleProvenance(provenancePath string) (LifecycleProvenance, error) {
	body, err := b.readFile(provenancePath)
	if err != nil {
		return LifecycleProvenance{}, fmt.Errorf("read lifecycle provenance: %w", err)
	}

	var provenance LifecycleProvenance
	if err := json.Unmarshal(body, &provenance); err != nil {
		return LifecycleProvenance{}, fmt.Errorf("decode lifecycle provenance: %w", err)
	}
	if strings.TrimSpace(provenance.StatePath) == "" {
		provenance.StatePath = strings.TrimSpace(provenancePath)
	}

	return normalizeLifecycleProvenance(provenance)
}

func (b *Bootstrapper) PlanLifecycleProvenance(cfg BootstrapConfig) (LifecycleProvenance, error) {
	if err := ValidateConfig(cfg); err != nil {
		return LifecycleProvenance{}, err
	}

	_, _, provenance, err := b.plan(cfg)
	if err != nil {
		return LifecycleProvenance{}, err
	}

	return provenance, nil
}

func normalizeLifecycleProvenance(provenance LifecycleProvenance) (LifecycleProvenance, error) {
	provenance.Mode = strings.TrimSpace(provenance.Mode)
	provenance.InstalledBin = strings.TrimSpace(provenance.InstalledBin)
	provenance.InstalledRef = strings.TrimSpace(provenance.InstalledRef)
	provenance.InstalledVersion = strings.TrimSpace(provenance.InstalledVersion)
	provenance.ServiceName = strings.TrimSpace(provenance.ServiceName)
	provenance.StatePath = strings.TrimSpace(provenance.StatePath)
	provenance.ManagedPaths = normalizeCleanupPaths(provenance.ManagedPaths)
	provenance.Intent = normalizeLifecycleIntent(provenance.Intent)

	if provenance.Version == 0 {
		provenance.Version = 1
	}
	if provenance.Version != 1 && provenance.Version != lifecycleProvenanceVersion {
		return LifecycleProvenance{}, fmt.Errorf("unsupported lifecycle provenance version %d", provenance.Version)
	}
	if provenance.Mode == "" {
		return LifecycleProvenance{}, errors.New("lifecycle provenance is missing mode")
	}
	if provenance.ServiceName == "" {
		return LifecycleProvenance{}, errors.New("lifecycle provenance is missing service_name")
	}
	if provenance.StatePath == "" {
		return LifecycleProvenance{}, errors.New("lifecycle provenance is missing state_path")
	}

	return provenance, nil
}

func normalizeLifecycleIntent(intent LifecycleIntent) LifecycleIntent {
	intent.Addr = strings.TrimSpace(intent.Addr)
	intent.PublicURL = strings.TrimSpace(intent.PublicURL)
	intent.RuntimeTLSMode = strings.TrimSpace(intent.RuntimeTLSMode)
	intent.TLSCertFile = strings.TrimSpace(intent.TLSCertFile)
	intent.TLSKeyFile = strings.TrimSpace(intent.TLSKeyFile)
	intent.AuthPostgresDSN = strings.TrimSpace(intent.AuthPostgresDSN)
	intent.StorageRoot = strings.TrimSpace(intent.StorageRoot)
	intent.DatabasePath = strings.TrimSpace(intent.DatabasePath)
	intent.ContentPath = strings.TrimSpace(intent.ContentPath)
	intent.BootstrapStatePath = strings.TrimSpace(intent.BootstrapStatePath)
	intent.EnvPath = strings.TrimSpace(intent.EnvPath)
	intent.UnitPath = strings.TrimSpace(intent.UnitPath)
	intent.BinaryPath = strings.TrimSpace(intent.BinaryPath)
	intent.ServiceName = strings.TrimSpace(intent.ServiceName)
	intent.TrivyInterval = strings.TrimSpace(intent.TrivyInterval)
	intent.TrivyTimeout = strings.TrimSpace(intent.TrivyTimeout)
	intent.TrivyCacheDir = strings.TrimSpace(intent.TrivyCacheDir)
	intent.TrivyBinaryPath = strings.TrimSpace(intent.TrivyBinaryPath)
	return intent
}

func normalizeCleanupPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func uninstallCleanupTargets(provenance LifecycleProvenance) []string {
	ordered := make([]string, 0, len(provenance.ManagedPaths)+2)
	seen := make(map[string]struct{}, len(provenance.ManagedPaths)+2)
	installedBin := strings.TrimSpace(provenance.InstalledBin)

	appendUnique := func(path string) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			return
		}
		if trimmed == installedBin {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		ordered = append(ordered, trimmed)
	}

	for i := len(provenance.ManagedPaths) - 1; i >= 0; i-- {
		appendUnique(provenance.ManagedPaths[i])
	}
	appendUnique(provenance.StatePath)
	if installedBin != "" {
		ordered = append(ordered, installedBin)
	}

	return ordered
}

func (r UninstallReport) Format() string {
	lines := []string{"Uninstall report:"}
	if strings.TrimSpace(r.Service.Path) != "" {
		lines = append(lines, fmt.Sprintf("- service %s: %s (%s)", r.Service.Path, r.Service.Status, r.Service.Detail))
	}
	for _, item := range r.Items {
		line := fmt.Sprintf("- %s: %s", item.Path, item.Status)
		if strings.TrimSpace(item.Detail) != "" {
			line += " (" + item.Detail + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

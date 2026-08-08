package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	lifecycleProvenanceVersion  = 1
	lifecycleProvenanceFileName = "regixtry-lifecycle-state.json"
)

const (
	CleanupStatusRemoved = "removed"
	CleanupStatusMissing = "missing"
	CleanupStatusSkipped = "skipped"
	CleanupStatusFailed  = "failed"
)

type LifecycleProvenance struct {
	Version      int      `json:"version"`
	Mode         string   `json:"mode"`
	InstalledBin string   `json:"installed_bin"`
	ServiceName  string   `json:"service_name"`
	StatePath    string   `json:"state_path"`
	ManagedPaths []string `json:"managed_paths"`
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
	provenance.ServiceName = strings.TrimSpace(provenance.ServiceName)
	provenance.StatePath = strings.TrimSpace(provenance.StatePath)
	provenance.ManagedPaths = normalizeCleanupPaths(provenance.ManagedPaths)

	if provenance.Version == 0 {
		provenance.Version = lifecycleProvenanceVersion
	}
	if provenance.Version != lifecycleProvenanceVersion {
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

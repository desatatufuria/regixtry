package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"regixtry/internal/ports"
)

type execRunner func(ctx context.Context, binaryPath string, args ...string) ([]byte, error)

type RunnerConfig struct {
	Exec execRunner
}

type Runner struct {
	exec execRunner
}

func New(cfg RunnerConfig) *Runner {
	run := cfg.Exec
	if run == nil {
		run = func(ctx context.Context, binaryPath string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, binaryPath, args...)
			return cmd.Output()
		}
	}
	return &Runner{exec: run}
}

func (r *Runner) Probe(ctx context.Context, settings ports.ScanSettings) (ports.FeatureRuntime, error) {
	if r == nil || r.exec == nil {
		return ports.FeatureRuntime{}, fmt.Errorf("trivy runner is not configured")
	}
	info, err := r.fetchVersionInfo(ctx, settings)
	if err != nil {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}, err
	}
	if strings.TrimSpace(info.Version) == "" {
		return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Detail: "version response did not include Version", LastError: "version response did not include Version"}, fmt.Errorf("version response did not include Version")
	}
	return ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusReady), Health: string(ports.FeatureRuntimeStatusReady), Version: info.Version, ActiveBinaryPath: info.BinaryPath}, nil
}

func (r *Runner) Run(ctx context.Context, imageRef string, settings ports.ScanSettings) (ports.ScanResult, error) {
	if r == nil || r.exec == nil {
		return ports.ScanResult{}, fmt.Errorf("trivy runner is not configured")
	}
	// Pre-flight readability check (design.md Decision 6): performed before
	// exec is invoked at all, including the version probe, so a typo'd
	// override path fails the run instead of letting Trivy silently scan
	// with no suppressions (its documented fail-open behavior for a missing
	// ignore file).
	if err := requireReadableFile("ignore file", settings.IgnoreFilePath); err != nil {
		return ports.ScanResult{}, err
	}
	if err := requireReadableFile("ignore policy", settings.IgnorePolicyPath); err != nil {
		return ports.ScanResult{}, err
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	versionInfo, err := r.fetchVersionInfo(runCtx, settings)
	if err != nil {
		return ports.ScanResult{}, err
	}
	args := []string{"image", "--format", "json"}
	if cacheDir := strings.TrimSpace(settings.CacheDir); cacheDir != "" {
		args = append(args, "--cache-dir", cacheDir)
	}
	if ignoreFilePath := strings.TrimSpace(settings.IgnoreFilePath); ignoreFilePath != "" {
		args = append(args, "--ignorefile", ignoreFilePath)
	}
	if ignorePolicyPath := strings.TrimSpace(settings.IgnorePolicyPath); ignorePolicyPath != "" {
		args = append(args, "--ignore-policy", ignorePolicyPath)
	}
	args = append(args, strings.TrimSpace(imageRef))
	output, err := r.exec(runCtx, versionInfo.BinaryPath, args...)
	if err != nil {
		return ports.ScanResult{}, err
	}
	var payload trivyImagePayload
	if err := json.Unmarshal(output, &payload); err != nil {
		return ports.ScanResult{}, fmt.Errorf("decode trivy output: %w", err)
	}
	findings := make([]ports.ScanRunFinding, 0)
	result := ports.ScanResult{TrivyVersion: versionInfo.Version, DBUpdatedAt: payload.Metadata.DBUpdatedAt}
	result.DBFreshness = ports.ScanRunDBFreshness{
		ReportSchemaVersion: payload.SchemaVersion,
		ReportCreatedAt:     payload.CreatedAt,
		TrivyVersion:        versionInfo.Version,
		DBVersion:           versionInfo.DBFreshness.DBVersion,
		DBUpdatedAt:         firstNonNilTime(payload.Metadata.DBUpdatedAt, versionInfo.DBFreshness.DBUpdatedAt),
		DBDownloadedAt:      versionInfo.DBFreshness.DBDownloadedAt,
		DBNextUpdateAt:      versionInfo.DBFreshness.DBNextUpdateAt,
	}
	for _, section := range payload.Results {
		for _, vulnerability := range section.Vulnerabilities {
			severity := strings.ToUpper(strings.TrimSpace(vulnerability.Severity))
			switch severity {
			case "CRITICAL":
				result.Critical++
			case "HIGH":
				result.High++
			case "MEDIUM":
				result.Medium++
			case "LOW":
				result.Low++
			}
			findings = append(findings, ports.ScanRunFinding{
				Target:           strings.TrimSpace(section.Target),
				Class:            strings.TrimSpace(section.Class),
				Type:             strings.TrimSpace(section.Type),
				Severity:         severity,
				VulnerabilityID:  strings.TrimSpace(vulnerability.VulnerabilityID),
				PackageName:      strings.TrimSpace(vulnerability.PackageName),
				InstalledVersion: strings.TrimSpace(vulnerability.InstalledVersion),
				FixedVersion:     strings.TrimSpace(vulnerability.FixedVersion),
				Title:            strings.TrimSpace(vulnerability.Title),
				PrimaryURL:       strings.TrimSpace(vulnerability.PrimaryURL),
				Fixable:          strings.TrimSpace(vulnerability.FixedVersion) != "",
				Status:           strings.TrimSpace(vulnerability.Status),
				DataSource:       strings.TrimSpace(vulnerability.DataSource.Name),
				DataSourceURL:    strings.TrimSpace(vulnerability.DataSource.URL),
				PublishedAt:      vulnerability.PublishedDate,
				ModifiedAt:       vulnerability.LastModifiedDate,
			})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return compareFindings(findings[i], findings[j]) < 0
	})
	result.Findings = findings
	result.DBFreshness.FreshnessState = deriveDBFreshnessState(result.DBFreshness)
	result.DBUpdatedAt = result.DBFreshness.DBUpdatedAt
	return result, nil
}

type trivyVersionPayload struct {
	Version         string `json:"Version"`
	VulnerabilityDB struct {
		Version      int        `json:"Version"`
		UpdatedAt    *time.Time `json:"UpdatedAt"`
		NextUpdate   *time.Time `json:"NextUpdate"`
		DownloadedAt *time.Time `json:"DownloadedAt"`
	} `json:"VulnerabilityDB"`
}

type trivyImagePayload struct {
	SchemaVersion int        `json:"SchemaVersion"`
	CreatedAt     *time.Time `json:"CreatedAt"`
	Metadata      struct {
		DBUpdatedAt *time.Time `json:"DBUpdatedAt"`
	} `json:"Metadata"`
	Results []struct {
		Target          string `json:"Target"`
		Class           string `json:"Class"`
		Type            string `json:"Type"`
		Vulnerabilities []struct {
			VulnerabilityID  string     `json:"VulnerabilityID"`
			PackageName      string     `json:"PkgName"`
			InstalledVersion string     `json:"InstalledVersion"`
			FixedVersion     string     `json:"FixedVersion"`
			Title            string     `json:"Title"`
			PrimaryURL       string     `json:"PrimaryURL"`
			Severity         string     `json:"Severity"`
			Status           string     `json:"Status"`
			PublishedDate    *time.Time `json:"PublishedDate"`
			LastModifiedDate *time.Time `json:"LastModifiedDate"`
			DataSource       struct {
				Name string `json:"Name"`
				URL  string `json:"URL"`
			} `json:"DataSource"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

type trivyVersionInfo struct {
	Version     string
	BinaryPath  string
	DBFreshness ports.ScanRunDBFreshness
}

func (r *Runner) fetchVersionInfo(ctx context.Context, settings ports.ScanSettings) (trivyVersionInfo, error) {
	binaryPath, err := managedBinaryPath(settings.BinaryPath)
	if err != nil {
		return trivyVersionInfo{}, err
	}
	output, err := r.exec(ctx, binaryPath, "version", "--format", "json")
	if err != nil {
		return trivyVersionInfo{}, err
	}
	var payload trivyVersionPayload
	if err := json.Unmarshal(output, &payload); err != nil {
		return trivyVersionInfo{}, fmt.Errorf("decode version response: %w", err)
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return trivyVersionInfo{}, fmt.Errorf("version response did not include Version")
	}
	return trivyVersionInfo{
		Version:    version,
		BinaryPath: binaryPath,
		DBFreshness: ports.ScanRunDBFreshness{
			TrivyVersion:   version,
			DBVersion:      payload.VulnerabilityDB.Version,
			DBUpdatedAt:    payload.VulnerabilityDB.UpdatedAt,
			DBDownloadedAt: payload.VulnerabilityDB.DownloadedAt,
			DBNextUpdateAt: payload.VulnerabilityDB.NextUpdate,
		},
	}, nil
}

func firstNonNilTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func deriveDBFreshnessState(freshness ports.ScanRunDBFreshness) string {
	if freshness.ReportCreatedAt != nil && freshness.DBNextUpdateAt != nil && freshness.ReportCreatedAt.After(*freshness.DBNextUpdateAt) {
		return ports.ScanRunDBFreshnessStateStale
	}
	if freshness.ReportCreatedAt != nil && freshness.DBUpdatedAt != nil && freshness.ReportCreatedAt.Sub(*freshness.DBUpdatedAt) > 7*24*time.Hour {
		return ports.ScanRunDBFreshnessStateStale
	}
	if freshness.DBUpdatedAt != nil || freshness.DBVersion > 0 {
		return ports.ScanRunDBFreshnessStateFresh
	}
	return ports.ScanRunDBFreshnessStateUnknown
}

func compareFindings(left ports.ScanRunFinding, right ports.ScanRunFinding) int {
	leftSeverity := severityRank(left.Severity)
	rightSeverity := severityRank(right.Severity)
	if leftSeverity != rightSeverity {
		return rightSeverity - leftSeverity
	}
	if left.Fixable != right.Fixable {
		if left.Fixable {
			return -1
		}
		return 1
	}
	if compare := strings.Compare(left.VulnerabilityID, right.VulnerabilityID); compare != 0 {
		return compare
	}
	return strings.Compare(left.PackageName, right.PackageName)
}

func severityRank(severity string) int {
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case "CRITICAL":
		return 4
	case "HIGH":
		return 3
	case "MEDIUM":
		return 2
	case "LOW":
		return 1
	default:
		return 0
	}
}

// requireReadableFile is the pre-flight readability check from design.md
// Decision 6: an empty path is not configured and is skipped; a non-empty
// path is opened (not merely `os.Stat`-ed) to prove it is actually readable,
// not just present, before argv construction ever runs.
func requireReadableFile(label string, path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	file, err := os.Open(trimmed)
	if err != nil {
		return fmt.Errorf("%s is not readable: %w", label, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return fmt.Errorf("%s is not a regular file", label)
	}
	return nil
}

func managedBinaryPath(binaryPath string) (string, error) {
	trimmed := strings.TrimSpace(binaryPath)
	if trimmed == "" {
		return "", fmt.Errorf("managed trivy runtime is not installed")
	}
	clean := filepath.Clean(trimmed)
	if !strings.Contains(clean, string(filepath.Separator)+"features"+string(filepath.Separator)+"trivy"+string(filepath.Separator)) {
		return "", fmt.Errorf("managed trivy runtime must execute only from the Regixtry-owned features/trivy layout")
	}
	return clean, nil
}

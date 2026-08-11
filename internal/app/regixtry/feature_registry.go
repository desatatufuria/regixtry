package regixtry

import (
	"context"
	"fmt"
	"strings"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const trivyFeatureName = "trivy"

// gitleaksFeatureName is regixtry's own identity for the gitleaks feature,
// distinct from (but equal in value to) internal/infra/scanning/gitleaks's
// own package-private gitleaksFeatureName constant used for its runtime
// state row key. Both must stay "gitleaks" since they key the same
// feature_runtime_state/scan_settings rows.
const gitleaksFeatureName = "gitleaks"

type featureDescriptor struct {
	name string
	kind ports.FeatureKind
}

var builtInFeatures = []featureDescriptor{
	{name: trivyFeatureName, kind: ports.FeatureKindBuiltin},
	{name: gitleaksFeatureName, kind: ports.FeatureKindBuiltin},
}

type trivyRuntimeProber interface {
	Probe(context.Context, ports.ScanSettings) (ports.FeatureRuntime, error)
}

func lookupFeature(name string) (featureDescriptor, error) {
	trimmed := strings.TrimSpace(name)
	for _, feature := range builtInFeatures {
		if feature.name == trimmed {
			return feature, nil
		}
	}
	return featureDescriptor{}, domain.NewValidationError("unsupported feature \"" + trimmed + "\"")
}

func ValidateFeatureName(name string) error {
	_, err := lookupFeature(name)
	return err
}

func (s *Service) ListFeatures(ctx context.Context) ([]ports.FeatureSummary, error) {
	summaries := make([]ports.FeatureSummary, 0, len(builtInFeatures))
	for _, feature := range builtInFeatures {
		details, err := s.GetFeature(ctx, feature.name)
		if err != nil {
			return nil, err
		}
		runtime := s.projectFeatureRuntime(ctx, feature.name)
		summaries = append(summaries, featureSummaryFromDetails(details, runtime))
	}
	return summaries, nil
}

func featureRuntimeValueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return strings.TrimSpace(value)
}

func (s *Service) GetFeature(ctx context.Context, name string) (ports.FeatureDetails, error) {
	feature, settings, configured, err := s.loadFeatureSettings(ctx, name)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	return featureDetailsFromSettings(feature, settings, configured), nil
}

func (s *Service) GetFeatureStatus(ctx context.Context, name string) (ports.FeatureDetails, error) {
	feature, settings, configured, err := s.loadFeatureSettings(ctx, name)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	details := featureDetailsFromSettings(feature, settings, configured)
	details.Runtime = s.projectFeatureRuntime(ctx, feature.name)
	return details, nil
}

func (s *Service) GetFeaturePage(ctx context.Context, name string) (ports.FeaturePage, error) {
	details, err := s.GetFeatureStatus(ctx, name)
	if err != nil {
		return ports.FeaturePage{}, err
	}
	return buildFeaturePage(featureSummaryFromDetails(details, details.Runtime), details, nil), nil
}

func (s *Service) ExecuteFeatureAction(ctx context.Context, name string, actionID string) (ports.FeatureActionResult, error) {
	actionID = strings.TrimSpace(actionID)
	if err := ValidateFeatureName(name); err != nil {
		return ports.FeatureActionResult{}, err
	}
	switch actionID {
	case "enable":
		if _, err := s.SetFeatureEnabled(ctx, name, true); err != nil {
			return ports.FeatureActionResult{}, err
		}
		return ports.FeatureActionResult{Message: fmt.Sprintf("Feature %q enabled.", name)}, nil
	case "disable":
		if _, err := s.SetFeatureEnabled(ctx, name, false); err != nil {
			return ports.FeatureActionResult{}, err
		}
		return ports.FeatureActionResult{Message: fmt.Sprintf("Feature %q disabled.", name)}, nil
	case "install-runtime":
		state, err := s.InstallFeatureRuntime(ctx, name, "")
		if err != nil {
			return ports.FeatureActionResult{}, err
		}
		return ports.FeatureActionResult{Message: fmt.Sprintf("Managed runtime installed for %q at %s.", name, featureRuntimeValueOrUnknown(state.ActiveVersion))}, nil
	case "upgrade-runtime":
		state, err := s.UpgradeFeatureRuntime(ctx, name, "")
		if err != nil {
			return ports.FeatureActionResult{}, err
		}
		return ports.FeatureActionResult{Message: fmt.Sprintf("Managed runtime upgraded for %q at %s.", name, featureRuntimeValueOrUnknown(state.ActiveVersion))}, nil
	case "rollback-runtime":
		state, err := s.RollbackFeatureRuntime(ctx, name)
		if err != nil {
			return ports.FeatureActionResult{}, err
		}
		return ports.FeatureActionResult{Message: fmt.Sprintf("Managed runtime rolled back for %q to %s.", name, featureRuntimeValueOrUnknown(state.ActiveVersion))}, nil
	default:
		return ports.FeatureActionResult{}, domain.NewValidationError(fmt.Sprintf("unsupported feature action %q", actionID))
	}
}

func (s *Service) projectFeatureRuntime(ctx context.Context, feature string) ports.FeatureRuntime {
	var (
		state   ports.FeatureRuntimeState
		err     error
		runtime ports.FeatureRuntime
	)
	manager := s.runtimes[feature]
	if manager != nil {
		state, err = manager.Status(ctx)
	} else {
		state, err = s.metadata.GetFeatureRuntimeState(ctx, s.tenant(ctx), feature)
	}
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			runtime = ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusUninstalled), Health: string(ports.FeatureRuntimeStatusUninstalled), Detail: "managed runtime is not installed"}
		} else {
			runtime = ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusDegraded), Health: string(ports.FeatureRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}
		}
	} else {
		health := string(state.Status)
		if health == "" {
			health = string(ports.FeatureRuntimeStatusUninstalled)
		}
		detail := strings.TrimSpace(state.MigrationHint)
		if detail == "" {
			detail = strings.TrimSpace(state.LastError)
		}
		runtime = ports.FeatureRuntime{
			Mode:              ports.FeatureRuntimeModeManaged,
			Status:            string(state.Status),
			Health:            health,
			Version:           strings.TrimSpace(state.ActiveVersion),
			Detail:            detail,
			RollbackAvailable: strings.TrimSpace(state.PreviousVersion) != "",
			ActiveBinaryPath:  strings.TrimSpace(state.ActiveBinaryPath),
			ReceiptPath:       strings.TrimSpace(state.ReceiptPath),
			LastVerifiedAt:    state.LastVerifiedAt,
			LastHealthCheckAt: state.LastHealthCheckAt,
			LastDBUpdatedAt:   state.LastDBUpdatedAt,
			LastError:         strings.TrimSpace(state.LastError),
		}
	}
	runtime.LatestVersion = "unknown"
	runtime.UpdateStatus = "unknown"
	if manager != nil {
		latest, latestErr := manager.LatestVersion(ctx)
		if trimmed := strings.TrimSpace(latest); latestErr == nil && trimmed != "" {
			runtime.LatestVersion = trimmed
			switch current := strings.TrimSpace(runtime.Version); {
			case current == "":
				runtime.UpdateStatus = "unknown"
			case current == trimmed:
				runtime.UpdateStatus = "up-to-date"
			default:
				runtime.UpdateStatus = "available"
			}
		}
	}
	return runtime
}

func (s *Service) ConfigureFeature(ctx context.Context, name string, input ports.FeatureConfigureInput) (ports.FeatureDetails, error) {
	feature, settings, configured, err := s.loadFeatureSettings(ctx, name)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	merged, err := s.mergeFeatureSettings(settings, configured, input)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	normalized, err := s.normalizeScanSettings(merged)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	if err := s.metadata.UpsertScanSettings(ctx, s.tenant(ctx), feature.name, normalized); err != nil {
		return ports.FeatureDetails{}, err
	}
	return featureDetailsFromSettings(feature, normalized, true), nil
}

func (s *Service) SetFeatureEnabled(ctx context.Context, name string, enabled bool) (ports.FeatureDetails, error) {
	return s.ConfigureFeature(ctx, name, ports.FeatureConfigureInput{Enabled: &enabled})
}

func (s *Service) ImportLegacyFeatureConfigIfMissing(ctx context.Context, name string, input ports.FeatureConfigureInput) (ports.FeatureDetails, error) {
	feature, settings, configured, err := s.loadFeatureSettings(ctx, name)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	if configured {
		return featureDetailsFromSettings(feature, settings, true), nil
	}
	return s.ConfigureFeature(ctx, name, ports.FeatureConfigureInput{
		Enabled:         input.Enabled,
		ScheduleEnabled: input.ScheduleEnabled,
		Interval:        input.Interval,
		Timeout:         input.Timeout,
		MaxConcurrency:  input.MaxConcurrency,
	})
}

func (s *Service) loadFeatureSettings(ctx context.Context, name string) (featureDescriptor, ports.ScanSettings, bool, error) {
	feature, err := lookupFeature(name)
	if err != nil {
		return featureDescriptor{}, ports.ScanSettings{}, false, err
	}
	settings, err := s.metadata.GetScanSettings(ctx, s.tenant(ctx), feature.name)
	if err == nil {
		return feature, settings, true, nil
	}
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return featureDescriptor{}, ports.ScanSettings{}, false, err
	}
	return feature, ports.ScanSettings{
		Enabled:         false,
		ScheduleEnabled: false,
		Interval:        24 * time.Hour,
		Timeout:         15 * time.Minute,
		MaxConcurrency:  1,
	}, false, nil
}

func (s *Service) mergeFeatureSettings(base ports.ScanSettings, configured bool, input ports.FeatureConfigureInput) (ports.ScanSettings, error) {
	settings := base
	if !configured {
		settings.UpdatedAt = time.Time{}
	}
	if input.Enabled != nil {
		settings.Enabled = *input.Enabled
	}
	if input.ScheduleEnabled != nil {
		settings.ScheduleEnabled = *input.ScheduleEnabled
	}
	if input.Interval != nil {
		settings.Interval = *input.Interval
	}
	if input.Timeout != nil {
		settings.Timeout = *input.Timeout
	}
	if input.ServiceURL != nil {
		settings.ServiceURL = *input.ServiceURL
	}
	if input.RegistryReachableURL != nil {
		settings.RegistryReachableURL = *input.RegistryReachableURL
	}
	if input.AuthToken != nil {
		settings.AuthToken = *input.AuthToken
	}
	if input.TLSCACertPath != nil {
		settings.TLSCACertPath = *input.TLSCACertPath
	}
	if input.TLSInsecureSkipVerify != nil {
		settings.TLSInsecureSkipVerify = *input.TLSInsecureSkipVerify
	}
	if input.MaxConcurrency != nil {
		settings.MaxConcurrency = *input.MaxConcurrency
	}
	return settings, nil
}

func featureDetailsFromSettings(feature featureDescriptor, settings ports.ScanSettings, configured bool) ports.FeatureDetails {
	return ports.FeatureDetails{
		Name:                 feature.name,
		Kind:                 feature.kind,
		Enabled:              settings.Enabled,
		Configured:           configured,
		ScheduleEnabled:      settings.ScheduleEnabled,
		Interval:             settings.Interval,
		Timeout:              settings.Timeout,
		RegistryReachableURL: settings.RegistryReachableURL,
		MaxConcurrency:       settings.MaxConcurrency,
	}
}

func featureSummaryFromDetails(details ports.FeatureDetails, runtime ports.FeatureRuntime) ports.FeatureSummary {
	return ports.FeatureSummary{
		Name:           details.Name,
		Kind:           details.Kind,
		Enabled:        details.Enabled,
		Configured:     details.Configured,
		CurrentVersion: strings.TrimSpace(runtime.Version),
		LatestVersion:  featureRuntimeValueOrUnknown(runtime.LatestVersion),
		UpdateStatus:   featureRuntimeValueOrUnknown(runtime.UpdateStatus),
	}
}

func buildFeaturePage(summary ports.FeatureSummary, details ports.FeatureDetails, runs []ports.ScanRun) ports.FeaturePage {
	page := ports.FeaturePage{
		Summary: summary,
		Header: []ports.FeatureField{
			{Label: "Kind", Value: string(summary.Kind)},
			{Label: "Enabled", Value: fmt.Sprintf("%t", summary.Enabled)},
			{Label: "Configured", Value: fmt.Sprintf("%t", summary.Configured)},
		},
	}
	// Config+Runtime sections are generic to any built-in managed feature
	// (Trivy and Gitleaks alike): both are driven entirely by the
	// feature-keyed ScanSettings/FeatureRuntime data Phase 2 made
	// (tenant, feature) scoped. Non-builtin feature kinds (e.g. a future
	// external-service plugin) stay on the lightweight minimal page, since
	// they are not guaranteed to have a managed runtime at all.
	if summary.Kind != ports.FeatureKindBuiltin {
		return page
	}
	page.Sections = []ports.FeatureSection{
		{
			ID:    "config",
			Title: "Configuration",
			Kind:  "fields",
			Fields: []ports.FeatureField{
				{Label: "Schedule Enabled", Value: fmt.Sprintf("%t", details.ScheduleEnabled)},
				{Label: "Interval", Value: details.Interval.String()},
				{Label: "Timeout", Value: details.Timeout.String()},
				{Label: "Registry Reachable URL", Value: details.RegistryReachableURL},
				{Label: "Max Concurrency", Value: fmt.Sprintf("%d", details.MaxConcurrency)},
			},
		},
		{
			ID:    "runtime",
			Title: "Runtime",
			Kind:  "fields",
			Fields: []ports.FeatureField{
				{Label: "Status", Value: featureRuntimeValueOrUnknown(details.Runtime.Status)},
				{Label: "Health", Value: featureRuntimeValueOrUnknown(details.Runtime.Health)},
				{Label: "Version", Value: featureRuntimeValueOrUnknown(details.Runtime.Version)},
				{Label: "Latest Version", Value: featureRuntimeValueOrUnknown(details.Runtime.LatestVersion)},
				{Label: "Update Status", Value: featureRuntimeValueOrUnknown(details.Runtime.UpdateStatus)},
				{Label: "Rollback Available", Value: fmt.Sprintf("%t", details.Runtime.RollbackAvailable)},
			},
		},
	}
	page.Actions = buildFeatureActions(details)
	return page
}

func buildFeatureRunRows(runs []ports.ScanRun) []ports.FeatureRow {
	if len(runs) == 0 {
		return []ports.FeatureRow{{Title: "No scan runs recorded yet.", Status: "empty"}}
	}
	rows := make([]ports.FeatureRow, 0, len(runs))
	for _, run := range runs {
		detail := fmt.Sprintf("critical=%d high=%d medium=%d low=%d", run.Critical, run.High, run.Medium, run.Low)
		if strings.TrimSpace(run.Error) != "" {
			detail = run.Error
		}
		rows = append(rows, ports.FeatureRow{
			Title:  fmt.Sprintf("%s@%s", run.Repository, run.RequestedRef),
			Status: run.Status,
			Detail: detail,
		})
	}
	return rows
}

func buildFeatureVulnerabilityFields(runs []ports.ScanRun) []ports.FeatureField {
	for _, run := range runs {
		if run.Status != ports.ScanRunStatusCompleted {
			continue
		}
		return []ports.FeatureField{
			{Label: "Critical", Value: fmt.Sprintf("%d", run.Critical)},
			{Label: "High", Value: fmt.Sprintf("%d", run.High)},
			{Label: "Medium", Value: fmt.Sprintf("%d", run.Medium)},
			{Label: "Low", Value: fmt.Sprintf("%d", run.Low)},
		}
	}
	return []ports.FeatureField{{Label: "Status", Value: "No completed scan runs available."}}
}

func buildFeatureRepositoryAlertRows(runs []ports.ScanRun) []ports.FeatureRow {
	rows := make([]ports.FeatureRow, 0, len(runs))
	for _, run := range runs {
		if run.Status == ports.ScanRunStatusFailed || run.Critical > 0 || run.High > 0 {
			detail := fmt.Sprintf("critical=%d high=%d medium=%d low=%d", run.Critical, run.High, run.Medium, run.Low)
			if strings.TrimSpace(run.Error) != "" {
				detail = run.Error
			}
			rows = append(rows, ports.FeatureRow{Title: fmt.Sprintf("%s@%s", run.Repository, run.RequestedRef), Status: run.Status, Detail: detail})
		}
	}
	if len(rows) == 0 {
		return []ports.FeatureRow{{Title: "No repository alerts detected.", Status: "clear"}}
	}
	return rows
}

func buildFeatureActions(details ports.FeatureDetails) []ports.FeatureAction {
	actions := []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}}
	if details.Enabled {
		actions = append(actions, ports.FeatureAction{ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: fmt.Sprintf("Confirm disable feature %q?", details.Name)})
	} else {
		actions = append(actions, ports.FeatureAction{ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: fmt.Sprintf("Confirm enable feature %q?", details.Name)})
	}
	status := strings.TrimSpace(details.Runtime.Status)
	if status == "" {
		status = strings.TrimSpace(details.Runtime.Health)
	}
	if details.Runtime.Mode == ports.FeatureRuntimeModeManaged && (status == string(ports.FeatureRuntimeStatusUninstalled) || status == string(ports.FeatureRuntimeStatusMigrationRequired)) {
		actions = append(actions, ports.FeatureAction{ID: "install-runtime", Label: "Install Runtime"})
	}
	if details.Runtime.Mode == ports.FeatureRuntimeModeManaged && strings.TrimSpace(details.Runtime.Version) != "" && strings.TrimSpace(details.Runtime.UpdateStatus) == "available" {
		actions = append(actions, ports.FeatureAction{ID: "upgrade-runtime", Label: "Upgrade Runtime"})
	}
	if details.Runtime.Mode == ports.FeatureRuntimeModeManaged && details.Runtime.RollbackAvailable {
		actions = append(actions, ports.FeatureAction{ID: "rollback-runtime", Label: "Rollback Runtime"})
	}
	return actions
}

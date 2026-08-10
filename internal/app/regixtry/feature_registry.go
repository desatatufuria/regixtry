package regixtry

import (
	"context"
	"strings"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const trivyFeatureName = "trivy"

type featureDescriptor struct {
	name string
	kind ports.FeatureKind
}

var builtInFeatures = []featureDescriptor{{name: trivyFeatureName, kind: ports.FeatureKindBuiltin}}

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
		runtime := s.projectFeatureRuntime(ctx)
		summaries = append(summaries, ports.FeatureSummary{
			Name:           details.Name,
			Kind:           details.Kind,
			Enabled:        details.Enabled,
			Configured:     details.Configured,
			CurrentVersion: strings.TrimSpace(runtime.Version),
			LatestVersion:  featureRuntimeValueOrUnknown(runtime.LatestVersion),
			UpdateStatus:   featureRuntimeValueOrUnknown(runtime.UpdateStatus),
		})
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
	details.Runtime = s.projectFeatureRuntime(ctx)
	return details, nil
}

func (s *Service) projectFeatureRuntime(ctx context.Context) ports.FeatureRuntime {
	var (
		state   ports.TrivyRuntimeState
		err     error
		runtime ports.FeatureRuntime
	)
	if s.runtime != nil {
		state, err = s.runtime.Status(ctx)
	} else {
		state, err = s.metadata.GetTrivyRuntimeState(ctx, s.tenant(ctx))
	}
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			runtime = ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusUninstalled), Health: string(ports.TrivyRuntimeStatusUninstalled), Detail: "managed runtime is not installed"}
		} else {
			runtime = ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.TrivyRuntimeStatusDegraded), Health: string(ports.TrivyRuntimeStatusDegraded), Detail: err.Error(), LastError: err.Error()}
		}
	} else {
		health := string(state.Status)
		if health == "" {
			health = string(ports.TrivyRuntimeStatusUninstalled)
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
	if s.runtime != nil {
		latest, latestErr := s.runtime.LatestVersion(ctx)
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
	if err := s.metadata.UpsertScanSettings(ctx, s.tenant(ctx), normalized); err != nil {
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
	settings, err := s.metadata.GetScanSettings(ctx, s.tenant(ctx))
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

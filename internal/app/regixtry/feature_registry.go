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
		summaries = append(summaries, ports.FeatureSummary{Name: details.Name, Kind: details.Kind, Enabled: details.Enabled, Configured: details.Configured})
	}
	return summaries, nil
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
	details.Runtime = s.probeFeatureRuntime(ctx, settings)
	return details, nil
}

func (s *Service) probeFeatureRuntime(ctx context.Context, settings ports.ScanSettings) ports.FeatureRuntime {
	if strings.TrimSpace(settings.ServiceURL) == "" {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "unconfigured", Detail: "service_url is required"}
	}
	if _, err := s.scannerReachableRegistryBase(settings); err != nil {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "degraded", Detail: err.Error()}
	}
	prober, ok := s.scanRunner.(trivyRuntimeProber)
	if !ok || prober == nil {
		return ports.FeatureRuntime{Mode: string(ports.FeatureKindExternalService), Health: "unknown", Detail: "service probe is not configured"}
	}
	runtime, err := prober.Probe(ctx, settings)
	if runtime.Mode == "" {
		runtime.Mode = string(ports.FeatureKindExternalService)
	}
	if err != nil {
		return runtime
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
		Name:                  feature.name,
		Kind:                  feature.kind,
		Enabled:               settings.Enabled,
		Configured:            configured,
		ScheduleEnabled:       settings.ScheduleEnabled,
		Interval:              settings.Interval,
		Timeout:               settings.Timeout,
		ServiceURL:            settings.ServiceURL,
		RegistryReachableURL:  settings.RegistryReachableURL,
		TLSCACertPath:         settings.TLSCACertPath,
		TLSInsecureSkipVerify: settings.TLSInsecureSkipVerify,
		MaxConcurrency:        settings.MaxConcurrency,
	}
}

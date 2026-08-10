package regixtry

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
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

var probeTrivyRuntime = func(binaryPath string) ports.FeatureRuntime {
	trimmed := strings.TrimSpace(binaryPath)
	if trimmed == "" {
		return ports.FeatureRuntime{Health: "unconfigured", Detail: "binary path is empty"}
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return ports.FeatureRuntime{Health: "unavailable", Detail: err.Error()}
	}
	return ports.FeatureRuntime{Health: "ready", Detail: fmt.Sprintf("binary reachable at %s", resolved)}
}

func lookupFeature(name string) (featureDescriptor, error) {
	trimmed := strings.TrimSpace(name)
	for _, feature := range builtInFeatures {
		if feature.name == trimmed {
			return feature, nil
		}
	}
	return featureDescriptor{}, domain.NewValidationError(fmt.Sprintf("unsupported feature %q", trimmed))
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
		summaries = append(summaries, ports.FeatureSummary{
			Name:       details.Name,
			Kind:       details.Kind,
			Enabled:    details.Enabled,
			Configured: details.Configured,
		})
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
	details, err := s.GetFeature(ctx, name)
	if err != nil {
		return ports.FeatureDetails{}, err
	}
	details.Runtime = probeTrivyRuntime(details.BinaryPath)
	return details, nil
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
	return s.ConfigureFeature(ctx, name, input)
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
		CacheDir:        filepath.Join(".", "trivy-cache"),
		BinaryPath:      "trivy",
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
	if input.CacheDir != nil {
		settings.CacheDir = *input.CacheDir
	}
	if input.BinaryPath != nil {
		settings.BinaryPath = *input.BinaryPath
	}
	if input.MaxConcurrency != nil {
		settings.MaxConcurrency = *input.MaxConcurrency
	}
	return settings, nil
}

func featureDetailsFromSettings(feature featureDescriptor, settings ports.ScanSettings, configured bool) ports.FeatureDetails {
	return ports.FeatureDetails{
		Name:            feature.name,
		Kind:            feature.kind,
		Enabled:         settings.Enabled,
		Configured:      configured,
		ScheduleEnabled: settings.ScheduleEnabled,
		Interval:        settings.Interval,
		Timeout:         settings.Timeout,
		CacheDir:        settings.CacheDir,
		BinaryPath:      settings.BinaryPath,
		MaxConcurrency:  settings.MaxConcurrency,
	}
}

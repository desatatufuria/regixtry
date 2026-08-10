package regixtry

import (
	"context"
	"fmt"

	"regixtry/internal/ports"
)

func (s *Service) InstallFeatureRuntime(ctx context.Context, name string, version string) (ports.TrivyRuntimeState, error) {
	return s.InstallFeatureRuntimeWithProgress(ctx, name, version, nil)
}

func (s *Service) InstallFeatureRuntimeWithProgress(ctx context.Context, name string, version string, progress func(ports.FeatureRuntimeProgress)) (ports.TrivyRuntimeState, error) {
	if err := ValidateFeatureName(name); err != nil {
		return ports.TrivyRuntimeState{}, err
	}
	if s.runtime == nil {
		return ports.TrivyRuntimeState{}, fmt.Errorf("trivy runtime manager is not configured")
	}
	return s.runtime.Install(ctx, version, progress)
}

func (s *Service) UpgradeFeatureRuntime(ctx context.Context, name string, version string) (ports.TrivyRuntimeState, error) {
	return s.UpgradeFeatureRuntimeWithProgress(ctx, name, version, nil)
}

func (s *Service) UpgradeFeatureRuntimeWithProgress(ctx context.Context, name string, version string, progress func(ports.FeatureRuntimeProgress)) (ports.TrivyRuntimeState, error) {
	if err := ValidateFeatureName(name); err != nil {
		return ports.TrivyRuntimeState{}, err
	}
	if s.runtime == nil {
		return ports.TrivyRuntimeState{}, fmt.Errorf("trivy runtime manager is not configured")
	}
	return s.runtime.Upgrade(ctx, version, progress)
}

func (s *Service) RollbackFeatureRuntime(ctx context.Context, name string) (ports.TrivyRuntimeState, error) {
	if err := ValidateFeatureName(name); err != nil {
		return ports.TrivyRuntimeState{}, err
	}
	if s.runtime == nil {
		return ports.TrivyRuntimeState{}, fmt.Errorf("trivy runtime manager is not configured")
	}
	return s.runtime.Rollback(ctx)
}

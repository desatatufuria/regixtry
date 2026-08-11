package regixtry

import (
	"context"
	"fmt"

	"regixtry/internal/ports"
)

func (s *Service) InstallFeatureRuntime(ctx context.Context, name string, version string) (ports.FeatureRuntimeState, error) {
	return s.InstallFeatureRuntimeWithProgress(ctx, name, version, nil)
}

func (s *Service) InstallFeatureRuntimeWithProgress(ctx context.Context, name string, version string, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	if err := ValidateFeatureName(name); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	manager, err := s.featureRuntimeManager(name)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	return manager.Install(ctx, version, progress)
}

func (s *Service) UpgradeFeatureRuntime(ctx context.Context, name string, version string) (ports.FeatureRuntimeState, error) {
	return s.UpgradeFeatureRuntimeWithProgress(ctx, name, version, nil)
}

func (s *Service) UpgradeFeatureRuntimeWithProgress(ctx context.Context, name string, version string, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	if err := ValidateFeatureName(name); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	manager, err := s.featureRuntimeManager(name)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	return manager.Upgrade(ctx, version, progress)
}

func (s *Service) RollbackFeatureRuntime(ctx context.Context, name string) (ports.FeatureRuntimeState, error) {
	if err := ValidateFeatureName(name); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	manager, err := s.featureRuntimeManager(name)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	return manager.Rollback(ctx)
}

func (s *Service) featureRuntimeManager(name string) (FeatureRuntimeManager, error) {
	manager, ok := s.runtimes[name]
	if !ok || manager == nil {
		return nil, fmt.Errorf("%s runtime manager is not configured", name)
	}
	return manager, nil
}

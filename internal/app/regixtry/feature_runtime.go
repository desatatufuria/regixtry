package regixtry

import (
	"context"
	"fmt"

	domain "regixtry/internal/domain/regixtry"
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

// featureRuntimeManager resolves the FeatureRuntimeManager for name, enforcing
// the same managedRuntime guard executeFeatureRuntimeAction already applies
// (design.md Decision 10): a feature registered with managedRuntime: false
// (e.g. signing) has no FeatureRuntimeManager and therefore no
// install/upgrade/rollback lifecycle at all, regardless of which route
// family a caller used (the ":install" shortcut calling this method
// directly, or "/actions/install-runtime" via executeFeatureRuntimeAction).
// Checking the guard here, ahead of the s.runtimes lookup, keeps both
// families returning the identical typed domain.NewValidationError instead
// of one of them falling through to an untyped "runtime manager is not
// configured" error for a nil manager.
func (s *Service) featureRuntimeManager(name string) (FeatureRuntimeManager, error) {
	descriptor, err := lookupFeature(name)
	if err != nil {
		return nil, err
	}
	if !descriptor.managedRuntime {
		return nil, domain.NewValidationError(fmt.Sprintf("feature %q has no managed runtime", descriptor.name))
	}
	manager, ok := s.runtimes[name]
	if !ok || manager == nil {
		return nil, fmt.Errorf("%s runtime manager is not configured", name)
	}
	return manager, nil
}

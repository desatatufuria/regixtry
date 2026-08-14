package regixtry

import (
	"context"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

// TestFeatureRuntimeLifecycleRejectsUnmanagedFeatureWithTypedValidationError
// is the post-ACL-audit RED test: InstallFeatureRuntime/
// UpgradeFeatureRuntime/RollbackFeatureRuntime for a feature with
// managedRuntime: false (signing) must reject with the same typed
// domain.NewValidationError executeFeatureRuntimeAction's managedRuntime
// guard already returns for the "/actions/install-runtime" family, not an
// untyped error from a nil-manager lookup. Both route families
// (":install" shortcut and "/actions/install-runtime") must behave
// identically regardless of which one a caller used.
func TestFeatureRuntimeLifecycleRejectsUnmanagedFeatureWithTypedValidationError(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	t.Run("install", func(t *testing.T) {
		_, err := service.InstallFeatureRuntime(context.Background(), signingFeatureName, "")
		if err == nil {
			t.Fatal("InstallFeatureRuntime(signing) error = nil, want a validation error")
		}
		if !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Fatalf("InstallFeatureRuntime(signing) error = %v, want ErrorCodeValidation", err)
		}
	})

	t.Run("upgrade", func(t *testing.T) {
		_, err := service.UpgradeFeatureRuntime(context.Background(), signingFeatureName, "")
		if err == nil {
			t.Fatal("UpgradeFeatureRuntime(signing) error = nil, want a validation error")
		}
		if !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Fatalf("UpgradeFeatureRuntime(signing) error = %v, want ErrorCodeValidation", err)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		_, err := service.RollbackFeatureRuntime(context.Background(), signingFeatureName)
		if err == nil {
			t.Fatal("RollbackFeatureRuntime(signing) error = nil, want a validation error")
		}
		if !domain.IsCode(err, domain.ErrorCodeValidation) {
			t.Fatalf("RollbackFeatureRuntime(signing) error = %v, want ErrorCodeValidation", err)
		}
	})
}

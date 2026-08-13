package regixtry

import (
	"context"
	"reflect"
	"strings"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// TestBuiltInFeaturesRegistersSigningAsBuiltinWithNoManagedRuntime is the
// Phase 6 RED test (tasks.md 6.1's setup / 6.9's descriptor shape,
// design.md Decision 10): signing joins trivy/gitleaks in builtInFeatures as
// FeatureKindBuiltin, but with managedRuntime: false — it has no
// FeatureRuntimeManager and therefore no install/upgrade/rollback lifecycle.
func TestBuiltInFeaturesRegistersSigningAsBuiltinWithNoManagedRuntime(t *testing.T) {
	t.Parallel()

	if err := ValidateFeatureName(signingFeatureName); err != nil {
		t.Fatalf("ValidateFeatureName(signing) error = %v, want signing accepted as a builtin feature", err)
	}

	descriptor, err := lookupFeature(signingFeatureName)
	if err != nil {
		t.Fatalf("lookupFeature(signing) error = %v", err)
	}
	if descriptor.kind != ports.FeatureKindBuiltin {
		t.Fatalf("lookupFeature(signing).kind = %v, want FeatureKindBuiltin", descriptor.kind)
	}
	if descriptor.managedRuntime {
		t.Fatal("lookupFeature(signing).managedRuntime = true, want false -- signing has no managed runtime binary")
	}
}

// TestServiceProjectFeatureRuntimeReturnsEmptyForFeatureWithNoManagedRuntime
// is the Phase 6 RED test (tasks.md 6.2): a feature descriptor with
// managedRuntime: false (signing) must never fabricate a managed runtime
// state -- projectFeatureRuntime must early-return the zero
// ports.FeatureRuntime{} instead of falling through to
// GetFeatureRuntimeState's NotFound -> {Mode: Managed, Status: uninstalled}
// fabrication (design.md Decision 10).
func TestServiceProjectFeatureRuntimeReturnsEmptyForFeatureWithNoManagedRuntime(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	runtime := service.projectFeatureRuntime(context.Background(), signingFeatureName)

	if (runtime != ports.FeatureRuntime{}) {
		t.Fatalf("projectFeatureRuntime(signing) = %#v, want ports.FeatureRuntime{} (Mode == \"\"), not a fabricated managed/uninstalled state", runtime)
	}
}

// TestBuildFeatureActionsOmitsRuntimeLifecycleForFeatureWithNoManagedRuntime
// is the Phase 6 RED test (tasks.md 6.3): buildFeatureActions for signing
// exposes exactly Refresh + Enable/Disable and omits Install/Upgrade/
// Rollback -- proving the existing Mode == Managed gates already do the
// right thing once 6.2's empty runtime lands. No new gating code is
// required inside buildFeatureActions itself (design.md's "None needed" row).
func TestBuildFeatureActionsOmitsRuntimeLifecycleForFeatureWithNoManagedRuntime(t *testing.T) {
	t.Parallel()

	disabled := ports.FeatureDetails{Name: signingFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: false, Runtime: ports.FeatureRuntime{}}
	if got, want := actionIDs(buildFeatureActions(disabled)), []string{"refresh", "enable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFeatureActions(signing, disabled) action IDs = %#v, want %#v (no install/upgrade/rollback)", got, want)
	}

	enabled := ports.FeatureDetails{Name: signingFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Runtime: ports.FeatureRuntime{}}
	if got, want := actionIDs(buildFeatureActions(enabled)), []string{"refresh", "disable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildFeatureActions(signing, enabled) action IDs = %#v, want %#v (no install/upgrade/rollback)", got, want)
	}
}

// TestExecuteFeatureActionRejectsRuntimeLifecycleActionsForFeatureWithNoManagedRuntime
// is the Phase 6 RED test (tasks.md 6.4): install-runtime/upgrade-runtime/
// rollback-runtime for signing return a typed domain.NewValidationError, not
// the untyped 500 featureRuntimeManager would otherwise produce for a nil
// manager -- defense in depth beyond buildFeatureActions already omitting
// the actions from the UI.
func TestExecuteFeatureActionRejectsRuntimeLifecycleActionsForFeatureWithNoManagedRuntime(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	for _, actionID := range []string{"install-runtime", "upgrade-runtime", "rollback-runtime"} {
		t.Run(actionID, func(t *testing.T) {
			_, err := service.ExecuteFeatureAction(context.Background(), signingFeatureName, actionID)
			if err == nil {
				t.Fatalf("ExecuteFeatureAction(signing, %s) error = nil, want a validation error", actionID)
			}
			if !domain.IsCode(err, domain.ErrorCodeValidation) {
				t.Fatalf("ExecuteFeatureAction(signing, %s) error = %v, want ErrorCodeValidation", actionID, err)
			}
		})
	}
}

// TestBuildFeaturePageGatesRuntimeSectionAndShowsSigningPolicyFieldsForSigningFeature
// is the Phase 6 RED test (tasks.md 6.5): buildFeaturePage for signing gates
// the Runtime section on Mode == FeatureRuntimeModeManaged (renders none,
// since signing's Runtime is the zero value) and shows a signing-specific
// Policy fields section instead of the Trivy/gitleaks
// Schedule/Interval/Timeout/Concurrency Config fields.
func TestBuildFeaturePageGatesRuntimeSectionAndShowsSigningPolicyFieldsForSigningFeature(t *testing.T) {
	t.Parallel()

	summary := ports.FeatureSummary{Name: signingFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true}
	details := ports.FeatureDetails{Name: signingFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true, Runtime: ports.FeatureRuntime{}}

	page := buildFeaturePage(summary, details, nil)

	if got, want := sectionIDs(page.Sections), []string{"policy"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("signing feature page section IDs = %#v, want %#v (no runtime section, no managed runtime)", got, want)
	}
	for _, section := range page.Sections {
		for _, field := range section.Fields {
			switch field.Label {
			case "Schedule Enabled", "Interval", "Timeout", "Max Concurrency", "Registry Reachable URL":
				t.Fatalf("signing feature page field %q present, want only signing-specific policy fields, not the Trivy/gitleaks Config fields", field.Label)
			}
		}
	}
	if got := featureFieldValue(page.Sections, "policy", "Enabled"); got != "true" {
		t.Fatalf("signing policy field Enabled = %q, want %q", got, "true")
	}
}

// TestBuildFeaturePageStillRendersRuntimeSectionForManagedFeatures pins the
// unchanged trivy/gitleaks shape: their Runtime.Mode is always Managed once
// a runtime state resolves, so the new Mode-based gate must not regress the
// existing runtime-only page shape (mirrors
// TestServiceGetFeaturePageBuildsRuntimeOnlyTrivySectionsAndDeclaredActions
// at the buildFeaturePage level directly).
func TestBuildFeaturePageStillRendersRuntimeSectionForManagedFeatures(t *testing.T) {
	t.Parallel()

	summary := ports.FeatureSummary{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin}
	details := ports.FeatureDetails{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Runtime: ports.FeatureRuntime{Mode: ports.FeatureRuntimeModeManaged, Status: string(ports.FeatureRuntimeStatusReady)}}

	page := buildFeaturePage(summary, details, nil)

	if got, want := sectionIDs(page.Sections), []string{"config", "runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trivy feature page section IDs = %#v, want %#v", got, want)
	}
}

// TestLoadFeatureSettingsForSigningProjectsSigningPolicyRowWithoutTouchingScanSettings
// is the Phase 6 RED test (tasks.md 6.6): loadFeatureSettings for signing
// projects the signing_policy_settings row into a
// ScanSettings{Enabled: policy.Enabled} shell, with configured = "a
// signing_policy_settings row exists" -- it never reads or writes a
// scan_settings(feature="signing") row.
func TestLoadFeatureSettingsForSigningProjectsSigningPolicyRowWithoutTouchingScanSettings(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	feature, settings, configured, err := service.loadFeatureSettings(context.Background(), signingFeatureName)
	if err != nil {
		t.Fatalf("loadFeatureSettings(signing) error = %v", err)
	}
	if feature.name != signingFeatureName || configured {
		t.Fatalf("loadFeatureSettings(signing) = %#v, configured = %v, want an unconfigured signing descriptor with no row written", feature, configured)
	}
	if settings.Enabled {
		t.Fatal("loadFeatureSettings(signing).Enabled = true, want false with no signing_policy_settings row written")
	}

	seedSigningPolicy(t, service, true, []string{fixtureTrustedKeyPEM(t)})

	_, settings, configured, err = service.loadFeatureSettings(context.Background(), signingFeatureName)
	if err != nil {
		t.Fatalf("loadFeatureSettings(signing) error = %v", err)
	}
	if !configured || !settings.Enabled {
		t.Fatalf("loadFeatureSettings(signing) after seeding = configured=%v settings=%#v, want configured=true Enabled=true", configured, settings)
	}

	if _, err := service.metadata.GetScanSettings(context.Background(), "tenant-a", signingFeatureName); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetScanSettings(signing) error = %v, want NotFound -- signing must never write a scan_settings row", err)
	}
}

// TestConfigureFeatureRejectsSigningOutright is the Phase 6 RED test
// (tasks.md 6.7): ConfigureFeature("signing", ...) is rejected outright,
// proving nothing can create a stray scan_settings row behind
// loadFeatureSettings' signing projection.
func TestConfigureFeatureRejectsSigningOutright(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, err := service.ConfigureFeature(context.Background(), signingFeatureName, ports.FeatureConfigureInput{Enabled: boolPtr(true)})
	if err == nil {
		t.Fatal("ConfigureFeature(signing) error = nil, want a validation error")
	}
	if !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("ConfigureFeature(signing) error = %v, want ErrorCodeValidation", err)
	}
	if !strings.Contains(err.Error(), "signing policy endpoint") {
		t.Fatalf("ConfigureFeature(signing) error = %v, want it to name the signing policy endpoint", err)
	}

	if _, err := service.metadata.GetScanSettings(context.Background(), "tenant-a", signingFeatureName); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetScanSettings(signing) error = %v, want NotFound -- the rejected call must never create a stray row", err)
	}
}

// TestExecuteFeatureActionEnableAndUpdateSigningPolicySettingsMoveTheSameBit
// is the Phase 6 RED test (tasks.md 6.8, the one-bit invariant): enabling
// via ExecuteFeatureAction("signing", "enable") and enabling via
// UpdateSigningPolicySettings both converge to identical
// GetSigningPolicySettings().Enabled and identical loadFeatureSettings-
// projected state.
func TestExecuteFeatureActionEnableAndUpdateSigningPolicySettingsMoveTheSameBit(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedSigningPolicy(t, service, false, []string{fixtureTrustedKeyPEM(t)})

	if _, err := service.ExecuteFeatureAction(context.Background(), signingFeatureName, "enable"); err != nil {
		t.Fatalf("ExecuteFeatureAction(signing, enable) error = %v", err)
	}

	settings, err := service.GetSigningPolicySettings(context.Background())
	if err != nil {
		t.Fatalf("GetSigningPolicySettings() error = %v", err)
	}
	if !settings.Enabled {
		t.Fatal("GetSigningPolicySettings().Enabled = false, want true after ExecuteFeatureAction(signing, enable)")
	}

	details, err := service.GetFeature(context.Background(), signingFeatureName)
	if err != nil {
		t.Fatalf("GetFeature(signing) error = %v", err)
	}
	if !details.Enabled {
		t.Fatal("GetFeature(signing).Enabled = false, want the feature projection to reflect the bit ExecuteFeatureAction just moved")
	}

	if _, err := service.UpdateSigningPolicySettings(context.Background(), ports.SigningPolicySettings{Enabled: false, TrustedPublicKeys: settings.TrustedPublicKeys}); err != nil {
		t.Fatalf("UpdateSigningPolicySettings() error = %v", err)
	}

	details, err = service.GetFeature(context.Background(), signingFeatureName)
	if err != nil {
		t.Fatalf("GetFeature(signing) error = %v", err)
	}
	if details.Enabled {
		t.Fatal("GetFeature(signing).Enabled = true, want the feature projection to reflect UpdateSigningPolicySettings' disable -- both paths must move the same bit")
	}
}

// TestExecuteFeatureActionEnableSigningRejectsWhenNoTrustedKeysConfigured
// covers the outage-rule guard setSigningFeatureEnabled applies: enabling
// signing through the feature-action shortcut with zero configured trusted
// keys must be refused, the same "single most important safety property"
// (design.md Decision 4) normalizeSigningOverride and the Phase 8 admin
// decoder enforce at write time.
func TestExecuteFeatureActionEnableSigningRejectsWhenNoTrustedKeysConfigured(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	_, err := service.ExecuteFeatureAction(context.Background(), signingFeatureName, "enable")
	if err == nil {
		t.Fatal("ExecuteFeatureAction(signing, enable) error = nil, want a validation error with zero trusted keys configured")
	}
	if !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("ExecuteFeatureAction(signing, enable) error = %v, want ErrorCodeValidation", err)
	}
}

// TestListAndInspectSigningFeatureShowConfigurableEnableableWithoutRuntimeActions
// is the feature-configuration spec.md scenario "Signing feature registers
// without a runtime manager" / "Install, Upgrade, and Rollback are
// unavailable": listing and inspecting signing via the existing
// ListFeatures/GetFeaturePage surface reports it as configurable and
// enableable, with zero Install/Upgrade/Rollback actions offered anywhere.
func TestListAndInspectSigningFeatureShowConfigurableEnableableWithoutRuntimeActions(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	features, err := service.ListFeatures(context.Background())
	if err != nil {
		t.Fatalf("ListFeatures() error = %v", err)
	}
	var signingSummary *ports.FeatureSummary
	for i := range features {
		if features[i].Name == signingFeatureName {
			signingSummary = &features[i]
		}
	}
	if signingSummary == nil {
		t.Fatal("ListFeatures() did not include signing")
	}
	if signingSummary.Kind != ports.FeatureKindBuiltin {
		t.Fatalf("signing summary.Kind = %v, want FeatureKindBuiltin", signingSummary.Kind)
	}

	page, err := service.GetFeaturePage(context.Background(), signingFeatureName)
	if err != nil {
		t.Fatalf("GetFeaturePage(signing) error = %v", err)
	}
	for _, action := range page.Actions {
		if action.ID == "install-runtime" || action.ID == "upgrade-runtime" || action.ID == "rollback-runtime" {
			t.Fatalf("signing feature page actions = %#v, want no install/upgrade/rollback action to appear", page.Actions)
		}
	}
	hasEnable := false
	for _, action := range page.Actions {
		if action.ID == "enable" || action.ID == "disable" {
			hasEnable = true
		}
	}
	if !hasEnable {
		t.Fatalf("signing feature page actions = %#v, want an enable/disable action to appear", page.Actions)
	}
}

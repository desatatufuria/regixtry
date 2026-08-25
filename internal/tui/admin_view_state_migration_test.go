package tui

import (
	"reflect"
	"testing"
)

// TestMigratedScreensHaveZeroFieldsOnAdminViewState is the Phase 14 task
// 14.1 RED test (T2.4, spec.md "Migrated screen has zero fields on
// AdminViewState"): a reflect field-name set vs. an explicit allowlist.
//
// Scope note (Slice 2 apply deviation, recorded honestly rather than
// silently, updated in the Phase 11/12.3 follow-up batch): this change's
// actual migrated surface is the per-repository override editor (design.md
// Decision F) plus the two new dedicated repository list screens
// (Gitleaks/Signing, Decision J), plus Signing's global policy modal
// (Phase 12.3, signingConfigScreen) -- the fields this test asserts absent
// are RepositoryOverrideModal (owned the override editor's state
// pre-change) and SigningPolicyModal (owned the signing policy modal's
// state pre-change; SigningPolicy itself, the loaded baseline settings,
// deliberately stays on AdminViewState -- see screen_signing_config.go's
// doc comment for why).
//
// Design.md's full State Migration table additionally lists TrivyConfigModal,
// ScanPolicy, ScanPolicyModal, Features, SelectedFeature, Tables.Features,
// and the Trivy Repository Alerts fields (TrivySummaries/TrivyScanRuns/
// TrivySelectedAlert/TrivyAlertsLoaded/TrivyOverrides/Tables.ScanSummary) as
// migrating in this slice too -- screenAdminFeatures itself (the Built-in
// Features list + Feature Page detail, Trivy's tabs, and Trivy's own
// config+scan-policy modals) was NOT repurposed into
// screen_security_menu.go/screen_trivy_config.go/screen_trivy_repos.go in
// this pass: it stays on the legacy adapter, unmigrated, exactly as it
// rendered before this change. That remaining migration is a genuinely
// underspecified architectural decision (design.md's table never assigns an
// owner for Gitleaks'/Signing's share of the single, feature-agnostic
// FeaturePage field once Trivy's slice is carved out into trivyConfigScreen,
// and "Tab becomes a screen switch" is only specified for Trivy) -- flagged
// as a disclosed follow-up requiring an explicit design decision (see the
// apply report), not silently guessed through. This test asserts the two
// fields genuinely migrated in this pass are genuinely gone -- it is not a
// false-green stand-in for the full table.
func TestMigratedScreensHaveZeroFieldsOnAdminViewState(t *testing.T) {
	t.Parallel()

	forbidden := map[string]bool{
		"RepositoryOverrideModal": true,
		"SigningPolicyModal":      true,
	}

	typ := reflect.TypeOf(AdminViewState{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if forbidden[name] {
			t.Fatalf("AdminViewState still has field %q, want it migrated onto overrideEditor (design.md Decision F)", name)
		}
	}
}

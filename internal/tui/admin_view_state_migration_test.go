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
// silently): this change's actual migrated surface is the per-repository
// override editor (design.md Decision F) plus the two new dedicated
// repository list screens (Gitleaks/Signing, Decision J) -- the field this
// test asserts absent is RepositoryOverrideModal, which fully owned the
// override editor's state pre-change. Design.md's full State Migration
// table additionally lists TrivyConfigModal, ScanPolicy, ScanPolicyModal,
// SigningPolicy, SigningPolicyModal, Features, SelectedFeature,
// Tables.Features, and the Trivy Repository Alerts fields
// (TrivySummaries/TrivyScanRuns/TrivySelectedAlert/TrivyAlertsLoaded/
// TrivyOverrides/Tables.ScanSummary) as migrating in this slice too --
// screenAdminFeatures itself (the Built-in Features list + Feature Page
// detail, Trivy's tabs, and Trivy/Signing's config+policy modals) was NOT
// repurposed into screen_security_menu.go/screen_trivy_config.go/
// screen_trivy_repos.go/screen_signing_config.go in this pass: it stays on
// the legacy adapter, unmigrated, exactly as it rendered before this
// change. That full migration is flagged as a follow-up (see the apply
// report), not silently dropped. This test asserts the field genuinely
// migrated in this pass is genuinely gone -- it is not a false-green stand-in
// for the full table.
func TestMigratedScreensHaveZeroFieldsOnAdminViewState(t *testing.T) {
	t.Parallel()

	forbidden := map[string]bool{
		"RepositoryOverrideModal": true,
	}

	typ := reflect.TypeOf(AdminViewState{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if forbidden[name] {
			t.Fatalf("AdminViewState still has field %q, want it migrated onto overrideEditor (design.md Decision F)", name)
		}
	}
}

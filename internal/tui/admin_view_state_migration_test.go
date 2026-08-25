package tui

import (
	"reflect"
	"testing"
)

// TestMigratedScreensHaveZeroFieldsOnAdminViewState is the Phase 14 task
// 14.1 RED test (T2.4, spec.md "Migrated screen has zero fields on
// AdminViewState"): a reflect field-name set vs. an explicit allowlist.
//
// Expanded to full scope in Phase 11 (this batch), which resolved the
// design gap the two prior batches correctly stopped on and disclosed
// instead of guessing through (see design.md's "Resolved gap (addendum,
// post-Slice-2-batch-1)" section and tasks.md's "Slice 2 apply deviations").
// Every AdminViewState field design.md's State Migration table lists as
// migrating in Slice 1/2 is now asserted absent:
//
//   - GitleaksConfigModal, RepositoryOverrideModal, SigningPolicyModal --
//     migrated in Slice 1/Phase 10/Phase 12.3 respectively (unchanged by
//     this batch, kept here for one single, exhaustive source of truth).
//   - TrivyConfigModal, ScanPolicy, ScanPolicyModal, FeaturePage, TrivyTab,
//     TrivySummaries, TrivyScanRuns, TrivySelectedAlert, TrivyAlertsLoaded,
//     TrivyOverrides -- migrated onto trivyConfigScreen/trivyReposScreen
//     this batch (Phase 11). FeaturePage is one shared struct field that
//     design.md's table lists three times (once per feature's own "slice"
//     of it); the resolved-gap addendum's own resolution is that Trivy,
//     Gitleaks, and Signing each now own an independent `page` field on
//     their own screen, so the single AdminViewState.FeaturePage field
//     itself is what this test asserts absent, counted once.
//   - SigningPolicy -- migrated onto signingConfigScreen this batch too,
//     closing the Phase 12.3 disclosed deviation: its only other reader
//     (signingPolicyBadge, composed into the retired
//     renderAdminFeaturesScreen's heading) no longer exists now that
//     screenAdminFeatures is securityMenuScreen, a bare peer list with no
//     Feature Page heading of its own.
//   - Features, SelectedFeature -- migrated onto securityMenuScreen this
//     batch (Tables.Features, the third item design.md's own row groups
//     with these two, is verified separately: it is a field of the
//     `Tables adminTablesState` struct, not a direct AdminViewState field
//     this reflect scan can enumerate by name here).
//
// Design.md's State Migration table lists ScanHistoryModal as migrating too
// (to scanHistoryScreen, Operations) -- explicitly Slice 3, not this
// change's scope, and deliberately NOT included below: trivyReposScreen
// (Phase 11) still opens it via openAdminScanHistoryMsg (screen.go), a
// migrated screen cannot write to it directly (design.md Decision B), so it
// necessarily still exists as an AdminViewState field.
func TestMigratedScreensHaveZeroFieldsOnAdminViewState(t *testing.T) {
	t.Parallel()

	forbidden := map[string]bool{
		"GitleaksConfigModal":     true,
		"RepositoryOverrideModal": true,
		"SigningPolicyModal":      true,
		"TrivyConfigModal":        true,
		"ScanPolicy":              true,
		"ScanPolicyModal":         true,
		"FeaturePage":             true,
		"TrivyTab":                true,
		"TrivySummaries":          true,
		"TrivyScanRuns":           true,
		"TrivySelectedAlert":      true,
		"TrivyAlertsLoaded":       true,
		"TrivyOverrides":          true,
		"SigningPolicy":           true,
		"Features":                true,
		"SelectedFeature":         true,
	}

	typ := reflect.TypeOf(AdminViewState{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if forbidden[name] {
			t.Fatalf("AdminViewState still has field %q, want it migrated onto its owning screen (design.md's State Migration table)", name)
		}
	}
}

// TestAdminTablesStateHoldsOnlyLegacyScanHistoryFields is the Phase 11
// companion to TestMigratedScreensHaveZeroFieldsOnAdminViewState: verifies
// design.md's Tables.Features/Tables.FeatureRows/Tables.ScanSummary rows
// (the third migration target the reflect scan above cannot enumerate,
// since they are fields of the nested adminTablesState struct, not
// AdminViewState itself) are also gone, leaving only the still-legacy
// ScanHistoryModal's own Findings/SecretFindings tables and the Selection
// projection (Slice 3, unmigrated).
func TestAdminTablesStateHoldsOnlyLegacyScanHistoryFields(t *testing.T) {
	t.Parallel()

	want := map[string]bool{"Findings": true, "SecretFindings": true, "Selection": true}
	typ := reflect.TypeOf(adminTablesState{})
	if got := typ.NumField(); got != len(want) {
		t.Fatalf("adminTablesState has %d fields, want exactly %d: %v", got, len(want), want)
	}
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !want[name] {
			t.Fatalf("adminTablesState has unexpected field %q, want only the still-legacy ScanHistoryModal fields %v", name, want)
		}
	}
}

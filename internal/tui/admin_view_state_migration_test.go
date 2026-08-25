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
// ScanHistoryModal -- migrated onto scanHistoryScreen this batch (Phase 19,
// design.md's State Migration table: "ScanHistoryModal -> scanHistoryScreen
// (Operations, Slice 3)"), closing the Phase 11 scope note that used to
// exclude it here.
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
		"ScanHistoryModal":        true,
	}

	typ := reflect.TypeOf(AdminViewState{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if forbidden[name] {
			t.Fatalf("AdminViewState still has field %q, want it migrated onto its owning screen (design.md's State Migration table)", name)
		}
	}
}

// TestAdminTablesStateHoldsOnlyLegacyScanHistoryFields was the Phase 11
// companion to TestMigratedScreensHaveZeroFieldsOnAdminViewState, asserting
// adminTablesState held only ScanHistoryModal's own Findings/SecretFindings/
// Selection fields. Phase 19 deletes adminTablesState/AdminViewState.Tables
// entirely -- scanHistoryScreen owns its own findings/secretFindings tables
// directly (design.md Decision B), so this test's subject type no longer
// exists; TestMigratedScreensHaveZeroFieldsOnAdminViewState's ScanHistoryModal
// entry above is the direct replacement proof.

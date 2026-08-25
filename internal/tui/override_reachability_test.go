package tui

import (
	"testing"
	"time"

	"regixtry/internal/ports"
)

// TestEveryOverridePathReachableBeforeIsReachableAfter is the Phase 8/15
// test (T2.0, proposal Success Criteria "Every override and results path
// reachable before the change is reachable after it — asserted per path,
// not summarized"): one case per path enumerated from the proposal's
// Success Criteria, asserted against the POST-reversal code (this apply
// batch implements Phase 8's capture and Phase 15's re-proof together,
// rather than as two separate commits -- the per-path assertions below are
// the same invariant the design's two-phase split exists to protect,
// checked once against the final shipped behavior).
func TestEveryOverridePathReachableBeforeIsReachableAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)

	t.Run("Trivy override via o on Repository Alerts", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			scanRuns: []ports.ScanRun{
				{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter") // trivyConfigScreen
		updated = runKey(t, updated, "tab")   // trivyReposScreen
		updated = runKey(t, updated, "o")

		// Phase 11: the uniform overrideEditor is now embedded directly in
		// trivyReposScreen (mirrors featureOverridesScreen's own pattern),
		// not a separate slotTrivyOverride slot.
		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok || !repos.editor.Active() || repos.editor.Feature() != trivyFeatureName {
			t.Fatalf("Trivy override path unreachable: adminScreens[slotTrivyRepos] = %#v", updated.adminScreens[slotTrivyRepos])
		}
	})

	t.Run("Gitleaks override via its own dedicated screen", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter") // gitleaksConfigScreen
		updated = runKey(t, updated, "o")     // screenSecurityGitleaksRepos
		updated = runKey(t, updated, "o")     // opens the override editor

		screen, ok := updated.adminScreens[slotGitleaksRepos].(featureOverridesScreen)
		if !ok || !screen.editor.Active() || screen.editor.Feature() != gitleaksFeatureName {
			t.Fatalf("Gitleaks override path unreachable: adminScreens[slotGitleaksRepos] = %#v", updated.adminScreens[slotGitleaksRepos])
		}
	})

	t.Run("Signing override via its own dedicated screen", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter") // signingConfigScreen
		updated = runKey(t, updated, "o")     // screenSecuritySigningRepos
		updated = runKey(t, updated, "o")     // opens the override editor

		screen, ok := updated.adminScreens[slotSigningRepos].(featureOverridesScreen)
		if !ok || !screen.editor.Active() || screen.editor.Feature() != signingFeatureName {
			t.Fatalf("Signing override path unreachable: adminScreens[slotSigningRepos] = %#v", updated.adminScreens[slotSigningRepos])
		}
	})

	t.Run("Secret findings via Trivy's Enter drill-down (unchanged)", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			scanRuns: []ports.ScanRun{
				{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter") // trivyConfigScreen
		updated = runKey(t, updated, "tab")   // trivyReposScreen
		updated = runKey(t, updated, "enter")

		if !updated.adminView.ScanHistoryModal.Active() {
			t.Fatalf("ScanHistoryModal.Active() = false, want true -- Trivy's Enter drill-down to secret findings must still work unchanged")
		}
	})
}

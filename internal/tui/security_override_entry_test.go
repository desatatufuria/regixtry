package tui

import (
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// TestGitleaksOverrideOpensWithoutEnteringTrivy is the Phase 13 task 13.1
// RED test (T2.1a, spec.md "Gitleaks override opens without entering
// Trivy"): the key sequence from Gitleaks' own screen -- 'f' to open the
// Built-in Features list, 'o' to enter Gitleaks' own dedicated repository
// list screen, 'o' again on the highlighted row -- never touches a Trivy
// screen id or Trivy's own key-routing path (m.isSelectedTrivyFeature()),
// and opens the override editor bound to gitleaks.
func TestGitleaksOverrideOpensWithoutEnteringTrivy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter") // navigate into gitleaksConfigScreen

	if strings.Contains(string(updated.screen), "trivy") {
		t.Fatalf("screen = %q, want no Trivy screen id reached", updated.screen)
	}

	updated = runKey(t, updated, "o")

	if got, want := updated.screen, screenSecurityGitleaksRepos; got != want {
		t.Fatalf("screen = %q, want %q -- Gitleaks' own dedicated repository list screen", got, want)
	}
	reposScreen, ok := updated.adminScreens[slotGitleaksRepos].(featureOverridesScreen)
	if !ok {
		t.Fatalf("adminScreens[slotGitleaksRepos] = %#v, want featureOverridesScreen", updated.adminScreens[slotGitleaksRepos])
	}
	if reposScreen.feature != gitleaksFeatureName {
		t.Fatalf("feature = %q, want %q", reposScreen.feature, gitleaksFeatureName)
	}

	updated = runKey(t, updated, "o")

	opened, ok := updated.adminScreens[slotGitleaksRepos].(featureOverridesScreen)
	if !ok || !opened.editor.Active() {
		t.Fatalf("adminScreens[slotGitleaksRepos].editor = %#v, want an active overrideEditor", opened.editor)
	}
	if got, want := opened.editor.Feature(), gitleaksFeatureName; got != want {
		t.Fatalf("editor.Feature() = %q, want %q -- opened WITHOUT ever selecting Trivy's screen", got, want)
	}
}

// TestSigningOverrideOpensWithoutEnteringTrivy is the Phase 13 task 13.2 RED
// test (T2.1b, spec.md "Signing override opens without entering Trivy"):
// same shape as the Gitleaks test above, for Signing's own dedicated
// repository list screen.
func TestSigningOverrideOpensWithoutEnteringTrivy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter") // navigate into signingConfigScreen

	if strings.Contains(string(updated.screen), "trivy") {
		t.Fatalf("screen = %q, want no Trivy screen id reached", updated.screen)
	}

	updated = runKey(t, updated, "o")

	if got, want := updated.screen, screenSecuritySigningRepos; got != want {
		t.Fatalf("screen = %q, want %q -- Signing's own dedicated repository list screen", got, want)
	}

	updated = runKey(t, updated, "o")

	opened, ok := updated.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if !ok || !opened.editor.Active() {
		t.Fatalf("adminScreens[slotSigningRepos].editor = %#v, want an active overrideEditor", opened.editor)
	}
	if got, want := opened.editor.Feature(), signingFeatureName; got != want {
		t.Fatalf("editor.Feature() = %q, want %q -- opened WITHOUT ever selecting Trivy's screen", got, want)
	}
}

// TestOverrideKeyIsInertWithoutAHighlightedRow is the Phase 13 task 13.3 RED
// test (T2.5, spec.md "The override key is scoped to the opening screen's
// row only"): 'o' pressed on a featureOverridesScreen with no rows loaded
// (empty catalog, no stored overrides) does not open the editor.
func TestOverrideKeyIsInertWithoutAHighlightedRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
	}
	// No repository catalog and no stored overrides -- newAdminReadyModelWithCatalog(nil, ...)
	// yields an empty row set.
	updated := runAdminLogin(t, newAdminReadyModelWithCatalog(t, nil, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter") // navigate into gitleaksConfigScreen
	updated = runKey(t, updated, "o")     // enters screenSecurityGitleaksRepos, zero rows

	screenBefore, ok := updated.adminScreens[slotGitleaksRepos].(featureOverridesScreen)
	if !ok || len(screenBefore.rows) != 0 {
		t.Fatalf("rows = %#v, want zero rows for this test's setup", screenBefore.rows)
	}

	updated = runKey(t, updated, "o")

	screenAfter, _ := updated.adminScreens[slotGitleaksRepos].(featureOverridesScreen)
	if screenAfter.editor.Active() {
		t.Fatal("editor.Active() = true, want false -- 'o' must be inert with no highlighted row")
	}
}

// TestOverrideModalStaysWithinViewport is the Phase 13 task 13.4 RED test
// (T2.6, preserved MODIFIED-requirement scenario): at minimum viable
// terminal height, the rendered override editor overlay does not exceed the
// terminal's visible rows.
func TestOverrideModalStaysWithinViewport(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	editor := overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, exists: true, enabled: true, pathPrimary: "/etc/trivy/ignore", pathSecondary: "/etc/trivy/policy.rego", err: "ignore_file_path must be absolute"}

	rendered := renderOverrideEditor(theme, editor)
	height := strings.Count(rendered, "\n") + 1
	if height > minViewportHeight {
		t.Fatalf("renderOverrideEditor() height = %d rows, want <= %d (minViewportHeight)\n%s", height, minViewportHeight, rendered)
	}
}

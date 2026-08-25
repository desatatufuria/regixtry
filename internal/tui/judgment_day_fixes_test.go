package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSecurityConfigScreensJoinAdminPrincipalScreenSet is the Judgment Day
// fix-round RED test for Finding 1 (Judge A, CRITICAL): isAdminPrincipalScreen
// gates the bare 'q' quit key (model.go:1685), checked before any screen ever
// sees the key. screenSecurityTrivy/screenSecurityGitleaksConfig/
// screenSecuritySigningConfig each advertise "q: quit" in their own
// Keys()-derived footer (see trivyConfigScreen.Keys/gitleaksConfigScreen.Keys/
// signingConfigScreen.Keys), so 'q' must actually reach tea.Quit from them.
// The 3 sibling repos screens (screenSecurityTrivyRepos/
// screenSecurityGitleaksRepos/screenSecuritySigningRepos) do NOT advertise
// "q: quit" in their own Keys() (trivyReposKeys/featureOverridesKeys), so
// they are deliberately excluded here.
func TestSecurityConfigScreensJoinAdminPrincipalScreenSet(t *testing.T) {
	t.Parallel()

	for _, screenID := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig} {
		if !isAdminPrincipalScreen(screenID) {
			t.Fatalf("isAdminPrincipalScreen(%v) = false, want true (screen advertises \"q: quit\" in its own footer)", screenID)
		}
	}

	for _, screenID := range []screen{screenSecurityTrivyRepos, screenSecurityGitleaksRepos, screenSecuritySigningRepos} {
		if isAdminPrincipalScreen(screenID) {
			t.Fatalf("isAdminPrincipalScreen(%v) = true, want false (screen does not advertise \"q: quit\")", screenID)
		}
	}
}

// TestCanLogoutFromAllSixNewSecurityScreens is the Judgment Day fix-round RED
// test for Finding 2 (Judge A, CRITICAL): canLogoutAdminFromCurrentScreen
// gates the bare 'l' logout key (model.go:1668). All 6 new Security &
// Compliance screens introduced by this change must be reachable via 'l',
// exactly as the legacy single screenAdminFeatures id was before this change.
func TestCanLogoutFromAllSixNewSecurityScreens(t *testing.T) {
	t.Parallel()

	for _, screenID := range []screen{
		screenSecurityTrivy,
		screenSecurityTrivyRepos,
		screenSecurityGitleaksConfig,
		screenSecuritySigningConfig,
		screenSecurityGitleaksRepos,
		screenSecuritySigningRepos,
	} {
		model := newAdminReadyModel(t, &fakeAdminClient{})
		model.adminAuth = adminAuthStateAuthenticated
		model.screen = screenID

		if !model.canLogoutAdminFromCurrentScreen() {
			t.Fatalf("canLogoutAdminFromCurrentScreen() = false, want true on %v", screenID)
		}
	}
}

// TestCanLogoutFromSecurityRepoScreenIsFalseWhileItsOwnOverrideEditorIsActive
// guards against a regression the naive Finding 2 fix introduces (caught by
// the pre-existing TestFeatureOverridesScreenSigningSaveIncludesUnsignedSelfRead):
// featureOverridesScreen/trivyReposScreen each embed their own overrideEditor
// as free-text input, invisible to the legacy m.adminView.Confirm.Active()
// guard (design.md Decision B). Naively joining these screens' ids to
// canLogoutAdminFromCurrentScreen's unconditional-true case, without also
// checking the mounted screen's own editor state, makes typing an upper-case
// "L" (case-insensitively matched by isRuneKey) into the editor's free-text
// field log the operator out mid-keystroke instead of inserting the rune.
func TestCanLogoutFromSecurityRepoScreenIsFalseWhileItsOwnOverrideEditorIsActive(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenSecuritySigningRepos
	slot, ok := slotFor(screenSecuritySigningRepos)
	if !ok {
		t.Fatalf("slotFor(screenSecuritySigningRepos) = false, want true")
	}
	model.adminScreens[slot] = featureOverridesScreen{
		id:      screenSecuritySigningRepos,
		feature: signingFeatureName,
		editor:  newOverrideEditor(signingFeatureName, "library/alpine"),
	}

	if model.canLogoutAdminFromCurrentScreen() {
		t.Fatalf("canLogoutAdminFromCurrentScreen() = true, want false while the screen's own override editor is active (would swallow free-text keystrokes as a logout)")
	}
}

// TestTrivyGitleaksSigningConfigScreensShowSubmittingStatusOnConfirmDispatch
// is the Judgment Day fix-round RED test for Finding 3 (Judge B, CRITICAL):
// confirmPrompt.submitting is built with a real "Submitting X for Y..."
// value by each of trivyConfigScreen/gitleaksConfigScreen/
// signingConfigScreen's dispatchAction, but was never read/rendered by any
// of their own Update/View methods -- unlike the legacy
// AdminViewState.Confirm path (model.go's updateAdminKey sets
// m.status = before.submitting), which these migrated screens no longer go
// through (design.md Decision B: a migrated screen owns its own state).
// Each screen must surface its own submitting status text, entirely within
// its own confirmPrompt/Update/View handling, the instant Enter dispatches
// onConfirm's command.
func TestTrivyGitleaksSigningConfigScreensShowSubmittingStatusOnConfirmDispatch(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	// contentBudget gives fitLines a real SectionRows budget so the rendered
	// Body actually contains every line instead of being clamped to a
	// near-zero visible window (a bare screenEnv{} zero-value Layout has
	// SectionRows == 0, which would hide the submitting line regardless of
	// whether the fix under test works).
	env := screenEnv{Layout: contentBudget(150, 40, "", "")}

	t.Run("trivy", func(t *testing.T) {
		t.Parallel()

		called := false
		s := trivyConfigScreen{
			loaded: true,
			confirm: newConfirmPrompt("Disable Trivy", "Disable trivy?", "disable", "Submitting disable for trivy...", func(screenEnv) tea.Cmd {
				called = true
				return func() tea.Msg { return nil }
			}),
		}

		next, cmd, consumed := s.Update(env, tea.KeyMsg{Type: tea.KeyEnter})
		if !consumed {
			t.Fatalf("consumed = false, want true")
		}
		if cmd == nil {
			t.Fatalf("cmd = nil, want the onConfirm-dispatched command")
		}
		if !called {
			t.Fatalf("onConfirm was not invoked")
		}

		frame := next.View(theme, env)
		if !strings.Contains(frame.Body, "Submitting disable for trivy...") {
			t.Fatalf("View().Body = %q, want it to contain the submitting status text", frame.Body)
		}
	})

	t.Run("gitleaks", func(t *testing.T) {
		t.Parallel()

		called := false
		s := gitleaksConfigScreen{
			loaded: true,
			confirm: newConfirmPrompt("Disable Gitleaks", "Disable gitleaks?", "disable", "Submitting disable for gitleaks...", func(screenEnv) tea.Cmd {
				called = true
				return func() tea.Msg { return nil }
			}),
		}

		next, cmd, consumed := s.Update(env, tea.KeyMsg{Type: tea.KeyEnter})
		if !consumed {
			t.Fatalf("consumed = false, want true")
		}
		if cmd == nil {
			t.Fatalf("cmd = nil, want the onConfirm-dispatched command")
		}
		if !called {
			t.Fatalf("onConfirm was not invoked")
		}

		frame := next.View(theme, env)
		if !strings.Contains(frame.Body, "Submitting disable for gitleaks...") {
			t.Fatalf("View().Body = %q, want it to contain the submitting status text", frame.Body)
		}
	})

	t.Run("signing", func(t *testing.T) {
		t.Parallel()

		called := false
		s := signingConfigScreen{
			loaded: true,
			confirm: newConfirmPrompt("Disable Signing", "Disable signing?", "disable", "Submitting disable for signing...", func(screenEnv) tea.Cmd {
				called = true
				return func() tea.Msg { return nil }
			}),
		}

		next, cmd, consumed := s.Update(env, tea.KeyMsg{Type: tea.KeyEnter})
		if !consumed {
			t.Fatalf("consumed = false, want true")
		}
		if cmd == nil {
			t.Fatalf("cmd = nil, want the onConfirm-dispatched command")
		}
		if !called {
			t.Fatalf("onConfirm was not invoked")
		}

		frame := next.View(theme, env)
		if !strings.Contains(frame.Body, "Submitting disable for signing...") {
			t.Fatalf("View().Body = %q, want it to contain the submitting status text", frame.Body)
		}
	})
}

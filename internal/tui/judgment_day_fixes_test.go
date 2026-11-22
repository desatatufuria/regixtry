package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSecurityScreensJoinAdminPrincipalScreenSet pins that the bare 'q' quit
// key (isAdminPrincipalScreen, checked before any screen sees the key) works on
// all six Security screens: the three configuration screens and the three
// repositories screens. The repositories screens hold it back while their
// override editor is open (migratedScreenCapturesTextInput).
func TestSecurityScreensJoinAdminPrincipalScreenSet(t *testing.T) {
	t.Parallel()

	for _, screenID := range []screen{
		screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig,
		screenSecurityTrivyRepos, screenSecurityGitleaksRepos, screenSecuritySigningRepos,
	} {
		if !isAdminPrincipalScreen(screenID) {
			t.Fatalf("isAdminPrincipalScreen(%v) = false, want true", screenID)
		}
	}
}

// TestCanLogoutFromAllSixNewSecurityScreens is the Judgment Day fix-round RED
// test for Finding 2 (Judge A, CRITICAL): canLogoutAdminFromCurrentScreen
// gates the bare 'l' logout key. All 6 Security screens must be reachable
// via 'l'.
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

// TestTrivyGitleaksSigningConfigScreensShowSubmittingStatusOnConfirmDispatch
// is the Judgment Day fix-round RED test for Finding 3 (Judge B, CRITICAL):
// confirmPrompt.submitting is built with a real "Submitting X for Y..."
// value by each of trivyConfigScreen/gitleaksConfigScreen/
// signingConfigScreen's dispatchAction, but was never read/rendered by any
// of their own Update/View methods -- unlike a shared
// Model-level status (design.md Decision B: a migrated screen owns its own state).
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

		next, cmd, consumed := s.Update(env, testKeyEnter())
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

		next, cmd, consumed := s.Update(env, testKeyEnter())
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

		next, cmd, consumed := s.Update(env, testKeyEnter())
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

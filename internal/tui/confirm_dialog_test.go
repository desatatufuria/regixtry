package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func confirmDialogPrompt(confirmText string) (confirmPrompt, *int) {
	calls := new(int)
	return newConfirmPrompt("Confirm "+confirmText, "Do it?", confirmText, "Submitting...", func(screenEnv) tea.Cmd {
		*calls++
		return func() tea.Msg { return nil }
	}), calls
}

// ---- key behavior, identical for every caller ----

func TestConfirmPromptKeysEveryCallerRelies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		key         keyMsg
		wantCalls   int
		wantActive  bool
		wantCommand bool
	}{
		{"enter confirms and keeps the prompt open", testKeyEnter(), 1, true, true},
		{"y confirms and keeps the prompt open", testKeyRunes("y"), 1, true, true},
		{"capital Y does not confirm", testKeyRunes("Y"), 0, true, false},
		{"esc cancels without dispatching", testKeyEsc(), 0, false, false},
		{"n is swallowed: it neither confirms nor cancels", testKeyRunes("n"), 0, true, false},
		{"tab is swallowed", testKeyTab(), 0, true, false},
		{"space is swallowed", testKeySpace(), 0, true, false},
		{"another letter is swallowed", testKeyRunes("x"), 0, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, calls := confirmDialogPrompt("delete")
			next, cmd, consumed := c.update(screenEnv{}, tt.key)
			if !consumed {
				t.Fatal("key was not consumed")
			}
			if *calls != tt.wantCalls || next.Active() != tt.wantActive || (cmd != nil) != tt.wantCommand {
				t.Fatalf("calls = %d active = %v command = %v, want %d %v %v", *calls, next.Active(), cmd != nil, tt.wantCalls, tt.wantActive, tt.wantCommand)
			}
		})
	}
}

func TestConfirmPromptInTheAdminViewRecordsSubmittingOnConfirmAndClearsOnEsc(t *testing.T) {
	t.Parallel()
	for _, confirmKey := range []keyMsg{testKeyEnter(), testKeyRunes("y")} {
		m := charAdminModel(t, 150, 24)
		m.screen = screenAdminRobots
		m.adminView.Confirm, _ = confirmDialogPrompt("disable")
		m.adminView.Confirm.submitting = "Submitting disable for alice..."
		m = testSendKey(t, m, confirmKey)
		if m.status != "Submitting disable for alice..." || !m.adminView.Confirm.Active() {
			t.Fatalf("%v: status = %q active = %v", confirmKey, m.status, m.adminView.Confirm.Active())
		}
	}
	m := charAdminModel(t, 150, 24)
	m.screen = screenAdminRobots
	m.adminView.Confirm, _ = confirmDialogPrompt("disable")
	m = testSendKey(t, m, testKeyEsc())
	if m.adminView.Confirm.Active() || m.status != "" {
		t.Fatalf("esc: active = %v status = %q", m.adminView.Confirm.Active(), m.status)
	}
}

// ---- the dialog ----

func TestConfirmDialogShowsTitleMessageTargetConsequenceAndButtons(t *testing.T) {
	t.Parallel()
	c, _ := confirmDialogPrompt("revoke")
	c = c.withTarget("release-pipeline", "robot ci-publisher, act_9c41…e0d2").withConsequence("Cannot be undone. Issue a new token to restore access.")
	plain := flattenBoxed(c.view(newAdminTheme()))
	for _, want := range []string{"Confirm revoke", "Do it?", "release-pipeline", "robot ci-publisher, act_9c41…e0d2", "Cannot be undone. Issue a new token to restore access.", "Confirm y / enter", "Cancel esc"} {
		if !strings.Contains(plain, want) {
			t.Errorf("dialog lacks %q:\n%s", want, plain)
		}
	}
}

func TestConfirmDialogWithoutTargetOrConsequenceShowsOnlyTitleMessageAndButtons(t *testing.T) {
	t.Parallel()
	c, _ := confirmDialogPrompt("enable")
	out := ansi.Strip(c.view(newAdminTheme()))
	if got := strings.Count(out, "╭"); got != 1 {
		t.Fatalf("dialog has %d boxes, want just its own border:\n%s", got, out)
	}
	if strings.Contains(out, "Enter: enable") || strings.Contains(out, "Esc: cancel") {
		t.Fatalf("dialog still prints the old help line:\n%s", out)
	}
}

func TestConfirmDialogDestructiveActionsUseTheErrorColor(t *testing.T) {
	t.Parallel()
	theme := newAdminTheme()
	errorSeq := ansi.Style{}.ForegroundColor(theme.error.GetForeground()).String()
	for verb, wantError := range map[string]bool{"delete": true, "remove": true, "revoke": true, "disable": true, "enable": false, "": false} {
		c, _ := confirmDialogPrompt(verb)
		if got := strings.Contains(c.view(theme), strings.TrimSuffix(errorSeq, "m")); got != wantError {
			t.Errorf("confirmText %q: error color present = %v, want %v", verb, got, wantError)
		}
	}
}

func TestConfirmDialogFitsEverySizeOverTheScreen(t *testing.T) {
	t.Parallel()
	for _, size := range loginSizes {
		for _, long := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/long=%v", size.w, size.h, long), func(t *testing.T) {
				t.Parallel()
				m := charAdminModel(t, size.w, size.h)
				m.screen = screenAdminRobots
				c, _ := confirmDialogPrompt("remove")
				c = c.withTarget("alice on team/api", "Writer").withConsequence("Grant access again to restore it.")
				if long {
					c.message = strings.Repeat("a very long message ", 12)
				}
				m.adminView.Confirm = c
				view := m.View().Content
				assertGrantNoOverflow(t, "confirm", view, size.w, size.h)
				if !long && !strings.Contains(ansi.Strip(view), "Cancel") {
					t.Fatalf("buttons missing:\n%s", ansi.Strip(view))
				}
			})
		}
	}
}

func confirmGoldenModel(t *testing.T, w, h int) Model {
	m := charAdminModel(t, w, h)
	m.screen = screenAdminRobots
	c, _ := confirmDialogPrompt("disable")
	c.title, c.message = "Disable user?", "The user is signed out and cannot sign in. Their tokens stop working."
	m.adminView.Confirm = c.withTarget("carol", "user, 2 grants, 2 active tokens").withConsequence("Grants are kept. Enable carol again to restore everything.")
	return m
}

func TestConfirmDialogGoldens(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
		name := fmt.Sprintf("confirm_disable_user_%dx%d", size.w, size.h)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertLoginConfirmGolden(t, name, ansi.Strip(confirmGoldenModel(t, size.w, size.h).View().Content))
		})
	}
	t.Run("enable_150x24", func(t *testing.T) {
		t.Parallel()
		m := charAdminModel(t, 150, 24)
		m.screen = screenAdminRobots
		m.adminView.Confirm = newConfirmPrompt("Enable User", "Enable alice?", "enable", "", func(screenEnv) tea.Cmd { return nil })
		assertLoginConfirmGolden(t, "confirm_enable_user_150x24", ansi.Strip(m.View().Content))
	})
}

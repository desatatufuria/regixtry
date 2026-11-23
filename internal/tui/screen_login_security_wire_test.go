package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	appauth "regixtry/internal/app/auth"
)

// loginDrive runs cmd and feeds every message it yields back to the screen, as
// the program would, until no command is left.
func loginDrive(t *testing.T, s adminScreen, env screenEnv, cmd tea.Cmd) adminScreen {
	t.Helper()
	for cmd != nil {
		next, more, _ := s.Update(env, cmd())
		s, cmd = next, more
	}
	return s
}

func loginPressScreen(t *testing.T, s adminScreen, env screenEnv, keys ...keyMsg) adminScreen {
	t.Helper()
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		s = loginDrive(t, next, env, cmd)
	}
	return s
}

func loginScreenText(s adminScreen, env screenEnv) string {
	frame := s.View(newAdminTheme(), env)
	return ansi.Strip(frame.Body + "\n" + frame.Overlay)
}

// TestLoginSecurityScreenThroughTheRealStack drives the screen's own commands
// through the real HTTPAdminClient, router, service and store: failures made by
// the real login path appear, an enforce-mode lockout shows as an account
// waiting, resetting it through the screen lets it log in again, an unknown name
// is only ever a hash chip, and a reset that is already done reads as gone.
func TestLoginSecurityScreenThroughTheRealStack(t *testing.T) {
	w := newLoginWire(t, appauth.BackoffEnforce)
	w.user("alice")
	for range 5 {
		if status := w.exchange("alice", "wrong-password"); status != 401 {
			t.Fatalf("wrong password status = %d", status)
		}
		w.exchange("Ghost-Name", "wrong-password")
	}
	if status := w.exchange("alice", loginWirePassword); status != 429 {
		t.Fatalf("alice is not locked out: status = %d", status)
	}

	env := w.env(200, 40)
	var s adminScreen = newLoginSecurityScreen()
	s = loginDrive(t, s, env, s.Init(env))
	text := loginScreenText(s, env)
	for _, want := range []string{"backoff mode: enforce", "alice", "unknown #", "bad password", "unknown user", "throttled", "RECENT FAILURES"} {
		if !strings.Contains(text, want) {
			t.Errorf("screen is missing %q:\n%s", want, text)
		}
	}
	for _, banned := range []string{"ghost", "Ghost", "hash:"} {
		if strings.Contains(text, banned) {
			t.Errorf("the screen shows %q (an unknown username must only be a chip):\n%s", banned, text)
		}
	}

	waiting := s.(loginSecurityScreen).backoff.Accounts
	if len(waiting) != 2 {
		t.Fatalf("accounts waiting = %+v, want alice and the unknown name", waiting)
	}
	aliceRow := -1
	for i, account := range waiting {
		if account.Account == "alice" {
			aliceRow = i
		}
	}
	if aliceRow < 0 {
		t.Fatalf("alice is not waiting: %+v", waiting)
	}

	// Select alice, x, Enter: one real reset, and she can log in at once.
	for range aliceRow {
		s = loginPressScreen(t, s, env, testKeyDown())
	}
	s = loginPressScreen(t, s, env, testKeyRunes("x"))
	if text := loginScreenText(s, env); !strings.Contains(text, "Reset backoff?") || !strings.Contains(text, "user, 5 failed logins") {
		t.Fatalf("confirm:\n%s", text)
	}
	if status := w.exchange("alice", loginWirePassword); status != 429 {
		t.Fatalf("opening the dialog already reset alice: status = %d", status)
	}
	s = loginPressScreen(t, s, env, testKeyEnter())
	if text := loginScreenText(s, env); !strings.Contains(text, "Backoff reset for alice. alice can try again now.") {
		t.Fatalf("result:\n%s", text)
	}
	if status := w.exchange("alice", loginWirePassword); status != 200 {
		t.Fatalf("alice cannot log in after the reset: status = %d", status)
	}
	if got := s.(loginSecurityScreen).backoff.Accounts; len(got) != 1 || got[0].Account == "alice" {
		t.Fatalf("accounts after the reset = %+v", got)
	}

	// The unknown name is reset by its hash label, never by a raw name.
	label := s.(loginSecurityScreen).backoff.Accounts[0].Account
	if !strings.HasPrefix(label, "hash:") {
		t.Fatalf("the unknown name is listed as %q, want a hash label", label)
	}
	s = loginPressScreen(t, s, env, testKeyRunes("x"))
	if text := loginScreenText(s, env); !strings.Contains(text, "no such account, 5 failed logins") || strings.Contains(text, "hash:") {
		t.Fatalf("unknown confirm:\n%s", text)
	}
	// Somebody resets it first (second terminal): Enter then reads as gone.
	if _, err := w.client.ResetLoginBackoff(t.Context(), w.session, label); err != nil {
		t.Fatal(err)
	}
	s = loginPressScreen(t, s, env, testKeyEnter())
	text = loginScreenText(s, env)
	if !strings.Contains(text, " is no longer waiting. The list was refreshed.") || strings.Contains(text, "Could not reset") || strings.Contains(text, "not found") {
		t.Fatalf("gone result:\n%s", text)
	}
	if got := s.(loginSecurityScreen).backoff.Accounts; len(got) != 0 {
		t.Fatalf("list not refreshed: %+v", got)
	}
	if !strings.Contains(text, "Nobody is waiting.") {
		t.Errorf("empty waiting state missing:\n%s", text)
	}
}

// The failures list pages by cursor through the real service.
func TestLoginSecurityScreenPagesRealFailures(t *testing.T) {
	w := newLoginWire(t, appauth.BackoffLog)
	for i := range 130 {
		// Distinct names: one aggregated row each.
		w.exchange("zyxprobe"+string(rune('a'+i%26))+string(rune('a'+i/26)), "wrong-password")
	}
	env := w.env(200, 40)
	var s adminScreen = newLoginSecurityScreen()
	s = loginDrive(t, s, env, s.Init(env))
	screen := s.(loginSecurityScreen)
	if len(screen.failures) != loginFailuresPageSize || screen.next == "" {
		t.Fatalf("first page: %d rows, next=%q", len(screen.failures), screen.next)
	}
	s = loginPressScreen(t, s, env, testKeyTab(), testKeyRunes("n"), testKeyRunes("n"), testKeyRunes("n"))
	screen = s.(loginSecurityScreen)
	if len(screen.failures) != 130 || screen.next != "" {
		t.Fatalf("after n: %d rows, next=%q, want all 130 and no next page", len(screen.failures), screen.next)
	}
	if text := loginScreenText(s, env); strings.Contains(text, "zyxprobe") {
		t.Errorf("a raw unknown username is on the screen")
	}
}

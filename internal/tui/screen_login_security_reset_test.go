package tui

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// loginSend delivers one key to the model and returns the command it produced,
// WITHOUT running it: the test decides when the call reaches the server.
func loginSend(t *testing.T, m Model, k keyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(k)
	return updated.(Model), cmd
}

// loginDeliver runs cmd and delivers its message (and what that returns) to m.
func loginDeliver(t *testing.T, m Model, cmd tea.Cmd) Model { return runCmd(t, m, cmd) }

func TestLoginResetXOpensTheConfirmAndNeverResetsDirectly(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	m, cmd := loginSend(t, m, testKeyRunes("x"))
	if cmd != nil || len(client.loginAPI.resetCalls) != 0 {
		t.Fatalf("x issued a call: cmd=%v calls=%v", cmd != nil, client.loginAPI.resetCalls)
	}
	c := loginScreenOf(t, m).confirm
	if c.title != "Reset backoff?" || c.message != "The login delay for this account ends now." || c.target != "alice" || c.targetDetail != "user, 7 failed logins, waiting 4 min" ||
		c.consequence != "Clears the failure counter for alice. alice can try again immediately. Active access tokens are not affected." || c.buttonLabel != "Reset backoff" || c.submitting != "Resetting backoff…" {
		t.Fatalf("confirm = %+v", c)
	}
	out := overviewRender(m)
	for _, want := range []string{
		"Reset backoff?", "The login delay for this account ends now.",
		"alice", "user, 7 failed logins, waiting 4 min", "Clears the failure counter for alice.",
		"Reset backoff", "y / enter", "Cancel", "esc",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "y / enter confirm") || !strings.Contains(out, "esc cancel") {
		t.Errorf("confirm footer:\n%s", out)
	}
}

func TestLoginResetConfirmForAnUnknownNameUsesTheNotesWording(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	m, _ = loginSend(t, m, testKeyDown())
	m, _ = loginSend(t, m, testKeyRunes("x"))
	c := loginScreenOf(t, m).confirm
	if c.message != "The login delay for this name ends now." || c.target != "unknown #ab12cd" || c.targetDetail != "no such account, 12 failed logins, waiting 5 min" ||
		c.consequence != "Clears the failure counter for unknown #ab12cd. Anyone using that name can try again immediately. Active access tokens are not affected." {
		t.Fatalf("confirm = %+v", c)
	}
	out := overviewRender(m)
	for _, want := range []string{
		"The login delay for this name ends now.", "unknown #ab12cd", "no such account, 12 failed logins, waiting 5 min", "Clears the failure counter for unknown #ab12cd.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hash:") || strings.Contains(out, "ab12cd34") {
		t.Errorf("the confirm shows the full hash label:\n%s", out)
	}
}

func TestLoginResetEnterAndYConfirmOnceAndEscCancels(t *testing.T) {
	t.Parallel()
	for name, confirm := range map[string]keyMsg{"enter": testKeyEnter(), "y": testKeyRunes("y")} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := loginScreenClient()
			m := loginModelAt(t, client, 200, 40)
			m, _ = loginSend(t, m, testKeyRunes("x"))
			m, cmd := loginSend(t, m, confirm)
			if cmd == nil {
				t.Fatal("confirm returned no command")
			}
			// A second confirm while the first is in flight sends nothing.
			m, again := loginSend(t, m, testKeyEnter())
			m, again2 := loginSend(t, m, testKeyRunes("y"))
			if again != nil || again2 != nil {
				t.Fatalf("double confirm issued a second command")
			}
			if out := overviewRender(m); !strings.Contains(out, "Resetting backoff…") {
				t.Errorf("no progress line while resetting:\n%s", out)
			}
			m = loginDeliver(t, m, cmd)
			if len(client.loginAPI.resetCalls) != 1 || client.loginAPI.resetCalls[0] != "alice" {
				t.Fatalf("reset calls = %v, want exactly [alice]", client.loginAPI.resetCalls)
			}
			out := overviewRender(m)
			if !strings.Contains(out, "Backoff reset for alice. alice can try again now.") || strings.Contains(out, "Reset backoff?") {
				t.Errorf("result:\n%s", out)
			}
		})
	}
	t.Run("esc", func(t *testing.T) {
		t.Parallel()
		client := loginScreenClient()
		m := loginModelAt(t, client, 200, 40)
		m, _ = loginSend(t, m, testKeyRunes("x"))
		m, cmd := loginSend(t, m, testKeyEsc())
		if cmd != nil && m.screen != screenAdminLoginSecurity {
			t.Fatalf("esc left the screen")
		}
		if m.screen != screenAdminLoginSecurity || strings.Contains(overviewRender(m), "Reset backoff?") || len(client.loginAPI.resetCalls) != 0 {
			t.Fatalf("esc: screen=%q calls=%v", m.screen, client.loginAPI.resetCalls)
		}
	})
	t.Run("other keys and a stray Y never confirm", func(t *testing.T) {
		t.Parallel()
		client := loginScreenClient()
		m := loginModelAt(t, client, 200, 40)
		m, _ = loginSend(t, m, testKeyRunes("x"))
		for _, k := range []keyMsg{testKeyRunes("Y"), testKeyAlt("y"), testKeyCtrl('y'), testKeyRunes("n"), testKeySpace(), testKeyTab()} {
			var cmd tea.Cmd
			if m, cmd = loginSend(t, m, k); cmd != nil {
				t.Fatalf("key %q issued a command", keyString(k))
			}
		}
		if len(client.loginAPI.resetCalls) != 0 {
			t.Fatalf("calls = %v", client.loginAPI.resetCalls)
		}
	})
}

func TestLoginResetOfAnUnknownNameSendsTheHashLabel(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	m, _ = loginSend(t, m, testKeyDown())
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	m = loginDeliver(t, m, cmd)
	if len(client.loginAPI.resetCalls) != 1 || client.loginAPI.resetCalls[0] != "hash:ab12cd34" {
		t.Fatalf("reset calls = %v, want the hash label", client.loginAPI.resetCalls)
	}
	out := overviewRender(m)
	if !strings.Contains(out, "Backoff reset for unknown #ab12cd. Anyone using that name can try again now.") {
		t.Errorf("notice:\n%s", out)
	}
	if strings.Contains(out, "hash:") {
		t.Errorf("hash label on screen:\n%s", out)
	}
}

func TestLoginResetSuccessRemovesTheRowAndRefreshesTheSummary(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	reads := client.loginAPI.summaryCalls
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	m = loginDeliver(t, m, cmd)
	s := loginScreenOf(t, m)
	if len(s.backoff.Accounts) != 1 || s.backoff.Accounts[0].Account != "hash:ab12cd34" {
		t.Fatalf("accounts = %+v, want only the unknown one left", s.backoff.Accounts)
	}
	if client.loginAPI.summaryCalls != reads+1 || s.summary.AccountsInBackoff != 1 {
		t.Errorf("summary reads %d -> %d, waiting=%d: the summary was not refreshed", reads, client.loginAPI.summaryCalls, s.summary.AccountsInBackoff)
	}
	if s.focus != loginPanelWaiting {
		t.Errorf("focus left the waiting panel")
	}
}

func TestLoginResetGoneShowsACleanNoticeAndRefreshes(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	// Somebody else already reset alice: the server has no state for her.
	client.loginAPI.backoff.Accounts = client.loginAPI.backoff.Accounts[1:]
	client.loginAPI.summary.AccountsInBackoff = 1
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	m = loginDeliver(t, m, cmd)
	out := overviewRender(m)
	if !strings.Contains(out, "alice is no longer waiting. The list was refreshed.") {
		t.Errorf("gone notice:\n%s", out)
	}
	if strings.Contains(out, "Could not reset") || strings.Contains(out, "login backoff not found") {
		t.Errorf("a raw error leaked:\n%s", out)
	}
	if got := loginScreenOf(t, m).backoff.Accounts; len(got) != 1 || got[0].Account != "hash:ab12cd34" {
		t.Errorf("list not refreshed: %+v", got)
	}
}

func TestLoginResetErrorKeepsTheListAndSaysWhy(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.resetErr = errors.New("service unavailable")
	m := loginModelAt(t, client, 200, 40)
	reads := client.loginAPI.backoffCalls
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	m = loginDeliver(t, m, cmd)
	out := overviewRender(m)
	if !strings.Contains(out, "Could not reset backoff: service unavailable. The list stays as it was.") {
		t.Errorf("error notice:\n%s", out)
	}
	if len(loginScreenOf(t, m).backoff.Accounts) != 2 || client.loginAPI.backoffCalls != reads {
		t.Errorf("the list changed or was re-read after a failed reset")
	}
	if strings.Contains(out, "Reset backoff?") {
		t.Errorf("dialog still open after the failure:\n%s", out)
	}
	// And the operator can try again.
	client.loginAPI.resetErr = nil
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd = loginSend(t, m, testKeyEnter())
	m = loginDeliver(t, m, cmd)
	if !strings.Contains(overviewRender(m), "Backoff reset for alice.") {
		t.Errorf("retry did not succeed:\n%s", overviewRender(m))
	}
}

func TestLoginResetExpiredSessionEndsTheSession(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.resetErr = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	m := loginModelAt(t, client, 200, 40)
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	m = loginDeliver(t, m, cmd)
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login screen after an expired session", m.screen)
	}
}

func TestLoginResetResultOfAnotherScreenInstanceIsDropped(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	s := loginScreenOf(t, m)
	next, cmd, _ := s.Update(screenEnv{}, loginSecurityResetMsg{gen: s.gen + 1000, account: "alice", display: "alice"})
	if cmd != nil || len(next.(loginSecurityScreen).backoff.Accounts) != 2 || next.(loginSecurityScreen).notice != "" {
		t.Errorf("a result of another instance changed the screen")
	}
}

func TestLoginResetKeyIsHiddenWhereItDoesNotApply(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	if out := overviewRender(m); !strings.Contains(out, "x reset") {
		t.Errorf("x reset hint missing on the waiting panel:\n%s", out)
	}
	m = overviewPress(t, m, testKeyTab())
	out := overviewRender(m)
	if strings.Contains(out, "x reset") {
		t.Errorf("x reset hint shown on the failures panel:\n%s", out)
	}
	m, cmd := loginSend(t, m, testKeyRunes("x"))
	if cmd != nil || strings.Contains(overviewRender(m), "Reset backoff?") {
		t.Errorf("x did something on the failures panel")
	}
	// No accounts: nothing to reset, no hint, x is a no-op.
	client.loginAPI.backoff.Accounts = nil
	m = loginModelAt(t, client, 200, 40)
	if out := overviewRender(m); strings.Contains(out, "x reset") {
		t.Errorf("x reset hint shown with nobody waiting:\n%s", out)
	}
	if m, cmd = loginSend(t, m, testKeyRunes("x")); cmd != nil || strings.Contains(overviewRender(m), "Reset backoff?") {
		t.Errorf("x opened a dialog with nobody waiting")
	}
}

func TestLoginResetXHintInTheBorderOnlyWhileTheWaitingPanelIsFocused(t *testing.T) {
	t.Parallel()
	m := loginModelAt(t, loginScreenClient(), 200, 40)
	border := func(out string) string {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "ACCOUNTS WAITING ─") {
				return line
			}
		}
		return ""
	}
	if line := border(overviewRender(m)); !strings.Contains(line, "x reset") {
		t.Errorf("border hint missing while focused: %q", line)
	}
	m = overviewPress(t, m, testKeyTab())
	if line := border(overviewRender(m)); strings.Contains(line, "x reset") {
		t.Errorf("border hint shown while the panel is not focused: %q", line)
	}
}

func TestLoginResetBusyIgnoresKeysThatChangeData(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	// Esc closes the dialog while the call is on its way; a new reset and a
	// refresh are still refused until the result lands.
	m, _ = loginSend(t, m, testKeyEsc())
	var again tea.Cmd
	m, _ = loginSend(t, m, testKeyRunes("x"))
	if m, again = loginSend(t, m, testKeyEnter()); again != nil {
		t.Fatalf("a second reset started while one was in flight")
	}
	reads := client.loginAPI.summaryCalls
	if m, again = loginSend(t, m, testKeyRunes("r")); again != nil || client.loginAPI.summaryCalls != reads {
		t.Fatalf("a refresh started while a reset was in flight")
	}
	m = loginDeliver(t, m, cmd)
	if len(client.loginAPI.resetCalls) != 1 {
		t.Fatalf("calls = %v, want one", client.loginAPI.resetCalls)
	}
	_ = m
}

// A read that started before the reset must not bring the account back when it
// lands after it (the sequence floor, as on Edit user).
func TestLoginResetStaleReloadCannotResurrectTheRow(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	// r starts a full read that sees alice still waiting; hold its result.
	m, readCmd := loginSend(t, m, testKeyRunes("r"))
	staleMsg := readCmd()
	// Now alice is reset through the screen. The reset result lands, its own
	// reload is on its way (not run yet), and THEN the stale read arrives.
	m, _ = loginSend(t, m, testKeyRunes("x"))
	m, cmd := loginSend(t, m, testKeyEnter())
	updated, reload := m.Update(cmd())
	m = updated.(Model)
	if reload == nil {
		t.Fatal("the reset result started no reload")
	}
	if got := loginScreenOf(t, m).backoff.Accounts; len(got) != 1 || got[0].Account == "alice" {
		t.Fatalf("accounts after the reset = %+v", got)
	}
	updated, _ = m.Update(staleMsg)
	m = updated.(Model)
	for _, account := range loginScreenOf(t, m).backoff.Accounts {
		if account.Account == "alice" {
			t.Fatalf("a read that started before the reset brought alice back")
		}
	}
	// The reload the reset started then lands and agrees.
	m = loginDeliver(t, m, reload)
	if got := loginScreenOf(t, m).backoff.Accounts; len(got) != 1 || got[0].Account != "hash:ab12cd34" {
		t.Fatalf("accounts after the reload = %+v", got)
	}
}

// Whatever happens to the list while the dialog is open, Enter resets exactly
// the account the dialog named: the confirm is built from the row at open time.
func TestLoginResetConfirmTargetAlwaysEqualsTheExecutedReset(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(7))
	for round := range 200 {
		client := loginScreenClient()
		var accounts []ports.AdminBackoffAccount
		for i := range 2 + rng.Intn(6) {
			name := fmt.Sprintf("user%d", i)
			if rng.Intn(3) == 0 {
				name = fmt.Sprintf("hash:%08x", rng.Uint32())
			}
			accounts = append(accounts, ports.AdminBackoffAccount{Account: name, Failures: 5 + i, RemainingSeconds: 30 + 30*i})
		}
		client.loginAPI.backoff.Accounts = accounts
		client.loginAPI.summary.AccountsInBackoff = len(accounts)
		m := loginModelAt(t, client, 200, 40)
		for range rng.Intn(5) {
			switch rng.Intn(3) {
			case 0:
				m, _ = loginSend(t, m, testKeyDown())
			case 1:
				m, _ = loginSend(t, m, testKeyUp())
			case 2:
				rng.Shuffle(len(client.loginAPI.backoff.Accounts), func(i, j int) {
					a := client.loginAPI.backoff.Accounts
					a[i], a[j] = a[j], a[i]
				})
				m = loginDeliver(t, m, func() tea.Msg { return nil })
				var cmd tea.Cmd
				m, cmd = loginSend(t, m, testKeyRunes("r"))
				m = loginDeliver(t, m, cmd)
			}
		}
		s := loginScreenOf(t, m)
		selected, ok := s.selectedWaiting()
		if !ok {
			t.Fatalf("round %d: no selection", round)
		}
		m, _ = loginSend(t, m, testKeyRunes("x"))
		shown := overviewRender(m)
		if want := loginAccountName(selected.Account); !strings.Contains(shown, want) {
			t.Fatalf("round %d: the dialog does not name %q:\n%s", round, want, shown)
		}
		// The list is reordered and reloaded while the dialog is open.
		if rng.Intn(2) == 0 {
			rng.Shuffle(len(client.loginAPI.backoff.Accounts), func(i, j int) {
				a := client.loginAPI.backoff.Accounts
				a[i], a[j] = a[j], a[i]
			})
			updated, cmd := m.Update(loginSecurityLoadedMsg{gen: loginScreenOf(t, m).gen, seq: loginSecurityLoadSeq.Add(1), full: false,
				summary: client.loginAPI.summary, backoff: client.loginAPI.backoff})
			m = updated.(Model)
			_ = cmd
		}
		var cmd tea.Cmd
		m, cmd = loginSend(t, m, testKeyEnter())
		m = loginDeliver(t, m, cmd)
		calls := client.loginAPI.resetCalls
		if len(calls) != 1 || calls[0] != selected.Account {
			t.Fatalf("round %d: dialog named %q but the reset was %v", round, selected.Account, calls)
		}
	}
}

func TestEventSentenceWordsTheBackoffReset(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		event ports.AdminAccessEvent
		want  string
	}{
		{ports.AdminAccessEvent{Action: "login.backoff.reset", Actor: "admin", Subject: "alice", Detail: "backoff counter cleared"}, "reset the login backoff of alice"},
		{ports.AdminAccessEvent{Action: "login.backoff.reset", Actor: "admin", Subject: "hash:ab12cd34"}, "reset the login backoff of unknown #ab12cd"},
		{ports.AdminAccessEvent{Action: "login.backoff.reset", Actor: "admin"}, "reset the login backoff"},
	} {
		if got := eventSentence(tt.event); got != tt.want {
			t.Errorf("eventSentence(%+v) = %q, want %q", tt.event, got, tt.want)
		}
	}
}

func TestOverviewRecentChangesShowsTheBackoffResetSentence(t *testing.T) {
	t.Parallel()
	client := overviewRichClient()
	client.accessEvents = []ports.AdminAccessEvent{{ID: "e9", At: fixedTestTime.Add(-30 * 60 * 1e9), Action: "login.backoff.reset", Actor: "admin", Subject: "alice", Detail: "backoff counter cleared"}}
	out := overviewRender(overviewModelAt(t, client, 200, 40))
	if !strings.Contains(out, "admin reset the login backoff of alice") {
		t.Errorf("recent changes row:\n%s", out)
	}
	if strings.Contains(out, "login.backoff.reset") || strings.Contains(out, "backoff counter cleared") {
		t.Errorf("raw action or detail shown:\n%s", out)
	}
}

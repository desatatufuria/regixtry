package tui

import (
	"errors"
	"strings"
	"testing"

	"regixtry/internal/ports"
)

// loginOverviewClient is the Overview fake with a login summary and backoff
// state: failures failed logins in 15 minutes and waiting accounts in mode.
func loginOverviewClient(failures int64, waiting int, mode string) *fakeAdminClient {
	client := overviewClient()
	client.loginAPI.summary = ports.AdminLoginSummary{WindowMinutes: 15, Failures: failures, DistinctIPs: 2, DistinctAccounts: 3, AccountsInBackoff: waiting}
	client.loginAPI.backoff = ports.AdminLoginBackoff{Mode: mode, Threshold: 5, BaseSeconds: 2, MaxSeconds: 300, MaxConcurrency: 8}
	return client
}

func loginRowOf(t *testing.T, m Model) (attentionItem, bool) {
	t.Helper()
	for _, item := range overviewScreenOf(t, m).attentionItems(fixedTestTime) {
		if strings.Contains(item.text, "failed login") {
			return item, true
		}
	}
	return attentionItem{}, false
}

func TestOverviewLoginRowTextTonesAndLogModeSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		failures int64
		waiting  int
		mode     string
		text     string
		tone     attentionTone
		shown    bool
	}{
		{name: "quiet is hidden", mode: "enforce"},
		{name: "quiet in log mode is hidden", mode: "log"},
		{name: "singular", failures: 1, waiting: 1, mode: "off", text: "1 failed login in 15 min; 1 account waiting", tone: attentionMuted, shown: true},
		{name: "below the threshold is muted", failures: 19, waiting: 0, mode: "enforce", text: "19 failed logins in 15 min; 0 accounts waiting", tone: attentionMuted, shown: true},
		{name: "at the threshold is a warning", failures: loginWarnFailures, waiting: 0, mode: "enforce", text: "20 failed logins in 15 min; 0 accounts waiting", tone: attentionWarning, shown: true},
		{name: "an account waiting in enforce mode is a warning", failures: 3, waiting: 1, mode: "enforce", text: "3 failed logins in 15 min; 1 account waiting", tone: attentionWarning, shown: true},
		{name: "waiting alone in log mode never warns", failures: 4, waiting: 3, mode: "log", text: "4 failed logins in 15 min; 3 accounts waiting (log mode)", tone: attentionMuted, shown: true},
		{name: "failures warn in log mode too", failures: 42, waiting: 3, mode: "log", text: "42 failed logins in 15 min; 3 accounts waiting (log mode)", tone: attentionWarning, shown: true},
		{name: "waiting with no failures is still shown", failures: 0, waiting: 2, mode: "enforce", text: "0 failed logins in 15 min; 2 accounts waiting", tone: attentionWarning, shown: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item, ok := loginRowOf(t, overviewModelAt(t, loginOverviewClient(tt.failures, tt.waiting, tt.mode), 200, 40))
			if ok != tt.shown {
				t.Fatalf("row shown = %v, want %v", ok, tt.shown)
			}
			if ok && (item.text != tt.text || item.tone != tt.tone) {
				t.Fatalf("row = %q tone %v, want %q tone %v", item.text, item.tone, tt.text, tt.tone)
			}
		})
	}
}

func TestOverviewLoginRowIsTheLastAttentionRowAndRendersInThePanel(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, loginOverviewClient(42, 3, "log"), 200, 40)
	items := overviewScreenOf(t, m).attentionItems(fixedTestTime)
	if len(items) == 0 || !strings.Contains(items[len(items)-1].text, "42 failed logins in 15 min; 3 accounts waiting (log mode)") {
		t.Fatalf("login row is not the last attention row: %+v", items)
	}
	if out := overviewRender(m); !strings.Contains(out, "42 failed logins in 15 min; 3 accounts waiting (log mode)") {
		t.Errorf("row missing from the panel:\n%s", out)
	}
}

func TestOverviewLoginRowTabFocusAndEnterOpensLoginSecurity(t *testing.T) {
	t.Parallel()
	client := loginOverviewClient(42, 3, "enforce")
	m := overviewModelAt(t, client, 200, 40)
	rows := len(overviewScreenOf(t, m).attentionItems(fixedTestTime))
	for range rows {
		m = overviewPress(t, m, testKeyTab())
	}
	if got := overviewScreenOf(t, m).attentionFocus; got != rows {
		t.Fatalf("focus = %d after %d tabs, want the last row %d", got, rows, rows)
	}
	if out := overviewRender(m); !strings.Contains(out, "▸ 42 failed logins in 15 min; 3 accounts waiting") || !strings.Contains(out, "enter open") {
		t.Errorf("focused row not marked with its enter hint:\n%s", out)
	}
	_, cmd, _ := overviewScreenOf(t, m).Update(screenEnv{}, testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter on the row returned no command")
	}
	if msg, ok := cmd().(navigateMsg); !ok || msg.To != screenAdminLoginSecurity {
		t.Fatalf("Enter opened %#v, want navigation to the Login security screen", cmd())
	}
}

// A failed login read never breaks the rest of the Overview: the row simply is
// not there, nothing else changes and no error line is added.
func TestOverviewLoginReadFailuresOnlyHideTheRow(t *testing.T) {
	t.Parallel()
	base := overviewModelAt(t, overviewRichClient(), 200, 40)
	want := overviewRender(base)
	for name, mutate := range map[string]func(*fakeAdminClient){
		"summary":     func(c *fakeAdminClient) { c.loginAPI.summaryErr = errors.New("boom") },
		"backoff":     func(c *fakeAdminClient) { c.loginAPI.backoffErr = errors.New("boom") },
		"not allowed": func(c *fakeAdminClient) { c.loginAPI.summaryErr = errors.New("403 global administrator required") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := overviewRichClient()
			client.loginAPI.summary = ports.AdminLoginSummary{Failures: 50, AccountsInBackoff: 2}
			client.loginAPI.backoff = ports.AdminLoginBackoff{Mode: "enforce"}
			mutate(client)
			m := overviewModelAt(t, client, 200, 40)
			if _, ok := loginRowOf(t, m); ok {
				t.Fatal("row shown although a login read failed")
			}
			got := overviewRender(m)
			if got != want {
				t.Errorf("the Overview changed because a login read failed:\n--- want\n%s\n--- got\n%s", want, got)
			}
			if strings.Contains(got, "boom") || strings.Contains(got, "Could not check login") {
				t.Errorf("an error note leaked into the panel:\n%s", got)
			}
		})
	}
}

func TestOverviewLoginRowLoadsWithTheOtherReadsOnce(t *testing.T) {
	t.Parallel()
	client := loginOverviewClient(5, 0, "enforce")
	overviewModelAt(t, client, 150, 24)
	if client.loginAPI.summaryCalls != 1 || client.loginAPI.backoffCalls != 1 || len(client.loginAPI.failureReads) != 0 {
		t.Errorf("reads: summary=%d backoff=%d failures=%d, want 1, 1 and none", client.loginAPI.summaryCalls, client.loginAPI.backoffCalls, len(client.loginAPI.failureReads))
	}
}

func TestOverviewExpiredSessionInTheLoginReadEndsTheSession(t *testing.T) {
	t.Parallel()
	client := loginOverviewClient(5, 0, "enforce")
	client.loginAPI.summaryErr = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	m := overviewModelAt(t, client, 150, 24)
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login screen after an expired session", m.screen)
	}
}

func TestOverviewLoginRowShowsTheLatestLoadAfterRefresh(t *testing.T) {
	t.Parallel()
	client := loginOverviewClient(42, 3, "enforce")
	m := overviewModelAt(t, client, 200, 40)
	client.loginAPI.summary = ports.AdminLoginSummary{}
	updated, cmd := m.Update(navigateMsg{To: screenAdminOverview})
	m = runCmd(t, updated.(Model), cmd)
	if _, ok := loginRowOf(t, m); ok {
		t.Fatal("row still shown after a reload with nothing to report")
	}
}

func TestOverviewFooterNamesTheTabMoveOnlyWhileAnAttentionRowIsFocused(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, loginOverviewClient(42, 3, "enforce"), 200, 40)
	if out := overviewRender(m); !strings.Contains(out, "tab attention") || strings.Contains(out, "next attention row") {
		t.Errorf("unfocused footer changed:\n%s", out)
	}
	m = overviewPress(t, m, testKeyTab())
	if out := overviewRender(m); !strings.Contains(out, "tab next attention row") || !strings.Contains(out, "enter open") {
		t.Errorf("focused footer missing the tab hint:\n%s", out)
	}
}

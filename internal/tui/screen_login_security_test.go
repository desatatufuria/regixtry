package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

var loginGeometrySizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

// loginScreenClient serves the screen: enforce mode, alice and an unknown name
// waiting, and five aggregated failures newest first.
func loginScreenClient() *fakeAdminClient {
	client := &fakeAdminClient{}
	client.loginAPI.summary = ports.AdminLoginSummary{WindowMinutes: 15, Failures: 42, DistinctIPs: 5, DistinctAccounts: 9, AccountsInBackoff: 2}
	client.loginAPI.backoff = ports.AdminLoginBackoff{
		Mode: loginModeEnforce, Threshold: 5, BaseSeconds: 2, MaxSeconds: 300, MaxConcurrency: 8,
		Accounts: []ports.AdminBackoffAccount{
			{Account: "alice", Failures: 7, RemainingSeconds: 240},
			{Account: "hash:ab12cd34", Failures: 12, RemainingSeconds: 300},
		},
	}
	at := func(minutes int) time.Time { return fixedTestTime.Add(-time.Duration(minutes) * time.Minute) }
	day := fixedTestTime.Add(-10 * time.Hour)
	client.loginAPI.failures = []ports.LoginFailureRecord{
		{WindowStart: at(5), LastSeen: at(1), Endpoint: "/auth/token", Reason: "wrong password or unknown admin credential token", Account: "alice", IP: "203.0.113.7", Count: 7},
		{WindowStart: at(10), LastSeen: at(6), Endpoint: "/auth/token", Reason: "unknown user", Account: "hash:ab12cd34", IP: "198.51.100.4", Count: 12},
		{WindowStart: at(15), LastSeen: at(11), Endpoint: "/auth/token", Reason: "throttled: account backoff", Account: "hash:ab12cd34", IP: "198.51.100.4", Count: 3},
		{WindowStart: at(20), LastSeen: at(16), Endpoint: "/v2", Reason: "invalid credentials", IP: "2001:db8:85a3:0:0:8a2e:370:7334", Count: 2},
		{WindowStart: day, LastSeen: day, Endpoint: "/admin/v1", Reason: "invalid credentials", IP: "192.0.2.9", Count: 1},
	}
	return client
}

// loginModelAt returns a model on the Login security screen at the given size
// with the screen's own reads already applied.
func loginModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, client)
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenAdminLoginSecurity})
	return runCmd(t, updated.(Model), cmd)
}

func loginScreenOf(t *testing.T, m Model) loginSecurityScreen {
	t.Helper()
	s, ok := m.adminScreens[slotLoginSecurity].(loginSecurityScreen)
	if !ok {
		t.Fatalf("slot holds %T, want loginSecurityScreen", m.adminScreens[slotLoginSecurity])
	}
	return s
}

func loginRowsAndWidths(t *testing.T, out string, w, h int, label string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Errorf("%s %dx%d: %d rows", label, w, h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("%s %dx%d row %d is %d columns wide: %q", label, w, h, i, got, line)
		}
	}
}

func TestLoginSecurityIsTheLastEntryUnderSecurity(t *testing.T) {
	t.Parallel()
	for _, group := range shellNavGroups {
		if group.Title != "SECURITY" {
			continue
		}
		last := group.Items[len(group.Items)-1]
		if last.ID != shellNavLoginSecurity || last.Label != "Login security" || group.Items[len(group.Items)-2].Label != "Signing policy" {
			t.Fatalf("SECURITY items = %+v, want Login security after Signing policy", group.Items)
		}
		return
	}
	t.Fatal("no SECURITY group")
}

func TestLoginSecurityScreenShowsTheBoardContent(t *testing.T) {
	t.Parallel()
	out := overviewRender(loginModelAt(t, loginScreenClient(), 200, 40))
	for _, want := range []string{
		"Admin › Security › Login security", "Login security", "backoff mode: enforce",
		"FAILED LOGINS, 15 MIN", "DISTINCT IPS", "DISTINCT ACCOUNTS", "ACCOUNTS WAITING",
		"EFFECTIVE SETTINGS", "Set by service flags. Read-only here.",
		"Backoff mode", "Failure threshold", "5 failures per account", "Base delay", "under 1 min", "Max delay", "5 min",
		"Concurrency limit", "8 logins checked at once", "Trusted proxies", "configured: no",
		"ACCOUNTS WAITING", "ACCOUNT", "FAILS", "TIME LEFT", "alice", "unknown #ab12cd", "4 min",
		"Time left grows up to the max delay.",
		"RECENT FAILURES", "LAST SEEN", "ENDPOINT", "REASON", "IP", "COUNT", "/ filter",
		"bad password", "unknown user", "throttled", "token", "admin", "newest first  ▾ 1-5 of 5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("screen is missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"hash:", "ab12cd34", "seconds", "wrong password or unknown admin"} {
		if strings.Contains(out, banned) {
			t.Errorf("screen shows %q:\n%s", banned, out)
		}
	}
	// Stripped order of the strip labels.
	order := []string{"FAILED LOGINS, 15 MIN", "DISTINCT IPS", "DISTINCT ACCOUNTS"}
	last := -1
	for _, label := range order {
		i := strings.Index(out, label)
		if i < last {
			t.Errorf("strip label %q out of order", label)
		}
		last = i
	}
}

func TestLoginSecurityFailuresAreNewestFirstWithBoardTimes(t *testing.T) {
	t.Parallel()
	out := overviewRender(loginModelAt(t, loginScreenClient(), 200, 40))
	// fixedTestTime is 14 Mar 2026 09:30 UTC: 1 minute before is 09:29, ten hours before is 23:30 of the 13th.
	if !strings.Contains(out, "09:29") || !strings.Contains(out, "13 Mar 23:30") {
		t.Errorf("times are not 'HH:MM' for today and 'D Mon HH:MM' otherwise:\n%s", out)
	}
	if strings.Index(out, "09:29") > strings.Index(out, "13 Mar 23:30") {
		t.Errorf("the older failure is listed first:\n%s", out)
	}
}

func TestLoginSecurityLogModeAndOffModeBannersAndWouldWait(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.backoff.Mode = loginModeLog
	out := overviewRender(loginModelAt(t, client, 200, 40))
	for _, want := range []string{
		"Backoff is in log mode: failures are counted but nobody is delayed. Start the service with -login-backoff-mode=enforce to delay repeated failures.",
		"WOULD WAIT", "log only, nobody is delayed", "backoff mode: log",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log mode screen is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TIME LEFT") {
		t.Errorf("log mode keeps the TIME LEFT header:\n%s", out)
	}
	// At 150 columns the banner takes two rows; its words are all there.
	narrow := strings.Join(strings.Fields(overviewRender(loginModelAt(t, client, 150, 24))), " ")
	if !strings.Contains(narrow, "Backoff is in log mode: failures are counted") && !strings.Contains(narrow, "Backoff is in log mode:") {
		t.Errorf("banner missing at 150x24:\n%s", narrow)
	}

	client.loginAPI.backoff.Mode = loginModeOff
	client.loginAPI.backoff.Accounts = nil
	off := overviewRender(loginModelAt(t, client, 200, 40))
	if !strings.Contains(off, "Backoff is off: failures are counted and nobody is delayed.") || !strings.Contains(off, "backoff mode: off") {
		t.Errorf("off mode banner missing:\n%s", off)
	}
}

func TestLoginSecurityEnforceModeHasNoBanner(t *testing.T) {
	t.Parallel()
	out := overviewRender(loginModelAt(t, loginScreenClient(), 200, 40))
	if strings.Contains(out, "Backoff is in log mode") || strings.Contains(out, "Backoff is off") {
		t.Errorf("banner shown in enforce mode:\n%s", out)
	}
}

func TestLoginSecurityTrustedProxiesYes(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.backoff.TrustedProxiesConfigured = true
	if out := overviewRender(loginModelAt(t, client, 200, 40)); !strings.Contains(out, "configured: yes") {
		t.Errorf("trusted proxies yes missing:\n%s", out)
	}
}

func TestLoginSecurityStatesLoadingErrorEmptyAndNotAllowed(t *testing.T) {
	t.Parallel()
	// Loading: the screen before its reads land.
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, loginScreenClient())
	m.now = func() time.Time { return fixedTestTime }
	m = resizeModel(t, m, 200, 40)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedTestTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, _ := m.Update(navigateMsg{To: screenAdminLoginSecurity})
	loading := overviewRender(updated.(Model))
	if !strings.Contains(loading, "Loading login security…") || !strings.Contains(loading, "…") {
		t.Errorf("loading state:\n%s", loading)
	}
	if strings.Contains(loading, "r refresh") || strings.Contains(loading, "tab next panel") || !strings.Contains(loading, "esc back") || !strings.Contains(loading, "q quit") {
		t.Errorf("loading footer must only offer esc and q:\n%s", loading)
	}

	// Error, then retry.
	client := loginScreenClient()
	client.loginAPI.summaryErr = errors.New("connection refused")
	m = loginModelAt(t, client, 200, 40)
	if out := overviewRender(m); !strings.Contains(out, "Could not load login security: connection refused. r retry") || !strings.Contains(out, "r retry") {
		t.Errorf("error state:\n%s", out)
	}
	client.loginAPI.summaryErr = nil
	m = overviewPress(t, m, testKeyRunes("r"))
	if out := overviewRender(m); !strings.Contains(out, "RECENT FAILURES") || strings.Contains(out, "Could not load") {
		t.Errorf("retry did not recover:\n%s", out)
	}

	// Empty.
	empty := &fakeAdminClient{}
	empty.loginAPI.backoff = ports.AdminLoginBackoff{Mode: loginModeEnforce, Threshold: 5, BaseSeconds: 2, MaxSeconds: 300, MaxConcurrency: 8}
	out := overviewRender(loginModelAt(t, empty, 200, 40))
	if !strings.Contains(out, "No failed logins in the last 24 hours.") || !strings.Contains(out, "Nobody is waiting.") {
		t.Errorf("empty state:\n%s", out)
	}

	// Not allowed.
	denied := loginScreenClient()
	denied.loginAPI.summaryErr = adminStatusError{status: 403, message: "x", detail: "global administrator required"}
	out = overviewRender(loginModelAt(t, denied, 200, 40))
	if !strings.Contains(out, "Only global administrators can see login security.") || !strings.Contains(out, "Ask a global administrator to open this screen.") {
		t.Errorf("not-allowed state:\n%s", out)
	}
	if strings.Contains(out, "EFFECTIVE SETTINGS") {
		t.Errorf("panels shown to a non-admin:\n%s", out)
	}
}

func TestLoginSecurityReadsEverythingOnceAndRefreshRereads(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	m := loginModelAt(t, client, 200, 40)
	if client.loginAPI.summaryCalls != 1 || client.loginAPI.backoffCalls != 1 || len(client.loginAPI.failureReads) != 1 {
		t.Fatalf("reads = %d/%d/%d, want one of each", client.loginAPI.summaryCalls, client.loginAPI.backoffCalls, len(client.loginAPI.failureReads))
	}
	q := client.loginAPI.failureReads[0]
	if q.Limit != loginFailuresPageSize || q.Cursor != "" || !q.From.Equal(fixedTestTime.Add(-24*time.Hour)) {
		t.Errorf("first failures read = %+v, want the last 24 hours, one page", q)
	}
	client.loginAPI.summary.Failures = 77
	m = overviewPress(t, m, testKeyRunes("r"))
	if client.loginAPI.summaryCalls != 2 || client.loginAPI.backoffCalls != 2 || len(client.loginAPI.failureReads) != 2 {
		t.Errorf("after r: reads = %d/%d/%d, want two of each", client.loginAPI.summaryCalls, client.loginAPI.backoffCalls, len(client.loginAPI.failureReads))
	}
	if out := overviewRender(m); !strings.Contains(out, "77") {
		t.Errorf("refresh did not update the strip:\n%s", out)
	}
}

func TestLoginSecurityTabMovesBetweenPanelsAndArrowsMoveTheFocusedOne(t *testing.T) {
	t.Parallel()
	m := loginModelAt(t, loginScreenClient(), 200, 40)
	if s := loginScreenOf(t, m); s.focus != loginPanelWaiting || s.selWaiting != 0 {
		t.Fatalf("starts on %v/%d, want the waiting panel, first row", s.focus, s.selWaiting)
	}
	m = overviewPress(t, m, testKeyDown())
	if s := loginScreenOf(t, m); s.selWaiting != 1 || s.selFail != 0 {
		t.Fatalf("down moved waiting=%d failures=%d", s.selWaiting, s.selFail)
	}
	m = overviewPress(t, m, testKeyTab(), testKeyDown(), testKeyDown())
	if s := loginScreenOf(t, m); s.focus != loginPanelFailures || s.selFail != 2 || s.selWaiting != 1 {
		t.Fatalf("after tab+down+down: focus %v waiting=%d failures=%d", s.focus, s.selWaiting, s.selFail)
	}
	m = overviewPress(t, m, testKeyTab())
	if s := loginScreenOf(t, m); s.focus != loginPanelWaiting {
		t.Fatalf("tab did not wrap back, focus %v", s.focus)
	}
	m = overviewPress(t, m, testKeyShiftTab())
	if s := loginScreenOf(t, m); s.focus != loginPanelFailures {
		t.Fatalf("shift+tab focus %v", s.focus)
	}
}

func TestLoginSecurityFooterHintsFollowTheFocusedPanel(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.pageSize = 2
	m := loginModelAt(t, client, 200, 40)
	out := overviewRender(m)
	for _, want := range []string{"↑↓ move", "tab next panel", "/ filter", "r refresh", "esc back", "q quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("waiting footer missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pgup/pgdn") || strings.Contains(out, "n load more") {
		t.Errorf("failures-only hints shown on the waiting panel:\n%s", out)
	}
	m = overviewPress(t, m, testKeyTab())
	out = overviewRender(m)
	if !strings.Contains(out, "pgup/pgdn page") || !strings.Contains(out, "n load more") {
		t.Errorf("failures footer missing paging hints:\n%s", out)
	}
	client.loginAPI.pageSize = 0
	m = loginModelAt(t, client, 200, 40)
	m = overviewPress(t, m, testKeyTab())
	if out := overviewRender(m); strings.Contains(out, "n load more") {
		t.Errorf("'n load more' offered with no further page:\n%s", out)
	}
}

func TestLoginSecurityPagesFailuresByCursorOnScrollAndN(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.pageSize = 2
	m := loginModelAt(t, client, 200, 40)
	if got := len(loginScreenOf(t, m).failures); got != 2 || loginScreenOf(t, m).next == "" {
		t.Fatalf("first page holds %d rows, next=%q", got, loginScreenOf(t, m).next)
	}
	if out := overviewRender(m); !strings.Contains(out, "newest first  ▾ 1-2 of 2+") {
		t.Errorf("bottom border does not say more is coming:\n%s", out)
	}
	// n loads the next page with the cursor of the previous one.
	m = overviewPress(t, m, testKeyTab(), testKeyRunes("n"))
	reads := client.loginAPI.failureReads
	if len(reads) != 2 || reads[1].Cursor != "c2" || !reads[1].From.Equal(reads[0].From) {
		t.Fatalf("reads = %+v, want a second read with cursor c2 and the same window", reads)
	}
	if got := len(loginScreenOf(t, m).failures); got != 4 {
		t.Fatalf("rows after n = %d, want 4", got)
	}
	// Scrolling past the last loaded row loads the last page.
	m = overviewPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown())
	if s := loginScreenOf(t, m); len(s.failures) != 5 || s.next != "" {
		t.Fatalf("after scrolling: %d rows, next=%q, want all 5 and none after", len(s.failures), s.next)
	}
	if len(client.loginAPI.failureReads) != 3 {
		t.Errorf("reads = %d, want exactly three pages", len(client.loginAPI.failureReads))
	}
	// n with nothing more does not read again.
	overviewPress(t, m, testKeyRunes("n"))
	if len(client.loginAPI.failureReads) != 3 {
		t.Errorf("n read again with no next page")
	}
}

func TestLoginSecurityLoadMoreFailureKeepsRowsAndRetriesWithN(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.pageSize = 2
	client.loginAPI.pageErr = errors.New("database busy")
	m := loginModelAt(t, client, 200, 40)
	m = overviewPress(t, m, testKeyTab(), testKeyRunes("n"))
	if s := loginScreenOf(t, m); len(s.failures) != 2 || !strings.Contains(overviewRender(m), "Could not load more failures: database busy. n retry") {
		t.Fatalf("rows=%d; screen:\n%s", len(s.failures), overviewRender(m))
	}
	client.loginAPI.pageErr = nil
	m = overviewPress(t, m, testKeyRunes("n"))
	if got := len(loginScreenOf(t, m).failures); got != 4 {
		t.Errorf("retry loaded %d rows, want 4", got)
	}
}

func TestLoginSecurityStalePageAndStaleLoadAreDropped(t *testing.T) {
	t.Parallel()
	client := loginScreenClient()
	client.loginAPI.pageSize = 2
	m := loginModelAt(t, client, 200, 40)
	s := loginScreenOf(t, m)
	// A page that belongs to an older read lands after a refresh: dropped.
	stale := loginSecurityPageMsg{gen: s.gen, base: s.appliedSeq - 1, cursor: s.next, page: ports.AdminLoginFailurePage{Failures: []ports.LoginFailureRecord{{Account: "ghost"}}}}
	s.loadingMore = true
	next, _, _ := s.Update(screenEnv{}, stale)
	if got := len(next.(loginSecurityScreen).failures); got != 2 {
		t.Errorf("stale page applied: %d rows", got)
	}
	// A read older than the last applied one never overwrites it.
	old := loginSecurityLoadedMsg{gen: s.gen, seq: s.floor - 1, full: true, summary: ports.AdminLoginSummary{Failures: 999}, backoff: client.loginAPI.backoff}
	next, _, _ = s.Update(screenEnv{}, old)
	if got := next.(loginSecurityScreen).summary.Failures; got == 999 {
		t.Errorf("stale load applied")
	}
	// A message of another screen instance is ignored.
	other := loginSecurityLoadedMsg{gen: s.gen + 1000, seq: s.floor + 5, full: true, summary: ports.AdminLoginSummary{Failures: 999}}
	if next, _, _ = s.Update(screenEnv{}, other); next.(loginSecurityScreen).summary.Failures == 999 {
		t.Errorf("a result of another instance applied")
	}
}

func TestLoginSecurityFilterMatchesAccountIPOrReasonAsASubstring(t *testing.T) {
	t.Parallel()
	typeText := func(m Model, text string) Model {
		for _, r := range text {
			m = overviewPress(t, m, testKeyRunes(string(r)))
		}
		return m
	}
	m := overviewPress(t, loginModelAt(t, loginScreenClient(), 200, 40), testKeyRunes("/"))
	if out := overviewRender(m); !strings.Contains(out, "filter: ") || !strings.Contains(out, "enter apply") || !strings.Contains(out, "esc clear") || strings.Contains(out, "r refresh") {
		t.Errorf("filter input footer:\n%s", out)
	}
	for text, want := range map[string]int{"alic": 1, "198.51": 2, "throttled": 1, "bad pass": 1, "unknown #ab": 2, "zzz": 0, "": 5} {
		m := overviewPress(t, loginModelAt(t, loginScreenClient(), 200, 40), testKeyRunes("/"))
		m = overviewPress(t, typeText(m, text), testKeyEnter())
		if s := loginScreenOf(t, m); s.filtering || len(s.visibleFailures()) != want {
			t.Errorf("filter %q: filtering=%v rows=%d, want %d", text, s.filtering, len(s.visibleFailures()), want)
		}
	}
	// Enter applies and keeps the filter in the border; esc clears it.
	m = overviewPress(t, typeText(m, "alice"), testKeyEnter())
	if out := overviewRender(m); !strings.Contains(out, "/ filter: alice") || strings.Contains(out, "198.51.100.4") {
		t.Errorf("applied filter:\n%s", out)
	}
	m = overviewPress(t, m, testKeyRunes("/"), testKeyEsc())
	if s := loginScreenOf(t, m); s.filter != "" || s.filtering || len(s.visibleFailures()) != 5 {
		t.Errorf("esc did not clear the filter: %+v", s.filter)
	}
	if m.screen != screenAdminLoginSecurity {
		t.Errorf("esc while filtering left the screen: %q", m.screen)
	}
	// No match says so.
	m = overviewPress(t, typeText(overviewPress(t, m, testKeyRunes("/")), "nomatch"), testKeyEnter())
	if out := overviewRender(m); !strings.Contains(out, "No failures match the filter.") {
		t.Errorf("no-match state:\n%s", out)
	}
}

func TestLoginSecurityEscGoesBackToTheOverviewAndQuitKeyWorks(t *testing.T) {
	t.Parallel()
	m := overviewPress(t, loginModelAt(t, loginScreenClient(), 200, 40), testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("esc went to %q", m.screen)
	}
	m = loginModelAt(t, loginScreenClient(), 200, 40)
	if _, cmd := m.Update(testKeyRunes("q")); cmd == nil {
		t.Errorf("q did not quit")
	}
}

func TestLoginSecuritySidebarClickAndOverviewEnterOpenIt(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, loginScreenClient(), 150, 24)
	for i, item := range shellNavItems() {
		if item.ID != shellNavLoginSecurity {
			continue
		}
		m = overviewPress(t, m)
		for range i {
			m = overviewPress(t, m, testKeyDown())
		}
	}
	m = overviewPress(t, m, testKeyEnter())
	if m.screen != screenAdminLoginSecurity {
		t.Fatalf("Enter on the sidebar item opened %q", m.screen)
	}
	if !strings.Contains(overviewRender(m), "FAILED LOGINS, 15 MIN") {
		t.Errorf("screen not loaded:\n%s", overviewRender(m))
	}
}

func TestLoginSecurityGeometryAtEverySize(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a-very-long-account-name-", 8)
	build := func(accounts, failures int) *fakeAdminClient {
		client := loginScreenClient()
		client.loginAPI.backoff.Accounts = nil
		client.loginAPI.failures = nil
		for i := range accounts {
			name := fmt.Sprintf("user%d", i)
			if i%3 == 0 {
				name = long + fmt.Sprint(i)
			}
			if i%5 == 1 {
				name = fmt.Sprintf("hash:%08x", i*7919)
			}
			client.loginAPI.backoff.Accounts = append(client.loginAPI.backoff.Accounts, ports.AdminBackoffAccount{Account: name, Failures: 5 + i, RemainingSeconds: 30 + i*60})
		}
		for i := range failures {
			ip := fmt.Sprintf("203.0.113.%d", i%250)
			account := fmt.Sprintf("user%d", i)
			if i%2 == 0 {
				ip = "2001:0db8:85a3:0000:0000:8a2e:0370:7334"
				account = long
			}
			if i%7 == 3 {
				account = fmt.Sprintf("hash:%08x", i)
			}
			client.loginAPI.failures = append(client.loginAPI.failures, ports.LoginFailureRecord{
				WindowStart: fixedTestTime.Add(-time.Duration(i) * time.Minute), LastSeen: fixedTestTime.Add(-time.Duration(i) * time.Minute),
				Endpoint: "/auth/token", Reason: "wrong password or unknown admin credential token", Account: account, IP: ip, Count: int64(i + 1)})
		}
		client.loginAPI.summary.AccountsInBackoff = accounts
		return client
	}
	for _, size := range loginGeometrySizes {
		for _, counts := range [][2]int{{0, 0}, {1, 1}, {100, 100}} {
			for _, mode := range []string{loginModeEnforce, loginModeLog, loginModeOff} {
				client := build(counts[0], counts[1])
				client.loginAPI.backoff.Mode = mode
				m := loginModelAt(t, client, size.w, size.h)
				label := fmt.Sprintf("%d accounts, %d failures, %s", counts[0], counts[1], mode)
				out := overviewRender(m)
				loginRowsAndWidths(t, out, size.w, size.h, label)
				if !strings.Contains(out, "RECENT FAILURES") || !strings.Contains(out, "ACCOUNTS WAITING") {
					t.Errorf("%s %dx%d: a panel is missing:\n%s", label, size.w, size.h, out)
				}
				if counts[0] > 0 || counts[1] > 0 {
					// Walk both panels to the end: the window must keep fitting.
					for _, k := range []keyMsg{testKeyTab(), testKeyEnd(), testKeyPgDown(), testKeyPgDown(), testKeyDown(), testKeyTab(), testKeyDown(), testKeyDown(), testKeyPgDown()} {
						m = overviewPress(t, m, k)
						loginRowsAndWidths(t, overviewRender(m), size.w, size.h, label+" after "+keyString(k))
					}
				}
			}
		}
	}
}

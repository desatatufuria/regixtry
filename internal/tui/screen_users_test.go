package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// The user-management screens have their own flag so regenerating their
// goldens can never touch another screen's.
var updateUsersGoldens = flag.Bool("update-users", false, "rewrite the user-management golden files")

var usersViewSizes = []struct{ w, h int }{{150, 24}, {200, 40}}

// geometrySizes are the five terminal sizes every screen must fit.
var geometrySizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

var usersTestNow = time.Date(2026, time.November, 21, 10, 0, 0, 0, time.UTC)

func usersTestUsers() []ports.AdminUser {
	created := time.Date(2026, time.March, 3, 8, 0, 0, 0, time.UTC)
	return []ports.AdminUser{
		{ID: "u-1", Username: "admin", IsAdmin: true, Enabled: true, CreatedAt: created},
		{ID: "u-2", Username: "alice", Enabled: true, CreatedAt: created},
		{ID: "u-3", Username: "bob", IsReadOnly: true, Enabled: true, CreatedAt: created},
		{ID: "u-4", Username: "carol", Enabled: true, CreatedAt: created},
		{ID: "u-5", Username: "dave", Enabled: false, CreatedAt: created},
	}
}

func usersTestClient() *fakeAdminClient {
	return &fakeAdminClient{
		users: usersTestUsers(),
		expiringTokens: []ports.AdminExpiringToken{
			{ID: "t-1", Accessor: "acc-1", Name: "laptop", UserID: "u-4", Username: "carol", ExpiresAt: usersTestNow.Add(4 * 24 * time.Hour), CreatedAt: usersTestNow.Add(-26 * 24 * time.Hour)},
			{ID: "t-2", Accessor: "acc-2", Name: "ci", UserID: "u-4", Username: "carol", ExpiresAt: usersTestNow.Add(30 * time.Hour), CreatedAt: usersTestNow.Add(-10 * 24 * time.Hour)},
			{ID: "t-3", Accessor: "acc-3", Name: "phone", UserID: "u-2", Username: "alice", ExpiresAt: usersTestNow.Add(6 * 24 * time.Hour), CreatedAt: usersTestNow.Add(-1 * 24 * time.Hour)},
			{ID: "t-4", Accessor: "acc-4", Name: "bot", UserID: "r-1", Username: "ci-bot", IsRobot: true, ExpiresAt: usersTestNow.Add(2 * 24 * time.Hour), CreatedAt: usersTestNow.Add(-1 * 24 * time.Hour)},
		},
	}
}

func usersTestEnv(client AdminClient, w, h int) screenEnv {
	return screenEnv{
		Client:  client,
		Session: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: usersTestNow.Add(24 * time.Hour)},
		Layout:  consoleLayout{Width: w, Height: h, SectionRows: h - 7},
		Now:     func() time.Time { return usersTestNow },
	}
}

// settleScreen runs cmd and feeds every message it makes back to the screen
// until no command is left; the messages that are NOT for the screen (open
// requests) are returned in order.
func settleScreen(s adminScreen, env screenEnv, cmd tea.Cmd) (adminScreen, []tea.Msg) {
	var outgoing []tea.Msg
	for cmd != nil {
		msg := cmd()
		switch msg.(type) {
		case openUserScreenMsg, openGrantAccessMsg, navigateMsg:
			outgoing = append(outgoing, msg)
			return s, outgoing
		}
		s, cmd, _ = s.Update(env, msg)
	}
	return s, outgoing
}

func usersOpen(t *testing.T, client AdminClient, selectID string) (usersScreen, screenEnv) {
	t.Helper()
	env := usersTestEnv(client, 150, 24)
	s := newUsersScreen(selectID)
	next, _ := settleScreen(s, env, s.Init(env))
	return next.(usersScreen), env
}

// usersPress sends the keys one by one, settling every command, and returns
// the screen and the open requests it made.
func usersPress(t *testing.T, s usersScreen, env screenEnv, keys ...keyMsg) (usersScreen, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		settled, out := settleScreen(next, env, cmd)
		s = settled.(usersScreen)
		all = append(all, out...)
	}
	return s, all
}

func typeKeys(text string) []keyMsg {
	keys := make([]keyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

func renderUsers(s usersScreen, env screenEnv) string {
	return ansi.Strip(s.View(newAdminTheme(), env).Body)
}

// assertFitsTerminal fails when a line is wider than the terminal or the view
// has more rows than it.
func assertFitsTerminal(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Errorf("%d rows, terminal has %d", len(lines), h)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > w {
			t.Errorf("row %d is %d columns, terminal has %d: %q", i, got, w, line)
		}
	}
}

func assertUsersGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "users", name+".golden")
	if *updateUsersGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-users): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-users to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestUsersScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()
	var _ adminScreen = usersScreen{}
	var _ adminScreen = editUserScreen{}
	var _ adminScreen = createUserScreen{}
	var _ adminScreen = resetPasswordScreen{}
}

// ---- loading ----

func TestUsersLoadsUsersAndTheExpiringTokensInOneCall(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	s, _ := usersOpen(t, client, "")
	if client.listUsersCalls != 1 || client.listExpiringCalls != 1 {
		t.Fatalf("calls: users=%d expiring=%d, want one each (no call per user)", client.listUsersCalls, client.listExpiringCalls)
	}
	if client.lastExpiringWithin != 7*24*time.Hour || client.lastExpiringLimit != 200 {
		t.Fatalf("expiring asked for within=%v limit=%d, want 7 days and 200", client.lastExpiringWithin, client.lastExpiringLimit)
	}
	if client.listGrantsCalls != 0 || client.listTokensCalls != 0 {
		t.Fatalf("the list made per-user calls: grants=%d tokens=%d", client.listGrantsCalls, client.listTokensCalls)
	}
	if !s.loaded || len(s.users) != 5 {
		t.Fatalf("loaded=%v users=%d", s.loaded, len(s.users))
	}
}

func TestUsersExpiringColumnCountsPerUser(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	out := renderUsers(s, env)
	row := func(name string) string {
		for _, line := range strings.Split(out, "\n") {
			for _, marker := range []string{"│   " + name + " ", "│ ▸ " + name + " "} {
				if i := strings.Index(line, marker); i >= 0 {
					rest := line[i+len("│"):]
					return rest[:strings.Index(rest, "│")]
				}
			}
		}
		t.Fatalf("no row for %q:\n%s", name, out)
		return ""
	}
	for name, want := range map[string]string{"admin": "0", "alice": "1", "bob": "0", "carol": "2", "dave": "0"} {
		fields := strings.Fields(row(name))
		if got := fields[len(fields)-1]; got != want {
			t.Errorf("%s EXPIRING = %q, want %q (row %q)", name, got, want, row(name))
		}
	}
	if strings.Contains(out, "ci-bot") {
		t.Errorf("a robot's token showed up in the user list:\n%s", out)
	}
	if strings.Contains(out, "GRANTS") {
		t.Errorf("a GRANTS column is shown but no single call returns the counts:\n%s", out)
	}
}

func TestUsersExpiringCountsShowThatTheyMayBeIncompleteWhenTheReportIsFull(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.expiringTokens = nil
	for i := range usersExpiringLimit {
		client.expiringTokens = append(client.expiringTokens, ports.AdminExpiringToken{ID: fmt.Sprint(i), UserID: "u-4", Name: "t", ExpiresAt: usersTestNow.Add(time.Hour)})
	}
	s, env := usersOpen(t, client, "")
	out := renderUsers(s, env)
	if !strings.Contains(out, "200+") || !strings.Contains(out, "more may exist") {
		t.Errorf("a full report is not flagged as possibly incomplete:\n%s", out)
	}
}

func TestUsersListIsShownWhenTheExpiringReportFails(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.expiringErr = errors.New("expiring exploded")
	s, env := usersOpen(t, client, "")
	out := renderUsers(s, env)
	if !strings.Contains(out, "alice") || !strings.Contains(out, "Could not load expiring tokens: expiring exploded") {
		t.Errorf("the list or the error is missing:\n%s", out)
	}
	if strings.Contains(out, "EXPIRING counts") {
		t.Errorf("the footnote claims counts that were never loaded:\n%s", out)
	}
}

func TestUsersLoadErrorAndEmptyStates(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.usersErr = errors.New("users exploded")
	s, env := usersOpen(t, client, "")
	if out := renderUsers(s, env); !strings.Contains(out, "Could not load users: users exploded") {
		t.Errorf("no load error:\n%s", out)
	}
	empty := &fakeAdminClient{}
	s, env = usersOpen(t, empty, "")
	if out := renderUsers(s, env); !strings.Contains(out, "No users yet. Press n to create one.") {
		t.Errorf("no empty state:\n%s", out)
	}
}

func TestUsersExpiredSessionIsReportedForRelogin(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.usersErr = NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	env := usersTestEnv(client, 150, 24)
	s := newUsersScreen("")
	msg := s.Init(env)()
	expired, ok := msg.(userScreenMsg)
	if !ok {
		t.Fatalf("%T is not a userScreenMsg", msg)
	}
	if reason, isExpired := expired.expiredReason(); !isExpired || reason != AdminSessionExpiredReasonExpired {
		t.Fatalf("expiredReason() = %q, %v", reason, isExpired)
	}
}

func TestUsersDropsAResultOfAnotherScreenInstance(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	next, cmd, _ := s.Update(env, usersLoadedMsg{gen: s.gen + 99, users: nil})
	if cmd != nil || len(next.(usersScreen).users) != 5 {
		t.Fatalf("a foreign result changed the screen: users=%d", len(next.(usersScreen).users))
	}
	next, _, _ = s.Update(env, userEnabledMsg{gen: s.gen + 99, userID: "u-2", username: "alice"})
	if next.(usersScreen).notice != "" {
		t.Fatalf("a foreign toggle result set the notice %q", next.(usersScreen).notice)
	}
}

// ---- selection ----

func TestUsersSelectionMovesWithArrowsAndJK(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	if got, _ := s.current(); got.Username != "admin" {
		t.Fatalf("starts on %q, want the first user", got.Username)
	}
	for _, k := range []keyMsg{testKeyDown(), testKeyRunes("j"), testKeyRunes("j")} {
		s, _ = usersPress(t, s, env, k)
	}
	if got, _ := s.current(); got.Username != "carol" {
		t.Fatalf("after down, j, j on %q, want carol", got.Username)
	}
	s, _ = usersPress(t, s, env, testKeyUp(), testKeyRunes("k"))
	if got, _ := s.current(); got.Username != "alice" {
		t.Fatalf("after up, k on %q, want alice", got.Username)
	}
	s, _ = usersPress(t, s, env, testKeyUp(), testKeyUp(), testKeyUp())
	if got, _ := s.current(); got.Username != "admin" {
		t.Fatalf("up past the top reached %q", got.Username)
	}
	for range 10 {
		s, _ = usersPress(t, s, env, testKeyDown())
	}
	if got, _ := s.current(); got.Username != "dave" {
		t.Fatalf("down past the bottom reached %q", got.Username)
	}
}

func TestUsersPreselectsTheGivenUserAndKeepsItAcrossAReload(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	s, env := usersOpen(t, client, "u-4")
	if got, _ := s.current(); got.Username != "carol" {
		t.Fatalf("preselected %q, want carol", got.Username)
	}
	// A reload that reorders the list keeps the cursor on the same person.
	reordered := usersTestUsers()
	reordered[0], reordered[3] = reordered[3], reordered[0]
	next, _, _ := s.Update(env, usersLoadedMsg{gen: s.gen, users: reordered})
	if got, _ := next.(usersScreen).current(); got.Username != "carol" {
		t.Fatalf("after reload on %q, want carol", got.Username)
	}
}

// ---- search ----

func TestUsersSearchTypesEveryLetterIncludingJKAndQ(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	s, _ = usersPress(t, s, env, testKeyRunes("/"))
	if !s.searching {
		t.Fatal("/ did not start the search")
	}
	s, out := usersPress(t, s, env, typeKeys("jkq")...)
	if s.query != "jkq" || len(out) != 0 || !s.searching {
		t.Fatalf("query=%q searching=%v out=%v, want jkq typed and still searching", s.query, s.searching, out)
	}
	if got, _ := s.current(); got.Username != "" {
		t.Fatalf("a no-match query still shows %q", got.Username)
	}
}

func TestUsersSearchFiltersCaseInsensitivelyAndBackspaceWidens(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	s, _ = usersPress(t, s, env, append([]keyMsg{testKeyRunes("/")}, typeKeys("AL")...)...)
	if visible := s.visible(); len(visible) != 1 || visible[0].Username != "alice" {
		t.Fatalf("visible = %v, want only alice", visible)
	}
	if out := renderUsers(s, env); !strings.Contains(out, "1 of 5 users") {
		t.Errorf("the summary does not say how many match:\n%s", out)
	}
	s, _ = usersPress(t, s, env, testKeyBackspace(), testKeyBackspace())
	if len(s.visible()) != 5 || s.query != "" {
		t.Fatalf("after backspaces: query=%q visible=%d", s.query, len(s.visible()))
	}
}

func TestUsersSearchPasteIsTypedAndControlCharactersAreDropped(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	s, _ = usersPress(t, s, env, testKeyRunes("/"))
	s, _ = usersPress(t, s, env, keyMsg{Text: "ca\nrol\t"})
	if s.query != "carol" {
		t.Fatalf("query = %q, want carol", s.query)
	}
}

func TestUsersSearchArrowsMoveEnterKeepsTheFilterEscEndsTheSearch(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	s, _ = usersPress(t, s, env, testKeyRunes("/"), testKeyRunes("a"))
	// admin, alice, carol, dave contain an "a".
	s, _ = usersPress(t, s, env, testKeyDown())
	if got, _ := s.current(); got.Username != "alice" {
		t.Fatalf("down in the search moved to %q, want alice", got.Username)
	}
	s, _ = usersPress(t, s, env, testKeyEnter())
	if s.searching || s.query != "a" {
		t.Fatalf("enter: searching=%v query=%q, want the filter kept and the search ended", s.searching, s.query)
	}
	s, _ = usersPress(t, s, env, testKeyRunes("/"), testKeyEsc())
	if s.searching || s.query != "a" {
		t.Fatalf("esc: searching=%v query=%q", s.searching, s.query)
	}
	// With the search ended, Esc leaves the screen.
	_, out := usersPress(t, s, env, testKeyEsc())
	if len(out) != 1 || out[0].(navigateMsg).To != screenAdminOverview {
		t.Fatalf("esc went to %v, want the Overview", out)
	}
}

func TestUsersJKMoveOnlyWhenNotSearching(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "")
	s, _ = usersPress(t, s, env, testKeyRunes("/"), testKeyRunes("j"))
	if got, _ := s.current(); s.query != "j" || got.Username != "" {
		t.Fatalf("j in the search: query=%q", s.query)
	}
}

// ---- actions ----

func TestUsersActionsAskForTheirScreens(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "u-4")
	tests := []struct {
		name string
		key  keyMsg
		want func(t *testing.T, msgs []tea.Msg)
	}{
		{"enter opens the profile", testKeyEnter(), func(t *testing.T, msgs []tea.Msg) {
			open := msgs[0].(openUserScreenMsg)
			if open.to != screenAdminEditUser || open.userID != "u-4" || open.username != "carol" {
				t.Fatalf("open = %+v", open)
			}
		}},
		{"g grants access with the user preselected and comes back here", testKeyRunes("g"), func(t *testing.T, msgs []tea.Msg) {
			open := msgs[0].(openGrantAccessMsg)
			if open.preselectID != "u-4" || open.returnTo != screenAdminUsers {
				t.Fatalf("open = %+v", open)
			}
		}},
		{"t opens the tokens of the selected user", testKeyRunes("t"), func(t *testing.T, msgs []tea.Msg) {
			open := msgs[0].(openUserScreenMsg)
			if open.to != screenAdminEditUserTokens || open.userID != "u-4" || open.username != "carol" {
				t.Fatalf("open = %+v", open)
			}
		}},
		{"p resets the password of the selected user", testKeyRunes("p"), func(t *testing.T, msgs []tea.Msg) {
			open := msgs[0].(openUserScreenMsg)
			if open.to != screenAdminChangePassword || open.userID != "u-4" || open.username != "carol" {
				t.Fatalf("open = %+v", open)
			}
		}},
		{"n opens the New user form", testKeyRunes("n"), func(t *testing.T, msgs []tea.Msg) {
			if open := msgs[0].(openUserScreenMsg); open.to != screenAdminCreateUser {
				t.Fatalf("open = %+v", open)
			}
		}},
		{"esc goes back to the Overview", testKeyEsc(), func(t *testing.T, msgs []tea.Msg) {
			if nav := msgs[0].(navigateMsg); nav.To != screenAdminOverview {
				t.Fatalf("nav = %+v", nav)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, msgs := usersPress(t, s, env, tt.key)
			if len(msgs) != 1 {
				t.Fatalf("got %d requests, want 1", len(msgs))
			}
			tt.want(t, msgs)
		})
	}
}

func TestUsersActionsOnAnEmptyListSayToSelectAUserFirst(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, &fakeAdminClient{}, "")
	for _, k := range []keyMsg{testKeyEnter(), testKeyRunes("g"), testKeyRunes("t"), testKeyRunes("p"), testKeyRunes("d")} {
		next, out := usersPress(t, s, env, k)
		if len(out) != 0 || next.notice != "Select a user first." || next.confirm.Active() {
			t.Fatalf("key %q: out=%v notice=%q confirm=%v", keyText(k), out, next.notice, next.confirm.Active())
		}
	}
	// New user still works.
	if _, out := usersPress(t, s, env, testKeyRunes("n")); len(out) != 1 {
		t.Fatalf("n on an empty list made %d requests", len(out))
	}
}

func TestUsersDisableAsksThenDisablesOnceAndReloads(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.disableUser = ports.AdminUser{ID: "u-4", Username: "carol"}
	s, env := usersOpen(t, client, "u-4")

	s, _ = usersPress(t, s, env, testKeyRunes("d"))
	if !s.confirm.Active() || client.disableCalls != 0 {
		t.Fatalf("d: confirm=%v calls=%d, want a prompt and no call yet", s.confirm.Active(), client.disableCalls)
	}
	view := s.View(newAdminTheme(), env)
	if !strings.Contains(ansi.Strip(view.Overlay), "Disable user?") {
		t.Fatalf("overlay = %q", view.Overlay)
	}
	loadsBefore := client.listUsersCalls
	// Enter twice before the result arrives: the command must go out once.
	next, cmd, _ := s.Update(env, testKeyEnter())
	s = next.(usersScreen)
	next, again, _ := s.Update(env, testKeyEnter())
	s = next.(usersScreen)
	if again != nil {
		t.Fatal("a second Enter while the disable is in flight dispatched again")
	}
	settled, _ := settleScreen(s, env, cmd)
	s = settled.(usersScreen)
	if client.disableCalls != 1 {
		t.Fatalf("DisableUser called %d times, want 1", client.disableCalls)
	}
	if s.confirm.Active() || !strings.Contains(s.notice, `User "carol" disabled.`) || s.noticeTone != userNoticeSuccess {
		t.Fatalf("after the result: confirm=%v notice=%q", s.confirm.Active(), s.notice)
	}
	if client.listUsersCalls != loadsBefore+1 {
		t.Fatalf("the list was not reloaded (%d -> %d calls)", loadsBefore, client.listUsersCalls)
	}
}

func TestUsersEnableUsesEnableForADisabledUser(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	s, env := usersOpen(t, client, "u-5")
	s, _ = usersPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	if client.enableCalls != 1 || client.disableCalls != 0 {
		t.Fatalf("enable=%d disable=%d, want one enable", client.enableCalls, client.disableCalls)
	}
	if !strings.Contains(s.notice, `User "dave" enabled.`) {
		t.Fatalf("notice = %q", s.notice)
	}
	if out := renderUsers(s, env); !strings.Contains(out, "Enable user") {
		t.Errorf("the detail panel does not offer Enable for a disabled user:\n%s", out)
	}
}

func TestUsersEscCancelsTheConfirmWithoutACall(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	s, env := usersOpen(t, client, "u-4")
	s, out := usersPress(t, s, env, testKeyRunes("d"), testKeyEsc())
	if s.confirm.Active() || client.disableCalls != 0 || len(out) != 0 {
		t.Fatalf("confirm=%v calls=%d out=%v", s.confirm.Active(), client.disableCalls, out)
	}
}

func TestUsersDisableFailureIsShownAndThePromptCloses(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.disableErr = errors.New("cannot disable the last admin")
	s, env := usersOpen(t, client, "u-1")
	s, _ = usersPress(t, s, env, testKeyRunes("d"), testKeyEnter())
	if s.confirm.Active() || s.noticeTone != userNoticeError || s.notice != "cannot disable the last admin" {
		t.Fatalf("confirm=%v notice=%q tone=%v", s.confirm.Active(), s.notice, s.noticeTone)
	}
	if out := renderUsers(s, env); !strings.Contains(out, "cannot disable the last admin") {
		t.Errorf("the failure is not on screen:\n%s", out)
	}
}

func TestUsersOtherKeysAreSwallowedWhileTheConfirmIsOpen(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "u-4")
	s, out := usersPress(t, s, env, testKeyRunes("d"), testKeyRunes("g"), testKeyRunes("n"), testKeyDown())
	if !s.confirm.Active() || len(out) != 0 {
		t.Fatalf("confirm=%v out=%v", s.confirm.Active(), out)
	}
	if got, _ := s.current(); got.Username != "carol" {
		t.Fatalf("the cursor moved to %q under the prompt", got.Username)
	}
}

// ---- needs attention ----

func TestUsersNeedsAttentionComesOnlyFromDataThatExists(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "u-4")
	out := renderUsers(s, env)
	for _, want := range []string{"NEEDS ATTENTION", "Token laptop expires in 4 days", "Token ci expires in 30 hours", "t tokens"} {
		if !strings.Contains(out, want) {
			t.Errorf("carol's panel lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Token phone") {
		t.Errorf("another user's token is listed for carol:\n%s", out)
	}

	s, env = usersOpen(t, usersTestClient(), "u-5")
	out = renderUsers(s, env)
	if !strings.Contains(out, "User is disabled") || !strings.Contains(out, "d enable") {
		t.Errorf("a disabled user is not flagged:\n%s", out)
	}

	s, env = usersOpen(t, usersTestClient(), "u-3")
	if out = renderUsers(s, env); !strings.Contains(out, "Nothing needs attention.") {
		t.Errorf("no empty attention state:\n%s", out)
	}
}

func TestUsersNeverMentionsSecondsInAnyTimeText(t *testing.T) {
	t.Parallel()
	cases := map[time.Duration]string{
		30 * time.Minute:     "less than 1 hour",
		time.Hour:            "1 hour",
		30 * time.Hour:       "30 hours",
		2 * 24 * time.Hour:   "2 days",
		61 * 24 * time.Hour:  "61 days",
		800 * 24 * time.Hour: "2 years",
	}
	for left, want := range cases {
		if got := remainingText(left); got != want {
			t.Errorf("remainingText(%v) = %q, want %q", left, got, want)
		}
	}
}

// ---- view ----

func TestUsersViewHasTheBoardsPartsAndNoInventedData(t *testing.T) {
	t.Parallel()
	s, env := usersOpen(t, usersTestClient(), "u-4")
	out := renderUsers(s, env)
	for _, want := range []string{"Admin › Access › Users", "signed in as operator", "5 users, 1 disabled", "/ search", "USERNAME", "ROLE", "STATE", "EXPIRING",
		"▸ carol", "user, enabled, created 3 Mar 2026", "ACTIONS", "Open profile", "Grant access", "Tokens", "Reset password", "Disable user",
		"move", "open", "search", "new user", "grant access", "tokens", "password", "disable", "back"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"Sample data", "last sign-in", "GRANTS"} {
		if strings.Contains(out, banned) {
			t.Errorf("view shows %q", banned)
		}
	}
}

func TestUsersViewFitsEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	many := usersTestClient()
	many.users = nil
	for i := range 80 {
		many.users = append(many.users, ports.AdminUser{ID: fmt.Sprintf("u-%d", i), Username: fmt.Sprintf("user-%02d-with-a-very-long-name-that-keeps-going-%s", i, strings.Repeat("é", 30)), Enabled: i%7 != 0})
	}
	many.expiringTokens = []ports.AdminExpiringToken{{ID: "t", UserID: "u-40", Name: strings.Repeat("long-token-name-", 8), ExpiresAt: usersTestNow.Add(time.Hour)}}
	failing := usersTestClient()
	failing.usersErr = errors.New(strings.Repeat("a very long error ", 20))
	failing.expiringErr = errors.New(strings.Repeat("another long error ", 20))
	cases := map[string]func() (usersScreen, screenEnv, []keyMsg){
		"list":  func() (usersScreen, screenEnv, []keyMsg) { return usersAt(usersTestClient(), "u-4"), screenEnv{}, nil },
		"empty": func() (usersScreen, screenEnv, []keyMsg) { return usersAt(&fakeAdminClient{}, ""), screenEnv{}, nil },
		"error": func() (usersScreen, screenEnv, []keyMsg) { return usersAt(failing, ""), screenEnv{}, nil },
		"many": func() (usersScreen, screenEnv, []keyMsg) {
			return usersAt(many, "u-40"), screenEnv{}, []keyMsg{testKeyDown()}
		},
		"search": func() (usersScreen, screenEnv, []keyMsg) {
			return usersAt(usersTestClient(), ""), screenEnv{}, append([]keyMsg{testKeyRunes("/")}, typeKeys("zzz")...)
		},
		"confirm": func() (usersScreen, screenEnv, []keyMsg) {
			return usersAt(usersTestClient(), "u-4"), screenEnv{}, []keyMsg{testKeyRunes("d")}
		},
		"loading": func() (usersScreen, screenEnv, []keyMsg) { return newUsersScreen(""), screenEnv{}, nil },
	}
	for name, build := range cases {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				s, _, keys := build()
				env := usersTestEnv(usersTestClient(), size.w, size.h)
				s, _ = usersPress(t, s, env, keys...)
				frame := s.View(newAdminTheme(), env)
				assertFitsTerminal(t, ansi.Strip(frame.Body), size.w, size.h)
				if frame.Overlay != "" {
					assertFitsTerminal(t, ansi.Strip(compositeOverlay(frame.Body, frame.Overlay, size.w, size.h)), size.w, size.h)
				}
			})
		}
	}
}

// usersAt builds a loaded screen at the default size.
func usersAt(client AdminClient, selectID string) usersScreen {
	env := usersTestEnv(client, 150, 24)
	s := newUsersScreen(selectID)
	next, _ := settleScreen(s, env, s.Init(env))
	return next.(usersScreen)
}

func TestUsersSelectionStaysVisibleInALongList(t *testing.T) {
	t.Parallel()
	client := usersTestClient()
	client.users = nil
	for i := range 60 {
		client.users = append(client.users, ports.AdminUser{ID: fmt.Sprintf("u-%02d", i), Username: fmt.Sprintf("person-%02d", i), Enabled: true})
	}
	for _, size := range geometrySizes {
		env := usersTestEnv(client, size.w, size.h)
		s := usersAt(client, "")
		for i := range 59 {
			s, _ = usersPress(t, s, env, testKeyDown())
			user, _ := s.current()
			if out := renderUsers(s, env); !strings.Contains(out, "▸ "+user.Username) {
				t.Fatalf("%dx%d: after %d moves the selected %s is not on screen:\n%s", size.w, size.h, i+1, user.Username, out)
			}
		}
	}
}

func TestUsersViewGoldens(t *testing.T) {
	cases := []struct {
		name string
		make func() (usersScreen, []keyMsg)
	}{
		{"users_list", func() (usersScreen, []keyMsg) { return usersAt(usersTestClient(), "u-4"), nil }},
		{"users_disabled_selected", func() (usersScreen, []keyMsg) { return usersAt(usersTestClient(), "u-5"), nil }},
		{"users_empty", func() (usersScreen, []keyMsg) { return usersAt(&fakeAdminClient{}, ""), nil }},
		{"users_search", func() (usersScreen, []keyMsg) {
			return usersAt(usersTestClient(), ""), append([]keyMsg{testKeyRunes("/")}, typeKeys("al")...)
		}},
		{"users_error", func() (usersScreen, []keyMsg) {
			client := usersTestClient()
			client.expiringErr = errors.New("expiring exploded")
			return usersAt(client, ""), nil
		}},
		{"users_confirm_disable", func() (usersScreen, []keyMsg) { return usersAt(usersTestClient(), "u-4"), []keyMsg{testKeyRunes("d")} }},
	}
	for _, tc := range cases {
		for _, size := range usersViewSizes {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				s, keys := tc.make()
				env := usersTestEnv(usersTestClient(), size.w, size.h)
				s, _ = usersPress(t, s, env, keys...)
				frame := s.View(newAdminTheme(), env)
				out := frame.Body
				if frame.Overlay != "" {
					out = compositeOverlay(frame.Body, frame.Overlay, size.w, size.h)
				}
				assertUsersGolden(t, name, ansi.Strip(out))
			})
		}
	}
}

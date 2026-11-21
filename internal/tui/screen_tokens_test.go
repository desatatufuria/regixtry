package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"regixtry/internal/ports"
)

func tokensFixture(robot bool) (*accessClient, screenEnv, tokensScreen) {
	client := newAccessClient()
	client.tokens["r-ci"] = []ports.AdminToken{
		accessToken("old-deploy", "act_55f00000000b81c", accessNow.Add(-32*day), accessNow.Add(-19*day)),
		accessToken("staging-deploy", "act_17ab0000003f2a", accessNow.Add(-12*day), accessNow.Add(18*day)),
		accessToken("release-pipeline", "act_9c410000000e0d2", accessNow.Add(-24*day), accessNow.Add(6*day+2*time.Hour)),
	}
	env := accessEnv(client)
	s := newTokensScreen("r-ci", "ci-publisher", robot, screenAdminRobots)
	settled, _ := accessSettle(s, env, s.Init(env))
	return client, env, settled.(tokensScreen)
}

func TestTokensLoadOrdersByWhatEndsFirstThenExpiredThenRevoked(t *testing.T) {
	t.Parallel()
	revoked := accessNow.Add(-day)
	tokens := []ports.AdminToken{
		{Accessor: "revoked", ExpiresAt: accessNow.Add(50 * day), RevokedAt: &revoked},
		{Accessor: "expired", ExpiresAt: accessNow.Add(-2 * day)},
		{Accessor: "late", ExpiresAt: accessNow.Add(60 * day)},
		{Accessor: "soon", ExpiresAt: accessNow.Add(3 * day)},
	}
	var got []string
	for _, token := range orderTokens(tokens, accessNow) {
		got = append(got, token.Accessor)
	}
	if strings.Join(got, ",") != "soon,late,expired,revoked" {
		t.Fatalf("order = %v", got)
	}
}

func TestTokensListShowsTheBoardCardsWithTimeLeftAndShortAccessors(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	view := accessView(s, env)
	mustContain(t, view,
		"Admin › Access › Robots › ci-publisher › Tokens",
		"Tokens for ci-publisher",
		"Each bar is the time left out of the token's full lifetime.",
		"▸ release-pipeline", "6 days left", "act_9c41…e0d2", "issued 28 Oct, expires 27 Nov 2026",
		"staging-deploy", "18 days left", "act_17ab…3f2a", "issued 9 Nov, expires 9 Dec 2026",
		"old-deploy", "expired", "act_55f0…b81c", "expired 2 Nov 2026",
	)
	mustNotContain(t, view, "seconds", "Sample data")
}

func TestTokensBreadcrumbNamesUsersOrRobots(t *testing.T) {
	t.Parallel()
	_, userEnv, user := tokensFixture(false)
	mustContain(t, accessView(user, userEnv), "Admin › Access › Users › ci-publisher › Tokens")
	crumbs, nav := user.breadcrumb()
	if nav != shellNavUsers || crumbs[1] != "Users" {
		t.Fatalf("user: %v %v", crumbs, nav)
	}
	_, nav = tokensScreen{robot: true}.breadcrumb()
	if nav != shellNavRobots {
		t.Fatalf("robot nav = %v", nav)
	}
}

func TestTimeLeftBarIsTheTimeLeftOutOfTheFullLifetime(t *testing.T) {
	t.Parallel()
	theme := newAdminTheme()
	count := func(bar string) (filled, track int) {
		plain := accessStrip(bar)
		return strings.Count(plain, "█"), strings.Count(plain, "░")
	}
	cases := []struct {
		name        string
		left, total time.Duration
		wantFilled  int
		width       int
	}{
		{"6 of 30 days", 6 * day, 30 * day, 4, 20},
		{"18 of 30 days", 18 * day, 30 * day, 12, 20},
		{"full", 30 * day, 30 * day, 20, 20},
		{"expired is empty", 0, 30 * day, 0, 20},
		{"negative is empty", -3 * day, 30 * day, 0, 20},
		{"a sliver still shows", time.Hour, 730 * day, 1, 20},
		{"left above total is full", 40 * day, 30 * day, 20, 20},
	}
	for _, tc := range cases {
		filled, track := count(timeLeftBar(theme, tc.left, tc.total, tc.width))
		if filled != tc.wantFilled || filled+track != tc.width {
			t.Fatalf("%s: filled=%d track=%d, want filled=%d of %d", tc.name, filled, track, tc.wantFilled, tc.width)
		}
	}
}

func TestTimeLeftWordingAndTones(t *testing.T) {
	t.Parallel()
	cases := []struct {
		left time.Duration
		text string
		tone expiryTone
	}{
		{6*day + 23*time.Hour, "6 days", expirySoon},
		{7 * day, "7 days", expiryHealthy},
		{18 * day, "18 days", expiryHealthy},
		{day, "1 day", expirySoon},
		{5 * time.Hour, "5 hours", expirySoon},
		{time.Hour, "1 hour", expirySoon},
		{40 * time.Minute, "<1 hour", expirySoon},
		{0, "expired", expiryExpired},
		{-time.Hour, "expired", expiryExpired},
		{204 * day, "204 days", expiryHealthy},
	}
	for _, tc := range cases {
		if got := timeLeftText(tc.left); got != tc.text {
			t.Fatalf("timeLeftText(%v) = %q, want %q", tc.left, got, tc.text)
		}
		if got := expiryToneOf(tc.left); got != tc.tone {
			t.Fatalf("expiryToneOf(%v) = %v, want %v", tc.left, got, tc.tone)
		}
		if strings.Contains(timeLeftText(tc.left), "second") {
			t.Fatal("seconds shown")
		}
	}
}

func TestShortAccessor(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"act_9c41d2e5a7b3e0d2": "act_9c41…e0d2",
		"act_c07e1b52d9a4":     "act_c07e…d9a4",
		"short":                "short",
		"":                     "",
	} {
		if got := shortAccessor(in); got != want {
			t.Fatalf("shortAccessor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokensSelectionAndKeys(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	var cur adminScreen = s
	for i, step := range []struct {
		key  keyMsg
		want int
	}{{testKeyDown(), 1}, {testKeyDown(), 2}, {testKeyDown(), 2}, {testKeyUp(), 1}, {testKeyRunes("k"), 0}, {testKeyRunes("j"), 1}} {
		cur, _ = accessPress(t, cur, env, step.key)
		if got := cur.(tokensScreen).selected; got != step.want {
			t.Fatalf("step %d: selected = %d, want %d", i, got, step.want)
		}
	}
}

func TestTokensEscGoesBackToWhereItCameFrom(t *testing.T) {
	t.Parallel()
	_, env, robot := tokensFixture(true)
	_, cmd, _ := robot.Update(env, testKeyEsc())
	if msg, ok := cmd().(openRobotsMsg); !ok || msg.selectID != "r-ci" {
		t.Fatalf("robot Esc = %#v, want the Robots list with the robot selected", cmd())
	}
	_, _, user := tokensFixture(false)
	user.returnTo = screenAdminUsers
	_, cmd, _ = user.Update(env, testKeyEsc())
	if msg, ok := cmd().(navigateMsg); !ok || msg.To != screenAdminUsers {
		t.Fatalf("user Esc = %#v", cmd())
	}
}

func TestRevokingAsksFirstThenRevokesTheSelectedTokenOnce(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	asked, _ := accessPress(t, s, env, testKeyRunes("x"))
	got := asked.(tokensScreen)
	if !got.confirm.Active() || len(client.revoked) != 0 {
		t.Fatalf("x must open the confirm and call nothing (revoked=%v)", client.revoked)
	}
	overlay := accessStrip(asked.View(newAdminTheme(), env).Overlay)
	mustContain(t, overlay, "Confirm Token Revocation", `"release-pipeline"`, "act_9c41…e0d2", `"ci-publisher"`)

	first, cmd, _ := asked.Update(env, testKeyEnter())
	if cmd == nil || !first.(tokensScreen).revoking {
		t.Fatal("Enter did not dispatch the revoke")
	}
	if _, second, _ := first.Update(env, testKeyEnter()); second != nil {
		t.Fatal("a second Enter revoked again while in flight")
	}
	msg := cmd()
	done, _ := accessSettle(first, env, func() tea.Msg { return msg })
	after := done.(tokensScreen)
	if len(client.revoked) != 1 || client.revoked[0] != "act_9c410000000e0d2" {
		t.Fatalf("RevokeUserAdminToken got %v, want the full accessor", client.revoked)
	}
	if after.confirm.Active() || after.revoking || after.notice != "Token revoked." {
		t.Fatalf("after revoke: confirm=%v revoking=%v notice=%q", after.confirm.Active(), after.revoking, after.notice)
	}
}

func TestRevokeFailureAndEscKeepTheToken(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	client.revokeTokenErr = fmt.Errorf("cannot revoke")
	failed, _ := accessPress(t, s, env, testKeyRunes("x"), testKeyEnter())
	if got := failed.(tokensScreen); got.notice != "cannot revoke" || !got.noticeFail || got.confirm.Active() {
		t.Fatalf("notice=%q fail=%v", got.notice, got.noticeFail)
	}
	client.revoked = nil
	cancelled, _ := accessPress(t, s, env, testKeyRunes("x"), testKeyEsc())
	if cancelled.(tokensScreen).confirm.Active() || len(client.revoked) != 0 {
		t.Fatal("Esc must cancel without revoking")
	}
}

func TestRevokingARevokedOrMissingTokenSaysSo(t *testing.T) {
	t.Parallel()
	client := newAccessClient()
	revoked := accessNow.Add(-day)
	client.tokens["u-1"] = []ports.AdminToken{{Accessor: "act_r", Name: "r", CreatedAt: accessNow.Add(-5 * day), ExpiresAt: accessNow.Add(5 * day), RevokedAt: &revoked}}
	env := accessEnv(client)
	s := newTokensScreen("u-1", "alice", false, screenAdminUsers)
	settled, _ := accessSettle(s, env, s.Init(env))
	next, _ := accessPress(t, settled, env, testKeyRunes("x"))
	if got := next.(tokensScreen); got.confirm.Active() || got.notice != "That token is already revoked." {
		t.Fatalf("notice = %q", got.notice)
	}
	mustContain(t, accessView(next, env), "revoked", "That token is already revoked.")

	empty := newTokensScreen("u-2", "bob", false, screenAdminUsers)
	settled, _ = accessSettle(empty, env, empty.Init(env))
	next, _ = accessPress(t, settled, env, testKeyRunes("x"))
	if got := next.(tokensScreen).notice; got != "No token selected to revoke." {
		t.Fatalf("notice = %q", got)
	}
	mustContain(t, accessView(next, env), "No tokens yet. Press n to issue one.")
}

func TestIssuePanelShowsTheBoardsLabelsAndTheExactExpiry(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	view := accessView(s, env)
	mustContain(t, view,
		"Issue a new token", "Name", "Lifetime",
		"(*) 30 days", "( ) 7 days", "( ) 90 days", "( ) 1 year", "( ) 2 years", "( ) Custom",
		"EXPIRES", "Mon 21 Dec 2026 09:30 UTC", "30 days from today",
		// The panel wraps the board's two sentences.
		"A leaked token stays valid until it expires", "or you revoke it. Shorter for people, longer", "only for CI robots.",
		"The secret is shown once, right after", "creation. Copy it before leaving.",
		"Create token", "enter", "Cancel", "esc",
	)
}

func TestNStartsIssuingAndEveryLetterIsTyped(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	started, _ := accessPress(t, s, env, testKeyRunes("n"))
	if started.(tokensScreen).focus != tokensFocusName || !started.(tokensScreen).capturesInput() {
		t.Fatal("n must focus the name field and capture input")
	}
	typed, _ := accessPress(t, started, env, accessType("jkq-xn l")...)
	if got := typed.(tokensScreen); got.name != "jkq-xn l" || got.selected != 0 || len(got.tokens) != 3 {
		t.Fatalf("name = %q (x, n, l, j, k, q must be typed)", got.name)
	}
	typed, _ = accessPress(t, typed, env, testKeyBackspace())
	if got := typed.(tokensScreen).name; got != "jkq-xn " {
		t.Fatalf("Backspace: name = %q", got)
	}
	pasted, _ := accessPress(t, started, env, keyMsg{Text: "ci\nrunner"})
	if got := pasted.(tokensScreen).name; got != "cirunner" {
		t.Fatalf("pasted name = %q", got)
	}
}

func TestTokensPanelEscCancelsAndEscAgainLeaves(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	typed, _ := accessPress(t, s, env, testKeyRunes("n"), testKeyRunes("a"), testKeyRight(), testKeyEsc())
	got := typed.(tokensScreen)
	if got.focus != tokensFocusList || got.name != "" || got.capturesInput() {
		t.Fatalf("Esc must return to the list with a clean form: %#v", got)
	}
	if _, cmd, _ := typed.Update(env, testKeyEsc()); cmd == nil {
		t.Fatal("Esc on the list must leave")
	}
}

func TestIssuingSendsNameAndTTLAndShowsTheSecretOnce(t *testing.T) {
	t.Parallel()
	secret := "rgx_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_0123456789"
	client, env, s := tokensFixture(true)
	client.createToken = ports.AdminCreatedToken{
		Token:  ports.AdminToken{Name: "release-pipeline-2", Accessor: "act_new0000000001"},
		Secret: secret, Accessor: "act_new0000000001", ExpiresAt: accessNow.Add(90 * day),
		TargetUser: ports.AdminUser{ID: "r-ci", Username: "ci-publisher"},
	}
	// n, name, Tab to Lifetime, Right selects 90 days, Enter creates.
	keys := append([]keyMsg{testKeyRunes("n")}, accessType("release-pipeline-2")...)
	keys = append(keys, testKeyTab(), testKeyRight(), testKeyEnter())
	done, _ := accessPress(t, s, env, keys...)
	if len(client.tokenInputs) != 1 {
		t.Fatalf("CreateUserAdminToken calls = %d", len(client.tokenInputs))
	}
	in := client.tokenInputs[0]
	if in.UserID != "r-ci" || in.Name != "release-pipeline-2" || in.TTL != 90*day {
		t.Fatalf("input = %#v", in)
	}
	got := done.(tokensScreen)
	if !got.done || got.creating {
		t.Fatalf("done=%v creating=%v", got.done, got.creating)
	}
	view := accessView(done, env)
	mustContain(t, view, "✓ Token release-pipeline-2 created", "act_new0000000001", "Shown once. Copy it before leaving: it", "cannot be displayed again.", "Fri 19 Feb 2027 09:30 UTC")
	if strings.Count(strings.ReplaceAll(view, "\n", ""), "rgx_0123") < 1 {
		t.Fatal("secret missing")
	}
	// The secret is wrapped across lines in the panel but every character is there.
	var joined strings.Builder
	for _, line := range accessLines(view) {
		if i := strings.LastIndex(line, "│"); i > 0 {
			line = line[:i]
		}
		if i := strings.LastIndex(line, "│"); i >= 0 {
			joined.WriteString(strings.TrimSpace(line[i+len("│"):]))
		}
	}
	if !strings.Contains(joined.String(), secret) {
		t.Fatalf("the secret is not whole in the panel: %q", joined.String())
	}

	// Done (Enter) drops it and returns to the list; the list was reloaded.
	back, _ := accessPress(t, done, env, testKeyEnter())
	after := back.(tokensScreen)
	if after.done || after.created.secret != "" || after.focus != tokensFocusList || strings.Contains(accessView(back, env), "rgx_0123") {
		t.Fatal("the secret survived Done")
	}
	if len(client.listedTokens) < 2 {
		t.Fatal("the token list was not reloaded after creating")
	}
}

func TestTokensCreatingIgnoresEnterAndEscStays(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	client.createToken = ports.AdminCreatedToken{Secret: "s", Accessor: "a"}
	started, _ := accessPress(t, s, env, testKeyRunes("n"))
	first, cmd, _ := started.Update(env, testKeyEnter())
	if cmd == nil || !first.(tokensScreen).creating {
		t.Fatal("Enter did not start creating")
	}
	if _, second, _ := first.Update(env, testKeyEnter()); second != nil {
		t.Fatal("second Enter dispatched while in flight")
	}
	stay, escCmd, _ := first.Update(env, testKeyEsc())
	if escCmd != nil || !strings.Contains(stay.(tokensScreen).formErr, "please wait") {
		t.Fatal("Esc left mid-creation")
	}
	mustContain(t, accessView(stay, env), "Creating token...")
	done, _ := accessSettle(stay, env, func() tea.Msg { return cmd() })
	if len(client.tokenInputs) != 1 || !done.(tokensScreen).done {
		t.Fatalf("calls=%d done=%v", len(client.tokenInputs), done.(tokensScreen).done)
	}
}

func TestTokensCreateErrorShowsInThePanelAndAllowsRetry(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	client.createTokenErr = fmt.Errorf("token limit reached")
	failed, _ := accessPress(t, s, env, testKeyRunes("n"), testKeyEnter())
	got := failed.(tokensScreen)
	if got.done || got.creating || got.formErr != "token limit reached" {
		t.Fatalf("done=%v creating=%v err=%q", got.done, got.creating, got.formErr)
	}
	mustContain(t, accessView(failed, env), "token limit reached")
	client.createTokenErr = nil
	retry, _ := accessPress(t, failed, env, testKeyEnter())
	if len(client.tokenInputs) != 2 || !retry.(tokensScreen).done {
		t.Fatalf("calls=%d", len(client.tokenInputs))
	}
}

func TestTokensInvalidCustomLifetimeBlocksCreating(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	keys := []keyMsg{testKeyRunes("n"), testKeyTab()}
	for range 5 {
		keys = append(keys, testKeyRight())
	}
	keys = append(keys, accessType("3y")...)
	typed, _ := accessPress(t, s, env, keys...)
	mustContain(t, accessView(typed, env), "Enter a whole number followed by h, d or y")
	after, _ := accessPress(t, typed, env, testKeyEnter())
	if len(client.tokenInputs) != 0 || after.(tokensScreen).creating {
		t.Fatal("an invalid Custom lifetime was sent")
	}
	valid, _ := accessPress(t, typed, env, testKeyBackspace(), testKeyBackspace(), testKeyRunes("1"), testKeyRunes("2"), testKeyRunes("h"))
	mustContain(t, accessView(valid, env), "Sat 21 Nov 2026 21:30 UTC", "12 hours from now")
	accessPress(t, valid, env, testKeyEnter())
	if len(client.tokenInputs) != 1 || client.tokenInputs[0].TTL != 12*time.Hour {
		t.Fatalf("inputs = %#v", client.tokenInputs)
	}
}

func TestTokensDropForeignResultsAndReportExpiry(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	next, _, _ := s.Update(env, tokenCreatedMsg{gen: s.gen + 1, created: ports.AdminCreatedToken{Secret: "leak"}})
	if next.(tokensScreen).done {
		t.Fatal("a foreign creation result showed a secret")
	}
	next, _, _ = s.Update(env, tokensLoadedMsg{gen: s.gen + 1})
	if len(next.(tokensScreen).tokens) != 3 {
		t.Fatal("a foreign list replaced the tokens")
	}
	expired := NewAdminSessionExpiredError("session expired")
	for name, msg := range map[string]accessMsg{
		"list": tokensLoadedMsg{err: expired}, "create": tokenCreatedMsg{err: expired}, "revoke": tokenRevokedMsg{err: expired},
	} {
		if reason, ok := msg.expiredReason(); !ok || reason != "session expired" {
			t.Fatalf("%s: %q %v", name, reason, ok)
		}
	}
}

func TestTokensLoadFailureAndLoadingStates(t *testing.T) {
	t.Parallel()
	client := newAccessClient()
	client.tokensErr = fmt.Errorf("no route")
	env := accessEnv(client)
	s := newTokensScreen("u-1", "alice", false, screenAdminUsers)
	mustContain(t, accessView(s, env), "Loading tokens...")
	settled, _ := accessSettle(s, env, s.Init(env))
	mustContain(t, accessView(settled, env), "Could not load tokens: no route")
}

func TestTokensReloadKeepsTheSelectedToken(t *testing.T) {
	t.Parallel()
	client, env, s := tokensFixture(true)
	cur, _ := accessPress(t, s, env, testKeyDown())
	selected, _ := cur.(tokensScreen).selectedToken()
	client.tokens["r-ci"] = append(client.tokens["r-ci"], accessToken("new", "act_zzzz0000000zzzz", accessNow, accessNow.Add(day)))
	reloaded, _ := accessSettle(cur, env, tokensLoadCmd(env, cur.(tokensScreen).gen, "r-ci"))
	now, _ := reloaded.(tokensScreen).selectedToken()
	if now.Accessor != selected.Accessor {
		t.Fatalf("selection moved from %s to %s", selected.Accessor, now.Accessor)
	}
}

func TestTokensKeysFollowTheState(t *testing.T) {
	t.Parallel()
	_, _, s := tokensFixture(true)
	help := func(k screenKeys) string {
		var parts []string
		for _, b := range k.short {
			parts = append(parts, b.Help().Key+" "+b.Help().Desc)
		}
		return strings.Join(parts, "|")
	}
	list := help(s.Keys())
	for _, want := range []string{"n new token", "x revoke", "↑↓ move", "esc back", "q quit"} {
		if !strings.Contains(list, want) {
			t.Fatalf("list keys %q lack %q", list, want)
		}
	}
	s.focus = tokensFocusName
	if panel := help(s.Keys()); strings.Contains(panel, "q quit") || !strings.Contains(panel, "enter create token") {
		t.Fatalf("panel keys = %q", panel)
	}
	s.done = true
	if done := help(s.Keys()); !strings.Contains(done, "enter done") {
		t.Fatalf("done keys = %q", done)
	}
}

func TestTokensCapturesInputOnlyWhereLettersAreTyped(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	if s.capturesInput() {
		t.Fatal("the list does not capture input")
	}
	asked, _ := accessPress(t, s, env, testKeyRunes("x"))
	if !asked.(tokensScreen).capturesInput() {
		t.Fatal("the confirm captures input")
	}
	s.done = true
	if !s.capturesInput() {
		t.Fatal("the secret state captures input (q must not quit)")
	}
}

func TestTokensPastedLetterIsNotACommand(t *testing.T) {
	t.Parallel()
	_, env, s := tokensFixture(true)
	for _, text := range []string{"n", "x"} {
		next, cmd, _ := s.Update(env, keyMsg{Text: text})
		if cmd != nil || next.(tokensScreen).focus != tokensFocusList || next.(tokensScreen).confirm.Active() {
			t.Fatalf("a pasted %q acted as a command", text)
		}
	}
}

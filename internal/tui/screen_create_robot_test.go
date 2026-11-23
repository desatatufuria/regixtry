package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

var createRobotKnown = []string{"team/api", "team/analytics", "team/web", "ops/console"}

func newCreateRobotFixture() (*accessClient, screenEnv, adminScreen) {
	client := newAccessClient()
	env := accessEnv(client, createRobotKnown...)
	return client, env, newCreateRobotScreen()
}

// fillRobot types a name, moves to Repository (past the Scope field) and types
// a repository.
func fillRobot(t *testing.T, s adminScreen, env screenEnv, name, repository string) adminScreen {
	t.Helper()
	keys := append(accessType(name), testKeyTab(), testKeyTab())
	keys = append(keys, accessType(repository)...)
	s, _ = accessPress(t, s, env, keys...)
	return s
}

func TestCreateRobotTypesEveryLetterIncludingJKAndQ(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s = fillRobot(t, s, env, "jkq-bot", "team/jkq")
	got := s.(createRobotScreen)
	if got.name != "jkq-bot" || got.repository != "team/jkq" {
		t.Fatalf("name=%q repository=%q", got.name, got.repository)
	}
	if _, cmd, _ := s.Update(env, testKeyRunes("q")); cmd != nil {
		t.Fatal("q must be typed, not act")
	}
}

func TestCreateRobotArrowsMoveSuggestionsAndLettersDoNot(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s = fillRobot(t, s, env, "bot", "team/a")
	// Matches: team/api, team/analytics, then the "(new name)" row for team/a.
	if got := s.(createRobotScreen).suggestion; got != 0 {
		t.Fatalf("suggestion = %d, want 0", got)
	}
	steps := []struct {
		key  keyMsg
		want int
	}{{testKeyDown(), 1}, {testKeyDown(), 2}, {testKeyDown(), 2}, {testKeyUp(), 1}, {testKeyUp(), 0}, {testKeyUp(), 0}}
	for i, step := range steps {
		s, _ = accessPress(t, s, env, step.key)
		if got := s.(createRobotScreen).suggestion; got != step.want {
			t.Fatalf("step %d: suggestion = %d, want %d", i, got, step.want)
		}
	}
	s, _ = accessPress(t, s, env, testKeyRunes("j"), testKeyRunes("k"))
	got := s.(createRobotScreen)
	if got.repository != "team/ajk" {
		t.Fatalf("j and k must be typed: repository = %q", got.repository)
	}
}

func TestCreateRobotViewListsSuggestionsAndTheNewNameRow(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s = fillRobot(t, s, env, "release-bot", "team/a")
	view := accessView(s, env)
	mustContain(t, view,
		"New robot", "Name", "release-bot", "Repository", "team/a",
		"▸ team/api", "team/analytics", "team/a (new name)",
		"Role", "(*) Reader", "( ) Writer", "( ) Admin",
		"Lifetime", "(*) 30 days", "Custom",
		"Expires Mon 21 Dec 2026 09:30 UTC (in 30 days)",
		"A leaked token works until it expires or you revoke it. Pick the shortest lifetime CI can live with.",
		"Create robot", "enter", "Cancel", "esc",
	)
	mustContain(t, accessCrumb(s, env), "Admin › Access › Robots › New robot")
}

func accessCrumb(s adminScreen, env screenEnv) string {
	return accessLines(accessView(s, env))[0]
}

func TestCreateRobotEnterOnRepositoryAcceptsTheHighlightedSuggestion(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s = fillRobot(t, s, env, "bot", "team/a")
	s, _ = accessPress(t, s, env, testKeyDown(), testKeyEnter())
	got := s.(createRobotScreen)
	if got.repository != "team/analytics" || got.focus != robotFieldRole || got.creating {
		t.Fatalf("repository=%q focus=%v creating=%v", got.repository, got.focus, got.creating)
	}
}

func TestCreateRobotPrefersTheExactlyTypedRepository(t *testing.T) {
	t.Parallel()
	known := []string{"team/api-v2", "team/api"}
	if got := preferredSuggestion(known, "team/api"); got != 1 {
		t.Fatalf("preferredSuggestion = %d, want 1 (the exact name)", got)
	}
	if got := preferredSuggestion(known, "team/ap"); got != 0 {
		t.Fatalf("preferredSuggestion = %d, want 0", got)
	}
}

func TestCreateRobotAcceptsANameNotPushedYet(t *testing.T) {
	t.Parallel()
	client, env, s := newCreateRobotFixture()
	s = fillRobot(t, s, env, "bot", "brand/new")
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
	if client.lastCreateRobotInput.Repository != "brand/new" {
		t.Fatalf("created for %q, want brand/new", client.lastCreateRobotInput.Repository)
	}
}

func TestCreateRobotRoleAndLifetimeUseLeftAndRight(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s, _ = accessPress(t, s, env, testKeyTab(), testKeyTab(), testKeyTab(), testKeyRight(), testKeyRight(), testKeyRight(), testKeyLeft())
	got := s.(createRobotScreen)
	if got.role != domainauth.RepoRoleWriter || got.focus != robotFieldRole {
		t.Fatalf("role=%q focus=%v", got.role, got.focus)
	}
	s, _ = accessPress(t, s, env, testKeyTab(), testKeyLeft())
	d, ok := s.(createRobotScreen).lifetime.duration(lifetimeCeiling())
	if !ok || d != 7*day {
		t.Fatalf("lifetime = %v %v, want 7 days", d, ok)
	}
	mustContain(t, accessView(s, env), "Expires Sat 28 Nov 2026 09:30 UTC (in 7 days)")
}

func TestCreateRobotCustomLifetimeShowsItsExpiryAndRejectsGarbage(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	toLifetime := []keyMsg{testKeyTab(), testKeyTab(), testKeyTab(), testKeyTab(), testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight(), testKeyRight()}
	s, _ = accessPress(t, s, env, append(toLifetime, accessType("12h")...)...)
	mustContain(t, accessView(s, env), "Expires Sat 21 Nov 2026 21:30 UTC (in 12 hours)")

	s, _ = accessPress(t, s, env, testKeyBackspace(), testKeyBackspace(), testKeyBackspace())
	s, _ = accessPress(t, s, env, accessType("3y")...)
	mustContain(t, accessView(s, env), lifetimeInvalidText())
}

func TestCreateRobotSendsTheTypedTTLAndRole(t *testing.T) {
	t.Parallel()
	client, env, s := newCreateRobotFixture()
	client.createRobot = ports.AdminCreatedRobot{Robot: ports.AdminRobot{ID: "r-9", Username: "release-bot", Repository: "team/api", Role: domainauth.RepoRoleWriter}, Secret: "sk", Accessor: "acc", ExpiresAt: accessNow.Add(90 * day)}
	s = fillRobot(t, s, env, "release-bot", "team/api")
	// Enter accepts the suggestion, then Role: Right selects Writer, Tab, Right selects 90 days.
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyRight(), testKeyTab(), testKeyRight(), testKeyEnter())
	in := client.lastCreateRobotInput
	if in.Name != "release-bot" || in.Repository != "team/api" || in.Role != domainauth.RepoRoleWriter || in.TTL != 90*day {
		t.Fatalf("CreateRobot input = %#v", in)
	}
}

func TestCreateRobotRequiresNameAndRepository(t *testing.T) {
	t.Parallel()
	client, env, s := newCreateRobotFixture()
	s, _ = accessPress(t, s, env, testKeyEnter())
	if got := s.(createRobotScreen); got.err != "Name and repository are required." || got.creating || client.createRobotCalls != 0 {
		t.Fatalf("err=%q creating=%v calls=%d", got.err, got.creating, client.createRobotCalls)
	}
	noRepos := accessEnv(client)
	s, _ = accessPress(t, s, noRepos, testKeyTab(), testKeyTab(), testKeyEnter())
	if got := s.(createRobotScreen); got.err != "Repository is required." {
		t.Fatalf("err = %q", got.err)
	}
}

func TestCreateRobotIgnoresEnterWhileInFlightAndEscLeavesButTheSecretComesBack(t *testing.T) {
	t.Parallel()
	client, env, s := newCreateRobotFixture()
	client.createRobot = ports.AdminCreatedRobot{Robot: ports.AdminRobot{ID: "r-1", Username: "bot"}, Secret: "s3cret", Accessor: "acc"}
	s = fillRobot(t, s, env, "bot", "team/web")
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyTab(), testKeyTab())
	first, cmd, _ := s.Update(env, testKeyEnter())
	if cmd == nil || !first.(createRobotScreen).creating {
		t.Fatal("Enter did not start the creation")
	}
	again, second, _ := first.Update(env, testKeyEnter())
	if second != nil {
		t.Fatal("a second Enter dispatched while in flight")
	}
	// Esc still leaves; the secret is not lost when the robot is created anyway.
	stay, escCmd, _ := again.Update(env, testKeyEsc())
	if msg, ok := escCmd().(openRobotsMsg); !ok || msg.selectID != "" {
		t.Fatalf("Esc mid-creation = %#v, want back to the list", escCmd())
	}
	done, _ := accessSettle(stay, env, func() tea.Msg { return cmd() })
	if client.createRobotCalls != 1 || !done.(createRobotScreen).done || done.(createRobotScreen).creating {
		t.Fatalf("calls=%d done=%v creating=%v", client.createRobotCalls, done.(createRobotScreen).done, done.(createRobotScreen).creating)
	}
}

func TestCreateRobotShowsTheWholeSecretOnceAndDropsItOnDone(t *testing.T) {
	t.Parallel()
	secret := "rgx_tst_n4TqZ8vKx2mR7wYb3LpJd9HcE5sUa6FgRgx_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGH"
	client, env, s := newCreateRobotFixture()
	client.createRobot = ports.AdminCreatedRobot{
		Robot:  ports.AdminRobot{ID: "r-7", Username: "release-bot", Repository: "team/api", Role: domainauth.RepoRoleWriter},
		Secret: secret, Accessor: "act_c07e1b52d9a4", ExpiresAt: time.Date(2026, 12, 21, 9, 30, 0, 0, time.UTC),
	}
	s = fillRobot(t, s, env, "release-bot", "team/api")
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
	view := accessView(s, env)
	mustContain(t, view,
		"✓ Robot release-bot created, Writer on team/api",
		"Accessor", "act_c07e1b52d9a4", secret,
		oneTimeSecretWarning,
		"Expires", "Mon 21 Dec 2026 09:30 UTC", "Repository", "team/api", "Role", "Writer",
		"Done", "enter",
	)
	mustNotContain(t, view, "Copy secret")
	mustNotContain(t, view, "Known repositories", "Lifetime")

	left, cmd, _ := s.Update(env, testKeyEnter())
	if _, ok := cmd().(openRobotsMsg); !ok {
		t.Fatalf("Done must go back to the Robots list, got %#v", cmd())
	}
	if strings.Contains(accessView(left, env), secret) {
		t.Fatal("the secret is still rendered after Done")
	}
	if msg := cmd().(openRobotsMsg); msg.selectID != "r-7" {
		t.Fatalf("selectID = %q, want the new robot", msg.selectID)
	}
	// Esc from the secret state also drops it.
	escLeft, escCmd, _ := s.Update(env, testKeyEsc())
	if _, ok := escCmd().(openRobotsMsg); !ok || strings.Contains(accessView(escLeft, env), secret) {
		t.Fatal("Esc must behave as Done")
	}
}

func TestCreateRobotSecretIsWrappedNotCutOnNarrowBoxes(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("abcdefghij", 12)
	lines := secretBox(newAdminTheme(), "acc", secret, 40)
	var joined []string
	for _, line := range lines {
		joined = append(joined, strings.TrimSpace(strings.Trim(strings.TrimSpace(accessStrip(line)), "│╭╮╰╯─")))
	}
	text := strings.Join(joined, "")
	if !strings.Contains(strings.ReplaceAll(text, " ", ""), secret) {
		t.Fatalf("the secret is not whole in the wrapped box:\n%s", strings.Join(lines, "\n"))
	}
}

func TestCreateRobotServerErrorStaysOnTheFormAndAllowsRetry(t *testing.T) {
	t.Parallel()
	client, env, s := newCreateRobotFixture()
	client.createRobotErr = fmt.Errorf("name already taken")
	s = fillRobot(t, s, env, "bot", "team/web")
	s, _ = accessPress(t, s, env, testKeyEnter(), testKeyEnter())
	got := s.(createRobotScreen)
	if got.done || got.creating || got.err != "name already taken" {
		t.Fatalf("done=%v creating=%v err=%q", got.done, got.creating, got.err)
	}
	mustContain(t, accessView(s, env), "name already taken")
	client.createRobotErr = nil
	s, _ = accessPress(t, s, env, testKeyEnter())
	if client.createRobotCalls != 2 {
		t.Fatalf("calls = %d, want a retry", client.createRobotCalls)
	}
}

func TestCreateRobotDropsAForeignResultAndReportsExpiry(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	cur := s.(createRobotScreen)
	next, _, _ := cur.Update(env, robotCreatedMsg{gen: cur.gen + 1, created: ports.AdminCreatedRobot{Secret: "leak"}})
	if next.(createRobotScreen).done {
		t.Fatal("a foreign result produced a secret screen")
	}
	if reason, ok := (robotCreatedMsg{err: NewAdminSessionExpiredError("expired")}).expiredReason(); !ok || reason != "expired" {
		t.Fatalf("expiredReason = %q %v", reason, ok)
	}
}

func TestCreateRobotEscCancelsToTheRobotsList(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	_, cmd, _ := s.Update(env, testKeyEsc())
	if msg, ok := cmd().(openRobotsMsg); !ok || msg.selectID != "" {
		t.Fatalf("Esc must open the Robots list, got %#v", cmd())
	}
}

func TestCreateRobotPasteGoesIntoTheFocusedFieldOnly(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s, _ = accessPress(t, s, env, keyMsg{Text: "ci bot\n"}, testKeyTab(), testKeyTab(), keyMsg{Text: " team/api\n"})
	got := s.(createRobotScreen)
	if got.name != "ci bot" || got.repository != "team/api" {
		t.Fatalf("name=%q repository=%q", got.name, got.repository)
	}
}

func TestCreateRobotBackspaceAndShiftTab(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	s, _ = accessPress(t, s, env, accessType("abc")...)
	s, _ = accessPress(t, s, env, testKeyBackspace())
	if got := s.(createRobotScreen).name; got != "ab" {
		t.Fatalf("name = %q", got)
	}
	s, _ = accessPress(t, s, env, testKeyShiftTab())
	if got := s.(createRobotScreen).focus; got != robotFieldLifetime {
		t.Fatalf("Shift+Tab focus = %v, want Lifetime", got)
	}
}

func TestCreateRobotKeysChangeWithTheState(t *testing.T) {
	t.Parallel()
	form := newCreateRobotScreen()
	done := form
	done.done = true
	if len(form.Keys().short) == len(done.Keys().short) {
		t.Fatal("the footer must differ between the form and the secret state")
	}
}

func TestCreateRobotStartsAsReaderOnTheNameFieldAndTabCyclesAllFiveFields(t *testing.T) {
	t.Parallel()
	_, env, s := newCreateRobotFixture()
	got := s.(createRobotScreen)
	if got.role != domainauth.RepoRoleReader || got.focus != robotFieldName {
		t.Fatalf("role=%q focus=%v, want Reader on Name (never the unrestricted role by accident)", got.role, got.focus)
	}
	want := []robotField{robotFieldScope, robotFieldRepository, robotFieldRole, robotFieldLifetime, robotFieldName}
	for i, focus := range want {
		s, _ = accessPress(t, s, env, testKeyTab())
		if got := s.(createRobotScreen).focus; got != focus {
			t.Fatalf("Tab %d: focus = %v, want %v", i+1, got, focus)
		}
	}
}

var createRobotOverflowStates = []struct {
	name string
	keys []keyMsg
}{
	{"repository_focus", nil},
	{"role_focus", []keyMsg{testKeyTab()}},
	{"custom_empty", toCustomKeys()},
	{"custom_valid", append(toCustomKeys(), accessType("12h")...)},
	{"custom_invalid", append(toCustomKeys(), accessType("3y")...)},
}

// toCustomKeys moves the focus to Lifetime and selects Custom.
func toCustomKeys() []keyMsg {
	keys := []keyMsg{testKeyTab(), testKeyTab()}
	for range 5 {
		keys = append(keys, testKeyRight())
	}
	return keys
}

// The Create robot form keeps the overflow budgeting of the screen it
// replaced: at every size and for 0 to 5 matching repositories, in every
// Lifetime state, it fits the terminal, keeps the highlighted suggestion and
// the Lifetime rows visible, and only the suggestion list shrinks.
func TestCreateRobotFormNeverExceedsTheTerminal(t *testing.T) {
	t.Parallel()
	for _, size := range accessSizes {
		for suggestions := 0; suggestions <= 5; suggestions++ {
			for _, state := range createRobotOverflowStates {
				t.Run(fmt.Sprintf("%s n=%d %s", sizeLabel(size.w, size.h), suggestions, state.name), func(t *testing.T) {
					t.Parallel()
					var known []string
					for i := range suggestions {
						known = append(known, fmt.Sprintf("team/app-%d", i+1))
					}
					env := accessEnvSized(newAccessClient(), size.w, size.h, known...)
					s := fillRobot(t, newCreateRobotScreen(), env, "ci-bot", "team/app")
					s, _ = accessPress(t, s, env, state.keys...)
					view := accessView(s, env)
					if lines := accessLines(view); len(lines) > size.h {
						t.Fatalf("%d rows, terminal has %d", len(lines), size.h)
					}
					if suggestions > 0 {
						mustContain(t, view, "team/app-1")
					}
					mustContain(t, view, "Lifetime", "esc")
				})
			}
		}
	}
}

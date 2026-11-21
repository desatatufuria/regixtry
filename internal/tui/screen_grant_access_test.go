package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// grantPut is one recorded PutUserGrant call.
type grantPut struct {
	UserID     string
	Repository string
	Role       domainauth.RepoRole
}

// recordingGrantClient extends the shared fakeAdminClient (test-only) so the
// grant screen's calls are recorded exactly, in order.
type recordingGrantClient struct {
	*fakeAdminClient
	puts      []grantPut
	putErrors map[string]error
	grantsFor map[string][]ports.AdminRepoGrant
	listed    []string
}

func newRecordingGrantClient() *recordingGrantClient {
	return &recordingGrantClient{
		fakeAdminClient: &fakeAdminClient{
			users: []ports.AdminUser{
				{ID: "u-1", Username: "alice", Enabled: true},
				{ID: "u-2", Username: "bob", Enabled: true},
				{ID: "u-3", Username: "Carol", Enabled: true},
			},
			robots: []ports.AdminRobot{
				{ID: "r-1", Username: "ci-bot", Enabled: true},
				{ID: "r-2", Username: "deploy-bot", Enabled: true},
			},
		},
		putErrors: map[string]error{},
		grantsFor: map[string][]ports.AdminRepoGrant{},
	}
}

func (c *recordingGrantClient) PutUserGrant(_ context.Context, _ AdminSession, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error) {
	c.puts = append(c.puts, grantPut{UserID: input.UserID, Repository: input.Repository, Role: input.Role})
	if err := c.putErrors[input.Repository]; err != nil {
		return ports.AdminRepoGrant{}, err
	}
	grant := ports.AdminRepoGrant{UserID: input.UserID, Repository: regixtrydomain.MustParseRepositoryRef(input.Repository), Role: input.Role}
	kept := c.grantsFor[input.UserID][:0:0]
	for _, existing := range c.grantsFor[input.UserID] {
		if existing.Repository.Name != input.Repository {
			kept = append(kept, existing)
		}
	}
	c.grantsFor[input.UserID] = append(kept, grant)
	return grant, nil
}

func (c *recordingGrantClient) ListUserGrants(_ context.Context, _ AdminSession, userID string) ([]ports.AdminRepoGrant, error) {
	c.listed = append(c.listed, userID)
	if c.grantsErr != nil {
		return nil, c.grantsErr
	}
	return append([]ports.AdminRepoGrant(nil), c.grantsFor[userID]...), nil
}

func grantTestGrant(userID, repo string, role domainauth.RepoRole) ports.AdminRepoGrant {
	return ports.AdminRepoGrant{UserID: userID, Repository: regixtrydomain.MustParseRepositoryRef(repo), Role: role}
}

var grantTestKnown = []string{"library/alpine", "team/backend", "team/demo", "ops/console"}

func grantTestEnv(client AdminClient, known ...string) screenEnv {
	if known == nil {
		known = grantTestKnown
	}
	return screenEnv{
		Client:            client,
		Session:           AdminSession{Username: "operator", BearerToken: "token"},
		Layout:            consoleLayout{Width: 150, Height: 24, SectionRows: 17},
		KnownRepositories: known,
		Now:               func() time.Time { return time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC) },
	}
}

// grantSettle runs cmd and feeds its message back until no command is left.
func grantSettle(s adminScreen, env screenEnv, cmd tea.Cmd) adminScreen {
	for cmd != nil {
		msg := cmd()
		s, cmd, _ = s.Update(env, msg)
	}
	return s
}

func grantOpen(t *testing.T, env screenEnv, preselect string) grantAccessScreen {
	t.Helper()
	s := newGrantAccessScreen(screenAdminOverview, preselect)
	return grantSettle(s, env, s.Init(env)).(grantAccessScreen)
}

// grantPress sends the keys one by one, running every returned command.
func grantPress(t *testing.T, s grantAccessScreen, env screenEnv, keys ...keyMsg) grantAccessScreen {
	t.Helper()
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		s = grantSettle(next, env, cmd).(grantAccessScreen)
	}
	return s
}

func grantType(text string) []keyMsg {
	keys := make([]keyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

func TestGrantAccessScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()
	var _ adminScreen = grantAccessScreen{}
}

// ---- pure logic ----

func TestMergeGrantPrincipalsListsUsersThenRobotsAndDedupes(t *testing.T) {
	t.Parallel()
	got := mergeGrantPrincipals(
		[]ports.AdminUser{{ID: "u-1", Username: "alice"}, {ID: "x-1", Username: "dup"}},
		[]ports.AdminRobot{{ID: "r-1", Username: "ci-bot"}, {ID: "x-1", Username: "dup"}},
	)
	want := []grantPrincipal{
		{ID: "u-1", Name: "alice", Kind: "user"},
		{ID: "x-1", Name: "dup", Kind: "user"},
		{ID: "r-1", Name: "ci-bot", Kind: "robot"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeGrantPrincipals() = %#v, want %#v", got, want)
	}
}

func TestFilterGrantPrincipalsMatchesUsersAndRobotsCaseInsensitively(t *testing.T) {
	t.Parallel()
	all := []grantPrincipal{
		{ID: "u-1", Name: "Alice", Kind: "user"},
		{ID: "u-2", Name: "bob", Kind: "user"},
		{ID: "r-1", Name: "ci-bot", Kind: "robot"},
		{ID: "r-2", Name: "deploy-BOT", Kind: "robot"},
	}
	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{"empty keeps all", "", []string{"u-1", "u-2", "r-1", "r-2"}},
		{"case insensitive substring", "ALI", []string{"u-1"}},
		{"matches users and robots", "bot", []string{"r-1", "r-2"}},
		{"bob is a substring of bot? no", "bob", []string{"u-2"}},
		{"no match", "zzz", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			for _, p := range filterGrantPrincipals(all, tt.filter) {
				ids = append(ids, p.ID)
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("filter %q ids = %v, want %v", tt.filter, ids, tt.want)
			}
		})
	}
}

func TestGrantRepoRowsFilterAndNotPushedYetRow(t *testing.T) {
	t.Parallel()
	known := []string{"library/alpine", "team/backend", "team/demo", "ops/console"}
	tests := []struct {
		name   string
		filter string
		want   []grantRepoRow
	}{
		{"empty shows every known row and no extra row", "", []grantRepoRow{{Name: "library/alpine"}, {Name: "team/backend"}, {Name: "team/demo"}, {Name: "ops/console"}}},
		{"case-insensitive substring", "TEAM", []grantRepoRow{{Name: "team/backend"}, {Name: "team/demo"}}},
		{"valid unknown name adds a trailing extra row", "team/new", []grantRepoRow{{Name: "team/new", New: true}}},
		{"valid unknown name keeps matching known rows first", "team", []grantRepoRow{{Name: "team/backend"}, {Name: "team/demo"}, {Name: "team", New: true}}},
		{"exact known name adds no extra row", "team/demo", []grantRepoRow{{Name: "team/demo"}}},
		{"invalid name adds no extra row", "Bad Name!", nil},
		{"uppercase is not a valid repository name", "TEAM/NEW", nil},
		{"filter is trimmed", "  team/new ", []grantRepoRow{{Name: "team/new", New: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := grantRepoRows(known, tt.filter)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("grantRepoRows(%q) = %#v, want %#v", tt.filter, got, tt.want)
			}
		})
	}
}

func TestGrantSelectionSurvivesFilterAndOrdersKnownBeforeExtras(t *testing.T) {
	t.Parallel()
	known := []string{"library/alpine", "team/backend", "team/demo", "ops/console"}
	var sel grantSelection
	// Pick in a deliberately scrambled order, through different filters.
	for _, filter := range []string{"ops", "team/new", "team/backend", "zz/other-new"} {
		rows := grantRepoRows(known, filter)
		sel = sel.toggled(rows[0])
	}
	got := sel.ordered(known)
	want := []string{"team/backend", "ops/console", "team/new", "zz/other-new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered() = %v, want known order then extras in pick order %v", got, want)
	}
	// Toggling again removes, wherever it came from.
	sel = sel.toggled(grantRepoRow{Name: "team/new", New: true})
	if sel.has("team/new") {
		t.Fatal("toggling a selected extra row did not unselect it")
	}
	if got := sel.ordered(known); !reflect.DeepEqual(got, []string{"team/backend", "ops/console", "zz/other-new"}) {
		t.Fatalf("ordered() after unselect = %v", got)
	}
}

func TestGrantSelectionToggleDoesNotMutateTheOriginal(t *testing.T) {
	t.Parallel()
	base := (grantSelection{}).toggled(grantRepoRow{Name: "team/demo"})
	next := base.toggled(grantRepoRow{Name: "ops/console"})
	if base.has("ops/console") {
		t.Fatal("toggled() mutated its receiver")
	}
	if !next.has("ops/console") || !next.has("team/demo") {
		t.Fatal("toggled() lost state")
	}
}

func TestGrantSelectionToggleAllShown(t *testing.T) {
	t.Parallel()
	rows := []grantRepoRow{{Name: "a/one"}, {Name: "a/two"}, {Name: "a/new", New: true}}
	hidden := grantRepoRow{Name: "b/hidden"}

	all := (grantSelection{}).toggled(hidden).toggledAll(rows)
	for _, name := range []string{"a/one", "a/two", "b/hidden"} {
		if !all.has(name) {
			t.Fatalf("toggledAll from empty: %s not selected", name)
		}
	}
	none := all.toggledAll(rows)
	for _, name := range []string{"a/one", "a/two"} {
		if none.has(name) {
			t.Fatalf("toggledAll when all shown selected: %s still selected", name)
		}
	}
	if !none.has("b/hidden") {
		t.Fatal("toggledAll touched a row that is not shown")
	}
	mixed := (grantSelection{}).toggled(rows[0]).toggledAll(rows)
	for _, row := range rows[:2] {
		if !mixed.has(row.Name) {
			t.Fatalf("toggledAll from a mixed state must select all shown known rows; %s is not", row.Name)
		}
	}
	if mixed.has("a/new") {
		t.Fatal("toggledAll ticked the new-name row")
	}
	if got := (grantSelection{}).toggledAll(nil); len(got.ordered(nil)) != 0 {
		t.Fatal("toggledAll(nil) must stay empty")
	}
}

func TestGrantRoleOptionsAndDefault(t *testing.T) {
	t.Parallel()
	want := []struct {
		role  domainauth.RepoRole
		label string
		desc  string
	}{
		{domainauth.RepoRoleReader, "Reader", "pull"},
		{domainauth.RepoRoleWriter, "Writer", "pull, push, delete tags"},
		{domainauth.RepoRoleAdmin, "Admin", "Writer plus manage who has access"},
	}
	if len(grantRoleOptions) != len(want) {
		t.Fatalf("len(grantRoleOptions) = %d, want %d", len(grantRoleOptions), len(want))
	}
	for i, w := range want {
		got := grantRoleOptions[i]
		if got.Role != w.role || got.Label != w.label || got.Description != w.desc {
			t.Fatalf("grantRoleOptions[%d] = %#v, want %v", i, got, w)
		}
		if grantRoleLabel(w.role) != w.label {
			t.Fatalf("grantRoleLabel(%q) = %q, want %q", w.role, grantRoleLabel(w.role), w.label)
		}
	}
	if grantRoleLabel("weird") != "weird" {
		t.Fatalf("grantRoleLabel of an unknown role must echo it")
	}
	// The default role is the one the existing add-grant form starts with.
	screen := newGrantAccessScreen(screenAdminOverview, "")
	if screen.role != domainauth.RepoRoleReader {
		t.Fatalf("default role = %q, want %q", screen.role, domainauth.RepoRoleReader)
	}
}

func TestGrantRoleOnFindsExactRepositoryOnly(t *testing.T) {
	t.Parallel()
	grants := []ports.AdminRepoGrant{
		grantTestGrant("u-1", "team/demo", domainauth.RepoRoleReader),
		grantTestGrant("u-1", "team/demo-2", domainauth.RepoRoleAdmin),
	}
	if role, ok := grantRoleOn(grants, "team/demo"); !ok || role != domainauth.RepoRoleReader {
		t.Fatalf("grantRoleOn(team/demo) = %q, %v", role, ok)
	}
	if _, ok := grantRoleOn(grants, "team"); ok {
		t.Fatal("grantRoleOn matched a prefix; grants match the exact name only")
	}
}

func TestGrantReviewLines(t *testing.T) {
	t.Parallel()
	t.Run("lists names under the sentence", func(t *testing.T) {
		got := grantReviewLines("alice", domainauth.RepoRoleWriter, []string{"a/one", "b/two"}, 60, 10)
		want := []string{"alice becomes Writer on 2 repositories:", "a/one, b/two"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("grantReviewLines = %#v, want %#v", got, want)
		}
	})
	t.Run("truncates with +k more inside the budget", func(t *testing.T) {
		names := []string{"a/1", "a/2", "a/3", "a/4", "a/5", "a/6", "a/7", "a/8", "a/9", "a/10"}
		got := grantReviewLines("alice", domainauth.RepoRoleReader, names, 20, 4)
		want := []string{"alice becomes Reader", "on 10 repositories:", "a/1, a/2, a/3, a/4,", "+6 more"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("grantReviewLines = %#v, want %#v", got, want)
		}
	})
	t.Run("exact fit does not add +k more", func(t *testing.T) {
		got := grantReviewLines("alice", domainauth.RepoRoleReader, []string{"a/1", "a/2", "a/3"}, 60, 2)
		if len(got) != 2 || strings.Contains(strings.Join(got, "\n"), "more") {
			t.Fatalf("grantReviewLines = %#v", got)
		}
	})
	t.Run("names are one wrapped comma-separated line", func(t *testing.T) {
		names := []string{"a/1", "a/2", "a/3", "a/4", "a/5", "a/6"}
		got := grantReviewLines("alice", domainauth.RepoRoleReader, names, 60, 4)
		want := []string{"alice becomes Reader on 6 repositories:", "a/1, a/2, a/3, a/4, a/5, a/6"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("grantReviewLines = %#v, want %#v", got, want)
		}
		one := grantReviewLines("alice", domainauth.RepoRoleReader, []string{"a/1"}, 60, 4)
		if one[0] != "alice becomes Reader on 1 repository:" {
			t.Fatalf("singular = %#v", one)
		}
	})
	t.Run("wraps a long sentence and still respects the budget", func(t *testing.T) {
		got := grantReviewLines("a-very-long-robot-name-indeed", domainauth.RepoRoleAdmin, []string{"a/1", "a/2", "a/3", "a/4"}, 30, 4)
		if len(got) > 4 {
			t.Fatalf("got %d lines, budget is 4: %#v", len(got), got)
		}
		for _, line := range got {
			if len([]rune(line)) > 30 {
				t.Fatalf("line %q wider than 30", line)
			}
		}
		if !strings.Contains(strings.Join(got, " "), "Admin") {
			t.Fatalf("role missing: %#v", got)
		}
	})
	t.Run("nothing selected", func(t *testing.T) {
		got := grantReviewLines("alice", domainauth.RepoRoleReader, nil, 60, 5)
		if len(got) == 0 || strings.Contains(strings.Join(got, " "), "becomes") {
			t.Fatalf("grantReviewLines(no names) = %#v, want a hint and no sentence", got)
		}
	})
}

// ---- key helpers ----

func TestGrantKeyHelpers(t *testing.T) {
	t.Parallel()
	if !isArrowUpKey(testKeyUp()) || isArrowUpKey(testKeyRunes("k")) {
		t.Fatal("isArrowUpKey must match Up and not the letter k")
	}
	if !isArrowDownKey(testKeyDown()) || isArrowDownKey(testKeyRunes("j")) {
		t.Fatal("isArrowDownKey must match Down and not the letter j")
	}
	if !isCtrlKey(testKeyCtrl('a'), 'a') || isCtrlKey(testKeyRunes("a"), 'a') || isCtrlKey(testKeyCtrl('b'), 'a') {
		t.Fatal("isCtrlKey must match only the requested ctrl combination")
	}
	if isCtrlKey(keyMsg{Text: "ctrl+a"}, 'a') {
		t.Fatal("pasted text spelling ctrl+a must not count as the key")
	}
	if !isPasteKey(keyMsg{Text: "hello"}) || isPasteKey(testKeyRunes("h")) || isPasteKey(testKeySpace()) {
		t.Fatal("isPasteKey must match only synthetic paste keys (no key code)")
	}
}

// ---- Update behavior ----

func TestGrantScreenInitLoadsUsersAndRobotsInOneCommand(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := newGrantAccessScreen(screenAdminOverview, "")
	cmd := s.Init(env)
	if cmd == nil {
		t.Fatal("Init returned nil")
	}
	if client.listUsersCalls != 0 || client.listRobotsCalls != 0 {
		t.Fatal("Init must not call the client before its command runs")
	}
	next, nextCmd, _ := s.Update(env, cmd())
	if client.listUsersCalls != 1 || client.listRobotsCalls != 1 {
		t.Fatalf("users calls = %d, robots calls = %d, want 1 each", client.listUsersCalls, client.listRobotsCalls)
	}
	loaded := next.(grantAccessScreen)
	if len(loaded.principals) != 5 {
		t.Fatalf("principals = %d, want 3 users + 2 robots", len(loaded.principals))
	}
	if nextCmd != nil {
		t.Fatal("no principal is highlighted yet, so no grants load is expected")
	}
}

func TestGrantScreenLoadsAlreadyHasOncePerPrincipal(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.grantsFor["u-1"] = []ports.AdminRepoGrant{grantTestGrant("u-1", "team/demo", domainauth.RepoRoleReader)}
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	if !reflect.DeepEqual(client.listed, []string{"u-1"}) {
		t.Fatalf("listed after open = %v, want only the chosen principal", client.listed)
	}
	// Moving the highlight loads nothing; choosing a principal loads it once.
	s = grantPress(t, s, env, testKeyDown(), testKeyUp(), testKeyDown(), testKeyUp())
	if !reflect.DeepEqual(client.listed, []string{"u-1"}) {
		t.Fatalf("listed after only moving the highlight = %v, want no new call", client.listed)
	}
	s = grantPress(t, s, env, testKeyDown(), testKeySpace(), testKeyUp(), testKeySpace(), testKeyDown(), testKeySpace())
	if !reflect.DeepEqual(client.listed, []string{"u-1", "u-2"}) {
		t.Fatalf("listed after choosing back and forth = %v, want one call per principal (cached)", client.listed)
	}
	if got := s.grants["u-1"].items; len(got) != 1 {
		t.Fatalf("cached grants for u-1 = %v", got)
	}
}

func TestGrantScreenNothingIsCalledBeforeEnter(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeySpace(), testKeyDown(), testKeySpace(), testKeyCtrl('a'), testKeyTab(), testKeyRunes("2"), testKeyDown())
	if len(client.puts) != 0 {
		t.Fatalf("puts before Enter = %v, want none", client.puts)
	}
	if s.role != domainauth.RepoRoleAdmin {
		t.Fatalf("role = %q, want Admin after 2 then Down", s.role)
	}
}

func TestGrantScreenEnterWithNothingSelectedCallsNothingAndSaysSo(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	next, cmd, consumed := s.Update(env, testKeyEnter())
	if cmd != nil {
		t.Fatal("Enter with no repository selected must not return a command")
	}
	if !consumed {
		t.Fatal("Enter must be consumed")
	}
	got := next.(grantAccessScreen)
	if !strings.Contains(got.status, "Select at least one repository") {
		t.Fatalf("status = %q, want a clear message", got.status)
	}
	if len(client.puts) != 0 {
		t.Fatal("PutUserGrant was called")
	}
}

func TestGrantScreenEnterWithoutAPrincipalCallsNothingAndSaysSo(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.users, client.robots = nil, nil
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeySpace())
	next, cmd, _ := s.Update(env, testKeyEnter())
	if cmd != nil {
		t.Fatal("Enter with no principal must not return a command")
	}
	if got := next.(grantAccessScreen).status; !strings.Contains(got, "Pick who to grant access to first") {
		t.Fatalf("status = %q", got)
	}
	if len(client.puts) != 0 {
		t.Fatal("PutUserGrant was called")
	}
}

func TestGrantScreenApplyIssuesOneGrantPerRepositoryInShownOrderInOneCommand(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	// Move to robot ci-bot (index 3), pick rows in scrambled order: 3rd then 1st, then a new name.
	s = grantPress(t, s, env, testKeyDown(), testKeyDown(), testKeyDown(), testKeySpace())
	s = grantPress(t, s, env, testKeyTab(), testKeyDown(), testKeyDown(), testKeySpace(), testKeyUp(), testKeyUp(), testKeySpace())
	s = grantPress(t, s, env, grantType("zz/fresh")...)
	s = grantPress(t, s, env, testKeySpace())
	s = grantPress(t, s, env, testKeyTab(), testKeyRunes("2"))
	if len(client.puts) != 0 {
		t.Fatalf("puts before Enter = %v", client.puts)
	}

	next, cmd, _ := s.Update(env, testKeyEnter())
	if cmd == nil {
		t.Fatal("Enter returned no command")
	}
	if !next.(grantAccessScreen).applying {
		t.Fatal("screen must be applying while the command runs")
	}
	// A second Enter while applying must not start another run.
	if _, again, _ := next.Update(env, testKeyEnter()); again != nil {
		t.Fatal("Enter while applying returned a command")
	}
	msg := cmd()
	applied, ok := msg.(grantAccessAppliedMsg)
	if !ok {
		t.Fatalf("command returned %T, want exactly one grantAccessAppliedMsg", msg)
	}
	if len(applied.outcomes) != 3 {
		t.Fatalf("outcomes = %v, want 3", applied.outcomes)
	}
	want := []grantPut{
		{UserID: "r-1", Repository: "library/alpine", Role: domainauth.RepoRoleWriter},
		{UserID: "r-1", Repository: "team/demo", Role: domainauth.RepoRoleWriter},
		{UserID: "r-1", Repository: "zz/fresh", Role: domainauth.RepoRoleWriter},
	}
	if !reflect.DeepEqual(client.puts, want) {
		t.Fatalf("puts = %#v, want %#v", client.puts, want)
	}
}

func TestGrantScreenPartialFailureKeepsFailedCheckedAndReportsPerRepository(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.putErrors["team/backend"] = errors.New("quota exceeded")
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	// Select library/alpine, team/backend, team/demo with Ctrl+A minus ops/console.
	s = grantPress(t, s, env, testKeyTab(), testKeyCtrl('a'), testKeyDown(), testKeyDown(), testKeyDown(), testKeySpace())
	s = grantPress(t, s, env, testKeyTab(), testKeyDown(), testKeyEnter())

	wantPuts := []grantPut{
		{"u-1", "library/alpine", domainauth.RepoRoleWriter},
		{"u-1", "team/backend", domainauth.RepoRoleWriter},
		{"u-1", "team/demo", domainauth.RepoRoleWriter},
	}
	if !reflect.DeepEqual(client.puts, wantPuts) {
		t.Fatalf("puts = %#v, want %#v", client.puts, wantPuts)
	}
	if s.applying {
		t.Fatal("still applying after the result arrived")
	}
	if want := "Granted Writer on 2 of 3 repositories to alice"; s.status != want {
		t.Fatalf("status = %q, want %q", s.status, want)
	}
	if !s.selection.has("team/backend") || s.selection.has("library/alpine") || s.selection.has("team/demo") {
		t.Fatalf("selection after partial failure: backend checked=%v alpine=%v demo=%v, want only the failed one still checked",
			s.selection.has("team/backend"), s.selection.has("library/alpine"), s.selection.has("team/demo"))
	}
	var rendered []string
	for _, r := range s.results {
		if r.err != nil {
			rendered = append(rendered, r.repo+": "+r.err.Error())
		} else {
			rendered = append(rendered, r.repo+": granted")
		}
	}
	wantResults := []string{"library/alpine: granted", "team/backend: quota exceeded", "team/demo: granted"}
	if !reflect.DeepEqual(rendered, wantResults) {
		t.Fatalf("results = %v, want %v", rendered, wantResults)
	}
	view := s.View(newAdminTheme(), env).Body
	for _, want := range []string{"granted", "quota exceeded"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not show %q:\n%s", want, view)
		}
	}
	if s.returnTo != screenAdminOverview {
		t.Fatal("screen state lost")
	}

	// Retry: only the failed repository is attempted again.
	delete(client.putErrors, "team/backend")
	client.puts = nil
	s = grantPress(t, s, env, testKeyEnter())
	if !reflect.DeepEqual(client.puts, []grantPut{{"u-1", "team/backend", domainauth.RepoRoleWriter}}) {
		t.Fatalf("retry puts = %#v", client.puts)
	}
	if want := "Granted Writer on 1 of 1 repositories to alice"; s.status != want {
		t.Fatalf("retry status = %q, want %q", s.status, want)
	}
	if len(s.selection.ordered(grantTestKnown)) != 0 {
		t.Fatal("selection must be empty after a full success")
	}
}

func TestGrantScreenReloadsAlreadyHasAfterApplyAndAnnotatesRows(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.grantsFor["u-1"] = []ports.AdminRepoGrant{grantTestGrant("u-1", "team/demo", domainauth.RepoRoleReader)}
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	if !strings.Contains(s.View(newAdminTheme(), env).Body, "alice is Reader") {
		t.Fatalf("row annotation missing before apply:\n%s", s.View(newAdminTheme(), env).Body)
	}
	before := len(client.listed)
	s = grantPress(t, s, env, testKeyTab(), testKeyDown(), testKeySpace(), testKeyTab(), testKeyRunes("3"), testKeyEnter())
	if len(client.listed) != before+1 || client.listed[len(client.listed)-1] != "u-1" {
		t.Fatalf("listed = %v, want one more ListUserGrants for u-1 after applying", client.listed)
	}
	body := s.View(newAdminTheme(), env).Body
	if !strings.Contains(body, "alice is Admin") {
		t.Fatalf("annotation not refreshed after apply:\n%s", body)
	}
}

func TestGrantScreenLettersAreTypedIntoFiltersAndBackspaceDeletes(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)

	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyRunes("j"), testKeyRunes("k"))
	if s.whoFilter != "jk" {
		t.Fatalf("who filter = %q, want %q (j and k are letters here)", s.whoFilter, "jk")
	}
	s = grantPress(t, s, env, testKeyBackspace())
	if s.whoFilter != "j" {
		t.Fatalf("who filter after Backspace = %q, want %q", s.whoFilter, "j")
	}
	if s.focus != grantPaneWho {
		t.Fatal("Backspace must not leave the pane")
	}

	s = grantPress(t, s, env, testKeyTab(), testKeyRunes("k"), testKeyRunes("j"), testKeyRunes("q"), testKeyRunes("l"))
	if s.repoFilter != "kjql" {
		t.Fatalf("repo filter = %q, want %q", s.repoFilter, "kjql")
	}
	s = grantPress(t, s, env, testKeyBackspace(), testKeyBackspace())
	if s.repoFilter != "kj" {
		t.Fatalf("repo filter after Backspace = %q, want %q", s.repoFilter, "kj")
	}
	if len(client.puts) != 0 {
		t.Fatal("unexpected grant call")
	}
}

func TestGrantScreenSpaceTogglesOnlyInRepositoriesAndNeverTypesIntoAFilter(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeySpace())
	if s.whoFilter != "" {
		t.Fatalf("Space typed into the who filter: %q", s.whoFilter)
	}
	s = grantPress(t, s, env, testKeyTab(), testKeyRunes("t"), testKeySpace())
	if s.repoFilter != "t" {
		t.Fatalf("repo filter = %q, want %q (Space must not type)", s.repoFilter, "t")
	}
	if !s.selection.has("team/backend") {
		t.Fatal("Space did not toggle the highlighted repository")
	}
	s = grantPress(t, s, env, testKeySpace())
	if s.selection.has("team/backend") {
		t.Fatal("second Space did not untoggle")
	}
}

func TestGrantScreenPastedKeyNamesStayTextInFilters(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	for _, pasted := range []string{"enter", "esc", "tab", "ctrl+a", "q"} {
		var cmd tea.Cmd
		var next adminScreen
		next, cmd, _ = s.Update(env, keyMsg{Text: pasted})
		if cmd != nil {
			if _, isNav := cmd().(navigateMsg); isNav {
				t.Fatalf("pasted %q acted as a key (navigated)", pasted)
			}
		}
		s = next.(grantAccessScreen)
	}
	if s.whoFilter != "enteresctabctrl+aq" {
		t.Fatalf("who filter = %q, want the pasted text", s.whoFilter)
	}
	if s.focus != grantPaneWho {
		t.Fatal("a pasted tab moved the focus")
	}
	if len(client.puts) != 0 {
		t.Fatal("a pasted enter applied")
	}

	s = grantPress(t, s, env, testKeyTab())
	next, cmd, _ := s.Update(env, keyMsg{Text: "enter"})
	if cmd != nil {
		t.Fatal("pasted enter in the repositories pane returned a command")
	}
	s = next.(grantAccessScreen)
	if s.repoFilter != "enter" {
		t.Fatalf("repo filter = %q, want %q", s.repoFilter, "enter")
	}
	// A pasted newline or space never reaches a filter.
	next, _, _ = s.Update(env, keyMsg{Text: "a b\nc"})
	if got := next.(grantAccessScreen).repoFilter; got != "enterabc" {
		t.Fatalf("repo filter after multi-line paste = %q, want %q", got, "enterabc")
	}
}

func TestGrantScreenEscLeavesToWhereItWasOpenedFrom(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	for _, from := range []screen{screenAdminOverview, screenAdminUsers, screenAdminRobots} {
		s := newGrantAccessScreen(from, "")
		for _, k := range []keyMsg{testKeyEsc()} {
			_, cmd, consumed := s.Update(env, k)
			if !consumed || cmd == nil {
				t.Fatalf("Esc from %s: consumed=%v cmd=%v", from, consumed, cmd != nil)
			}
			if msg, ok := cmd().(navigateMsg); !ok || msg.To != from {
				t.Fatalf("Esc from %s navigated to %#v", from, cmd())
			}
		}
	}
	// Esc also leaves from the text panes with a filter typed.
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyRunes("a"))
	if _, cmd, _ := s.Update(env, testKeyEsc()); cmd == nil {
		t.Fatal("Esc with text in the filter did not leave")
	}
}

func TestGrantScreenTabAndShiftTabCycleThePanes(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	s := grantOpenFirst(t, env)
	order := []grantPane{grantPaneRepos, grantPaneRole, grantPaneWho, grantPaneRepos}
	for i, want := range order {
		s = grantPress(t, s, env, testKeyTab())
		if s.focus != want {
			t.Fatalf("after %d Tab focus = %v, want %v", i+1, s.focus, want)
		}
	}
	s = grantPress(t, s, env, testKeyShiftTab(), testKeyShiftTab())
	if s.focus != grantPaneRole {
		t.Fatalf("after two Shift+Tab focus = %v, want the role pane", s.focus)
	}
}

func TestGrantScreenRolePaneKeys(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeyTab())
	if s.focus != grantPaneRole {
		t.Fatal("not on the role pane")
	}
	for _, tc := range []struct {
		keys []keyMsg
		want domainauth.RepoRole
	}{
		{[]keyMsg{testKeyRunes("2")}, domainauth.RepoRoleWriter},
		{[]keyMsg{testKeyRunes("3")}, domainauth.RepoRoleAdmin},
		{[]keyMsg{testKeyRunes("1")}, domainauth.RepoRoleReader},
		{[]keyMsg{testKeyDown()}, domainauth.RepoRoleWriter},
		{[]keyMsg{testKeyDown(), testKeyDown(), testKeyDown()}, domainauth.RepoRoleAdmin},
		{[]keyMsg{testKeyUp(), testKeyUp(), testKeyUp(), testKeyUp()}, domainauth.RepoRoleReader},
		{[]keyMsg{testKeyRunes("x"), testKeyRunes("j")}, domainauth.RepoRoleReader},
	} {
		s = grantPress(t, s, env, tc.keys...)
		if s.role != tc.want {
			t.Fatalf("after %v role = %q, want %q", tc.keys, s.role, tc.want)
		}
	}
	// Digits are typed in a filter pane, they do not pick a role there.
	s = grantPress(t, s, env, testKeyTab(), testKeyRunes("3"))
	if s.role != domainauth.RepoRoleReader || s.whoFilter != "3" {
		t.Fatalf("digit in the who pane: role=%q filter=%q", s.role, s.whoFilter)
	}
}

func TestGrantScreenCtrlAToggleAllShownKeepsHiddenSelection(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeyCtrl('a'))
	if got := len(s.selection.ordered(grantTestKnown)); got != 4 {
		t.Fatalf("selected after Ctrl+A = %d, want 4", got)
	}
	s = grantPress(t, s, env, grantType("team/")...)
	s = grantPress(t, s, env, testKeyCtrl('a'))
	got := s.selection.ordered(grantTestKnown)
	if !reflect.DeepEqual(got, []string{"library/alpine", "ops/console"}) {
		t.Fatalf("selected after Ctrl+A on the filtered rows = %v, want the two hidden ones kept", got)
	}
}

func TestGrantScreenNotPushedYetNameCanBeGranted(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab())
	s = grantPress(t, s, env, grantType("brand/new")...)
	body := s.View(newAdminTheme(), env).Body
	if !strings.Contains(body, "+ brand/new") || !strings.Contains(body, "not pushed yet") {
		t.Fatalf("extra row missing:\n%s", body)
	}
	s = grantPress(t, s, env, testKeySpace(), testKeyEnter())
	if !reflect.DeepEqual(client.puts, []grantPut{{"u-1", "brand/new", domainauth.RepoRoleReader}}) {
		t.Fatalf("puts = %#v", client.puts)
	}
}

func TestGrantScreenWhoFilterNarrowsPrincipalsAndPicksTheHighlighted(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, grantType("BOT")...)
	got, ok := s.highlighted()
	if !ok || got.ID != "r-1" || got.Kind != "robot" {
		t.Fatalf("highlighted = %#v, %v, want robot ci-bot", got, ok)
	}
	s = grantPress(t, s, env, testKeyDown())
	got, _ = s.highlighted()
	if got.ID != "r-2" {
		t.Fatalf("highlighted after Down = %#v, want deploy-bot", got)
	}
	// Widening and narrowing keep the highlighted principal when it still matches.
	s = grantPress(t, s, env, testKeyBackspace())
	got, _ = s.highlighted()
	if got.ID != "r-2" {
		t.Fatalf("highlighted after widening the filter = %#v, want deploy-bot kept (not reset to the first match)", got)
	}
	s = grantPress(t, s, env, testKeyRunes("t"))
	got, _ = s.highlighted()
	if got.ID != "r-2" {
		t.Fatalf("highlighted after narrowing = %#v", got)
	}
	s = grantPress(t, s, env, grantType("zzz")...)
	if _, ok := s.highlighted(); ok {
		t.Fatal("a principal is highlighted with no match")
	}
	if body := s.View(newAdminTheme(), env).Body; !strings.Contains(body, "No users or robots match") {
		t.Fatalf("empty state missing:\n%s", body)
	}
}

func TestGrantScreenPreselectsPrincipalAndFocusesRepositories(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	s := grantOpen(t, env, "r-2")
	got, ok := s.highlighted()
	if !ok || got.ID != "r-2" {
		t.Fatalf("highlighted = %#v, want the preselected robot", got)
	}
	if s.focus != grantPaneRepos {
		t.Fatalf("focus = %v, want the repositories pane when a principal is preselected", s.focus)
	}
	if !reflect.DeepEqual(client.listed, []string{"r-2"}) {
		t.Fatalf("listed = %v, want grants only for the preselected principal", client.listed)
	}
	// Without a preselection the first pane is focused.
	if grantOpenFirst(t, env).focus != grantPaneWho {
		t.Fatal("default focus is not the who pane")
	}
}

func TestGrantScreenKeysAreIgnoredWhileApplying(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeySpace())
	next, cmd, _ := s.Update(env, testKeyEnter())
	applying := next.(grantAccessScreen)
	if cmd == nil || !applying.applying {
		t.Fatal("not applying")
	}
	after, c2, _ := applying.Update(env, testKeyRunes("x"))
	if c2 != nil || after.(grantAccessScreen).repoFilter != "" {
		t.Fatal("typing while applying changed state")
	}
	if _, c3, _ := applying.Update(env, testKeyEsc()); c3 != nil {
		t.Fatal("Esc must not leave while applying")
	}
}

func TestGrantScreenLoadingAndErrorStatesRender(t *testing.T) {
	t.Parallel()
	theme := newAdminTheme()
	t.Run("loading", func(t *testing.T) {
		env := grantTestEnv(newRecordingGrantClient())
		s := newGrantAccessScreen(screenAdminOverview, "")
		body := s.View(theme, env).Body
		if !strings.Contains(body, "Loading") {
			t.Fatalf("no loading state:\n%s", body)
		}
	})
	t.Run("users and robots failing", func(t *testing.T) {
		client := newRecordingGrantClient()
		client.usersErr = errors.New("users backend down")
		client.listRobotsErr = errors.New("robots backend down")
		env := grantTestEnv(client)
		s := grantOpenFirst(t, env)
		body := s.View(theme, env).Body
		for _, want := range []string{"users backend down", "robots backend down"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q:\n%s", want, body)
			}
		}
	})
	t.Run("only robots failing still lists users", func(t *testing.T) {
		client := newRecordingGrantClient()
		client.listRobotsErr = errors.New("robots backend down")
		env := grantTestEnv(client)
		s := grantOpenFirst(t, env)
		body := s.View(theme, env).Body
		if !strings.Contains(body, "alice") || !strings.Contains(body, "robots backend down") {
			t.Fatalf("partial load not shown:\n%s", body)
		}
	})
	t.Run("grants failing", func(t *testing.T) {
		client := newRecordingGrantClient()
		client.grantsErr = errors.New("grants backend down")
		env := grantTestEnv(client)
		s := grantOpenFirst(t, env)
		body := s.View(theme, env).Body
		if !strings.Contains(body, "grants backend down") {
			t.Fatalf("grants error not shown:\n%s", body)
		}
		// Choosing the principal again retries instead of staying failed forever.
		client.grantsErr = nil
		before := len(client.listed)
		s = grantPress(t, s, env, testKeyDown(), testKeySpace(), testKeyUp(), testKeySpace())
		if len(client.listed) <= before {
			t.Fatal("a failed grants load was never retried")
		}
	})
	t.Run("bare env does not panic", func(t *testing.T) {
		_ = newGrantAccessScreen(screenAdminOverview, "").View(theme, screenEnv{})
	})
}

// ---- model-level wiring ----

func grantModel(t *testing.T, client AdminClient, width, height int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, grantTestKnown, client)
	m.now = func() time.Time { return charFixedTime }
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(Model)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	return m
}

func grantModelPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = runCmd(t, updated.(Model), cmd)
	}
	return m
}

func TestGrantScreenTypesLettersThatAreGlobalKeysElsewhere(t *testing.T) {
	t.Parallel()
	m := grantModel(t, newRecordingGrantClient(), 150, 24)
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminOverview})
	m = runCmd(t, updated.(Model), cmd)
	// q quits and l logs out on other admin screens; here they are text.
	m = grantModelPress(t, m, testKeyRunes("q"), testKeyRunes("l"))
	s := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if m.screen != screenAdminGrantAccess || s.whoFilter != "ql" {
		t.Fatalf("screen=%q whoFilter=%q, want the letters typed", m.screen, s.whoFilter)
	}
}

func TestGrantScreenPasteReachesTheFocusedFilterThroughTheModel(t *testing.T) {
	t.Parallel()
	m := grantModel(t, newRecordingGrantClient(), 150, 24)
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminOverview})
	m = runCmd(t, updated.(Model), cmd)
	updated, cmd = m.Update(tea.PasteMsg{Content: "enter"})
	m = runCmd(t, updated.(Model), cmd)
	s := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if s.whoFilter != "enter" || m.screen != screenAdminGrantAccess {
		t.Fatalf("paste: screen=%q whoFilter=%q", m.screen, s.whoFilter)
	}
}

func TestGrantAccessScreenRoutesLikeEveryMigratedScreen(t *testing.T) {
	t.Parallel()
	slot, ok := slotFor(screenAdminGrantAccess)
	if !ok || slot != slotGrantAccess {
		t.Fatalf("slotFor(screenAdminGrantAccess) = %v, %v", slot, ok)
	}
	if !isAdminScreen(screenAdminGrantAccess) {
		t.Fatal("isAdminScreen is false for the grant access screen")
	}
	if isAdminPrincipalScreen(screenAdminGrantAccess) {
		t.Fatal("the grant screen has text filters; a bare q must not quit")
	}
	if _, isGrant := newAdminScreenFor(screenAdminGrantAccess).(grantAccessScreen); !isGrant {
		t.Fatal("newAdminScreenFor does not build the grant screen")
	}
	if got := newGrantAccessScreen(screenAdminOverview, "").Keys(); len(got.short) == 0 {
		t.Fatal("no key hints")
	}
	if keys := shortHelpView(newAdminTheme(), newGrantAccessScreen(screenAdminOverview, "").Keys()); !strings.Contains(keys, "space") || !strings.Contains(keys, "enter") {
		t.Fatalf("footer = %q", keys)
	}
}

func TestGrantSessionExpiryDuringApplyExpiresTheAdminSession(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantModel(t, client, 150, 24)
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminOverview})
	m = runCmd(t, updated.(Model), cmd)
	// The message must carry the open screen's generation (a foreign one is dropped).
	expired := grantAccessAppliedMsg{gen: grantScreenOf(t, m).gen, principalID: "u-1", outcomes: []grantOutcome{{repo: "team/demo", err: AdminSessionExpiredError{Reason: "expired"}}}}
	updated, _ = m.Update(expired)
	got := updated.(Model)
	if got.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login screen after an expired session", got.screen)
	}
}

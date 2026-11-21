package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
)

func grantOpenModel(t *testing.T, client AdminClient, preselect string) Model {
	t.Helper()
	m := grantModel(t, client, 150, 24)
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminMenu, preselectID: preselect})
	return runCmd(t, updated.(Model), cmd)
}

func grantScreenOf(t *testing.T, m Model) grantAccessScreen {
	t.Helper()
	s, ok := m.adminScreens[slotGrantAccess].(grantAccessScreen)
	if !ok {
		t.Fatalf("slot holds %T", m.adminScreens[slotGrantAccess])
	}
	return s
}

// B1: Ctrl+A never ticks the not-pushed-yet row.
func TestGrantCtrlANeverTicksTheNewNameRow(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client, "team/api", "team/app", "ops/console")
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab())
	s = grantPress(t, s, env, grantType("team/a")...)
	rows := grantRepoRows(env.KnownRepositories, s.repoFilter)
	if len(rows) != 3 || !rows[2].New {
		t.Fatalf("rows = %#v, want two known rows and a new-name row", rows)
	}
	s = grantPress(t, s, env, testKeyCtrl('a'))
	if s.selection.has("team/a") {
		t.Fatal("Ctrl+A ticked the new-name row")
	}
	s = grantPress(t, s, env, testKeyEnter())
	want := []grantPut{
		{"u-1", "team/api", domainauth.RepoRoleReader},
		{"u-1", "team/app", domainauth.RepoRoleReader},
	}
	if !reflect.DeepEqual(client.puts, want) {
		t.Fatalf("puts = %#v, want %#v", client.puts, want)
	}
	// Space on the new-name row still ticks it.
	s = grantPress(t, s, env, testKeyDown(), testKeyDown(), testKeySpace())
	if !s.selection.has("team/a") {
		t.Fatal("Space on the new-name row did not tick it")
	}
}

// B2: a late result never touches a freshly opened screen.
func TestGrantLateApplyResultNeverTouchesAFreshScreen(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeySpace())
	updated, applyCmd := m.Update(testKeyEnter())
	m = updated.(Model)
	if applyCmd == nil {
		t.Fatal("no apply command")
	}
	// Force a fresh screen in the slot, as a reopen would after the screen was left.
	fresh := newGrantAccessScreen(screenAdminMenu, "u-2")
	m.adminScreens[slotGrantAccess] = fresh
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminMenu, preselectID: "u-2"})
	m = runCmd(t, updated.(Model), cmd)
	m = grantModelPress(t, m, testKeySpace())
	before := grantScreenOf(t, m)
	if !before.selection.has("library/alpine") {
		t.Fatal("precondition: new selection missing")
	}
	updated, _ = m.Update(applyCmd())
	m = updated.(Model)
	after := grantScreenOf(t, m)
	if !after.selection.has("library/alpine") || len(after.results) != 0 || strings.Contains(after.status, "alice") || after.applying != before.applying {
		t.Fatalf("stale result touched the fresh screen: selection=%v results=%v status=%q", after.selection.ordered(grantTestKnown), after.results, after.status)
	}
}

// B2/B3: Esc during apply stays, says so, and leaves once the result is in.
func TestGrantEscWhileApplyingStaysAndShowsApplying(t *testing.T) {
	t.Parallel()
	env := grantTestEnv(newRecordingGrantClient())
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeySpace())
	next, cmd, _ := s.Update(env, testKeyEnter())
	applying := next.(grantAccessScreen)
	after, escCmd, consumed := applying.Update(env, testKeyEsc())
	if escCmd != nil || !consumed {
		t.Fatal("Esc while applying must be consumed without leaving")
	}
	if got := after.(grantAccessScreen).status; !strings.Contains(got, "Applying 1 grants, please wait") {
		t.Fatalf("status = %q", got)
	}
	done := grantSettle(after, env, cmd).(grantAccessScreen)
	if _, leave, _ := done.Update(env, testKeyEsc()); leave == nil {
		t.Fatal("Esc must leave once the result is shown")
	}
	if !strings.Contains(done.status, "Granted Reader on 1 of 1") {
		t.Fatalf("result status lost: %q", done.status)
	}
}

// (d) no second apply can start while one is in flight.
func TestGrantNoSecondApplyWhileInFlight(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeySpace())
	updated, first := m.Update(testKeyEnter())
	m = updated.(Model)
	updated, second := m.Update(testKeyEnter())
	m = updated.(Model)
	if first == nil || second != nil {
		t.Fatalf("first=%v second=%v, want exactly one apply command", first != nil, second != nil)
	}
	updated, esc := m.Update(testKeyEsc())
	m = updated.(Model)
	if esc != nil || m.screen != screenAdminGrantAccess {
		t.Fatal("Esc left the screen mid-apply")
	}
	// A reopen request while applying keeps the running screen.
	running := grantScreenOf(t, m)
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminMenu, preselectID: "u-2"})
	m = updated.(Model)
	if cmd != nil || !grantScreenOf(t, m).applying || grantScreenOf(t, m).gen != running.gen {
		t.Fatal("a reopen replaced the screen that is applying")
	}
	m = runCmd(t, m, first)
	if len(client.puts) != 1 {
		t.Fatalf("puts = %v, want exactly one write", client.puts)
	}
}

// Defense in depth: messages carry the generation of the screen that made them.
func TestGrantScreenDropsMessagesFromAnotherGeneration(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	env := grantTestEnv(client)
	a := newGrantAccessScreen(screenAdminMenu, "")
	b := newGrantAccessScreen(screenAdminMenu, "")
	if a.gen == b.gen {
		t.Fatal("generations are not unique")
	}
	stale := a.Init(env)()
	next, cmd, _ := b.Update(env, stale)
	got := next.(grantAccessScreen)
	if got.principalsLoaded || cmd != nil {
		t.Fatal("a principals message from another generation was applied")
	}
	loaded := grantSettle(b, env, b.Init(env)).(grantAccessScreen)
	staleGrants := grantAccessGrantsLoadedMsg{gen: a.gen, userID: "u-1", err: errors.New("x")}
	after, _, _ := loaded.Update(env, staleGrants)
	if after.(grantAccessScreen).grants["u-1"].err != "" {
		t.Fatal("a grants message from another generation was applied")
	}
	staleApplied := grantAccessAppliedMsg{gen: a.gen, principalID: "u-1", role: domainauth.RepoRoleAdmin, outcomes: []grantOutcome{{repo: "a/b"}}}
	after, _, _ = loaded.Update(env, staleApplied)
	if after.(grantAccessScreen).status != "" {
		t.Fatal("an applied message from another generation was applied")
	}
	_ = tea.Quit
}

// Advisory: an expired session stops the remaining writes.
func TestGrantExpiredSessionStopsRemainingWrites(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.putErrors["team/backend"] = AdminSessionExpiredError{Reason: "expired"}
	env := grantTestEnv(client)
	s := grantOpenFirst(t, env)
	s = grantPress(t, s, env, testKeyTab(), testKeyCtrl('a'))
	_, cmd, _ := s.Update(env, testKeyEnter())
	msg := cmd().(grantAccessAppliedMsg)
	if len(client.puts) != 2 {
		t.Fatalf("puts = %v, want the calls to stop after the expired one", client.puts)
	}
	if len(msg.outcomes) != 4 {
		t.Fatalf("outcomes = %d, want all four reported", len(msg.outcomes))
	}
	for _, o := range msg.outcomes[2:] {
		if o.err == nil || !strings.Contains(o.err.Error(), "not attempted (session expired)") {
			t.Fatalf("outcome %v not reported as not attempted", o)
		}
	}
	m := grantModel(t, client, 150, 24)
	m.adminScreens[slotGrantAccess] = s // the screen that issued the apply is the mounted one
	updated, _ := m.Update(msg)
	if updated.(Model).screen != screenAdminLogin {
		t.Fatal("no redirect to login")
	}
}

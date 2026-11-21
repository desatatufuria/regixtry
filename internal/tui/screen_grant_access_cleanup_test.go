package tui

import (
	"reflect"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
)

// grantOpenFirst opens the screen without a preselection and chooses the
// first principal (Down reveals the highlight, Space chooses it): the state
// older tests assumed from the start.
func grantOpenFirst(t *testing.T, env screenEnv) grantAccessScreen {
	t.Helper()
	return grantPress(t, grantOpen(t, env, ""), env, testKeyDown(), testKeySpace())
}

func grantExpiredErr() error {
	return AdminSessionExpiredError{Reason: AdminSessionExpiredReasonExpired}
}

// Item 1: grants applied before a session expiry are reported on the login screen.
func TestGrantExpiryDuringApplyReportsWhatWasApplied(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.putErrors["team/backend"] = grantExpiredErr()
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeySpace(), testKeyDown(), testKeySpace(), testKeyDown(), testKeySpace())
	m = grantModelPress(t, m, testKeyEnter())

	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login screen", m.screen)
	}
	want := []grantPut{
		{"u-1", "library/alpine", domainauth.RepoRoleReader},
		{"u-1", "team/backend", domainauth.RepoRoleReader},
	}
	if !reflect.DeepEqual(client.puts, want) {
		t.Fatalf("puts = %#v, want %#v", client.puts, want)
	}
	view := m.View().Content
	for _, part := range []string{"Session expired", "Applied before it expired: library/alpine", "Not applied: 2", "Sign in and apply the rest"} {
		if !strings.Contains(view, part) {
			t.Errorf("login view lacks %q:\n%s", part, view)
		}
	}
	if strings.Contains(view, "bearer-token") {
		t.Error("login view leaks the bearer token")
	}

	// Esc clears it for good.
	esc := grantModelPress(t, m, testKeyEsc())
	if strings.Contains(esc.status, "Applied before") || strings.Contains(esc.adminSession.ExpiredReason, "Applied before") {
		t.Fatalf("Esc kept the note: status=%q reason=%q", esc.status, esc.adminSession.ExpiredReason)
	}
	// A later successful login clears it.
	updated, _ := m.Update(adminLoginCompletedMsg{session: AdminSession{Username: "operator", BearerToken: "t2"}})
	if strings.Contains(updated.(Model).adminSession.ExpiredReason, "Applied before") {
		t.Fatal("login success kept the note")
	}
}

func TestGrantExpiryWithNothingAppliedShowsOnlyTheExpiredMessage(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.putErrors["library/alpine"] = grantExpiredErr()
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeySpace(), testKeyEnter())
	if m.screen != screenAdminLogin || m.status != AdminSessionExpiredReasonExpired {
		t.Fatalf("screen=%q status=%q, want login with the plain expiry message", m.screen, m.status)
	}
}

func TestGrantExpiryNoteListsAFewNamesThenMore(t *testing.T) {
	t.Parallel()
	got := grantExpiryNote([]string{"a/1", "a/2", "a/3", "a/4", "a/5"}, 2)
	for _, part := range []string{"a/1, a/2, a/3", "+2 more", "Not applied: 2"} {
		if !strings.Contains(got, part) {
			t.Errorf("note %q lacks %q", got, part)
		}
	}
	if strings.Contains(got, "a/4") {
		t.Errorf("note %q lists more than three names", got)
	}
}

func TestGrantFullySuccessfulApplyIsUnchanged(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeySpace(), testKeyEnter())
	if m.screen != screenAdminGrantAccess || !strings.Contains(grantScreenOf(t, m).status, "Granted Reader on 1 of 1") {
		t.Fatalf("screen=%q status=%q", m.screen, grantScreenOf(t, m).status)
	}
}

// Item 2: choosing a principal is required.
func TestGrantWithoutPreselectionNobodyIsChosenAndEnterDoesNothing(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantOpenModel(t, client, "")
	s := grantScreenOf(t, m)
	if s.focus != grantPaneWho || s.chosenID != "" {
		t.Fatalf("focus=%v chosen=%q, want Who pane and nobody chosen", s.focus, s.chosenID)
	}
	m = grantModelPress(t, m, testKeyEnter())
	if got := grantScreenOf(t, m).status; got != "Pick who to grant access to first" {
		t.Fatalf("status = %q", got)
	}
	// Even with a repository ticked, Enter from another pane applies nothing.
	m = grantModelPress(t, m, testKeyTab(), testKeySpace(), testKeyEnter())
	if len(client.puts) != 0 {
		t.Fatalf("puts = %#v, want no calls", client.puts)
	}
	view := grantScreenOf(t, m).View(newAdminTheme(), m.screenEnv()).Body
	for _, name := range []string{"alice becomes", "bob becomes", "Nobody becomes"} {
		if strings.Contains(view, name) {
			t.Fatalf("review box names an unchosen principal (%q):\n%s", name, view)
		}
	}
}

func TestGrantChoosingAPrincipalWithSpaceOrEnterThenApplying(t *testing.T) {
	t.Parallel()
	for name, choose := range map[string]keyMsg{"space": testKeySpace(), "enter": testKeyEnter()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := newRecordingGrantClient()
			m := grantOpenModel(t, client, "")
			m = grantModelPress(t, m, testKeyDown(), testKeyDown(), choose)
			if len(client.puts) != 0 {
				t.Fatalf("choosing applied something: %#v", client.puts)
			}
			if got := grantScreenOf(t, m).chosenID; got != "u-2" {
				t.Fatalf("chosen = %q, want u-2", got)
			}
			m = grantModelPress(t, m, testKeyTab(), testKeySpace(), testKeyEnter())
			want := []grantPut{{"u-2", "library/alpine", domainauth.RepoRoleReader}}
			if !reflect.DeepEqual(client.puts, want) {
				t.Fatalf("puts = %#v, want %#v", client.puts, want)
			}
		})
	}
}

func TestGrantPreselectedPrincipalIsChosenAsBefore(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantOpenModel(t, client, "u-2")
	if got := grantScreenOf(t, m).chosenID; got != "u-2" {
		t.Fatalf("chosen = %q, want u-2", got)
	}
	m = grantModelPress(t, m, testKeySpace(), testKeyEnter())
	want := []grantPut{{"u-2", "library/alpine", domainauth.RepoRoleReader}}
	if !reflect.DeepEqual(client.puts, want) {
		t.Fatalf("puts = %#v, want %#v", client.puts, want)
	}
}

// Item 3: a stale (foreign generation) expiry message is dropped first.
func TestGrantStaleExpiryMessageIsDroppedAndCurrentOneLogsOut(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	m := grantOpenModel(t, client, "u-1")
	gen := grantScreenOf(t, m).gen
	expired := []grantOutcome{{repo: "team/backend", err: grantExpiredErr()}}

	updated, _ := m.Update(grantAccessAppliedMsg{gen: gen + 1000, principalID: "u-1", outcomes: expired})
	stale := updated.(Model)
	if stale.screen != screenAdminGrantAccess || stale.adminAuth != adminAuthStateAuthenticated {
		t.Fatalf("stale expiry changed state: screen=%q auth=%q", stale.screen, stale.adminAuth)
	}
	updated, _ = m.Update(grantAccessGrantsLoadedMsg{gen: gen + 1000, userID: "u-1", err: grantExpiredErr()})
	if got := updated.(Model); got.screen != screenAdminGrantAccess {
		t.Fatalf("stale grants-loaded expiry logged out: %q", got.screen)
	}

	updated, _ = m.Update(grantAccessAppliedMsg{gen: gen, principalID: "u-1", outcomes: expired})
	if got := updated.(Model); got.screen != screenAdminLogin {
		t.Fatalf("current expiry screen = %q, want login", got.screen)
	}
}

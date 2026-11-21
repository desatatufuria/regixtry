package tui

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

var updateGrantCleanupGoldens = flag.Bool("update-grant-cleanup", false, "rewrite the grant cleanup golden files")

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

// ---- B1: the expiry note fits the login screen ----

func grantWorstCaseExpiredModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := grantOpenModel(t, newRecordingGrantClient(), "u-1")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(Model)
	var outcomes []grantOutcome
	for i := 0; i < 5; i++ {
		outcomes = append(outcomes, grantOutcome{repo: fmt.Sprintf("organization-%d/%s", i, strings.Repeat("x", 45))})
	}
	outcomes = append(outcomes, grantOutcome{repo: "team/last", err: grantExpiredErr()})
	updated, _ = m.Update(grantAccessAppliedMsg{gen: grantScreenOf(t, m).gen, principalID: "u-1", outcomes: outcomes})
	return updated.(Model)
}

func TestGrantExpiryNoteFitsTheLoginScreenAtEverySize(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()
			m := grantWorstCaseExpiredModel(t, size.w, size.h)
			if m.screen != screenAdminLogin {
				t.Fatalf("screen = %q", m.screen)
			}
			view := m.View().Content
			assertGrantNoOverflow(t, "login", view, size.w, size.h)
			plain := ansi.Strip(view)
			for _, part := range []string{"Username", "Password", "Esc: back", "+2 more", "Not applied: 1", "Sign in and apply the rest."} {
				if !strings.Contains(strings.Join(strings.Fields(plain), " "), part) {
					t.Errorf("login view lacks %q:\n%s", part, plain)
				}
			}
			if strings.Contains(plain, strings.Repeat("x", 45)) {
				t.Error("a repository name was not truncated")
			}
		})
	}
}

func TestGrantExpiryNoteWorstCaseGolden(t *testing.T) {
	t.Parallel()
	got := ansi.Strip(grantWorstCaseExpiredModel(t, 150, 24).View().Content)
	path := filepath.Join("testdata", "grant_cleanup", "login_expiry_note_150x24.golden")
	if *updateGrantCleanupGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("login view differs from %s\n--- got\n%s", path, got)
	}
}

// ---- F1: an expired apply never leaves a stuck screen behind ----

func TestGrantExpiredApplyLeavesAFreshScreenAfterSigningInAgain(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.putErrors["team/backend"] = grantExpiredErr()
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeySpace(), testKeyDown(), testKeySpace(), testKeyEnter())
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q", m.screen)
	}
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(openGrantAccessMsg{returnTo: screenAdminOverview})
	m = runCmd(t, updated.(Model), cmd)
	s := grantScreenOf(t, m)
	if s.applying || s.chosenID != "" || s.status != "" || len(s.selection.names) != 0 {
		t.Fatalf("reopened screen is not fresh: %+v", s)
	}
	if strings.Contains(grantRender(m), "Applied before") {
		t.Fatal("the earlier expiry note reappeared")
	}
	m = grantModelPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc left screen = %q, want the menu", m.screen)
	}
}

// ---- A1: annotations describe the chosen principal only ----

func TestGrantAnnotationsAndAlreadyHasFollowTheChosenPrincipal(t *testing.T) {
	t.Parallel()
	client := newRecordingGrantClient()
	client.grantsFor["u-1"] = []ports.AdminRepoGrant{grantTestGrant("u-1", "team/demo", domainauth.RepoRoleReader)}
	client.grantsFor["u-2"] = []ports.AdminRepoGrant{grantTestGrant("u-2", "ops/console", domainauth.RepoRoleAdmin)}
	m := grantOpenModel(t, client, "u-1")
	m = grantModelPress(t, m, testKeyDown()) // highlight bob, still chosen alice
	view := grantRender(m)
	if !strings.Contains(view, "Already has (alice)") || !strings.Contains(view, "alice is Reader") {
		t.Fatalf("chosen principal not described:\n%s", view)
	}
	if strings.Contains(view, "bob is") || strings.Contains(view, "Already has (bob)") {
		t.Fatalf("view describes the merely highlighted principal:\n%s", view)
	}
	// Nobody chosen: both are hidden.
	n := grantOpenModel(t, newRecordingGrantClient(), "")
	n = grantModelPress(t, n, testKeyDown())
	nview := grantRender(n)
	if strings.Contains(nview, "Already has (") || strings.Contains(nview, " is Reader") || strings.Contains(nview, " is Writer") {
		t.Fatalf("nothing chosen but the view describes someone:\n%s", nview)
	}
}

// ---- seed fuzz: grants only ever go to the chosen principal ----

func TestGrantFuzzCallsOnlyGoToTheChosenPrincipal(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20260721))
	alphabet := []keyMsg{
		testKeyDown(), testKeyUp(), testKeySpace(), testKeyEnter(), testKeyTab(), testKeyShiftTab(),
		testKeyBackspace(), testKeyCtrl('a'), testKeyRunes("a"), testKeyRunes("b"), testKeyRunes("1"), testKeyRunes("3"),
	}
	preselects := []string{"", "", "u-2", "r-1"}
	for seq := 0; seq < 300; seq++ {
		client := newRecordingGrantClient()
		env := grantTestEnv(client)
		s := grantOpen(t, env, preselects[rng.Intn(len(preselects))])
		for step := 0; step < 25; step++ {
			before := len(client.puts)
			s = grantPress(t, s, env, alphabet[rng.Intn(len(alphabet))])
			for _, put := range client.puts[before:] {
				if s.chosenID == "" || put.UserID != s.chosenID {
					t.Fatalf("seq %d step %d: call for %q but chosen is %q", seq, step, put.UserID, s.chosenID)
				}
			}
		}
	}
}

package tui

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// Repository access has its own golden flag so regenerating its goldens can
// never touch another screen's.
var updateRepoAccessGoldens = flag.Bool("update-repo-access", false, "rewrite the repository access golden files")

var repoAccessSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

func repoAccessDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 8, 0, 0, 0, time.UTC)
}

func repoAccessEntry(repo, id, user string, robot bool, role domainauth.RepoRole, at time.Time) ports.AdminGrantEntry {
	return ports.AdminGrantEntry{Repository: repo, UserID: id, Username: user, IsRobot: robot, Role: role, CreatedAt: at, UpdatedAt: at}
}

// repoAccessClient serves the board's sample, in the endpoint's order
// (repository, then username): team/api has four grants, team/docs one and
// team/infra one, and team/infra is not in the catalog (never pushed).
func repoAccessClient() *fakeAdminClient {
	return &fakeAdminClient{
		allGrants: []ports.AdminGrantEntry{
			repoAccessEntry("team/api", "u-alice", "alice", false, domainauth.RepoRoleWriter, repoAccessDay(time.October, 14)),
			repoAccessEntry("team/api", "u-bob", "bob", false, domainauth.RepoRoleReader, repoAccessDay(time.November, 2)),
			repoAccessEntry("team/api", "u-carol", "carol", false, domainauth.RepoRoleWriter, repoAccessDay(time.November, 9)),
			repoAccessEntry("team/api", "r-ci", "ci-publisher", true, domainauth.RepoRoleWriter, repoAccessDay(time.January, 12)),
			repoAccessEntry("team/docs", "u-alice", "alice", false, domainauth.RepoRoleReader, repoAccessDay(time.March, 3)),
			repoAccessEntry("team/infra", "u-old", "old-deploy", false, domainauth.RepoRoleAdmin, repoAccessDay(time.February, 5)),
		},
	}
}

// repoAccessRun runs cmd and everything it triggers, flattening batches the
// way the Bubble Tea runtime does.
func repoAccessRun(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = repoAccessRun(t, m, sub)
		}
		return m
	}
	updated, next := m.Update(msg)
	return repoAccessRun(t, updated.(Model), next)
}

func repoAccessModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := newAdminReadyModel(t, client)
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenAdminRepositoryAccess})
	return repoAccessRun(t, updated.(Model), cmd)
}

func repoAccessPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = repoAccessRun(t, updated.(Model), cmd)
	}
	return m
}

func repoAccessType(text string) []keyMsg {
	keys := make([]keyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

func repoAccessScreenOf(t *testing.T, m Model) repoAccessScreen {
	t.Helper()
	s, ok := m.adminScreens[slotRepositoryAccess].(repoAccessScreen)
	if !ok {
		t.Fatalf("slot holds %T, want repoAccessScreen", m.adminScreens[slotRepositoryAccess])
	}
	return s
}

func repoAccessRender(m Model) string { return ansi.Strip(m.View().Content) }

func repoAccessKeyPairs(s repoAccessScreen) []string {
	var out []string
	for _, r := range s.visibleRows() {
		out = append(out, r.repo+"|"+r.username)
	}
	return out
}

func TestRepoAccessListsEveryGrantInOneCall(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, 150, 24)
	if m.screen != screenAdminRepositoryAccess {
		t.Fatalf("screen = %q", m.screen)
	}
	got := repoAccessKeyPairs(repoAccessScreenOf(t, m))
	want := []string{"team/api|alice", "team/api|bob", "team/api|carol", "team/api|ci-publisher", "team/docs|alice", "team/infra|old-deploy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v (team/infra is not in the catalog and must be listed)", got, want)
	}
	if strings.Join(client.allGrantsCursors, ",") != "" || len(client.allGrantsCursors) != 1 {
		t.Fatalf("grant reads = %q, want exactly one, for the first page", client.allGrantsCursors)
	}
	if client.listRepoGrantsCalls != 0 || client.listUsersCalls != 0 || client.listRobotsCalls != 0 {
		t.Fatalf("per-repository or principal reads: grants=%d users=%d robots=%d, want none", client.listRepoGrantsCalls, client.listUsersCalls, client.listRobotsCalls)
	}
	out := repoAccessRender(m)
	for _, text := range []string{"Admin › Access › Repository access", "6 grants on 3 repositories", "REPOSITORY", "PRINCIPAL", "ROLE", "SINCE",
		"team/api", "14 Oct", "WHO HAS WHAT", "4 principals have access", "ACTIONS ON SELECTED GRANT", "Open principal", "Revoke alice on team/api",
		"No Admin on this repository.", "server admins can change access"} {
		if !strings.Contains(out, text) {
			t.Errorf("view lacks %q\n%s", text, out)
		}
	}
}

func TestRepoAccessSidebarItemOpensItAndEscReturnsToTheOverview(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc went to %q, want the Overview", m.screen)
	}
}

// repoAccessBulk is total grants of alice on org/repo000.. in endpoint order.
func repoAccessBulk(total int) *fakeAdminClient {
	client := &fakeAdminClient{}
	for i := range total {
		client.allGrants = append(client.allGrants, repoAccessEntry(fmt.Sprintf("org/repo%03d", i), "u-alice", "alice", false, domainauth.RepoRoleReader, repoAccessDay(time.May, 1)))
	}
	return client
}

func TestRepoAccessReadsTheNextPageOnlyWhenTheCursorNearsTheEnd(t *testing.T) {
	t.Parallel()
	client := repoAccessBulk(450)
	m := repoAccessModelAt(t, client, 150, 24)
	if len(client.allGrantsCursors) != 1 || len(repoAccessScreenOf(t, m).rows) != 200 {
		t.Fatalf("on open: cursors=%q rows=%d, want one read of 200 rows", client.allGrantsCursors, len(repoAccessScreenOf(t, m).rows))
	}
	if out := repoAccessRender(m); !strings.Contains(out, "200 grants on 200 repositories (not all loaded)") {
		t.Fatalf("the title does not say more remains:\n%s", out)
	}
	for range 100 {
		m = repoAccessPress(t, m, testKeyDown())
	}
	if len(client.allGrantsCursors) != 1 {
		t.Fatalf("reads = %q while the cursor was still far from the end", client.allGrantsCursors)
	}
	for range 100 {
		m = repoAccessPress(t, m, testKeyDown())
	}
	if got := strings.Join(client.allGrantsCursors, ","); got != ",c200" {
		t.Fatalf("reads = %q, want the first page then the cursor the server returned", got)
	}
	if got := len(repoAccessScreenOf(t, m).rows); got != 400 {
		t.Fatalf("rows = %d, want 400", got)
	}
}

func TestRepoAccessFollowsNextUntilEmptyAndNeverReadsMoreThanThePages(t *testing.T) {
	t.Parallel()
	client := repoAccessBulk(450)
	m := repoAccessModelAt(t, client, 150, 24)
	// A filter that matches nothing walks every page.
	m = repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("zzz")...)...)
	if got := strings.Join(client.allGrantsCursors, ","); got != ",c200,c400" {
		t.Fatalf("reads = %q, want ,c200,c400 (ceil(450/200) = 3 calls)", got)
	}
	s := repoAccessScreenOf(t, m)
	if !s.done || len(s.rows) != 450 || s.loading {
		t.Fatalf("done=%v rows=%d loading=%v, want a finished walk", s.done, len(s.rows), s.loading)
	}
	m = repoAccessPress(t, m, repoAccessType("z")...)
	if len(client.allGrantsCursors) != 3 {
		t.Fatalf("a finished walk read again: %q", client.allGrantsCursors)
	}
}

func TestRepoAccessFilterWithEnoughMatchesReadsNoMorePages(t *testing.T) {
	t.Parallel()
	client := repoAccessBulk(450)
	m := repoAccessModelAt(t, client, 150, 24)
	m = repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("org/repo0")...)...)
	if len(client.allGrantsCursors) != 1 {
		t.Fatalf("reads = %q, the first page already holds enough matches", client.allGrantsCursors)
	}
}

func TestRepoAccessFilterWithFewMatchesReadsPagesOnlyAsNeeded(t *testing.T) {
	t.Parallel()
	client := repoAccessBulk(1000)
	m := repoAccessModelAt(t, client, 150, 24)
	// repo3xx matches 100 rows: page 2 (200..399) holds them all, and 100 is
	// more than a screen, so reading stops there.
	m = repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("repo3")...)...)
	if got := strings.Join(client.allGrantsCursors, ","); got != ",c200" {
		t.Fatalf("reads = %q, want only the pages needed (,c200)", got)
	}
}

func TestRepoAccessFilterMatchesRepositoryOrPrincipal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"docs", []string{"team/docs|alice"}},
		{"CAROL", []string{"team/api|carol"}},
		{"alice", []string{"team/api|alice", "team/docs|alice"}},
		{"nothing", nil},
	} {
		m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
		m = repoAccessPress(t, m, testKeyRunes("/"))
		m = repoAccessPress(t, m, repoAccessType(tc.filter)...)
		if got := repoAccessKeyPairs(repoAccessScreenOf(t, m)); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("filter %q: rows = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

func TestRepoAccessFilterTypesJKQAndDoesNotQuit(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	client.allGrants = append(client.allGrants, repoAccessEntry("team/web", "u-jkq", "jkq-bot", false, domainauth.RepoRoleReader, time.Time{}))
	m := repoAccessModelAt(t, client, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("/"))
	for _, k := range repoAccessType("jkq") {
		updated, cmd := m.Update(k)
		m = updated.(Model)
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("typing %q in the filter quit", keyText(k))
			}
			m = repoAccessRun(t, m, cmd)
		}
	}
	if s := repoAccessScreenOf(t, m); s.filter != "jkq" || len(s.visibleRows()) != 1 {
		t.Fatalf("filter = %q rows = %d, want jkq matching jkq-bot", s.filter, len(s.visibleRows()))
	}
	m = repoAccessPress(t, m, testKeyBackspace())
	if got := repoAccessScreenOf(t, m).filter; got != "jk" {
		t.Fatalf("Backspace left %q", got)
	}
}

func TestRepoAccessFilterAcceptsPastedText(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("/"))
	m = repoAccessPress(t, m, keyMsg{Text: "team/docs"})
	if s := repoAccessScreenOf(t, m); s.filter != "team/docs" || len(s.visibleRows()) != 1 {
		t.Fatalf("pasted filter = %q rows = %d", s.filter, len(s.visibleRows()))
	}
}

func TestRepoAccessEscEndsTheSearchThenGoesBack(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("/"), testKeyRunes("a"), testKeyEsc())
	if s := repoAccessScreenOf(t, m); s.filtering || s.filter != "a" || m.screen != screenAdminRepositoryAccess {
		t.Fatalf("first Esc: filtering=%v filter=%q screen=%q", s.filtering, s.filter, m.screen)
	}
	m = repoAccessPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("second Esc went to %q", m.screen)
	}
}

func TestRepoAccessQQuitsOutsideTheFilterAndIsTypedInsideIt(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	if _, cmd := m.Update(testKeyRunes("q")); !didQuit(cmd) {
		t.Fatal("q outside the filter did not quit")
	}
	m = repoAccessPress(t, m, testKeyRunes("/"))
	updated, cmd := m.Update(testKeyRunes("q"))
	if didQuit(cmd) {
		t.Fatal("q quit while the filter had focus")
	}
	if got := repoAccessScreenOf(t, updated.(Model)).filter; got != "q" {
		t.Fatalf("filter = %q, want q typed", got)
	}
	m = repoAccessPress(t, repoAccessModelAt(t, repoAccessClient(), 150, 24), testKeyRunes("x"))
	if _, cmd := m.Update(testKeyRunes("q")); didQuit(cmd) {
		t.Fatal("q quit while the revoke prompt was open")
	}
}

// repoAccessCounting records the deletes so a fuzz can compare them to the
// dialog.
type repoAccessCounting struct {
	*fakeAdminClient
	dels [][2]string
}

func (c *repoAccessCounting) DeleteRepositoryGrant(_ context.Context, _ AdminSession, repo, user string) error {
	c.dels = append(c.dels, [2]string{repo, user})
	return nil
}

func repoAccessFlatten(cmd tea.Cmd, out *[]tea.Msg) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			repoAccessFlatten(sub, out)
		}
		return
	}
	if msg != nil {
		*out = append(*out, msg)
	}
}

func TestRepoAccessStopsReadingOnceLeft(t *testing.T) {
	t.Parallel()
	client := repoAccessBulk(450)
	m := newAdminReadyModel(t, client)
	m = charResize(t, m, 150, 24)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "b", ExpiresAt: charFixedTime.Add(time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated

	var pending []tea.Msg
	step := func(msg tea.Msg) {
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		repoAccessFlatten(cmd, &pending)
	}
	deliver := func() {
		msg := pending[0]
		pending = pending[1:]
		step(msg)
	}
	step(navigateMsg{To: screenAdminRepositoryAccess})
	deliver()
	// A filter with no match asks for the second page, which stays in flight.
	step(testKeyRunes("/"))
	step(testKeyRunes("z"))
	if len(pending) != 1 || len(client.allGrantsCursors) != 2 {
		t.Fatalf("pending=%d reads=%q, want the second page in flight", len(pending), client.allGrantsCursors)
	}
	step(testKeyEsc())
	step(testKeyEsc())
	for i, msg := range pending {
		if nav, ok := msg.(navigateMsg); ok {
			pending = append(pending[:i], pending[i+1:]...)
			step(nav)
			break
		}
	}
	if m.screen != screenAdminOverview || m.adminScreens[slotRepositoryAccess] != nil {
		t.Fatalf("the screen did not leave and unmount (screen=%q)", m.screen)
	}
	atLeave := len(client.allGrantsCursors)
	for len(pending) > 0 {
		deliver()
	}
	for range 3 {
		step(testKeyDown())
	}
	if len(client.allGrantsCursors) != atLeave || len(pending) != 0 {
		t.Fatalf("reads continued behind another screen (%d -> %d, %d pending)", atLeave, len(client.allGrantsCursors), len(pending))
	}
	// Coming back mounts a fresh screen that reads the first page again.
	step(navigateMsg{To: screenAdminRepositoryAccess})
	if s := repoAccessScreenOf(t, m); len(s.rows) != 0 || s.next != "" || !s.loading {
		t.Fatalf("re-entry kept state: rows=%d next=%q loading=%v", len(s.rows), s.next, s.loading)
	}
}

func TestRepoAccessLoadingErrorPartialFailureAndEmptyStates(t *testing.T) {
	t.Parallel()
	// Loading: nothing delivered yet.
	s := newRepoAccessScreen()
	s.rows = []repoAccessRow{{repo: "a/b", username: "alice"}, {repo: "a/c", username: "bob"}}
	out := ansi.Strip(s.View(newAdminTheme(), screenEnv{Layout: consoleLayout{Width: 150, Height: 24}}).Body)
	if !strings.Contains(out, "Reading grants… 2 loaded") {
		t.Fatalf("loading line missing:\n%s", out)
	}
	for _, bad := range []string{"No grants yet", "0 grants on 0 repositories"} {
		if strings.Contains(out, bad) {
			t.Fatalf("loading state shows %q:\n%s", bad, out)
		}
	}

	// The first read fails: the endpoint's text and r to retry, never empty.
	client := repoAccessClient()
	client.allGrantsErrs = map[string]error{"": errors.New("boom")}
	m := repoAccessModelAt(t, client, 150, 24)
	out = repoAccessRender(m)
	for _, text := range []string{"Could not read grants: boom. Press r to retry.", "Grants unavailable", "r retry"} {
		if !strings.Contains(out, text) {
			t.Fatalf("error state lacks %q:\n%s", text, out)
		}
	}
	if strings.Contains(out, "No grants yet") {
		t.Fatalf("error state shows the empty text:\n%s", out)
	}
	client.allGrantsErrs = nil
	m = repoAccessPress(t, m, testKeyRunes("r"))
	if got := repoAccessKeyPairs(repoAccessScreenOf(t, m)); len(got) != 6 || strings.Join(client.allGrantsCursors, ",") != "," {
		t.Fatalf("after r: rows=%v reads=%q, want 6 rows from a second read of the first page", got, client.allGrantsCursors)
	}
	if out = repoAccessRender(m); strings.Contains(out, "Could not read grants") || strings.Contains(out, "r retry") {
		t.Fatalf("the error stayed after a good retry:\n%s", out)
	}

	// A later page fails: what was loaded stays, r retries from that cursor.
	client = repoAccessBulk(450)
	client.allGrantsErrs = map[string]error{"c200": errors.New("page boom")}
	m = repoAccessModelAt(t, client, 150, 24)
	m = repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("zzz")...)...)
	m = repoAccessPress(t, m, testKeyEnter())
	if s := repoAccessScreenOf(t, m); len(s.rows) != 200 || s.loadErr != "page boom" || s.next != "c200" {
		t.Fatalf("partial failure: rows=%d err=%q next=%q", len(s.rows), s.loadErr, s.next)
	}
	if out = repoAccessRender(m); !strings.Contains(out, "Could not read grants: page boom. Press r to retry.") {
		t.Fatalf("partial failure not shown:\n%s", out)
	}
	if got := len(client.allGrantsCursors); got != 2 {
		t.Fatalf("a failed read was retried on its own (%d reads)", got)
	}
	client.allGrantsErrs = nil
	m = repoAccessPress(t, m, testKeyRunes("r"))
	if got := strings.Join(client.allGrantsCursors, ","); got != ",c200,c200,c400" {
		t.Fatalf("reads = %q, want the retry to resume from c200 and go on to c400", got)
	}
	if s := repoAccessScreenOf(t, m); len(s.rows) != 450 || s.loadErr != "" || !s.done {
		t.Fatalf("after retry: rows=%d err=%q done=%v", len(s.rows), s.loadErr, s.done)
	}

	// Completed with zero grants: only now the empty text.
	out = repoAccessRender(repoAccessModelAt(t, &fakeAdminClient{}, 150, 24))
	if !strings.Contains(out, "No grants yet. Press g to grant access.") {
		t.Fatalf("empty state missing:\n%s", out)
	}
}

func TestRepoAccessRTypesIntoTheFilterAndDoesNothingWithoutAnError(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("r"))
	if len(client.allGrantsCursors) != 1 {
		t.Fatalf("r without an error read again: %q", client.allGrantsCursors)
	}
	m = repoAccessPress(t, m, testKeyRunes("/"), testKeyRunes("r"))
	if s := repoAccessScreenOf(t, m); s.filter != "r" {
		t.Fatalf("filter = %q, want r typed", s.filter)
	}
}

// TestRepoAccessRevokeTargetFuzz drives seeded random key and message
// sequences and checks, for every delete the client sees, that it equals the
// target the dialog showed at confirmation, that no delete happens without a
// confirm key, and that the target never changes while the dialog is open,
// including when a page of grants arrives mid-dialog.
func TestRepoAccessRevokeTargetFuzz(t *testing.T) {
	t.Parallel()
	names := []string{"alice", "bob", "carol", "Dave", "ci", "kjq", "jack", "old-deploy", "queue", "yak"}
	letters := "jkqxyYagnN/ e"
	r := rand.New(rand.NewSource(20260721))
	for seq := range 300 {
		nrepo := 1 + r.Intn(150)
		fake := &fakeAdminClient{}
		for i := range nrepo {
			repo := fmt.Sprintf("org/repo%03d", i)
			used := map[string]bool{}
			for range r.Intn(4) {
				name := names[r.Intn(len(names))]
				if !used[name] {
					used[name] = true
					fake.allGrants = append(fake.allGrants, repoAccessEntry(repo, "id-"+name, name, false, domainauth.RepoRoleWriter, time.Time{}))
				}
			}
		}
		client := &repoAccessCounting{fakeAdminClient: fake}
		m := newAdminReadyModel(t, client)
		m = charResize(t, m, 150, 24)
		m.adminSession = AdminSession{Username: "operator", BearerToken: "b", ExpiresAt: charFixedTime.Add(time.Hour)}
		m.adminAuth = adminAuthStateAuthenticated
		var pending []tea.Msg
		step := func(msg tea.Msg) {
			updated, cmd := m.Update(msg)
			m = updated.(Model)
			repoAccessFlatten(cmd, &pending)
		}
		step(navigateMsg{To: screenAdminRepositoryAccess})
		openTarget := ""
		for i := range 24 {
			for k := r.Intn(3); k > 0 && len(pending) > 0; k-- {
				j := r.Intn(len(pending))
				msg := pending[j]
				pending = append(pending[:j], pending[j+1:]...)
				before := len(client.dels)
				step(msg)
				if len(client.dels) != before {
					t.Fatalf("seq %d step %d: a delete came from a message, not a key", seq, i)
				}
			}
			if m.screen != screenAdminRepositoryAccess {
				break
			}
			s := repoAccessScreenOf(t, m)
			if s.confirm.Active() && openTarget != "" && s.confirm.target != openTarget {
				t.Fatalf("seq %d step %d: dialog target changed %q -> %q", seq, i, openTarget, s.confirm.target)
			}
			var key keyMsg
			switch c := r.Intn(8); c {
			case 0:
				key = testKeyDown()
			case 1:
				key = testKeyUp()
			case 2:
				key = testKeyEnter()
			case 3:
				key = testKeyBackspace()
			default:
				key = testKeyRunes(string(letters[r.Intn(len(letters))]))
			}
			wasActive, wasInFlight, target := s.confirm.Active(), s.confirm.inFlight, s.confirm.target
			confirms := wasActive && !wasInFlight && (isEnterKey(key) || isPlainYKey(key))
			before := len(client.dels)
			step(key)
			deleted := client.dels[before:]
			switch {
			case len(deleted) > 1, len(deleted) == 1 && !confirms:
				t.Fatalf("seq %d step %d: %d deletes on key %q (confirming=%v)", seq, i, len(deleted), key.Text, confirms)
			case len(deleted) == 1 && deleted[0][1]+" on "+deleted[0][0] != target:
				t.Fatalf("seq %d step %d: deleted %v, dialog said %q", seq, i, deleted[0], target)
			case confirms && len(deleted) == 0:
				t.Fatalf("seq %d step %d: a confirm key sent nothing", seq, i)
			}
			if after := repoAccessScreenOf(t, m); after.confirm.Active() {
				if !wasActive {
					openTarget = after.confirm.target
				}
			} else {
				openTarget = ""
			}
		}
	}
}

func TestRepoAccessJAndKDoNotMove(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("j"), testKeyRunes("j"))
	if got := repoAccessScreenOf(t, m).cursor; got != 0 {
		t.Fatalf("j moved the cursor to %d", got)
	}
	m = repoAccessPress(t, m, testKeyDown(), testKeyDown())
	if got := repoAccessScreenOf(t, m).cursor; got != 2 {
		t.Fatalf("Down twice -> %d", got)
	}
	m = repoAccessPress(t, m, testKeyUp(), testKeyUp(), testKeyUp())
	if got := repoAccessScreenOf(t, m).cursor; got != 0 {
		t.Fatalf("Up past the top -> %d", got)
	}
}

func TestRepoAccessPanelFollowsTheSelectedRepository(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown())
	out := repoAccessRender(m)
	for _, text := range []string{"1 principal has access", "Revoke old-deploy on team/infra"} {
		if !strings.Contains(out, text) {
			t.Errorf("view lacks %q\n%s", text, out)
		}
	}
	if strings.Contains(out, "No Admin on this repository") {
		t.Errorf("a repository with an Admin shows the no-Admin warning")
	}
}

func TestRepoAccessRevokeAsksThenDeletesOnce(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("x"))
	if !repoAccessScreenOf(t, m).confirm.Active() {
		t.Fatal("x did not open the confirm prompt")
	}
	if client.deleteRepoGrantCalls != 0 {
		t.Fatal("x revoked without confirming")
	}
	if out := repoAccessRender(m); !strings.Contains(out, "Revoke alice's Writer access to team/api?") {
		t.Fatalf("prompt text missing:\n%s", out)
	}
	m = repoAccessPress(t, m, testKeyEsc())
	if repoAccessScreenOf(t, m).confirm.Active() || client.deleteRepoGrantCalls != 0 || m.screen != screenAdminRepositoryAccess {
		t.Fatalf("Esc must only close the prompt (calls=%d screen=%q)", client.deleteRepoGrantCalls, m.screen)
	}

	m = repoAccessPress(t, m, testKeyRunes("x"))
	updated, cmd := m.Update(testKeyEnter())
	m = updated.(Model)
	// A second Enter while the delete is in flight sends nothing.
	if _, again := m.Update(testKeyEnter()); again != nil {
		t.Fatal("Enter while revoking issued another command")
	}
	m = repoAccessRun(t, m, cmd)
	if client.deleteRepoGrantCalls != 1 || client.lastDeleteRepoGrantRepo != "team/api" || client.lastDeleteRepoGrantUsername != "alice" {
		t.Fatalf("delete calls=%d repo=%q user=%q", client.deleteRepoGrantCalls, client.lastDeleteRepoGrantRepo, client.lastDeleteRepoGrantUsername)
	}
	s := repoAccessScreenOf(t, m)
	if s.confirm.Active() || s.revoking {
		t.Fatal("the prompt stayed open after the result")
	}
	for _, pair := range repoAccessKeyPairs(s) {
		if pair == "team/api|alice" {
			t.Fatal("the revoked grant is still listed")
		}
	}
	if out := repoAccessRender(m); !strings.Contains(out, "Revoked alice on team/api.") {
		t.Fatalf("no confirmation line:\n%s", out)
	}
}

func TestRepoAccessRevokeFailureKeepsTheGrantAndSaysWhy(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	client.deleteRepoGrantErr = errors.New("forbidden")
	m := repoAccessModelAt(t, client, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
	s := repoAccessScreenOf(t, m)
	if len(s.rows) != 6 || s.confirm.Active() || s.revoking {
		t.Fatalf("rows=%d confirm=%v revoking=%v", len(s.rows), s.confirm.Active(), s.revoking)
	}
	if out := repoAccessRender(m); !strings.Contains(out, "Could not revoke alice on team/api: forbidden") {
		t.Fatalf("error not shown:\n%s", out)
	}
	// The operator can try again.
	client.deleteRepoGrantErr = nil
	m = repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
	if client.deleteRepoGrantCalls != 2 || len(repoAccessScreenOf(t, m).rows) != 5 {
		t.Fatalf("retry: calls=%d rows=%d", client.deleteRepoGrantCalls, len(repoAccessScreenOf(t, m).rows))
	}
}

func TestRepoAccessXWithNothingSelectedDoesNothing(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, &fakeAdminClient{}, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
	if repoAccessScreenOf(t, m).confirm.Active() || m.screen != screenAdminRepositoryAccess {
		t.Fatalf("confirm=%v screen=%q", repoAccessScreenOf(t, m).confirm.Active(), m.screen)
	}
}

func TestRepoAccessRevokeWithAnExpiredSessionGoesToLogin(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, 150, 24)
	client.deleteRepoGrantErr = AdminSessionExpiredError{}
	m = repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login", m.screen)
	}
}

func TestRepoAccessListingWithAnExpiredSessionGoesToLoginAfterOneCall(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	client.allGrantsErrs = map[string]error{"": AdminSessionExpiredError{}}
	m := repoAccessModelAt(t, client, 150, 24)
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login", m.screen)
	}
	if len(client.allGrantsCursors) != 1 {
		t.Fatalf("grant reads after expiry = %d, want 1", len(client.allGrantsCursors))
	}
}

func TestRepoAccessDropsMessagesOfAnotherInstance(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	before := len(repoAccessScreenOf(t, m).rows)
	stale := repoAccessScreenOf(t, m).gen - 1
	for _, msg := range []tea.Msg{
		repoAccessGrantsLoadedMsg{gen: stale, page: ports.AdminGrantPage{Grants: []ports.AdminGrantEntry{{Repository: "x/y", Username: "mallory"}}, Next: "c9"}},
		repoAccessGrantsLoadedMsg{gen: stale, err: AdminSessionExpiredError{}},
		repoAccessRevokedMsg{gen: stale, repo: "team/api", username: "alice"},
		repoAccessRevokedMsg{gen: stale, repo: "team/api", username: "alice", err: AdminSessionExpiredError{}},
	} {
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if m.screen != screenAdminRepositoryAccess || len(repoAccessScreenOf(t, m).rows) != before {
			t.Fatalf("a foreign %T changed the screen (screen=%q rows=%d)", msg, m.screen, len(repoAccessScreenOf(t, m).rows))
		}
	}
}

func TestRepoAccessEnterOpensTheUserOrTheRobot(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyEnter())
	edit, _ := m.adminScreens[slotEditUser].(editUserScreen)
	if m.screen != screenAdminEditUser || edit.userID != "u-alice" || edit.username != "alice" {
		t.Fatalf("Enter on alice: screen=%q id=%q name=%q", m.screen, edit.userID, edit.username)
	}

	m = repoAccessModelAt(t, repoAccessClient(), 150, 24)
	m = repoAccessPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyEnter())
	if m.screen != screenAdminRobots {
		t.Fatalf("Enter on a robot opened %q, want the Robots list", m.screen)
	}
}

func TestRepoAccessGOpensGrantAccessAndEscComesBackToAFreshList(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, 150, 24)
	oldGen := repoAccessScreenOf(t, m).gen
	m = repoAccessPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("g opened %q", m.screen)
	}
	callsBefore := len(client.allGrantsCursors)
	m = repoAccessPress(t, m, testKeyEsc())
	if m.screen != screenAdminRepositoryAccess {
		t.Fatalf("Esc from Grant access went to %q", m.screen)
	}
	if s := repoAccessScreenOf(t, m); s.gen == oldGen || len(client.allGrantsCursors) <= callsBefore {
		t.Fatalf("the list was not re-read (gen %d -> %d, calls %d -> %d)", oldGen, s.gen, callsBefore, len(client.allGrantsCursors))
	}
}

func TestRepoAccessEmptyStates(t *testing.T) {
	t.Parallel()
	noGrants := repoAccessModelAt(t, &fakeAdminClient{}, 150, 24)
	if out := repoAccessRender(noGrants); !strings.Contains(out, "No grants yet. Press g to grant access.") {
		t.Errorf("no-grants state:\n%s", out)
	}
	noMatch := repoAccessPress(t, repoAccessModelAt(t, repoAccessClient(), 150, 24), testKeyRunes("/"), testKeyRunes("z"))
	if out := repoAccessRender(noMatch); !strings.Contains(out, "No grants match.") {
		t.Errorf("no-match state:\n%s", out)
	}
}

func TestRepoAccessFooterHintsFollowTheState(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), 150, 24)
	out := repoAccessRender(m)
	for _, text := range []string{"↑↓ move", "enter open", "/ search", "g grant access", "x revoke", "esc back"} {
		if !strings.Contains(out, text) {
			t.Errorf("footer lacks %q", text)
		}
	}
	filtering := repoAccessRender(repoAccessPress(t, m, testKeyRunes("/")))
	if !strings.Contains(filtering, "enter done") {
		t.Errorf("filter footer lacks \"enter done\"")
	}
}

func repoAccessManyRows(count int) *fakeAdminClient {
	client := &fakeAdminClient{}
	for i := range count {
		name := fmt.Sprintf("organización-ñandú/servicio-con-un-nombre-muy-largo-número-%03d", i)
		client.allGrants = append(client.allGrants,
			repoAccessEntry(name, "u-alice", "alice", false, domainauth.RepoRoleAdmin, time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)),
			repoAccessEntry(name, "u-long", "usuario-con-nombre-larguísimo-日本語", false, domainauth.RepoRoleWriter, repoAccessDay(time.April, 3)),
		)
	}
	return client
}

func repoAccessNoOverflow(t *testing.T, label, rendered string, w, h int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) > h {
		t.Fatalf("%s: %d rows, terminal has %d\n%s", label, len(lines), h, rendered)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > w {
			t.Fatalf("%s: line %d is %d cells wide, terminal has %d: %q", label, i+1, width, w, line)
		}
	}
}

// repoAccessLoadingModel mounts the screen and leaves its first read
// undelivered.
func repoAccessLoadingModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := newAdminReadyModel(t, repoAccessClient())
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, _ := m.Update(navigateMsg{To: screenAdminRepositoryAccess})
	return updated.(Model)
}

func TestRepoAccessGeometryAtEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	for _, size := range repoAccessSizes {
		states := map[string]func() Model{
			"normal":  func() Model { return repoAccessModelAt(t, repoAccessClient(), size.w, size.h) },
			"empty":   func() Model { return repoAccessModelAt(t, &fakeAdminClient{}, size.w, size.h) },
			"loading": func() Model { return repoAccessLoadingModel(t, size.w, size.h) },
			"partial failure": func() Model {
				client := repoAccessBulk(450)
				client.allGrantsErrs = map[string]error{"c200": errors.New(strings.Repeat("a very long error ", 20))}
				m := repoAccessModelAt(t, client, size.w, size.h)
				return repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("zzz")...)...)
			},
			"read error": func() Model {
				client := repoAccessClient()
				client.allGrantsErrs = map[string]error{"": errors.New(strings.Repeat("a very long error ", 20))}
				return repoAccessModelAt(t, client, size.w, size.h)
			},
			"many rows, long and multibyte names": func() Model {
				m := repoAccessModelAt(t, repoAccessManyRows(80), size.w, size.h)
				for range 25 {
					m = repoAccessPress(t, m, testKeyDown())
				}
				return m
			},
			"filtering": func() Model {
				m := repoAccessModelAt(t, repoAccessClient(), size.w, size.h)
				return repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("team/日本語-long-long-long-long-long-long")...)...)
			},
			"revoke prompt": func() Model {
				m := repoAccessModelAt(t, repoAccessClient(), size.w, size.h)
				return repoAccessPress(t, m, testKeyRunes("x"))
			},
		}
		for name, build := range states {
			m := build()
			label := fmt.Sprintf("%s at %dx%d", name, size.w, size.h)
			repoAccessNoOverflow(t, label, m.View().Content, size.w, size.h)
		}
	}
}

func TestRepoAccessSelectedRowStaysVisibleWhileScrolling(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessManyRows(80), 150, 24)
	for i := range 40 {
		m = repoAccessPress(t, m, testKeyDown())
		s := repoAccessScreenOf(t, m)
		row, _ := s.selected(s.visibleRows())
		if out := repoAccessRender(m); !strings.Contains(out, "▸ "+grantTruncate(row.repo, 20)[:10]) {
			t.Fatalf("step %d: the selected row is not on screen:\n%s", i, out)
		}
	}
}

func assertRepoAccessGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "repo_access", name+".golden")
	if *updateRepoAccessGoldens {
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
		t.Fatalf("read golden %s (run with -update-repo-access): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-repo-access to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestRepoAccessViewGoldens(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, w, h int) Model
	}{
		{"grants", func(t *testing.T, w, h int) Model {
			return repoAccessModelAt(t, repoAccessClient(), w, h)
		}},
		{"filtered", func(t *testing.T, w, h int) Model {
			m := repoAccessModelAt(t, repoAccessClient(), w, h)
			return repoAccessPress(t, m, testKeyRunes("/"), testKeyRunes("t"), testKeyRunes("e"), testKeyRunes("a"), testKeyRunes("m"), testKeyRunes("/"), testKeyRunes("d"))
		}},
		{"revoke_prompt", func(t *testing.T, w, h int) Model {
			m := repoAccessModelAt(t, repoAccessClient(), w, h)
			return repoAccessPress(t, m, testKeyRunes("x"))
		}},
		{"revoked", func(t *testing.T, w, h int) Model {
			m := repoAccessModelAt(t, repoAccessClient(), w, h)
			return repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
		}},
		{"revoke_failed", func(t *testing.T, w, h int) Model {
			client := repoAccessClient()
			client.deleteRepoGrantErr = errors.New("forbidden")
			m := repoAccessModelAt(t, client, w, h)
			return repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
		}},
		{"empty", func(t *testing.T, w, h int) Model {
			return repoAccessModelAt(t, &fakeAdminClient{}, w, h)
		}},
		{"read_error", func(t *testing.T, w, h int) Model {
			client := repoAccessClient()
			client.allGrantsErrs = map[string]error{"": errors.New("boom")}
			return repoAccessModelAt(t, client, w, h)
		}},
		{"loading", func(t *testing.T, w, h int) Model { return repoAccessLoadingModel(t, w, h) }},
		{"partial_error", func(t *testing.T, w, h int) Model {
			client := repoAccessBulk(450)
			client.allGrantsErrs = map[string]error{"c200": errors.New("boom")}
			m := repoAccessModelAt(t, client, w, h)
			return repoAccessPress(t, m, testKeyRunes("/"), testKeyRunes("z"), testKeyEnter())
		}},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
			t.Run(fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), func(t *testing.T) {
				assertRepoAccessGolden(t, fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), repoAccessRender(tc.build(t, size.w, size.h)))
			})
		}
	}
}

// repoAccessSplit serves two pages (limit 200): team/split holds 200 grants
// from page 1 onward, and its only Admin ("zed", last by username) is on
// page 2 when withAdmin is set.
func repoAccessSplit(withAdmin bool) *fakeAdminClient {
	client := &fakeAdminClient{}
	client.allGrants = append(client.allGrants, repoAccessEntry("team/alone", "u-w", "wanda", false, domainauth.RepoRoleWriter, time.Time{}))
	for i := range 198 {
		client.allGrants = append(client.allGrants, repoAccessEntry("team/split", fmt.Sprintf("u-%03d", i), fmt.Sprintf("user%03d", i), false, domainauth.RepoRoleReader, time.Time{}))
	}
	// Page 1 ends here: 199 rows; the 200th is the first of page 2's view.
	client.allGrants = append(client.allGrants, repoAccessEntry("team/split", "u-x", "xavier", false, domainauth.RepoRoleReader, time.Time{}))
	role := domainauth.RepoRoleReader
	if withAdmin {
		role = domainauth.RepoRoleAdmin
	}
	client.allGrants = append(client.allGrants, repoAccessEntry("team/split", "u-z", "zed", false, role, time.Time{}))
	return client
}

func TestRepoAccessPanelNeverClaimsNoAdminOnASplitRepository(t *testing.T) {
	t.Parallel()
	const noAdmin = "No Admin on this repository."
	for _, withAdmin := range []bool{true, false} {
		client := repoAccessSplit(withAdmin)
		m := repoAccessModelAt(t, client, 200, 40)
		if s := repoAccessScreenOf(t, m); s.done || len(s.rows) != 200 {
			t.Fatalf("page 1: done=%v rows=%d", s.done, len(s.rows))
		}
		// A repository fully inside page 1 is exact even while next is set.
		out := repoAccessRender(m)
		if !strings.Contains(out, noAdmin) || strings.Contains(out, "Loading more…") {
			t.Fatalf("team/alone (first, complete): want the exact warning and no Loading more…:\n%s", out)
		}
		m = repoAccessPress(t, m, testKeyDown())
		if len(client.allGrantsCursors) != 1 {
			t.Fatalf("reads = %q, want the split repository shown from page 1 only", client.allGrantsCursors)
		}
		out = repoAccessRender(m)
		if strings.Contains(out, noAdmin) || !strings.Contains(out, "Loading more…") || !strings.Contains(out, "199+ principals have access") {
			t.Fatalf("admin=%v split repository on page 1: no warning, Loading more… and a partial count expected:\n%s", withAdmin, out)
		}
		m = repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("zed")...)...)
		if s := repoAccessScreenOf(t, m); !s.done || len(s.rows) != 201 {
			t.Fatalf("page 2: done=%v rows=%d", s.done, len(s.rows))
		}
		out = repoAccessRender(m)
		if strings.Contains(out, "Loading more…") || strings.Contains(out, "+ principals") || strings.Contains(out, noAdmin) == withAdmin {
			t.Fatalf("admin=%v after page 2: exact panel expected (warning iff no admin):\n%s", withAdmin, out)
		}
	}
}

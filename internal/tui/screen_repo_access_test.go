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
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// Repository access has its own golden flag so regenerating its goldens can
// never touch another screen's.
var updateRepoAccessGoldens = flag.Bool("update-repo-access", false, "rewrite the repository access golden files")

var repoAccessSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

var repoAccessKnown = []string{"team/api", "team/docs", "team/web", "team/infra"}

func repoAccessDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 8, 0, 0, 0, time.UTC)
}

// repoAccessClient serves the board's sample: four repositories, three of them
// with grants, one of them (team/infra) granted to a principal that is neither
// a user nor a robot any more.
func repoAccessClient() *fakeAdminClient {
	return &fakeAdminClient{
		users: []ports.AdminUser{
			{ID: "u-alice", Username: "alice", Enabled: true},
			{ID: "u-bob", Username: "bob", Enabled: true},
			{ID: "u-carol", Username: "carol", Enabled: true},
		},
		robots: []ports.AdminRobot{{ID: "r-ci", Username: "ci-publisher", Enabled: true}},
		repoGrants: map[string][]ports.AdminRepositoryGrant{
			"team/api": {
				{Username: "bob", Role: domainauth.RepoRoleReader, CreatedAt: repoAccessDay(time.November, 2)},
				{Username: "alice", Role: domainauth.RepoRoleWriter, CreatedAt: repoAccessDay(time.October, 14)},
				{Username: "ci-publisher", Role: domainauth.RepoRoleWriter, CreatedAt: repoAccessDay(time.January, 12)},
				{Username: "carol", Role: domainauth.RepoRoleWriter, CreatedAt: repoAccessDay(time.November, 9)},
			},
			"team/docs": {
				{Username: "alice", Role: domainauth.RepoRoleReader, CreatedAt: repoAccessDay(time.March, 3)},
			},
			"team/infra": {
				{Username: "old-deploy", Role: domainauth.RepoRoleAdmin, CreatedAt: repoAccessDay(time.February, 5)},
			},
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

func repoAccessModelAt(t *testing.T, client AdminClient, known []string, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, known, client)
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

func TestRepoAccessListsEveryGrantByRepositoryWithTheMinimumCalls(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
	if m.screen != screenAdminRepositoryAccess {
		t.Fatalf("screen = %q", m.screen)
	}
	got := repoAccessKeyPairs(repoAccessScreenOf(t, m))
	want := []string{"team/api|alice", "team/api|bob", "team/api|carol", "team/api|ci-publisher", "team/docs|alice", "team/infra|old-deploy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	// One read per repository, one users read, one robots read.
	if client.listRepoGrantsCalls != len(repoAccessKnown) || client.listUsersCalls < 1 || client.listRobotsCalls < 1 {
		t.Fatalf("calls: grants=%d users=%d robots=%d", client.listRepoGrantsCalls, client.listUsersCalls, client.listRobotsCalls)
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
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc went to %q, want the Overview", m.screen)
	}
}

func TestRepoAccessReadsOnlyThePageUntilTheCursorNeedsMore(t *testing.T) {
	t.Parallel()
	client := &fakeAdminClient{repoGrants: map[string][]ports.AdminRepositoryGrant{}}
	var known []string
	for i := range 60 {
		name := fmt.Sprintf("org/repo%02d", i)
		known = append(known, name)
		client.repoGrants[name] = []ports.AdminRepositoryGrant{{Username: "alice", Role: domainauth.RepoRoleReader, CreatedAt: repoAccessDay(time.May, 1)}}
	}
	m := repoAccessModelAt(t, client, known, 150, 24)
	page := repoAccessPageRows(24)
	if client.listRepoGrantsCalls != page {
		t.Fatalf("initial reads = %d, want one page (%d) of 60 repositories", client.listRepoGrantsCalls, page)
	}
	if out := repoAccessRender(m); !strings.Contains(out, fmt.Sprintf("(read %d of 60 repositories)", page)) {
		t.Fatalf("the title does not say how much was read:\n%s", out)
	}
	for range page - 1 {
		m = repoAccessPress(t, m, testKeyDown())
	}
	if client.listRepoGrantsCalls <= page {
		t.Fatalf("moving to the end of the page read nothing more (%d calls)", client.listRepoGrantsCalls)
	}
	if got := len(repoAccessScreenOf(t, m).rows); got < page+1 {
		t.Fatalf("rows = %d after scrolling", got)
	}
	if client.listRepoGrantsCalls >= 60 {
		t.Fatalf("read every repository (%d calls) instead of paging", client.listRepoGrantsCalls)
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
		m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
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
	client.repoGrants["team/api"] = append(client.repoGrants["team/api"], ports.AdminRepositoryGrant{Username: "jkq-bot", Role: domainauth.RepoRoleReader})
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
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
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("/"))
	m = repoAccessPress(t, m, keyMsg{Text: "team/docs"})
	if s := repoAccessScreenOf(t, m); s.filter != "team/docs" || len(s.visibleRows()) != 1 {
		t.Fatalf("pasted filter = %q rows = %d", s.filter, len(s.visibleRows()))
	}
}

func TestRepoAccessEscEndsTheSearchThenGoesBack(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("/"), testKeyRunes("a"), testKeyEsc())
	if s := repoAccessScreenOf(t, m); s.filtering || s.filter != "a" || m.screen != screenAdminRepositoryAccess {
		t.Fatalf("first Esc: filtering=%v filter=%q screen=%q", s.filtering, s.filter, m.screen)
	}
	m = repoAccessPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("second Esc went to %q", m.screen)
	}
}

func TestRepoAccessQOutsideTheFilterDoesNotLeaveTheScreen(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	updated, cmd := m.Update(testKeyRunes("q"))
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatalf("q quit although the footer does not offer it")
		}
	}
	if updated.(Model).screen != screenAdminRepositoryAccess {
		t.Fatalf("screen = %q", updated.(Model).screen)
	}
}

func TestRepoAccessJAndKDoNotMove(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
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
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
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
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
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
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
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
	m := repoAccessModelAt(t, &fakeAdminClient{}, repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
	if repoAccessScreenOf(t, m).confirm.Active() || m.screen != screenAdminRepositoryAccess {
		t.Fatalf("confirm=%v screen=%q", repoAccessScreenOf(t, m).confirm.Active(), m.screen)
	}
}

func TestRepoAccessRevokeWithAnExpiredSessionGoesToLogin(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
	client.deleteRepoGrantErr = AdminSessionExpiredError{}
	m = repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login", m.screen)
	}
}

func TestRepoAccessListingWithAnExpiredSessionGoesToLoginAfterOneCall(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	client.listRepoGrantsErr = AdminSessionExpiredError{}
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login", m.screen)
	}
	if client.listRepoGrantsCalls != 1 {
		t.Fatalf("grant reads after expiry = %d, want the batch to stop at 1", client.listRepoGrantsCalls)
	}
}

func TestRepoAccessDropsMessagesOfAnotherInstance(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	before := len(repoAccessScreenOf(t, m).rows)
	stale := repoAccessScreenOf(t, m).gen - 1
	for _, msg := range []tea.Msg{
		repoAccessGrantsLoadedMsg{gen: stale, results: []repoAccessRepoGrants{{repo: "x/y", grants: []ports.AdminRepositoryGrant{{Username: "mallory"}}}}},
		repoAccessGrantsLoadedMsg{gen: stale, results: []repoAccessRepoGrants{{repo: "x/y", err: AdminSessionExpiredError{}}}},
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

func TestRepoAccessReadFailureOfOneRepositoryIsShownAndOthersStay(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	client.listRepoGrantsErr = errors.New("boom")
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
	out := repoAccessRender(m)
	if !strings.Contains(out, "Could not read grants of team/api: boom (+3 more)") {
		t.Fatalf("read failure not shown:\n%s", out)
	}
}

func TestRepoAccessEnterOpensTheUserOrTheRobot(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyEnter())
	edit, _ := m.adminScreens[slotEditUser].(editUserScreen)
	if m.screen != screenAdminEditUser || edit.userID != "u-alice" || edit.username != "alice" {
		t.Fatalf("Enter on alice: screen=%q id=%q name=%q", m.screen, edit.userID, edit.username)
	}

	m = repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyEnter())
	if m.screen != screenAdminRobots {
		t.Fatalf("Enter on a robot opened %q, want the Robots list", m.screen)
	}
}

func TestRepoAccessEnterOnAnUnknownPrincipalSaysSo(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
	m = repoAccessPress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyEnter())
	if m.screen != screenAdminRepositoryAccess {
		t.Fatalf("screen = %q", m.screen)
	}
	if out := repoAccessRender(m); !strings.Contains(out, "Cannot open old-deploy") {
		t.Fatalf("no explanation:\n%s", out)
	}
}

func TestRepoAccessGOpensGrantAccessAndEscComesBackToAFreshList(t *testing.T) {
	t.Parallel()
	client := repoAccessClient()
	m := repoAccessModelAt(t, client, repoAccessKnown, 150, 24)
	oldGen := repoAccessScreenOf(t, m).gen
	m = repoAccessPress(t, m, testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("g opened %q", m.screen)
	}
	callsBefore := client.listRepoGrantsCalls
	m = repoAccessPress(t, m, testKeyEsc())
	if m.screen != screenAdminRepositoryAccess {
		t.Fatalf("Esc from Grant access went to %q", m.screen)
	}
	if s := repoAccessScreenOf(t, m); s.gen == oldGen || client.listRepoGrantsCalls <= callsBefore {
		t.Fatalf("the list was not re-read (gen %d -> %d, calls %d -> %d)", oldGen, s.gen, callsBefore, client.listRepoGrantsCalls)
	}
}

func TestRepoAccessEmptyStates(t *testing.T) {
	t.Parallel()
	noGrants := repoAccessModelAt(t, &fakeAdminClient{}, repoAccessKnown, 150, 24)
	if out := repoAccessRender(noGrants); !strings.Contains(out, "No grants yet. Press g to grant access.") {
		t.Errorf("no-grants state:\n%s", out)
	}
	noRepos := repoAccessModelAt(t, &fakeAdminClient{}, nil, 150, 24)
	if out := repoAccessRender(noRepos); !strings.Contains(out, "No repositories in the catalog yet.") {
		t.Errorf("no-repositories state:\n%s", out)
	}
	noMatch := repoAccessPress(t, repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24), testKeyRunes("/"), testKeyRunes("z"))
	if out := repoAccessRender(noMatch); !strings.Contains(out, "No grants match.") {
		t.Errorf("no-match state:\n%s", out)
	}
}

func TestRepoAccessFooterHintsFollowTheState(t *testing.T) {
	t.Parallel()
	m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, 150, 24)
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

func repoAccessManyRows(count int) (*fakeAdminClient, []string) {
	client := &fakeAdminClient{
		users:      []ports.AdminUser{{ID: "u-1", Username: "alice"}},
		repoGrants: map[string][]ports.AdminRepositoryGrant{},
	}
	var known []string
	for i := range count {
		name := fmt.Sprintf("organización-ñandú/servicio-con-un-nombre-muy-largo-número-%03d", i)
		known = append(known, name)
		client.repoGrants[name] = []ports.AdminRepositoryGrant{
			{Username: "usuario-con-nombre-larguísimo-日本語", Role: domainauth.RepoRoleWriter, CreatedAt: repoAccessDay(time.April, 3)},
			{Username: "alice", Role: domainauth.RepoRoleAdmin, CreatedAt: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)},
		}
	}
	return client, known
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

func TestRepoAccessGeometryAtEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	for _, size := range repoAccessSizes {
		states := map[string]func() Model{
			"normal": func() Model { return repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, size.w, size.h) },
			"empty":  func() Model { return repoAccessModelAt(t, &fakeAdminClient{}, repoAccessKnown, size.w, size.h) },
			"no catalog": func() Model {
				return repoAccessModelAt(t, &fakeAdminClient{}, nil, size.w, size.h)
			},
			"read error": func() Model {
				client := repoAccessClient()
				client.listRepoGrantsErr = errors.New(strings.Repeat("a very long error ", 20))
				return repoAccessModelAt(t, client, repoAccessKnown, size.w, size.h)
			},
			"many rows, long and multibyte names": func() Model {
				client, known := repoAccessManyRows(80)
				m := repoAccessModelAt(t, client, known, size.w, size.h)
				for range 25 {
					m = repoAccessPress(t, m, testKeyDown())
				}
				return m
			},
			"filtering": func() Model {
				m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, size.w, size.h)
				return repoAccessPress(t, m, append([]keyMsg{testKeyRunes("/")}, repoAccessType("team/日本語-long-long-long-long-long-long")...)...)
			},
			"revoke prompt": func() Model {
				m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, size.w, size.h)
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
	client, known := repoAccessManyRows(80)
	m := repoAccessModelAt(t, client, known, 150, 24)
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
			return repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, w, h)
		}},
		{"filtered", func(t *testing.T, w, h int) Model {
			m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, w, h)
			return repoAccessPress(t, m, testKeyRunes("/"), testKeyRunes("t"), testKeyRunes("e"), testKeyRunes("a"), testKeyRunes("m"), testKeyRunes("/"), testKeyRunes("d"))
		}},
		{"revoke_prompt", func(t *testing.T, w, h int) Model {
			m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, w, h)
			return repoAccessPress(t, m, testKeyRunes("x"))
		}},
		{"revoked", func(t *testing.T, w, h int) Model {
			m := repoAccessModelAt(t, repoAccessClient(), repoAccessKnown, w, h)
			return repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
		}},
		{"revoke_failed", func(t *testing.T, w, h int) Model {
			client := repoAccessClient()
			client.deleteRepoGrantErr = errors.New("forbidden")
			m := repoAccessModelAt(t, client, repoAccessKnown, w, h)
			return repoAccessPress(t, m, testKeyRunes("x"), testKeyEnter())
		}},
		{"empty", func(t *testing.T, w, h int) Model {
			return repoAccessModelAt(t, &fakeAdminClient{}, repoAccessKnown, w, h)
		}},
		{"read_error", func(t *testing.T, w, h int) Model {
			client := repoAccessClient()
			client.listRepoGrantsErr = errors.New("boom")
			return repoAccessModelAt(t, client, repoAccessKnown, w, h)
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

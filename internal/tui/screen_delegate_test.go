package tui

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// The delegate screen has its own golden flag so regenerating its goldens can
// never touch another screen's.
var updateDelegateGoldens = flag.Bool("update-delegate", false, "rewrite the delegate golden files")

var delegateSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

// delegateClient records the grant calls the screen makes and serves one
// grants list per repository.
type delegateClient struct {
	*fakeAdminClient
	mu      sync.Mutex
	grants  map[string][]ports.AdminRepositoryGrant
	lists   []string
	puts    []ports.AdminPutRepositoryGrantInput
	deletes [][2]string
	listErr error
	putErr  error
	delErr  error
}

func (c *delegateClient) ListRepositoryGrants(_ context.Context, _ AdminSession, repository string) ([]ports.AdminRepositoryGrant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lists = append(c.lists, repository)
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]ports.AdminRepositoryGrant(nil), c.grants[repository]...), nil
}

func (c *delegateClient) PutRepositoryGrant(_ context.Context, _ AdminSession, input ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.puts = append(c.puts, input)
	if c.putErr != nil {
		return ports.AdminRepositoryGrant{}, c.putErr
	}
	return ports.AdminRepositoryGrant{Username: input.Username, Role: input.Role}, nil
}

func (c *delegateClient) DeleteRepositoryGrant(_ context.Context, _ AdminSession, repository, username string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletes = append(c.deletes, [2]string{repository, username})
	return c.delErr
}

func (c *delegateClient) counts() (lists, puts, deletes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.lists), len(c.puts), len(c.deletes)
}

// delegateClientFor serves the board's sample: team/api, administered by erin.
func delegateClientFor() *delegateClient {
	return &delegateClient{fakeAdminClient: &fakeAdminClient{}, grants: map[string][]ports.AdminRepositoryGrant{
		"team/api": {
			{Username: "alice", Role: domainauth.RepoRoleWriter},
			{Username: "bob", Role: domainauth.RepoRoleReader},
			{Username: "carol", Role: domainauth.RepoRoleWriter},
			{Username: "ci-publisher", Role: domainauth.RepoRoleWriter},
			{Username: "dave", Role: domainauth.RepoRoleAdmin},
			{Username: "erin", Role: domainauth.RepoRoleAdmin},
		},
		"team/other": {{Username: "mallory", Role: domainauth.RepoRoleReader}},
	}}
}

// delegateRun runs cmd and everything it triggers, flattening batches the way
// the Bubble Tea runtime does.
func delegateRun(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = delegateRun(t, m, sub)
		}
		return m
	}
	updated, next := m.Update(msg)
	return delegateRun(t, updated.(Model), next)
}

func delegatePress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = delegateRun(t, updated.(Model), cmd)
	}
	return m
}

func delegateType(text string) []keyMsg {
	keys := make([]keyMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, testKeyRunes(string(r)))
	}
	return keys
}

// delegateConsole is the console Repositories screen of an operator who is
// signed in as erin.
func delegateConsole(t *testing.T, client AdminClient, repositories []string, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, repositories, client)
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "erin", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	return m
}

// delegateModelAt opens the delegate screen of team/api through the console's
// g key and delivers the read.
func delegateModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := delegateConsole(t, client, []string{"team/api"}, w, h)
	return delegatePress(t, m, testKeyRunes("g"))
}

// delegateLoadingModel opens the screen and leaves its first read undelivered.
func delegateLoadingModel(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := delegateConsole(t, client, []string{"team/api"}, w, h)
	updated, _ := m.Update(testKeyRunes("g"))
	return updated.(Model)
}

func delegateScreenOf(t *testing.T, m Model) delegateScreen {
	t.Helper()
	s, ok := m.adminScreens[slotDelegate].(delegateScreen)
	if !ok {
		t.Fatalf("slot holds %T, want delegateScreen", m.adminScreens[slotDelegate])
	}
	return s
}

func delegateRender(m Model) string { return ansi.Strip(m.View().Content) }

func delegateManyGrants(count int) *delegateClient {
	client := delegateClientFor()
	var grants []ports.AdminRepositoryGrant
	for i := range count {
		name := fmt.Sprintf("user%03d", i)
		if i%7 == 0 {
			name = fmt.Sprintf("日本語のユーザー名-%03d-long-long-long-long-long-long-long", i)
		}
		role := domainauth.RepoRoleReader
		if i%3 == 0 {
			role = domainauth.RepoRoleWriter
		}
		grants = append(grants, ports.AdminRepositoryGrant{Username: name, Role: role})
	}
	grants = append(grants, ports.AdminRepositoryGrant{Username: "erin", Role: domainauth.RepoRoleAdmin})
	client.grants["team/api"] = grants
	return client
}

// ---- opening ----

func TestDelegateOpensFromTheConsoleForTheSelectedRepositoryOnly(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	s := delegateScreenOf(t, m)
	if m.screen != screenRepoAdminGrants || s.repo != "team/api" || s.loading || !s.loaded {
		t.Fatalf("screen=%q repo=%q loading=%v loaded=%v", m.screen, s.repo, s.loading, s.loaded)
	}
	if client.lists[0] != "team/api" || len(client.lists) != 1 {
		t.Fatalf("lists = %v, want exactly team/api", client.lists)
	}
	out := delegateRender(m)
	for _, want := range []string{"alice", "Writer", "bob", "Reader", "ci-publisher", "dave", "Admin", "USERNAME", "ROLE", "GRANTS ON TEAM/API", "you are Admin on this repository"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "mallory") || strings.Contains(out, "team/other") {
		t.Fatalf("the screen shows another repository's grants:\n%s", out)
	}
}

func TestDelegateShellShowsARepositoryAdminContextNotTheAdminNavigation(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	out := delegateRender(m)
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "regixtry › Repository access › team/api") || !strings.Contains(lines[0], "signed in as erin") {
		t.Fatalf("header = %q", lines[0])
	}
	for _, item := range shellNavItems() {
		if item.ID == shellNavRepositoryAccess {
			continue
		}
		if strings.Contains(out, item.Label) {
			t.Errorf("a repository admin sees the admin-only item %q:\n%s", item.Label, out)
		}
	}
	var sidebar []string
	for _, line := range lines {
		sidebar = append(sidebar, strings.Fields(strings.ReplaceAll(string([]rune(line)[:min(shellSidebarWidth, len([]rune(line)))]), "│", ""))...)
	}
	text := strings.Join(sidebar, " ")
	for _, want := range []string{"regixtry repository admin", "ACCESS", "Repository access", "You administer team/api. Global settings are visible to global admins only."} {
		if !strings.Contains(text, want) {
			t.Errorf("sidebar lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(lines[0], "Admin ›") {
		t.Fatalf("the admin root crumb is shown to a repository admin: %q", lines[0])
	}
}

func TestDelegateEmptyStateAndLoadingState(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	client.grants["team/api"] = nil
	if out := delegateRender(delegateModelAt(t, client, 150, 24)); !strings.Contains(out, "No grants for this repository.") {
		t.Fatalf("empty state missing:\n%s", out)
	}
	if out := delegateRender(delegateLoadingModel(t, delegateClientFor(), 150, 24)); !strings.Contains(out, "Reading grants…") {
		t.Fatalf("loading state missing:\n%s", out)
	}
}

func TestDelegateLoadErrorIsVisibleRefusesNewGrantAndRetries(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	client.listErr = errors.New("forbidden")
	m := delegateModelAt(t, client, 150, 24)
	out := delegateRender(m)
	if !strings.Contains(out, "forbidden") || !strings.Contains(out, "(press r to retry)") || !strings.Contains(out, "r retry") {
		t.Fatalf("the error is not visible with its retry:\n%s", out)
	}
	m = delegatePress(t, m, testKeyRunes("n"))
	if s := delegateScreenOf(t, m); s.form.active || !strings.Contains(s.status, "not be an administrator") {
		t.Fatalf("n after a failed read: form=%v status=%q", s.form.active, s.status)
	}
	client.listErr = nil
	m = delegatePress(t, m, testKeyRunes("r"))
	if s := delegateScreenOf(t, m); !s.loaded || s.loadErr != "" || len(s.rows) != 6 {
		t.Fatalf("retry: loaded=%v err=%q rows=%d", s.loaded, s.loadErr, len(s.rows))
	}
	m = delegatePress(t, m, testKeyRunes("n"))
	if !delegateScreenOf(t, m).form.active {
		t.Fatal("n after a successful retry did not open the form")
	}
}

func TestDelegateNewGrantIsRefusedWhileTheFirstReadIsInFlight(t *testing.T) {
	t.Parallel()
	m := delegateLoadingModel(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	if s := delegateScreenOf(t, m); s.form.active || s.status == "" {
		t.Fatalf("form=%v status=%q", s.form.active, s.status)
	}
}

func TestDelegateLoginThenOpensTheRepositoryTheOperatorChose(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	client.loginSession = AdminSession{Username: "erin", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)}
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, client)
	m.adminIntent, m.adminIntentRepository = adminIntentRepoGrants, "team/api"
	updated := runAdminLogin(t, m, "erin", "secret-pass")
	if updated.screen != screenRepoAdminGrants || updated.adminIntent != adminIntentOperator || updated.adminIntentRepository != "" {
		t.Fatalf("screen=%q intent=%q repo=%q", updated.screen, updated.adminIntent, updated.adminIntentRepository)
	}
	if s := delegateScreenOf(t, updated); s.repo != "team/api" {
		t.Fatalf("repo = %q", s.repo)
	}
}

func TestDelegateGKeyAsksForLoginWhenSignedOut(t *testing.T) {
	t.Parallel()
	m := newAdminReadyModelWithCatalog(t, []string{"team/app"}, &delegateClient{fakeAdminClient: &fakeAdminClient{}})
	updated := runKey(t, m, "g")
	if updated.screen != screenAdminLogin || updated.adminIntent != adminIntentRepoGrants || updated.adminIntentRepository != "team/app" {
		t.Fatalf("screen=%q intent=%q repo=%q", updated.screen, updated.adminIntent, updated.adminIntentRepository)
	}
}

func TestDelegateEveryOpeningMountsAFreshScreen(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateConsole(t, client, []string{"team/api", "team/other"}, 150, 24)
	m = delegatePress(t, m, testKeyRunes("g"))
	first := delegateScreenOf(t, m)
	m = delegatePress(t, m, testKeyRunes("n"), testKeyRunes("x"))
	m = delegatePress(t, m, testKeyEsc(), testKeyEsc())
	if m.screen != screenRepositories || m.adminScreens[slotDelegate] != nil {
		t.Fatalf("screen=%q slot=%T, want the console and an empty slot", m.screen, m.adminScreens[slotDelegate])
	}
	m = delegatePress(t, m, testKeyDown(), testKeyRunes("g"))
	second := delegateScreenOf(t, m)
	if second.gen == first.gen || second.repo != "team/other" || second.form.active || second.status != "" {
		t.Fatalf("second = %+v", second)
	}
	if out := delegateRender(m); strings.Contains(out, "alice") || !strings.Contains(out, "mallory") {
		t.Fatalf("grants of the first repository leaked into the second:\n%s", out)
	}
}

// ---- list keys ----

func TestDelegateMovesWithArrowsAndStaysInBounds(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyUp())
	if s := delegateScreenOf(t, m); s.cursor != 0 {
		t.Fatalf("cursor = %d above the first row", s.cursor)
	}
	m = delegatePress(t, m, testKeyDown(), testKeyDown(), testKeyRunes("j"), testKeyRunes("k"))
	if s := delegateScreenOf(t, m); s.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", s.cursor)
	}
	for range 20 {
		m = delegatePress(t, m, testKeyDown())
	}
	if s := delegateScreenOf(t, m); s.cursor != 5 {
		t.Fatalf("cursor = %d below the last row, want 5", s.cursor)
	}
	m = delegatePress(t, m, testKeyPgUp())
	if s := delegateScreenOf(t, m); s.cursor >= 5 {
		t.Fatalf("PgUp did not move: %d", s.cursor)
	}
}

func TestDelegateEscGoesBackToTheConsole(t *testing.T) {
	t.Parallel()
	m := delegatePress(t, delegateModelAt(t, delegateClientFor(), 150, 24), testKeyEsc())
	if m.screen != screenRepositories {
		t.Fatalf("screen = %q, want the console Repositories screen", m.screen)
	}
}

func TestDelegateQQuitsInTheListAndLogsOutWithL(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	if _, cmd := m.Update(testKeyRunes("q")); !didQuit(cmd) {
		t.Fatal("q in the list did not quit")
	}
	updated, _ := m.Update(testKeyRunes("l"))
	if got := updated.(Model); got.screen != screenAdminLogin {
		t.Fatalf("l in the list: screen = %q", got.screen)
	}
}

// ---- the form ----

func TestDelegateFormTypesJKQAndEveryLetterIntoTheUsername(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	for _, k := range delegateType("jkqlnexr-1") {
		updated, cmd := m.Update(k)
		if didQuit(cmd) {
			t.Fatalf("%q quit the program from the username field", k.Text)
		}
		m = updated.(Model)
	}
	s := delegateScreenOf(t, m)
	if s.form.username != "jkqlnexr-1" || m.screen != screenRepoAdminGrants {
		t.Fatalf("username = %q screen = %q", s.form.username, m.screen)
	}
	m = delegatePress(t, m, testKeyBackspace())
	if got := delegateScreenOf(t, m).form.username; got != "jkqlnexr-" {
		t.Fatalf("after backspace %q", got)
	}
}

func TestDelegateFormAcceptsPasteWithoutWhitespaceOrControls(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"), keyMsg{Text: "zoe \n\t\x1b[31m"})
	if got := delegateScreenOf(t, m).form.username; strings.ContainsAny(got, " \n\t\x1b") || !strings.HasPrefix(got, "zoe") {
		t.Fatalf("username = %q", got)
	}
	m = delegatePress(t, m, keyMsg{Text: strings.Repeat("a", 500)})
	if got := len([]rune(delegateScreenOf(t, m).form.username)); got > delegateUsernameLimit {
		t.Fatalf("username holds %d runes, limit %d", got, delegateUsernameLimit)
	}
}

func TestDelegateRoleCyclesBetweenReaderAndWriterOnlyAndNeverOffersAdmin(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	if s := delegateScreenOf(t, m); s.form.role != domainauth.RepoRoleReader || s.form.focus != delegateFieldUsername {
		t.Fatalf("form = %+v, want Reader on the username", s.form)
	}
	// Space on the username types nothing and does not change the role.
	m = delegatePress(t, m, testKeySpace())
	if s := delegateScreenOf(t, m); s.form.username != "" || s.form.role != domainauth.RepoRoleReader {
		t.Fatalf("space on the username: %+v", s.form)
	}
	m = delegatePress(t, m, testKeyTab())
	seen := map[domainauth.RepoRole]bool{}
	for range 6 {
		m = delegatePress(t, m, testKeySpace())
		s := delegateScreenOf(t, m)
		seen[s.form.role] = true
		if s.form.role == domainauth.RepoRoleAdmin {
			t.Fatal("the form offered the Admin role")
		}
		// The panel is the only place the form renders: its role field must
		// not show Admin either.
		for _, line := range strings.Split(delegateRender(m), "\n") {
			if at := strings.Index(line, "[Writer]"); at >= 0 && strings.Contains(line[at:], "Admin") {
				t.Fatalf("role field shows Admin: %q", line)
			}
			if at := strings.Index(line, "[Reader]"); at >= 0 && strings.Contains(line[at:], "Admin") {
				t.Fatalf("role field shows Admin: %q", line)
			}
		}
	}
	if len(seen) != 2 || !seen[domainauth.RepoRoleReader] || !seen[domainauth.RepoRoleWriter] {
		t.Fatalf("roles seen = %v, want exactly Reader and Writer", seen)
	}
}

func TestDelegateFormTabArrowsAndEscMove(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"), testKeyTab())
	if delegateScreenOf(t, m).form.focus != delegateFieldRole {
		t.Fatal("tab did not move to the role")
	}
	m = delegatePress(t, m, testKeyUp())
	if delegateScreenOf(t, m).form.focus != delegateFieldUsername {
		t.Fatal("up did not move back to the username")
	}
	m = delegatePress(t, m, testKeyRunes("a"), testKeyEsc())
	s := delegateScreenOf(t, m)
	if s.form.active || m.screen != screenRepoAdminGrants {
		t.Fatalf("esc: form=%v screen=%q, want the list", s.form.active, m.screen)
	}
}

func TestDelegateSaveSendsOnePutForTheFixedRepositoryAndRefreshes(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	m = delegatePress(t, m, delegateType("zoe")...)
	m = delegatePress(t, m, testKeyTab(), testKeySpace(), testKeyEnter())
	if len(client.puts) != 1 {
		t.Fatalf("puts = %v, want one", client.puts)
	}
	got := client.puts[0]
	if got.Repository != "team/api" || got.Username != "zoe" || got.Role != domainauth.RepoRoleWriter {
		t.Fatalf("PUT = %+v", got)
	}
	s := delegateScreenOf(t, m)
	if s.form.active || s.saving || !strings.Contains(s.status, "Saved zoe as Writer.") {
		t.Fatalf("after the save: form=%v saving=%v status=%q", s.form.active, s.saving, s.status)
	}
	if lists, _, _ := client.counts(); lists != 2 {
		t.Fatalf("lists = %d, want the open and the refresh", lists)
	}
}

func TestDelegateDefaultRoleIsReader(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	m = delegatePress(t, m, delegateType("zoe")...)
	delegatePress(t, m, testKeyEnter())
	if len(client.puts) != 1 || client.puts[0].Role != domainauth.RepoRoleReader {
		t.Fatalf("PUT = %+v, want Reader", client.puts)
	}
}

func TestDelegateDoubleEnterIsOnePut(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	m = delegatePress(t, m, delegateType("zoe")...)
	first, cmd1 := m.Update(testKeyEnter())
	_, cmd2 := first.Update(testKeyEnter())
	if cmd1 == nil || cmd2 != nil {
		t.Fatalf("first enter command = %v, second = %v: the second must be ignored while the save is in flight", cmd1 != nil, cmd2 != nil)
	}
	delegateRun(t, first.(Model), cmd1)
	if _, puts, _ := client.counts(); puts != 1 {
		t.Fatalf("puts = %d, want 1", puts)
	}
}

func TestDelegateSaveErrorKeepsTheFormVisibleAndAllowsRetry(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	client.putErr = errors.New("user not found")
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	m = delegatePress(t, m, delegateType("zoe")...)
	m = delegatePress(t, m, testKeyEnter())
	s := delegateScreenOf(t, m)
	if !s.form.active || s.saving || s.form.username != "zoe" || !strings.Contains(s.form.err, "user not found") {
		t.Fatalf("form=%v saving=%v username=%q err=%q", s.form.active, s.saving, s.form.username, s.form.err)
	}
	out := delegateRender(m)
	if !strings.Contains(out, "user not found") {
		t.Fatalf("the error is not on screen:\n%s", out)
	}
	client.putErr = nil
	m = delegatePress(t, m, testKeyEnter())
	if _, puts, _ := client.counts(); puts != 2 || delegateScreenOf(t, m).form.active {
		t.Fatalf("retry: puts=%d form=%v", puts, delegateScreenOf(t, m).form.active)
	}
}

func TestDelegateEmptyUsernameIsRefusedWithoutACall(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"), testKeyEnter())
	s := delegateScreenOf(t, m)
	if s.form.err != "Username is required." || !s.form.active {
		t.Fatalf("err = %q form = %v", s.form.err, s.form.active)
	}
	if _, puts, _ := client.counts(); puts != 0 {
		t.Fatalf("puts = %d", puts)
	}
}

func TestDelegateNewGrantCannotOverwriteAnAdminGrant(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"dave", "DAVE", "erin"} {
		client := delegateClientFor()
		m := delegateModelAt(t, client, 150, 24)
		m = delegatePress(t, m, testKeyRunes("n"))
		m = delegatePress(t, m, delegateType(name)...)
		m = delegatePress(t, m, testKeyEnter())
		if _, puts, _ := client.counts(); puts != 0 {
			t.Fatalf("%s: a PUT was sent over an Admin grant", name)
		}
		if err := delegateScreenOf(t, m).form.err; !strings.Contains(err, "ask a global admin") {
			t.Fatalf("%s: err = %q", name, err)
		}
	}
}

func TestDelegateEditChangesOnlyTheRoleOfTheSelectedGrant(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	// bob is the second row (Reader).
	m = delegatePress(t, m, testKeyDown(), testKeyRunes("e"))
	s := delegateScreenOf(t, m)
	if !s.form.active || !s.form.editing || s.form.username != "bob" || s.form.role != domainauth.RepoRoleReader || s.form.focus != delegateFieldRole {
		t.Fatalf("form = %+v", s.form)
	}
	// The username cannot be edited, not even by paste, and tab keeps the focus.
	m = delegatePress(t, m, delegateType("xyz")...)
	m = delegatePress(t, m, testKeyBackspace(), testKeyTab(), keyMsg{Text: "evil"})
	if got := delegateScreenOf(t, m); got.form.username != "bob" || got.form.focus != delegateFieldRole {
		t.Fatalf("form = %+v", got.form)
	}
	m = delegatePress(t, m, testKeySpace(), testKeyEnter())
	if len(client.puts) != 1 || client.puts[0] != (ports.AdminPutRepositoryGrantInput{Repository: "team/api", Username: "bob", Role: domainauth.RepoRoleWriter}) {
		t.Fatalf("PUT = %+v", client.puts)
	}
}

func TestDelegateEditRefusesAnAdminGrant(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	// dave is the fifth row (Admin).
	m = delegatePress(t, m, testKeyDown(), testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("e"))
	s := delegateScreenOf(t, m)
	if s.form.active || !strings.Contains(s.status, "ask a global admin") {
		t.Fatalf("form=%v status=%q", s.form.active, s.status)
	}
}

func TestDelegateEditAndRevokeWithNoGrantsSayNoGrantSelected(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	client.grants["team/api"] = nil
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("e"))
	if s := delegateScreenOf(t, m); s.form.active || s.status != "No grant selected to edit." {
		t.Fatalf("e: %+v", s)
	}
	m = delegatePress(t, m, testKeyRunes("x"))
	if s := delegateScreenOf(t, m); s.confirm.Active() || s.status != "No grant selected to revoke." {
		t.Fatalf("x: %+v", s)
	}
}

func TestDelegateRoleSentIsNeverAdminWhateverTheFormHolds(t *testing.T) {
	t.Parallel()
	for _, role := range []domainauth.RepoRole{domainauth.RepoRoleAdmin, "", "owner", domainauth.RepoRoleReader, domainauth.RepoRoleWriter} {
		got := delegateClampRole(role)
		if got != domainauth.RepoRoleReader && got != domainauth.RepoRoleWriter {
			t.Fatalf("delegateClampRole(%q) = %q", role, got)
		}
	}
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"), testKeyRunes("z"))
	s := delegateScreenOf(t, m)
	s.form.role = domainauth.RepoRoleAdmin
	m.adminScreens[slotDelegate] = s
	delegatePress(t, m, testKeyEnter())
	if len(client.puts) != 1 || client.puts[0].Role == domainauth.RepoRoleAdmin {
		t.Fatalf("PUT = %+v", client.puts)
	}
}

func TestNextDelegateGrantRoleNeverProducesRepoAdmin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current domainauth.RepoRole
		want    domainauth.RepoRole
	}{
		{"reader cycles to writer", domainauth.RepoRoleReader, domainauth.RepoRoleWriter},
		{"writer cycles to reader", domainauth.RepoRoleWriter, domainauth.RepoRoleReader},
		{"admin (unreachable) falls back to reader", domainauth.RepoRoleAdmin, domainauth.RepoRoleReader},
	}
	for _, tt := range tests {
		if got := nextDelegateGrantRole(tt.current); got != tt.want || got == domainauth.RepoRoleAdmin {
			t.Fatalf("%s: nextDelegateGrantRole(%q) = %q, want %q", tt.name, tt.current, got, tt.want)
		}
	}
}

// ---- revoke ----

func TestDelegateRevokeAsksThenDeletesOnceAndRefreshes(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("x"))
	if !delegateScreenOf(t, m).confirm.Active() {
		t.Fatal("x did not open the confirm")
	}
	out := delegateRender(m)
	for _, want := range []string{"Revoke access", "Revoke alice's Writer access to team/api?", "alice on team/api", "alice loses Writer access to team/api."} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Errorf("dialog lacks %q:\n%s", want, out)
		}
	}
	if _, _, deletes := client.counts(); deletes != 0 {
		t.Fatal("the DELETE ran before the confirmation")
	}
	first, cmd1 := m.Update(testKeyEnter())
	_, cmd2 := first.Update(testKeyEnter())
	if cmd1 == nil || cmd2 != nil {
		t.Fatalf("first enter command = %v, second = %v", cmd1 != nil, cmd2 != nil)
	}
	m = delegateRun(t, first.(Model), cmd1)
	if len(client.deletes) != 1 || client.deletes[0] != [2]string{"team/api", "alice"} {
		t.Fatalf("deletes = %v", client.deletes)
	}
	s := delegateScreenOf(t, m)
	if s.confirm.Active() || s.removing || !strings.Contains(s.status, "Revoked alice on team/api.") {
		t.Fatalf("after the revoke: confirm=%v removing=%v status=%q", s.confirm.Active(), s.removing, s.status)
	}
	if lists, _, _ := client.counts(); lists != 2 {
		t.Fatalf("lists = %d, want the open and the refresh", lists)
	}
}

func TestDelegateRevokeEscDoesNotDeleteAndErrorClosesThePrompt(t *testing.T) {
	t.Parallel()
	client := delegateClientFor()
	m := delegateModelAt(t, client, 150, 24)
	m = delegatePress(t, m, testKeyRunes("x"), testKeyEsc())
	if s := delegateScreenOf(t, m); s.confirm.Active() {
		t.Fatal("esc left the confirm open")
	}
	if _, _, deletes := client.counts(); deletes != 0 {
		t.Fatal("esc deleted")
	}
	client.delErr = errors.New("grant is protected")
	m = delegatePress(t, m, testKeyRunes("x"), testKeyEnter())
	s := delegateScreenOf(t, m)
	if s.confirm.Active() || s.removing || !strings.Contains(s.status, "Could not revoke alice: grant is protected") {
		t.Fatalf("confirm=%v removing=%v status=%q", s.confirm.Active(), s.removing, s.status)
	}
	if out := delegateRender(m); !strings.Contains(out, "grant is protected") {
		t.Fatalf("the error is not on screen:\n%s", out)
	}
	client.delErr = nil
	m = delegatePress(t, m, testKeyRunes("x"), testKeyEnter())
	if _, _, deletes := client.counts(); deletes != 2 {
		t.Fatalf("deletes = %d, want a working retry", deletes)
	}
}

func TestDelegateConfirmCapturesQAndLSoNothingQuitsOrLogsOut(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("x"))
	for _, k := range []keyMsg{testKeyRunes("q"), testKeyRunes("l"), testKeyRunes("n")} {
		updated, cmd := m.Update(k)
		if didQuit(cmd) {
			t.Fatalf("%q quit from the confirm", k.Text)
		}
		m = updated.(Model)
		if m.screen != screenRepoAdminGrants || !delegateScreenOf(t, m).confirm.Active() {
			t.Fatalf("%q left the confirm: screen=%q", k.Text, m.screen)
		}
	}
}

func TestDelegateFormCapturesLSoItNeverLogsOut(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"), testKeyRunes("l"))
	if m.screen != screenRepoAdminGrants || delegateScreenOf(t, m).form.username != "l" {
		t.Fatalf("screen=%q username=%q", m.screen, delegateScreenOf(t, m).form.username)
	}
}

// ---- generations, repositories and expiry ----

func TestDelegateDropsResultsOfAnotherInstanceOrRepository(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	s := delegateScreenOf(t, m)
	foreign := []tea.Msg{
		delegateLoadedMsg{gen: s.gen + 1, seq: s.seq, repo: s.repo, grants: []ports.AdminRepositoryGrant{{Username: "intruder", Role: domainauth.RepoRoleWriter}}},
		delegateLoadedMsg{gen: s.gen, seq: s.seq, repo: "team/other", grants: []ports.AdminRepositoryGrant{{Username: "mallory", Role: domainauth.RepoRoleReader}}},
		delegateLoadedMsg{gen: s.gen, seq: s.seq - 1, repo: s.repo, grants: []ports.AdminRepositoryGrant{{Username: "stale", Role: domainauth.RepoRoleReader}}},
		delegateSavedMsg{gen: s.gen + 1, repo: s.repo, username: "x", role: domainauth.RepoRoleReader},
		delegateSavedMsg{gen: s.gen, repo: "team/other", username: "x", role: domainauth.RepoRoleReader},
		delegateRemovedMsg{gen: s.gen + 1, repo: s.repo, username: "x"},
		delegateRemovedMsg{gen: s.gen, repo: "team/other", username: "x"},
	}
	for _, msg := range foreign {
		updated, cmd := m.Update(msg)
		if cmd != nil {
			t.Fatalf("%T started a command", msg)
		}
		got := delegateScreenOf(t, updated.(Model))
		if len(got.rows) != 6 || got.status != "" || got.loading || got.cursor != 0 {
			t.Fatalf("%T changed the screen: %+v", msg, got)
		}
		if out := delegateRender(updated.(Model)); strings.Contains(out, "intruder") || strings.Contains(out, "mallory") || strings.Contains(out, "stale") {
			t.Fatalf("%T reached the screen:\n%s", msg, out)
		}
	}
}

func TestDelegateAStaleExpiryNeverLogsTheOperatorOut(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	s := delegateScreenOf(t, m)
	expired := NewAdminSessionExpiredError("Session expired. Log in again.")
	updated, _ := m.Update(delegateLoadedMsg{gen: s.gen + 7, seq: s.seq, repo: s.repo, err: expired})
	if got := updated.(Model); got.screen != screenRepoAdminGrants || got.adminAuth != adminAuthStateAuthenticated {
		t.Fatalf("a foreign expiry logged out: screen=%q auth=%v", got.screen, got.adminAuth)
	}
}

func TestDelegateExpiryOfTheCurrentScreenRedirectsToLogin(t *testing.T) {
	t.Parallel()
	expired := NewAdminSessionExpiredError("Session expired. Log in again.")
	for name, build := range map[string]func(s delegateScreen) tea.Msg{
		"load": func(s delegateScreen) tea.Msg {
			return delegateLoadedMsg{gen: s.gen, seq: s.seq, repo: s.repo, err: expired}
		},
		"save": func(s delegateScreen) tea.Msg {
			return delegateSavedMsg{gen: s.gen, repo: s.repo, username: "x", err: expired}
		},
		"revoke": func(s delegateScreen) tea.Msg {
			return delegateRemovedMsg{gen: s.gen, repo: s.repo, username: "x", err: expired}
		},
	} {
		m := delegateModelAt(t, delegateClientFor(), 150, 24)
		updated, _ := m.Update(build(delegateScreenOf(t, m)))
		got := updated.(Model)
		if got.screen != screenAdminLogin || got.adminAuth != adminAuthStateExpired || got.adminScreens[slotDelegate] != nil {
			t.Fatalf("%s: screen=%q auth=%v slot=%T", name, got.screen, got.adminAuth, got.adminScreens[slotDelegate])
		}
		if !strings.Contains(got.status, "Session expired") {
			t.Fatalf("%s: status = %q", name, got.status)
		}
	}
}

// ---- geometry ----

func delegateStates(t *testing.T, w, h int) map[string]Model {
	t.Helper()
	failing := func(mutate func(*delegateClient)) *delegateClient {
		client := delegateClientFor()
		mutate(client)
		return client
	}
	long := strings.Repeat("a very long error ", 20)
	return map[string]Model{
		"normal":  delegateModelAt(t, delegateClientFor(), w, h),
		"empty":   delegateModelAt(t, failing(func(c *delegateClient) { c.grants["team/api"] = nil }), w, h),
		"loading": delegateLoadingModel(t, delegateClientFor(), w, h),
		"read error": delegateModelAt(t, failing(func(c *delegateClient) {
			c.listErr = errors.New(long)
		}), w, h),
		"200 grants, long and multibyte names": func() Model {
			m := delegateModelAt(t, delegateManyGrants(200), w, h)
			for range 150 {
				m = delegatePress(t, m, testKeyDown())
			}
			return m
		}(),
		"new grant form": delegatePress(t, delegateModelAt(t, delegateClientFor(), w, h), append([]keyMsg{testKeyRunes("n")}, delegateType("日本語のユーザー名-long-long-long-long-long-long-long-long-long-long")...)...),
		"form with an error": func() Model {
			m := delegateModelAt(t, failing(func(c *delegateClient) { c.putErr = errors.New(long) }), w, h)
			return delegatePress(t, m, append([]keyMsg{testKeyRunes("n")}, append(delegateType("zoe"), testKeyEnter())...)...)
		}(),
		"edit form": delegatePress(t, delegateModelAt(t, delegateClientFor(), w, h), testKeyDown(), testKeyRunes("e")),
		"revoke prompt": func() Model {
			return delegatePress(t, delegateModelAt(t, delegateManyGrants(200), w, h), testKeyRunes("x"))
		}(),
		"revoke error": func() Model {
			m := delegateModelAt(t, failing(func(c *delegateClient) { c.delErr = errors.New(long) }), w, h)
			return delegatePress(t, m, testKeyRunes("x"), testKeyEnter())
		}(),
	}
}

func TestDelegateGeometryAtEverySizeInEveryState(t *testing.T) {
	t.Parallel()
	for _, size := range delegateSizes {
		for name, m := range delegateStates(t, size.w, size.h) {
			label := fmt.Sprintf("%s at %dx%d", name, size.w, size.h)
			assertGrantNoOverflow(t, label, m.View().Content, size.w, size.h)
			if rows := len(strings.Split(strings.TrimRight(m.View().Content, "\n"), "\n")); rows != size.h {
				// The shell fills the screen: the view is exactly the terminal.
				t.Errorf("%s: %d rows, want exactly %d", label, rows, size.h)
			}
		}
	}
}

func TestDelegateSelectedRowStaysVisibleWhileScrolling(t *testing.T) {
	t.Parallel()
	for _, size := range delegateSizes {
		m := delegateModelAt(t, delegateManyGrants(200), size.w, size.h)
		for i := range 120 {
			m = delegatePress(t, m, testKeyDown())
			s := delegateScreenOf(t, m)
			row, _ := s.selected()
			prefix := []rune(row.Username)
			if len(prefix) > 6 {
				prefix = prefix[:6]
			}
			if out := delegateRender(m); !strings.Contains(out, "▸ "+string(prefix)) {
				t.Fatalf("%dx%d step %d: the selected row %q is not on screen:\n%s", size.w, size.h, i, row.Username, out)
			}
		}
	}
}

func TestDelegateFormFieldsAreVisibleAtTheSmallestSize(t *testing.T) {
	t.Parallel()
	m := delegateModelAt(t, delegateClientFor(), 150, 24)
	m = delegatePress(t, m, testKeyRunes("n"))
	out := delegateRender(m)
	for _, want := range []string{"NEW GRANT", "Username", "Role", "[Reader]", "Writer", "tab next field", "enter save", "esc cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("form lacks %q at 150x24:\n%s", want, out)
		}
	}
	m = delegatePress(t, m, testKeyEnter())
	if out := delegateRender(m); !strings.Contains(out, "Username is required.") {
		t.Fatalf("the form error is clipped at 150x24:\n%s", out)
	}
}

// ---- goldens ----

func assertDelegateGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "delegate", name+".golden")
	if *updateDelegateGoldens {
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
		t.Fatalf("read golden %s (run with -update-delegate): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-delegate to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestDelegateGoldens(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
		for name, m := range delegateStates(t, size.w, size.h) {
			slug := strings.NewReplacer(" ", "_", ",", "", "200_grants_long_and_multibyte_names", "many_rows").Replace(name)
			t.Run(fmt.Sprintf("%s_%dx%d", slug, size.w, size.h), func(t *testing.T) {
				assertDelegateGolden(t, fmt.Sprintf("%s_%dx%d", slug, size.w, size.h), delegateRender(m))
			})
		}
	}
}

func TestDelegateNewAndEditAreIgnoredWhileASaveOrRevokeIsInFlight(t *testing.T) {
	t.Parallel()
	for name, inFlight := range map[string]func(s *delegateScreen){
		"save":   func(s *delegateScreen) { s.saving = true },
		"revoke": func(s *delegateScreen) { s.removing = true },
	} {
		for _, k := range []string{"n", "e"} {
			m := delegateModelAt(t, delegateClientFor(), 150, 24)
			s := delegateScreenOf(t, m)
			inFlight(&s)
			m.adminScreens[slotDelegate] = s
			m = delegatePress(t, m, testKeyRunes(k))
			if got := delegateScreenOf(t, m); got.form.active || got.status == "" {
				t.Fatalf("%s then %s: form=%v status=%q, want the key ignored with a note", name, k, got.form.active, got.status)
			}
		}
	}
}

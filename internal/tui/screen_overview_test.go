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
	"regixtry/internal/ports"
)

// The Overview has its own flag so regenerating its goldens can never touch
// the characterization goldens (which use -update).
var updateOverviewGoldens = flag.Bool("update-overview", false, "rewrite the overview golden files")

var overviewViewSizes = []struct{ w, h int }{{150, 24}, {200, 40}}

// overviewClient serves the Overview's two reads: two robots (one without a
// grant) and three repositories, two of them with critical findings.
func overviewClient() *fakeAdminClient {
	return &fakeAdminClient{
		users: []ports.AdminUser{{ID: "u-1", Username: "alice", Enabled: true}},
		robots: []ports.AdminRobot{
			{ID: "r-1", Username: "ci-bot", Repository: "team/api", Enabled: true},
			{ID: "r-2", Username: "gh-actions", Enabled: true},
		},
		repositoryScanSummaries: []ports.RepositoryScanSummary{
			{Run: ports.ScanRun{Repository: "team/api", Critical: 3}, RunCount: 1},
			{Run: ports.ScanRun{Repository: "team/web", Critical: 1}, RunCount: 2},
			{Run: ports.ScanRun{Repository: "team/docs", High: 4}, RunCount: 1},
		},
	}
}

// overviewModelAt returns a model on the Overview at the given size, with an
// authenticated session and the screen's own loads already applied.
func overviewModelAt(t *testing.T, client AdminClient, w, h int) Model {
	t.Helper()
	m := newAdminReadyModelWithCatalog(t, []string{"team/api"}, client)
	m.now = func() time.Time { return charFixedTime }
	m = charResize(t, m, w, h)
	m.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: charFixedTime.Add(24 * time.Hour)}
	m.adminAuth = adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenAdminOverview})
	return runCmd(t, updated.(Model), cmd)
}

func overviewScreenOf(t *testing.T, m Model) overviewScreen {
	t.Helper()
	s, ok := m.adminScreens[slotOverview].(overviewScreen)
	if !ok {
		t.Fatalf("slot holds %T, want overviewScreen", m.adminScreens[slotOverview])
	}
	return s
}

func overviewPress(t *testing.T, m Model, keys ...keyMsg) Model {
	t.Helper()
	for _, k := range keys {
		updated, cmd := m.Update(k)
		m = runCmd(t, updated.(Model), cmd)
	}
	return m
}

func overviewRender(m Model) string { return ansi.Strip(m.View().Content) }

func TestOverviewSidebarItemsOpenTheirScreens(t *testing.T) {
	t.Parallel()
	wantScreens := map[shellNavID]screen{
		shellNavUsers:            screenAdminUsers,
		shellNavRobots:           screenAdminRobots,
		shellNavRepositoryAccess: screenAdminGrantAccess,
		shellNavImageScanning:    screenSecurityTrivy,
		shellNavSecretScanning:   screenSecurityGitleaksConfig,
		shellNavSigningPolicy:    screenSecuritySigningConfig,
		shellNavScanRuns:         screenAdminScanRuns,
		shellNavSecretFindings:   screenAdminSecretFindings,
		shellNavUpdateChannel:    screenAdminUpdateChannel,
	}
	for index, item := range shellNavItems() {
		if item.ID == shellNavOverview {
			continue
		}
		t.Run(string(item.ID), func(t *testing.T) {
			t.Parallel()
			m := overviewModelAt(t, overviewClient(), 150, 24)
			for range index {
				m = overviewPress(t, m, testKeyDown())
			}
			if got := overviewScreenOf(t, m).selectedID(); got != item.ID {
				t.Fatalf("cursor on %q, want %q", got, item.ID)
			}
			m = overviewPress(t, m, testKeyEnter())
			if m.screen != wantScreens[item.ID] {
				t.Fatalf("Enter on %q opened %q, want %q", item.Label, m.screen, wantScreens[item.ID])
			}
		})
	}
}

func TestOverviewEnterOnOverviewStaysPut(t *testing.T) {
	t.Parallel()
	m := overviewPress(t, overviewModelAt(t, overviewClient(), 150, 24), testKeyEnter())
	if m.screen != screenAdminOverview {
		t.Fatalf("screen = %q, want the Overview", m.screen)
	}
}

func TestOverviewRobotsItemLoadsTheRobots(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	m := overviewModelAt(t, client, 150, 24)
	before := client.listRobotsCalls
	m = overviewPress(t, m, testKeyDown(), testKeyDown(), testKeyEnter())
	if m.screen != screenAdminRobots || client.listRobotsCalls != before+1 || len(m.adminView.Robots) != 2 {
		t.Fatalf("screen=%q loads=%d robots=%d", m.screen, client.listRobotsCalls-before, len(m.adminView.Robots))
	}
}

func TestOverviewGOpensGrantAccessAndEscComesBack(t *testing.T) {
	t.Parallel()
	m := overviewPress(t, overviewModelAt(t, overviewClient(), 150, 24), testKeyRunes("g"))
	if m.screen != screenAdminGrantAccess {
		t.Fatalf("g opened %q", m.screen)
	}
	m = overviewPress(t, m, testKeyEsc())
	if m.screen != screenAdminOverview {
		t.Fatalf("Esc from Grant access went to %q, want the Overview", m.screen)
	}
}

func TestOverviewROpensCreateRobot(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewClient(), 150, 24)
	m.adminView.CreateRobotForm.Name = "left over"
	m = overviewPress(t, m, testKeyRunes("r"))
	if m.screen != screenAdminCreateRobot {
		t.Fatalf("r opened %q", m.screen)
	}
	if m.adminView.CreateRobotForm.Name != "" {
		t.Fatalf("the form kept %q from an earlier visit", m.adminView.CreateRobotForm.Name)
	}
}

func TestOverviewTOpensUsersWithAPointerToTheTokenKey(t *testing.T) {
	t.Parallel()
	m := overviewPress(t, overviewModelAt(t, overviewClient(), 150, 24), testKeyRunes("t"))
	if m.screen != screenAdminUsers {
		t.Fatalf("t opened %q", m.screen)
	}
	if m.status != "Pick a user, then press t to issue a token" {
		t.Fatalf("status = %q", m.status)
	}
}

func TestUsersListTIssuesATokenForTheSelectedUser(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	m := overviewModelAt(t, client, 150, 24)
	m.adminView.Users = client.users
	m.adminView.SelectedUser = 0
	m.adminView.SelectedUserID = "u-1"
	m.adminView.SelectedUsername = "alice"
	m = overviewPress(t, m, testKeyRunes("t"))
	if m.screen != screenAdminUsers {
		t.Fatalf("setup: screen = %q", m.screen)
	}
	m = overviewPress(t, m, testKeyRunes("t"))
	if m.screen != screenAdminEditUserTokens || m.adminView.SelectedUsername != "alice" {
		t.Fatalf("t on Users: screen=%q user=%q, want alice's tokens", m.screen, m.adminView.SelectedUsername)
	}
}

func TestOverviewCursorStopsAtBothEnds(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewClient(), 150, 24)
	m = overviewPress(t, m, testKeyUp())
	if got := overviewScreenOf(t, m).selectedID(); got != shellNavOverview {
		t.Fatalf("Up at the top moved to %q", got)
	}
	items := shellNavItems()
	for range len(items) + 3 {
		m = overviewPress(t, m, testKeyDown())
	}
	if got := overviewScreenOf(t, m).selectedID(); got != items[len(items)-1].ID {
		t.Fatalf("Down past the bottom reached %q, want the last item", got)
	}
}

func TestOverviewJAndKAreNotSpecial(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewClient(), 150, 24)
	m = overviewPress(t, m, testKeyRunes("j"), testKeyRunes("j"), testKeyRunes("k"))
	if s := overviewScreenOf(t, m); s.selected != 0 || m.screen != screenAdminOverview {
		t.Fatalf("j/k moved the cursor to %d on %q", s.selected, m.screen)
	}
}

func TestOverviewQAndCtrlCQuit(t *testing.T) {
	t.Parallel()
	for name, k := range map[string]keyMsg{"q": testKeyRunes("q"), "ctrl+c": testKeyCtrl('c')} {
		m := overviewModelAt(t, overviewClient(), 150, 24)
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%s returned no command", name)
		}
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Fatalf("%s did not quit", name)
		}
	}
}

func TestOverviewEscLeavesTheAdminPanel(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewClient(), 150, 24)
	m.adminReturn = screenRepositories
	m = overviewPress(t, m, testKeyEsc())
	if m.screen != screenRepositories {
		t.Fatalf("Esc went to %q, want the Console", m.screen)
	}
}

func TestOverviewNeedsAttentionListsRobotsWithoutGrants(t *testing.T) {
	t.Parallel()
	out := overviewRender(overviewModelAt(t, overviewClient(), 150, 24))
	if !strings.Contains(out, "Robot gh-actions has no repository access") || !strings.Contains(out, "g grant") {
		t.Errorf("the robot without a grant is not listed:\n%s", out)
	}
	if strings.Contains(out, "ci-bot") {
		t.Errorf("a robot with a grant is listed:\n%s", out)
	}
}

func TestOverviewNeedsAttentionCountsImagesWithCriticalFindings(t *testing.T) {
	t.Parallel()
	out := overviewRender(overviewModelAt(t, overviewClient(), 150, 24))
	if !strings.Contains(out, "2 images have critical findings") || !strings.Contains(out, "enter open") {
		t.Errorf("critical images not reported:\n%s", out)
	}
	client := overviewClient()
	client.repositoryScanSummaries = client.repositoryScanSummaries[:1]
	out = overviewRender(overviewModelAt(t, client, 150, 24))
	if !strings.Contains(out, "1 image has critical findings") {
		t.Errorf("the singular is wrong:\n%s", out)
	}
}

func TestOverviewNeedsAttentionEmptyState(t *testing.T) {
	t.Parallel()
	client := &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}
	out := overviewRender(overviewModelAt(t, client, 150, 24))
	if !strings.Contains(out, "Nothing needs attention.") {
		t.Errorf("no empty state:\n%s", out)
	}
}

func TestOverviewNeedsAttentionReportsAFailedRead(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	client.listRobotsErr = errors.New("robots exploded")
	out := overviewRender(overviewModelAt(t, client, 150, 24))
	if !strings.Contains(out, "Could not check robots: robots exploded") {
		t.Errorf("a failed robots read is hidden:\n%s", out)
	}
	if strings.Contains(out, "Nothing needs attention.") {
		t.Errorf("a failed read is shown as all clear:\n%s", out)
	}
	if !strings.Contains(out, "2 images have critical findings") {
		t.Errorf("the other read was dropped:\n%s", out)
	}
}

func TestOverviewNeedsAttentionClipsToTheRowBudget(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	client.robots = nil
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		client.robots = append(client.robots, ports.AdminRobot{ID: name, Username: "bot-" + name})
	}
	out := overviewRender(overviewModelAt(t, client, 150, 24))
	if rows := len(strings.Split(out, "\n")); rows != 24 {
		t.Fatalf("%d rows, want 24", rows)
	}
	if !strings.Contains(out, "more") || !strings.Contains(out, "2 images have critical findings") {
		t.Errorf("overflow is not summarised or the critical row was lost:\n%s", out)
	}
}

func TestOverviewComingBackReloadsNeedsAttention(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	m := overviewModelAt(t, client, 150, 24)
	if got := len(overviewScreenOf(t, m).robotsWithoutAccess); got != 1 {
		t.Fatalf("setup: %d robots without access", got)
	}
	m = overviewPress(t, m, testKeyRunes("g"))
	client.robots[1].Repository = "team/api" // the operator granted gh-actions
	m = overviewPress(t, m, testKeyEsc())
	if got := len(overviewScreenOf(t, m).robotsWithoutAccess); got != 0 {
		t.Fatalf("%d robots still listed after returning", got)
	}
}

func TestOverviewExpiredSessionInTheLoadReturnsToLogin(t *testing.T) {
	t.Parallel()
	client := overviewClient()
	client.listRobotsErr = AdminSessionExpiredError{Reason: AdminSessionExpiredReasonExpired}
	m := overviewModelAt(t, client, 150, 24)
	if m.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want the login", m.screen)
	}
}

func TestOverviewHintsOmitSearch(t *testing.T) {
	t.Parallel()
	hints := shortHelpView(newAdminTheme(), overviewKeys)
	for _, want := range []string{"↑↓: move", "enter: open", "g: grant access", "r: robot", "t: token", "esc: back", "q: quit"} {
		if !strings.Contains(hints, want) {
			t.Errorf("hints %q lack %q", hints, want)
		}
	}
	if strings.Contains(hints, "search") || strings.Contains(hints, "/") {
		t.Errorf("hints advertise search: %q", hints)
	}
}

func TestOverviewFitsEveryTerminalSize(t *testing.T) {
	t.Parallel()
	for _, size := range append([]struct{ w, h int }{{151, 25}, {160, 30}, {220, 60}}, overviewViewSizes...) {
		lines := strings.Split(overviewRender(overviewModelAt(t, overviewClient(), size.w, size.h)), "\n")
		if len(lines) > size.h {
			t.Errorf("%dx%d: %d rows", size.w, size.h, len(lines))
		}
		for i, line := range lines {
			if got := ansi.StringWidth(line); got > size.w {
				t.Errorf("%dx%d row %d is %d columns wide", size.w, size.h, i, got)
			}
		}
	}
}

func TestOverviewShowsTheOperatorAndBreadcrumb(t *testing.T) {
	t.Parallel()
	out := overviewRender(overviewModelAt(t, overviewClient(), 150, 24))
	first := strings.Split(out, "\n")[0]
	if !strings.Contains(first, "Admin › Overview") || !strings.Contains(first, "signed in as operator") {
		t.Errorf("header = %q", first)
	}
	for _, want := range []string{"Overview", "The three things you do most are one key away from anywhere.",
		"Grant repository access", "Create robot", "Issue token", "NEEDS ATTENTION"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	if strings.Contains(out, "RECENT CHANGES") {
		t.Errorf("view shows a Recent changes panel")
	}
}

func assertOverviewGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "overview", name+".golden")
	if *updateOverviewGoldens {
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
		t.Fatalf("read golden %s (run with -update-overview): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("rendered view differs from %s (run with -update-overview to inspect)\n--- want\n%s\n--- got\n%s", path, strings.TrimRight(string(want), "\n"), strings.TrimRight(got, "\n"))
	}
}

func TestOverviewViewGoldens(t *testing.T) {
	cases := []struct {
		name   string
		client func() *fakeAdminClient
	}{
		{"overview_attention", overviewClient},
		{"overview_empty", func() *fakeAdminClient {
			return &fakeAdminClient{repositoryScanSummaries: []ports.RepositoryScanSummary{}}
		}},
	}
	for _, tc := range cases {
		for _, size := range overviewViewSizes {
			name := fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h)
			t.Run(name, func(t *testing.T) {
				m := overviewModelAt(t, tc.client(), size.w, size.h)
				assertOverviewGolden(t, name, overviewRender(m))
			})
		}
	}
}

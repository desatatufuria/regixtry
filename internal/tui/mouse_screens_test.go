package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"regixtry/internal/ports"
)

// mouseCase describes one list screen for the shared mouse tests: how to put
// it on show, the texts of its rows in display order, and how to read which
// row is selected and whether Enter opened something.
type mouseCase struct {
	name  string
	build func(t *testing.T, w, h int) Model
	// rows are texts that identify one row each, unique inside the list area.
	rows []string
	// listWidth is the number of cells, counted from the content's left
	// padding, that hold the list (the detail panel to its right is excluded).
	listWidth func(w int) int
	// noSidebar marks a screen drawn without the admin sidebar.
	noSidebar bool
	selected  func(Model) string
	// opens reports that a click on the selected row opened something; nil
	// means Enter does nothing harmless to issue and the click must not act.
	opens func(Model) bool
	// block puts the screen in a state where the mouse must do nothing.
	block func(t *testing.T, m Model) Model
}

// cellInList finds the cell where text starts inside the list area of the
// rendered Model view, skipping the header and footer bars.
func cellInList(m Model, c mouseCase, text string) (x, y int, ok bool) {
	left, pad := shellSidebarWidth, shellContentPadX
	if c.noSidebar {
		left, pad = 0, 0 // the console's listWidth already counts its own padding
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for row := shellHeaderRows; row < len(lines)-shellFooterRows; row++ {
		area := ansi.Cut(lines[row], left, left+pad+c.listWidth(m.viewport.Width))
		if i := strings.Index(area, text); i >= 0 {
			return left + lipgloss.Width(area[:i]), row, true
		}
	}
	return 0, 0, false
}

func mouseSizes() []struct{ w, h int } { return geometrySizes }

func eachMouseSize(t *testing.T, c mouseCase, run func(t *testing.T, m Model)) {
	t.Helper()
	for _, size := range mouseSizes() {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) { run(t, c.build(t, size.w, size.h)) })
	}
}

var mouseCases = []mouseCase{
	usersMouseCase(),
	robotsMouseCase(),
	repoAccessMouseCase(),
	projectsMouseCase(),
	projectMembersMouseCase(),
	scanRunsMouseCase(),
	secretFindingsMouseCase(),
	scanHistoryMouseCase(),
	delegateMouseCase(),
	tokensMouseCase(),
	consoleRepositoriesMouseCase(),
	consoleProjectsMouseCase(),
	consoleTagsMouseCase(),
	overridesMouseCase("image scanning repositories", screenSecurityTrivyRepos, true),
	overridesMouseCase("secret scanning repositories", screenSecurityGitleaksRepos, false),
	overridesMouseCase("signing repositories", screenSecuritySigningRepos, false),
}

func TestMouseClickSelectsTheRowDrawnUnderIt(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		t.Run(c.name, func(t *testing.T) {
			eachMouseSize(t, c, func(t *testing.T, m Model) {
				clicked := 0
				for _, row := range c.rows {
					if c.selected(m) == row {
						continue // a click on the selected row is Enter: tested apart
					}
					x, y, ok := cellInList(m, c, row)
					if !ok {
						continue // scrolled out of the window
					}
					m = sendMouse(t, m, mouseClickAt(x, y))
					if got := c.selected(m); got != row {
						t.Fatalf("click on %q at (%d,%d) selected %q", row, x, y, got)
					}
					clicked++
				}
				if clicked < 2 {
					t.Fatalf("only %d rows could be clicked", clicked)
				}
			})
		})
	}
}

func TestMouseWheelMovesTheSelectionOneRowAtATime(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t, 150, 24)
			first := c.selected(m)
			m = sendMouse(t, m, mouseWheel(tea.MouseWheelDown))
			second := c.selected(m)
			if second == first {
				t.Fatalf("wheel down left the selection on %q", first)
			}
			m = sendMouse(t, m, mouseWheel(tea.MouseWheelUp))
			if got := c.selected(m); got != first {
				t.Fatalf("wheel up went to %q, want %q", got, first)
			}
			m = sendMouse(t, m, mouseWheel(tea.MouseWheelUp))
			if got := c.selected(m); got != first {
				t.Fatalf("wheel up at the top moved to %q", got)
			}
		})
	}
}

func TestMouseSecondClickOnTheSelectedRowOpensItOnlyWhereEnterIsHarmless(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t, 150, 24)
			row := c.selected(m)
			x, y, ok := cellInList(m, c, row)
			if !ok {
				t.Fatalf("selected row %q is not on screen", row)
			}
			after := sendMouse(t, m, mouseClickAt(x, y))
			if c.opens == nil {
				if after.screen != m.screen || c.selected(after) != row {
					t.Fatalf("the click acted: screen %s selected %q", after.screen, c.selected(after))
				}
				return
			}
			if !c.opens(after) {
				t.Fatalf("a click on the selected row opened nothing (screen %s)", after.screen)
			}
		})
	}
}

func TestMouseDoesNothingWhileADialogOrTextInputIsInFront(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		if c.block == nil {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			m := c.block(t, c.build(t, 150, 24))
			before := ansi.Strip(m.View().Content)
			for _, msg := range []tea.Msg{
				mouseWheel(tea.MouseWheelDown), mouseWheel(tea.MouseWheelUp),
				mouseClickAt(40, 8), mouseClickAt(40, 9), mouseClickAt(40, 12), mouseClickAt(2, 6),
			} {
				updated, cmd := m.Update(msg)
				if cmd != nil {
					t.Fatalf("%T returned a command while blocked", msg)
				}
				m = updated.(Model)
			}
			if after := ansi.Strip(m.View().Content); after != before {
				t.Fatalf("the view changed while blocked:\n%s\n---\n%s", before, after)
			}
		})
	}
}

func TestMouseDelugeNeverQueuesMoreCommandsThanKeysWould(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		t.Run(c.name, func(t *testing.T) {
			mouse, keys := c.build(t, 150, 24), c.build(t, 150, 24)
			mouseCmds, keyCmds := 0, 0
			for i := range 200 {
				button, key := tea.MouseWheelDown, testKeyDown()
				if i%3 == 2 {
					button, key = tea.MouseWheelUp, testKeyUp()
				}
				updated, cmd := mouse.Update(mouseWheel(button))
				mouse = updated.(Model)
				if cmd != nil {
					mouseCmds++
				}
				updated, cmd = keys.Update(key)
				keys = updated.(Model)
				if cmd != nil {
					keyCmds++
				}
			}
			if mouseCmds > keyCmds || c.selected(mouse) != c.selected(keys) {
				t.Fatalf("200 wheel events: %d commands, selected %q; the same arrows: %d commands, selected %q",
					mouseCmds, c.selected(mouse), keyCmds, c.selected(keys))
			}
		})
	}
}

func TestMouseClickOutsideTheRowsDoesNothing(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t, 150, 24)
			selected := c.selected(m)
			for _, at := range [][2]int{{0, 0}, {149, 0}, {60, 1}, {149, 23}, {60, 23}, {200, 200}, {-1, -1}} {
				updated, cmd := m.Update(mouseClickAt(at[0], at[1]))
				if cmd != nil || c.selected(updated.(Model)) != selected || updated.(Model).screen != m.screen {
					t.Fatalf("a click at %v acted", at)
				}
			}
		})
	}
}

func usersMouseCase() mouseCase {
	return mouseCase{
		name: "users",
		build: func(t *testing.T, w, h int) Model {
			return usersModelAt(t, usersTestClient(), w, h)
		},
		rows:      []string{"admin", "alice", "bob", "carol", "dave"},
		listWidth: func(w int) int { return w - shellSidebarWidth - 2*shellContentPadX - usersDetailWidth - usersColumnGap },
		selected: func(m Model) string {
			user, _ := m.adminScreens[slotUsers].(usersScreen).current()
			return user.Username
		},
		opens: func(m Model) bool { return m.screen == screenAdminEditUser },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("/")) },
	}
}

func robotsMouseCase() mouseCase {
	return mouseCase{
		name: "robots",
		build: func(t *testing.T, w, h int) Model {
			client := newAccessClient()
			client.robots = sampleRobots()
			client.tokens = sampleRobotTokens()
			return openSidebarItem(t, overviewModelAt(t, client, w, h), shellNavRobots)
		},
		rows:      []string{"ci-publisher", "gh-actions", "staging-deploy", "old-deploy"},
		listWidth: func(w int) int { return w - shellSidebarWidth - 2*shellContentPadX - robotsPanelWidth - robotsPanelGap },
		selected: func(m Model) string {
			robot, _ := m.adminScreens[slotRobots].(robotsScreen).selectedRobot()
			return robot.Username
		},
		opens: func(m Model) bool { return m.screen == screenAdminEditUserTokens },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("x")) },
	}
}

func repoAccessMouseCase() mouseCase {
	return mouseCase{
		name: "repository access",
		build: func(t *testing.T, w, h int) Model {
			return repoAccessModelAt(t, repoAccessClient(), w, h)
		},
		rows:      []string{"bob", "carol", "ci-publisher", "old-deploy"},
		listWidth: func(w int) int { return w - shellSidebarWidth - 2*shellContentPadX - repoAccessPanelWidth - 2 },
		selected: func(m Model) string {
			s := m.adminScreens[slotRepositoryAccess].(repoAccessScreen)
			row, _ := s.selected(s.visibleRows())
			return row.username
		},
		opens: func(m Model) bool { return m.screen != screenAdminRepositoryAccess },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("/")) },
	}
}

func scanListWidth(w int) int {
	bodyW := w - shellSidebarWidth - 2*shellContentPadX
	return bodyW - min(scanPanelMaxWidth, max(bodyW/3, 1)) - scanColGap
}

func scanRunsMouseCase() mouseCase {
	return mouseCase{
		name: "scan runs",
		build: func(t *testing.T, w, h int) Model {
			client := scanFixtureClient()
			client.scanRuns = nil // one row per repository: the summaries' runs
			return scanModelAt(t, client, screenAdminScanRuns, w, h)
		},
		rows:      []string{"team/api", "team/web", "team/docs"},
		listWidth: scanListWidth,
		selected: func(m Model) string {
			repository, _ := m.adminScreens[slotScanRuns].(scanRunsScreen).selectedRepository()
			return repository
		},
		opens: func(m Model) bool { return m.adminScreens[slotScanHistory] != nil },
	}
}

func secretFindingsMouseCase() mouseCase {
	return mouseCase{
		name: "secret findings",
		build: func(t *testing.T, w, h int) Model {
			return scanModelAt(t, scanFixtureClient(), screenAdminSecretFindings, w, h)
		},
		rows:      []string{"aws-access-key", "generic-api-key", "slack-webhook-url"},
		listWidth: scanListWidth,
		selected: func(m Model) string {
			s := m.adminScreens[slotSecretFindings].(scanRunsScreen)
			rows := s.secretRows()
			return rows[boundedIndex(s.cursor, len(rows))].finding.RuleID
		},
		opens: func(m Model) bool { return m.adminScreens[slotScanHistory] != nil },
	}
}

func scanHistoryMouseCase() mouseCase {
	return mouseCase{
		name: "scan history",
		build: func(t *testing.T, w, h int) Model {
			m := scanModelAt(t, scanFixtureClient(), screenAdminScanRuns, w, h)
			return scanPress(t, m, testKeyEnter())
		},
		rows:      []string{"CVE-2026-1000", "CVE-2026-1001", "CVE-2026-1002"},
		listWidth: scanListWidth,
		selected: func(m Model) string {
			s := m.adminScreens[slotScanHistory].(scanHistoryScreen)
			return s.modal.Detail.Findings[boundedIndex(s.modal.FindingCursor, len(s.modal.Detail.Findings))].VulnerabilityID
		},
	}
}

func overridesMouseCase(name string, to screen, opensHistory bool) mouseCase {
	c := mouseCase{
		name: name,
		build: func(t *testing.T, w, h int) Model {
			return ovModelAt(t, ovClient(), securityKnown, to, w, h)
		},
		rows:      []string{"team/api", "team/web", "team/docs", "team/infra"},
		listWidth: scanListWidth,
		selected: func(m Model) string {
			var list overridesList
			switch screens := m.adminScreens; to {
			case screenSecurityTrivyRepos:
				list = screens[slotTrivyRepos].(trivyReposScreen).overridesList
			case screenSecurityGitleaksRepos:
				list = screens[slotGitleaksRepos].(featureOverridesScreen).overridesList
			default:
				list = screens[slotSigningRepos].(featureOverridesScreen).overridesList
			}
			row, _ := list.selectedRow()
			return row.Repository
		},
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("o")) },
	}
	if opensHistory {
		c.opens = func(m Model) bool { return m.adminScreens[slotScanHistory] != nil }
	}
	return c
}

func delegateMouseCase() mouseCase {
	return mouseCase{
		name: "delegate grants",
		build: func(t *testing.T, w, h int) Model {
			return delegateModelAt(t, delegateClientFor(), w, h)
		},
		rows:      []string{"alice", "bob", "carol", "ci-publisher", "dave", "erin"},
		listWidth: func(w int) int { return w - shellSidebarWidth - 2*shellContentPadX - delegatePanelWidth - 2 },
		selected: func(m Model) string {
			s := m.adminScreens[slotDelegate].(delegateScreen)
			return s.rows[boundedIndex(s.cursor, len(s.rows))].Username
		},
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("n")) },
	}
}

func consoleListWidth(w int) int { return consolePadX + w - 2*consolePadX }

func consoleMouseModel(t *testing.T, id screen, built adminScreen, w, h int) Model {
	t.Helper()
	m := NewModel(newConsoleService(), WithAdminClient(&fakeAdminClient{}))
	m.now = func() time.Time { return consoleTime }
	slot, _ := slotFor(id)
	m.adminScreens[slot] = built
	m.screen = id
	return resizeModel(t, m, w, h)
}

func consoleRepositoriesMouseCase() mouseCase {
	return mouseCase{
		name: "console repositories",
		build: func(t *testing.T, w, h int) Model {
			return consoleMouseModel(t, screenRepositories, reposScreen(t, consoleRepos("library/alpine", "team/api", "team/web", "ops/x", "flat")), w, h)
		},
		rows:      []string{"library/alpine", "team/api", "team/web", "ops/x", "flat"},
		listWidth: consoleListWidth,
		noSidebar: true,
		selected: func(m Model) string {
			repository, _ := m.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen).selectedRepository()
			return repository.Name
		},
		opens: func(m Model) bool { return m.screen == screenTags },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("d")) },
	}
}

func consoleProjectsMouseCase() mouseCase {
	return mouseCase{
		name: "console projects",
		build: func(t *testing.T, w, h int) Model {
			return consoleMouseModel(t, screenProjects, newConsoleProjectsScreen(consoleRepos("library/alpine", "team/api", "team/web", "ops/x", "flat")), w, h)
		},
		rows:      []string{"flat", "library", "ops", "team"},
		listWidth: consoleListWidth,
		noSidebar: true,
		selected: func(m Model) string {
			project, _ := m.adminScreens[slotConsoleProjects].(consoleProjectsScreen).selectedProject()
			return project.Name
		},
		opens: func(m Model) bool { return m.screen == screenRepositories },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("d")) },
	}
}

func consoleTagsMouseCase() mouseCase {
	return mouseCase{
		name: "console tags",
		build: func(t *testing.T, w, h int) Model {
			return consoleMouseModel(t, screenTags, tagsScreenWith(tagsFixture(), sortByNameAsc), w, h)
		},
		rows:      []string{"0.9", "1.0", "latest"},
		listWidth: consoleListWidth,
		noSidebar: true,
		selected: func(m Model) string {
			tag, _ := m.adminScreens[slotConsoleTags].(consoleTagsScreen).selectedTag()
			return tag.Name
		},
		opens: func(m Model) bool { return m.screen != screenTags },
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("d")) },
	}
}

func tokensMouseCase() mouseCase {
	return mouseCase{
		name: "token cards",
		build: func(t *testing.T, w, h int) Model {
			m, _ := accessModel(t, func(c *accessClient) {
				c.tokens["r-ci"] = []ports.AdminToken{
					accessToken("alpha-deploy", "act_aaaa00000001", accessNow.Add(-24*time.Hour), accessNow.Add(24*time.Hour)),
					accessToken("beta-deploy", "act_bbbb00000002", accessNow.Add(-24*time.Hour), accessNow.Add(48*time.Hour)),
					accessToken("gamma-deploy", "act_cccc00000003", accessNow.Add(-24*time.Hour), accessNow.Add(72*time.Hour)),
				}
			})
			m = resizeModel(t, m, w, h)
			return overviewPress(t, openRobotsOf(t, m), testKeyRunes("t"))
		},
		rows: []string{"alpha-deploy", "beta-deploy", "gamma-deploy"},
		listWidth: func(w int) int {
			leftW, _ := tokensWidths(w - shellSidebarWidth - 2*shellContentPadX)
			return leftW
		},
		selected: func(m Model) string {
			s := m.adminScreens[slotTokens].(tokensScreen)
			return s.tokens[boundedIndex(s.selected, len(s.tokens))].Name
		},
		block: func(t *testing.T, m Model) Model { return testSendKey(t, m, testKeyRunes("n")) },
	}
}

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sidebarCell finds a sidebar item's label in the rendered view, looking only
// at the sidebar's columns.
func sidebarCell(t *testing.T, m Model, label string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if row < shellHeaderRows {
			continue
		}
		area := ansi.Cut(line, 0, shellSidebarWidth-1)
		if i := strings.Index(area, label); i >= 0 {
			return i, row
		}
	}
	t.Fatalf("sidebar item %q is not drawn:\n%s", label, ansi.Strip(m.View().Content))
	return 0, 0
}

var sidebarTargets = []struct {
	label string
	to    screen
}{
	{"Users", screenAdminUsers},
	{"Robots", screenAdminRobots},
	{"Repository access", screenAdminRepositoryAccess},
	{"Image scanning", screenSecurityTrivy},
	{"Secret scanning", screenSecurityGitleaksConfig},
	{"Signing policy", screenSecuritySigningConfig},
	{"Scan runs", screenAdminScanRuns},
	{"Secret findings", screenAdminSecretFindings},
	{"Update channel", screenAdminUpdateChannel},
}

func TestMouseClickOnASidebarItemOpensItsScreenFromTheOverviewAtEverySize(t *testing.T) {
	t.Parallel()
	for _, size := range geometrySizes {
		for _, target := range sidebarTargets {
			t.Run(sizeLabel(size.w, size.h)+"/"+target.label, func(t *testing.T) {
				m := overviewModelAt(t, overviewClient(), size.w, size.h)
				x, y := sidebarCell(t, m, target.label)
				m = sendMouse(t, m, mouseClickAt(x, y))
				if m.screen != target.to {
					t.Fatalf("click on %q at (%d,%d) opened %q, want %q", target.label, x, y, m.screen, target.to)
				}
			})
		}
	}
}

func TestMouseSidebarOpensTheSameScreenAsEnterOnTheOverview(t *testing.T) {
	t.Parallel()
	for _, target := range sidebarTargets {
		id := map[string]shellNavID{
			"Users": shellNavUsers, "Robots": shellNavRobots, "Repository access": shellNavRepositoryAccess,
			"Image scanning": shellNavImageScanning, "Secret scanning": shellNavSecretScanning, "Signing policy": shellNavSigningPolicy,
			"Scan runs": shellNavScanRuns, "Secret findings": shellNavSecretFindings, "Update channel": shellNavUpdateChannel,
		}[target.label]
		byKeys := openSidebarItem(t, overviewModelAt(t, overviewClient(), 150, 24), id)
		byMouse := overviewModelAt(t, overviewClient(), 150, 24)
		x, y := sidebarCell(t, byMouse, target.label)
		byMouse = sendMouse(t, byMouse, mouseClickAt(x, y))
		if byKeys.screen != byMouse.screen || ansi.Strip(byKeys.View().Content) != ansi.Strip(byMouse.View().Content) {
			t.Fatalf("%s: mouse and Enter ended on different screens (%s, %s)", target.label, byMouse.screen, byKeys.screen)
		}
	}
}

func TestMouseOverviewClickOnTheOverviewItemIsAHarmlessNoOp(t *testing.T) {
	t.Parallel()
	m := overviewModelAt(t, overviewClient(), 150, 24)
	x, y := sidebarCell(t, m, "Overview")
	before := ansi.Strip(m.View().Content)
	after := sendMouse(t, m, mouseClickAt(x, y))
	if after.screen != screenAdminOverview || ansi.Strip(after.View().Content) != before {
		t.Fatalf("click on the active Overview item changed the screen to %q", after.screen)
	}
}

func TestMouseSidebarNavigatesBetweenListScreens(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		if c.noSidebar || c.name == "delegate grants" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			m := c.build(t, 150, 24)
			from := m.screen
			target := "Users"
			if from == screenAdminUsers {
				target = "Robots"
			}
			x, y := sidebarCell(t, m, target)
			after := sendMouse(t, m, mouseClickAt(x, y))
			want := screenAdminUsers
			if target == "Robots" {
				want = screenAdminRobots
			}
			if m.adminScreens[slotScanHistory] != nil {
				want = from // the history floating over a list keeps the mouse
			}
			if after.screen != want {
				t.Fatalf("from %s a click on %q reached %s, want %s", from, target, after.screen, want)
			}
		})
	}
}

func TestMouseSidebarDoesNothingWhileADialogFormOrInFlightCommandBlocksTheScreen(t *testing.T) {
	t.Parallel()
	for _, c := range mouseCases {
		if c.block == nil || c.noSidebar || c.name == "delegate grants" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			m := c.block(t, c.build(t, 150, 24))
			from := m.screen
			x, y := sidebarCell(t, m, "Update channel")
			updated, cmd := m.Update(mouseClickAt(x, y))
			if cmd != nil || updated.(Model).screen != from {
				t.Fatalf("a sidebar click acted while %s was blocked", from)
			}
		})
	}
}

func TestMouseSidebarDoesNothingOnFormsAndOnConsoleScreens(t *testing.T) {
	t.Parallel()
	forms := map[string]func(*Model){
		"new user":       func(m *Model) { mountCreateUser(m, createUserFieldUsername) },
		"reset password": mountResetPassword,
		"new robot":      mountCreateRobotForTest,
		"tokens":         mountTokensForTest,
	}
	for name, mount := range forms {
		t.Run(name, func(t *testing.T) {
			m := overviewModelAt(t, overviewClient(), 150, 24)
			mount(&m)
			from := m.screen
			x, y := sidebarCell(t, m, "Users")
			updated, cmd := m.Update(mouseClickAt(x, y))
			if cmd != nil || updated.(Model).screen != from {
				t.Fatalf("a sidebar click left the %s form", name)
			}
		})
	}
	console := consoleRepositoriesMouseCase().build(t, 150, 24)
	for _, at := range [][2]int{{1, 6}, {2, 8}, {3, 12}} {
		updated, cmd := console.Update(mouseClickAt(at[0], at[1]))
		if cmd != nil || updated.(Model).screen != screenRepositories {
			t.Fatalf("a click in the console's left margin at %v acted", at)
		}
	}
}

func TestMouseOverviewAttentionRowsFocusThenOpen(t *testing.T) {
	t.Parallel()
	rows := []string{"Tokens expiring soon", "Robot gh-actions has no repository access", "images have critical findings"}
	for _, size := range geometrySizes {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) {
			m := overviewModelAt(t, overviewRichClient(), size.w, size.h)
			for want := len(rows) - 1; want >= 0; want-- {
				x, y, ok := cellInList(m, mouseCase{listWidth: func(w int) int { return w / 2 }}, rows[want])
				if !ok {
					t.Fatalf("row %q is not on screen", rows[want])
				}
				m = sendMouse(t, m, mouseClickAt(x, y))
				if got := overviewScreenOf(t, m).attentionFocus; got != want+1 {
					t.Fatalf("click on %q focused %d, want %d", rows[want], got, want+1)
				}
			}
		})
	}
	m := overviewModelAt(t, overviewRichClient(), 150, 24)
	x, y, _ := cellInList(m, mouseCase{listWidth: func(w int) int { return w / 2 }}, rows[2])
	m = sendMouse(t, m, mouseClickAt(x, y))
	m = sendMouse(t, m, mouseClickAt(x, y))
	if m.screen != screenSecurityTrivy {
		t.Fatalf("a second click on the focused row opened %q, want the image scanning screen", m.screen)
	}
}

func TestMouseOverviewWheelMovesTheSidebarCursorLikeTheArrows(t *testing.T) {
	t.Parallel()
	byKeys := overviewPress(t, overviewModelAt(t, overviewClient(), 150, 24), testKeyDown(), testKeyDown(), testKeyUp())
	byMouse := overviewModelAt(t, overviewClient(), 150, 24)
	byMouse = sendMouse(t, byMouse, mouseWheel(5)) // MouseWheelDown
	byMouse = sendMouse(t, byMouse, mouseWheel(5))
	byMouse = sendMouse(t, byMouse, mouseWheel(4)) // MouseWheelUp
	if overviewScreenOf(t, byKeys).selected != overviewScreenOf(t, byMouse).selected {
		t.Fatalf("wheel and arrows ended on different rows")
	}
}

func TestMouseRepositoryAdministratorsSidebarNeverLeavesTheirScreen(t *testing.T) {
	t.Parallel()
	m := delegateMouseCase().build(t, 150, 24)
	x, y := sidebarCell(t, m, "Repository access")
	updated, cmd := m.Update(mouseClickAt(x, y))
	if cmd != nil || updated.(Model).screen != screenRepoAdminGrants {
		t.Fatalf("a click on the repository administrator's sidebar left the screen (%s)", updated.(Model).screen)
	}
}

func TestMouseSidebarWorksFromTheConfigurationScreensAndNeverClicksTheirButtons(t *testing.T) {
	t.Parallel()
	for _, to := range []screen{screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminUpdateChannel} {
		t.Run(string(to), func(t *testing.T) {
			m := overviewModelAt(t, overviewClient(), 150, 24)
			updated, cmd := m.Update(navigateMsg{To: to})
			m = runCmd(t, updated.(Model), cmd)
			// Clicking in the body acts on nothing.
			before := ansi.Strip(m.View().Content)
			for _, at := range [][2]int{{60, 8}, {60, 12}, {100, 14}, {140, 20}} {
				m2, cmd := m.Update(mouseClickAt(at[0], at[1]))
				if cmd != nil || ansi.Strip(m2.(Model).View().Content) != before {
					t.Fatalf("a click in the body at %v acted", at)
				}
			}
			x, y := sidebarCell(t, m, "Users")
			m = sendMouse(t, m, mouseClickAt(x, y))
			if m.screen != screenAdminUsers {
				t.Fatalf("sidebar click opened %q", m.screen)
			}
		})
	}
}

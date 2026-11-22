package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testKeyF2() keyMsg { return keyMsg{Code: tea.KeyF2} }

func mouseClickAt(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func mouseWheel(button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: 60, Y: 8, Button: button}
}

// sendMouse delivers one mouse message to the Model and runs the command it
// returns (and the ones that follow), like the program loop would.
func sendMouse(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, cmd := m.Update(msg)
	return runCmd(t, updated.(Model), cmd)
}

func lastRow(m Model) string {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	return lines[len(lines)-1]
}

func TestMouseIsOnByDefaultAndTheViewRequestsIt(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want cell motion by default", got)
	}
	if !strings.Contains(lastRow(m), "F2 mouse on") {
		t.Fatalf("footer does not show the mouse switch: %q", lastRow(m))
	}
}

func TestF2TurnsTheMouseOffAndOnAgain(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = testSendKey(t, m, testKeyF2())
	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("mouse mode after F2 = %v, want none so the terminal can select text", got)
	}
	if row := lastRow(m); !strings.Contains(row, "F2 mouse off") || strings.Contains(row, "mouse on") {
		t.Fatalf("footer after F2: %q", row)
	}
	m = testSendKey(t, m, testKeyF2())
	if m.View().MouseMode != tea.MouseModeCellMotion || !strings.Contains(lastRow(m), "F2 mouse on") {
		t.Fatalf("second F2 did not turn the mouse back on: %q", lastRow(m))
	}
}

func TestMouseSwitchSurvivesNavigationAndWorksOnEveryScreen(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = testSendKey(t, m, testKeyF2())
	for _, to := range []screen{screenAdminOverview, screenAdminRobots, screenAdminUsers, screenSecurityTrivy, screenAdminScanRuns} {
		updated, cmd := m.Update(navigateMsg{To: to})
		m = runCmd(t, updated.(Model), cmd)
		if m.screen != to || m.View().MouseMode != tea.MouseModeNone {
			t.Fatalf("on %s the mouse switch was lost (mode %v)", to, m.View().MouseMode)
		}
		before := m.mouseDisabled
		m = testSendKey(t, m, testKeyF2())
		m = testSendKey(t, m, testKeyF2())
		if m.mouseDisabled != before || m.screen != to {
			t.Fatalf("F2 twice on %s changed the switch or the screen", to)
		}
	}
}

func TestF2WorksOnTheLoginScreenAndInsideADialog(t *testing.T) {
	t.Parallel()
	login := usersModelAt(t, usersTestClient(), 150, 24)
	login.screen = screenAdminLogin
	login = testSendKey(t, login, testKeyF2())
	if !login.mouseDisabled || login.screen != screenAdminLogin || login.adminLogin.Username != "" {
		t.Fatalf("F2 on the login screen: disabled=%v screen=%s typed=%q", login.mouseDisabled, login.screen, login.adminLogin.Username)
	}

	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = testSendKey(t, m, testKeyRunes("d"))
	if !usersScreenOf(t, m).confirm.Active() {
		t.Fatal("d opened no dialog")
	}
	m = testSendKey(t, m, testKeyF2())
	if !m.mouseDisabled || !usersScreenOf(t, m).confirm.Active() {
		t.Fatal("F2 in a dialog must toggle the mouse and leave the dialog open")
	}
}

func TestF2IsNeverTypedIntoATextField(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	m = testSendKey(t, m, testKeyRunes("/"))
	m = testSendKey(t, m, testKeyF2())
	if got := usersScreenOf(t, m).query; got != "" || !m.mouseDisabled {
		t.Fatalf("F2 in the search box: query %q, disabled %v", got, m.mouseDisabled)
	}
}

func TestMouseIsIgnoredWhileTheTerminalIsTooSmall(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	small := resizeModel(t, m, 60, 10)
	before := usersScreenOf(t, small)
	out := sendMouse(t, small, mouseWheel(tea.MouseWheelDown))
	out = sendMouse(t, out, mouseClickAt(40, 8))
	if after := usersScreenOf(t, out); after.selectedID != before.selectedID || out.screen != screenAdminUsers {
		t.Fatalf("the mouse acted on the too-small notice: %q -> %q", before.selectedID, after.selectedID)
	}
}

func TestMouseReleaseMotionAndOtherButtonsAreIgnored(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	before := m.View().Content
	for _, msg := range []tea.Msg{
		tea.MouseReleaseMsg{X: 40, Y: 8, Button: tea.MouseLeft},
		tea.MouseMotionMsg{X: 40, Y: 9, Button: tea.MouseLeft},
		tea.MouseClickMsg{X: 40, Y: 8, Button: tea.MouseRight},
		tea.MouseClickMsg{X: 40, Y: 8, Button: tea.MouseMiddle},
		tea.MouseWheelMsg{X: 40, Y: 8, Button: tea.MouseWheelLeft},
	} {
		updated, cmd := m.Update(msg)
		if cmd != nil || updated.(Model).View().Content != before {
			t.Fatalf("%T changed the screen or returned a command", msg)
		}
	}
}

func TestMouseIsIgnoredWhileSwitchedOffAndWorksAgainWhenSwitchedOn(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	first := usersScreenOf(t, m).selectedID
	m = testSendKey(t, m, testKeyF2())
	for _, msg := range []tea.Msg{mouseWheel(tea.MouseWheelDown), mouseClickAt(40, 9), mouseClickAt(3, 10)} {
		updated, cmd := m.Update(msg)
		if cmd != nil || usersScreenOf(t, updated.(Model)).selectedID != first || updated.(Model).screen != screenAdminUsers {
			t.Fatalf("%T acted while the mouse was off", msg)
		}
	}
	m = testSendKey(t, m, testKeyF2())
	m = sendMouse(t, m, mouseWheel(tea.MouseWheelDown))
	if usersScreenOf(t, m).selectedID == first {
		t.Fatal("the wheel does nothing after switching the mouse back on")
	}
}

func TestMouseNeverReachesAScreenThatIsNotMounted(t *testing.T) {
	t.Parallel()
	m := usersModelAt(t, usersTestClient(), 150, 24)
	for _, to := range []screen{screenAdminLogin, screenAdminAuthenticating, screenLoading, screenError, screenManifest} {
		other := m
		other.screen = to
		for _, msg := range []tea.Msg{mouseWheel(tea.MouseWheelDown), mouseClickAt(40, 9), mouseClickAt(3, 10)} {
			updated, cmd := other.Update(msg)
			if cmd != nil || updated.(Model).screen != to {
				t.Fatalf("%T on %s acted", msg, to)
			}
		}
	}
	// A slot screen whose slot is empty: no panic, nothing happens.
	empty := m
	empty.adminScreens[slotUsers] = nil
	if updated, cmd := empty.Update(mouseClickAt(40, 9)); cmd != nil || updated.(Model).screen != screenAdminUsers {
		t.Fatal("a click acted on an unmounted Users slot")
	}
}

func TestMouseDoesNothingWhileARobotChangeIsInFlight(t *testing.T) {
	t.Parallel()
	c := robotsMouseCase()
	m := c.build(t, 150, 24)
	robots := m.adminScreens[slotRobots].(robotsScreen)
	robots.busy = true
	m.adminScreens[slotRobots] = robots
	x, y := sidebarCell(t, m, "Users")
	for _, msg := range []tea.Msg{mouseWheel(tea.MouseWheelDown), mouseClickAt(x, y)} {
		updated, cmd := m.Update(msg)
		if cmd != nil || updated.(Model).screen != screenAdminRobots || c.selected(updated.(Model)) != c.selected(m) {
			t.Fatalf("%T acted while a robot change was in flight", msg)
		}
	}
}

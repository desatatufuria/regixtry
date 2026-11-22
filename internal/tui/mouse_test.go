package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// cellOf finds the first cell where text starts in the rendered Model view:
// the column counts terminal cells, the row terminal rows.
func cellOf(t *testing.T, m Model, text string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return lipgloss.Width(line[:i]), row
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, ansi.Strip(m.View().Content))
	return 0, 0
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

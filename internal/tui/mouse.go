package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Mouse support. The Model owns one switch (Model.mouseDisabled, toggled with
// F2) and one router (Model.updateMouse); a slot screen opts in by
// implementing mouseScreen. Mouse input only ever does what a key already
// does: the wheel and a click are turned into the arrow and Enter keys the
// screen would receive, so every guard the keys respect (a dialog in front, an
// in-flight command, a generation) applies to the mouse as well. A mouse
// event never confirms a dialog and never reaches a destructive action.

// mouseToggleKey is the key that switches the mouse on and off. F2 is no
// printable character, so it never conflicts with a text input.
func isMouseToggleKey(msg keyMsg) bool { return isCode(msg, tea.KeyF2) }

// mouseHint is the footer indicator of the mouse switch.
func mouseHint(on bool) (key, state string) {
	if on {
		return "F2", "mouse on"
	}
	return "F2", "mouse off"
}

// withMouseIndicator writes the mouse switch into the right end of the last
// row of a rendered screen (the footer of every screen), when the hints leave
// room for it; the hints are never clipped to make space.
func withMouseIndicator(theme adminTheme, screen string, width int, on bool) string {
	lines := strings.Split(screen, "\n")
	if len(lines) == 0 || width < 1 {
		return screen
	}
	last := len(lines) - 1
	line := lines[last]
	keyText, state := mouseHint(on)
	label := keyText + " " + state
	room := lipgloss.Width(line) - lipgloss.Width(strings.TrimRight(ansi.Strip(line), " "))
	if need := lipgloss.Width(label) + 2; room < need {
		return screen
	}
	bar := shellBarStyle()
	indicator := theme.text.Inherit(bar).Render(keyText) + theme.muted.Inherit(bar).Render(" "+state) + bar.Render("  ")
	cut := width - lipgloss.Width(indicator)
	lines[last] = ansi.Truncate(line, cut, "") + indicator
	return strings.Join(lines, "\n")
}

// mouseRows is where a list drew its rows, in terminal cells: Count items of
// Step lines each, starting at line Y, X wide Width, the first being item
// First of the whole list. The last Gap lines of every item are blank
// spacing that belongs to no item.
type mouseRows struct {
	X, Y, Width int
	Count, Step int
	Gap         int
	First       int
}

// rowAt is the list item drawn under the cell (x, y).
func (r mouseRows) rowAt(x, y int) (int, bool) {
	step := max(r.Step, 1)
	if x < r.X || x >= r.X+r.Width || y < r.Y || y >= r.Y+r.Count*step || (y-r.Y)%step >= step-r.Gap {
		return 0, false
	}
	return r.First + (y-r.Y)/step, true
}

// shellContentOrigin is the terminal cell of the content area's first line.
func shellContentOrigin() (x, y int) {
	return shellSidebarWidth + shellContentPadX, shellHeaderRows + shellContentPadTop
}

// mouseList is what a list screen tells the generic mouse handler: where its
// rows are, which one is selected and whether Enter on a row is harmless to
// issue on a click.
type mouseList struct {
	Rows     mouseRows
	Selected int
	// Open reports that Enter on the selected row only opens something (a
	// screen, an editor); a screen whose Enter acts on data sets it false.
	Open bool
	// Up and Down are the keys that move the selection one item; the arrows
	// when zero. Wrap > 0 makes the move cyclic over that many items (the
	// Overview's Tab focus).
	Up, Down keyMsg
	Wrap     int
}

var (
	mouseKeyUp    = keyMsg{Code: tea.KeyUp}
	mouseKeyDown  = keyMsg{Code: tea.KeyDown}
	mouseKeyEnter = keyMsg{Code: tea.KeyEnter}
)

// wheelStep is the direction of a wheel event: -1 up, 1 down, 0 for any other.
func wheelStep(msg tea.MouseMsg) int {
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		switch wheel.Button {
		case tea.MouseWheelUp:
			return -1
		case tea.MouseWheelDown:
			return 1
		}
	}
	return 0
}

// leftClick is the position of a left button press.
func leftClick(msg tea.MouseMsg) (x, y int, ok bool) {
	click, isClick := msg.(tea.MouseClickMsg)
	if !isClick || click.Button != tea.MouseLeft {
		return 0, 0, false
	}
	return click.X, click.Y, true
}

// pressKeys sends keys to s one by one and batches the commands they return,
// exactly as if the operator had typed them.
func pressKeys(s adminScreen, env screenEnv, keys ...keyMsg) (adminScreen, tea.Cmd) {
	var cmds []tea.Cmd
	for _, k := range keys {
		next, cmd, _ := s.Update(env, k)
		s = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return s, tea.Batch(cmds...)
}

// listMouse is the whole mouse behavior of a list screen: the wheel moves the
// selection one item (the arrow key), a click on a row selects it and a click
// on the selected row presses Enter when list.Open allows it. Anything else,
// including a click outside the rows, does nothing.
func listMouse(s adminScreen, env screenEnv, msg tea.MouseMsg, list mouseList) (adminScreen, tea.Cmd) {
	up, down := list.Up, list.Down
	if up == (keyMsg{}) {
		up = mouseKeyUp
	}
	if down == (keyMsg{}) {
		down = mouseKeyDown
	}
	switch step := wheelStep(msg); step {
	case -1:
		return pressKeys(s, env, up)
	case 1:
		return pressKeys(s, env, down)
	}
	x, y, ok := leftClick(msg)
	if !ok {
		return s, nil
	}
	row, ok := list.Rows.rowAt(x, y)
	if !ok {
		return s, nil
	}
	if row == list.Selected {
		if list.Open {
			return pressKeys(s, env, mouseKeyEnter)
		}
		return s, nil
	}
	var keys []keyMsg
	delta := row - list.Selected
	if list.Wrap > 0 {
		for range ((delta % list.Wrap) + list.Wrap) % list.Wrap {
			keys = append(keys, down)
		}
	} else {
		for ; delta > 0; delta-- {
			keys = append(keys, down)
		}
		for ; delta < 0; delta++ {
			keys = append(keys, up)
		}
	}
	return pressKeys(s, env, keys...)
}

// shellNavAt is the sidebar item drawn under the cell (x, y).
func shellNavAt(x, y int) (shellNavID, bool) {
	if x < 1 || x >= 1+shellNavRowWidth {
		return "", false
	}
	index := y - shellHeaderRows - 1 // the brand row comes first
	lines := shellSidebarLines()
	if index < 0 || index >= len(lines) || !lines[index].IsItem {
		return "", false
	}
	return lines[index].Item.ID, true
}

// shellNavScreen is the screen a sidebar item opens.
func shellNavScreen(id shellNavID) screen {
	switch id {
	case shellNavOverview:
		return screenAdminOverview
	case shellNavUsers:
		return screenAdminUsers
	case shellNavRobots:
		return screenAdminRobots
	case shellNavRepositoryAccess:
		return screenAdminRepositoryAccess
	case shellNavImageScanning:
		return screenSecurityTrivy
	case shellNavSecretScanning:
		return screenSecurityGitleaksConfig
	case shellNavSigningPolicy:
		return screenSecuritySigningConfig
	case shellNavScanRuns:
		return screenAdminScanRuns
	case shellNavSecretFindings:
		return screenAdminSecretFindings
	case shellNavUpdateChannel:
		return screenAdminUpdateChannel
	}
	return ""
}

// updateMouse is the one mouse router. It ignores the mouse when it is
// switched off or the terminal is too small, and every event but the wheel and
// a left click; it routes the rest to the mounted slot screen when that screen
// implements mouseScreen and no dialog, text input or in-flight command blocks
// it. A click on a sidebar item opens it through the same command the Overview's
// Enter uses.
func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mouseDisabled || m.tooSmall() {
		return m, nil
	}
	_, _, click := leftClick(msg)
	if !click && wheelStep(msg) == 0 {
		return m, nil
	}
	slot, ok := slotFor(m.screen)
	overlay := false
	if _, mounted := m.adminScreens[slotScanHistory].(scanHistoryScreen); mounted {
		slot, ok, overlay = slotScanHistory, true, true
	}
	if !ok || m.adminScreens[slot] == nil {
		return m, nil
	}
	handler, ok := m.adminScreens[slot].(mouseScreen)
	if !ok || handler.mouseBlocked() {
		return m, nil
	}
	if x, y, _ := leftClick(msg); click && !overlay && isShellScreen(m.screen) {
		if id, hit := shellNavAt(x, y); hit {
			target := shellNavScreen(id)
			if target == m.screen {
				return m, nil
			}
			if id == shellNavOverview {
				return m, navigate(screenAdminOverview)
			}
			return m, overviewOpenItem(id)
		}
	}
	next, cmd := handler.Mouse(m.screenEnv(), msg)
	m.adminScreens[slot] = next
	return m, cmd
}

// isShellScreen reports the screens drawn inside the admin shell with its
// sidebar of global navigation; the delegate screen shows the repository
// administrator's own sidebar, and the console browser has none.
func isShellScreen(current screen) bool {
	return isAdminScreen(current) && current != screenRepoAdminGrants &&
		current != screenAdminLogin && current != screenAdminAuthenticating
}

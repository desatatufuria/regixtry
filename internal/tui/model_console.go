package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// isConsoleScreen reports the three console browser screens: the Repositories,
// Projects and Tags screens, all slot screens that need no admin session.
func isConsoleScreen(current screen) bool {
	return current == screenRepositories || current == screenProjects || current == screenTags
}

// knownRepositories is the catalog's repository names, for the admin screens'
// suggestions; the Repositories screen owns the catalog.
func (m Model) knownRepositories() []string {
	if repositories, ok := m.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen); ok {
		return repositories.names()
	}
	return nil
}

// updateConsoleKey routes a key to the mounted console screen, after the two
// keys that leave the console and so need the Model: Tab opens the admin panel
// and g the repository's delegate screen. A dialog in front swallows both, and
// without an admin API the screen itself says so.
func (m Model) updateConsoleKey(msg keyMsg) (tea.Model, tea.Cmd) {
	slot, _ := slotFor(m.screen)
	if current, ok := m.adminScreens[slot].(consoleScreen); ok && !current.dialogOpen() && m.adminClient != nil {
		switch {
		case isTabKey(msg):
			return m.openAdmin()
		case isCommandRune(msg, 'g') && m.screen == screenRepositories:
			if repository, ok := current.(consoleRepositoriesScreen).selectedRepository(); ok {
				return m.openRepoAdminGrants(repository.Name)
			}
		}
	}
	return routeAdminKey(m, msg)
}

// consoleScreen is what the Model asks of a console screen besides adminScreen.
type consoleScreen interface {
	adminScreen
	dialogOpen() bool
}

// withMountedConsole makes sure the console screen the operator returns to
// exists: leaving the login of a startup-login deployment, before the catalog
// was ever read, lands on an empty Repositories screen, never on nothing.
func (m Model) withMountedConsole() Model {
	if m.screen == screenRepositories && m.adminScreens[slotConsoleRepositories] == nil {
		m.adminScreens[slotConsoleRepositories] = newConsoleRepositoriesScreen(nil)
	}
	return m
}

// viewConsole draws the mounted console screen over the whole terminal; its
// dialog, when one is open, is composited over it.
func (m Model) viewConsole() string {
	theme := newAdminTheme()
	slot, _ := slotFor(m.screen)
	current := m.adminScreens[slot]
	if current == nil {
		current = newConsoleRepositoriesScreen(nil)
	}
	env := m.screenEnv()
	env.Layout = consoleLayout{Width: m.viewport.Width, Height: m.viewport.Height}
	frame := current.View(theme, env)
	if frame.Overlay == "" {
		return frame.Body
	}
	return compositeOverlay(frame.Body, frame.Overlay, m.viewport.Width, m.viewport.Height)
}

// openConsole handles a console screen's request to move to another one.
func (m Model) openConsole(request tea.Msg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch msg := request.(type) {
	case openProjectsMsg:
		m.adminScreens[slotConsoleProjects] = newConsoleProjectsScreen(msg.repositories)
		m.screen = screenProjects
	case openRepositoriesMsg:
		if repositories, ok := m.adminScreens[slotConsoleRepositories].(consoleRepositoriesScreen); ok {
			m.adminScreens[slotConsoleRepositories] = repositories.narrowedTo(msg.project)
		}
		m.screen = screenRepositories
	case openTagsMsg:
		// A fresh screen: nothing of another repository's tags survives into
		// it. The chosen sort carries over, as it always has.
		mode := sortByNameAsc
		if previous, ok := m.adminScreens[slotConsoleTags].(consoleTagsScreen); ok {
			mode = previous.sortMode
		}
		next := newConsoleTagsScreen(msg.repository, mode)
		m.adminScreens[slotConsoleTags] = next
		m.screen = screenTags
		return m, next.Init(m.screenEnv())
	case openManifestMsg:
		m.screen = screenLoading
		m.loadingText = fmt.Sprintf("Loading manifest %s:%s...", msg.repository, msg.tag)
		return m, m.loadManifestCmd(msg.repository, msg.tag)
	}
	return m, nil
}

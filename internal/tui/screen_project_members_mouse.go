package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// mouseBlocked: a revoke dialog is in front, or a revoke is on its way.
func (s adminProjectMembersScreen) mouseBlocked() bool { return s.confirm.Active() || s.revoking }

// Mouse: the wheel moves the selection of the focused list; a click on a
// member selects it and focuses the members, a click on a repository selects
// it and focuses the repositories. Granting and revoking stay keys, and the
// mouse never confirms a dialog.
func (s adminProjectMembersScreen) Mouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	members, repos := s.mouseMembers(env), s.mouseRepositories(env)
	if x, y, ok := leftClick(msg); ok {
		if _, hit := repos.rowAt(x, y); hit {
			s.focusRepos = true
			return listMouse(s, env, msg, mouseList{Rows: repos, Selected: boundedIndex(s.repoCursor, s.repoCount)})
		}
		if _, hit := members.rowAt(x, y); hit {
			s.focusRepos = false
		}
	}
	if s.focusRepos {
		return listMouse(s, env, msg, mouseList{Rows: repos, Selected: boundedIndex(s.repoCursor, s.repoCount)})
	}
	return listMouse(s, env, msg, mouseList{Rows: members, Selected: s.index()})
}

func (s adminProjectMembersScreen) mouseMembers(env screenEnv) mouseRows {
	if len(s.members) == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	leftW, _, panelH, _ := s.membersLayout(bodyW, bodyH)
	visible := max(panelH-2-2, 1)
	start, end := grantWindow(s.index(), len(s.members), visible)
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + membersRowsAbove + membersTableAbove, Width: leftW, Count: end - start, First: start}
}

func (s adminProjectMembersScreen) mouseRepositories(env screenEnv) mouseRows {
	repos := s.repositories(env)
	if len(repos) == 0 {
		return mouseRows{}
	}
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	leftW, panelW, panelH, _ := s.membersLayout(bodyW, bodyH)
	footer := len(grantWrap(fmt.Sprintf("%s. Browse them in Console › Projects › %s.", plural(len(repos), "repository"), s.project), max(panelW-4, 1)))
	rows := max(panelH-2-1-footer, 1)
	start, end := grantWindow(boundedIndex(s.repoCursor, len(repos)), len(repos), rows)
	x, y := shellContentOrigin()
	return mouseRows{X: x + leftW + 2, Y: y + membersRowsAbove + 2, Width: panelW, Count: end - start, First: start}
}

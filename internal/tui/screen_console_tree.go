package tui

import (
	"maps"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// The Repositories screen under a project is a visual-only folder tree over
// the repository names (S8). A folder is a name prefix: it has no record, no
// permission and no action of its own, and nothing here calls the registry
// again: the tree is derived from the catalog the screen already holds.

// rowKey identifies a visible row across reloads: by path, never by index.
type rowKey struct {
	path string
	self bool
}

// rebuildTree derives the tree again from the project's repositories. A
// rooted folder that no longer exists falls back to its nearest surviving
// ancestor, never above the project.
func (s consoleRepositoriesScreen) rebuildTree() consoleRepositoriesScreen {
	s.tree = buildFolderTree(s.visible())
	for s.rootPath != "" && s.rootPath != s.project {
		if _, ok := s.tree.node(s.rootPath); ok {
			break
		}
		s.rootPath = parentPath(s.rootPath)
	}
	return s
}

// rows are the visible rows under the current root.
func (s consoleRepositoriesScreen) rows() []folderRow {
	root, ok := s.tree.node(s.rootPath)
	if !ok {
		return nil
	}
	return flattenFolderRows(root, s.expanded)
}

func (s consoleRepositoriesScreen) selectedRow() (folderRow, bool) {
	rows := s.rows()
	if len(rows) == 0 {
		return folderRow{}, false
	}
	return rows[boundedIndex(s.selected, len(rows))], true
}

// treeMark remembers the selected row before the catalog changes.
func (s consoleRepositoriesScreen) treeMark() (rowKey, bool) {
	if !s.filtered {
		return rowKey{}, false
	}
	row, ok := s.selectedRow()
	return rowKey{path: row.node.path, self: row.self}, ok
}

// reselect puts the cursor back on the marked row by path, or on its nearest
// surviving ancestor, or keeps the index bounded when nothing survives.
func (s consoleRepositoriesScreen) reselect(mark rowKey, marked bool) consoleRepositoriesScreen {
	rows := s.rows()
	s.selected = boundedIndex(s.selected, len(rows))
	if !marked {
		return s
	}
	if i := slices.IndexFunc(rows, func(r folderRow) bool { return r.node.path == mark.path && r.self == mark.self }); i >= 0 {
		s.selected = i
		return s
	}
	for path := parentPath(mark.path); path != ""; path = parentPath(path) {
		if i := slices.IndexFunc(rows, func(r folderRow) bool { return r.node.path == path && !r.self }); i >= 0 {
			s.selected = i
			break
		}
	}
	return s
}

// withExpanded opens or closes one folder. The set is copied on write: the
// screen is copied by value everywhere.
func (s consoleRepositoriesScreen) withExpanded(path string, open bool) consoleRepositoriesScreen {
	next := maps.Clone(s.expanded)
	if next == nil {
		next = map[string]bool{}
	}
	if open {
		next[path] = true
	} else {
		delete(next, path)
	}
	s.expanded = next
	return s
}

// into re-roots the view at a folder.
func (s consoleRepositoriesScreen) into(path string) consoleRepositoriesScreen {
	s.rootPath, s.selected, s.notice = path, 0, ""
	return s
}

// up goes one folder up and leaves the cursor on the folder it came from.
func (s consoleRepositoriesScreen) up() consoleRepositoriesScreen {
	from := s.rootPath
	s.rootPath, s.notice = parentPath(from), ""
	s.selected = 0
	if i := slices.IndexFunc(s.rows(), func(r folderRow) bool { return r.node.path == from && !r.self }); i >= 0 {
		s.selected = i
	}
	return s
}

// unfiltered leaves the project: the whole catalog shows again.
func (s consoleRepositoriesScreen) unfiltered() consoleRepositoriesScreen {
	s.filtered, s.project, s.selected, s.notice = false, "", 0, ""
	s.rootPath, s.expanded, s.tree = "", nil, folderTree{}
	return s
}

// updateTreeKey handles the keys the tree changes. It reports handled false
// for the keys that keep their meaning (p, g, d on a repository, Tab).
func (s consoleRepositoriesScreen) updateTreeKey(msg keyMsg) (adminScreen, tea.Cmd, bool) {
	rows := s.rows()
	if cursor, moved := consoleMove(msg, s.selected, len(rows)); moved {
		s.selected = cursor
		return s, nil, true
	}
	var row folderRow
	has := len(rows) > 0
	if has {
		row = rows[boundedIndex(s.selected, len(rows))]
	}
	switch {
	case isBackKey(msg):
		if s.rootPath != s.project && s.rootPath != "" {
			return s.up(), nil, true
		}
		return s.unfiltered(), nil, true
	case isMoveRightKey(msg), isCommandRune(msg, 'l'):
		if has && !row.self && row.node.isFolder() && !row.expanded {
			s = s.withExpanded(row.node.path, true)
		}
		return s, nil, true
	case isMoveLeftKey(msg), isCommandRune(msg, 'h'):
		switch {
		case !has || row.self:
		case row.expanded:
			s = s.withExpanded(row.node.path, false)
		default:
			// A leaf or a collapsed folder: the cursor goes to its parent.
			parent := parentPath(row.node.path)
			if i := slices.IndexFunc(rows, func(r folderRow) bool { return r.node.path == parent && !r.self }); i >= 0 {
				s.selected = i
			}
		}
		return s, nil, true
	case isCommandRune(msg, 'f'):
		if has && !row.self && row.node.isFolder() {
			s = s.into(row.node.path)
		}
		return s, nil, true
	case isEnterKey(msg):
		switch {
		case !has:
		case row.node.isRepository:
			return s, consoleAsk(openTagsMsg{repository: row.node.repository.Name}), true
		case row.node.isFolder():
			s = s.into(row.node.path)
		}
		return s, nil, true
	case isCommandRune(msg, 'd'):
		// A folder is not a record: there is nothing to delete.
		if has && !row.node.isRepository {
			return s, nil, true
		}
	}
	return s, nil, false
}

// treeHints is the footer of the tree: the board's wording, with the longer
// "up one level" where the terminal has room for it.
func treeHints(wide bool) []key.Binding {
	up := "up"
	if wide {
		up = "up one level"
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand")),
		key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open tags")),
		key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "into folder")),
		key.NewBinding(key.WithKeys("backspace", "esc"), key.WithHelp("bksp/esc", up)),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

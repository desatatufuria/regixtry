package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// treePanelMinWidth is the terminal width from which the detail panel
	// shows; narrower terminals hide it, they never scroll sideways.
	treePanelMinWidth = 170
	treePanelWidth    = 44
	treePanelGap      = 2
	// treeTallBody is the body height from which the legend takes two lines.
	treeTallBody = 30
	// Count columns.
	treeCountWidth = 6
	treeDateWidth  = 16
)

const (
	treeLegend      = "● repository  ▸ collapsed folder  ▾ expanded folder. A row with both is a repository that also has children."
	treeLegendNoACL = "Folders carry no permissions. Grant access on the project or on a repository."
)

// treeLayout is where the tree's parts sit for a terminal width by height; the
// view and the mouse both read it so they cannot disagree.
type treeLayout struct {
	bodyW, bodyH int
	tableW       int
	tableH       int
	panel        bool
	legendRows   int
	visible      int
}

func treeLayoutFor(width, height int) treeLayout {
	l := treeLayout{legendRows: 1}
	l.bodyW, l.bodyH = consoleBodySize(width, height)
	if l.bodyH >= treeTallBody {
		l.legendRows = 2
	}
	l.tableW = l.bodyW
	if width >= treePanelMinWidth {
		l.panel = true
		l.tableW = l.bodyW - treePanelWidth - treePanelGap
	}
	// Below the table: the status line and the legend.
	l.tableH = max(l.bodyH-consoleHeadRows-1-l.legendRows, 5)
	l.visible = consoleTableVisible(l.tableH)
	return l
}

func treeColumns() []consoleColumn {
	return []consoleColumn{{title: "NAME"}, {title: padLeft("REPOS", treeCountWidth), width: treeCountWidth}, {title: padLeft("TAGS", treeCountWidth), width: treeCountWidth}, {title: "LAST PUSH", width: treeDateWidth}}
}

// padLeft right-aligns text in width cells.
func padLeft(text string, width int) string {
	if gap := width - lipgloss.Width(text); gap > 0 {
		return strings.Repeat(" ", gap) + text
	}
	return text
}

// treeIndent is the indent of a row at a depth: two cells a level, never more
// than a third of the name column, so a deep name keeps room. When it is cut,
// the first cell is an ellipsis.
func treeIndent(depth, nameWidth int) string {
	want := 2 * depth
	limit := max(nameWidth/3, 0)
	if want <= limit {
		return strings.Repeat(" ", want)
	}
	if limit < 1 {
		return ""
	}
	return "…" + strings.Repeat(" ", limit-1)
}

// treeNameCell is the NAME cell: indent, fold slot, repository slot, name.
func treeNameCell(row folderRow, nameWidth int) string {
	fold, repo := " ", " "
	if !row.self && row.node.isFolder() {
		fold = "▸"
		if row.expanded {
			fold = "▾"
		}
	}
	if row.node.isRepository {
		repo = "●"
	}
	name := sanitizeDisplay(row.node.name)
	if !row.self && !row.node.isRepository {
		name += "/"
	}
	return treeIndent(row.depth, nameWidth) + fold + " " + repo + " " + name
}

// treeRowCells are the cells of one row. The pinned own row of a root that is
// also a repository counts only itself.
func treeRowCells(theme adminTheme, row folderRow, nameWidth int) []consoleCell {
	repos, tags, last := row.node.repositoryCount, row.node.tagCount, row.node.lastPushed
	if row.self {
		repos, tags, last = 1, row.node.repository.TagCount, row.node.repository.LastPushed
	}
	return []consoleCell{
		{treeNameCell(row, nameWidth), theme.text},
		{padLeft(fmt.Sprint(repos), treeCountWidth), theme.text},
		{padLeft(fmt.Sprint(tags), treeCountWidth), theme.text},
		{formatRepositoryLastPushed(last), theme.text},
	}
}

func (s consoleRepositoriesScreen) treeCrumbs() []string {
	return append([]string{"Console", "Projects"}, strings.Split(s.rootPath, "/")...)
}

func (s consoleRepositoriesScreen) treeView(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	l := treeLayoutFor(width, height)
	rows := s.rows()
	cursor := boundedIndex(s.selected, len(rows))

	crumbs := s.treeCrumbs()
	title := crumbs[len(crumbs)-1]
	root, _ := s.tree.node(s.rootPath)
	var nRepos, nTags int
	if root != nil {
		nRepos, nTags = root.repositoryCount, root.tagCount
	}
	summary := countOf(nRepos, "repository", "repositories") + ", " + countOf(nTags, "tag", "tags")
	if s.rootPath != s.project {
		summary += " in this folder"
	}

	columns := treeColumns()
	spec := consoleTableSpec{columns: columns}
	if len(rows) == 0 {
		spec.message, spec.messageStyle = "No repositories in this folder.", theme.muted
	} else {
		nameWidth := consoleColumnWidths(columns, max(l.tableW-4, 1))[0]
		start, end := grantWindow(cursor, len(rows), l.visible)
		for _, row := range rows[start:end] {
			spec.rows = append(spec.rows, treeRowCells(theme, row, nameWidth))
		}
		spec.cursor = cursor - start
	}
	table := renderConsoleTable(theme, l.tableW, l.tableH, spec)
	if l.panel {
		panel := strings.Split(userPanel(theme, treePanelWidth, l.tableH, s.treePanelLines(theme, rows, cursor)), "\n")
		for i := range table {
			if i < len(panel) {
				table[i] += strings.Repeat(" ", treePanelGap) + panel[i]
			}
		}
	}

	lines := []string{spread(theme.subheading.Render(sanitizeDisplay(title)), theme.muted.Render(grantTruncate(summary, max(l.bodyW-lipgloss.Width(title)-2, 1))), l.bodyW), ""}
	lines = append(lines, table...)
	lines = append(lines, consoleStatusLine(theme, s.confirm, s.notice, s.noticeFail, l.bodyW), theme.muted.Render(grantTruncate(treeLegend, l.bodyW)))
	if l.legendRows == 2 {
		lines = append(lines, theme.muted.Render(grantTruncate(treeLegendNoACL, l.bodyW)))
	}
	body := strings.Join(lines, "\n")

	frame := screenFrame{Context: strings.Join(crumbs, " › "), Full: true, Body: renderConsoleFrame(theme, env, crumbs, body, treeHints(width >= treePanelMinWidth))}
	if s.confirm.Active() {
		frame.Overlay = s.confirm.view(theme)
	}
	return frame
}

// treePanelLines is the detail panel of the selected row.
func (s consoleRepositoriesScreen) treePanelLines(theme adminTheme, rows []folderRow, cursor int) []string {
	lines := []string{theme.muted.Bold(true).Render("SELECTED")}
	if len(rows) == 0 {
		return lines
	}
	row := rows[cursor]
	node := row.node
	inner := treePanelWidth - 4
	name := node.path
	if node.isRepository {
		name = node.repository.Name
	}
	yesNo := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	field := func(label, value string) string { return padText(label, 16) + value }
	lines = append(lines,
		theme.text.Render(grantTruncate(sanitizeDisplay(name), inner)),
		"",
		field("Repository", yesNo(node.isRepository)),
		field("Folder", yesNo(node.isFolder())),
		field("In this folder", countOf(node.repositoryCount, "repository", "repositories")),
		field("", countOf(node.tagCount, "tag", "tags")),
		field("Last push", formatRepositoryLastPushed(node.lastPushed)),
		"Access",
	)
	access := "project " + repositoryProjectName(node.path) + " roles + exact grants"
	if repositoryProjectName(node.path) == projectUngroupedLabel {
		access = "exact grants only, no project"
	}
	lines = append(lines, theme.muted.Render(grantTruncate(access, inner)), "")
	var help string
	switch {
	case node.isRepository && node.isFolder():
		help = fmt.Sprintf("enter opens the tags of %s. f moves into the folder, so the title and the breadcrumb become %s. Folders have no access of their own.", name, node.name)
	case node.isRepository:
		help = fmt.Sprintf("enter opens the tags of %s.", name)
	default:
		help = fmt.Sprintf("enter moves into the folder, so the title and the breadcrumb become %s. Folders have no access of their own.", node.name)
	}
	for _, line := range grantWrap(sanitizeDisplay(help), inner) {
		lines = append(lines, theme.muted.Render(line))
	}
	return lines
}

// ---- mouse ----

// treeMouseRows is where the tree drew its data rows, in the terminal's cells.
func (s consoleRepositoriesScreen) treeMouseRows(env screenEnv, total int) mouseRows {
	if total == 0 {
		return mouseRows{}
	}
	l := treeLayoutFor(env.Layout.Width, env.Layout.Height)
	start, end := grantWindow(boundedIndex(s.selected, total), total, l.visible)
	return mouseRows{
		X:     consolePadX,
		Y:     shellHeaderRows + shellContentPadTop + consoleHeadRows + consoleTableChromeRows,
		Width: l.tableW, Count: end - start, First: start,
	}
}

// treeMouse is the wheel and the clicks of the tree: they select, and a click
// on a folder's fold marker opens or closes it. Nothing here can delete: the
// mouse sends only moves, Enter (which opens tags or a folder) and the toggle.
func (s consoleRepositoriesScreen) treeMouse(env screenEnv, msg tea.MouseMsg) (adminScreen, tea.Cmd) {
	rows := s.rows()
	mouse := s.treeMouseRows(env, len(rows))
	list := mouseList{Rows: mouse, Selected: boundedIndex(s.selected, len(rows)), Open: true}
	if x, y, ok := leftClick(msg); ok {
		if index, hit := mouse.rowAt(x, y); hit {
			row := rows[index]
			l := treeLayoutFor(env.Layout.Width, env.Layout.Height)
			nameWidth := consoleColumnWidths(treeColumns(), max(l.tableW-4, 1))[0]
			// Border and padding, the selection marker, then the indent.
			foldX := consolePadX + 2 + consoleMarkerWidth + lipgloss.Width(treeIndent(row.depth, nameWidth))
			if x == foldX && !row.self && row.node.isFolder() {
				s.selected = index
				return s.withExpanded(row.node.path, !row.expanded), nil
			}
		}
	}
	return listMouse(s, env, msg, list)
}

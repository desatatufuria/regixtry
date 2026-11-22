package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appregixtry "regixtry/internal/app/regixtry"
)

// The console browser (tui-v2-redesign S3, the Projects, Repositories and Tags
// boards): the three screens that browse the registry's own catalog and
// delete its data. They are slot screens drawn inside the application shell's
// frame, without the admin sidebar: the console is open to the local operator
// before any admin sign-in, so no admin navigation may appear on it.

// consoleGenerations hands out the unique id of every console browser screen
// instance. Every async message a screen produces carries it and a message
// with another id is dropped, so a late result never lands on a newer screen
// that reuses the slot.
var consoleGenerations atomic.Uint64

const (
	// consolePadX is the board's 36px side padding at about 8.5px a column.
	consolePadX = 4
	// consoleTableMaxWidth is the board's 760 to 860px table column.
	consoleTableMaxWidth = 100
	// consoleColumnGap separates table columns; consoleMarkerWidth is the
	// selection marker's own column.
	consoleColumnGap   = 2
	consoleMarkerWidth = 2
)

// consoleUnavailableNotice is shown when a key needs the admin API and the
// session has none.
const consoleUnavailableNotice = "Admin API is unavailable for this session."

// errConsoleUnavailable is the error of a command built without a service.
var errConsoleUnavailable = errors.New("registry service is unavailable")

// ---- messages to the parent ----

// openProjectsMsg asks the parent to mount a FRESH Projects screen over a copy
// of the catalog the Repositories screen holds.
type openProjectsMsg struct {
	repositories []appregixtry.RepositorySummary
}

// openRepositoriesMsg asks the parent to show the Repositories screen
// narrowed to one project (Enter on the Projects screen).
type openRepositoriesMsg struct{ project string }

// openTagsMsg asks the parent to mount a FRESH Tags screen for one repository.
type openTagsMsg struct{ repository string }

// openManifestMsg asks the parent to open the manifest of one tag.
type openManifestMsg struct{ repository, tag string }

func consoleAsk(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// ---- delete bookkeeping ----

// consoleShared is the delete bookkeeping that outlives any one console screen:
// the Model owns one and mounts every console screen on it, so a screen
// re-created while a delete is in flight still blocks a second one, and a list
// read before a delete never shows the deleted row, whichever instance asked.
// It is a pointer on purpose: Model and the screens are copied by value.
type consoleShared struct {
	busy    bool
	deletes int
	deleted map[string]bool
}

func newConsoleShared() *consoleShared { return &consoleShared{deleted: map[string]bool{}} }

// Keys name a deleted row across screens.
func repositoryKey(name string) string     { return "r:" + name }
func tagKey(repository, tag string) string { return "t:" + repository + ":" + tag }

func (c *consoleShared) isBusy() bool { return c != nil && c.busy }
func (c *consoleShared) setBusy(busy bool) {
	if c != nil {
		c.busy = busy
	}
}

func (c *consoleShared) isDeleted(key string) bool { return c != nil && c.deleted[key] }

// markDeleted records rows whose delete landed. It is idempotent: the Model and
// the screen that asked both apply the same result.
func (c *consoleShared) markDeleted(keys ...string) {
	if c == nil {
		return
	}
	added := false
	for _, k := range keys {
		if !c.deleted[k] {
			c.deleted[k], added = true, true
		}
	}
	if added {
		c.deletes++
	}
}

func (c *consoleShared) deleteCount() int {
	if c == nil {
		return 0
	}
	return c.deletes
}

// applyResult records a delete result of any screen instance: the row is gone
// and no delete is in flight any more.
func (c *consoleShared) applyResult(msg tea.Msg) {
	switch m := msg.(type) {
	case repositoryDeletedMsg:
		c.setBusy(false)
		if m.err == nil {
			c.markDeleted(repositoryKey(m.repository))
		}
	case tagDeletedMsg:
		c.setBusy(false)
		if m.err == nil {
			c.markDeleted(tagKey(m.repository, m.tag))
		}
	case projectDeletedMsg:
		c.setBusy(false)
		for _, name := range m.removed() {
			c.markDeleted(repositoryKey(name))
		}
	}
}

// consoleSync keeps a reload from undoing a delete. A list read that was
// launched before a delete landed may still contain the deleted row: it is
// filtered with the shared deleted set. A list launched after the last delete
// is authoritative. A list older than one already applied is dropped.
type consoleSync struct {
	launched int
	applied  int
}

// launch registers a read about to start and returns the stamp it carries.
func (c consoleSync) launch(shared *consoleShared) (consoleSync, consoleStamp) {
	c.launched++
	return c, consoleStamp{seq: c.launched, deletes: shared.deleteCount()}
}

// accept decides what to do with a list stamped at launch: ok is false for a
// list older than the last one applied; hide is true when the list predates a
// delete and so must not show the deleted rows.
func (c consoleSync) accept(stamp consoleStamp, shared *consoleShared) (next consoleSync, ok, hide bool) {
	if stamp.seq < c.applied {
		return c, false, false
	}
	c.applied = stamp.seq
	return c, true, stamp.deletes < shared.deleteCount()
}

// consoleStamp is what a read carries from its launch.
type consoleStamp struct{ seq, deletes int }

// ---- frame ----

// renderConsoleFrame draws a whole console screen: the shell's header bar and
// footer hints around a full-width body, exactly Layout.Width by Layout.Height.
func renderConsoleFrame(theme adminTheme, env screenEnv, crumbs []string, body string, hints []key.Binding) string {
	width, height := max(env.Layout.Width, shellSidebarWidth+1), max(env.Layout.Height, shellChromeRows+1)
	rows := height - shellChromeRows
	innerW := max(width-2*consolePadX, 1)

	rule := lipgloss.NewStyle().Foreground(theme.borderColor).Render(strings.Repeat("─", width))
	lines := make([]string, 0, height)
	lines = append(lines, consoleHeader(theme, env, crumbs, width), rule)
	bodyLines := strings.Split(body, "\n")
	for i := range rows {
		line := ""
		if row := i - shellContentPadTop; row >= 0 && row < len(bodyLines) {
			line = strings.Repeat(" ", consolePadX) + lipgloss.NewStyle().MaxWidth(innerW).Render(bodyLines[row])
		}
		lines = append(lines, line)
	}
	lines = append(lines, rule, shellFooter(theme, shellSpec{Hints: hints}, width))
	return strings.Join(lines, "\n")
}

// consoleBodySize is the width and height of the body a console screen lays
// out for a terminal of this size.
func consoleBodySize(width, height int) (int, int) {
	return max(width-2*consolePadX, 1), max(height-shellChromeRows-shellContentPadTop, 1)
}

// consoleHeader is the shell's header bar. "signed in as" shows only when an
// admin session exists: the console itself needs none.
func consoleHeader(theme adminTheme, env screenEnv, crumbs []string, width int) string {
	reserve := 4
	if env.Session.IsAuthenticated() {
		reserve += len("signed in as ") + lipgloss.Width(env.Session.Username) + 2
	}
	crumbs = fitCrumbs(crumbs, width-reserve)
	if env.Session.IsAuthenticated() {
		return shellHeader(theme, shellSpec{Breadcrumb: crumbs, Operator: env.Session.Username}, width)
	}
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	text := theme.text.Inherit(bar)
	parts := make([]string, len(crumbs))
	for i, crumb := range crumbs {
		if i == len(crumbs)-1 {
			parts[i] = text.Render(crumb)
		} else {
			parts[i] = muted.Render(crumb)
		}
	}
	return shellFitBar(bar.Render("  ")+strings.Join(parts, muted.Render(" › ")), width)
}

// ---- table ----

// consoleColumn is one table column; width 0 takes what the others leave.
type consoleColumn struct {
	title string
	width int
}

// consoleCell is one cell's text and style.
type consoleCell struct {
	text  string
	style lipgloss.Style
}

// consoleTableSpec is what renderConsoleTable draws.
type consoleTableSpec struct {
	columns []consoleColumn
	rows    [][]consoleCell
	cursor  int
	// message replaces the rows (loading, error, empty) when not empty.
	message      string
	messageStyle lipgloss.Style
}

// renderConsoleTable draws the bordered table card of the boards: a header
// row, a rule, then the rows windowed around the cursor. It is exactly height
// rows by width columns.
func renderConsoleTable(theme adminTheme, width, height int, spec consoleTableSpec) []string {
	inner := max(width-4, 1)
	visible := max(height-4, 1)
	widths := consoleColumnWidths(spec.columns, inner)

	head := make([]string, len(spec.columns))
	for i, c := range spec.columns {
		head[i] = padText(c.title, widths[i])
	}
	box := []string{
		theme.muted.Bold(true).Render(strings.Repeat(" ", consoleMarkerWidth) + strings.Join(head, strings.Repeat(" ", consoleColumnGap))),
		lipgloss.NewStyle().Foreground(theme.borderColor).Render(strings.Repeat("─", inner)),
	}
	if spec.message != "" {
		box = append(box, spec.messageStyle.Render(grantTruncate(spec.message, inner)))
	} else {
		cursor := boundedIndex(spec.cursor, len(spec.rows))
		start, end := grantWindow(cursor, len(spec.rows), visible)
		for i := start; i < end; i++ {
			box = append(box, renderConsoleRow(theme, spec.rows[i], widths, i == cursor))
		}
	}
	for len(box) < height-2 {
		box = append(box, "")
	}
	return strings.Split(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(box[:height-2], "\n")), "\n")
}

// consoleColumnWidths gives every fixed column its width and the flexible one
// the rest of a row inner cells wide.
func consoleColumnWidths(columns []consoleColumn, inner int) []int {
	widths := make([]int, len(columns))
	fixed := consoleMarkerWidth + consoleColumnGap*(len(columns)-1)
	for i, c := range columns {
		fixed += c.width
		widths[i] = c.width
	}
	for i, c := range columns {
		if c.width == 0 {
			widths[i] = max(inner-fixed, 4)
		}
	}
	return widths
}

func renderConsoleRow(theme adminTheme, cells []consoleCell, widths []int, selected bool) string {
	background := lipgloss.NewStyle()
	marker := strings.Repeat(" ", consoleMarkerWidth)
	if selected {
		background = background.Background(lipgloss.Color(paletteSelectedBGHex))
		marker = "▸ "
	}
	var b strings.Builder
	b.WriteString(background.Inherit(theme.accent).Render(marker))
	for i, cell := range cells {
		if i > 0 {
			b.WriteString(background.Render(strings.Repeat(" ", consoleColumnGap)))
		}
		style := cell.style
		if selected && i == 0 {
			style = theme.accent
		}
		b.WriteString(style.Background(background.GetBackground()).Render(padText(cell.text, widths[i])))
	}
	return b.String()
}

// consoleStatusLine is the one line under the table: a delete in flight, or the
// last action's outcome.
func consoleStatusLine(theme adminTheme, confirm confirmPrompt, notice string, fail bool, width int) string {
	switch {
	case confirm.inFlight && confirm.submitting != "":
		return theme.muted.Render(grantTruncate(confirm.submitting, width))
	case notice != "" && fail:
		return theme.error.Render(grantTruncate(notice, width))
	case notice != "":
		return theme.success.Render(grantTruncate(notice, width))
	}
	return ""
}

// consoleBody lays out the common body: the title and summary line, a blank,
// the table card, the status line and the footnote, in exactly rows rows.
func consoleBody(theme adminTheme, width, rows int, title, summary string, table consoleTableSpec, status, footnote string) string {
	tableW := min(width, consoleTableMaxWidth)
	lines := []string{spread(theme.subheading.Render(title), theme.muted.Render(grantTruncate(summary, max(width-lipgloss.Width(title)-2, 1))), width), ""}
	tableH := max(rows-len(lines)-2, 5)
	lines = append(lines, renderConsoleTable(theme, tableW, tableH, table)...)
	lines = append(lines, status, theme.muted.Render(grantTruncate(footnote, width)))
	return strings.Join(lines, "\n")
}

// consoleMove moves a cursor over total rows for a navigation key and reports
// whether the key was one.
func consoleMove(msg keyMsg, cursor, total int) (int, bool) {
	switch {
	case isMoveUpKey(msg):
		return boundedIndex(cursor-1, total), true
	case isMoveDownKey(msg):
		return boundedIndex(cursor+1, total), true
	case isPgUpKey(msg):
		return boundedIndex(cursor-consolePage, total), true
	case isPgDnKey(msg):
		return boundedIndex(cursor+consolePage, total), true
	case isHomeKey(msg):
		return 0, true
	case isEndKey(msg):
		return boundedIndex(total-1, total), true
	}
	return cursor, false
}

// consolePage is how many rows PgUp and PgDn move.
const consolePage = 10

// countOf words a count with its unit: "1 repository", "2 repositories".
func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// fitCrumbs shortens a breadcrumb trail to width columns: the longest of the
// middle crumbs first, so the last one, the place the operator is in, stays
// whole for as long as it can.
func fitCrumbs(crumbs []string, width int) []string {
	if len(crumbs) == 0 {
		return nil
	}
	out := slices.Clone(crumbs)
	total := func() int {
		n := 3 * (len(out) - 1)
		for _, c := range out {
			n += lipgloss.Width(c)
		}
		return n
	}
	const floor = 6
	for total() > width {
		longest := -1
		for i := 0; i < len(out)-1; i++ {
			if lipgloss.Width(out[i]) > floor && (longest < 0 || lipgloss.Width(out[i]) > lipgloss.Width(out[longest])) {
				longest = i
			}
		}
		if longest < 0 {
			longest = len(out) - 1
			if lipgloss.Width(out[longest]) <= floor {
				break
			}
		}
		out[longest] = grantTruncate(out[longest], lipgloss.Width(out[longest])-1)
	}
	return out
}

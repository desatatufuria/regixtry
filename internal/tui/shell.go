package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// The application shell of the admin TUI (tui-v2-redesign S3, the approved
// "Admin workspace" board): a header bar, a sidebar, the content area and a
// footer key-hint bar. renderShell is the ONE place that builds it; the
// Overview uses it now and every redesigned screen will use it later.
//
// Row budget (height H): header 1 + rule 1 + body H-4 + rule 1 + footer 1.
// Column budget (width W): sidebar shellSidebarWidth, then the content area.
const (
	// shellSidebarWidth is the board's 272px sidebar at about 8.5px a column.
	shellSidebarWidth = 32
	// shellChromeRows is the header, the two rules and the footer.
	shellChromeRows = 4
	// shellContentPadX is the content area's left and right margin; the top
	// margin is shellContentPadTop rows.
	shellContentPadX   = 2
	shellContentPadTop = 1
	// shellNavRowWidth is the width of one sidebar row: the sidebar minus its
	// one-column margin on each side and its right border column.
	shellNavRowWidth = shellSidebarWidth - 3
)

// shellNavID names one sidebar item.
type shellNavID string

const (
	shellNavOverview         shellNavID = "overview"
	shellNavUsers            shellNavID = "users"
	shellNavRobots           shellNavID = "robots"
	shellNavRepositoryAccess shellNavID = "repository-access"
	shellNavImageScanning    shellNavID = "image-scanning"
	shellNavSecretScanning   shellNavID = "secret-scanning"
	shellNavSigningPolicy    shellNavID = "signing-policy"
	shellNavScanRuns         shellNavID = "scan-runs"
	shellNavSecretFindings   shellNavID = "secret-findings"
	shellNavUpdateChannel    shellNavID = "update-channel"
)

type shellNavItem struct {
	ID    shellNavID
	Label string
}

// shellNavGroup is a titled run of sidebar items; the untitled group holds
// the Overview.
type shellNavGroup struct {
	Title string
	Items []shellNavItem
}

var shellNavGroups = []shellNavGroup{
	{Items: []shellNavItem{{shellNavOverview, "Overview"}}},
	{Title: "ACCESS", Items: []shellNavItem{
		{shellNavUsers, "Users"},
		{shellNavRobots, "Robots"},
		{shellNavRepositoryAccess, "Repository access"},
	}},
	{Title: "SECURITY", Items: []shellNavItem{
		{shellNavImageScanning, "Image scanning"},
		{shellNavSecretScanning, "Secret scanning"},
		{shellNavSigningPolicy, "Signing policy"},
	}},
	{Title: "OPERATIONS", Items: []shellNavItem{
		{shellNavScanRuns, "Scan runs"},
		{shellNavSecretFindings, "Secret findings"},
		{shellNavUpdateChannel, "Update channel"},
	}},
}

// shellNavItems is the sidebar flattened in display order, the order up and
// down move through.
func shellNavItems() []shellNavItem {
	var items []shellNavItem
	for _, group := range shellNavGroups {
		items = append(items, group.Items...)
	}
	return items
}

// shellSpec is everything renderShell needs.
type shellSpec struct {
	// Breadcrumb is the header trail, left to right; the last crumb is the
	// current place.
	Breadcrumb []string
	// Operator is shown on the right of the header as "signed in as <name>".
	Operator string
	// Active is the highlighted sidebar item.
	Active shellNavID
	// Body is the content area's text, already laid out to
	// shellContentSize(Width, Height); the shell clips what overflows.
	Body string
	// Hints are the footer key hints, in order.
	Hints []key.Binding
	// Width and Height are the terminal size the shell must fit exactly.
	Width, Height int
}

// shellContentSize is the width and height available to spec.Body.
func shellContentSize(width, height int) (int, int) {
	return max(width-shellSidebarWidth-2*shellContentPadX, 1), max(height-shellChromeRows-shellContentPadTop, 1)
}

// renderShell renders the whole screen: exactly spec.Height rows, none wider
// than spec.Width.
func renderShell(theme adminTheme, spec shellSpec) string {
	width, height := max(spec.Width, shellSidebarWidth+1), max(spec.Height, shellChromeRows+1)
	bodyRows := height - shellChromeRows

	rule := lipgloss.NewStyle().Foreground(theme.borderColor).Render(strings.Repeat("─", width))
	lines := make([]string, 0, height)
	lines = append(lines, shellHeader(theme, spec, width), rule)
	lines = append(lines, shellBody(theme, spec, width, bodyRows)...)
	lines = append(lines, rule, shellFooter(theme, spec, width))
	return strings.Join(lines, "\n")
}

func shellBarStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(lipgloss.Color(paletteBarHex))
}

// shellFitBar clips line to width and pads it with the bar background.
func shellFitBar(line string, width int) string {
	line = lipgloss.NewStyle().MaxWidth(width).Render(line)
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += shellBarStyle().Render(strings.Repeat(" ", pad))
	}
	return line
}

func shellHeader(theme adminTheme, spec shellSpec, width int) string {
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	text := theme.text.Inherit(bar)

	crumbs := make([]string, len(spec.Breadcrumb))
	for i, crumb := range spec.Breadcrumb {
		if i == len(spec.Breadcrumb)-1 {
			crumbs[i] = text.Render(crumb)
		} else {
			crumbs[i] = muted.Render(crumb)
		}
	}
	left := bar.Render("  ") + strings.Join(crumbs, muted.Render(" › "))
	right := muted.Render("signed in as ") + text.Render(spec.Operator) + bar.Render("  ")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return shellFitBar(left, width)
	}
	return left + bar.Render(strings.Repeat(" ", gap)) + right
}

func shellFooter(theme adminTheme, spec shellSpec, width int) string {
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	text := theme.text.Inherit(bar)

	parts := make([]string, 0, len(spec.Hints))
	for _, hint := range spec.Hints {
		if !hint.Enabled() {
			continue
		}
		parts = append(parts, text.Render(hint.Help().Key)+muted.Render(" "+hint.Help().Desc))
	}
	return shellFitBar(bar.Render("  ")+strings.Join(parts, bar.Render("   ")), width)
}

func shellBody(theme adminTheme, spec shellSpec, width, rows int) []string {
	sidebar := shellSidebar(theme, spec.Active, rows)
	contentWidth := width - shellSidebarWidth
	inner, _ := shellContentSize(width, rows+shellChromeRows)
	bodyLines := strings.Split(spec.Body, "\n")

	lines := make([]string, rows)
	for i := range rows {
		content := ""
		if row := i - shellContentPadTop; row >= 0 && row < len(bodyLines) {
			content = lipgloss.NewStyle().MaxWidth(inner).Render(bodyLines[row])
		}
		pad := max(contentWidth-shellContentPadX-lipgloss.Width(content), 0)
		lines[i] = sidebar[i] + strings.Repeat(" ", shellContentPadX) + content + strings.Repeat(" ", pad)
	}
	return lines
}

// shellSidebar renders the sidebar column: exactly rows lines, each
// shellSidebarWidth wide, ending in the border column.
func shellSidebar(theme adminTheme, active shellNavID, rows int) []string {
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	border := lipgloss.NewStyle().Foreground(theme.borderColor).Background(lipgloss.Color(paletteBarHex)).Render("│")

	var content []string
	content = append(content, bar.Render(" ")+theme.title.Inherit(bar).Render("regixtry")+muted.Render(" admin"))
	for _, group := range shellNavGroups {
		content = append(content, "")
		if group.Title != "" {
			content = append(content, bar.Render(" ")+muted.Bold(true).Padding(0, 1).Render(group.Title))
		}
		for _, item := range group.Items {
			content = append(content, shellNavRow(theme, item, group.Title != "", item.ID == active))
		}
	}

	lines := make([]string, rows)
	for i := range rows {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		lines[i] = shellFitBar(line, shellSidebarWidth-1) + border
	}
	return lines
}

// shellNavRow renders one sidebar item: the active one is the theme's
// selected style (accent on the selected background) with the "▸" marker.
func shellNavRow(theme adminTheme, item shellNavItem, indented, active bool) string {
	bar := shellBarStyle()
	marker := "  "
	if active {
		marker = "▸ "
	}
	label := marker + item.Label
	if indented {
		label = "  " + label
	}
	row := theme.selected.Width(shellNavRowWidth)
	if !active {
		row = theme.text.Inherit(bar).Padding(0, 1).Width(shellNavRowWidth)
	}
	return bar.Render(" ") + row.Render(label)
}

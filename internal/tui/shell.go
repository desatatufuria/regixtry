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
	// shellHeaderRows is the header bar and its rule: the sidebar and the
	// content start on the row after them.
	shellHeaderRows = 2
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
	shellNavProjects         shellNavID = "projects"
	shellNavImageScanning    shellNavID = "image-scanning"
	shellNavSecretScanning   shellNavID = "secret-scanning"
	shellNavSigningPolicy    shellNavID = "signing-policy"
	shellNavLoginSecurity    shellNavID = "login-security"
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
		{shellNavProjects, "Projects"},
	}},
	{Title: "SECURITY", Items: []shellNavItem{
		{shellNavImageScanning, "Image scanning"},
		{shellNavSecretScanning, "Secret scanning"},
		{shellNavSigningPolicy, "Signing policy"},
		{shellNavLoginSecurity, "Login security"},
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
	// RepoAdmin, when set, is the repository the signed-in repository
	// administrator manages: the sidebar then shows only "Repository access"
	// and a note that global settings belong to global admins, never the
	// admin-only navigation.
	RepoAdmin string
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
		crumb = sanitizeDisplay(crumb)
		if i == len(spec.Breadcrumb)-1 {
			crumbs[i] = text.Render(crumb)
		} else {
			crumbs[i] = muted.Render(crumb)
		}
	}
	left := bar.Render("  ") + strings.Join(crumbs, muted.Render(" › "))
	right := muted.Render("signed in as ") + text.Render(sanitizeDisplay(spec.Operator)) + bar.Render("  ")

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
	sidebar := shellSidebar(theme, spec.Active, spec.RepoAdmin, rows)
	contentWidth := width - shellSidebarWidth
	inner, _ := shellContentSize(width, rows+shellChromeRows)
	bodyLines := strings.Split(spec.Body, "\n")

	lines := make([]string, rows)
	for i := range rows {
		content := ""
		if row := i - shellContentPadTop; row >= 0 && row < len(bodyLines) {
			content = lipgloss.NewStyle().MaxWidth(inner).Render(sanitizeFrame(bodyLines[row]))
		}
		pad := max(contentWidth-shellContentPadX-lipgloss.Width(content), 0)
		lines[i] = sidebar[i] + strings.Repeat(" ", shellContentPadX) + content + strings.Repeat(" ", pad)
	}
	return lines
}

// shellSidebar renders the sidebar column: exactly rows lines, each
// shellSidebarWidth wide, ending in the border column.
func shellSidebar(theme adminTheme, active shellNavID, repoAdmin string, rows int) []string {
	border := lipgloss.NewStyle().Foreground(theme.borderColor).Background(lipgloss.Color(paletteBarHex)).Render("│")

	var content []string
	if repoAdmin != "" {
		content = shellRepoAdminSidebar(theme, repoAdmin)
	} else {
		content = shellAdminSidebar(theme, active)
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

// shellRepoAdminSidebar is the sidebar of a repository administrator: the
// one screen they can use, and why nothing else is listed.
func shellRepoAdminSidebar(theme adminTheme, repository string) []string {
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	content := []string{
		bar.Render(" ") + theme.title.Inherit(bar).Render("regixtry") + muted.Render(" repository admin"),
		"",
		bar.Render(" ") + muted.Bold(true).Padding(0, 1).Render("ACCESS"),
		shellNavRow(theme, shellNavItem{shellNavRepositoryAccess, "Repository access"}, true, true),
		"",
	}
	for _, line := range grantWrap("You administer "+repository+". Global settings are visible to global admins only.", shellSidebarWidth-5) {
		content = append(content, bar.Render("  ")+muted.Render(line))
	}
	return content
}

// shellSidebarLine is one line of the admin sidebar under its brand row: a
// blank separator, a group title or a navigation item. The renderer and the
// mouse hit-test both walk shellSidebarLines, so a click lands on the row that
// was drawn.
type shellSidebarLine struct {
	Title    string
	Item     shellNavItem
	IsItem   bool
	Indented bool
}

func shellSidebarLines() []shellSidebarLine {
	var lines []shellSidebarLine
	for _, group := range shellNavGroups {
		lines = append(lines, shellSidebarLine{})
		if group.Title != "" {
			lines = append(lines, shellSidebarLine{Title: group.Title})
		}
		for _, item := range group.Items {
			lines = append(lines, shellSidebarLine{Item: item, IsItem: true, Indented: group.Title != ""})
		}
	}
	return lines
}

func shellAdminSidebar(theme adminTheme, active shellNavID) []string {
	bar := shellBarStyle()
	muted := theme.muted.Inherit(bar)
	content := []string{bar.Render(" ") + theme.title.Inherit(bar).Render("regixtry") + muted.Render(" admin")}
	for _, line := range shellSidebarLines() {
		switch {
		case line.IsItem:
			content = append(content, shellNavRow(theme, line.Item, line.Indented, line.Item.ID == active))
		case line.Title != "":
			content = append(content, bar.Render(" ")+muted.Bold(true).Padding(0, 1).Render(line.Title))
		default:
			content = append(content, "")
		}
	}
	return content
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

// shellCrumb builds a breadcrumb trail below the shell's root crumb: Access,
// Security and Operations name the sidebar group, then come the screen and
// anything deeper.
func shellCrumb(parts ...string) []string { return append([]string{"Admin"}, parts...) }

// adminContext is shellCrumb as the one-line context the workspace prints
// under the title.
func adminContext(parts ...string) string { return strings.Join(shellCrumb(parts...), " › ") }

// featureNavLabel is the sidebar label of the screen that owns a feature.
func featureNavLabel(feature string) string {
	switch feature {
	case trivyFeatureName:
		return "Image scanning"
	case gitleaksFeatureName:
		return "Secret scanning"
	case signingFeatureName:
		return "Signing policy"
	}
	return feature
}

// featureNavGroup is the sidebar group of the screen that owns a feature.
const featureNavGroup = "Security"

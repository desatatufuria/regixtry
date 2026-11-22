package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

var shellSizes = []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}}

func shellTestHints() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func shellTestSpec(w, h int, active shellNavID) shellSpec {
	bodyW, bodyH := shellContentSize(w, h)
	body := make([]string, bodyH+5) // deliberately taller than the content area
	for i := range body {
		body[i] = strings.Repeat("x", bodyW+10) // and wider
	}
	return shellSpec{
		Breadcrumb: []string{"Admin", "Overview"},
		Operator:   "admin",
		Active:     active,
		Body:       strings.Join(body, "\n"),
		Hints:      shellTestHints(),
		Width:      w,
		Height:     h,
	}
}

func TestShellFitsEveryTerminalSize(t *testing.T) {
	t.Parallel()
	for _, size := range shellSizes {
		out := ansi.Strip(renderShell(newAdminTheme(), shellTestSpec(size.w, size.h, shellNavOverview)))
		lines := strings.Split(out, "\n")
		if len(lines) != size.h {
			t.Errorf("%dx%d: %d rows, want exactly %d", size.w, size.h, len(lines), size.h)
		}
		for i, line := range lines {
			if got := ansi.StringWidth(line); got != size.w {
				t.Errorf("%dx%d row %d is %d columns wide, want %d: %q", size.w, size.h, i, got, size.w, line)
			}
		}
	}
}

func TestShellHeaderShowsBreadcrumbAndOperator(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(renderShell(newAdminTheme(), shellTestSpec(150, 24, shellNavOverview)))
	header := strings.Split(out, "\n")[0]
	if !strings.HasPrefix(header, "  Admin › Overview") {
		t.Errorf("header %q does not start with the breadcrumb", header)
	}
	if !strings.HasSuffix(strings.TrimRight(header, " "), "signed in as admin") {
		t.Errorf("header %q does not end with the operator", header)
	}
}

func TestShellSidebarGroupsAndLabelsAreExact(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(renderShell(newAdminTheme(), shellTestSpec(150, 24, shellNavOverview)))
	var sidebar []string
	for _, line := range strings.Split(out, "\n")[2:] {
		column := []rune(line)
		if len(column) < shellSidebarWidth || column[shellSidebarWidth-1] != '│' {
			continue
		}
		if text := strings.TrimSpace(string(column[:shellSidebarWidth-1])); text != "" {
			sidebar = append(sidebar, text)
		}
	}
	want := []string{
		"regixtry admin",
		"▸ Overview",
		"ACCESS", "Users", "Robots", "Repository access",
		"SECURITY", "Image scanning", "Secret scanning", "Signing policy",
		"OPERATIONS", "Scan runs", "Secret findings", "Update channel",
	}
	if strings.Join(sidebar, "|") != strings.Join(want, "|") {
		t.Errorf("sidebar = %q\nwant      %q", sidebar, want)
	}
}

func TestShellMarksOnlyTheActiveItem(t *testing.T) {
	t.Parallel()
	for _, item := range shellNavItems() {
		out := ansi.Strip(renderShell(newAdminTheme(), shellTestSpec(150, 24, item.ID)))
		if got := strings.Count(out, "▸"); got != 1 {
			t.Errorf("active %q: %d markers, want 1", item.ID, got)
		}
		if !strings.Contains(out, "▸ "+item.Label) {
			t.Errorf("active %q: marker is not on %q", item.ID, item.Label)
		}
	}
}

func TestShellActiveItemUsesTheSelectedStyle(t *testing.T) {
	t.Parallel()
	theme := newAdminTheme()
	out := renderShell(theme, shellTestSpec(150, 24, shellNavUsers))
	selected := theme.selected.Width(shellNavRowWidth).Render("  ▸ Users")
	if !strings.Contains(out, selected) {
		t.Errorf("the active row is not rendered with theme.selected")
	}
}

func TestShellFooterShowsTheHints(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(renderShell(newAdminTheme(), shellTestSpec(150, 24, shellNavOverview)))
	lines := strings.Split(out, "\n")
	footer := strings.TrimSpace(lines[len(lines)-1])
	if footer != "↑↓ move   enter open   esc back" {
		t.Errorf("footer = %q", footer)
	}
}

func TestShellPlacesTheBodyInTheContentArea(t *testing.T) {
	t.Parallel()
	spec := shellTestSpec(150, 24, shellNavOverview)
	spec.Body = "Overview\nsubtitle"
	lines := strings.Split(ansi.Strip(renderShell(newAdminTheme(), spec)), "\n")
	// header, rule, one blank content row, then the body.
	if got := string([]rune(lines[3])[shellSidebarWidth+shellContentPadX:]); !strings.HasPrefix(got, "Overview") {
		t.Errorf("first body row = %q", got)
	}
	if got := string([]rune(lines[4])[shellSidebarWidth+shellContentPadX:]); !strings.HasPrefix(got, "subtitle") {
		t.Errorf("second body row = %q", got)
	}
}

func TestShellContentSizeLeavesRoomForTheChrome(t *testing.T) {
	t.Parallel()
	w, h := shellContentSize(150, 24)
	if w != 150-shellSidebarWidth-2*shellContentPadX || h != 24-shellChromeRows-shellContentPadTop {
		t.Errorf("shellContentSize(150, 24) = %d, %d", w, h)
	}
}

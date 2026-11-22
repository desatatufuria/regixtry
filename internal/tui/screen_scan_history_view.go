package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

// activeKind is the kind of the tab the history is on.
func (s scanHistoryScreen) activeKind() adminScanHistoryTabKind {
	if len(s.modal.Tabs) == 0 {
		return adminScanHistoryTabVulnerabilities
	}
	return s.modal.Tabs[boundedIndex(s.modal.ActiveTab, len(s.modal.Tabs))].Kind
}

func (s scanHistoryScreen) Keys() screenKeys {
	short := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	}
	if s.activeKind() == adminScanHistoryTabVulnerabilities {
		short = append(short, key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open advisory")))
	}
	short = append(short,
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "run")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "vulnerabilities / leaks")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	)
	return screenKeys{short: short}
}

// View satisfies adminScreen for the router's broadcast; the workspace draws
// the history itself through viewFull so it can show the status line.
func (s scanHistoryScreen) View(theme adminTheme, env screenEnv) screenFrame {
	return s.viewFull(theme, env, "")
}

// breadcrumb names where the history was opened from.
func (s scanHistoryScreen) breadcrumb() ([]string, shellNavID) {
	repo := adminFirstNonEmpty(s.modal.Repository, "unknown")
	switch s.returnTo {
	case screenAdminSecretFindings:
		return shellCrumb("Operations", "Secret findings", repo), shellNavSecretFindings
	case screenSecurityTrivyRepos:
		return shellCrumb("Security", "Image scanning", repo), shellNavImageScanning
	}
	return shellCrumb("Operations", "Scan runs", repo), shellNavScanRuns
}

func (s scanHistoryScreen) viewFull(theme adminTheme, env screenEnv, status string) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH, listW, panelW := scanBoardSize(env)
	now := env.now()
	m := s.modal

	// title, tabs, then the columns; the table box, its footnote.
	columnsH := max(bodyH-2, 3)
	visible := max(columnsH-1-scanTableChromeRows, 1)

	table, footnote := s.renderList(theme, listW, visible, status)
	var panel string
	if run, ok := s.currentRun(); ok {
		panel = scanRunPanel(theme, run, now, nil, panelW, columnsH)
	} else {
		panel = scanEmptyPanel(theme, "No run to show.", panelW, columnsH)
	}
	title := scanTitleLine(theme, adminFirstNonEmpty(m.Repository, "unknown"), s.position(now), bodyW)
	body := title + "\n" + s.tabBar(theme) + "\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, table+"\n"+footnote, strings.Repeat(" ", scanColGap), panel)

	crumb, nav := s.breadcrumb()
	return screenFrame{
		Context: strings.Join(crumb, " › "),
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: crumb,
			Operator:   env.Session.Username,
			Active:     nav,
			Body:       body,
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
}

// currentRun is the run on screen: its detail once read, else the list entry.
func (s scanHistoryScreen) currentRun() (ports.ScanRun, bool) {
	if s.modal.Detail.Run.ID != "" {
		return s.modal.Detail.Run, true
	}
	if len(s.modal.Runs) == 0 {
		return ports.ScanRun{}, false
	}
	return s.modal.Runs[boundedIndex(s.modal.Cursor, len(s.modal.Runs))], true
}

// position is "Run 2 of 17, 21 Nov 09:12".
func (s scanHistoryScreen) position(now time.Time) string {
	m := s.modal
	switch {
	case m.Error != "" && len(m.Runs) == 0:
		return "Unavailable"
	case len(m.Runs) == 0 && m.Loading:
		return "Reading…"
	case len(m.Runs) == 0:
		return "No runs"
	}
	cursor := boundedIndex(m.Cursor, len(m.Runs))
	run := m.Runs[cursor]
	at, inProgress := effectiveScanRunTime(run)
	text := fmt.Sprintf("Run %d of %d, %s", cursor+1, len(m.Runs), scanClock(at, now))
	if inProgress {
		text += " (in progress)"
	}
	return text
}

// tabBar is "Vulnerabilities (14)  Leaks (2)" with the active tab selected.
func (s scanHistoryScreen) tabBar(theme adminTheme) string {
	m := s.modal
	if len(m.Tabs) == 0 {
		return theme.muted.Render("No tabs available.")
	}
	active := boundedIndex(m.ActiveTab, len(m.Tabs))
	labels := make([]string, len(m.Tabs))
	for i, tab := range m.Tabs {
		label := tab.Label
		if count, ok := s.tabCount(tab.Kind); ok {
			label = fmt.Sprintf("%s (%d)", label, count)
		}
		if i == active {
			labels[i] = theme.selected.Render(label)
		} else {
			labels[i] = theme.muted.Render(" " + label + " ")
		}
	}
	return strings.Join(labels, " ")
}

// tabCount is how many entries a tab holds, once they are read.
func (s scanHistoryScreen) tabCount(kind adminScanHistoryTabKind) (int, bool) {
	m := s.modal
	if kind == adminScanHistoryTabLeaks {
		return len(m.Secrets), m.Detail.Run.ID != "" && !m.Loading
	}
	return len(m.Detail.Findings), m.Detail.Run.ID != ""
}

// renderList renders the active tab's table box and the line under it.
func (s scanHistoryScreen) renderList(theme adminTheme, width, visible int, status string) (string, string) {
	m := s.modal
	inner := max(width-4, 1)
	cursor := m.FindingCursor

	var specs []scanColSpec
	var rows [][]scanCell
	hint := ""
	switch s.activeKind() {
	case adminScanHistoryTabLeaks:
		locW := len("LOCATION")
		for _, f := range m.Secrets {
			locW = max(locW, min(lipgloss.Width(secretFindingLocation(f)), 28))
		}
		locW = max(min(locW, max(width-4, 1)-scanMarkerWidth-2*scanColGap-18-scanDescriptionMin), len("LOCATION"))
		specs = []scanColSpec{
			{title: "RULE", fixed: 18},
			{title: "LOCATION", fixed: locW},
			{title: "DESCRIPTION", flex: 1, min: 8},
		}
		for _, f := range m.Secrets {
			desc := strings.TrimSpace(f.Description)
			if len(f.Tags) > 0 {
				desc = strings.TrimSpace(desc + " (" + strings.Join(f.Tags, ", ") + ")")
			}
			rows = append(rows, []scanCell{
				{adminFirstNonEmpty(f.RuleID, "unknown"), theme.text},
				{secretFindingLocation(f), theme.text},
				{desc, theme.muted},
			})
		}
		hint = "The secret value itself is never shown."
	default:
		specs = []scanColSpec{
			{title: "SEVERITY", fixed: 9},
			{title: "FINDING", fixed: 16},
			{title: "PACKAGE", flex: 1, min: 8},
			{title: "INSTALLED", fixed: 10},
			{title: "FIXED", fixed: 10},
		}
		for _, f := range m.Detail.Findings {
			severity := strings.ToUpper(adminFirstNonEmpty(f.Severity, "unknown"))
			rows = append(rows, []scanCell{
				{severity, severityStyle(severity)},
				{adminFirstNonEmpty(f.VulnerabilityID, "unknown"), theme.text},
				{adminFirstNonEmpty(f.PackageName, "unknown"), theme.text},
				{adminFirstNonEmpty(f.InstalledVersion, "unknown"), theme.muted},
				{adminFirstNonEmpty(f.FixedVersion, "n/a"), theme.muted},
			})
		}
		hint = "enter opens the advisory of the selected finding."
	}
	table := renderScanTable(theme, specs, rows, cursor, visible, width, s.notice(theme, inner))
	foot := theme.muted.Render(grantTruncate(hint, width))
	if strings.TrimSpace(status) != "" && !m.Loading && m.Error == "" {
		foot = theme.warning.Render(grantTruncate(status, width))
	}
	return table, foot
}

// notice is what the list says instead of rows.
func (s scanHistoryScreen) notice(theme adminTheme, inner int) []string {
	m := s.modal
	leaks := s.activeKind() == adminScanHistoryTabLeaks
	switch {
	case strings.TrimSpace(m.Error) != "":
		return []string{theme.error.Render(grantTruncate("Could not read scan history: "+m.Error, inner))}
	case len(m.Runs) == 0 && m.Loading:
		return []string{theme.muted.Render("Reading scan history…")}
	case len(m.Runs) == 0:
		return []string{theme.muted.Render("No scans recorded for this repository.")}
	case leaks && m.Loading:
		return []string{theme.muted.Render("Reading secret findings…")}
	case !leaks && m.Detail.Run.ID == "":
		return []string{theme.muted.Render("Reading this run…")}
	case leaks:
		return []string{theme.muted.Render("No secret findings recorded for this run.")}
	}
	return []string{theme.muted.Render("No findings recorded for this run.")}
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

// overridesView is what differs between the three override screens when they
// are drawn: the heading, where they sit in the shell and what the footer says.
type overridesView struct {
	title    string
	subtitle string
	crumb    []string
	nav      shellNavID
	// tabs is the Config / Repositories strip of Trivy; "" on the other two.
	tabs string
	foot string
	keys screenKeys
	// history is true when Enter opens the scan history of the repository.
	history bool
}

func overridesShell(theme adminTheme, env screenEnv, v overridesView, body string, hints screenKeys) string {
	return renderShell(theme, shellSpec{
		Breadcrumb: shellCrumb(v.crumb...),
		Operator:   env.Session.Username,
		Active:     v.nav,
		Body:       body,
		Hints:      hints.short,
		Width:      env.Layout.Width,
		Height:     env.Layout.Height,
	})
}

// frame draws the screen inside the shell, with the editor over it while one is
// open.
func (l overridesList) frame(theme adminTheme, env screenEnv, v overridesView) screenFrame {
	bodyW, bodyH, listW, panelW := scanBoardSize(env)

	head := l.headLines(theme, v, bodyW)
	foot := l.footLines(theme, v, listW)
	visible := max(bodyH-len(head)-len(foot)-scanTableChromeRows, 1)
	panelH := max(bodyH-len(head), 3)

	table := l.table(theme, env, v, listW, visible)
	panel := l.panel(theme, env, v, panelW, panelH)
	columns := lipgloss.JoinHorizontal(lipgloss.Top, table+"\n"+strings.Join(foot, "\n"), strings.Repeat(" ", scanColGap), panel)
	body := strings.Join(head, "\n") + "\n" + columns

	hints := v.keys
	frame := screenFrame{Context: strings.Join(v.crumb, " › "), Full: true}
	switch {
	case l.editor.confirm.Active():
		frame.Overlay = l.editor.confirm.view(theme)
		hints = l.editorHints()
	case l.editor.Active():
		frame.Overlay = renderOverrideEditor(theme, env, l.editor)
		hints = l.editorHints()
	}
	frame.Body = overridesShell(theme, env, v, body, hints)
	return frame
}

// headLines is the title, the notice and, on Trivy, the tab strip above the
// columns.
func (l overridesList) headLines(theme adminTheme, v overridesView, bodyW int) []string {
	head := []string{
		scanTitleLine(theme, v.title, v.subtitle, bodyW),
		l.noticeLine(theme, bodyW),
	}
	if v.tabs != "" {
		head = append(head, v.tabs)
	}
	return head
}

// footLines is the one or two muted lines under the table.
func (l overridesList) footLines(theme adminTheme, v overridesView, listW int) []string {
	foot := wrapStyled(theme.muted, v.foot, max(listW-4, 1))
	if len(foot) > 2 {
		foot = foot[:2]
	}
	return foot
}

// mouseRows is where the table drew its repository rows.
func (l overridesList) mouseRows(theme adminTheme, env screenEnv, v overridesView) mouseRows {
	if len(l.rows) == 0 {
		return mouseRows{}
	}
	bodyW, bodyH, listW, _ := scanBoardSize(env)
	head := len(l.headLines(theme, v, bodyW))
	visible := max(bodyH-head-len(l.footLines(theme, v, listW))-scanTableChromeRows, 1)
	start, end := grantWindow(boundedIndex(l.cursor, len(l.rows)), len(l.rows), visible)
	x, y := shellContentOrigin()
	return mouseRows{X: x, Y: y + head + scanTableChromeRows - 1, Width: listW, Count: end - start, First: start}
}

// noticeLine is the failure or outcome of the last action, else how many
// repositories there are.
func (l overridesList) noticeLine(theme adminTheme, width int) string {
	switch {
	case l.loaded && l.err != "":
		return theme.error.UnsetBold().Render(fitText("Could not refresh: "+l.err, width))
	case l.scansErr != "" && l.loaded:
		return theme.error.UnsetBold().Render(fitText("Could not read the scans: "+l.scansErr, width))
	case l.note != "":
		return theme.success.UnsetBold().Render(fitText(l.note, width))
	case l.reading:
		return theme.warning.UnsetBold().Render("Reading…")
	case !l.loaded:
		return ""
	}
	overridden := 0
	for _, row := range l.rows {
		if row.HasOverride {
			overridden++
		}
	}
	return theme.muted.Render(fitText(fmt.Sprintf("%s, %d with an override", plural(len(l.rows), "repository"), overridden), width))
}

// listNotice is what the table says instead of rows.
func (l overridesList) listNotice(theme adminTheme, inner int) []string {
	switch {
	case !l.loaded && l.err != "":
		return []string{
			theme.error.UnsetBold().Render(grantTruncate("Could not read the overrides: "+l.err, inner)),
			theme.muted.Render("Press r to try again."),
		}
	case !l.loaded:
		return []string{theme.muted.Render("Reading repositories…")}
	}
	return []string{theme.muted.Render("No repositories yet.")}
}

func (l overridesList) table(theme adminTheme, env screenEnv, v overridesView, width, visible int) string {
	now := env.now()
	var specs []scanColSpec
	if l.feature == trivyFeatureName {
		specs = []scanColSpec{
			{title: "REPOSITORY", flex: 1, min: 8},
			{title: "CRIT", fixed: 4},
			{title: "HIGH", fixed: 4},
			{title: "LAST SCAN", fixed: 15},
			{title: "STATUS", fixed: 9},
			{title: "ENABLED", fixed: 7},
		}
	} else {
		specs = []scanColSpec{
			{title: "REPOSITORY", flex: 1, min: 8},
			{title: "STATUS", fixed: 9},
			{title: "ENABLED", fixed: 7},
			{title: "DETAIL", fixed: 16},
			{title: "UPDATED", fixed: 11},
		}
	}
	rows := make([][]scanCell, len(l.rows))
	for i, row := range l.rows {
		status, enabled, detail, updated := scanCell{"inherited", theme.muted}, scanCell{"—", theme.muted}, scanCell{securityInheritedDetail(l.feature), theme.muted}, scanCell{"-", theme.muted}
		if row.HasOverride {
			status = scanCell{"override", theme.text}
			enabled = scanCell{"yes", theme.text}
			if !row.Detail.Enabled {
				enabled = scanCell{"no", theme.warning.UnsetBold()}
			}
			detail = scanCell{securityOverrideDetail(l.feature, row.Detail), theme.text}
			updated = scanCell{securityOverrideDate(row.Detail.UpdatedAt, now), theme.text}
		}
		repo := scanCell{row.Repository, theme.text}
		if l.feature == trivyFeatureName {
			crit, high, last := scanCell{"-", theme.muted}, scanCell{"-", theme.muted}, scanCell{"never", theme.muted}
			if scan, ok := l.scans[row.Repository]; ok {
				run := scan.Run
				crit = scanCell{scanCount(run, run.Critical), scanCountStyle(theme, run, run.Critical, theme.severityCritical)}
				high = scanCell{scanCount(run, run.High), scanCountStyle(theme, run, run.High, theme.severityHigh)}
				last = scanCell{lastScanText(run, now), lastScanStyle(theme, run)}
			}
			rows[i] = []scanCell{repo, crit, high, last, status, enabled}
			continue
		}
		rows[i] = []scanCell{repo, status, enabled, detail, updated}
	}
	return renderScanTable(theme, specs, rows, l.cursor, visible, width, l.listNotice(theme, max(width-4, 1)))
}

// panel is the selected repository: what its override sets (or that it
// inherits), its latest scan on Trivy, and the keys that act on it.
func (l overridesList) panel(theme adminTheme, env screenEnv, v overridesView, width, height int) string {
	row, ok := l.selectedRow()
	if !ok {
		return scanEmptyPanel(theme, "Select a repository to see its override.", width, height)
	}
	inner := max(width-4, 1)
	now := env.now()

	subtitle := "inherits the global settings"
	if l.feature == signingFeatureName {
		subtitle = "inherits the global policy"
	}
	if row.HasOverride {
		subtitle = "override"
	}
	var head []string
	for _, line := range overridesWrap(row.Repository, inner, 2) {
		head = append(head, theme.subheading.Render(line))
	}
	head = append(head, theme.muted.Render(subtitle))

	override := l.overrideSection(theme, row, now, width)
	actions := []string{scanSectionTitle(theme, "ACTIONS"), scanAction(theme, "Edit override", "o", width)}
	if v.history {
		actions = append(actions, scanAction(theme, "Scan history", "enter", width))
	}
	actions = append(actions, scanAction(theme, "Refresh", "r", width))
	sections := [][]string{override, actions}
	// The latest scan goes between them when the panel has the room: the
	// actions and the override matter more than a second look at what the
	// table already counts.
	if scan, ok := l.scans[row.Repository]; ok && l.feature == trivyFeatureName {
		for _, section := range [][]string{l.scanSection(theme, scan, now, width), l.compactScanSection(theme, scan, width)} {
			if len(head)+len(override)+len(section)+len(actions) <= max(height-2, 1) {
				sections = [][]string{override, section, actions}
				break
			}
		}
	}
	return scanPanel(theme, head, sections, nil, width, height)
}

// overrideSection lists what the stored override sets, from the fields it
// really carries; a repository without one says so.
func (l overridesList) overrideSection(theme adminTheme, row featureOverrideRow, now time.Time, width int) []string {
	title := scanSectionTitle(theme, "OVERRIDE")
	if !row.HasOverride {
		inner := max(width-4, 1)
		lines := []string{title}
		return append(lines, wrapStyled(theme.muted, "Uses the global settings. o creates an override for this repository.", inner)...)
	}
	d := row.Detail
	flag := func(on bool, onText, offText string) (string, lipgloss.Style) {
		if on {
			return onText, theme.success.UnsetBold()
		}
		return offText, theme.warning.UnsetBold()
	}
	kv := func(label, value string, style lipgloss.Style) string {
		return scanKV(theme, label, value, style, width)
	}
	orDash := func(value string) string { return adminFirstNonEmpty(strings.TrimSpace(value), "-") }

	lines := []string{title}
	switch l.feature {
	case signingFeatureName:
		text, style := flag(d.Enabled, "enforced", "not enforced")
		lines = append(lines,
			kv("Pull-time gate", text, style),
			kv("Unsigned self-read", normalizeUnsignedSelfRead(d.UnsignedSelfRead), theme.text),
			kv("Trusted keys", fmt.Sprintf("%d", len(d.TrustedPublicKeys)), theme.text),
			kv("Identities", fmt.Sprintf("%d", len(d.TrustedIdentities)), theme.text),
		)
	case gitleaksFeatureName:
		text, style := flag(d.Enabled, "on", "off")
		lines = append(lines, kv("Scanning", text, style), kv("Config path", orDash(d.ConfigPath), theme.text))
	default:
		text, style := flag(d.Enabled, "on", "off")
		lines = append(lines,
			kv("Scanning", text, style),
			kv("Ignore file", orDash(d.IgnoreFilePath), theme.text),
			kv("Ignore policy", orDash(d.IgnorePolicyPath), theme.text),
		)
	}
	return append(lines, kv("Updated", securityOverrideDate(d.UpdatedAt, now), theme.text))
}

// scanSection is the repository's latest scan (Trivy), in full.
func (l overridesList) scanSection(theme adminTheme, scan ports.RepositoryScanSummary, now time.Time, width int) []string {
	run := scan.Run
	kv := func(label, value string, style lipgloss.Style) string {
		return scanKV(theme, label, value, style, width)
	}
	lines := []string{
		scanSectionTitle(theme, "LATEST SCAN"),
		kv("Reference", adminFirstNonEmpty(run.RequestedRef, "-"), theme.text),
		kv("Status", adminFirstNonEmpty(run.Status, "unknown"), scanStatusStyle(theme, run.Status)),
		kv("Critical", scanCount(run, run.Critical), scanCountStyle(theme, run, run.Critical, theme.severityCritical)),
		kv("High", scanCount(run, run.High), scanCountStyle(theme, run, run.High, theme.severityHigh)),
		kv("Runs", fmt.Sprintf("%d", scan.RunCount), theme.text),
		kv("Scanned", scanClock(scanRunTime(run), now), theme.text),
	}
	if run.Status == ports.ScanRunStatusFailed && strings.TrimSpace(run.Error) != "" {
		lines = append(lines, kv("Error", strings.TrimSpace(run.Error), theme.error))
	}
	return lines
}

// compactScanSection is what the panel keeps of the scan when it has no room
// for the full one: the reference and the runs (the list has the rest).
func (l overridesList) compactScanSection(theme adminTheme, scan ports.RepositoryScanSummary, width int) []string {
	return []string{
		scanSectionTitle(theme, "LATEST SCAN"),
		scanKV(theme, "Reference", adminFirstNonEmpty(scan.Run.RequestedRef, "-"), theme.text, width),
		scanKV(theme, "Runs", fmt.Sprintf("%d", scan.RunCount), theme.text, width),
	}
}

// lastScanText is the scan result of the list: "ok 3 h ago", "failed 2 d ago",
// "running". A scan in progress has no age yet.
func lastScanText(run ports.ScanRun, now time.Time) string {
	label := adminFirstNonEmpty(run.Status, "unknown")
	switch run.Status {
	case ports.ScanRunStatusCompleted:
		label = "ok"
	case ports.ScanRunStatusRunning, ports.ScanRunStatusQueued:
		return label
	}
	at := scanRunTime(run)
	if at.IsZero() {
		return label
	}
	return label + " " + scanAge(at, now)
}

func lastScanStyle(theme adminTheme, run ports.ScanRun) lipgloss.Style {
	switch run.Status {
	case ports.ScanRunStatusFailed:
		return theme.error
	case ports.ScanRunStatusCompleted:
		return theme.text
	}
	return scanStatusStyle(theme, run.Status)
}

// scanAge is how long ago at was: hours, days or years, never seconds.
func scanAge(at, now time.Time) string {
	d := now.Sub(at)
	switch {
	case d < time.Hour:
		return "<1 h ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d d ago", int(d/(24*time.Hour)))
	}
	return fmt.Sprintf("%d y ago", int(d/(365*24*time.Hour)))
}

// scanRunTime is when a run finished, else when it was created.
func scanRunTime(run ports.ScanRun) time.Time {
	if run.FinishedAt != nil {
		return *run.FinishedAt
	}
	return run.CreatedAt
}

// overridesWrap breaks text into at most maxLines lines of width columns, cutting
// inside a word when it must (a repository name has no spaces); the last line
// ends in an ellipsis when text does not fit.
func overridesWrap(text string, width, maxLines int) []string {
	width = max(width, 1)
	var lines []string
	var current []rune
	flush := func() {
		lines = append(lines, string(current))
		current = nil
	}
	for _, r := range text {
		if lipgloss.Width(string(current)+string(r)) > width {
			flush()
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		flush()
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = fitText(lines[maxLines-1]+"…", width)
	}
	return lines
}

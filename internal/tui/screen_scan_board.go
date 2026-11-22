package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

// The shared pieces of the three scan screens (Scan runs, Secret findings and
// the scan history of one repository): all of them are a table on the left
// and a detail panel on the right, drawn inside the application shell.
const (
	// scanPanelMaxWidth is the board's 340px detail panel at about 8.5px a
	// column.
	scanPanelMaxWidth = 40
	scanColGap        = 2
	// scanTableChromeRows is the table box's border (2) and header (1).
	scanTableChromeRows = 3
	// scanMarkerWidth is the "▸ " selection marker in front of a row.
	scanMarkerWidth = 2
	// scanDescriptionMin is the least a description column is left with.
	scanDescriptionMin = 12
)

// scanBoardSize splits the shell's content area into the table's width and the
// panel's width.
func scanBoardSize(env screenEnv) (bodyW, bodyH, listW, panelW int) {
	bodyW, bodyH = shellContentSize(env.Layout.Width, env.Layout.Height)
	panelW = min(scanPanelMaxWidth, max(bodyW/3, 1))
	listW = max(bodyW-panelW-scanColGap, 1)
	return bodyW, bodyH, listW, panelW
}

// scanColSpec describes one column: a fixed width, or a flexible one that
// takes a share (flex) of what the fixed columns leave, never less than min.
type scanColSpec struct {
	title string
	fixed int
	flex  int
	min   int
}

// scanColumnWidths resolves specs to widths inside a table box of the given
// outer width.
func scanColumnWidths(boxWidth int, specs []scanColSpec) []int {
	inner := max(boxWidth-4, 1)
	room := inner - scanMarkerWidth - scanColGap*(len(specs)-1)
	totalFlex := 0
	for _, spec := range specs {
		if spec.flex > 0 {
			totalFlex += spec.flex
		} else {
			room -= spec.fixed
		}
	}
	widths := make([]int, len(specs))
	for i, spec := range specs {
		switch {
		case spec.flex > 0:
			widths[i] = max(room*spec.flex/totalFlex, spec.min, 1)
		default:
			widths[i] = spec.fixed
		}
	}
	return widths
}

// scanCell is one table cell: its text and the style a row that is not
// selected draws it in.
type scanCell struct {
	text  string
	style lipgloss.Style
}

func scanPad(text string, width int) string {
	text = grantTruncate(text, width)
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// renderScanTable renders the table box: exactly visible+scanTableChromeRows
// lines, the cursor row always inside the window. notice lines replace the
// rows while there are none.
func renderScanTable(theme adminTheme, specs []scanColSpec, rows [][]scanCell, cursor, visible, boxWidth int, notice []string) string {
	widths := scanColumnWidths(boxWidth, specs)
	gap := strings.Repeat(" ", scanColGap)

	titles := make([]string, len(specs))
	for i, spec := range specs {
		titles[i] = scanPad(spec.title, widths[i])
	}
	lines := []string{theme.muted.Bold(true).Render("  " + strings.Join(titles, gap))}
	if len(rows) == 0 {
		lines = append(lines, notice...)
	}
	cursor = boundedIndex(cursor, len(rows))
	start, end := grantWindow(cursor, len(rows), visible)
	for i := start; i < end; i++ {
		cells := rows[i]
		if i == cursor {
			plain := make([]string, len(cells))
			for c, cell := range cells {
				plain[c] = scanPad(cell.text, widths[c])
			}
			lines = append(lines, theme.selected.Padding(0).Render("▸ "+strings.Join(plain, gap)))
			continue
		}
		styled := make([]string, len(cells))
		for c, cell := range cells {
			text := grantTruncate(cell.text, widths[c])
			styled[c] = cell.style.Render(text) + strings.Repeat(" ", max(widths[c]-lipgloss.Width(text), 0))
		}
		lines = append(lines, "  "+strings.Join(styled, gap))
	}
	return scanBox(theme, lines, boxWidth, visible+1)
}

// scanBox draws lines in a bordered box of exactly innerRows+2 rows and
// boxWidth columns; a line that does not fit is cut, never wrapped.
func scanBox(theme adminTheme, lines []string, boxWidth, innerRows int) string {
	inner := max(boxWidth-4, 1)
	innerRows = max(innerRows, 1)
	if len(lines) > innerRows {
		lines = lines[:innerRows]
	}
	cut := lipgloss.NewStyle().MaxWidth(inner)
	out := make([]string, innerRows)
	for i := range out {
		if i < len(lines) {
			out[i] = cut.Render(lines[i])
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.borderColor).
		Padding(0, 1).
		Width(boxWidth).
		Render(strings.Join(out, "\n"))
}

// scanPanel renders a detail panel of exactly height rows: the head lines,
// then the sections, the tail last. Blank rows separate the sections when
// there is room for them; otherwise the lines at the bottom are the ones cut.
func scanPanel(theme adminTheme, head []string, sections [][]string, tail []string, width, height int) string {
	innerH := max(height-2, 1)
	flat := func(spaced bool) []string {
		lines := append([]string(nil), head...)
		for _, section := range sections {
			if spaced {
				lines = append(lines, "")
			}
			lines = append(lines, section...)
		}
		if len(tail) > 0 {
			if spaced {
				lines = append(lines, "")
			}
			lines = append(lines, tail...)
		}
		return lines
	}
	lines := flat(true)
	if len(lines) > innerH {
		lines = flat(false)
	}
	return scanBox(theme, lines, width, innerH)
}

func scanSectionTitle(theme adminTheme, title string) string {
	return theme.muted.Bold(true).Render(title)
}

// scanKV is one "label ........ value" row of a panel.
func scanKV(theme adminTheme, label, value string, valueStyle lipgloss.Style, width int) string {
	inner := max(width-4, 1)
	left := theme.muted.Render(label)
	room := max(inner-lipgloss.Width(label)-1, 1)
	right := valueStyle.Render(grantTruncate(value, room))
	gap := max(inner-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// scanAction is one "label ........ key" row of a panel's ACTIONS section.
func scanAction(theme adminTheme, label, keyName string, width int) string {
	inner := max(width-4, 1)
	hint := theme.muted.Render(keyName)
	text := theme.text.Render(grantTruncate(label, max(inner-lipgloss.Width(hint)-1, 1)))
	return text + strings.Repeat(" ", max(inner-lipgloss.Width(text)-lipgloss.Width(hint), 1)) + hint
}

// scanTitleLine is a screen's title with its muted note on the right.
func scanTitleLine(theme adminTheme, title, note string, width int) string {
	right := theme.muted.Render(note)
	left := theme.subheading.MaxWidth(max(width-lipgloss.Width(right)-2, 1)).Render(title)
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

// ---- values ----

// scanRunStart is when a run started; a run still queued has no start.
func scanRunStart(run ports.ScanRun) time.Time {
	if run.StartedAt != nil {
		return *run.StartedAt
	}
	return time.Time{}
}

// scanClock is a time as "21 Nov 09:12" (UTC), with the year instead of the
// clock once it is more than a year old.
func scanClock(at, now time.Time) string {
	if at.IsZero() {
		return "-"
	}
	at = at.UTC()
	if now.Sub(at) > 365*24*time.Hour {
		return at.Format("2 Jan 2006")
	}
	return at.Format("2 Jan 15:04")
}

func scanClockUTC(at *time.Time, now time.Time) string {
	if at == nil || at.IsZero() {
		return "-"
	}
	return scanClock(*at, now) + " UTC"
}

// scanTook is how long a finished run took.
func scanTook(run ports.ScanRun) string {
	if run.StartedAt == nil || run.FinishedAt == nil || run.FinishedAt.Before(*run.StartedAt) {
		return "-"
	}
	d := run.FinishedAt.Sub(*run.StartedAt)
	switch {
	case d < time.Minute:
		return "<1 min"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	case d < 24*time.Hour:
		hours, minutes := int(d/time.Hour), int(d%time.Hour/time.Minute)
		if minutes == 0 {
			return fmt.Sprintf("%d h", hours)
		}
		return fmt.Sprintf("%d h %d min", hours, minutes)
	}
	days, hours := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour)
	if hours == 0 {
		return fmt.Sprintf("%d d", days)
	}
	return fmt.Sprintf("%d d %d h", days, hours)
}

// scanShortDigest is "sha256:9c41…e0d2".
func scanShortDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return "-"
	}
	if len(digest) <= 19 {
		return digest
	}
	return digest[:11] + "…" + digest[len(digest)-4:]
}

func scanStatusStyle(theme adminTheme, status string) lipgloss.Style {
	switch status {
	case ports.ScanRunStatusCompleted:
		return theme.success
	case ports.ScanRunStatusFailed:
		return theme.error
	case ports.ScanRunStatusRunning, ports.ScanRunStatusQueued:
		return theme.accent
	case ports.ScanRunStatusSkipped:
		return theme.muted
	}
	return theme.text
}

// scanCount is a finding count; only a completed run has one.
func scanCount(run ports.ScanRun, n int) string {
	if run.Status != ports.ScanRunStatusCompleted {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}

func scanCountStyle(theme adminTheme, run ports.ScanRun, n int, hot lipgloss.Style) lipgloss.Style {
	if run.Status != ports.ScanRunStatusCompleted || n == 0 {
		return theme.muted
	}
	return hot
}

// scanRunPanel renders one run: its reference and status, the run's facts and
// its finding counts. actions lists the keys of the screen it sits on.
func scanRunPanel(theme adminTheme, run ports.ScanRun, now time.Time, actions [][2]string, width, height int) string {
	return scanRunPanelFor(theme, run, now, actions, false, width, height)
}

// scanRunPanelFor is scanRunPanel; freshness adds what the latest-only scan
// model records per digest: the vulnerability DB the result used, the last
// check, and why a kept result is stale.
func scanRunPanelFor(theme adminTheme, run ports.ScanRun, now time.Time, actions [][2]string, freshness bool, width, height int) string {
	inner := max(width-4, 1)
	title := run.Repository
	if ref := strings.TrimSpace(run.RequestedRef); ref != "" {
		title += ":" + ref
	}
	subtitle := adminFirstNonEmpty(run.Status, "unknown")
	if trigger := strings.TrimSpace(run.Trigger); trigger != "" {
		subtitle += ", trigger " + trigger
	}
	head := []string{theme.subheading.Render(grantTruncate(title, inner))}
	head = append(head, wrapStyled(theme.muted, subtitle, inner)...)
	if freshness && run.Stale {
		reason := "Rescan failed: this result is kept and still enforced."
		if detail := strings.TrimSpace(run.StaleReason); detail != "" {
			reason = "Rescan failed: " + detail
		}
		lines := wrapStyled(theme.warning, reason, inner)
		if len(lines) > 2 {
			lines = lines[:2]
		}
		head = append(head, lines...)
	}
	runSection := []string{
		scanSectionTitle(theme, "RUN"),
		scanKV(theme, "Digest", scanShortDigest(run.Digest), theme.text, width),
		scanKV(theme, "Started", scanClockUTC(run.StartedAt, now), theme.text, width),
		scanKV(theme, "Finished", scanClockUTC(run.FinishedAt, now), theme.text, width),
		scanKV(theme, "Trivy", adminFirstNonEmpty(run.TrivyVersion, "-"), theme.text, width),
	}
	if freshness {
		// One row per digest keeps its latest result: the panel trades the
		// start time for what makes the result current.
		runSection = []string{
			scanSectionTitle(theme, "RESULT"),
			scanKV(theme, "Digest", scanShortDigest(run.Digest), theme.text, width),
			scanKV(theme, "Finished", scanClockUTC(run.FinishedAt, now), theme.text, width),
			scanKV(theme, "Trivy", adminFirstNonEmpty(run.TrivyVersion, "-"), theme.text, width),
			scanKV(theme, "Trivy DB", scanDBText(run, now), theme.text, width),
			scanKV(theme, "Checked", scanCheckedText(run, now), theme.text, width),
		}
	}
	if run.Status == ports.ScanRunStatusFailed && strings.TrimSpace(run.Error) != "" {
		runSection = append(runSection, scanKV(theme, "Error", run.Error, theme.error, width))
	}
	if freshness && run.Status == ports.ScanRunStatusSkipped && strings.TrimSpace(run.Error) != "" {
		runSection = append(runSection, scanKV(theme, "Reason", run.Error, theme.muted, width))
	}
	findings := []string{
		scanSectionTitle(theme, "FINDINGS"),
		scanKV(theme, "Critical", scanCount(run, run.Critical), scanCountStyle(theme, run, run.Critical, theme.severityCritical), width),
		scanKV(theme, "High", scanCount(run, run.High), scanCountStyle(theme, run, run.High, theme.severityHigh), width),
		scanKV(theme, "Medium", scanCount(run, run.Medium), theme.text, width),
		scanKV(theme, "Low", scanCount(run, run.Low), theme.text, width),
	}
	sections := [][]string{runSection, findings}
	if len(actions) > 0 {
		section := []string{scanSectionTitle(theme, "ACTIONS")}
		for _, action := range actions {
			section = append(section, scanAction(theme, action[0], action[1], width))
		}
		sections = append(sections, section)
	}
	return scanPanel(theme, head, sections, nil, width, height)
}

// scanEmptyPanel is the panel while nothing is selected.
func scanEmptyPanel(theme adminTheme, text string, width, height int) string {
	return scanBox(theme, []string{theme.muted.Render(grantTruncate(text, max(width-4, 1)))}, width, max(height-2, 1))
}

// scanColumns joins the table and the panel side by side under the screen's
// title.
func scanColumns(title, left, panel string) string {
	columns := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", scanColGap), panel)
	return title + "\n" + columns
}

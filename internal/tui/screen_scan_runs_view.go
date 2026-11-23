package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

func (s scanRunsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH, listW, panelW := scanBoardSize(env)
	foot := s.footnoteLines(theme, listW)
	visible := s.visibleRows(foot, bodyH)
	panelH := max(bodyH-1, 3)

	title, crumb, nav := "Scan runs", "Scan runs", shellNavScanRuns
	if s.isSecretFindings() {
		title, crumb, nav = "Secret findings", "Secret findings", shellNavSecretFindings
	}

	var table, panel string
	if s.isSecretFindings() {
		table, panel = s.secretBoard(theme, env, listW, panelW, visible, panelH)
	} else {
		table, panel = s.runsBoard(theme, env, listW, panelW, visible, panelH)
	}
	left := table + "\n" + strings.Join(foot, "\n")
	body := scanColumns(scanTitleLine(theme, title, s.note(), bodyW), left, panel)

	return screenFrame{
		Context: crumb,
		Full:    true,
		Body: renderShell(theme, shellSpec{
			Breadcrumb: shellCrumb("Operations", crumb),
			Operator:   env.Session.Username,
			Active:     nav,
			Body:       body,
			Hints:      s.Keys().short,
			Width:      width,
			Height:     height,
		}),
	}
}

// scanRescanRule says when a digest is scanned again.
const scanRescanRule = "Rescans only when the vulnerability DB or scan settings change"

// scanTitleRows is the title line above the table box.
const scanTitleRows = 1

// visibleRows is how many list rows fit under the title with the footnote
// lines under the box.
func (s scanRunsScreen) visibleRows(foot []string, bodyH int) int {
	return max(bodyH-scanTitleRows-len(foot)-scanTableChromeRows, 1)
}

// note is the muted text on the right of the title.
func (s scanRunsScreen) note() string {
	switch {
	case s.err != "":
		return "Unavailable"
	case !s.loaded:
		return "Reading…"
	case s.isSecretFindings():
		if len(s.pending) > 0 {
			return fmt.Sprintf("Reading… %d of %d scans read", s.requested-len(s.pending), s.requested)
		}
		return "Gitleaks, leaked secrets across repositories"
	}
	if n := len(s.digestPending); n > 0 {
		return fmt.Sprintf("Reading… %d of %d repositories", len(s.summaries)-n, len(s.summaries))
	}
	return "Trivy scans, latest result of each image digest"
}

// notice is what the list says instead of rows.
func (s scanRunsScreen) notice(theme adminTheme, inner int) []string {
	switch {
	case s.err != "":
		return []string{
			theme.error.Render(grantTruncate(sanitizeDisplay("Could not read scan history: "+s.err), inner)),
			theme.muted.Render("Press r to try again."),
		}
	case !s.loaded && s.isSecretFindings():
		return []string{theme.muted.Render("Reading secret findings…")}
	case !s.loaded:
		return []string{theme.muted.Render("Reading scan history…")}
	case len(s.summaries) == 0:
		return []string{theme.muted.Render("No repository scan history found.")}
	case len(s.pending) > 0:
		return []string{theme.muted.Render("Reading secret findings…")}
	}
	return []string{theme.muted.Render("No secret findings in the latest scans.")}
}

// ---- Scan runs ----

func (s scanRunsScreen) runsBoard(theme adminTheme, env screenEnv, listW, panelW, visible, panelH int) (table, panel string) {
	now := env.now()
	specs := []scanColSpec{
		{title: "REPOSITORY", flex: 1, min: 8},
		{title: "DIGEST", fixed: 11},
		{title: "STATUS", fixed: 9},
		{title: "TRIVY DB", fixed: 9},
		{title: "CHECKED", fixed: 9},
		{title: "CRIT", fixed: 4},
		{title: "HIGH", fixed: 4},
	}
	rows := make([][]scanCell, len(s.rows))
	for i, run := range s.rows {
		status := scanStatusStyle(theme, run.Status)
		if scanRowStatus(run) == "stale" {
			status = theme.warning
		}
		rows[i] = []scanCell{
			{run.Repository, theme.text},
			{scanShortHash(run.Digest), theme.muted},
			{scanRowStatus(run), status},
			{scanDBText(run, now), theme.text},
			{scanCheckedText(run, now), theme.text},
			{scanCount(run, run.Critical), scanCountStyle(theme, run, run.Critical, theme.severityCritical)},
			{scanCount(run, run.High), scanCountStyle(theme, run, run.High, theme.severityHigh)},
		}
	}
	table = renderScanTable(theme, specs, rows, s.cursor, visible, listW, s.notice(theme, max(listW-4, 1)))
	run, ok := s.selectedRun()
	if !ok {
		return table, scanEmptyPanel(theme, "Select a digest for details.", panelW, panelH)
	}
	actions := [][2]string{{"Scan history", "enter"}, {"Refresh", "r"}}
	return table, scanRunPanelFor(theme, run, now, actions, true, panelW, panelH)
}

// ---- Secret findings ----

func (s scanRunsScreen) secretBoard(theme adminTheme, env screenEnv, listW, panelW, visible, panelH int) (table, panel string) {
	found := s.secretRows()
	repoW, locW := len("REPOSITORY"), len("LOCATION")
	for _, row := range found {
		repoW = max(repoW, lipgloss.Width(row.run.Repository))
		locW = max(locW, min(lipgloss.Width(secretFindingLocation(row.finding)), 28))
	}
	// The description keeps at least scanDescriptionMin columns: the
	// location, then the repository, give way first.
	room := max(listW-4, 1) - scanMarkerWidth - 3*scanColGap - 18 - scanDescriptionMin
	repoW = min(repoW, 20)
	locW = max(min(locW, room-repoW), 12)
	specs := []scanColSpec{
		{title: "REPOSITORY", fixed: repoW},
		{title: "RULE", fixed: 18},
		{title: "LOCATION", fixed: locW},
		{title: "DESCRIPTION", flex: 1, min: 8},
	}
	rows := make([][]scanCell, len(found))
	for i, row := range found {
		f := row.finding
		rows[i] = []scanCell{
			{row.run.Repository, theme.text},
			{adminFirstNonEmpty(f.RuleID, "unknown"), theme.text},
			{secretFindingLocation(f), theme.text},
			{strings.TrimSpace(f.Description), theme.muted},
		}
	}
	table = renderScanTable(theme, specs, rows, s.cursor, visible, listW, s.notice(theme, max(listW-4, 1)))
	if len(found) == 0 {
		return table, scanEmptyPanel(theme, "Select a finding to see its details.", panelW, panelH)
	}
	return table, s.secretPanel(theme, found[boundedIndex(s.cursor, len(found))], panelW, panelH)
}

// footnoteLines is the muted text under the list: one line, or two when the
// secret findings' advice needs them.
func (s scanRunsScreen) footnoteLines(theme adminTheme, width int) []string {
	if !s.isSecretFindings() {
		lines := wrapStyled(theme.muted, "One row per image digest, its latest result. "+scanRescanRule+". enter opens its scan history.", width)
		if len(lines) > 2 {
			lines = lines[:2]
		}
		return lines
	}
	found := s.secretRows()
	if len(found) == 0 {
		return []string{theme.muted.Render(grantTruncate("enter opens the scan history of the selected finding's repository.", width))}
	}
	repos := map[string]bool{}
	for _, row := range found {
		repos[row.run.Repository] = true
	}
	text := fmt.Sprintf("%s in %s. A secret already pushed stays in image layers: rotate it.",
		plural(len(found), "finding"), plural(len(repos), "repository"))
	lines := wrapStyled(theme.muted, text, width)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	return lines
}

func (s scanRunsScreen) secretPanel(theme adminTheme, row secretRow, width, height int) string {
	inner := max(width-4, 1)
	f := row.finding
	head := []string{
		theme.subheading.Render(grantTruncate(sanitizeDisplay(adminFirstNonEmpty(f.RuleID, "unknown")), inner)),
	}
	head = append(head, theme.muted.Render(grantTruncate(sanitizeDisplay(row.run.Repository), inner)))
	head = append(head, wrapStyled(theme.muted, secretFindingPlace(f), inner)...)
	finding := []string{scanSectionTitle(theme, "FINDING")}
	if desc := strings.TrimSpace(f.Description); desc != "" {
		finding = append(finding, scanKV(theme, "Description", desc, theme.text, width))
	}
	if len(f.Tags) > 0 {
		finding = append(finding, scanKV(theme, "Tags", strings.Join(f.Tags, ", "), theme.text, width))
	}
	finding = append(finding, scanKV(theme, "Blob", scanShortDigest(f.BlobDigest), theme.text, width))
	run := []string{
		scanSectionTitle(theme, "SCAN RUN"),
		scanKV(theme, "Digest", scanShortDigest(row.run.Digest), theme.text, width),
		scanKV(theme, "Findings in run", fmt.Sprintf("%d", row.inRun), theme.warning, width),
	}
	note := wrapStyled(theme.muted, "The secret value itself is never shown.", inner)
	return scanPanel(theme, head, [][]string{finding, run}, note, width, height)
}

// secretFindingPlace is "src/deploy/prod.env line 14" or "... lines 14 to 20".
func secretFindingPlace(f ports.SecretFinding) string {
	place := sanitizeDisplay(adminFirstNonEmpty(f.Path, f.BlobDigest, "unknown location"))
	switch {
	case f.StartLine <= 0:
		return place
	case f.EndLine > f.StartLine:
		return fmt.Sprintf("%s lines %d to %d", place, f.StartLine, f.EndLine)
	}
	return fmt.Sprintf("%s line %d", place, f.StartLine)
}

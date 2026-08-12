package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
)

const (
	adminTableMetaFeatureName = "__feature_name"
	adminTableMetaScanRunID   = "__scan_run_id"
	adminTableMetaFindingID   = "__finding_id"

	adminTableColumnFeatureName       = "feature_name"
	adminTableColumnFeatureKind       = "feature_kind"
	adminTableColumnFeatureEnabled    = "feature_enabled"
	adminTableColumnFeatureConfigured = "feature_configured"
	adminTableColumnFeatureCurrent    = "feature_current"
	adminTableColumnFeatureLatest     = "feature_latest"
	adminTableColumnFeatureUpdate     = "feature_update"

	adminTableColumnRowTitle  = "row_title"
	adminTableColumnRowStatus = "row_status"
	adminTableColumnRowDetail = "row_detail"

	adminTableColumnScanRunRepository = "scan_repository"
	adminTableColumnScanRunReference  = "scan_reference"
	adminTableColumnScanRunStatus     = "scan_status"
	adminTableColumnScanRunCritical   = "scan_critical"
	adminTableColumnScanRunHigh       = "scan_high"
	adminTableColumnScanRunFixable    = "scan_fixable"

	adminTableColumnFindingSeverity  = "finding_severity"
	adminTableColumnFindingID        = "finding_id"
	adminTableColumnFindingPackage   = "finding_package"
	adminTableColumnFindingInstalled = "finding_installed"
	adminTableColumnFindingFixed     = "finding_fixed"
	adminTableColumnFindingFixable   = "finding_fixable"

	adminTableColumnSecretFindingRule     = "secret_finding_rule"
	adminTableColumnSecretFindingLocation = "secret_finding_location"

	adminTableColumnScanSummaryRepository   = "scan_summary_repository"
	adminTableColumnScanSummaryReference    = "scan_summary_reference"
	adminTableColumnScanSummaryStatus       = "scan_summary_status"
	adminTableColumnScanSummaryCritical     = "scan_summary_critical"
	adminTableColumnScanSummaryHigh         = "scan_summary_high"
	adminTableColumnScanSummaryFixable      = "scan_summary_fixable"
	adminTableColumnScanSummaryRuns         = "scan_summary_runs"
	adminTableColumnScanSummaryLastExecuted = "scan_summary_last_executed"
)

// newAdminBubbleTable is the sole construction point for every admin table.
// pageSize is the number of data rows shown per page (design.md decision #1:
// we derive this ourselves from the measured terminal budget since
// WithTargetHeight does not exist at the pinned bubble-table version).
// Rendered table height is deterministically pageSize+tableChromeRows once a
// page is filled (viewport.go's tableChromeRows, verified against
// evertras/bubble-table@v0.19.2).
func newAdminBubbleTable(columns []bubbletable.Column, rows []bubbletable.Row, highlighted int, theme adminTheme, pageSize int) bubbletable.Model {
	keys := bubbletable.DefaultKeyMap()
	keys.RowSelectToggle.SetKeys("ctrl+space")
	keys.FilterBlur.SetKeys("ctrl+g")
	keys.FilterClear.SetKeys("ctrl+shift+g")
	keys.PageDown.SetKeys("pgdown")
	keys.PageUp.SetKeys("pgup")
	keys.PageFirst.SetKeys("home")
	keys.PageLast.SetKeys("end")

	model := bubbletable.New(columns).
		WithRows(rows).
		WithKeyMap(keys).
		WithBaseStyle(lipgloss.NewStyle().Align(lipgloss.Left).BorderForeground(theme.borderColor)).
		HeaderStyle(theme.tableHeader).
		HighlightStyle(theme.selected).
		Focused(true).
		BorderRounded().
		WithPageSize(pageSize).
		WithFooterVisibility(true)

	if len(rows) > 0 {
		model = model.WithHighlightedRow(boundedIndex(highlighted, len(rows)))
	}

	return model
}

func buildAdminFeaturesTable(theme adminTheme, features []ports.FeatureSummary, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnFeatureName, "Name", 18),
		bubbletable.NewColumn(adminTableColumnFeatureKind, "Kind", 10),
		bubbletable.NewColumn(adminTableColumnFeatureEnabled, "Enabled", 8),
		bubbletable.NewColumn(adminTableColumnFeatureConfigured, "Configured", 10),
		bubbletable.NewColumn(adminTableColumnFeatureCurrent, "Current", 10),
		bubbletable.NewColumn(adminTableColumnFeatureLatest, "Latest", 10),
		bubbletable.NewColumn(adminTableColumnFeatureUpdate, "Update", 10),
	}
	rows := make([]bubbletable.Row, 0, len(features))
	for _, feature := range features {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnFeatureName:       feature.Name,
			adminTableColumnFeatureKind:       string(feature.Kind),
			adminTableColumnFeatureEnabled:    fmt.Sprintf("%t", feature.Enabled),
			adminTableColumnFeatureConfigured: fmt.Sprintf("%t", feature.Configured),
			adminTableColumnFeatureCurrent:    adminFirstNonEmpty(feature.CurrentVersion, "unknown"),
			adminTableColumnFeatureLatest:     adminFirstNonEmpty(feature.LatestVersion, "unknown"),
			adminTableColumnFeatureUpdate:     adminFirstNonEmpty(feature.UpdateStatus, "unknown"),
			adminTableMetaFeatureName:         feature.Name,
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

func buildAdminFeatureRowsTable(theme adminTheme, section ports.FeatureSection, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnRowTitle, "Title", 22),
		bubbletable.NewColumn(adminTableColumnRowStatus, "Status", 12),
		bubbletable.NewColumn(adminTableColumnRowDetail, "Detail", 42),
	}
	rows := make([]bubbletable.Row, 0, len(section.Rows))
	for _, row := range section.Rows {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnRowTitle:  adminFirstNonEmpty(row.Title, "unknown"),
			adminTableColumnRowStatus: adminFirstNonEmpty(row.Status, "unknown"),
			adminTableColumnRowDetail: adminFirstNonEmpty(row.Detail, "n/a"),
		}))
	}
	return newAdminBubbleTable(columns, rows, 0, theme, pageSize)
}

func buildAdminScanRunsTable(theme adminTheme, runs []ports.ScanRun, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnScanRunRepository, "Repository", 18),
		bubbletable.NewColumn(adminTableColumnScanRunReference, "Reference", 12),
		bubbletable.NewColumn(adminTableColumnScanRunStatus, "Status", 10),
		bubbletable.NewColumn(adminTableColumnScanRunCritical, "Critical", 8),
		bubbletable.NewColumn(adminTableColumnScanRunHigh, "High", 6),
		bubbletable.NewColumn(adminTableColumnScanRunFixable, "Fixable", 8),
	}
	rows := make([]bubbletable.Row, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnScanRunRepository: adminFirstNonEmpty(run.Repository, "unknown"),
			adminTableColumnScanRunReference:  adminFirstNonEmpty(run.RequestedRef, "unknown"),
			adminTableColumnScanRunStatus:     adminFirstNonEmpty(run.Status, "unknown"),
			adminTableColumnScanRunCritical:   run.Critical,
			adminTableColumnScanRunHigh:       run.High,
			adminTableColumnScanRunFixable:    fmt.Sprintf("%t", run.HasFixable),
			adminTableMetaScanRunID:           run.ID,
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

func buildAdminFindingsTable(theme adminTheme, findings []ports.ScanRunFinding, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnFindingSeverity, "Severity", 10),
		bubbletable.NewColumn(adminTableColumnFindingID, "Finding", 18),
		bubbletable.NewColumn(adminTableColumnFindingPackage, "Package", 16),
		bubbletable.NewColumn(adminTableColumnFindingInstalled, "Installed", 10),
		bubbletable.NewColumn(adminTableColumnFindingFixed, "Fixed", 10),
		bubbletable.NewColumn(adminTableColumnFindingFixable, "Fixable", 8),
	}
	rows := make([]bubbletable.Row, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnFindingSeverity:  severityStyledCell(theme, finding.Severity),
			adminTableColumnFindingID:        adminFirstNonEmpty(finding.VulnerabilityID, "unknown"),
			adminTableColumnFindingPackage:   adminFirstNonEmpty(finding.PackageName, "unknown"),
			adminTableColumnFindingInstalled: adminFirstNonEmpty(finding.InstalledVersion, "unknown"),
			adminTableColumnFindingFixed:     adminFirstNonEmpty(finding.FixedVersion, "n/a"),
			adminTableColumnFindingFixable:   fmt.Sprintf("%t", finding.Fixable),
			adminTableMetaFindingID:          adminFirstNonEmpty(finding.VulnerabilityID, finding.PackageName),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

// buildAdminSecretFindingsTable renders redacted secret-scan findings: rule
// ID and location only (spec.md "Redacted Secret Findings Model",
// "Informational Findings Only"). Deliberately no severity/status/fixable
// column — secret findings carry no severity or gating dimension in this
// change, unlike buildAdminFindingsTable's vulnerability rows.
func buildAdminSecretFindingsTable(theme adminTheme, findings []ports.SecretFinding, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnSecretFindingRule, "Rule", 22),
		bubbletable.NewColumn(adminTableColumnSecretFindingLocation, "Location", 40),
	}
	rows := make([]bubbletable.Row, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnSecretFindingRule:     adminFirstNonEmpty(finding.RuleID, "unknown"),
			adminTableColumnSecretFindingLocation: secretFindingLocation(finding),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

// buildAdminScanSummaryTable renders one row per repository
// (summarizeScanRunsByRepository's output) instead of one row per scan run
// (spec.md "Repository Alerts Summarized Per Repository With Ordering And
// Freshness"). It adds a last-execution column on top of
// buildAdminScanRunsTable's latest-run columns; buildAdminScanRunsTable
// itself is untouched and still used elsewhere (Phase 4 removes that old
// path once this replaces it on screen).
func buildAdminScanSummaryTable(theme adminTheme, summaries []repositorySummary, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnScanSummaryRepository, "Repository", 18),
		bubbletable.NewColumn(adminTableColumnScanSummaryReference, "Reference", 12),
		bubbletable.NewColumn(adminTableColumnScanSummaryStatus, "Status", 10),
		bubbletable.NewColumn(adminTableColumnScanSummaryCritical, "Critical", 8),
		bubbletable.NewColumn(adminTableColumnScanSummaryHigh, "High", 6),
		bubbletable.NewColumn(adminTableColumnScanSummaryFixable, "Fixable", 8),
		bubbletable.NewColumn(adminTableColumnScanSummaryRuns, "Runs", 6),
		bubbletable.NewColumn(adminTableColumnScanSummaryLastExecuted, "Last Executed", 22),
	}
	rows := make([]bubbletable.Row, 0, len(summaries))
	for _, summary := range summaries {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnScanSummaryRepository:   adminFirstNonEmpty(summary.Repository, "unknown"),
			adminTableColumnScanSummaryReference:    adminFirstNonEmpty(summary.LatestRun.RequestedRef, "unknown"),
			adminTableColumnScanSummaryStatus:       adminFirstNonEmpty(summary.LatestRun.Status, "unknown"),
			adminTableColumnScanSummaryCritical:     summary.LatestRun.Critical,
			adminTableColumnScanSummaryHigh:         summary.LatestRun.High,
			adminTableColumnScanSummaryFixable:      fmt.Sprintf("%t", summary.LatestRun.HasFixable),
			adminTableColumnScanSummaryRuns:         summary.RunCount,
			adminTableColumnScanSummaryLastExecuted: formatScanSummaryLastExecuted(summary),
			adminTableMetaScanRunID:                 summary.LatestRun.ID,
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

// formatScanSummaryLastExecuted renders a repositorySummary's last-execution
// column: never blank (spec.md "the date column SHALL show CreatedAt with an
// in-progress marker, never blank"), following effectiveScanRunTime's
// FinishedAt->CreatedAt fallback that summarizeScanRunsByRepository already
// applied when building LastExecuted/InProgress.
func formatScanSummaryLastExecuted(summary repositorySummary) string {
	if summary.LastExecuted.IsZero() {
		return "unknown"
	}
	formatted := summary.LastExecuted.UTC().Format("2006-01-02 15:04")
	if summary.InProgress {
		return formatted + " (in progress)"
	}
	return formatted
}

// adminScanHistoryModalChromeRows is the scan history modal's own fixed
// non-table content once open: title, tab bar, the "Execution N/M — date"
// history footer, and the help line — each guarded to exactly one row by
// TestRenderAdminScanHistoryModalChromeLinesAreSingleLine.
//
// Deviation from design.md's literal `sectionChromeRows + 4`: this is 4, not
// 8. adminScanHistoryRowSplit (Phase 1) already reserves the modal's own
// border+padding exactly once via `usable := outer.SectionRows -
// sectionChromeRows`, so the modalRows it returns is already a border-free
// content budget — re-subtracting sectionChromeRows here would charge the
// same border twice and starve the table for no reason. Re-derived from
// Phase 1's own tested row-split invariant while measuring real modal
// content for this phase, per the "measured, not guessed" discipline.
const adminScanHistoryModalChromeRows = 4

// adminScanHistoryModalMinTableBudget is the smallest number of rows a
// bordered table needs at all: its own fixed chrome (tableChromeRows) plus
// the smallest usable pageSize (minTableRows). Below this, the modal
// substitutes a single-line message instead of ever slicing a bordered
// table (design.md "the table is replaced by a single-line substitute,
// never sliced").
const adminScanHistoryModalMinTableBudget = tableChromeRows + minTableRows

// adminScanHistoryModalTablePageSize computes the scan history modal's
// active-tab table pageSize from the modal's own nested content budget
// (modalRows, from adminScanHistoryRowSplit), the modal's fixed chrome
// (adminScanHistoryModalChromeRows), and the measured height of any
// variable header content rendered above the table (an error or loading
// message) — same "measure, don't guess" discipline as
// trivyAlertDetailTableRoles. Floored at minTableRows.
func adminScanHistoryModalTablePageSize(modalRows, measuredHeaderHeight int) int {
	available := modalRows - adminScanHistoryModalChromeRows - measuredHeaderHeight - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	return available
}

func secretFindingLocation(finding ports.SecretFinding) string {
	location := adminFirstNonEmpty(finding.Path, finding.BlobDigest)
	if location == "" {
		return "unknown"
	}
	if finding.StartLine <= 0 {
		return location
	}
	if finding.EndLine > finding.StartLine {
		return fmt.Sprintf("%s:%d-%d", location, finding.StartLine, finding.EndLine)
	}
	return fmt.Sprintf("%s:%d", location, finding.StartLine)
}

func severityStyledCell(theme adminTheme, severity string) bubbletable.StyledCell {
	value := strings.ToUpper(strings.TrimSpace(severity))
	style := theme.text
	switch value {
	case "CRITICAL":
		style = theme.severityCritical
	case "HIGH":
		style = theme.severityHigh
	case "MEDIUM":
		style = theme.severityMedium
	case "LOW":
		style = theme.severityLow
	}
	return bubbletable.NewStyledCell(adminFirstNonEmpty(value, "UNKNOWN"), style)
}

func highlightedRowValue(model bubbletable.Model, key string) string {
	if model.TotalRows() == 0 {
		return ""
	}
	value, _ := model.HighlightedRow().Data[key].(string)
	return strings.TrimSpace(value)
}

// tableRoles splits a screen's row budget into two admin-table pageSize
// roles (design.md decision #6): primary tables (Features, ScanRuns — the
// operator-navigable top-level lists) get the whole budget available to a
// standalone table so a single-table screen fits exactly without relying on
// the outer clip; compact tables (FeatureRows, Findings, SecretFindings —
// always paired with a primary selection) are capped at compactTableRows,
// floored at minTableRows. When several tables stack on one screen (the
// Trivy alerts case), their combined height can still exceed l.SectionRows —
// renderSection's outer-pane clip (Phase 3) is the structural guarantee
// against overflow there, not this split; this split is ergonomics only.
func tableRoles(l consoleLayout) (primary, compact int) {
	available := l.SectionRows - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	primary = available
	compact = compactTableRows
	if compact > available {
		compact = available
	}
	return primary, compact
}

// trivyDetailFixedLines is the row count of every non-table line
// renderTrivyRepositoryAlerts (admin_views.go) composes around the
// ScanRuns/Findings/SecretFindings tables once a scan-run detail is open:
// the "Repository Alerts" title (1) + blank+"Selected Scan Run" heading (2)
// + the six fixed detail fields (Repository, Reference, Digest, Status,
// Reference freshness, DB freshness) (6) + blank+"Findings" heading (2) +
// blank+"Secret Findings" heading (2) = 13. The optional "Error: ..." line
// is deliberately not counted here (it is data-dependent); undercounting by
// one row in that rare case is covered by renderSection's outer-pane clip
// (viewport.go), which remains the structural overflow guarantee.
//
// trivyScreenChromeLines is the row count of the fixed lines
// renderAdminFeaturesScreen/renderFeaturePageBody (admin_views.go) compose
// around the whole Feature Page block — the block that itself contains
// renderTrivyRepositoryAlerts's output — on the only screen that renders
// this state today (adminTablesLayout's doc comment): "Built-in Features"
// heading (1) + blank before "Feature Page" (1) + "Feature Page" heading (1)
// + renderTrivyTabs's two lines (2) + trailing blank+Operator+Session
// remaining (3) = 8.
const (
	trivyDetailFixedLines  = 13
	trivyScreenChromeLines = 8
)

// trivyAlertDetailTableRoles computes ScanRuns/Findings pageSizes for the
// Trivy repository-alerts screen once a scan-run detail is open (post-verify
// regression fix). tableRoles's default split gives ScanRuns the whole
// primary budget on the assumption it is the screen's only table, which
// starves the Findings table when a detail is also being rendered below it
// — renderSection's outer clip then had to cut through the Findings table
// entirely, never showing its bordered box at all. Once the operator has
// drilled into a specific run, browsing the full run list is no longer the
// point, so ScanRuns collapses to the compact role and the freed budget goes
// to Findings instead, computed from the other rows actually being rendered
// on this screen (measured against the real composed structure — the
// Features table's actual rendered height via lipgloss.Height, same style as
// contentBudget's own status/help measurement, plus the documented fixed
// line counts above — rather than guessed) so Findings' own header/rows/
// footer have room to render before the outer clip has to act.
func trivyAlertDetailTableRoles(l consoleLayout, featuresTableHeight int, hasSecretFindings bool) (scanRuns, findings int) {
	_, compact := tableRoles(l)
	scanRuns = compact

	secretFindingsRows := 1 // empty-state message ("No secret findings...") is a single line
	if hasSecretFindings {
		secretFindingsRows = compact + tableChromeRows
	}

	fixed := trivyScreenChromeLines + featuresTableHeight + trivyDetailFixedLines + (scanRuns + tableChromeRows) + secretFindingsRows
	available := l.SectionRows - fixed - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	findings = available
	return scanRuns, findings
}

func (m *Model) rebuildAdminTables(layout consoleLayout) {
	layout.Primary, layout.Compact = tableRoles(layout)
	m.adminView.Layout = layout

	theme := newAdminTheme()
	m.adminView.Tables.Features = buildAdminFeaturesTable(theme, m.adminView.Features, m.adminView.SelectedFeature, layout.Primary)
	featureRows := make(map[string]bubbletable.Model)
	for _, section := range m.adminView.FeaturePage.Sections {
		if section.Kind != "rows" {
			continue
		}
		featureRows[section.ID] = buildAdminFeatureRowsTable(theme, section, layout.Compact)
	}
	m.adminView.Tables.FeatureRows = featureRows

	// ScanSummary is the per-repository Repository Alerts summary table
	// (spec.md "Repository Alerts Summarized Per Repository With Ordering
	// And Freshness"). Built alongside ScanRuns (not replacing it yet) —
	// Phase 4 removes the old per-scan-run rendering path this eventually
	// supersedes on screen.
	m.adminView.Tables.ScanSummary = buildAdminScanSummaryTable(theme, m.adminView.TrivySummaries, m.adminView.TrivySelectedAlert, layout.Primary)

	scanRunsPageSize, findingsPageSize := layout.Primary, layout.Compact
	if m.adminView.TrivyAlertDetailOpen {
		featuresTableHeight := lipgloss.Height(m.adminView.Tables.Features.View())
		scanRunsPageSize, findingsPageSize = trivyAlertDetailTableRoles(layout, featuresTableHeight, len(m.adminView.SecretFindings) > 0)
	}
	m.adminView.Tables.ScanRuns = buildAdminScanRunsTable(theme, m.adminView.TrivyScanRuns, m.adminView.TrivySelectedAlert, scanRunsPageSize)

	// The scan history modal's Findings/SecretFindings tables share the same
	// Tables.Findings/Tables.SecretFindings fields the old inline-detail flow
	// uses (design.md interfaces: adminScanHistoryModalTableBody reuses them
	// as-is). Only one of {TrivyAlertDetailOpen, ScanHistoryModal.Active()}
	// is ever true at a time (Enter routes to exactly one — no conflict), so
	// branching on which is active is safe.
	if m.adminView.ScanHistoryModal.Active() {
		_, modalRows, _, _, _ := adminScanHistoryModalRowSplit(theme, screenAdminFeatures, m.adminSession, m.adminView, nil, layout, m.now())
		measuredHeaderHeight := 0
		if strings.TrimSpace(m.adminView.ScanHistoryModal.Error) != "" || m.adminView.ScanHistoryModal.Loading {
			measuredHeaderHeight = 1
		}
		modalTablePageSize := adminScanHistoryModalTablePageSize(modalRows, measuredHeaderHeight)
		m.adminView.Tables.Findings = buildAdminFindingsTable(theme, m.adminView.ScanHistoryModal.Detail.Findings, 0, modalTablePageSize)
		m.adminView.Tables.SecretFindings = buildAdminSecretFindingsTable(theme, m.adminView.ScanHistoryModal.Secrets, 0, modalTablePageSize)
	} else {
		m.adminView.Tables.Findings = buildAdminFindingsTable(theme, m.adminView.TrivyScanRunDetail.Findings, 0, findingsPageSize)
		m.adminView.Tables.SecretFindings = buildAdminSecretFindingsTable(theme, m.adminView.SecretFindings, 0, layout.Compact)
	}
	m.syncAdminTableSelections()
}

func (m *Model) syncAdminTableHighlights() {
	if m.adminView.Tables.Features.TotalRows() > 0 {
		m.adminView.Tables.Features = m.adminView.Tables.Features.WithHighlightedRow(boundedIndex(m.adminView.SelectedFeature, len(m.adminView.Features)))
	}
	if m.adminView.Tables.ScanRuns.TotalRows() > 0 {
		m.adminView.Tables.ScanRuns = m.adminView.Tables.ScanRuns.WithHighlightedRow(boundedIndex(m.adminView.TrivySelectedAlert, len(m.adminView.TrivyScanRuns)))
	}
	if m.adminView.Tables.ScanSummary.TotalRows() > 0 {
		m.adminView.Tables.ScanSummary = m.adminView.Tables.ScanSummary.WithHighlightedRow(boundedIndex(m.adminView.TrivySelectedAlert, len(m.adminView.TrivySummaries)))
	}
	m.syncAdminTableSelections()
}

func (m *Model) syncAdminTableSelections() {
	m.adminView.Tables.Selection.FeatureName = highlightedRowValue(m.adminView.Tables.Features, adminTableMetaFeatureName)
	m.adminView.Tables.Selection.ScanRunID = highlightedRowValue(m.adminView.Tables.ScanRuns, adminTableMetaScanRunID)
	m.adminView.Tables.Selection.FindingID = highlightedRowValue(m.adminView.Tables.Findings, adminTableMetaFindingID)
}

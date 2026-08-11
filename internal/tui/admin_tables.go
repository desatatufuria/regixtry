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
	m.adminView.Tables.ScanRuns = buildAdminScanRunsTable(theme, m.adminView.TrivyScanRuns, m.adminView.TrivySelectedAlert, layout.Primary)
	m.adminView.Tables.Findings = buildAdminFindingsTable(theme, m.adminView.TrivyScanRunDetail.Findings, 0, layout.Compact)
	m.adminView.Tables.SecretFindings = buildAdminSecretFindingsTable(theme, m.adminView.SecretFindings, 0, layout.Compact)
	m.syncAdminTableSelections()
}

func (m *Model) syncAdminTableHighlights() {
	if m.adminView.Tables.Features.TotalRows() > 0 {
		m.adminView.Tables.Features = m.adminView.Tables.Features.WithHighlightedRow(boundedIndex(m.adminView.SelectedFeature, len(m.adminView.Features)))
	}
	if m.adminView.Tables.ScanRuns.TotalRows() > 0 {
		m.adminView.Tables.ScanRuns = m.adminView.Tables.ScanRuns.WithHighlightedRow(boundedIndex(m.adminView.TrivySelectedAlert, len(m.adminView.TrivyScanRuns)))
	}
	m.syncAdminTableSelections()
}

func (m *Model) syncAdminTableSelections() {
	m.adminView.Tables.Selection.FeatureName = highlightedRowValue(m.adminView.Tables.Features, adminTableMetaFeatureName)
	m.adminView.Tables.Selection.ScanRunID = highlightedRowValue(m.adminView.Tables.ScanRuns, adminTableMetaScanRunID)
	m.adminView.Tables.Selection.FindingID = highlightedRowValue(m.adminView.Tables.Findings, adminTableMetaFindingID)
}

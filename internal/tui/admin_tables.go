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

	adminTableColumnFindingSeverity  = "finding_severity"
	adminTableColumnFindingID        = "finding_id"
	adminTableColumnFindingPackage   = "finding_package"
	adminTableColumnFindingInstalled = "finding_installed"
	adminTableColumnFindingFixed     = "finding_fixed"
	adminTableColumnFindingFixable   = "finding_fixable"

	adminTableColumnSecretFindingRule        = "secret_finding_rule"
	adminTableColumnSecretFindingLocation    = "secret_finding_location"
	adminTableColumnSecretFindingDescription = "secret_finding_description"
	adminTableColumnSecretFindingTags        = "secret_finding_tags"

	adminTableColumnScanSummaryRepository   = "scan_summary_repository"
	adminTableColumnScanSummaryReference    = "scan_summary_reference"
	adminTableColumnScanSummaryStatus       = "scan_summary_status"
	adminTableColumnScanSummaryCritical     = "scan_summary_critical"
	adminTableColumnScanSummaryHigh         = "scan_summary_high"
	adminTableColumnScanSummaryFixable      = "scan_summary_fixable"
	adminTableColumnScanSummaryRuns         = "scan_summary_runs"
	adminTableColumnScanSummaryLastExecuted = "scan_summary_last_executed"

	adminTableColumnFeatureOverrideRepository = "feature_override_repository"
	adminTableColumnFeatureOverrideStatus     = "feature_override_status"
	adminTableColumnFeatureOverrideEnabled    = "feature_override_enabled"
	adminTableColumnFeatureOverrideDetail     = "feature_override_detail"
	adminTableColumnFeatureOverrideUpdated    = "feature_override_updated"
)

// Column widths below are measured, not guessed (design.md's recurring
// discipline): each was sized against the actual longest value it renders --
// e.g. adminScanSummaryLastExecutedWidth against
// formatScanSummaryLastExecuted's longest output, "YYYY-MM-DD HH:MM (in
// progress)" -- and widened for readability per the reported regression
// (claude-handoff.md: Last Executed values were splitting across what looked
// like a broken row boundary because the column was narrower than its own
// longest legitimate value). sectionWidth (viewport.go) and
// minViewportWidth/defaultViewportWidth were sized to comfortably fit the
// widest of these tables (Repository Alerts) so the outer bordered section
// never wraps a table's own rendered lines.
const (
	adminFeaturesColumnNameWidth       = 22
	adminFeaturesColumnKindWidth       = 10
	adminFeaturesColumnEnabledWidth    = 8
	adminFeaturesColumnConfiguredWidth = 12
	adminFeaturesColumnCurrentWidth    = 12
	adminFeaturesColumnLatestWidth     = 12
	adminFeaturesColumnUpdateWidth     = 10

	adminFindingsColumnSeverityWidth  = 10
	adminFindingsColumnFindingWidth   = 22
	adminFindingsColumnPackageWidth   = 18
	adminFindingsColumnInstalledWidth = 12
	adminFindingsColumnFixedWidth     = 12
	adminFindingsColumnFixableWidth   = 8

	// adminSecretColumnRuleWidth/LocationWidth were narrowed from their
	// original 24/46 (measured against a 2-column table) to make room for
	// the added Description/Tags columns below, re-measured against
	// TestBuildAdminSecretFindingsTableColumnsFitWithoutOverflow's realistic
	// values rather than the pathological long strings a rule ID or path can
	// theoretically reach -- bubble-table truncates an overlong cell value
	// rather than corrupting the table, so this is a readability budget, not
	// a hard byte limit.
	adminSecretColumnRuleWidth        = 20
	adminSecretColumnLocationWidth    = 26
	adminSecretColumnDescriptionWidth = 30
	adminSecretColumnTagsWidth        = 18

	adminScanSummaryColumnRepositoryWidth   = 26
	adminScanSummaryColumnReferenceWidth    = 16
	adminScanSummaryColumnStatusWidth       = 12
	adminScanSummaryColumnCriticalWidth     = 9
	adminScanSummaryColumnHighWidth         = 7
	adminScanSummaryColumnFixableWidth      = 8
	adminScanSummaryColumnRunsWidth         = 6
	adminScanSummaryColumnLastExecutedWidth = 34

	adminFeatureOverrideColumnRepositoryWidth = 30
	// adminFeatureOverrideColumnStatusWidth is measured against
	// featureOverrideRowStatus's longest value, "inheriting global settings"
	// (26 chars).
	adminFeatureOverrideColumnStatusWidth  = 27
	adminFeatureOverrideColumnEnabledWidth = 8
	// adminFeatureOverrideColumnDetailWidth is measured against the longer of
	// Signing's "N trusted key(s)" and Gitleaks' ConfigPath values.
	adminFeatureOverrideColumnDetailWidth = 30
	// adminFeatureOverrideColumnUpdatedWidth fits formatFeatureOverrideUpdated's
	// fixed-width "2006-01-02 15:04" (16 chars) output exactly.
	adminFeatureOverrideColumnUpdatedWidth = 16
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
		bubbletable.NewColumn(adminTableColumnFeatureName, "Name", adminFeaturesColumnNameWidth),
		bubbletable.NewColumn(adminTableColumnFeatureKind, "Kind", adminFeaturesColumnKindWidth),
		bubbletable.NewColumn(adminTableColumnFeatureEnabled, "Enabled", adminFeaturesColumnEnabledWidth),
		bubbletable.NewColumn(adminTableColumnFeatureConfigured, "Configured", adminFeaturesColumnConfiguredWidth),
		bubbletable.NewColumn(adminTableColumnFeatureCurrent, "Current", adminFeaturesColumnCurrentWidth),
		bubbletable.NewColumn(adminTableColumnFeatureLatest, "Latest", adminFeaturesColumnLatestWidth),
		bubbletable.NewColumn(adminTableColumnFeatureUpdate, "Update", adminFeaturesColumnUpdateWidth),
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

func buildAdminFindingsTable(theme adminTheme, findings []ports.ScanRunFinding, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnFindingSeverity, "Severity", adminFindingsColumnSeverityWidth),
		bubbletable.NewColumn(adminTableColumnFindingID, "Finding", adminFindingsColumnFindingWidth),
		bubbletable.NewColumn(adminTableColumnFindingPackage, "Package", adminFindingsColumnPackageWidth),
		bubbletable.NewColumn(adminTableColumnFindingInstalled, "Installed", adminFindingsColumnInstalledWidth),
		bubbletable.NewColumn(adminTableColumnFindingFixed, "Fixed", adminFindingsColumnFixedWidth),
		bubbletable.NewColumn(adminTableColumnFindingFixable, "Fixable", adminFindingsColumnFixableWidth),
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
// ID, location, description, and tags (spec.md "Redacted Secret Findings
// Model", "Informational Findings Only"). Deliberately no
// severity/status/fixable column — secret findings carry no severity or
// gating dimension in this change, unlike buildAdminFindingsTable's
// vulnerability rows. Description renders as-is (may be blank); Tags join
// with ", " and render as an empty cell when nil/empty rather than a
// placeholder like Rule/Location's "unknown" fallback — Description/Tags are
// optional supplementary metadata, not the row's identity.
func buildAdminSecretFindingsTable(theme adminTheme, findings []ports.SecretFinding, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnSecretFindingRule, "Rule", adminSecretColumnRuleWidth),
		bubbletable.NewColumn(adminTableColumnSecretFindingLocation, "Location", adminSecretColumnLocationWidth),
		bubbletable.NewColumn(adminTableColumnSecretFindingDescription, "Description", adminSecretColumnDescriptionWidth),
		bubbletable.NewColumn(adminTableColumnSecretFindingTags, "Tags", adminSecretColumnTagsWidth),
	}
	rows := make([]bubbletable.Row, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnSecretFindingRule:        adminFirstNonEmpty(finding.RuleID, "unknown"),
			adminTableColumnSecretFindingLocation:    secretFindingLocation(finding),
			adminTableColumnSecretFindingDescription: strings.TrimSpace(finding.Description),
			adminTableColumnSecretFindingTags:        strings.Join(finding.Tags, ", "),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

// buildAdminScanSummaryTable renders one row per repository
// (summarizeScanRunsByRepository's output) instead of one row per scan run
// (spec.md "Repository Alerts Summarized Per Repository With Ordering And
// Freshness"), with a last-execution column added on top of the latest
// run's own status/severity/fixable columns. It is the sole table backing
// the Repository Alerts tab.
func buildAdminScanSummaryTable(theme adminTheme, summaries []repositorySummary, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnScanSummaryRepository, "Repository", adminScanSummaryColumnRepositoryWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryReference, "Reference", adminScanSummaryColumnReferenceWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryStatus, "Status", adminScanSummaryColumnStatusWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryCritical, "Critical", adminScanSummaryColumnCriticalWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryHigh, "High", adminScanSummaryColumnHighWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryFixable, "Fixable", adminScanSummaryColumnFixableWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryRuns, "Runs", adminScanSummaryColumnRunsWidth),
		bubbletable.NewColumn(adminTableColumnScanSummaryLastExecuted, "Last Executed", adminScanSummaryColumnLastExecutedWidth),
	}
	rows := make([]bubbletable.Row, 0, len(summaries))
	for _, summary := range summaries {
		status := adminFirstNonEmpty(summary.LatestRun.Status, "unknown")
		if summary.Disabled {
			// operator-admin-tui spec's "Repository Alerts Renders Override-
			// Disabled Repositories Distinctly" requirement: an explicit
			// state, never confused with "unknown" (never scanned) or the
			// repository's own last scan status (normally scanned).
			status = "scanning disabled"
		}
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnScanSummaryRepository:   adminFirstNonEmpty(summary.Repository, "unknown"),
			adminTableColumnScanSummaryReference:    adminFirstNonEmpty(summary.LatestRun.RequestedRef, "unknown"),
			adminTableColumnScanSummaryStatus:       status,
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

// buildFeatureOverridesTable renders one row per repository (the catalog ∪
// stored-overrides union from mergeFeatureOverrideRows) for
// featureOverridesScreen (screen_gitleaks_repos.go), shared by both
// screenSecurityGitleaksRepos and screenSecuritySigningRepos. The Detail
// column is feature-aware: ports.RepositoryOverrideDetails carries different
// meaningful fields per feature (TrustedPublicKeys for Signing, ConfigPath
// for Gitleaks), so this is the one column that branches on feature.
// Enabled/Detail/Updated all render the "—" placeholder for an inheriting
// (HasOverride=false) row rather than fabricating a value for state that row
// does not own.
//
// The Signing trusted-key count is not cosmetic: a repository with an active
// override and zero trusted keys reports every pull "unverifiable" no matter
// how valid the actual signature is, and that misconfiguration was
// completely invisible in the old plain-text list -- this column makes it
// visible without opening the editor.
func buildFeatureOverridesTable(theme adminTheme, feature string, rows []featureOverrideRow, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(adminTableColumnFeatureOverrideRepository, "Repository", adminFeatureOverrideColumnRepositoryWidth),
		bubbletable.NewColumn(adminTableColumnFeatureOverrideStatus, "Status", adminFeatureOverrideColumnStatusWidth),
		bubbletable.NewColumn(adminTableColumnFeatureOverrideEnabled, "Enabled", adminFeatureOverrideColumnEnabledWidth),
		bubbletable.NewColumn(adminTableColumnFeatureOverrideDetail, "Detail", adminFeatureOverrideColumnDetailWidth),
		bubbletable.NewColumn(adminTableColumnFeatureOverrideUpdated, "Updated", adminFeatureOverrideColumnUpdatedWidth),
	}
	tableRows := make([]bubbletable.Row, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, bubbletable.NewRow(bubbletable.RowData{
			adminTableColumnFeatureOverrideRepository: adminFirstNonEmpty(row.Repository, "unknown"),
			adminTableColumnFeatureOverrideStatus:     featureOverrideRowStatus(row),
			adminTableColumnFeatureOverrideEnabled:    featureOverrideEnabledCell(row),
			adminTableColumnFeatureOverrideDetail:     featureOverrideDetailCell(feature, row),
			adminTableColumnFeatureOverrideUpdated:    featureOverrideUpdatedCell(row),
		}))
	}
	return newAdminBubbleTable(columns, tableRows, highlighted, theme, pageSize)
}

// featureOverridePlaceholder is rendered for any column whose value belongs
// to the override this row does not have (HasOverride=false) -- never a
// fabricated "off"/"0"/blank, an explicit "this row does not own this state".
const featureOverridePlaceholder = "—"

func featureOverrideEnabledCell(row featureOverrideRow) string {
	if !row.HasOverride {
		return featureOverridePlaceholder
	}
	if row.Detail.Enabled {
		return "on"
	}
	return "off"
}

func featureOverrideDetailCell(feature string, row featureOverrideRow) string {
	if !row.HasOverride {
		return featureOverridePlaceholder
	}
	switch feature {
	case signingFeatureName:
		return fmt.Sprintf("%d trusted key(s)", len(row.Detail.TrustedPublicKeys))
	case gitleaksFeatureName:
		return adminFirstNonEmpty(row.Detail.ConfigPath, "default config")
	default:
		return featureOverridePlaceholder
	}
}

func featureOverrideUpdatedCell(row featureOverrideRow) string {
	if !row.HasOverride || row.Detail.UpdatedAt.IsZero() {
		return featureOverridePlaceholder
	}
	return row.Detail.UpdatedAt.UTC().Format("2006-01-02 15:04")
}

// annotateDisabledSummaries marks each summary Disabled when a stored
// override for that repository has Enabled=false (design.md's Open Question
// "List endpoint scope" -- resolved by annotating existing rows, per its own
// listed fallback: a repository that has never been scanned has no summary
// row to annotate in the first place, so injecting a synthetic row is out of
// scope). Pure and side-effect free: returns a new slice, never mutates
// summaries in place.
func annotateDisabledSummaries(summaries []repositorySummary, overrides []ports.RepositoryOverrideDetails) []repositorySummary {
	if len(overrides) == 0 {
		return summaries
	}
	disabled := make(map[string]bool, len(overrides))
	for _, override := range overrides {
		if !override.Enabled {
			disabled[override.Repository] = true
		}
	}
	if len(disabled) == 0 {
		return summaries
	}
	annotated := make([]repositorySummary, len(summaries))
	for i, summary := range summaries {
		summary.Disabled = disabled[summary.Repository]
		annotated[i] = summary
	}
	return annotated
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
// adminScanHistoryModalRows (admin_scan_history.go) already reserves the
// modal's own border+padding exactly once via `l.Height - sectionChromeRows`,
// so the rows it returns are already a border-free content budget —
// re-subtracting sectionChromeRows here would charge the same border twice
// and starve the table for no reason.
const adminScanHistoryModalChromeRows = 4

// adminScanHistoryModalMinTableBudget is the smallest number of rows a
// bordered table needs at all: its own fixed chrome (tableChromeRows) plus
// the smallest usable pageSize (minTableRows). Below this, the modal
// substitutes a single-line message instead of ever slicing a bordered
// table (design.md "the table is replaced by a single-line substitute,
// never sliced").
const adminScanHistoryModalMinTableBudget = tableChromeRows + minTableRows

// adminScanHistoryModalTablePageSize computes the scan history modal's
// active-tab table pageSize from the modal's own content budget (modalRows,
// from adminScanHistoryModalRows), the modal's fixed chrome
// (adminScanHistoryModalChromeRows), and the measured height of any
// variable header content rendered above the table (an error or loading
// message) — measured, not guessed. Floored at minTableRows.
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

// tableRoles splits a screen's row budget into two admin-table pageSize
// roles (design.md decision #6): primary tables (Features, ScanSummary — the
// operator-navigable top-level lists) get the whole budget available to a
// standalone table so a single-table screen fits exactly without relying on
// the outer clip; compact tables (FeatureRows — always paired with a
// primary selection) are capped at compactTableRows, floored at
// minTableRows. When several tables stack on one screen, their combined
// height can still exceed l.SectionRows — renderSection's outer-pane clip
// (Phase 3) is the structural guarantee against overflow there, not this
// split; this split is ergonomics only.
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

// rebuildAdminTables keeps AdminViewState.Layout's Primary/Compact split
// fresh (design.md decision #6) -- a handful of tests read it as a proxy
// for "the row budget this screen was sized against". Every admin table
// itself is now built by its own owning screen (securityMenuScreen/
// trivyConfigScreen/trivyReposScreen's own fields since Phase 11;
// scanHistoryScreen's own findings/secretFindings fields since Phase 19,
// design.md's State Migration table) from inside its own Update, broadcast
// the same way every other migrated screen refreshes on tea.WindowSizeMsg
// (design.md Decision H). Still called unconditionally after almost every
// admin action -- harmless/cheap, this method itself now does no table
// construction of its own.
func (m *Model) rebuildAdminTables(layout consoleLayout) {
	layout.Primary, layout.Compact = tableRoles(layout)
	m.adminView.Layout = layout
}

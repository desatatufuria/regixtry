package tui

import (
	"strings"
	"time"

	bubbletable "github.com/evertras/bubble-table/table"
	appregixtry "regixtry/internal/app/regixtry"
)

const (
	consoleTableColumnTagName    = "console_tag_name"
	consoleTableColumnTagCreated = "console_tag_created"
	consoleTableColumnTagSigned  = "console_tag_signed"
)

// Column widths, measured against realistic longest values the same way
// admin_tables.go's own widths are (its own doc comment's discipline):
// consoleTagsColumnNameWidth against a realistic long tag name (semver +
// variant suffix), consoleTagsColumnCreatedWidth against
// formatTagCreatedAt's longest output ("YYYY-MM-DD HH:MM"),
// consoleTagsColumnSignedWidth against the Signed column's longest value
// ("n/a").
const (
	consoleTagsColumnNameWidth    = 34
	consoleTagsColumnCreatedWidth = 20
	consoleTagsColumnSignedWidth  = 8
)

// buildConsoleTagsTable renders the Console TUI's per-repository Tags screen
// as a 3-column table (Tag, Created, Signed) -- the user's deliberately
// minimal column set, cross-checked against Harbor's own richer Tags view
// and intentionally narrower since vulnerability/size data already live
// elsewhere in this app. Mirrors admin_tables.go's own
// newAdminBubbleTable-based construction pattern (sole construction point,
// theme-driven, pageSize-driven), on the Console side rather than the admin
// side.
func buildConsoleTagsTable(theme adminTheme, tags []appregixtry.TagDetails, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(consoleTableColumnTagName, "Tag", consoleTagsColumnNameWidth),
		bubbletable.NewColumn(consoleTableColumnTagCreated, "Created", consoleTagsColumnCreatedWidth),
		bubbletable.NewColumn(consoleTableColumnTagSigned, "Signed", consoleTagsColumnSignedWidth),
	}
	rows := make([]bubbletable.Row, 0, len(tags))
	for _, tag := range tags {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			consoleTableColumnTagName:    tag.Name,
			consoleTableColumnTagCreated: formatTagCreatedAt(tag.CreatedAt),
			consoleTableColumnTagSigned:  tagSignedLabel(tag),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, theme, pageSize)
}

// formatTagCreatedAt renders a tag's manifest creation time, never blank --
// mirrors formatScanSummaryLastExecuted's own "unknown" fallback for a zero
// time.Time.
func formatTagCreatedAt(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// tagSignedLabel maps SignatureStatus's existing five-state vocabulary
// (reused verbatim, never reinvented) to the Signed column's text-only
// yes/no/n-a value -- this TUI's established, deliberate no-icon/no-glyph
// convention (scanPolicyBadge, signingPolicyBadge), even though Harbor's own
// real UI uses a checkmark/X icon here. "n/a" when the signing policy is
// disabled entirely (TagDetails.SigningEnabled mirrors
// SignatureStatusResult.Policy.Enabled, the exact same bit
// signingFeatureName's own enable toggle flips -- design.md Decision 10's
// "one bit, one source of truth"): a Signed verdict is not meaningful when
// nothing is being enforced. "yes" only for the verified state; "no" for
// every other computed state while the policy is enabled.
func tagSignedLabel(tag appregixtry.TagDetails) string {
	if !tag.SigningEnabled {
		return "n/a"
	}
	if tag.SignatureState == appregixtry.SignatureStatusVerified {
		return "yes"
	}
	return "no"
}

// consoleTagsTablePageSize computes the Tags table's pageSize from the
// screen's own content budget, reserving exactly one row for the "Tags"
// subheading rendered above it -- mirrors
// adminScanHistoryModalTablePageSize's own fixed-header-row accounting -- so
// the section's total rendered height exactly matches layout.SectionRows and
// renderSection's fitLines never clips mid-table. Floored at minTableRows,
// same floor every other table in this package uses.
func consoleTagsTablePageSize(layout consoleLayout) int {
	available := layout.SectionRows - 1 - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	return available
}

// renderConsoleTagsSection builds the Tags screen's inner content
// (subheading + table, or a muted empty message), fitted the same way every
// other screen's bounded section is (renderSection).
func renderConsoleTagsSection(tags TagsModel, layout consoleLayout) string {
	theme := newAdminTheme()
	lines := []string{theme.subheading.Render("Tags")}
	if len(tags.Items) == 0 {
		lines = append(lines, theme.muted.Render("No items available."))
	} else {
		lines = append(lines, tags.Table.View())
	}
	return renderSection(theme, strings.Join(lines, "\n"), layout)
}

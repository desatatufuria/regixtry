package tui

import (
	"fmt"
	"strings"
	"time"

	bubbletable "github.com/evertras/bubble-table/table"
	appregixtry "regixtry/internal/app/regixtry"
)

const (
	consoleTableColumnTagName     = "console_tag_name"
	consoleTableColumnTagCreated  = "console_tag_created"
	consoleTableColumnTagPushedBy = "console_tag_pushed_by"
	consoleTableColumnTagSigned   = "console_tag_signed"
)

// Column widths, measured against realistic longest values the same way
// admin_tables.go's own widths are (its own doc comment's discipline):
// consoleTagsColumnNameWidth against a realistic long tag name (semver +
// variant suffix), consoleTagsColumnCreatedWidth against
// formatTagCreatedAt's longest output ("YYYY-MM-DD HH:MM"),
// consoleTagsColumnPushedByWidth against a realistic username/robot-account
// name (console-tags-pushed-by change), consoleTagsColumnSignedWidth
// against the Signed column's longest value ("n/a").
const (
	consoleTagsColumnNameWidth     = 34
	consoleTagsColumnCreatedWidth  = 20
	consoleTagsColumnPushedByWidth = 20
	consoleTagsColumnSignedWidth   = 8
)

// buildConsoleTagsTable renders the Console TUI's per-repository Tags screen
// as a 4-column table (Tag, Created, Pushed By, Signed) -- the user's
// deliberately minimal column set, cross-checked against Harbor's own
// richer Tags view and intentionally narrower since vulnerability/size data
// already live elsewhere in this app. Mirrors admin_tables.go's own
// newAdminBubbleTable-based construction pattern (sole construction point,
// theme-driven, pageSize-driven), on the Console side rather than the admin
// side.
func buildConsoleTagsTable(theme adminTheme, tags []appregixtry.TagDetails, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(consoleTableColumnTagName, "Tag", consoleTagsColumnNameWidth),
		bubbletable.NewColumn(consoleTableColumnTagCreated, "Created", consoleTagsColumnCreatedWidth),
		bubbletable.NewColumn(consoleTableColumnTagPushedBy, "Pushed By", consoleTagsColumnPushedByWidth),
		bubbletable.NewColumn(consoleTableColumnTagSigned, "Signed", consoleTagsColumnSignedWidth),
	}
	rows := make([]bubbletable.Row, 0, len(tags))
	for _, tag := range tags {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			consoleTableColumnTagName:     tag.Name,
			consoleTableColumnTagCreated:  formatTagCreatedAt(tag.CreatedAt),
			consoleTableColumnTagPushedBy: tagPushedByLabel(tag),
			consoleTableColumnTagSigned:   tagSignedLabel(tag),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, pageSize)
}

// tagPushedByLabel renders a tag's resolved pusher username, never blank --
// mirrors formatTagCreatedAt's own "unknown" fallback, covering a legacy
// manifest (pushed before pushed_by existed), a manifest pushed with no
// UsernameResolver configured, or a pusher the resolver has no username for
// (e.g. since deleted) -- TagDetails.PushedBy is already "" in all three
// cases, and this label is the only place that turns "" into display text.
func tagPushedByLabel(tag appregixtry.TagDetails) string {
	if strings.TrimSpace(tag.PushedBy) == "" {
		return "unknown"
	}
	return tag.PushedBy
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
	// The current sort mode is appended to the subheading (sortable-tags-
	// and-projects feature) -- discoverable at a glance without needing a
	// separate status line, this codebase's established text-only
	// convention (no icons/glyphs).
	lines := []string{theme.subheading.Render(fmt.Sprintf("Tags (sort: %s)", tags.SortMode.label()))}
	if len(tags.Items) == 0 {
		lines = append(lines, theme.muted.Render("No items available."))
	} else {
		lines = append(lines, tags.Table.View())
	}
	return renderSection(theme, strings.Join(lines, "\n"), layout)
}

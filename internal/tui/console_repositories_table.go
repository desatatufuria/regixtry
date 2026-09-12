package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	bubbletable "github.com/evertras/bubble-table/table"
	appregixtry "regixtry/internal/app/regixtry"
)

const (
	consoleTableColumnRepositoryName       = "console_repository_name"
	consoleTableColumnRepositoryTags       = "console_repository_tags"
	consoleTableColumnRepositoryLastPushed = "console_repository_last_pushed"
)

// Column widths, measured against realistic longest values the same way
// console_tags_table.go's own widths are: consoleRepositoriesColumnNameWidth
// against a realistic long org/team/service-style repository name,
// consoleRepositoriesColumnTagsWidth against the "Tags" header plus a
// several-digit count, consoleRepositoriesColumnLastPushedWidth against
// formatRepositoryLastPushed's longest output ("YYYY-MM-DD HH:MM").
const (
	consoleRepositoriesColumnNameWidth       = 34
	consoleRepositoriesColumnTagsWidth       = 6
	consoleRepositoriesColumnLastPushedWidth = 20
)

// buildConsoleRepositoriesTable renders the Console TUI's top-level
// Repositories screen as a 3-column table (Name, Tags, Last Pushed) --
// cross-checked against Harbor's own repository-list view (Name, Artifacts,
// Pulls, Last Modified Time), deliberately excluding Pulls since this
// codebase has no pull-count tracking (out of scope, not new
// instrumentation). Mirrors buildConsoleTagsTable's own construction
// pattern one level up, backed by Service.RepositorySummaries instead of
// Service.TagDetails.
func buildConsoleRepositoriesTable(theme adminTheme, repositories []appregixtry.RepositorySummary, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(consoleTableColumnRepositoryName, "Name", consoleRepositoriesColumnNameWidth),
		bubbletable.NewColumn(consoleTableColumnRepositoryTags, "Tags", consoleRepositoriesColumnTagsWidth),
		bubbletable.NewColumn(consoleTableColumnRepositoryLastPushed, "Last Pushed", consoleRepositoriesColumnLastPushedWidth),
	}
	rows := make([]bubbletable.Row, 0, len(repositories))
	for _, repository := range repositories {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			consoleTableColumnRepositoryName:       repository.Name,
			consoleTableColumnRepositoryTags:       strconv.Itoa(repository.TagCount),
			consoleTableColumnRepositoryLastPushed: formatRepositoryLastPushed(repository.LastPushed),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, pageSize)
}

// formatRepositoryLastPushed renders a repository's most recent push time,
// never blank -- mirrors formatTagCreatedAt's own "unknown" fallback for a
// zero time.Time (a repository with zero tags has no push time to
// aggregate).
func formatRepositoryLastPushed(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// consoleRepositoriesTablePageSize computes the Repositories table's
// pageSize from the screen's own content budget, reserving exactly one row
// for the "Repositories" subheading rendered above it -- mirrors
// consoleTagsTablePageSize's own fixed-header-row accounting -- so the
// section's total rendered height exactly matches layout.SectionRows and
// renderSection's fitLines never clips mid-table. Floored at minTableRows,
// same floor every other table in this package uses.
func consoleRepositoriesTablePageSize(layout consoleLayout) int {
	available := layout.SectionRows - 1 - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	return available
}

// renderConsoleRepositoriesSection builds the Repositories screen's inner
// content (subheading + table, or a muted empty message), fitted the same
// way every other screen's bounded section is (renderSection) -- mirrors
// renderConsoleTagsSection one level up.
func renderConsoleRepositoriesSection(repositories RepositoriesModel, layout consoleLayout) string {
	theme := newAdminTheme()
	// An active project filter (path-based-project-grouping feature) is
	// named in the subheading itself -- the operator must always be able to
	// tell at a glance whether they are looking at all repositories or one
	// project's subset.
	heading := "Repositories"
	if repositories.Filter != nil {
		heading = fmt.Sprintf("Repositories (project: %s)", repositories.Filter.Project)
	}
	lines := []string{theme.subheading.Render(heading)}
	if len(repositories.FilteredItems()) == 0 {
		lines = append(lines, theme.muted.Render("No items available."))
	} else {
		lines = append(lines, repositories.Table.View())
	}
	return renderSection(theme, strings.Join(lines, "\n"), layout)
}

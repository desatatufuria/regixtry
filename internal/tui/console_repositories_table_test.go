package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	appregixtry "regixtry/internal/app/regixtry"
)

// TestBuildConsoleRepositoriesTableRendersNameTagsLastPushedColumns is the
// RED test for the console-repositories-table change: the top-level
// Repositories screen's table has exactly the three columns the user chose
// (Name, Tags, Last Pushed -- Harbor's own "Pulls" column deliberately
// excluded, out of scope), and each row's cells carry the repository name,
// its tag count, and a formatted last-pushed timestamp.
func TestBuildConsoleRepositoriesTableRendersNameTagsLastPushedColumns(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	pushed := time.Date(2026, 8, 10, 9, 15, 0, 0, time.UTC)
	repositories := []appregixtry.RepositorySummary{
		{Name: "library/alpine", TagCount: 12, LastPushed: pushed},
		{Name: "team/platform-api-gateway-service", TagCount: 3, LastPushed: pushed},
		{Name: "library/untagged", TagCount: 0, LastPushed: time.Time{}},
	}

	table := buildConsoleRepositoriesTable(theme, repositories, 0, minTableRows)
	if got, want := table.TotalRows(), len(repositories); got != want {
		t.Fatalf("buildConsoleRepositoriesTable() TotalRows() = %d, want %d", got, want)
	}

	view := table.View()
	for _, want := range []string{"Name", "Tags", "Last Pushed", "library/alpine", "team/platform-api-gateway-service", "library/untagged", "12", "3", "0", "2026-08-10 09:15", "unknown"} {
		if !strings.Contains(view, want) {
			t.Fatalf("table view = %q, want it to contain %q", view, want)
		}
	}
}

// TestFormatRepositoryLastPushedNeverBlank mirrors
// TestFormatTagCreatedAtNeverBlank's own zero-time fallback discipline: a
// repository with zero tags has no push time to show, but the cell must
// never render blank.
func TestFormatRepositoryLastPushedNeverBlank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "non-zero time renders formatted date", when: time.Date(2026, 8, 10, 9, 15, 0, 0, time.UTC), want: "2026-08-10 09:15"},
		{name: "zero time still renders non-blank text", when: time.Time{}, want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatRepositoryLastPushed(tc.when)
			if got != tc.want {
				t.Fatalf("formatRepositoryLastPushed() = %q, want %q", got, tc.want)
			}
			if strings.TrimSpace(got) == "" {
				t.Fatalf("formatRepositoryLastPushed() returned blank text, want never blank")
			}
		})
	}
}

// TestConsoleRepositoriesTablePageSizeFloorsAtMinTableRows mirrors
// TestConsoleTagsTablePageSizeFloorsAtMinTableRows: a tiny/negative budget
// must never produce a degenerate table height.
func TestConsoleRepositoriesTablePageSizeFloorsAtMinTableRows(t *testing.T) {
	t.Parallel()

	got := consoleRepositoriesTablePageSize(consoleLayout{SectionRows: 0})
	if got != minTableRows {
		t.Fatalf("consoleRepositoriesTablePageSize(SectionRows=0) = %d, want floor %d", got, minTableRows)
	}
}

// TestBuildConsoleRepositoriesTableColumnsFitWithoutOverflow mirrors
// TestBuildConsoleTagsTableColumnsFitWithoutOverflow: no rendered line may
// exceed the table's own declared column widths, even with a long
// repository name.
func TestBuildConsoleRepositoriesTableColumnsFitWithoutOverflow(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	repositories := []appregixtry.RepositorySummary{
		{Name: "acme-corp/platform-services/api-gateway-edge-router", TagCount: 999, LastPushed: time.Now()},
	}
	table := buildConsoleRepositoriesTable(theme, repositories, 0, minTableRows)
	view := table.View()

	wantWidth := consoleRepositoriesColumnNameWidth + consoleRepositoriesColumnTagsWidth + consoleRepositoriesColumnLastPushedWidth + 4 // +4: 3 columns' own border/separator characters (cols+1)
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > wantWidth {
			t.Fatalf("line %d width = %d, want <= %d (table overflowed its own declared column widths):\n%s", i, w, wantWidth, view)
		}
	}
}

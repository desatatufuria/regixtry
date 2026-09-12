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
	consoleTableColumnProjectName       = "console_project_name"
	consoleTableColumnProjectRepos      = "console_project_repos"
	consoleTableColumnProjectTags       = "console_project_tags"
	consoleTableColumnProjectLastPushed = "console_project_last_pushed"
)

// Column widths, measured the same way console_repositories_table.go's own
// widths are.
const (
	consoleProjectsColumnNameWidth       = 34
	consoleProjectsColumnReposWidth      = 6
	consoleProjectsColumnTagsWidth       = 6
	consoleProjectsColumnLastPushedWidth = 20
)

// projectUngroupedLabel is the implicit bucket every flat (no "/") repository
// name collapses into (path-based-project-grouping feature): the user
// explicitly decided existing flat-named demo repositories are never
// retroactively grouped into invented projects, but they must still be
// reachable from the Projects screen, under one shared, visually distinct
// label that can never collide with a real project name (a real repository
// name's first path segment is validated elsewhere to disallow the
// parenthesis characters this label uses).
const projectUngroupedLabel = "(ungrouped)"

// projectSummary is one derived project row: RepositoryCount and TagCount
// are aggregated across every repository in the project (TagCount is a
// SUM, not a repository-level value), and LastPushed is the most recent
// push across all of them. Purely a client-side derivation over the
// already-fetched []appregixtry.RepositorySummary list -- never persisted,
// never a new backend concept (design.md's own framing: "purely a naming
// convention already supported end-to-end").
type projectSummary struct {
	Name            string
	RepositoryCount int
	TagCount        int
	LastPushed      time.Time
}

// repositoryProjectName derives one repository's project label: its name's
// first "/"-separated segment when present (Docker Hub/GHCR/ECR's own
// established convention), else the shared projectUngroupedLabel bucket.
func repositoryProjectName(repository string) string {
	if index := strings.Index(repository, "/"); index >= 0 {
		return repository[:index]
	}
	return projectUngroupedLabel
}

// deriveProjects groups repositories by repositoryProjectName and
// aggregates each group into one projectSummary row, returned sorted by
// Name ascending (a deterministic baseline order independent of Go's
// randomized map iteration -- the Projects screen's own "s" key re-sorts
// from here when the operator wants date-descending instead).
func deriveProjects(repositories []appregixtry.RepositorySummary) []projectSummary {
	order := make([]string, 0, len(repositories))
	byName := make(map[string]*projectSummary, len(repositories))

	for _, repository := range repositories {
		name := repositoryProjectName(repository.Name)
		summary, ok := byName[name]
		if !ok {
			summary = &projectSummary{Name: name}
			byName[name] = summary
			order = append(order, name)
		}
		summary.RepositoryCount++
		summary.TagCount += repository.TagCount
		if repository.LastPushed.After(summary.LastPushed) {
			summary.LastPushed = repository.LastPushed
		}
	}

	projects := make([]projectSummary, 0, len(order))
	for _, name := range order {
		projects = append(projects, *byName[name])
	}
	sortProjectItems(projects, sortByNameAsc)
	return projects
}

// buildConsoleProjectsTable renders the Console TUI's Projects screen as a
// 4-column table (Project, Repos, Tags, Last Pushed) -- mirrors
// buildConsoleRepositoriesTable's own construction pattern one screen over,
// backed by the client-side deriveProjects instead of a Service call.
func buildConsoleProjectsTable(theme adminTheme, projects []projectSummary, highlighted int, pageSize int) bubbletable.Model {
	columns := []bubbletable.Column{
		bubbletable.NewColumn(consoleTableColumnProjectName, "Project", consoleProjectsColumnNameWidth),
		bubbletable.NewColumn(consoleTableColumnProjectRepos, "Repos", consoleProjectsColumnReposWidth),
		bubbletable.NewColumn(consoleTableColumnProjectTags, "Tags", consoleProjectsColumnTagsWidth),
		bubbletable.NewColumn(consoleTableColumnProjectLastPushed, "Last Pushed", consoleProjectsColumnLastPushedWidth),
	}
	rows := make([]bubbletable.Row, 0, len(projects))
	for _, project := range projects {
		rows = append(rows, bubbletable.NewRow(bubbletable.RowData{
			consoleTableColumnProjectName:       project.Name,
			consoleTableColumnProjectRepos:      strconv.Itoa(project.RepositoryCount),
			consoleTableColumnProjectTags:       strconv.Itoa(project.TagCount),
			consoleTableColumnProjectLastPushed: formatRepositoryLastPushed(project.LastPushed),
		}))
	}
	return newAdminBubbleTable(columns, rows, highlighted, pageSize)
}

// consoleProjectsTablePageSize mirrors consoleRepositoriesTablePageSize's
// own fixed-header-row accounting exactly.
func consoleProjectsTablePageSize(layout consoleLayout) int {
	available := layout.SectionRows - 1 - tableChromeRows
	if available < minTableRows {
		available = minTableRows
	}
	return available
}

// renderConsoleProjectsSection builds the Projects screen's inner content
// (subheading + table, or a muted empty message), mirroring
// renderConsoleRepositoriesSection one level up. The current sort mode is
// appended to the subheading exactly like renderConsoleTagsSection's own
// (sortable-tags-and-projects feature).
func renderConsoleProjectsSection(projects ProjectsModel, layout consoleLayout) string {
	theme := newAdminTheme()
	lines := []string{theme.subheading.Render(fmt.Sprintf("Projects (sort: %s)", projects.SortMode.label()))}
	if len(projects.Items) == 0 {
		lines = append(lines, theme.muted.Render("No items available."))
	} else {
		lines = append(lines, projects.Table.View())
	}
	return renderSection(theme, strings.Join(lines, "\n"), layout)
}

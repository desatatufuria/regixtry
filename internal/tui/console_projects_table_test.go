package tui

import (
	"strings"
	"testing"
	"time"

	appregixtry "regixtry/internal/app/regixtry"
)

// TestDeriveProjectsGroupsByFirstPathSegmentAndAggregates is the RED test
// for the path-based-project-grouping feature's pure derivation function:
// repositories sharing a first "/"-segment collapse into one project row
// with the repository count, the SUM of their tag counts, and the MOST
// RECENT LastPushed across them.
func TestDeriveProjectsGroupsByFirstPathSegmentAndAggregates(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	repositories := []appregixtry.RepositorySummary{
		{Name: "team/az-deploy-demo", TagCount: 2, LastPushed: older},
		{Name: "team/other-service", TagCount: 5, LastPushed: newer},
		{Name: "smoke/keyless-live-verify", TagCount: 1, LastPushed: older},
	}

	projects := deriveProjects(repositories)

	var team, smoke *projectSummary
	for i := range projects {
		switch projects[i].Name {
		case "team":
			team = &projects[i]
		case "smoke":
			smoke = &projects[i]
		}
	}
	if team == nil {
		t.Fatalf("projects = %#v, want a %q project", projects, "team")
	}
	if team.RepositoryCount != 2 {
		t.Fatalf("team.RepositoryCount = %d, want 2", team.RepositoryCount)
	}
	if team.TagCount != 7 {
		t.Fatalf("team.TagCount = %d, want 7 (sum of 2+5)", team.TagCount)
	}
	if !team.LastPushed.Equal(newer) {
		t.Fatalf("team.LastPushed = %v, want the most recent %v", team.LastPushed, newer)
	}
	if smoke == nil {
		t.Fatalf("projects = %#v, want a %q project", projects, "smoke")
	}
	if smoke.RepositoryCount != 1 || smoke.TagCount != 1 {
		t.Fatalf("smoke = %#v, want RepositoryCount=1 TagCount=1", smoke)
	}
}

// TestDeriveProjectsGroupsFlatRepositoriesIntoOneUngroupedBucket covers the
// user's explicit decision: flat (no "/") repository names never get
// retroactively grouped into invented projects -- they all collapse into
// ONE implicit "(ungrouped)" bucket, visually distinct from any real
// project name.
func TestDeriveProjectsGroupsFlatRepositoriesIntoOneUngroupedBucket(t *testing.T) {
	t.Parallel()

	repositories := []appregixtry.RepositorySummary{
		{Name: "govault-api", TagCount: 3},
		{Name: "alpine", TagCount: 2},
		{Name: "team/app", TagCount: 1},
	}

	projects := deriveProjects(repositories)

	var ungrouped *projectSummary
	for i := range projects {
		if projects[i].Name == projectUngroupedLabel {
			ungrouped = &projects[i]
		}
	}
	if ungrouped == nil {
		t.Fatalf("projects = %#v, want an ungrouped bucket named %q", projects, projectUngroupedLabel)
	}
	if ungrouped.RepositoryCount != 2 {
		t.Fatalf("ungrouped.RepositoryCount = %d, want 2 (govault-api + alpine collapsed into one bucket)", ungrouped.RepositoryCount)
	}
	if ungrouped.TagCount != 5 {
		t.Fatalf("ungrouped.TagCount = %d, want 5", ungrouped.TagCount)
	}
	if len(projects) != 2 {
		t.Fatalf("len(projects) = %d, want 2 (team, ungrouped)", len(projects))
	}
}

// TestBuildConsoleProjectsTableRendersNameRepoTagLastPushedColumns mirrors
// TestBuildConsoleRepositoriesTable's own construction-pattern test one
// level up.
func TestBuildConsoleProjectsTableRendersNameRepoTagLastPushedColumns(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	pushed := time.Date(2026, 8, 10, 9, 15, 0, 0, time.UTC)
	projects := []projectSummary{
		{Name: "team", RepositoryCount: 2, TagCount: 7, LastPushed: pushed},
		{Name: projectUngroupedLabel, RepositoryCount: 5, TagCount: 12},
	}

	table := buildConsoleProjectsTable(theme, projects, 0, minTableRows)
	if got, want := table.TotalRows(), len(projects); got != want {
		t.Fatalf("TotalRows() = %d, want %d", got, want)
	}

	view := table.View()
	for _, want := range []string{"Project", "Repos", "Tags", "Last Pushed", "team", projectUngroupedLabel, "2", "7", "2026-08-10 09:15", "5", "12", "unknown"} {
		if !strings.Contains(view, want) {
			t.Fatalf("table view = %q, want it to contain %q", view, want)
		}
	}
}

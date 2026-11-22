package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	appregixtry "regixtry/internal/app/regixtry"
)

// projectSummary is one derived project row: RepositoryCount and TagCount
// aggregate every repository of the project and LastPushed is the most recent
// push across them. A project is a client-side grouping, never a backend
// concept: the first path segment of a repository name.
type projectSummary struct {
	Name            string
	RepositoryCount int
	TagCount        int
	LastPushed      time.Time
}

// projectUngroupedLabel is the bucket every flat (no "/") repository name
// collapses into, so existing flat-named repositories stay reachable. It can
// never collide with a real project name: a repository name's first segment
// cannot hold parentheses.
const projectUngroupedLabel = "(ungrouped)"

// repositoryProjectName derives a repository's project: its name's first
// "/"-separated segment, or the ungrouped bucket.
func repositoryProjectName(repository string) string {
	if index := strings.Index(repository, "/"); index >= 0 {
		return repository[:index]
	}
	return projectUngroupedLabel
}

// deriveProjects groups repositories by project, sorted by name.
func deriveProjects(repositories []appregixtry.RepositorySummary) []projectSummary {
	byName := make(map[string]*projectSummary, len(repositories))
	var order []string
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

// repositoriesInProject names every repository of a project with the SAME
// grouping deriveProjects uses, so the delete can never disagree with the
// table about what a project holds.
func repositoriesInProject(repositories []appregixtry.RepositorySummary, project string) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		if repositoryProjectName(repository.Name) == project {
			names = append(names, repository.Name)
		}
	}
	return names
}

// consoleProjectsScreen is "Console > Projects" (the Projects board): the
// projects derived from the catalog. It is derived at mount from a copy of the
// catalog, with no load of its own except the refresh after a delete.
type consoleProjectsScreen struct {
	gen          uint64
	repositories []appregixtry.RepositorySummary
	projects     []projectSummary
	sortMode     sortMode
	selected     int

	confirm    confirmPrompt
	busy       bool
	sync       consoleSync
	notice     string
	noticeFail bool
}

func newConsoleProjectsScreen(repositories []appregixtry.RepositorySummary) consoleProjectsScreen {
	s := consoleProjectsScreen{gen: consoleGenerations.Add(1), repositories: slices.Clone(repositories)}
	s.projects = deriveProjects(s.repositories)
	return s
}

func (s consoleProjectsScreen) ID() screen { return screenProjects }

func (s consoleProjectsScreen) Init(screenEnv) tea.Cmd { return nil }

func (s consoleProjectsScreen) selectedProject() (projectSummary, bool) {
	if len(s.projects) == 0 {
		return projectSummary{}, false
	}
	return s.projects[boundedIndex(s.selected, len(s.projects))], true
}

// withRepositories re-derives the projects from a new catalog, keeping the
// sort and the cursor on the project of this name when it is still listed.
func (s consoleProjectsScreen) withRepositories(repositories []appregixtry.RepositorySummary, keep string) consoleProjectsScreen {
	s.repositories = repositories
	s.projects = deriveProjects(repositories)
	sortProjectItems(s.projects, s.sortMode)
	s.selected = boundedIndex(s.selected, len(s.projects))
	if i := slices.IndexFunc(s.projects, func(p projectSummary) bool { return p.Name == keep }); i >= 0 {
		s.selected = i
	}
	return s
}

// ---- messages ----

// projectDeletionResult is one repository's outcome inside a project delete.
type projectDeletionResult struct {
	repository string
	err        error
}

// projectDeletedMsg is the aggregate result of a project delete: one entry per
// repository the project held, in attempt order, so a partial failure is
// always reported honestly.
type projectDeletedMsg struct {
	gen     uint64
	project string
	results []projectDeletionResult
}

// removed names the repositories whose delete succeeded.
func (m projectDeletedMsg) removed() []string {
	var names []string
	for _, r := range m.results {
		if r.err == nil {
			names = append(names, r.repository)
		}
	}
	return names
}

func (s consoleProjectsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case consoleCatalogMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyCatalog(typed), nil, false
	case projectDeletedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyDeleted(env, typed)
	}
	return s, nil, false
}

func (s consoleProjectsScreen) applyCatalog(msg consoleCatalogMsg) consoleProjectsScreen {
	sync, ok, hide := s.sync.accept(msg.stamp)
	if !ok {
		return s
	}
	s.sync = sync
	if msg.err != nil {
		s.notice, s.noticeFail = "Could not refresh projects: "+msg.err.Error(), true
		return s
	}
	keep := ""
	if current, ok := s.selectedProject(); ok {
		keep = current.Name
	}
	repositories := slices.DeleteFunc(slices.Clone(msg.result), func(r appregixtry.RepositorySummary) bool { return hide && s.sync.deleted[r.Name] })
	return s.withRepositories(repositories, keep)
}

func (s consoleProjectsScreen) applyDeleted(env screenEnv, msg projectDeletedMsg) (adminScreen, tea.Cmd, bool) {
	s.confirm, s.busy = confirmPrompt{}, false
	removed := msg.removed()
	if len(removed) > 0 {
		s.sync = s.sync.withDeleted(removed...)
		s = s.withRepositories(slices.DeleteFunc(slices.Clone(s.repositories), func(r appregixtry.RepositorySummary) bool { return slices.Contains(removed, r.Name) }), "")
	}
	s.notice, s.noticeFail = projectDeletionSummary(msg.project, msg.results), len(removed) < len(msg.results)
	if len(removed) == 0 {
		return s, nil, false
	}
	sync, stamp := s.sync.launch()
	s.sync = sync
	return s, consoleCatalogCmd(env, s.gen, stamp), false
}

// projectDeletionSummary turns a project delete's mixed per-repository results
// into one honest line: a partial failure is never collapsed into "succeeded"
// or "failed", and every failed repository is named with its reason.
func projectDeletionSummary(project string, results []projectDeletionResult) string {
	var succeeded, failed []string
	for _, result := range results {
		if result.err != nil {
			failed = append(failed, fmt.Sprintf("%s (%s)", result.repository, result.err.Error()))
			continue
		}
		succeeded = append(succeeded, result.repository)
	}
	switch {
	case len(failed) == 0:
		return fmt.Sprintf("Project %q deleted: %d repository(s) removed. Refreshing projects...", project, len(succeeded))
	case len(succeeded) == 0:
		return fmt.Sprintf("Project %q delete failed for all %d repository(s): %s", project, len(failed), strings.Join(failed, "; "))
	default:
		return fmt.Sprintf("Project %q partially deleted: %d succeeded, %d failed (%s). Refreshing projects...", project, len(succeeded), len(failed), strings.Join(failed, "; "))
	}
}

func (s consoleProjectsScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, _ := s.confirm.update(env, msg)
		s.confirm = next
		s.busy = s.busy || cmd != nil
		return s, cmd, true
	}
	current, has := s.selectedProject()
	if cursor, moved := consoleMove(msg, s.selected, len(s.projects)); moved {
		s.selected = cursor
		return s, nil, true
	}
	switch {
	case isBackKey(msg):
		return s, navigate(screenRepositories), true
	case isTabKey(msg):
		// The parent opens the admin panel whenever an admin API exists.
		if env.Client == nil {
			s.notice, s.noticeFail = consoleUnavailableNotice, true
			return s, nil, true
		}
		return s, nil, false
	case isEnterKey(msg):
		if has {
			return s, consoleAsk(openRepositoriesMsg{project: current.Name}), true
		}
	case isCommandRune(msg, 's'):
		s.sortMode = s.sortMode.next()
		s.selected = 0
		sortProjectItems(s.projects, s.sortMode)
	case isCommandRune(msg, 'd'):
		switch {
		case s.busy:
			// A delete is already on its way.
		case !has:
			s.notice, s.noticeFail = "No project selected to delete.", true
		default:
			s.confirm = s.deleteConfirm(current)
			s.notice = ""
		}
	}
	return s, nil, true
}

// deleteConfirm builds the confirmation of deleting a whole project. A project
// has no backend record: the delete calls Service.DeleteRepository once for
// every repository whose name starts with the project, each in its own
// transaction, and goes on after a failure. The repositories are fixed here,
// so the command deletes exactly what the dialog counts.
func (s consoleProjectsScreen) deleteConfirm(project projectSummary) confirmPrompt {
	gen, name := s.gen, project.Name
	repositories := repositoriesInProject(s.repositories, name)
	return newConfirmPrompt(
		"Delete project "+name,
		fmt.Sprintf("%s of the project are removed, with %s and every manifest.",
			countOf(len(repositories), "repository", "repositories"), countOf(project.TagCount, "tag", "tags")),
		"delete",
		fmt.Sprintf("Deleting project %s...", name),
		func(env screenEnv) tea.Cmd { return projectDeleteCmd(env, gen, name, repositories) },
	).withTarget(name, projectDetail(project)).withButtonLabel("Delete").
		withConsequence("Each repository is deleted on its own: if one fails the others are still removed. Blob files stay until garbage collection. This cannot be undone.")
}

func projectDeleteCmd(env screenEnv, gen uint64, project string, repositories []string) tea.Cmd {
	service := env.Service
	return func() tea.Msg {
		results := make([]projectDeletionResult, 0, len(repositories))
		for _, repository := range repositories {
			if service == nil {
				results = append(results, projectDeletionResult{repository: repository, err: errConsoleUnavailable})
				continue
			}
			_, err := service.DeleteRepository(context.Background(), repository)
			results = append(results, projectDeletionResult{repository: repository, err: err})
		}
		return projectDeletedMsg{gen: gen, project: project, results: results}
	}
}

func projectDetail(project projectSummary) string {
	return countOf(project.RepositoryCount, "repository", "repositories") + ", " + countOf(project.TagCount, "tag", "tags")
}

// ---- view ----

func (s consoleProjectsScreen) Keys() screenKeys {
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete project")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "admin")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}}
}

func (s consoleProjectsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := consoleBodySize(width, height)

	spec := consoleTableSpec{
		columns: []consoleColumn{{title: "PROJECT"}, {title: "REPOS", width: 6}, {title: "TAGS", width: 6}, {title: "LAST PUSHED", width: 16}},
		cursor:  s.selected,
	}
	if len(s.projects) == 0 {
		spec.message, spec.messageStyle = "No projects yet: no repository has been published.", theme.muted
	}
	for _, p := range s.projects {
		spec.rows = append(spec.rows, []consoleCell{
			{p.Name, theme.text}, {strconv.Itoa(p.RepositoryCount), theme.text}, {strconv.Itoa(p.TagCount), theme.text},
			{formatRepositoryLastPushed(p.LastPushed), theme.text},
		})
	}
	status := consoleStatusLine(theme, s.confirm, s.notice, s.noticeFail, min(bodyW, consoleTableMaxWidth))
	body := consoleBody(theme, bodyW, bodyH, "Projects", countOf(len(s.projects), "project", "projects")+", sort: "+s.sortMode.label(), spec, status,
		"A project is the first path segment of a repository name. Enter shows its repositories. d deletes the whole project.")

	crumbs := []string{"Console", "Projects"}
	frame := screenFrame{Context: strings.Join(crumbs, " › "), Full: true, Body: renderConsoleFrame(theme, env, crumbs, body, s.Keys().short)}
	if s.confirm.Active() {
		frame.Overlay = s.confirm.view(theme)
	}
	return frame
}

func (s consoleProjectsScreen) dialogOpen() bool { return s.confirm.Active() }

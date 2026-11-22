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

// consoleRepositoriesScreen is "Console > Repositories" (tui-v2-redesign S3,
// the Repositories board): the catalog as a table, optionally narrowed to one
// project. It owns the catalog the rest of the TUI reads (admin suggestions).
type consoleRepositoriesScreen struct {
	gen uint64
	// items is the whole catalog; project narrows what shows when filtered.
	items    []appregixtry.RepositorySummary
	filtered bool
	project  string
	selected int

	confirm confirmPrompt
	// busy is set from the moment a confirmed delete is sent until its result
	// arrives, even if Esc closed the prompt meanwhile.
	shared     *consoleShared
	sync       consoleSync
	notice     string
	noticeFail bool
}

func newConsoleRepositoriesScreen(items []appregixtry.RepositorySummary) consoleRepositoriesScreen {
	return consoleRepositoriesScreen{gen: consoleGenerations.Add(1), items: slices.Clone(items), shared: newConsoleShared()}
}

func (s consoleRepositoriesScreen) ID() screen { return screenRepositories }

func (s consoleRepositoriesScreen) Init(screenEnv) tea.Cmd { return nil }

// visible is the rows the table shows: the catalog, or one project's share.
func (s consoleRepositoriesScreen) visible() []appregixtry.RepositorySummary {
	if !s.filtered {
		return s.items
	}
	rows := make([]appregixtry.RepositorySummary, 0, len(s.items))
	for _, item := range s.items {
		if repositoryProjectName(item.Name) == s.project {
			rows = append(rows, item)
		}
	}
	return rows
}

// names are the catalog's repository names.
func (s consoleRepositoriesScreen) names() []string {
	names := make([]string, 0, len(s.items))
	for _, item := range s.items {
		names = append(names, item.Name)
	}
	return names
}

func (s consoleRepositoriesScreen) selectedRepository() (appregixtry.RepositorySummary, bool) {
	rows := s.visible()
	if len(rows) == 0 {
		return appregixtry.RepositorySummary{}, false
	}
	return rows[boundedIndex(s.selected, len(rows))], true
}

// narrowedTo shows only one project's repositories.
func (s consoleRepositoriesScreen) narrowedTo(project string) consoleRepositoriesScreen {
	s.filtered, s.project, s.selected = true, project, 0
	return s
}

// ---- messages ----

type consoleCatalogMsg struct {
	gen    uint64
	stamp  consoleStamp
	result []appregixtry.RepositorySummary
	err    error
}

type repositoryDeletedMsg struct {
	gen        uint64
	repository string
	err        error
}

func consoleCatalogCmd(env screenEnv, gen uint64, stamp consoleStamp) tea.Cmd {
	service := env.Service
	return func() tea.Msg {
		if service == nil {
			return consoleCatalogMsg{gen: gen, stamp: stamp, err: errConsoleUnavailable}
		}
		result, err := service.RepositorySummaries(context.Background(), 100, "")
		return consoleCatalogMsg{gen: gen, stamp: stamp, result: result, err: err}
	}
}

func (s consoleRepositoriesScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case consoleCatalogMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyCatalog(typed), nil, false
	case repositoryDeletedMsg:
		if typed.gen != s.gen {
			if typed.err == nil {
				return s.withoutRepositories([]string{typed.repository}), nil, false
			}
			return s, nil, false
		}
		return s.applyDeleted(env, typed)
	case projectDeletedMsg:
		// The Projects screen deleted repositories: the facts apply here
		// whoever asked, so none of them shows again.
		return s.withoutRepositories(typed.removed()), nil, false
	case tagDeletedMsg:
		if typed.err == nil {
			return s.withOneTagLess(typed.repository), nil, false
		}
	}
	return s, nil, false
}

func (s consoleRepositoriesScreen) applyCatalog(msg consoleCatalogMsg) consoleRepositoriesScreen {
	sync, ok, hide := s.sync.accept(msg.stamp, s.shared)
	if !ok {
		return s
	}
	s.sync = sync
	if msg.err != nil {
		s.notice, s.noticeFail = "Could not refresh repositories: "+msg.err.Error(), true
		return s
	}
	keep := ""
	if current, ok := s.selectedRepository(); ok {
		keep = current.Name
	}
	s.items = slices.DeleteFunc(slices.Clone(msg.result), func(r appregixtry.RepositorySummary) bool { return hide && s.shared.isDeleted(repositoryKey(r.Name)) })
	return s.keepSelection(keep)
}

// keepSelection puts the cursor back on the repository of this name, or on the
// nearest row when it is gone.
func (s consoleRepositoriesScreen) keepSelection(name string) consoleRepositoriesScreen {
	rows := s.visible()
	s.selected = boundedIndex(s.selected, len(rows))
	if i := slices.IndexFunc(rows, func(r appregixtry.RepositorySummary) bool { return r.Name == name }); i >= 0 {
		s.selected = i
	}
	return s
}

func (s consoleRepositoriesScreen) withoutRepositories(names []string) consoleRepositoriesScreen {
	if len(names) == 0 {
		return s
	}
	for _, name := range names {
		s.shared.markDeleted(repositoryKey(name))
	}
	s.items = slices.DeleteFunc(slices.Clone(s.items), func(r appregixtry.RepositorySummary) bool { return slices.Contains(names, r.Name) })
	s.selected = boundedIndex(s.selected, len(s.visible()))
	return s
}

// withOneTagLess keeps TAGS right after a tag of the repository was deleted.
func (s consoleRepositoriesScreen) withOneTagLess(repository string) consoleRepositoriesScreen {
	items := slices.Clone(s.items)
	for i := range items {
		if items[i].Name == repository && items[i].TagCount > 0 {
			items[i].TagCount--
		}
	}
	s.items = items
	return s
}

func (s consoleRepositoriesScreen) applyDeleted(env screenEnv, msg repositoryDeletedMsg) (adminScreen, tea.Cmd, bool) {
	s.confirm = confirmPrompt{}
	s.shared.setBusy(false)
	if msg.err != nil {
		s.notice, s.noticeFail = msg.err.Error(), true
		return s, nil, false
	}
	s = s.withoutRepositories([]string{msg.repository})
	s.notice, s.noticeFail = fmt.Sprintf("Repository %q deleted. Refreshing repositories...", msg.repository), false
	sync, stamp := s.sync.launch(s.shared)
	s.sync = sync
	return s, consoleCatalogCmd(env, s.gen, stamp), false
}

func (s consoleRepositoriesScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, _ := s.confirm.update(env, msg)
		s.confirm = next
		if cmd != nil {
			s.shared.setBusy(true)
		}
		return s, cmd, true
	}
	current, has := s.selectedRepository()
	if cursor, moved := consoleMove(msg, s.selected, len(s.visible())); moved {
		s.selected = cursor
		return s, nil, true
	}
	switch {
	case isBackKey(msg):
		if s.filtered {
			s.filtered, s.project, s.selected, s.notice = false, "", 0, ""
		}
	case isTabKey(msg):
		return s.needsAdminClient(env)
	case isCommandRune(msg, 'p'):
		return s, consoleAsk(openProjectsMsg{repositories: slices.Clone(s.items)}), true
	case isEnterKey(msg):
		if has {
			return s, consoleAsk(openTagsMsg{repository: current.Name}), true
		}
	case isCommandRune(msg, 'g'):
		if !has {
			s.notice, s.noticeFail = "Select a repository to manage grants.", true
			return s, nil, true
		}
		return s.needsAdminClient(env)
	case isCommandRune(msg, 'd'):
		switch {
		case s.shared.isBusy():
			// A delete is already on its way.
		case !has:
			s.notice, s.noticeFail = "No repository selected to delete.", true
		default:
			s.confirm = s.deleteConfirm(current)
			s.notice = ""
		}
	}
	return s, nil, true
}

// needsAdminClient is Tab and g, which the parent answers (it opens the admin
// panel or the delegate screen) whenever an admin API exists: all the screen
// does is say so when none does.
func (s consoleRepositoriesScreen) needsAdminClient(env screenEnv) (adminScreen, tea.Cmd, bool) {
	if env.Client == nil {
		s.notice, s.noticeFail = consoleUnavailableNotice, true
		return s, nil, true
	}
	return s, nil, false
}

// deleteConfirm builds the confirmation of deleting one repository. What the
// service does (Service.DeleteRepository): one transaction removes every
// manifest, tag and manifest-blob link of the repository and the repository
// itself; blob files on disk are never touched, the garbage collector frees
// them. The name is captured here: the command deletes exactly what the
// dialog names whatever the selection does meanwhile.
func (s consoleRepositoriesScreen) deleteConfirm(repository appregixtry.RepositorySummary) confirmPrompt {
	gen, name := s.gen, repository.Name
	return newConfirmPrompt(
		"Delete repository "+name,
		fmt.Sprintf("The repository is removed with %s and every manifest, in one transaction.", countOf(repository.TagCount, "tag", "tags")),
		"delete",
		fmt.Sprintf("Deleting repository %s...", name),
		func(env screenEnv) tea.Cmd { return repositoryDeleteCmd(env, gen, name) },
	).withTarget(name, repositoryDetail(repository)).withButtonLabel("Delete").
		withConsequence("Pulls by tag and by digest stop working at once. Blob files stay until garbage collection. This cannot be undone.")
}

func repositoryDeleteCmd(env screenEnv, gen uint64, repository string) tea.Cmd {
	service := env.Service
	return func() tea.Msg {
		if service == nil {
			return repositoryDeletedMsg{gen: gen, repository: repository, err: errConsoleUnavailable}
		}
		_, err := service.DeleteRepository(context.Background(), repository)
		return repositoryDeletedMsg{gen: gen, repository: repository, err: err}
	}
}

// repositoryDetail is the line under a repository's name in a dialog.
func repositoryDetail(repository appregixtry.RepositorySummary) string {
	return countOf(repository.TagCount, "tag", "tags") + ", last pushed " + formatRepositoryLastPushed(repository.LastPushed)
}

// formatRepositoryLastPushed renders a repository's most recent push time,
// never blank: a repository without tags has no push time to show.
func formatRepositoryLastPushed(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// ---- view ----

func (s consoleRepositoriesScreen) Keys() screenKeys {
	hints := []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open tags")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete repository")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "projects")),
		key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grants")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "admin")),
	}
	if s.filtered {
		hints = append(hints, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "show all")))
	}
	return screenKeys{short: append(hints, key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")))}
}

func (s consoleRepositoriesScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := consoleBodySize(width, height)
	rows := s.visible()

	crumbs := []string{"Console", "Repositories"}
	summary := countOf(len(rows), "repository", "repositories")
	if s.filtered {
		crumbs = []string{"Console", "Projects", s.project, "Repositories"}
		summary = "project " + s.project + ", " + summary
	}
	spec := consoleTableSpec{
		columns: []consoleColumn{{title: "NAME"}, {title: "TAGS", width: 6}, {title: "LAST PUSHED", width: 16}},
		cursor:  s.selected,
	}
	if len(rows) == 0 {
		spec.message, spec.messageStyle = "No repositories have been published yet.", theme.muted
		if s.filtered {
			spec.message = "This project has no repositories."
		}
	}
	for _, r := range rows {
		spec.rows = append(spec.rows, []consoleCell{
			{r.Name, theme.text}, {strconv.Itoa(r.TagCount), theme.text}, {formatRepositoryLastPushed(r.LastPushed), theme.text},
		})
	}
	status := consoleStatusLine(theme, s.confirm, s.notice, s.noticeFail, bodyW)
	body := consoleBody(theme, bodyW, bodyH, "Repositories", summary, spec, status,
		"Enter shows the tags of the selected repository. d deletes it with all its tags after confirmation.")

	frame := screenFrame{Context: strings.Join(crumbs, " › "), Full: true, Body: renderConsoleFrame(theme, env, crumbs, body, s.Keys().short)}
	if s.confirm.Active() {
		frame.Overlay = s.confirm.view(theme)
	}
	return frame
}

func (s consoleRepositoriesScreen) dialogOpen() bool { return s.confirm.Active() }

package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// adminProjectsScreen is "Admin > Access > Projects" (the Projects boards 25
// to 27 and 30): the projects of the registry with their repository and tag
// counts. A project is the first path segment of a repository name; this
// screen creates and deletes projects and opens one project's members.
// Creating and deleting are global-administrator operations: any other
// session gets the API's 403, shown as "Only global administrators manage
// projects."
type adminProjectsScreen struct {
	gen uint64

	loaded    bool
	loadErr   string
	forbidden bool
	projects  []ports.ProjectSummary
	sortBy    projectSort
	// selectedName is the highlighted project; it is kept by name so a reload
	// or a new sort never moves the selection to another project.
	selectedName string
	// floor is the load sequence below which a read is stale: a create or a
	// delete sets it, so a read that started before the change cannot bring a
	// deleted project back or hide a new one.
	floor uint64

	dialog projectCreateDialog
	// saving is set from the moment a create command is dispatched until its
	// result arrives, even if Esc closed the dialog meanwhile.
	saving   bool
	confirm  confirmPrompt
	deleting bool
	refusal  noticeDialog

	status     string
	statusTone grantTone
}

// projectCreateDialog is the New project dialog.
type projectCreateDialog struct {
	open bool
	name string
	// err is the server's answer to the last create ("already exists", or the
	// failure), shown in the dialog while it stays open.
	err string
}

type projectSort int

const (
	projectSortName projectSort = iota
	projectSortLastPush
	projectSortRepositories
	projectSortCount
)

func (p projectSort) label() string {
	switch p {
	case projectSortLastPush:
		return "last push"
	case projectSortRepositories:
		return "repositories"
	}
	return "name"
}

// projectLoadSeq numbers every read of the project list, across instances.
var projectLoadSeq atomic.Uint64

// projectScreenMsg marks the async results of the project screens: the Model
// delivers them like the other Access results (generation first, expiry next).
type projectScreenMsg interface {
	accessMsg
	projectScreenMsg()
}

func newAdminProjectsScreen() adminProjectsScreen {
	return adminProjectsScreen{gen: accessGenerations.Add(1)}
}

func (s adminProjectsScreen) ID() screen { return screenAdminProjects }

func (s adminProjectsScreen) generation() uint64 { return s.gen }

func (s adminProjectsScreen) Init(env screenEnv) tea.Cmd { return projectsLoadCmd(env, s.gen) }

// capturesInput reports a dialog in front: typed letters belong to it.
func (s adminProjectsScreen) capturesInput() bool {
	return s.dialog.open || s.confirm.Active() || s.refusal.active()
}

func (s adminProjectsScreen) Keys() screenKeys {
	switch {
	case s.dialog.open:
		if s.saving {
			return screenKeys{short: []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close"))}}
		}
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	case s.confirm.Active():
		return screenKeys{short: []key.Binding{
			key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / enter", "confirm")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	case s.refusal.active():
		return screenKeys{short: []key.Binding{key.NewBinding(key.WithKeys("esc", "enter"), key.WithHelp("esc / enter", "close"))}}
	case s.forbidden:
		return screenKeys{short: []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")), quitKey()}}
	}
	var keys []key.Binding
	hasRows := len(s.projects) > 0
	if hasRows {
		keys = append(keys,
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")))
	}
	if s.loadErr != "" {
		keys = append(keys, key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")))
	}
	keys = append(keys, key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new project")))
	if hasRows {
		keys = append(keys,
			key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete project")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")))
	}
	return screenKeys{short: append(keys, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")), quitKey())}
}

// ---- messages ----

type adminProjectsLoadedMsg struct {
	gen      uint64
	seq      uint64
	projects []ports.ProjectSummary
	err      error
}

type adminProjectCreatedMsg struct {
	gen  uint64
	name string
	err  error
}

type adminProjectDeletedMsg struct {
	gen  uint64
	name string
	err  error
}

func (adminProjectsLoadedMsg) projectScreenMsg()               {}
func (adminProjectCreatedMsg) projectScreenMsg()               {}
func (adminProjectDeletedMsg) projectScreenMsg()               {}
func (adminProjectsLoadedMsg) accessSlot() screenSlot          { return slotProjects }
func (adminProjectCreatedMsg) accessSlot() screenSlot          { return slotProjects }
func (adminProjectDeletedMsg) accessSlot() screenSlot          { return slotProjects }
func (m adminProjectsLoadedMsg) accessGen() uint64             { return m.gen }
func (m adminProjectCreatedMsg) accessGen() uint64             { return m.gen }
func (m adminProjectDeletedMsg) accessGen() uint64             { return m.gen }
func (m adminProjectsLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }
func (m adminProjectCreatedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }
func (m adminProjectDeletedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// openProjectMembersMsg asks the parent to mount the members of one project.
type openProjectMembersMsg struct {
	project string
	// toast is a one-line outcome to show on arrival (a role just granted).
	toast string
}

func projectsLoadCmd(env screenEnv, gen uint64) tea.Cmd {
	client, session := env.Client, env.Session
	seq := projectLoadSeq.Add(1)
	return func() tea.Msg {
		if client == nil {
			return adminProjectsLoadedMsg{gen: gen, seq: seq, err: errAdminUnavailable}
		}
		projects, err := client.ListProjects(context.Background(), session)
		return adminProjectsLoadedMsg{gen: gen, seq: seq, projects: projects, err: err}
	}
}

func projectCreateCmd(env screenEnv, gen uint64, name string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return adminProjectCreatedMsg{gen: gen, name: name, err: errAdminUnavailable}
		}
		_, err := client.CreateProject(context.Background(), session, name)
		return adminProjectCreatedMsg{gen: gen, name: name, err: err}
	}
}

func adminProjectDeleteCmd(env screenEnv, gen uint64, name string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return adminProjectDeletedMsg{gen: gen, name: name, err: errAdminUnavailable}
		}
		return adminProjectDeletedMsg{gen: gen, name: name, err: client.DeleteProject(context.Background(), session, name)}
	}
}

// ---- Update ----

func (s adminProjectsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case adminProjectsLoadedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyLoaded(typed), nil, false
	case adminProjectCreatedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyCreated(env, typed)
		return next, cmd, false
	case adminProjectDeletedMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		next, cmd := s.applyDeleted(env, typed)
		return next, cmd, false
	}
	return s, nil, false
}

func (s adminProjectsScreen) applyLoaded(msg adminProjectsLoadedMsg) adminProjectsScreen {
	if msg.seq < s.floor {
		return s
	}
	if msg.err != nil {
		s.loaded = true
		if IsAdminForbidden(msg.err) {
			s.forbidden, s.projects, s.loadErr = true, nil, ""
			return s
		}
		s.loadErr = strings.TrimRight(adminErrorDetail(msg.err), ".")
		return s
	}
	s.loaded, s.loadErr, s.forbidden = true, "", false
	s.projects = msg.projects
	s.selectedName = s.keptSelection()
	return s
}

// keptSelection is the selected project if it is still listed, else the first
// row of the current sort.
func (s adminProjectsScreen) keptSelection() string {
	rows := s.sorted()
	if len(rows) == 0 {
		return ""
	}
	for _, row := range rows {
		if row.Name == s.selectedName {
			return s.selectedName
		}
	}
	return rows[0].Name
}

func (s adminProjectsScreen) applyCreated(env screenEnv, msg adminProjectCreatedMsg) (adminProjectsScreen, tea.Cmd) {
	s.saving = false
	if msg.err != nil {
		var text string
		switch {
		case IsAdminConflict(msg.err):
			text = fmt.Sprintf("Project %s already exists.", msg.name)
		default:
			text = fmt.Sprintf("Could not create project: %s.", strings.TrimRight(adminErrorDetail(msg.err), "."))
		}
		if s.dialog.open && s.dialog.name == msg.name {
			s.dialog.err = text
			return s, nil
		}
		s.status, s.statusTone = text, grantToneError
		return s, nil
	}
	if s.dialog.open && s.dialog.name == msg.name {
		s.dialog = projectCreateDialog{}
	}
	s.selectedName = msg.name
	s.floor = projectLoadSeq.Add(1)
	s.status, s.statusTone = fmt.Sprintf("Created project %s.", msg.name), grantToneSuccess
	return s, projectsLoadCmd(env, s.gen)
}

func (s adminProjectsScreen) applyDeleted(env screenEnv, msg adminProjectDeletedMsg) (adminProjectsScreen, tea.Cmd) {
	s.deleting = false
	s.confirm = confirmPrompt{}
	reload := func(s adminProjectsScreen) (adminProjectsScreen, tea.Cmd) {
		s.floor = projectLoadSeq.Add(1)
		return s, projectsLoadCmd(env, s.gen)
	}
	switch {
	case msg.err == nil:
		s.projects = slices.DeleteFunc(slices.Clone(s.projects), func(p ports.ProjectSummary) bool { return p.Name == msg.name })
		s.selectedName = s.keptSelection()
		s.status, s.statusTone = fmt.Sprintf("Deleted project %s.", msg.name), grantToneSuccess
		return reload(s)
	case IsAdminNotFound(msg.err):
		s.status, s.statusTone = fmt.Sprintf("Project %s no longer exists. The list was refreshed.", msg.name), grantToneWarning
		return reload(s)
	case IsAdminConflict(msg.err):
		s.status, s.statusTone = fmt.Sprintf("Project %s is no longer empty. Nothing was deleted.", msg.name), grantToneWarning
		return reload(s)
	case IsAdminUnprocessable(msg.err) && strings.Contains(adminErrorDetail(msg.err), "deletion is not enabled"):
		s.refusal = deletionDisabledNotice()
		return s, nil
	case IsAdminUnavailable(msg.err):
		// The project row is gone but its grants were not purged: say so and
		// read the list again; the same delete converges.
		s.status, s.statusTone = fmt.Sprintf("Could not delete project: %s.", strings.TrimRight(adminErrorDetail(msg.err), ".")), grantToneError
		return reload(s)
	}
	s.status, s.statusTone = fmt.Sprintf("Could not delete project: %s.", strings.TrimRight(adminErrorDetail(msg.err), ".")), grantToneError
	return s, nil
}

func (s adminProjectsScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case s.refusal.active():
		if isEscKey(msg) || isEnterKey(msg) {
			s.refusal = noticeDialog{}
		}
		return s, nil, true
	case s.confirm.Active():
		confirm, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = confirm
		s.deleting = s.deleting || confirm.inFlight
		return s, cmd, consumed
	case s.dialog.open:
		return s.updateDialogKey(env, msg)
	}
	rows := s.sorted()
	switch {
	case isEscKey(msg):
		return s, navigate(screenAdminOverview), true
	case s.forbidden:
		return s, nil, true
	case isMoveUpKey(msg):
		return s.moveBy(rows, -1), nil, true
	case isMoveDownKey(msg):
		return s.moveBy(rows, 1), nil, true
	case isEnterKey(msg):
		if project, ok := s.selected(rows); ok {
			name := project.Name
			return s, func() tea.Msg { return openProjectMembersMsg{project: name} }, true
		}
	case isCommandRune(msg, 'r'):
		if s.loadErr != "" {
			s.loadErr = ""
			return s, projectsLoadCmd(env, s.gen), true
		}
	case isCommandRune(msg, 'n'):
		if s.saving {
			s.status, s.statusTone = "Creating a project, please wait.", grantToneInfo
			return s, nil, true
		}
		s.dialog, s.status = projectCreateDialog{open: true}, ""
	case isCommandRune(msg, 's'):
		if len(rows) > 0 {
			s.sortBy = (s.sortBy + 1) % projectSortCount
			s.selectedName = s.keptSelection()
		}
	case isCommandRune(msg, 'd'):
		return s.askDelete(rows), nil, true
	}
	return s, nil, true
}

func (s adminProjectsScreen) updateDialogKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.dialog = projectCreateDialog{}
	case s.saving:
	case isEnterKey(msg):
		if checkProjectName(s.dialog.name) != projectNameValid {
			return s, nil, true
		}
		s.saving, s.dialog.err = true, ""
		return s, projectCreateCmd(env, s.gen, s.dialog.name), true
	default:
		if typed := typeInto(s.dialog.name, msg, false); typed != s.dialog.name {
			s.dialog.name, s.dialog.err = typed, ""
		}
	}
	return s, nil, true
}

func (s adminProjectsScreen) moveBy(rows []ports.ProjectSummary, delta int) adminProjectsScreen {
	if len(rows) == 0 {
		return s
	}
	s.selectedName = rows[boundedIndex(s.index(rows)+delta, len(rows))].Name
	return s
}

func (s adminProjectsScreen) index(rows []ports.ProjectSummary) int {
	for i, row := range rows {
		if row.Name == s.selectedName {
			return i
		}
	}
	return 0
}

func (s adminProjectsScreen) selected(rows []ports.ProjectSummary) (ports.ProjectSummary, bool) {
	if len(rows) == 0 {
		return ports.ProjectSummary{}, false
	}
	return rows[s.index(rows)], true
}

// askDelete opens the dialog for the selected project: a confirm when it is
// empty, a refusal when it still holds repositories. The name is captured now,
// so a reload or a new sort while the dialog is open cannot change what Enter
// deletes.
func (s adminProjectsScreen) askDelete(rows []ports.ProjectSummary) adminProjectsScreen {
	project, ok := s.selected(rows)
	if !ok {
		return s
	}
	if s.deleting {
		s.status, s.statusTone = "Deleting, please wait.", grantToneInfo
		return s
	}
	s.status, s.statusTone = "", grantToneNone
	if project.Repositories > 0 {
		s.refusal = nonEmptyProjectNotice(project)
		return s
	}
	s.confirm = deleteProjectConfirm(s.gen, project)
	return s
}

// ---- derived data ----

// sorted is the projects in the chosen order: by name ascending, last push
// newest first, or repositories most first; ties by name.
func (s adminProjectsScreen) sorted() []ports.ProjectSummary {
	rows := slices.Clone(s.projects)
	slices.SortStableFunc(rows, func(a, b ports.ProjectSummary) int {
		switch s.sortBy {
		case projectSortLastPush:
			if c := b.LastPushed.Compare(a.LastPushed); c != 0 {
				return c
			}
		case projectSortRepositories:
			if a.Repositories != b.Repositories {
				return b.Repositories - a.Repositories
			}
		}
		return strings.Compare(a.Name, b.Name)
	})
	return rows
}

// deleteProjectConfirm is the dialog before an EMPTY project is deleted. Its
// consequence is the API's: the project row goes and its member grants are
// purged, and only an empty project (no repository, no open upload) can go.
func deleteProjectConfirm(gen uint64, project ports.ProjectSummary) confirmPrompt {
	name := project.Name
	return newConfirmPrompt(
		fmt.Sprintf("Delete project %s?", name),
		"The project is removed together with its member grants. The project must be empty.",
		"delete",
		fmt.Sprintf("Deleting project %s…", name),
		func(env screenEnv) tea.Cmd { return adminProjectDeleteCmd(env, gen, name) },
	).withTarget(name, fmt.Sprintf("%s, %s", plural(project.Repositories, "repository"), plural(project.Tags, "tag"))).
		withButtonLabel("Delete").
		withConsequence("Removes the project and all its member grants. The project must be empty. Cannot be undone.")
}

// nonEmptyProjectNotice is the refusal for a project that still holds
// repositories. Its text is the API's own message for the same case.
func nonEmptyProjectNotice(project ports.ProjectSummary) noticeDialog {
	return noticeDialog{
		title:       "Cannot delete project",
		text:        fmt.Sprintf("project %q still has %s; delete or finish them first", project.Name, plural(project.Repositories, "repository")),
		target:      project.Name,
		detail:      fmt.Sprintf("%s, %s", plural(project.Repositories, "repository"), plural(project.Tags, "tag")),
		consequence: fmt.Sprintf("Delete repositories one by one in Console › Projects › %s. Nothing was changed.", project.Name),
	}
}

func deletionDisabledNotice() noticeDialog {
	return noticeDialog{
		title:       "Deletion is disabled",
		text:        "This server does not allow deleting projects.",
		consequence: "Start the server with -delete-enabled to allow it. Nothing was changed.",
	}
}

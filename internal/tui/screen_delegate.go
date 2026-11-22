package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// delegateScreen is "Repository access" for a repository administrator (the
// "Delegate repository access" board): the grants of ONE repository, fixed by
// whoever opened the screen from the console Repositories screen, with a new
// grant form, a role edit and a revoke. It never offers the repo-admin role
// and never shows another repository's grants: every async message carries
// the screen generation and the repository it was issued for.
type delegateScreen struct {
	// gen identifies this instance: a message with another gen is dropped, so
	// a late result can never land on a newer screen that reuses the slot.
	gen uint64
	// repo is the repository whose grants are managed. It is set once, on
	// open, and no key can change it.
	repo string
	// seq numbers the reads of the list: only the answer to the latest read
	// is applied.
	seq uint64

	rows    []ports.AdminRepositoryGrant
	loading bool
	// loaded is true once a read succeeded: only then may a grant be added,
	// since a failed read usually means the operator is not a repository
	// administrator here.
	loaded  bool
	loadErr string
	cursor  int

	form    delegateForm
	confirm confirmPrompt
	// saving and removing are set from the moment the command is issued until
	// its result arrives, so a second Enter never issues a second command.
	saving   bool
	removing bool

	status     string
	statusTone grantTone
}

// delegateField is the focused field of the grant form.
type delegateField int

const (
	delegateFieldUsername delegateField = iota
	delegateFieldRole
)

// delegateUsernameLimit bounds what a paste can put into the username field.
const delegateUsernameLimit = 128

// delegateForm is the new grant / edit role form. The repository is not a
// field: it is the screen's.
type delegateForm struct {
	active bool
	// editing is true for "edit role": the username is then fixed.
	editing  bool
	username string
	role     domainauth.RepoRole
	focus    delegateField
	err      string
}

// delegateGenerations hands out the unique id of each delegateScreen.
var delegateGenerations atomic.Uint64

// newDelegateScreen starts loading the grants of repository: Init issues the
// read.
func newDelegateScreen(repository string) delegateScreen {
	return delegateScreen{gen: delegateGenerations.Add(1), repo: strings.TrimSpace(repository), loading: true, seq: 1}
}

func (s delegateScreen) ID() screen { return screenRepoAdminGrants }

func (s delegateScreen) Keys() screenKeys {
	switch {
	case s.form.active && s.form.editing:
		return delegateEditKeys
	case s.form.active:
		return delegateFormKeys
	case s.loadErr != "":
		return delegateRetryKeys
	}
	return delegateKeys
}

var delegateKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new grant")),
	key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit role")),
	key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}

// delegateRetryKeys adds "r retry" while the read of the grants has failed.
var delegateRetryKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
	key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new grant")),
	key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit role")),
	key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}}

var delegateFormKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "change role")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}}

var delegateEditKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "change role")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}}

// capturesInput reports whether a text field or the confirm prompt owns the
// keyboard, so the global q and l keys must not act.
func (s delegateScreen) capturesInput() bool { return s.form.active || s.confirm.Active() }

// ---- messages ----

// delegateLoadedMsg is the result of the read numbered seq of repo's grants.
type delegateLoadedMsg struct {
	gen    uint64
	seq    uint64
	repo   string
	grants []ports.AdminRepositoryGrant
	err    error
}

// delegateSavedMsg is the result of the PUT of one grant.
type delegateSavedMsg struct {
	gen      uint64
	repo     string
	username string
	role     domainauth.RepoRole
	err      error
}

// delegateRemovedMsg is the result of the DELETE of one grant.
type delegateRemovedMsg struct {
	gen      uint64
	repo     string
	username string
	err      error
}

func (m delegateLoadedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m delegateSavedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

func (m delegateRemovedMsg) expiredReason() (string, bool) { return expiredReasonOf(m.err) }

// delegateMsgGen returns the screen generation a delegate async message
// belongs to.
func delegateMsgGen(msg tea.Msg) (uint64, bool) {
	switch typed := msg.(type) {
	case delegateLoadedMsg:
		return typed.gen, true
	case delegateSavedMsg:
		return typed.gen, true
	case delegateRemovedMsg:
		return typed.gen, true
	}
	return 0, false
}

// ---- Init and Update ----

func (s delegateScreen) Init(env screenEnv) tea.Cmd {
	return delegateLoadCmd(env, s.gen, s.seq, s.repo)
}

func (s delegateScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	if gen, ok := delegateMsgGen(msg); ok && gen != s.gen {
		return s, nil, false
	}
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case delegateLoadedMsg:
		return s.loadedResult(typed), nil, false
	case delegateSavedMsg:
		next, cmd := s.savedResult(env, typed)
		return next, cmd, false
	case delegateRemovedMsg:
		next, cmd := s.removedResult(env, typed)
		return next, cmd, false
	}
	return s, nil, false
}

// reload issues a fresh read of the grants.
func (s delegateScreen) reload(env screenEnv) (delegateScreen, tea.Cmd) {
	s.seq++
	s.loading, s.loadErr = true, ""
	return s, delegateLoadCmd(env, s.gen, s.seq, s.repo)
}

func (s delegateScreen) loadedResult(msg delegateLoadedMsg) delegateScreen {
	// A result of another repository, or of an older read, is not for this
	// screen as it is now.
	if msg.repo != s.repo || msg.seq != s.seq {
		return s
	}
	s.loading = false
	if msg.err != nil {
		s.loadErr = msg.err.Error()
		return s
	}
	s.loadErr, s.loaded = "", true
	s.rows = append([]ports.AdminRepositoryGrant(nil), msg.grants...)
	sort.SliceStable(s.rows, func(i, j int) bool {
		return strings.ToLower(s.rows[i].Username) < strings.ToLower(s.rows[j].Username)
	})
	s.cursor = boundedIndex(s.cursor, len(s.rows))
	return s
}

func (s delegateScreen) savedResult(env screenEnv, msg delegateSavedMsg) (delegateScreen, tea.Cmd) {
	if msg.repo != s.repo {
		return s, nil
	}
	s.saving = false
	if msg.err != nil {
		text := fmt.Sprintf("Could not save %s: %s", msg.username, msg.err.Error())
		s.status, s.statusTone = text, grantToneError
		if s.form.active {
			s.form.err = text
		}
		return s, nil
	}
	s.form = delegateForm{}
	s.status, s.statusTone = fmt.Sprintf("Saved %s as %s.", msg.username, grantRoleLabel(msg.role)), grantToneSuccess
	return s.reload(env)
}

func (s delegateScreen) removedResult(env screenEnv, msg delegateRemovedMsg) (delegateScreen, tea.Cmd) {
	if msg.repo != s.repo {
		return s, nil
	}
	s.removing = false
	s.confirm = confirmPrompt{}
	if msg.err != nil {
		s.status, s.statusTone = fmt.Sprintf("Could not revoke %s: %s", msg.username, msg.err.Error()), grantToneError
		return s, nil
	}
	s.status, s.statusTone = fmt.Sprintf("Revoked %s on %s.", msg.username, msg.repo), grantToneSuccess
	return s.reload(env)
}

func delegateLoadCmd(env screenEnv, gen, seq uint64, repo string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return delegateLoadedMsg{gen: gen, seq: seq, repo: repo, err: errors.New("admin API is unavailable for this session")}
		}
		grants, err := client.ListRepositoryGrants(context.Background(), session, repo)
		return delegateLoadedMsg{gen: gen, seq: seq, repo: repo, grants: grants, err: err}
	}
}

func delegateSaveCmd(env screenEnv, gen uint64, input ports.AdminPutRepositoryGrantInput) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return delegateSavedMsg{gen: gen, repo: input.Repository, username: input.Username, role: input.Role, err: errors.New("admin API is unavailable for this session")}
		}
		_, err := client.PutRepositoryGrant(context.Background(), session, input)
		return delegateSavedMsg{gen: gen, repo: input.Repository, username: input.Username, role: input.Role, err: err}
	}
}

func delegateRemoveCmd(env screenEnv, gen uint64, repo, username string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return delegateRemovedMsg{gen: gen, repo: repo, username: username, err: errors.New("admin API is unavailable for this session")}
		}
		err := client.DeleteRepositoryGrant(context.Background(), session, repo, username)
		return delegateRemovedMsg{gen: gen, repo: repo, username: username, err: err}
	}
}

// ---- keys ----

func (s delegateScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		// The prompt ignores a second Enter while the delete is in flight.
		confirm, cmd, consumed := s.confirm.update(env, msg)
		s.confirm = confirm
		s.removing = s.removing || confirm.inFlight
		return s, cmd, consumed
	}
	if s.form.active {
		return s.updateFormKey(env, msg)
	}
	switch {
	case isEscKey(msg):
		return s, returnToInspectionCmd(), true
	case isMoveUpKey(msg):
		s.cursor = boundedIndex(s.cursor-1, len(s.rows))
	case isMoveDownKey(msg):
		s.cursor = boundedIndex(s.cursor+1, len(s.rows))
	case isPgUpKey(msg):
		s.cursor = boundedIndex(s.cursor-delegatePageRows(env.Layout.Height), len(s.rows))
	case isPgDnKey(msg):
		s.cursor = boundedIndex(s.cursor+delegatePageRows(env.Layout.Height), len(s.rows))
	case isRuneKey(msg, 'r'):
		if s.loadErr != "" && !s.loading {
			next, cmd := s.reload(env)
			return next, cmd, true
		}
	case isRuneKey(msg, 'n'):
		return s.openNewForm(), nil, true
	case isRuneKey(msg, 'e'):
		return s.openEditForm(), nil, true
	case isRuneKey(msg, 'x'):
		return s.askRemove(), nil, true
	}
	return s, nil, true
}

func (s delegateScreen) openNewForm() delegateScreen {
	if !s.loaded {
		s.status, s.statusTone = "Grants are not loaded: you may not be an administrator of this repository.", grantToneWarning
		if s.loading {
			s.status, s.statusTone = "Still reading the grants.", grantToneInfo
		}
		return s
	}
	s.form = delegateForm{active: true, role: domainauth.RepoRoleReader}
	s.status, s.statusTone = "", grantToneNone
	return s
}

func (s delegateScreen) openEditForm() delegateScreen {
	grant, ok := s.selected()
	if !ok {
		s.status, s.statusTone = "No grant selected to edit.", grantToneInfo
		return s
	}
	// The list is not filtered by role, so it can hold a peer's Admin grant:
	// editing it through this form would rewrite a row the operator was never
	// offered, so it is refused outright.
	if grant.Role == domainauth.RepoRoleAdmin {
		s.status, s.statusTone = fmt.Sprintf("%s is Admin on %s: ask a global admin to change that.", grant.Username, s.repo), grantToneWarning
		return s
	}
	s.form = delegateForm{active: true, editing: true, username: grant.Username, role: delegateClampRole(grant.Role), focus: delegateFieldRole}
	s.status, s.statusTone = "", grantToneNone
	return s
}

func (s delegateScreen) askRemove() delegateScreen {
	grant, ok := s.selected()
	if !ok {
		s.status, s.statusTone = "No grant selected to revoke.", grantToneInfo
		return s
	}
	if s.removing {
		s.status, s.statusTone = "Revoking, please wait.", grantToneInfo
		return s
	}
	gen, repo, username := s.gen, s.repo, grant.Username
	s.status, s.statusTone = "", grantToneNone
	s.confirm = newConfirmPrompt(
		"Revoke access",
		fmt.Sprintf("Revoke %s's %s access to %s?", username, grantRoleLabel(grant.Role), repo),
		"revoke",
		fmt.Sprintf("Revoking %s on %s...", username, repo),
		func(env screenEnv) tea.Cmd { return delegateRemoveCmd(env, gen, repo, username) },
	).withTarget(fmt.Sprintf("%s on %s", username, repo), grantRoleLabel(grant.Role)).
		withConsequence(fmt.Sprintf("%s loses %s access to %s.", username, grantRoleLabel(grant.Role), repo))
	return s
}

// updateFormKey handles the grant form. Every letter, j, k and q included, is
// typed into the username; only Tab and the arrows move between the fields.
func (s delegateScreen) updateFormKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	switch {
	case isEscKey(msg):
		s.form = delegateForm{}
		s.status, s.statusTone = "", grantToneNone
	case isTabKey(msg), isShiftTabKey(msg), isArrowUpKey(msg), isArrowDownKey(msg):
		if !s.form.editing {
			s.form.focus = 1 - s.form.focus
		}
	case isEnterKey(msg):
		return s.submit(env)
	case isBackspaceKey(msg):
		if s.form.focus == delegateFieldUsername && !s.form.editing {
			s.form.username = dropLastRune(s.form.username)
			s.form.err = ""
		}
	case isSpaceKey(msg):
		if s.form.focus == delegateFieldRole {
			s.form.role = nextDelegateGrantRole(s.form.role)
		}
	case isTextKey(msg):
		if s.form.focus == delegateFieldUsername && !s.form.editing {
			text := grantSanitizeFilterText(keyText(msg))
			if len([]rune(s.form.username+text)) <= delegateUsernameLimit {
				s.form.username += text
			}
			s.form.err = ""
		}
	}
	return s, nil, true
}

// submit issues the PUT. Enter while one is in flight does nothing.
func (s delegateScreen) submit(env screenEnv) (adminScreen, tea.Cmd, bool) {
	if s.saving {
		s.status, s.statusTone = "Saving, please wait.", grantToneInfo
		return s, nil, true
	}
	username := strings.TrimSpace(s.form.username)
	if username == "" {
		s.form.err = "Username is required."
		return s, nil, true
	}
	for _, row := range s.rows {
		if strings.EqualFold(row.Username, username) && row.Role == domainauth.RepoRoleAdmin {
			s.form.err = fmt.Sprintf("%s is Admin on %s: ask a global admin to change that.", row.Username, s.repo)
			return s, nil, true
		}
	}
	input := ports.AdminPutRepositoryGrantInput{Repository: s.repo, Username: username, Role: delegateClampRole(s.form.role)}
	s.saving, s.form.err = true, ""
	s.status, s.statusTone = fmt.Sprintf("Saving %s...", username), grantToneInfo
	return s, delegateSaveCmd(env, s.gen, input), true
}

// ---- derived data ----

func (s delegateScreen) selected() (ports.AdminRepositoryGrant, bool) {
	if len(s.rows) == 0 {
		return ports.AdminRepositoryGrant{}, false
	}
	return s.rows[boundedIndex(s.cursor, len(s.rows))], true
}

// ownRole is the operator's own grant on the repository, when the list holds
// one.
func (s delegateScreen) ownRole(username string) (domainauth.RepoRole, bool) {
	for _, row := range s.rows {
		if row.Username == username {
			return row.Role, true
		}
	}
	return "", false
}

// delegateClampRole is the structural guarantee of this screen: whatever
// role is asked for, the one sent is Reader or Writer, never Admin.
func delegateClampRole(role domainauth.RepoRole) domainauth.RepoRole {
	if role == domainauth.RepoRoleWriter {
		return domainauth.RepoRoleWriter
	}
	return domainauth.RepoRoleReader
}

// nextDelegateGrantRole cycles the form's role between exactly Reader and
// Writer: a repository administrator is never offered Admin, whether for a
// new grant or for self-assignment. Every branch, including the Admin
// starting value, resolves to Reader or Writer.
func nextDelegateGrantRole(current domainauth.RepoRole) domainauth.RepoRole {
	if current == domainauth.RepoRoleReader {
		return domainauth.RepoRoleWriter
	}
	return domainauth.RepoRoleReader
}

// delegatePageRows is how many rows PgUp and PgDn move at this height.
func delegatePageRows(height int) int {
	if height <= 0 {
		return 10
	}
	_, bodyH := shellContentSize(0, height)
	return max(bodyH-delegateTableChromeRows, 1)
}

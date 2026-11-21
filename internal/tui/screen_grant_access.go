package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// grantAccessScreen is "Grant repository access": one screen to give a user
// or robot a role on one or more repositories (tui-v2-redesign S4). It
// replaces the four-screen path Users > user > Grants > Add Grant for the
// common case; the old grant screens stay untouched.
//
// Three panes: Who (filter + list + "Already has"), Which repositories
// (filter + checklist) and Role and review. Nothing is called on the backend
// until Enter; applying is one tea.Cmd that issues one PutUserGrant per
// selected repository and returns one grantAccessAppliedMsg. There is no
// rollback: a partial failure keeps the grants that succeeded.
type grantAccessScreen struct {
	returnTo    screen
	preselectID string
	focus       grantPane

	principals       []grantPrincipal
	principalsLoaded bool
	usersErr         string
	robotsErr        string
	whoFilter        string
	whoCursor        int
	// grants caches "Already has" per principal id. It is a map, so every
	// write goes through cloneGrantCache: Model is copied by value on each
	// Update and a shared map would let a discarded copy see the write.
	grants map[string]grantsEntry

	repoFilter string
	repoCursor int
	selection  grantSelection

	role domainauth.RepoRole

	applying   bool
	results    []grantOutcome
	status     string
	statusTone grantTone
}

type grantPane int

const (
	grantPaneWho grantPane = iota
	grantPaneRepos
	grantPaneRole
	grantPaneCount
)

type grantTone int

const (
	grantToneNone grantTone = iota
	grantToneInfo
	grantToneError
	grantToneWarning
	grantToneSuccess
)

const (
	grantKindUser  = "user"
	grantKindRobot = "robot"
)

// grantPrincipal is a user or a robot. Robots are users in the backend, so ID
// is valid for ListUserGrants and PutUserGrant either way.
type grantPrincipal struct {
	ID   string
	Name string
	Kind string
}

type grantsEntry struct {
	loading bool
	loaded  bool
	err     string
	items   []ports.AdminRepoGrant
}

// grantOutcome is the result of one PutUserGrant: err == nil means granted.
type grantOutcome struct {
	repo string
	err  error
}

type grantAccessPrincipalsLoadedMsg struct {
	users     []ports.AdminUser
	usersErr  error
	robots    []ports.AdminRobot
	robotsErr error
}

type grantAccessGrantsLoadedMsg struct {
	userID string
	grants []ports.AdminRepoGrant
	err    error
}

type grantAccessAppliedMsg struct {
	principalID string
	role        domainauth.RepoRole
	outcomes    []grantOutcome
}

// expiredReason lets Model.Update turn an expired admin session found in any
// of these results into the normal re-login flow.
func (m grantAccessPrincipalsLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.usersErr, m.robotsErr)
}

func (m grantAccessGrantsLoadedMsg) expiredReason() (string, bool) {
	return expiredReasonOf(m.err)
}

func (m grantAccessAppliedMsg) expiredReason() (string, bool) {
	for _, outcome := range m.outcomes {
		if reason, ok := expiredReasonOf(outcome.err); ok {
			return reason, true
		}
	}
	return "", false
}

func expiredReasonOf(errs ...error) (string, bool) {
	for _, err := range errs {
		if err != nil && IsAdminSessionExpired(err) {
			return err.Error(), true
		}
	}
	return "", false
}

// openGrantAccessMsg asks the parent to mount a FRESH grantAccessScreen (state
// from an earlier visit is discarded) and optionally preselect a principal.
type openGrantAccessMsg struct {
	preselectID string
	returnTo    screen
}

func openGrantAccess(preselectID string, returnTo screen) tea.Cmd {
	return func() tea.Msg { return openGrantAccessMsg{preselectID: preselectID, returnTo: returnTo} }
}

func newGrantAccessScreen(returnTo screen, preselectID string) grantAccessScreen {
	s := grantAccessScreen{
		returnTo:    returnTo,
		preselectID: preselectID,
		// Same default the existing add-grant form starts with.
		role: newAdminViewState().GrantForm.Role,
	}
	if preselectID != "" {
		s.focus = grantPaneRepos
	}
	return s
}

// ---- pure helpers ----

func mergeGrantPrincipals(users []ports.AdminUser, robots []ports.AdminRobot) []grantPrincipal {
	seen := map[string]bool{}
	out := make([]grantPrincipal, 0, len(users)+len(robots))
	add := func(id, name, kind string) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, grantPrincipal{ID: id, Name: name, Kind: kind})
	}
	for _, user := range users {
		add(user.ID, user.Username, grantKindUser)
	}
	for _, robot := range robots {
		add(robot.ID, robot.Username, grantKindRobot)
	}
	return out
}

func filterGrantPrincipals(all []grantPrincipal, filter string) []grantPrincipal {
	needle := strings.ToLower(strings.TrimSpace(filter))
	var out []grantPrincipal
	for _, principal := range all {
		if needle == "" || strings.Contains(strings.ToLower(principal.Name), needle) {
			out = append(out, principal)
		}
	}
	return out
}

// grantRepoRow is one row of the repositories checklist. New marks the extra
// "not pushed yet" row built from the filter text.
type grantRepoRow struct {
	Name string
	New  bool
}

// grantRepoRows returns the known repositories whose name contains filter
// (case-insensitive), plus a trailing New row when the trimmed filter is a
// valid repository name that is not already known. Grants match the exact
// name, so a not-yet-pushed name is a valid target.
func grantRepoRows(known []string, filter string) []grantRepoRow {
	trimmed := strings.TrimSpace(filter)
	needle := strings.ToLower(trimmed)
	var rows []grantRepoRow
	for _, name := range known {
		if needle == "" || strings.Contains(strings.ToLower(name), needle) {
			rows = append(rows, grantRepoRow{Name: name})
		}
	}
	if trimmed != "" && !slices.Contains(known, trimmed) {
		if _, err := regixtrydomain.ParseRepositoryRef(trimmed); err == nil {
			rows = append(rows, grantRepoRow{Name: trimmed, New: true})
		}
	}
	return rows
}

// grantSelection is the set of ticked repositories. It survives filter
// changes. extra remembers, in pick order, ticked names that are not in the
// known list. Every method returns a new value and never mutates its receiver.
type grantSelection struct {
	names map[string]bool
	extra []string
}

func (g grantSelection) has(name string) bool { return g.names[name] }

func (g grantSelection) clone() grantSelection {
	next := grantSelection{names: make(map[string]bool, len(g.names)), extra: slices.Clone(g.extra)}
	for name, on := range g.names {
		next.names[name] = on
	}
	return next
}

func (g grantSelection) without(name string) grantSelection {
	next := g.clone()
	delete(next.names, name)
	next.extra = slices.DeleteFunc(next.extra, func(n string) bool { return n == name })
	return next
}

func (g grantSelection) with(row grantRepoRow) grantSelection {
	if g.names[row.Name] {
		return g.clone()
	}
	next := g.clone()
	next.names[row.Name] = true
	if row.New {
		next.extra = append(next.extra, row.Name)
	}
	return next
}

func (g grantSelection) toggled(row grantRepoRow) grantSelection {
	if g.has(row.Name) {
		return g.without(row.Name)
	}
	return g.with(row)
}

// toggledAll ticks every given row, or unticks them all when every one is
// already ticked. Selected rows that are not in rows are left alone.
func (g grantSelection) toggledAll(rows []grantRepoRow) grantSelection {
	allOn := len(rows) > 0
	for _, row := range rows {
		if !g.has(row.Name) {
			allOn = false
			break
		}
	}
	next := g.clone()
	for _, row := range rows {
		if allOn {
			next = next.without(row.Name)
		} else {
			next = next.with(row)
		}
	}
	return next
}

// ordered is the apply order: ticked known repositories in the known list's
// order, then ticked not-yet-pushed names in the order they were picked.
func (g grantSelection) ordered(known []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range known {
		if g.names[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, name := range g.extra {
		if g.names[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

type grantRoleOption struct {
	Role        domainauth.RepoRole
	Label       string
	Description string
}

var grantRoleOptions = []grantRoleOption{
	{Role: domainauth.RepoRoleReader, Label: "Reader", Description: "pull"},
	{Role: domainauth.RepoRoleWriter, Label: "Writer", Description: "pull, push, delete tags"},
	{Role: domainauth.RepoRoleAdmin, Label: "Admin", Description: "Writer plus manage who has access"},
}

func grantRoleLabel(role domainauth.RepoRole) string {
	for _, option := range grantRoleOptions {
		if option.Role == role {
			return option.Label
		}
	}
	return string(role)
}

func grantRoleIndex(role domainauth.RepoRole) int {
	for i, option := range grantRoleOptions {
		if option.Role == role {
			return i
		}
	}
	return 0
}

// grantRoleOn returns the role a principal already has on exactly this
// repository.
func grantRoleOn(grants []ports.AdminRepoGrant, repository string) (domainauth.RepoRole, bool) {
	for _, grant := range grants {
		if grant.Repository.Name == repository {
			return grant.Role, true
		}
	}
	return "", false
}

// grantReviewLines is the review box text: "<principal> becomes <Role> on N
// repositories:" (wrapped to width) then the names, using at most budget
// lines; names that do not fit collapse into "+k more".
func grantReviewLines(principal string, role domainauth.RepoRole, names []string, width, budget int) []string {
	if budget < 1 {
		return nil
	}
	if len(names) == 0 {
		return grantClipLines(grantWrap("Nothing selected yet. Pick a user or robot, tick repositories, then press Enter.", width), budget)
	}
	header := grantWrap(fmt.Sprintf("%s becomes %s on %d repositories:", principal, grantRoleLabel(role), len(names)), width)
	if len(header) >= budget {
		return header[:budget]
	}
	room := budget - len(header)
	lines := header
	if len(names) <= room {
		for _, name := range names {
			lines = append(lines, grantTruncate(name, width))
		}
		return lines
	}
	shown := room - 1
	for _, name := range names[:shown] {
		lines = append(lines, grantTruncate(name, width))
	}
	return append(lines, fmt.Sprintf("+%d more", len(names)-shown))
}

func grantClipLines(lines []string, budget int) []string {
	if len(lines) > budget {
		return lines[:budget]
	}
	return lines
}

// grantTruncate shortens plain text to width cells, ending with an ellipsis.
func grantTruncate(text string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// grantWrap word-wraps plain text; a word wider than width is truncated.
func grantWrap(text string, width int) []string {
	if width < 1 {
		return []string{""}
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		word = grantTruncate(word, width)
		switch {
		case current == "":
			current = word
		case lipgloss.Width(current)+1+lipgloss.Width(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// grantWindow returns the [start,end) slice of a list of total rows that
// shows rows entries and keeps cursor visible.
func grantWindow(cursor, total, rows int) (int, int) {
	if rows < 1 || total < 1 {
		return 0, 0
	}
	if total <= rows {
		return 0, total
	}
	start := cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start > total-rows {
		start = total - rows
	}
	return start, start + rows
}

func grantSanitizeFilterText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r > ' ' && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dropLastRune(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	return string(runes[:len(runes)-1])
}

func cloneGrantCache(cache map[string]grantsEntry) map[string]grantsEntry {
	next := make(map[string]grantsEntry, len(cache)+1)
	for id, entry := range cache {
		next[id] = entry
	}
	return next
}

// ---- adminScreen ----

func (s grantAccessScreen) ID() screen { return screenAdminGrantAccess }

func (s grantAccessScreen) Keys() screenKeys { return grantAccessKeys }

func (s grantAccessScreen) Init(env screenEnv) tea.Cmd {
	return grantAccessLoadPrincipalsCmd(env)
}

func (s grantAccessScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case grantAccessPrincipalsLoadedMsg:
		s.principalsLoaded = true
		s.usersErr, s.robotsErr = errText(typed.usersErr), errText(typed.robotsErr)
		s.principals = mergeGrantPrincipals(typed.users, typed.robots)
		if s.preselectID != "" {
			for i, principal := range filterGrantPrincipals(s.principals, s.whoFilter) {
				if principal.ID == s.preselectID {
					s.whoCursor = i
					break
				}
			}
			s.preselectID = ""
		}
		next, cmd := s.loadHighlightedGrants(env)
		return next, cmd, false
	case grantAccessGrantsLoadedMsg:
		s.grants = cloneGrantCache(s.grants)
		if typed.err != nil {
			s.grants[typed.userID] = grantsEntry{err: typed.err.Error()}
		} else {
			s.grants[typed.userID] = grantsEntry{loaded: true, items: typed.grants}
		}
		return s, nil, false
	case grantAccessAppliedMsg:
		next, cmd := s.applied(env, typed)
		return next, cmd, false
	}
	return s, nil, false
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s grantAccessScreen) highlighted() (grantPrincipal, bool) {
	filtered := filterGrantPrincipals(s.principals, s.whoFilter)
	if len(filtered) == 0 {
		return grantPrincipal{}, false
	}
	return filtered[boundedIndex(s.whoCursor, len(filtered))], true
}

// loadHighlightedGrants starts the "Already has" load for the highlighted
// principal unless it is cached or already loading (a failed load retries).
func (s grantAccessScreen) loadHighlightedGrants(env screenEnv) (grantAccessScreen, tea.Cmd) {
	principal, ok := s.highlighted()
	if !ok {
		return s, nil
	}
	if entry := s.grants[principal.ID]; entry.loading || entry.loaded {
		return s, nil
	}
	s.grants = cloneGrantCache(s.grants)
	s.grants[principal.ID] = grantsEntry{loading: true}
	return s, grantAccessLoadGrantsCmd(env, principal.ID)
}

// clearOutcome drops the last apply result and status once the operator
// changes what would be applied next.
func (s grantAccessScreen) clearOutcome() grantAccessScreen {
	s.results = nil
	s.status = ""
	s.statusTone = grantToneNone
	return s
}

func (s grantAccessScreen) setStatus(tone grantTone, text string) grantAccessScreen {
	s.status, s.statusTone = text, tone
	return s
}

func (s grantAccessScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if isEscKey(msg) {
		return s, navigate(s.returnTo), true
	}
	if s.applying {
		return s, nil, true
	}
	switch {
	case isTabKey(msg):
		s.focus = (s.focus + 1) % grantPaneCount
		return s, nil, true
	case isShiftTabKey(msg):
		s.focus = (s.focus + grantPaneCount - 1) % grantPaneCount
		return s, nil, true
	case isEnterKey(msg):
		next, cmd := s.apply(env)
		return next, cmd, true
	}
	var (
		next grantAccessScreen
		cmd  tea.Cmd
	)
	switch s.focus {
	case grantPaneWho:
		next, cmd = s.whoKey(env, msg)
	case grantPaneRepos:
		next, cmd = s.reposKey(env, msg)
	default:
		next, cmd = s.roleKey(msg), nil
	}
	return next, cmd, true
}

func (s grantAccessScreen) whoKey(env screenEnv, msg keyMsg) (grantAccessScreen, tea.Cmd) {
	switch {
	case isArrowUpKey(msg), isArrowDownKey(msg):
		filtered := filterGrantPrincipals(s.principals, s.whoFilter)
		before, _ := s.highlighted()
		delta := 1
		if isArrowUpKey(msg) {
			delta = -1
		}
		s.whoCursor = boundedIndex(boundedIndex(s.whoCursor, len(filtered))+delta, len(filtered))
		return s.afterHighlightChange(env, before)
	case isBackspaceKey(msg):
		return s.withWhoFilter(env, dropLastRune(s.whoFilter))
	case isTextKey(msg):
		text := grantSanitizeFilterText(keyText(msg))
		if text == "" {
			return s, nil
		}
		return s.withWhoFilter(env, s.whoFilter+text)
	}
	return s, nil
}

func (s grantAccessScreen) afterHighlightChange(env screenEnv, before grantPrincipal) (grantAccessScreen, tea.Cmd) {
	if after, ok := s.highlighted(); !ok || after.ID != before.ID {
		s = s.clearOutcome()
	}
	return s.loadHighlightedGrants(env)
}

// withWhoFilter sets the filter and keeps the highlighted principal when it
// still matches, otherwise highlights the first match.
func (s grantAccessScreen) withWhoFilter(env screenEnv, filter string) (grantAccessScreen, tea.Cmd) {
	before, hadBefore := s.highlighted()
	s.whoFilter = filter
	s.whoCursor = 0
	if hadBefore {
		for i, principal := range filterGrantPrincipals(s.principals, filter) {
			if principal.ID == before.ID {
				s.whoCursor = i
				break
			}
		}
	}
	return s.afterHighlightChange(env, before)
}

func (s grantAccessScreen) reposKey(env screenEnv, msg keyMsg) (grantAccessScreen, tea.Cmd) {
	rows := grantRepoRows(env.KnownRepositories, s.repoFilter)
	switch {
	case isArrowUpKey(msg):
		s.repoCursor = boundedIndex(boundedIndex(s.repoCursor, len(rows))-1, len(rows))
	case isArrowDownKey(msg):
		s.repoCursor = boundedIndex(boundedIndex(s.repoCursor, len(rows))+1, len(rows))
	case isCtrlKey(msg, 'a'):
		s.selection = s.selection.toggledAll(rows)
		s = s.clearOutcome()
	case isSpaceKey(msg) && !isPasteKey(msg):
		if len(rows) > 0 {
			s.selection = s.selection.toggled(rows[boundedIndex(s.repoCursor, len(rows))])
			s = s.clearOutcome()
		}
	case isBackspaceKey(msg):
		s = s.withRepoFilter(env, dropLastRune(s.repoFilter))
	case isTextKey(msg):
		if text := grantSanitizeFilterText(keyText(msg)); text != "" {
			s = s.withRepoFilter(env, s.repoFilter+text)
		}
	}
	return s, nil
}

func (s grantAccessScreen) withRepoFilter(env screenEnv, filter string) grantAccessScreen {
	var highlighted string
	if rows := grantRepoRows(env.KnownRepositories, s.repoFilter); len(rows) > 0 {
		highlighted = rows[boundedIndex(s.repoCursor, len(rows))].Name
	}
	s.repoFilter = filter
	s.repoCursor = 0
	for i, row := range grantRepoRows(env.KnownRepositories, filter) {
		if row.Name == highlighted {
			s.repoCursor = i
			break
		}
	}
	return s
}

func (s grantAccessScreen) roleKey(msg keyMsg) grantAccessScreen {
	index := grantRoleIndex(s.role)
	switch {
	case isArrowUpKey(msg):
		index = boundedIndex(index-1, len(grantRoleOptions))
	case isArrowDownKey(msg):
		index = boundedIndex(index+1, len(grantRoleOptions))
	case isRuneKey(msg, '1') && !isPasteKey(msg):
		index = 0
	case isRuneKey(msg, '2') && !isPasteKey(msg):
		index = 1
	case isRuneKey(msg, '3') && !isPasteKey(msg):
		index = 2
	default:
		return s
	}
	if next := grantRoleOptions[index].Role; next != s.role {
		s.role = next
		s = s.clearOutcome()
	}
	return s
}

func (s grantAccessScreen) apply(env screenEnv) (grantAccessScreen, tea.Cmd) {
	principal, ok := s.highlighted()
	if !ok {
		return s.setStatus(grantToneError, "Select a user or robot first."), nil
	}
	names := s.selection.ordered(env.KnownRepositories)
	if len(names) == 0 {
		return s.setStatus(grantToneError, "Select at least one repository (Space ticks the highlighted one)."), nil
	}
	s.applying = true
	s.results = nil
	s = s.setStatus(grantToneInfo, fmt.Sprintf("Granting %s on %d repositories to %s...", grantRoleLabel(s.role), len(names), principal.Name))
	return s, grantAccessApplyCmd(env, principal.ID, s.role, names)
}

func (s grantAccessScreen) applied(env screenEnv, msg grantAccessAppliedMsg) (grantAccessScreen, tea.Cmd) {
	s.applying = false
	granted := 0
	selection := s.selection
	for _, outcome := range msg.outcomes {
		if outcome.err == nil {
			granted++
			selection = selection.without(outcome.repo)
		}
	}
	s.selection = selection
	s.results = msg.outcomes
	name := msg.principalID
	for _, principal := range s.principals {
		if principal.ID == msg.principalID {
			name = principal.Name
		}
	}
	tone := grantToneSuccess
	if granted < len(msg.outcomes) {
		tone = grantToneWarning
	}
	s = s.setStatus(tone, fmt.Sprintf("Granted %s on %d of %d repositories to %s", grantRoleLabel(msg.role), granted, len(msg.outcomes), name))
	// Reload "Already has" for whoever was changed.
	s.grants = cloneGrantCache(s.grants)
	delete(s.grants, msg.principalID)
	return s.loadHighlightedGrants(env)
}

// ---- commands ----

func grantAccessLoadPrincipalsCmd(env screenEnv) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			unavailable := errors.New("admin API is unavailable for this session")
			return grantAccessPrincipalsLoadedMsg{usersErr: unavailable, robotsErr: unavailable}
		}
		users, usersErr := client.ListUsers(context.Background(), session)
		robots, robotsErr := client.ListRobots(context.Background(), session)
		return grantAccessPrincipalsLoadedMsg{users: users, usersErr: usersErr, robots: robots, robotsErr: robotsErr}
	}
}

func grantAccessLoadGrantsCmd(env screenEnv, userID string) tea.Cmd {
	client, session := env.Client, env.Session
	return func() tea.Msg {
		if client == nil {
			return grantAccessGrantsLoadedMsg{userID: userID, err: errors.New("admin API is unavailable for this session")}
		}
		grants, err := client.ListUserGrants(context.Background(), session, userID)
		return grantAccessGrantsLoadedMsg{userID: userID, grants: grants, err: err}
	}
}

// grantAccessApplyCmd runs every PutUserGrant sequentially inside ONE command
// and reports ONE message (this codebase deliberately avoids tea.Batch). A
// failure does not stop the remaining repositories and nothing is rolled back.
func grantAccessApplyCmd(env screenEnv, userID string, role domainauth.RepoRole, repositories []string) tea.Cmd {
	client, session := env.Client, env.Session
	repositories = slices.Clone(repositories)
	return func() tea.Msg {
		outcomes := make([]grantOutcome, 0, len(repositories))
		for _, repository := range repositories {
			if client == nil {
				outcomes = append(outcomes, grantOutcome{repo: repository, err: errors.New("admin API is unavailable for this session")})
				continue
			}
			_, err := client.PutUserGrant(context.Background(), session, ports.AdminPutRepoGrantInput{UserID: userID, Repository: repository, Role: role})
			outcomes = append(outcomes, grantOutcome{repo: repository, err: err})
		}
		return grantAccessAppliedMsg{principalID: userID, role: role, outcomes: outcomes}
	}
}

var grantAccessKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("Tab/Shift+Tab", "switch pane")),
	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("Up/Down", "move")),
	key.NewBinding(key.WithKeys("space"), key.WithHelp("Space", "toggle repository")),
	key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("Ctrl+A", "toggle all shown")),
	key.NewBinding(key.WithKeys("1", "2", "3"), key.WithHelp("1/2/3", "role")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "apply")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back")),
}}

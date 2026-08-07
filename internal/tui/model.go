package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appregixtry "regixtry/internal/app/regixtry"
	"regixtry/internal/ports"
)

type Option func(*Model)

func WithNotice(notice string) Option {
	return func(m *Model) {
		m.notice = strings.TrimSpace(notice)
	}
}

func WithAdminClient(adminClient AdminClient) Option {
	return func(m *Model) {
		m.adminClient = adminClient
	}
}

type QueryService interface {
	Catalog(ctx context.Context, limit int, after string) (appregixtry.CatalogResult, error)
	Tags(ctx context.Context, repositoryName string, limit int, after string) (appregixtry.TagsResult, error)
	ResolveManifest(ctx context.Context, repositoryName string, reference string) (appregixtry.ManifestDetails, error)
	Uploads(ctx context.Context, repositoryName string) ([]appregixtry.UploadDetails, error)
}

type RepositoriesModel struct {
	Items    []string
	Selected int
}

type TagsModel struct {
	Repository string
	Items      []string
	Selected   int
}

type ManifestModel struct {
	Details appregixtry.ManifestDetails
}

type BlobsModel struct {
	Items    []appregixtry.BlobDetails
	Selected int
}

type UploadsModel struct {
	Repository string
	Items      []appregixtry.UploadDetails
}

type EmptyStateModel struct {
	Title   string
	Message string
}

type MutationUnavailableModel struct {
	Action string
	Reason string
}

type adminUserMutationAction string

const (
	adminUserMutationActionEnable  adminUserMutationAction = "enable"
	adminUserMutationActionDisable adminUserMutationAction = "disable"
)

type adminUserMutationState struct {
	UserID   string
	Username string
	Action   adminUserMutationAction
	InFlight bool
}

func (m adminUserMutationState) Active() bool {
	return strings.TrimSpace(m.UserID) != ""
}

func (m adminUserMutationState) Verb() string {
	if m.Action == adminUserMutationActionEnable {
		return "enable"
	}
	return "disable"
}

func (m adminUserMutationState) PastTense() string {
	if m.Action == adminUserMutationActionEnable {
		return "enabled"
	}
	return "disabled"
}

type screen string

const (
	screenLoading             screen = "loading"
	screenRepositories        screen = "repositories"
	screenTags                screen = "tags"
	screenManifest            screen = "manifest"
	screenBlobs               screen = "blobs"
	screenUploads             screen = "uploads"
	screenEmpty               screen = "empty"
	screenError               screen = "error"
	screenAdminLogin          screen = "admin-login"
	screenAdminAuthenticating screen = "admin-authenticating"
	screenAdminUsers          screen = "admin-users"
	screenAdminGrants         screen = "admin-grants"
	screenAdminTokens         screen = "admin-tokens"
)

type adminAuthState string

const (
	adminAuthStateUnauthenticated adminAuthState = "unauthenticated"
	adminAuthStateAuthenticating  adminAuthState = "authenticating"
	adminAuthStateAuthenticated   adminAuthState = "authenticated"
	adminAuthStateExpired         adminAuthState = "expired"
)

type loginField int

const (
	loginFieldUsername loginField = iota
	loginFieldPassword
)

type adminLoginForm struct {
	Username string
	Password string
	Focus    loginField
}

type Model struct {
	ctx         context.Context
	service     QueryService
	notice      string
	screen      screen
	loadingText string
	err         error
	now         func() time.Time

	repositories       RepositoriesModel
	tags               TagsModel
	manifest           ManifestModel
	blobs              BlobsModel
	uploads            UploadsModel
	adminClient        AdminClient
	adminSession       AdminSession
	adminView          AdminViewState
	adminAuth          adminAuthState
	adminLogin         adminLoginForm
	adminReturn        screen
	empty              EmptyStateModel
	mutation           MutationUnavailableModel
	adminMutation      adminUserMutationState
	adminRefreshStatus string
	status             string

	showMutationNotice bool
	lastRepository     string
	lastTag            string
}

type catalogLoadedMsg struct {
	result appregixtry.CatalogResult
	err    error
}

type tagsLoadedMsg struct {
	repository string
	result     appregixtry.TagsResult
	err        error
}

type manifestLoadedMsg struct {
	repository string
	tag        string
	manifest   appregixtry.ManifestDetails
	uploads    []appregixtry.UploadDetails
	err        error
}

type adminLoginCompletedMsg struct {
	session AdminSession
	err     error
}

type adminUsersLoadedMsg struct {
	users []ports.AdminUser
	err   error
}

type adminUserMutatedMsg struct {
	user     ports.AdminUser
	mutation adminUserMutationState
	err      error
}

type adminUserGrantsLoadedMsg struct {
	userID   string
	username string
	grants   []ports.AdminRepoGrant
	err      error
}

type adminUserTokensLoadedMsg struct {
	userID   string
	username string
	tokens   []ports.AdminToken
	err      error
}

func NewModel(service QueryService, options ...Option) Model {
	m := Model{
		ctx:         context.Background(),
		service:     service,
		screen:      screenLoading,
		loadingText: "Loading repositories...",
		now:         func() time.Time { return time.Now().UTC() },
		adminAuth:   adminAuthStateUnauthenticated,
		adminReturn: screenLoading,
		empty: EmptyStateModel{
			Title:   "Regixtry is empty",
			Message: "No repositories have been published yet.",
		},
		mutation: MutationUnavailableModel{
			Action: "delete",
			Reason: "Mutations such as deletion and retention controls are unavailable in v1.",
		},
	}
	for _, option := range options {
		option(&m)
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return m.loadCatalogCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateKey(msg)
	case catalogLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}

		m.repositories = RepositoriesModel{Items: append([]string(nil), msg.result.Repositories...)}
		if len(m.repositories.Items) == 0 {
			m.screen = screenEmpty
			return m, nil
		}

		m.screen = screenRepositories
		m.loadingText = ""
		return m, nil
	case tagsLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}

		m.tags = TagsModel{Repository: msg.repository, Items: append([]string(nil), msg.result.Tags...)}
		m.lastRepository = msg.repository
		m.showMutationNotice = false
		m.status = ""
		if len(m.tags.Items) == 0 {
			m.screen = screenEmpty
			m.empty = EmptyStateModel{
				Title:   "Repository has no tags",
				Message: fmt.Sprintf("%s has no published tags yet.", msg.repository),
			}
			return m, nil
		}

		m.screen = screenTags
		return m, nil
	case manifestLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}

		m.lastRepository = msg.repository
		m.lastTag = msg.tag
		m.manifest = ManifestModel{Details: msg.manifest}
		m.blobs = BlobsModel{Items: append([]appregixtry.BlobDetails(nil), msg.manifest.Blobs...)}
		sort.Slice(msg.uploads, func(i, j int) bool {
			return msg.uploads[i].StartedAt.Before(msg.uploads[j].StartedAt)
		})
		m.uploads = UploadsModel{Repository: msg.repository, Items: append([]appregixtry.UploadDetails(nil), msg.uploads...)}
		m.showMutationNotice = false
		m.status = ""
		m.screen = screenManifest
		return m, nil
	case adminLoginCompletedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminAuth = adminAuthStateUnauthenticated
			m.adminSession, m.adminView = LogoutAdminState()
			m.adminLogin.Password = ""
			m.screen = screenAdminLogin
			m.loadingText = ""
			m.status = msg.err.Error()
			return m, nil
		}

		m.adminSession = msg.session
		m.adminSession.ExpiredReason = ""
		m.adminAuth = adminAuthStateAuthenticated
		m.adminLogin.Username = msg.session.Username
		m.adminLogin.Password = ""
		m.screen = screenAdminUsers
		m.loadingText = ""
		m.status = "Loading admin users..."
		return m, m.loadAdminUsersCmd()
	case adminUsersLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminRefreshStatus = ""
			m.screen = screenAdminUsers
			m.status = msg.err.Error()
			return m, nil
		}

		selected := m.adminView.SelectedUserID
		m.adminView.Users = append([]ports.AdminUser(nil), msg.users...)
		m.adminView.SelectedUser = 0
		if selected != "" {
			for index, user := range m.adminView.Users {
				if user.ID == selected {
					m.adminView.SelectedUser = index
					break
				}
			}
		}
		if len(m.adminView.Users) == 0 {
			m.adminView.SelectedUserID = ""
			m.adminView.SelectedUsername = ""
			m.adminView.Grants = nil
			m.adminView.AdminTokens = nil
			m.adminRefreshStatus = ""
			m.status = "No admin users found."
			m.screen = screenAdminUsers
			return m, nil
		}

		m.adminView.SelectedUser = boundedIndex(m.adminView.SelectedUser, len(m.adminView.Users))
		if user, ok := m.selectedAdminUser(); ok {
			m.adminView.SelectedUserID = user.ID
			m.adminView.SelectedUsername = user.Username
		}
		m.status = m.adminRefreshStatus
		m.adminRefreshStatus = ""
		m.screen = screenAdminUsers
		return m, nil
	case adminUserMutatedMsg:
		m.adminMutation = adminUserMutationState{}
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.screen = screenAdminUsers
			m.status = msg.err.Error()
			return m, nil
		}

		m.adminView.SelectedUserID = msg.user.ID
		m.adminView.SelectedUsername = msg.user.Username
		m.screen = screenAdminUsers
		m.adminRefreshStatus = fmt.Sprintf("User %q %s.", msg.user.Username, msg.mutation.PastTense())
		m.status = fmt.Sprintf("User %q %s. Refreshing users...", msg.user.Username, msg.mutation.PastTense())
		return m, m.loadAdminUsersCmd()
	case adminUserGrantsLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}

		m.adminView.SelectedUserID = msg.userID
		m.adminView.SelectedUsername = msg.username
		m.adminView.Grants = append([]ports.AdminRepoGrant(nil), msg.grants...)
		m.status = ""
		m.screen = screenAdminGrants
		return m, nil
	case adminUserTokensLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}

		m.adminView.SelectedUserID = msg.userID
		m.adminView.SelectedUsername = msg.username
		m.adminView.AdminTokens = append([]ports.AdminToken(nil), msg.tokens...)
		m.status = ""
		m.screen = screenAdminTokens
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	var body strings.Builder
	body.WriteString("Regixtry Console\n\n")

	switch m.screen {
	case screenLoading:
		body.WriteString(m.loadingText)
	case screenRepositories:
		body.WriteString("Repositories\n")
		body.WriteString(renderList(m.repositories.Items, m.repositories.Selected))
		body.WriteString("\n\n")
		body.WriteString("Enter: open tags · tab: admin · q: quit")
		if strings.TrimSpace(m.notice) != "" {
			body.WriteString("\n\nNotice\n")
			body.WriteString(m.notice)
		}
	case screenTags:
		body.WriteString(fmt.Sprintf("Tags · %s\n", m.tags.Repository))
		body.WriteString(renderList(m.tags.Items, m.tags.Selected))
		body.WriteString("\n\nEnter: inspect manifest · tab: admin · esc: back · q: quit")
	case screenManifest:
		body.WriteString(renderManifest(m.manifest.Details))
		body.WriteString("\n\n")
		body.WriteString("b: blobs · u: uploads · d: unsupported delete · tab: admin · esc: back · q: quit")
	case screenBlobs:
		body.WriteString(renderBlobs(m.blobs))
		body.WriteString("\n\ntab: admin · esc: back · q: quit")
	case screenUploads:
		body.WriteString(renderUploads(m.uploads))
		body.WriteString("\n\ntab: admin · esc: back · q: quit")
	case screenEmpty:
		body.WriteString(m.empty.Title)
		body.WriteString("\n")
		body.WriteString(m.empty.Message)
		body.WriteString("\n\ntab: admin · esc: back · q: quit")
	case screenError:
		body.WriteString("Error\n")
		body.WriteString(m.err.Error())
		body.WriteString("\n\nq: quit")
	case screenAdminLogin:
		body.WriteString(renderAdminLogin(m.adminLogin))
	case screenAdminAuthenticating:
		body.WriteString("Admin Login\n")
		body.WriteString(m.loadingText)
		body.WriteString("\n\nq: quit")
	case screenAdminUsers:
		body.WriteString(renderAdminUsers(m.adminSession, m.adminView, m.adminMutation, m.now()))
	case screenAdminGrants:
		body.WriteString(renderAdminGrants(m.adminSession, m.adminView, m.now()))
	case screenAdminTokens:
		body.WriteString(renderAdminTokens(m.adminSession, m.adminView, m.now()))
	}

	if m.showMutationNotice {
		body.WriteString("\n\nUnavailable in v1\n")
		body.WriteString(fmt.Sprintf("%s: %s", strings.Title(m.mutation.Action), m.mutation.Reason))
	}
	if m.status != "" {
		body.WriteString("\n\nStatus\n")
		body.WriteString(m.status)
	}

	return body.String()
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	}

	if isAdminScreen(m.screen) {
		return m.updateAdminKey(msg)
	}

	if msg.String() == "tab" {
		return m.openAdmin()
	}

	switch msg.String() {
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(1)
		return m, nil
	case "enter":
		switch m.screen {
		case screenRepositories:
			if repository, ok := m.selectedRepository(); ok {
				m.screen = screenLoading
				m.loadingText = fmt.Sprintf("Loading tags for %s...", repository)
				return m, m.loadTagsCmd(repository)
			}
		case screenTags:
			if tag, ok := m.selectedTag(); ok {
				m.screen = screenLoading
				m.loadingText = fmt.Sprintf("Loading manifest %s:%s...", m.tags.Repository, tag)
				return m, m.loadManifestCmd(m.tags.Repository, tag)
			}
		}
	case "esc", "backspace":
		m.showMutationNotice = false
		m.status = ""
		switch m.screen {
		case screenTags, screenEmpty:
			if m.lastRepository != "" {
				m.screen = screenRepositories
				return m, nil
			}
		case screenManifest:
			m.screen = screenTags
			return m, nil
		case screenBlobs, screenUploads:
			m.screen = screenManifest
			return m, nil
		}
	case "b":
		if m.screen == screenManifest {
			m.screen = screenBlobs
			m.showMutationNotice = false
		}
		return m, nil
	case "u":
		if m.screen == screenManifest {
			m.screen = screenUploads
			m.showMutationNotice = false
		}
		return m, nil
	case "d", "x":
		if m.screen == screenManifest || m.screen == screenBlobs || m.screen == screenUploads {
			m.showMutationNotice = true
		}
		return m, nil
	}

	return m, nil
}

func (m Model) updateAdminKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "l" && m.adminAuth == adminAuthStateAuthenticated {
		if m.adminMutation.InFlight {
			return m, nil
		}
		return m.logoutAdmin(), nil
	}

	switch m.screen {
	case screenAdminLogin:
		return m.updateAdminLoginKey(msg)
	case screenAdminAuthenticating:
		return m, nil
	case screenAdminUsers:
		return m.updateAdminUsersKey(msg)
	case screenAdminGrants, screenAdminTokens:
		return m.updateAdminDetailKey(msg)
	default:
		return m, nil
	}
}

func (m Model) updateAdminLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.returnToInspection(), nil
	case "tab", "up", "down":
		m.adminLogin.Focus = oppositeLoginField(m.adminLogin.Focus)
		return m, nil
	case "enter":
		username := strings.TrimSpace(m.adminLogin.Username)
		if username == "" || m.adminLogin.Password == "" {
			m.status = "Username and password are required."
			return m, nil
		}
		if m.adminClient == nil {
			m.status = "Admin API is unavailable for this session."
			return m, nil
		}

		m.adminAuth = adminAuthStateAuthenticating
		m.screen = screenAdminAuthenticating
		m.loadingText = fmt.Sprintf("Signing in as %s...", username)
		m.status = ""
		return m, m.loginCmd(username, m.adminLogin.Password)
	case "backspace":
		m.deleteLoginRune()
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		m.appendLoginRunes(string(msg.Runes))
		return m, nil
	}

	return m, nil
}

func (m Model) updateAdminUsersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.adminMutation.InFlight {
		return m, nil
	}

	if m.adminMutation.Active() {
		switch msg.String() {
		case "esc", "n":
			m.adminMutation = adminUserMutationState{}
			return m, nil
		case "enter":
			m.adminMutation.InFlight = true
			m.status = fmt.Sprintf("Submitting %s for %s...", m.adminMutation.Verb(), m.adminMutation.Username)
			return m, m.mutateAdminUserCmd(m.adminMutation)
		default:
			return m, nil
		}
	}

	switch msg.String() {
	case "esc":
		return m.returnToInspection(), nil
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(1)
		return m, nil
	case "enter":
		user, ok := m.selectedAdminUser()
		if !ok {
			return m, nil
		}
		m.adminView.SelectedUserID = user.ID
		m.adminView.SelectedUsername = user.Username
		m.adminView.Grants = nil
		m.adminView.AdminTokens = nil
		m.status = fmt.Sprintf("Loading grants for %s...", user.Username)
		return m, m.loadAdminGrantsCmd(user.ID, user.Username)
	case "r":
		m.status = "Loading admin users..."
		return m, m.loadAdminUsersCmd()
	case "e", "d":
		user, ok := m.selectedAdminUser()
		if !ok {
			return m, nil
		}
		if msg.String() == "e" && user.Enabled {
			return m, nil
		}
		if msg.String() == "d" && !user.Enabled {
			return m, nil
		}
		m.adminMutation = adminUserMutationState{UserID: user.ID, Username: user.Username}
		m.adminView.SelectedUserID = user.ID
		m.adminView.SelectedUsername = user.Username
		if msg.String() == "e" {
			m.adminMutation.Action = adminUserMutationActionEnable
		} else {
			m.adminMutation.Action = adminUserMutationActionDisable
		}
		m.status = ""
		return m, nil
	}

	return m, nil
}

func (m Model) updateAdminDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenAdminUsers
		m.status = ""
		return m, nil
	case "g":
		if m.adminView.SelectedUserID == "" {
			return m, nil
		}
		m.status = fmt.Sprintf("Loading grants for %s...", m.adminView.SelectedUsername)
		return m, m.loadAdminGrantsCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
	case "t":
		if m.adminView.SelectedUserID == "" {
			return m, nil
		}
		m.status = fmt.Sprintf("Loading admin tokens for %s...", m.adminView.SelectedUsername)
		return m, m.loadAdminTokensCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
	}

	return m, nil
}

func (m Model) moveSelection(delta int) {
	switch m.screen {
	case screenRepositories:
		m.repositories.Selected = boundedIndex(m.repositories.Selected+delta, len(m.repositories.Items))
	case screenTags:
		m.tags.Selected = boundedIndex(m.tags.Selected+delta, len(m.tags.Items))
	case screenBlobs:
		m.blobs.Selected = boundedIndex(m.blobs.Selected+delta, len(m.blobs.Items))
	case screenAdminUsers:
		m.adminView.SelectedUser = boundedIndex(m.adminView.SelectedUser+delta, len(m.adminView.Users))
		if user, ok := m.selectedAdminUser(); ok {
			m.adminView.SelectedUserID = user.ID
			m.adminView.SelectedUsername = user.Username
		}
	}
}

func boundedIndex(index int, size int) int {
	if size == 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= size {
		return size - 1
	}
	return index
}

func (m Model) selectedRepository() (string, bool) {
	if len(m.repositories.Items) == 0 {
		return "", false
	}
	return m.repositories.Items[m.repositories.Selected], true
}

func (m Model) selectedTag() (string, bool) {
	if len(m.tags.Items) == 0 {
		return "", false
	}
	return m.tags.Items[m.tags.Selected], true
}

func (m Model) selectedAdminUser() (ports.AdminUser, bool) {
	if len(m.adminView.Users) == 0 {
		return ports.AdminUser{}, false
	}
	index := boundedIndex(m.adminView.SelectedUser, len(m.adminView.Users))
	return m.adminView.Users[index], true
}

func (m Model) loadCatalogCmd() tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.Catalog(m.ctx, 100, "")
		return catalogLoadedMsg{result: result, err: err}
	}
}

func (m Model) loadTagsCmd(repository string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.Tags(m.ctx, repository, 100, "")
		return tagsLoadedMsg{repository: repository, result: result, err: err}
	}
}

func (m Model) loadManifestCmd(repository string, tag string) tea.Cmd {
	return func() tea.Msg {
		manifest, err := m.service.ResolveManifest(m.ctx, repository, tag)
		if err != nil {
			return manifestLoadedMsg{repository: repository, tag: tag, err: err}
		}

		uploads, uploadsErr := m.service.Uploads(m.ctx, repository)
		return manifestLoadedMsg{repository: repository, tag: tag, manifest: manifest, uploads: uploads, err: uploadsErr}
	}
}

func (m Model) loginCmd(username string, password string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminLoginCompletedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		session, err := m.adminClient.Login(m.ctx, username, password)
		return adminLoginCompletedMsg{session: session, err: err}
	}
}

func (m Model) loadAdminUsersCmd() tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUsersLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		users, err := m.adminClient.ListUsers(m.ctx, m.adminSession)
		return adminUsersLoadedMsg{users: users, err: err}
	}
}

func (m Model) mutateAdminUserCmd(mutation adminUserMutationState) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUserMutatedMsg{mutation: mutation, err: fmt.Errorf("admin API is unavailable for this session")}
		}

		var (
			user ports.AdminUser
			err  error
		)
		switch mutation.Action {
		case adminUserMutationActionEnable:
			user, err = m.adminClient.EnableUser(m.ctx, m.adminSession, mutation.UserID)
		default:
			user, err = m.adminClient.DisableUser(m.ctx, m.adminSession, mutation.UserID)
		}
		return adminUserMutatedMsg{user: user, mutation: mutation, err: err}
	}
}

func (m Model) loadAdminGrantsCmd(userID string, username string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUserGrantsLoadedMsg{userID: userID, username: username, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		grants, err := m.adminClient.ListUserGrants(m.ctx, m.adminSession, userID)
		return adminUserGrantsLoadedMsg{userID: userID, username: username, grants: grants, err: err}
	}
}

func (m Model) loadAdminTokensCmd(userID string, username string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUserTokensLoadedMsg{userID: userID, username: username, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		tokens, err := m.adminClient.ListUserAdminTokens(m.ctx, m.adminSession, userID)
		return adminUserTokensLoadedMsg{userID: userID, username: username, tokens: tokens, err: err}
	}
}

func (m Model) openAdmin() (tea.Model, tea.Cmd) {
	if m.adminClient == nil {
		m.status = "Admin API is unavailable for this session."
		return m, nil
	}

	m.adminReturn = m.screen
	m.showMutationNotice = false
	m.err = nil

	if m.adminSession.IsAuthenticated() {
		if m.adminSession.IsExpired(m.now()) {
			return m.expireAdminSession(AdminSessionExpiredReasonExpired), nil
		}
		m.adminAuth = adminAuthStateAuthenticated
		m.status = ""
		m.screen = screenAdminUsers
		if len(m.adminView.Users) == 0 {
			m.status = "Loading admin users..."
			return m, m.loadAdminUsersCmd()
		}
		return m, nil
	}

	if strings.TrimSpace(m.adminSession.ExpiredReason) != "" {
		m.adminAuth = adminAuthStateExpired
		m.status = m.adminSession.ExpiredReason
	} else {
		m.adminAuth = adminAuthStateUnauthenticated
		m.status = ""
	}
	m.screen = screenAdminLogin
	return m, nil
}

func (m Model) logoutAdmin() Model {
	m.adminSession, m.adminView = LogoutAdminState()
	m.adminAuth = adminAuthStateUnauthenticated
	m.adminLogin.Password = ""
	m.adminLogin.Focus = loginFieldUsername
	m.adminMutation = adminUserMutationState{}
	m.adminRefreshStatus = ""
	m.screen = screenAdminLogin
	m.loadingText = ""
	m.status = "Logged out."
	return m
}

func (m Model) returnToInspection() Model {
	m.screen = m.adminReturn
	m.loadingText = ""
	m.adminMutation = adminUserMutationState{}
	m.adminRefreshStatus = ""
	m.status = ""
	return m
}

func (m Model) expireAdminSession(reason string) Model {
	m.adminSession, m.adminView = ExpireAdminState(reason)
	m.adminAuth = adminAuthStateExpired
	m.adminLogin.Password = ""
	m.adminLogin.Focus = loginFieldUsername
	m.adminMutation = adminUserMutationState{}
	m.adminRefreshStatus = ""
	m.screen = screenAdminLogin
	m.loadingText = ""
	m.status = m.adminSession.ExpiredReason
	return m
}

func (m *Model) appendLoginRunes(value string) {
	if value == "" {
		return
	}
	if m.adminLogin.Focus == loginFieldPassword {
		m.adminLogin.Password += value
		return
	}
	m.adminLogin.Username += value
}

func (m *Model) deleteLoginRune() {
	if m.adminLogin.Focus == loginFieldPassword {
		m.adminLogin.Password = trimLastRune(m.adminLogin.Password)
		return
	}
	m.adminLogin.Username = trimLastRune(m.adminLogin.Username)
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func oppositeLoginField(field loginField) loginField {
	if field == loginFieldPassword {
		return loginFieldUsername
	}
	return loginFieldPassword
}

func isAdminScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminAuthenticating, screenAdminUsers, screenAdminGrants, screenAdminTokens:
		return true
	default:
		return false
	}
}

func renderList(items []string, selected int) string {
	if len(items) == 0 {
		return "- none -"
	}

	lines := make([]string, 0, len(items))
	for index, item := range items {
		prefix := "  "
		if index == selected {
			prefix = "> "
		}
		lines = append(lines, prefix+item)
	}
	return strings.Join(lines, "\n")
}

func renderManifest(manifest appregixtry.ManifestDetails) string {
	lines := []string{
		fmt.Sprintf("Manifest · %s:%s", manifest.Repository, manifest.Reference),
		fmt.Sprintf("Digest: %s", manifest.Digest),
		fmt.Sprintf("Media Type: %s", manifest.MediaType),
		fmt.Sprintf("Size: %d bytes", manifest.Size),
		fmt.Sprintf("Blobs: %d", len(manifest.Blobs)),
	}
	if len(manifest.Annotations) > 0 {
		keys := make([]string, 0, len(manifest.Annotations))
		for key := range manifest.Annotations {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("Annotation %s=%s", key, manifest.Annotations[key]))
		}
	}
	return strings.Join(lines, "\n")
}

func renderBlobs(blobs BlobsModel) string {
	lines := []string{"Blobs"}
	if len(blobs.Items) == 0 {
		return strings.Join(append(lines, "No blobs are linked to the selected manifest."), "\n")
	}
	for index, blob := range blobs.Items {
		prefix := "  "
		if index == blobs.Selected {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s (%d bytes) [%s]", prefix, blob.Digest, blob.Size, blob.MediaType))
	}
	return strings.Join(lines, "\n")
}

func renderUploads(uploads UploadsModel) string {
	lines := []string{fmt.Sprintf("Uploads · %s", uploads.Repository)}
	if len(uploads.Items) == 0 {
		return strings.Join(append(lines, "No in-progress uploads for this repository."), "\n")
	}
	for _, upload := range uploads.Items {
		lines = append(lines, fmt.Sprintf("- %s · %s · %d bytes", upload.ID, upload.Status, upload.Size))
	}
	return strings.Join(lines, "\n")
}

func renderAdminLogin(form adminLoginForm) string {
	usernamePrefix := "  "
	passwordPrefix := "  "
	if form.Focus == loginFieldUsername {
		usernamePrefix = "> "
	} else {
		passwordPrefix = "> "
	}

	return strings.Join([]string{
		"Admin Login",
		fmt.Sprintf("%sUsername: %s", usernamePrefix, form.Username),
		fmt.Sprintf("%sPassword: %s", passwordPrefix, strings.Repeat("*", len([]rune(form.Password)))),
		"",
		"Enter: sign in · tab: switch field · esc: back · q: quit",
	}, "\n")
}

func renderAdminUsers(session AdminSession, view AdminViewState, mutation adminUserMutationState, now time.Time) string {
	lines := adminHeader(session, now)
	lines = append(lines, "Users")
	if len(view.Users) == 0 {
		lines = append(lines, "No admin users available.")
	} else {
		for index, user := range view.Users {
			prefix := "  "
			if index == view.SelectedUser {
				prefix = "> "
			}
			role := "user"
			if user.IsAdmin {
				role = "admin"
			}
			state := "disabled"
			if user.Enabled {
				state = "enabled"
			}
			lines = append(lines, fmt.Sprintf("%s%s [%s, %s]", prefix, user.Username, role, state))
		}
	}
	if mutation.Active() {
		lines = append(lines, "")
		if mutation.InFlight {
			lines = append(lines,
				fmt.Sprintf("Submitting %s for %q...", mutation.Verb(), mutation.Username),
				"Please wait until the current mutation completes.",
			)
		} else {
			lines = append(lines,
				fmt.Sprintf("Confirm %s user %q?", mutation.Verb(), mutation.Username),
				"Enter: confirm · n: cancel · esc: cancel · l: logout · q: quit",
			)
		}
		return strings.Join(lines, "\n")
	}

	hints := []string{"Enter: view grants", "r: refresh"}
	if user, ok := selectedAdminUserForView(view); ok {
		if user.Enabled {
			hints = append(hints, "d: disable")
		} else {
			hints = append(hints, "e: enable")
		}
	}
	hints = append(hints, "esc: inspection", "l: logout", "q: quit")
	lines = append(lines, "", strings.Join(hints, " · "))
	return strings.Join(lines, "\n")
}

func selectedAdminUserForView(view AdminViewState) (ports.AdminUser, bool) {
	if len(view.Users) == 0 {
		return ports.AdminUser{}, false
	}
	index := boundedIndex(view.SelectedUser, len(view.Users))
	return view.Users[index], true
}

func renderAdminGrants(session AdminSession, view AdminViewState, now time.Time) string {
	lines := adminHeader(session, now)
	lines = append(lines, fmt.Sprintf("Repository Grants · %s", view.SelectedUsername))
	if len(view.Grants) == 0 {
		lines = append(lines, "No repository grants for the selected user.")
	} else {
		for _, grant := range view.Grants {
			lines = append(lines, fmt.Sprintf("- %s · %s", grant.Repository, grant.Role))
		}
	}
	lines = append(lines, "", "t: admin tokens · g: refresh grants · esc: back · l: logout · q: quit")
	return strings.Join(lines, "\n")
}

func renderAdminTokens(session AdminSession, view AdminViewState, now time.Time) string {
	lines := adminHeader(session, now)
	lines = append(lines, fmt.Sprintf("Admin Tokens · %s", view.SelectedUsername))
	if len(view.AdminTokens) == 0 {
		lines = append(lines, "No admin tokens for the selected user.")
	} else {
		for _, token := range view.AdminTokens {
			expiresAt := token.ExpiresAt.UTC().Format(time.RFC3339)
			state := "active"
			if token.RevokedAt != nil {
				state = "revoked"
			}
			lines = append(lines, fmt.Sprintf("- %s · %s · expires %s", token.Accessor, state, expiresAt))
		}
	}
	lines = append(lines, "", "g: grants · t: refresh tokens · esc: back · l: logout · q: quit")
	return strings.Join(lines, "\n")
}

func adminHeader(session AdminSession, now time.Time) []string {
	lines := []string{"Admin"}
	if strings.TrimSpace(session.Username) != "" {
		lines[0] = fmt.Sprintf("Admin · %s", session.Username)
	}
	if !session.ExpiresAt.IsZero() {
		lines = append(lines, fmt.Sprintf("Session expires in %s", formatRemaining(session.Remaining(now))))
	}
	return append(lines, "")
}

func formatRemaining(remaining time.Duration) string {
	if remaining <= 0 {
		return "0s"
	}
	return remaining.Truncate(time.Second).String()
}

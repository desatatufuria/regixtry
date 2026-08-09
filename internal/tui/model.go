package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
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
	screenAdminCreateUser     screen = "admin-create-user"
	screenAdminEditUser       screen = "admin-edit-user"
	screenAdminChangePassword screen = "admin-change-password"
	screenAdminEditUserGrants screen = "admin-edit-user-grants"
	screenAdminAddGrant       screen = "admin-add-grant"
	screenAdminEditUserTokens screen = "admin-edit-user-tokens"
	screenAdminCreateToken    screen = "admin-create-token"
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

	repositories RepositoriesModel
	tags         TagsModel
	manifest     ManifestModel
	blobs        BlobsModel
	uploads      UploadsModel

	adminClient  AdminClient
	adminSession AdminSession
	adminView    AdminViewState
	adminAuth    adminAuthState
	adminLogin   adminLoginForm
	adminReturn  screen

	empty    EmptyStateModel
	mutation MutationUnavailableModel
	status   string

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

type adminUserCreatedMsg struct {
	user ports.AdminUser
	err  error
}

type adminPasswordResetMsg struct {
	userID   string
	username string
	err      error
}

type adminGrantMutatedMsg struct {
	userID     string
	username   string
	repository string
	err        error
}

type adminTokenCreatedMsg struct {
	created ports.AdminCreatedToken
	err     error
}

type adminTokenRevokedMsg struct {
	userID   string
	username string
	accessor string
	err      error
}

type adminUserEnabledMsg struct {
	user    ports.AdminUser
	enabled bool
	err     error
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
		adminView:   newAdminViewState(),
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
			m.empty = EmptyStateModel{Title: "Repository has no tags", Message: fmt.Sprintf("%s has no published tags yet.", msg.repository)}
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
		sort.Slice(msg.uploads, func(i, j int) bool { return msg.uploads[i].StartedAt.Before(msg.uploads[j].StartedAt) })
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
		m.loadingText = ""
		m.status = "Loading admin users..."
		m.screen = screenAdminUsers
		return m, m.loadAdminUsersCmd()
	case adminUsersLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.applyLoadedUsers(msg.users)
		if len(m.adminView.Users) == 0 {
			m.status = "No admin users found."
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		return m, nil
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
		m.adminView.SelectedGrant = boundedIndex(m.adminView.SelectedGrant, len(m.adminView.Grants))
		if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
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
		m.adminView.SelectedToken = boundedIndex(m.adminView.SelectedToken, len(m.adminView.AdminTokens))
		if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		return m, nil
	case adminUserCreatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.CreateUserForm = newAdminViewState().CreateUserForm
		m.adminView.SelectedUserID = msg.user.ID
		m.adminView.SelectedUsername = msg.user.Username
		m.status = fmt.Sprintf("User %q created. Refreshing users...", msg.user.Username)
		m.screen = screenAdminUsers
		return m, m.loadAdminUsersCmd()
	case adminPasswordResetMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.ResetPasswordForm = adminResetPasswordForm{}
		m.adminView.SelectedUserID = msg.userID
		m.adminView.SelectedUsername = msg.username
		m.status = fmt.Sprintf("Password reset for %q.", msg.username)
		m.screen = screenAdminEditUser
		return m, nil
	case adminGrantMutatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.GrantForm = newAdminViewState().GrantForm
		m.adminView.ConfirmModal = adminConfirmModal{}
		m.adminView.SelectedUserID = msg.userID
		m.adminView.SelectedUsername = msg.username
		if msg.repository == "" {
			m.status = fmt.Sprintf("Grant saved for %q. Refreshing grants...", msg.username)
		} else {
			m.status = fmt.Sprintf("Grant removed from %q. Refreshing grants...", msg.username)
		}
		m.screen = screenAdminEditUserGrants
		return m, m.loadAdminGrantsCmd(msg.userID, msg.username)
	case adminTokenCreatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.TokenForm = adminTokenForm{}
		m.adminView.SelectedUserID = msg.created.TargetUser.ID
		m.adminView.SelectedUsername = msg.created.TargetUser.Username
		m.adminView.RevealedTokenSecret = msg.created.Secret
		m.adminView.RevealedTokenAccessor = msg.created.Accessor
		m.adminView.RevealedTokenExpiresAt = msg.created.ExpiresAt
		m.status = fmt.Sprintf("Admin token created for %q. Refreshing tokens...", msg.created.TargetUser.Username)
		m.screen = screenAdminEditUserTokens
		return m, m.loadAdminTokensCmd(msg.created.TargetUser.ID, msg.created.TargetUser.Username)
	case adminTokenRevokedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{}
		m.adminView.RevealedTokenSecret = ""
		m.adminView.RevealedTokenAccessor = ""
		m.status = fmt.Sprintf("Admin token revoked for %q. Refreshing tokens...", msg.username)
		m.screen = screenAdminEditUserTokens
		return m, m.loadAdminTokensCmd(msg.userID, msg.username)
	case adminUserEnabledMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{}
		m.adminView.SelectedUserID = msg.user.ID
		m.adminView.SelectedUsername = msg.user.Username
		verb := "disabled"
		if msg.enabled {
			verb = "enabled"
		}
		m.status = fmt.Sprintf("User %q %s. Refreshing users...", msg.user.Username, verb)
		m.screen = screenAdminEditUser
		return m, m.loadAdminUsersCmd()
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
		if m.status != "" {
			body.WriteString("\n\nStatus\n")
			body.WriteString(m.status)
		}
	case screenAdminAuthenticating:
		body.WriteString("Admin Login\n")
		body.WriteString(m.loadingText)
		body.WriteString("\n\nq: quit")
	case screenAdminUsers, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens, screenAdminCreateToken:
		body.WriteString(renderAdminWorkspace(m.screen, m.adminSession, m.adminView, m.status, m.now()))
	}

	if m.showMutationNotice {
		body.WriteString("\n\nUnavailable in v1\n")
		body.WriteString(fmt.Sprintf("%s: %s", strings.Title(m.mutation.Action), m.mutation.Reason))
	}

	return body.String()
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		if !isAdminScreen(m.screen) {
			return m, tea.Quit
		}
	}

	if isAdminScreen(m.screen) {
		return m.updateAdminKey(msg)
	}

	if msg.String() == "tab" {
		return m.openAdmin()
	}

	switch {
	case isMoveUpKey(msg):
		m.moveSelection(-1)
		return m, nil
	case isMoveDownKey(msg):
		m.moveSelection(1)
		return m, nil
	case isEnterKey(msg):
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
	case isBackKey(msg):
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
	case isRuneKey(msg, 'b'):
		if m.screen == screenManifest {
			m.screen = screenBlobs
			m.showMutationNotice = false
		}
		return m, nil
	case isRuneKey(msg, 'u'):
		if m.screen == screenManifest {
			m.screen = screenUploads
			m.showMutationNotice = false
		}
		return m, nil
	case isRuneKey(msg, 'd', 'x'):
		if m.screen == screenManifest || m.screen == screenBlobs || m.screen == screenUploads {
			m.showMutationNotice = true
		}
		return m, nil
	}

	return m, nil
}

func (m Model) updateAdminKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if isRuneKey(msg, 'l') && m.adminAuth == adminAuthStateAuthenticated && !m.adminView.ConfirmModal.Active() {
		return m.logoutAdmin(), nil
	}

	if m.adminView.ConfirmModal.Active() {
		return m.updateAdminConfirmKey(msg)
	}

	if isRuneKey(msg, 'q') && isAdminPrincipalScreen(m.screen) {
		return m, tea.Quit
	}

	switch m.screen {
	case screenAdminLogin:
		return m.updateAdminLoginKey(msg)
	case screenAdminAuthenticating:
		return m, nil
	case screenAdminUsers:
		return m.updateAdminUsersKey(msg)
	case screenAdminCreateUser:
		return m.updateCreateUserFormKey(msg)
	case screenAdminEditUser:
		return m.updateAdminEditUserKey(msg)
	case screenAdminChangePassword:
		return m.updateResetPasswordFormKey(msg)
	case screenAdminEditUserGrants:
		return m.updateAdminGrantsKey(msg)
	case screenAdminAddGrant:
		return m.updateGrantFormKey(msg)
	case screenAdminEditUserTokens:
		return m.updateAdminTokensKey(msg)
	case screenAdminCreateToken:
		return m.updateTokenFormKey(msg)
	default:
		return m, nil
	}
}

func (m Model) updateAdminLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		return m.returnToInspection(), nil
	case isTabKey(msg), isMoveUpKey(msg), isMoveDownKey(msg):
		m.adminLogin.Focus = oppositeLoginField(m.adminLogin.Focus)
		return m, nil
	case isEnterKey(msg):
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
	case isBackspaceKey(msg):
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
	switch {
	case isEscKey(msg):
		return m.returnToInspection(), nil
	case m.adminView.UserSearchActive:
		return m.updateAdminSearchKey(msg)
	case isRuneKey(msg, '/'):
		m.adminView.UserSearchActive = true
		m.status = ""
		return m, nil
	case isMoveUpKey(msg):
		return m.moveAdminSidebarSelection(-1)
	case isMoveDownKey(msg):
		return m.moveAdminSidebarSelection(1)
	case isEnterKey(msg), isRuneKey(msg, 'e'):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user to edit."
			return m, nil
		}
		return m.openAdminEditUser()
	case isRuneKey(msg, 'n'):
		m.screen = screenAdminCreateUser
		m.adminView.CreateUserForm = newAdminViewState().CreateUserForm
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'r'):
		m.status = "Loading admin users..."
		return m, m.loadAdminUsersCmd()
	}

	return m, nil
}

func (m Model) updateAdminSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.UserSearchActive = false
		m.status = ""
		return m, nil
	case isEnterKey(msg):
		m.adminView.UserSearchActive = false
		m.status = ""
		return m, nil
	case isBackspaceKey(msg):
		m.adminView.UserSearchQuery = trimLastRune(m.adminView.UserSearchQuery)
		return m.applyAdminUserFilter()
	case isMoveUpKey(msg):
		return m.moveAdminSidebarSelection(-1)
	case isMoveDownKey(msg):
		return m.moveAdminSidebarSelection(1)
	}

	if msg.Type == tea.KeyRunes {
		m.adminView.UserSearchQuery += string(msg.Runes)
		return m.applyAdminUserFilter()
	}

	return m, nil
}

func (m Model) moveAdminSidebarSelection(delta int) (tea.Model, tea.Cmd) {
	previousUserID := m.adminView.SelectedUserID
	filteredUsers := filteredAdminUsers(m.adminView)
	if len(filteredUsers) == 0 {
		m.adminView.SelectedUser = 0
		m.adminView.SelectedUserID = ""
		m.adminView.SelectedUsername = ""
		m.clearSelectedAdminDetails()
		return m, nil
	}

	m.adminView.SelectedUser = boundedIndex(m.adminView.SelectedUser+delta, len(filteredUsers))
	selectedUser := filteredUsers[m.adminView.SelectedUser]
	m.adminView.SelectedUserID = selectedUser.ID
	m.adminView.SelectedUsername = selectedUser.Username
	if previousUserID != selectedUser.ID {
		m.clearSelectedAdminDetails()
	}
	return m, m.reloadAdminPanelForSelectedUserChange(previousUserID != selectedUser.ID)
}

func (m Model) applyAdminUserFilter() (tea.Model, tea.Cmd) {
	previousUserID := m.adminView.SelectedUserID
	selectionChanged := m.syncAdminUserSelection(previousUserID)
	if m.adminView.SelectedUserID == "" {
		m.clearSelectedAdminDetails()
	}
	return m, m.reloadAdminPanelForSelectedUserChange(selectionChanged)
}

func (m Model) updateAdminEditUserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.clearRevealedAdminToken()
		m.screen = screenAdminUsers
		m.status = ""
		return m, nil
	case isRuneKey(msg, 't'):
		return m.openAdminEditTokens()
	case isRuneKey(msg, 'g'):
		return m.openAdminEditGrants()
	case isRuneKey(msg, 'p'):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user before changing a password."
			return m, nil
		}
		m.adminView.ResetPasswordForm = adminResetPasswordForm{}
		m.screen = screenAdminChangePassword
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'r'):
		m.status = "Loading admin users..."
		return m, m.loadAdminUsersCmd()
	case isRuneKey(msg, 'e', 'x'):
		user, ok := m.selectedAdminUser()
		if !ok {
			return m, nil
		}
		if isRuneKey(msg, 'e') && user.Enabled {
			return m, nil
		}
		if isRuneKey(msg, 'x') && !user.Enabled {
			return m, nil
		}
		kind := adminConfirmDisableUser
		verb := "disable"
		if isRuneKey(msg, 'e') {
			kind = adminConfirmEnableUser
			verb = "enable"
		}
		m.adminView.ConfirmModal = adminConfirmModal{
			Kind:        kind,
			Title:       fmt.Sprintf("Confirm %s", strings.Title(verb)),
			Message:     fmt.Sprintf("Confirm %s user %q?", verb, user.Username),
			ConfirmText: verb,
			UserID:      user.ID,
			Username:    user.Username,
		}
		m.status = ""
		return m, nil
	}
	return m, nil
}

func (m Model) updateAdminGrantsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenAdminEditUser
		m.status = ""
		return m, nil
	case isRuneKey(msg, 't'):
		return m.openAdminEditTokens()
	case isRuneKey(msg, 'r'):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user to manage grants."
			return m, nil
		}
		m.status = fmt.Sprintf("Loading grants for %s...", m.adminView.SelectedUsername)
		return m, m.loadAdminGrantsCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
	case isRuneKey(msg, 'n'):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user to manage grants."
			return m, nil
		}
		m.adminView.GrantForm = newAdminViewState().GrantForm
		m.screen = screenAdminAddGrant
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'e'):
		grant, ok := selectedGrantForView(m.adminView)
		if !ok {
			m.status = "No grant selected to edit."
			return m, nil
		}
		m.adminView.GrantForm.Repository = grant.Repository.String()
		m.adminView.GrantForm.Role = grant.Role
		m.adminView.GrantForm.Focus = adminGrantFieldRepository
		m.screen = screenAdminAddGrant
		m.status = ""
		return m, nil
	case isMoveUpKey(msg):
		m.adminView.SelectedGrant = boundedIndex(m.adminView.SelectedGrant-1, len(m.adminView.Grants))
		return m, nil
	case isMoveDownKey(msg):
		m.adminView.SelectedGrant = boundedIndex(m.adminView.SelectedGrant+1, len(m.adminView.Grants))
		return m, nil
	case isRuneKey(msg, 'x'):
		grant, ok := selectedGrantForView(m.adminView)
		if !ok {
			m.status = "No grant selected to remove."
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{
			Kind:        adminConfirmDeleteGrant,
			Title:       "Confirm Grant Removal",
			Message:     fmt.Sprintf("Remove grant %q from %q?", grant.Repository.String(), m.adminView.SelectedUsername),
			ConfirmText: "remove",
			UserID:      m.adminView.SelectedUserID,
			Username:    m.adminView.SelectedUsername,
			Repository:  grant.Repository.String(),
		}
		m.status = ""
		return m, nil
	}
	return m, nil
}

func (m Model) updateAdminTokensKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.clearRevealedAdminToken()
		m.screen = screenAdminEditUser
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'g'):
		m.clearRevealedAdminToken()
		return m.openAdminEditGrants()
	case isRuneKey(msg, 'n'):
		if m.adminView.SelectedUserID == "" {
			m.status = "Select a user to manage admin tokens."
			return m, nil
		}
		m.adminView.TokenForm = adminTokenForm{}
		m.screen = screenAdminCreateToken
		m.status = ""
		return m, nil
	case isMoveUpKey(msg):
		m.adminView.SelectedToken = boundedIndex(m.adminView.SelectedToken-1, len(m.adminView.AdminTokens))
		return m, nil
	case isMoveDownKey(msg):
		m.adminView.SelectedToken = boundedIndex(m.adminView.SelectedToken+1, len(m.adminView.AdminTokens))
		return m, nil
	case isRuneKey(msg, 'r'):
		if m.adminView.SelectedUserID == "" {
			m.status = "Select a user to manage admin tokens."
			return m, nil
		}
		m.clearRevealedAdminToken()
		m.status = fmt.Sprintf("Loading admin tokens for %s...", m.adminView.SelectedUsername)
		return m, m.loadAdminTokensCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
	case isRuneKey(msg, 'x'):
		token, ok := selectedTokenForView(m.adminView)
		if !ok {
			m.status = "No admin token selected to revoke."
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{
			Kind:        adminConfirmRevokeToken,
			Title:       "Confirm Token Revocation",
			Message:     fmt.Sprintf("Revoke admin token %q for %q?", token.Accessor, m.adminView.SelectedUsername),
			ConfirmText: "revoke",
			UserID:      m.adminView.SelectedUserID,
			Username:    m.adminView.SelectedUsername,
			Accessor:    token.Accessor,
		}
		m.status = ""
		return m, nil
	}
	return m, nil
}

func (m Model) updateAdminConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.ConfirmModal = adminConfirmModal{}
		m.status = ""
		return m, nil
	case isEnterKey(msg):
		modal := m.adminView.ConfirmModal
		switch modal.Kind {
		case adminConfirmEnableUser:
			m.status = fmt.Sprintf("Submitting enable for %s...", modal.Username)
			return m, m.enableDisableUserCmd(modal.UserID, true)
		case adminConfirmDisableUser:
			m.status = fmt.Sprintf("Submitting disable for %s...", modal.Username)
			return m, m.enableDisableUserCmd(modal.UserID, false)
		case adminConfirmDeleteGrant:
			m.status = fmt.Sprintf("Removing grant %q from %s...", modal.Repository, modal.Username)
			return m, m.deleteAdminGrantCmd(modal.UserID, modal.Username, modal.Repository)
		case adminConfirmRevokeToken:
			m.status = fmt.Sprintf("Revoking token %q for %s...", modal.Accessor, modal.Username)
			return m, m.revokeAdminTokenCmd(modal.UserID, modal.Username, modal.Accessor)
		}
	}
	return m, nil
}

func (m Model) updateCreateUserFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenAdminUsers
		return m, nil
	case isTabKey(msg):
		m.adminView.CreateUserForm.Focus = nextCreateUserField(m.adminView.CreateUserForm.Focus)
		return m, nil
	case isBackspaceKey(msg):
		m.deleteCreateUserRune()
		return m, nil
	case isEnterKey(msg):
		input := ports.AdminCreateUserInput{
			Username: strings.TrimSpace(m.adminView.CreateUserForm.Username),
			Password: m.adminView.CreateUserForm.Password,
			IsAdmin:  m.adminView.CreateUserForm.IsAdmin,
			Enabled:  m.adminView.CreateUserForm.Enabled,
		}
		if input.Username == "" || input.Password == "" {
			m.status = "Username and password are required."
			return m, nil
		}
		m.status = fmt.Sprintf("Creating user %s...", input.Username)
		return m, m.createAdminUserCmd(input)
	case isRuneKey(msg, ' '):
		m.toggleCreateUserField()
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.appendCreateUserRunes(string(msg.Runes))
		return m, nil
	}
	return m, nil
}

func (m Model) updateResetPasswordFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenAdminEditUser
		return m, nil
	case isBackspaceKey(msg):
		m.adminView.ResetPasswordForm.NewPassword = trimLastRune(m.adminView.ResetPasswordForm.NewPassword)
		return m, nil
	case isEnterKey(msg):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user before resetting a password."
			return m, nil
		}
		if m.adminView.ResetPasswordForm.NewPassword == "" {
			m.status = "New password is required."
			return m, nil
		}
		input := ports.AdminResetPasswordInput{UserID: m.adminView.SelectedUserID, NewPassword: m.adminView.ResetPasswordForm.NewPassword}
		m.status = fmt.Sprintf("Resetting password for %s...", m.adminView.SelectedUsername)
		return m, m.resetAdminPasswordCmd(input, m.adminView.SelectedUsername)
	}
	if msg.Type == tea.KeyRunes {
		m.adminView.ResetPasswordForm.NewPassword += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m Model) updateGrantFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenAdminEditUserGrants
		return m, nil
	case isTabKey(msg):
		if m.adminView.GrantForm.Focus == adminGrantFieldRepository {
			m.adminView.GrantForm.Focus = adminGrantFieldRole
		} else {
			m.adminView.GrantForm.Focus = adminGrantFieldRepository
		}
		return m, nil
	case isBackspaceKey(msg):
		if m.adminView.GrantForm.Focus == adminGrantFieldRepository {
			m.adminView.GrantForm.Repository = trimLastRune(m.adminView.GrantForm.Repository)
		}
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.GrantForm.Focus == adminGrantFieldRole {
			m.adminView.GrantForm.Role = nextGrantRole(m.adminView.GrantForm.Role)
		}
		return m, nil
	case isEnterKey(msg):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user to manage grants."
			return m, nil
		}
		repository := strings.TrimSpace(m.adminView.GrantForm.Repository)
		if repository == "" {
			m.status = "Repository is required."
			return m, nil
		}
		input := ports.AdminPutRepoGrantInput{UserID: m.adminView.SelectedUserID, Repository: repository, Role: m.adminView.GrantForm.Role}
		m.status = fmt.Sprintf("Saving grant for %s...", m.adminView.SelectedUsername)
		return m, m.putAdminGrantCmd(input, m.adminView.SelectedUsername)
	}
	if msg.Type == tea.KeyRunes && m.adminView.GrantForm.Focus == adminGrantFieldRepository {
		m.adminView.GrantForm.Repository += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m Model) updateTokenFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenAdminEditUserTokens
		return m, nil
	case isTabKey(msg):
		if m.adminView.TokenForm.Focus == adminTokenFieldName {
			m.adminView.TokenForm.Focus = adminTokenFieldTTL
		} else {
			m.adminView.TokenForm.Focus = adminTokenFieldName
		}
		return m, nil
	case isBackspaceKey(msg):
		if m.adminView.TokenForm.Focus == adminTokenFieldName {
			m.adminView.TokenForm.Name = trimLastRune(m.adminView.TokenForm.Name)
		} else {
			m.adminView.TokenForm.TTLSeconds = trimLastRune(m.adminView.TokenForm.TTLSeconds)
		}
		return m, nil
	case isEnterKey(msg):
		if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
			m.status = "Select a user to manage admin tokens."
			return m, nil
		}
		input := ports.AdminCreateTokenInput{UserID: m.adminView.SelectedUserID, Name: strings.TrimSpace(m.adminView.TokenForm.Name)}
		if ttl := strings.TrimSpace(m.adminView.TokenForm.TTLSeconds); ttl != "" {
			seconds, err := strconv.ParseInt(ttl, 10, 64)
			if err != nil {
				m.status = "TTL seconds must be a whole number."
				return m, nil
			}
			input.TTL = time.Duration(seconds) * time.Second
		}
		m.status = fmt.Sprintf("Creating admin token for %s...", m.adminView.SelectedUsername)
		return m, m.createAdminTokenCmd(input)
	}
	if msg.Type == tea.KeyRunes {
		if m.adminView.TokenForm.Focus == adminTokenFieldTTL {
			for _, r := range msg.Runes {
				if !unicode.IsDigit(r) {
					return m, nil
				}
			}
			m.adminView.TokenForm.TTLSeconds += string(msg.Runes)
			return m, nil
		}
		m.adminView.TokenForm.Name += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func isMoveUpKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyUp || msg.String() == "up" || isRuneKey(msg, 'k')
}

func isMoveDownKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyDown || msg.String() == "down" || isRuneKey(msg, 'j')
}

func isEnterKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyEnter || msg.String() == "enter"
}

func isTabKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyTab || msg.String() == "tab"
}

func isEscKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyEsc || msg.String() == "esc"
}

func isBackspaceKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyBackspace || msg.String() == "backspace"
}

func isBackKey(msg tea.KeyMsg) bool {
	return isEscKey(msg) || isBackspaceKey(msg)
}

func isRuneKey(msg tea.KeyMsg, candidates ...rune) bool {
	if len(msg.Runes) != 1 {
		return false
	}
	r := unicode.ToLower(msg.Runes[0])
	for _, candidate := range candidates {
		if r == unicode.ToLower(candidate) {
			return true
		}
	}
	return false
}

func (m *Model) moveSelection(delta int) {
	switch m.screen {
	case screenRepositories:
		m.repositories.Selected = boundedIndex(m.repositories.Selected+delta, len(m.repositories.Items))
	case screenTags:
		m.tags.Selected = boundedIndex(m.tags.Selected+delta, len(m.tags.Items))
	case screenBlobs:
		m.blobs.Selected = boundedIndex(m.blobs.Selected+delta, len(m.blobs.Items))
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
	filteredUsers := filteredAdminUsers(m.adminView)
	if len(filteredUsers) == 0 {
		return ports.AdminUser{}, false
	}
	index := boundedIndex(m.adminView.SelectedUser, len(filteredUsers))
	return filteredUsers[index], true
}

func filteredAdminUsers(view AdminViewState) []ports.AdminUser {
	query := strings.ToLower(strings.TrimSpace(view.UserSearchQuery))
	if query == "" {
		return view.Users
	}

	filteredUsers := make([]ports.AdminUser, 0, len(view.Users))
	for _, user := range view.Users {
		if strings.Contains(strings.ToLower(user.Username), query) {
			filteredUsers = append(filteredUsers, user)
		}
	}
	return filteredUsers
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

func (m Model) createAdminUserCmd(input ports.AdminCreateUserInput) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUserCreatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		user, err := m.adminClient.CreateUser(m.ctx, m.adminSession, input)
		return adminUserCreatedMsg{user: user, err: err}
	}
}

func (m Model) resetAdminPasswordCmd(input ports.AdminResetPasswordInput, username string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminPasswordResetMsg{userID: input.UserID, username: username, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		err := m.adminClient.ResetPassword(m.ctx, m.adminSession, input)
		return adminPasswordResetMsg{userID: input.UserID, username: username, err: err}
	}
}

func (m Model) putAdminGrantCmd(input ports.AdminPutRepoGrantInput, username string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminGrantMutatedMsg{userID: input.UserID, username: username, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		_, err := m.adminClient.PutUserGrant(m.ctx, m.adminSession, input)
		return adminGrantMutatedMsg{userID: input.UserID, username: username, err: err}
	}
}

func (m Model) deleteAdminGrantCmd(userID string, username string, repository string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminGrantMutatedMsg{userID: userID, username: username, repository: repository, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		err := m.adminClient.DeleteUserGrant(m.ctx, m.adminSession, userID, repository)
		return adminGrantMutatedMsg{userID: userID, username: username, repository: repository, err: err}
	}
}

func (m Model) createAdminTokenCmd(input ports.AdminCreateTokenInput) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminTokenCreatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		created, err := m.adminClient.CreateUserAdminToken(m.ctx, m.adminSession, input)
		return adminTokenCreatedMsg{created: created, err: err}
	}
}

func (m Model) revokeAdminTokenCmd(userID string, username string, accessor string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminTokenRevokedMsg{userID: userID, username: username, accessor: accessor, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		err := m.adminClient.RevokeUserAdminToken(m.ctx, m.adminSession, userID, accessor)
		return adminTokenRevokedMsg{userID: userID, username: username, accessor: accessor, err: err}
	}
}

func (m Model) enableDisableUserCmd(userID string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUserEnabledMsg{enabled: enabled, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		var (
			user ports.AdminUser
			err  error
		)
		if enabled {
			user, err = m.adminClient.EnableUser(m.ctx, m.adminSession, userID)
		} else {
			user, err = m.adminClient.DisableUser(m.ctx, m.adminSession, userID)
		}
		return adminUserEnabledMsg{user: user, enabled: enabled, err: err}
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
		m.syncAdminUserSelection(m.adminView.SelectedUserID)
		if m.adminView.SelectedUserID == "" {
			m.clearSelectedAdminDetails()
		}
		m.adminView.UserSearchActive = false
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
	m.screen = screenAdminLogin
	m.loadingText = ""
	m.status = "Logged out."
	return m
}

func (m Model) returnToInspection() Model {
	m.screen = m.adminReturn
	m.loadingText = ""
	m.status = ""
	m.adminView.ConfirmModal = adminConfirmModal{}
	m.adminView.UserSearchActive = false
	m.clearRevealedAdminToken()
	return m
}

func (m Model) expireAdminSession(reason string) Model {
	m.adminSession, m.adminView = ExpireAdminState(reason)
	m.adminAuth = adminAuthStateExpired
	m.adminLogin.Password = ""
	m.adminLogin.Focus = loginFieldUsername
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

func (m *Model) appendCreateUserRunes(value string) {
	if value == "" {
		return
	}
	switch m.adminView.CreateUserForm.Focus {
	case adminCreateUserFieldPassword:
		m.adminView.CreateUserForm.Password += value
	case adminCreateUserFieldUsername:
		m.adminView.CreateUserForm.Username += value
	}
}

func (m *Model) deleteCreateUserRune() {
	switch m.adminView.CreateUserForm.Focus {
	case adminCreateUserFieldPassword:
		m.adminView.CreateUserForm.Password = trimLastRune(m.adminView.CreateUserForm.Password)
	case adminCreateUserFieldUsername:
		m.adminView.CreateUserForm.Username = trimLastRune(m.adminView.CreateUserForm.Username)
	}
}

func (m *Model) toggleCreateUserField() {
	switch m.adminView.CreateUserForm.Focus {
	case adminCreateUserFieldIsAdmin:
		m.adminView.CreateUserForm.IsAdmin = !m.adminView.CreateUserForm.IsAdmin
	case adminCreateUserFieldEnabled:
		m.adminView.CreateUserForm.Enabled = !m.adminView.CreateUserForm.Enabled
	}
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

func nextCreateUserField(field adminCreateUserField) adminCreateUserField {
	if field >= adminCreateUserFieldEnabled {
		return adminCreateUserFieldUsername
	}
	return field + 1
}

func nextGrantRole(current domainauth.RepoRole) domainauth.RepoRole {
	switch current {
	case domainauth.RepoRoleWriter:
		return domainauth.RepoRoleAdmin
	case domainauth.RepoRoleAdmin:
		return domainauth.RepoRoleReader
	default:
		return domainauth.RepoRoleWriter
	}
}

func isAdminScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminAuthenticating, screenAdminUsers, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens, screenAdminCreateToken:
		return true
	default:
		return false
	}
}

func isAdminPrincipalScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminUsers, screenAdminCreateUser, screenAdminEditUser, screenAdminEditUserGrants, screenAdminEditUserTokens:
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

func formatRemaining(remaining time.Duration) string {
	if remaining <= 0 {
		return "0s"
	}
	return remaining.Truncate(time.Second).String()
}

func (m *Model) applyLoadedUsers(users []ports.AdminUser) {
	m.adminView.Users = append([]ports.AdminUser(nil), users...)
	if m.syncAdminUserSelection(m.adminView.SelectedUserID) || len(m.adminView.Users) == 0 {
		m.clearSelectedAdminDetails()
	}
}

func (m *Model) clearSelectedAdminDetails() {
	m.adminView.Grants = nil
	m.adminView.AdminTokens = nil
	m.adminView.SelectedGrant = 0
	m.adminView.SelectedToken = 0
	m.adminView.ResetPasswordForm = adminResetPasswordForm{}
	m.adminView.GrantForm = newAdminViewState().GrantForm
	m.adminView.TokenForm = adminTokenForm{}
	m.adminView.RevealedTokenSecret = ""
	m.adminView.RevealedTokenAccessor = ""
	m.adminView.RevealedTokenExpiresAt = time.Time{}
}

func (m *Model) clearRevealedAdminToken() {
	m.adminView.RevealedTokenSecret = ""
	m.adminView.RevealedTokenAccessor = ""
	m.adminView.RevealedTokenExpiresAt = time.Time{}
}

func (m *Model) syncAdminUserSelection(preferredUserID string) bool {
	previousUserID := m.adminView.SelectedUserID
	filteredUsers := filteredAdminUsers(m.adminView)
	if len(filteredUsers) == 0 {
		m.adminView.SelectedUser = 0
		m.adminView.SelectedUserID = ""
		m.adminView.SelectedUsername = ""
		return previousUserID != ""
	}

	selectedIndex := -1
	if preferredUserID != "" {
		for index, user := range filteredUsers {
			if user.ID == preferredUserID {
				selectedIndex = index
				break
			}
		}
	}
	if selectedIndex == -1 {
		selectedIndex = boundedIndex(m.adminView.SelectedUser, len(filteredUsers))
	}

	m.adminView.SelectedUser = selectedIndex
	m.adminView.SelectedUserID = filteredUsers[selectedIndex].ID
	m.adminView.SelectedUsername = filteredUsers[selectedIndex].Username
	return previousUserID != m.adminView.SelectedUserID
}

func (m *Model) reloadAdminPanelForSelectedUserChange(selectionChanged bool) tea.Cmd {
	if !selectionChanged || m.adminView.SelectedUserID == "" {
		return nil
	}

	switch m.screen {
	case screenAdminEditUserGrants, screenAdminAddGrant:
		m.status = fmt.Sprintf("Loading grants for %s...", m.adminView.SelectedUsername)
		return m.loadAdminGrantsCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
	case screenAdminEditUserTokens, screenAdminCreateToken:
		m.clearRevealedAdminToken()
		m.status = fmt.Sprintf("Loading admin tokens for %s...", m.adminView.SelectedUsername)
		return m.loadAdminTokensCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
	default:
		return nil
	}
}

func (m Model) openAdminEditUser() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
		m.status = "Select a user to edit."
		return m, nil
	}
	m.clearRevealedAdminToken()
	m.screen = screenAdminEditUser
	m.adminView.UserSearchActive = false
	m.status = ""
	return m, nil
}

func (m Model) openAdminEditGrants() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
		m.status = "Select a user to manage grants."
		return m, nil
	}
	m.clearRevealedAdminToken()
	m.screen = screenAdminEditUserGrants
	m.adminView.UserSearchActive = false
	m.status = fmt.Sprintf("Loading grants for %s...", m.adminView.SelectedUsername)
	return m, m.loadAdminGrantsCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
}

func (m Model) openAdminEditTokens() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.adminView.SelectedUserID) == "" {
		m.status = "Select a user to manage admin tokens."
		return m, nil
	}
	m.screen = screenAdminEditUserTokens
	m.adminView.UserSearchActive = false
	m.status = fmt.Sprintf("Loading admin tokens for %s...", m.adminView.SelectedUsername)
	return m, m.loadAdminTokensCmd(m.adminView.SelectedUserID, m.adminView.SelectedUsername)
}

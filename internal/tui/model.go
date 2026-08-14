package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	bubbletable "github.com/evertras/bubble-table/table"
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

func WithStartupLogin() Option {
	return func(m *Model) {
		m.startupLogin = true
	}
}

type QueryService interface {
	RepositorySummaries(ctx context.Context, limit int, after string) ([]appregixtry.RepositorySummary, error)
	TagDetails(ctx context.Context, repositoryName string, limit int, after string) ([]appregixtry.TagDetails, error)
	ResolveManifest(ctx context.Context, repositoryName string, reference string) (appregixtry.ManifestDetails, error)
	Uploads(ctx context.Context, repositoryName string) ([]appregixtry.UploadDetails, error)
	SignatureStatus(ctx context.Context, repositoryName string, reference string) (appregixtry.SignatureStatusResult, error)
}

type RepositoriesModel struct {
	Items    []appregixtry.RepositorySummary
	Selected int
	// Table is the Repositories screen's bubbletable.Model, baked from Items
	// at rebuildRepositoriesTable() time -- mirrors TagsModel.Table's own
	// baked-not-computed-in-View() pattern (design.md decision #6).
	Table bubbletable.Model
}

// Names extracts each repository's name, used by admin-side screens that
// only need name lookup/suggestions (grantRepositorySuggestions,
// renderAdminWorkspace, adminBaseBodyHeight), not the Tags/Last Pushed
// columns this screen's own table renders.
func (m RepositoriesModel) Names() []string {
	names := make([]string, 0, len(m.Items))
	for _, item := range m.Items {
		names = append(names, item.Name)
	}
	return names
}

type TagsModel struct {
	Repository string
	Items      []appregixtry.TagDetails
	Selected   int
	// Table is the Tags screen's bubbletable.Model, baked from Items at
	// rebuildTagsTable() time -- mirrors AdminViewState.Tables' own
	// baked-not-computed-in-View() pattern (design.md decision #6).
	Table bubbletable.Model
}

type ManifestModel struct {
	Details appregixtry.ManifestDetails
	// Signature is the SAME SignatureStatusResult shape the Console Tags
	// table's Signed column already resolves per tag, fetched fresh for
	// this reference (loadManifestCmd) rather than threaded through from
	// the Tags screen -- see loadManifestCmd's own comment for why.
	Signature appregixtry.SignatureStatusResult
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
	screenAdminFeatures       screen = "admin-features"
	screenAdminCreateUser     screen = "admin-create-user"
	screenAdminEditUser       screen = "admin-edit-user"
	screenAdminChangePassword screen = "admin-change-password"
	screenAdminEditUserGrants screen = "admin-edit-user-grants"
	screenAdminAddGrant       screen = "admin-add-grant"
	screenAdminEditUserTokens screen = "admin-edit-user-tokens"
	screenAdminCreateToken    screen = "admin-create-token"
	// screenRepoAdminGrants/screenRepoAdminAddGrant are the delegate-facing
	// entry point (design.md Decision 7): reached from the Console
	// Repositories screen, not from the global-admin user workspace, and
	// scoped to exactly one repository via adminIntent/adminIntentRepository.
	screenRepoAdminGrants   screen = "repo-admin-grants"
	screenRepoAdminAddGrant screen = "repo-admin-add-grant"
	// screenAdminRobots/screenAdminCreateRobot are global-admin-only
	// screens mirroring screenAdminUsers/screenAdminCreateUser (design.md
	// Decision 7), reached from screenAdminUsers via "b". Enable/disable
	// and token issuance reuse the existing user routes/screens through
	// SelectedUserID -- there is no separate robot edit screen.
	screenAdminRobots      screen = "admin-robots"
	screenAdminCreateRobot screen = "admin-create-robot"
)

// adminIntent is a one-shot field set before screenAdminLogin and consumed
// exactly once on successful auth (design.md Decision 7), so the shared
// login screen can route a repo-admin delegate straight to their own
// repository's grants instead of the global-admin screenAdminUsers.
type adminIntent string

const (
	adminIntentOperator   adminIntent = "operator"
	adminIntentRepoGrants adminIntent = "repo-grants"
)

type adminAuthState string

const (
	adminAuthStateUnauthenticated adminAuthState = "unauthenticated"
	adminAuthStateAuthenticating  adminAuthState = "authenticating"
	adminAuthStateAuthenticated   adminAuthState = "authenticated"
	adminAuthStateExpired         adminAuthState = "expired"
)

type loginField int

const trivyFeatureName = "trivy"

// gitleaksFeatureName mirrors internal/app/regixtry's own package-private
// constant of the same name and value, used by repositoryOverrideModal to
// tell the two feature codecs apart (design.md Decision 8).
const gitleaksFeatureName = "gitleaks"

// signingFeatureName mirrors internal/app/regixtry's own package-private
// constant of the same name and value (design.md Decision 11), used both by
// signingPolicyModal's own opener/badge and as the third value in
// repositoryOverrideModal's Feature cycle.
const signingFeatureName = "signing"

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
	// adminIntent/adminIntentRepository carry the one-shot repo-admin-grants
	// destination (design.md Decision 7) from the Console Repositories
	// screen's grant action through screenAdminLogin to the post-login
	// routing decision. Both are reset once consumed.
	adminIntent           adminIntent
	adminIntentRepository string

	empty    EmptyStateModel
	mutation MutationUnavailableModel
	status   string

	showMutationNotice bool
	lastRepository     string
	lastTag            string
	startupLogin       bool
	pendingAdminStatus string

	// viewport is the terminal size captured from the most recent
	// tea.WindowSizeMsg (or the default from NewModel before any resize
	// event, e.g. under the --snapshot CLI path — design.md decision #8).
	// bodyScroll is the current scroll offset applied by fitLines() when
	// rendering a screen's bounded section (wired in Phase 3).
	viewport   viewportSize
	bodyScroll int
}

// viewportSize holds the raw terminal dimensions captured from
// tea.WindowSizeMsg. contentBudget() derives the per-screen consoleLayout
// budget from it (see viewport.go).
type viewportSize struct {
	Width  int
	Height int
}

// contentBudget wraps the package-level pure contentBudget() (viewport.go)
// with this Model's captured viewport, the status/help text the calling
// screen actually renders, and this Model's current bodyScroll.
//
// Phase 3 deviation from design.md's zero-arg method: several screens
// render a status other than m.status (e.g. m.notice) plus their own help
// string. Measuring "" instead would under-count chrome and let the row
// budget exceed the terminal height once content is clipped.
func (m Model) contentBudget(status, help string) consoleLayout {
	layout := contentBudget(m.viewport.Width, m.viewport.Height, status, help)
	layout.Scroll = m.bodyScroll
	return layout
}

// adminTablesLayout computes the consoleLayout used to size admin tables at
// rebuild time (design.md decision #6). It uses the Features screen's help
// text since that is the only admin screen that renders tables today —
// callers of rebuildAdminTables run from Update handlers, not View(), so no
// screen-specific help string is otherwise available.
func (m Model) adminTablesLayout() consoleLayout {
	return m.contentBudget(m.status, adminFeatureHelp(m.adminView))
}

// tagsTableLayout computes the consoleLayout used to size the Tags table at
// rebuild time, mirroring adminTablesLayout()'s own use of the exact
// status/help this screen renders (scrollableBodyContext) so the layout used
// to build the table can never drift from what View() actually shows.
func (m Model) tagsTableLayout() consoleLayout {
	status, help, _, _ := m.scrollableBodyContext()
	return m.contentBudget(status, help)
}

// rebuildTagsTable bakes m.tags.Items into a fresh bubbletable.Model sized
// by layout (consoleTagsTablePageSize), mirroring rebuildAdminTables' own
// bake-at-mutation-time pattern rather than computing the table in View().
func (m *Model) rebuildTagsTable(layout consoleLayout) {
	theme := newAdminTheme()
	m.tags.Table = buildConsoleTagsTable(theme, m.tags.Items, m.tags.Selected, consoleTagsTablePageSize(layout))
}

// repositoriesTableLayout computes the consoleLayout used to size the
// Repositories table at rebuild time -- mirrors tagsTableLayout's own use of
// the exact status/help this screen renders (scrollableBodyContext) so the
// layout used to build the table can never drift from what View() actually
// shows.
func (m Model) repositoriesTableLayout() consoleLayout {
	status, help, _, _ := m.scrollableBodyContext()
	return m.contentBudget(status, help)
}

// rebuildRepositoriesTable bakes m.repositories.Items into a fresh
// bubbletable.Model sized by layout (consoleRepositoriesTablePageSize),
// mirroring rebuildTagsTable's own bake-at-mutation-time pattern one level
// up.
func (m *Model) rebuildRepositoriesTable(layout consoleLayout) {
	theme := newAdminTheme()
	m.repositories.Table = buildConsoleRepositoriesTable(theme, m.repositories.Items, m.repositories.Selected, consoleRepositoriesTablePageSize(layout))
}

type catalogLoadedMsg struct {
	result []appregixtry.RepositorySummary
	err    error
}

type tagsLoadedMsg struct {
	repository string
	result     []appregixtry.TagDetails
	err        error
}

type manifestLoadedMsg struct {
	repository string
	tag        string
	manifest   appregixtry.ManifestDetails
	uploads    []appregixtry.UploadDetails
	signature  appregixtry.SignatureStatusResult
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

type adminFeaturesLoadedMsg struct {
	features []ports.FeatureSummary
	err      error
}

type adminFeaturePageLoadedMsg struct {
	page ports.FeaturePage
	err  error
}

type adminFeatureActionCompletedMsg struct {
	result ports.FeatureActionResult
	err    error
}

type adminFeatureConfiguredMsg struct {
	name string
	err  error
}

type adminScanPolicyLoadedMsg struct {
	settings ports.ScanPolicySettings
	err      error
}

type adminScanPolicyUpdatedMsg struct {
	settings ports.ScanPolicySettings
	err      error
}

type adminSigningPolicyLoadedMsg struct {
	settings ports.SigningPolicySettings
	err      error
}

type adminSigningPolicyUpdatedMsg struct {
	settings ports.SigningPolicySettings
	err      error
}

// adminRepositoryOverrideLoadedMsg carries the result of opening
// repositoryOverrideModal (design.md Decision 8 piece 2), echoing the
// queried repository/feature so a stale response for a modal the operator
// has since closed or switched away from can be discarded, mirroring
// adminScanHistoryLoadedMsg's own staleness guard.
type adminRepositoryOverrideLoadedMsg struct {
	repository string
	feature    string
	override   ports.RepositoryOverrideDetails
	exists     bool
	err        error
}

// adminRepositoryOverrideSavedMsg carries the result of a set (submit) or
// clear action on repositoryOverrideModal. exists distinguishes the two:
// true after a successful set, false after a successful clear.
type adminRepositoryOverrideSavedMsg struct {
	repository string
	feature    string
	override   ports.RepositoryOverrideDetails
	exists     bool
	err        error
}

// adminRepositoryOverridesListLoadedMsg carries every stored override row
// for one feature (design.md Decision 7's "List (TUI annotation)" row),
// fetched alongside TrivyScanRuns to annotate the Repository Alerts table
// with a distinct "scanning disabled" state.
type adminRepositoryOverridesListLoadedMsg struct {
	feature   string
	overrides []ports.RepositoryOverrideDetails
	err       error
}

type adminFeatureRuntimeMutatedMsg struct {
	name   string
	action string
	state  ports.FeatureRuntimeState
	err    error
}

type adminScanRunsLoadedMsg struct {
	runs []ports.ScanRun
	err  error
}

type adminScanRunDetailLoadedMsg struct {
	detail ports.ScanRunDetail
	err    error
}

type adminSecretScanFindingsLoadedMsg struct {
	repository string
	digest     string
	findings   []ports.SecretFinding
	err        error
}

// adminScanHistoryLoadedMsg carries a repository's chronological scan
// history (design.md "Chronological history by client-side re-sort"),
// echoing the queried repository so a stale response for a repository the
// operator has since closed/switched away from can be discarded.
type adminScanHistoryLoadedMsg struct {
	repository string
	runs       []ports.ScanRun
	err        error
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

// adminRepoGrantsLoadedMsg carries one repository's grants for the
// repo-admin delegate's own grants screen (design.md Decision 7), a sibling
// of adminUserGrantsLoadedMsg keyed by repository instead of userID.
type adminRepoGrantsLoadedMsg struct {
	repository string
	grants     []ports.AdminRepositoryGrant
	err        error
}

// adminRepoGrantMutatedMsg carries the outcome of a put or delete on the
// repo-admin delegate's grants screen. deleted distinguishes the two (unlike
// adminGrantMutatedMsg's repository-emptiness convention above) because
// username is always present here.
type adminRepoGrantMutatedMsg struct {
	repository string
	username   string
	deleted    bool
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

type adminRobotsLoadedMsg struct {
	robots []ports.AdminRobot
	err    error
}

type adminRobotCreatedMsg struct {
	created ports.AdminCreatedRobot
	err     error
}

// adminRobotEnabledMsg is a sibling of adminUserEnabledMsg carrying only the
// robot's ID, not the full ports.AdminUser: after a mutation the screen
// reloads screenAdminRobots' own ports.AdminRobot-shaped list rather than
// reusing the enable/disable response body.
type adminRobotEnabledMsg struct {
	robotID string
	enabled bool
	err     error
}

type startupLoginMsg struct{}

func NewModel(service QueryService, options ...Option) Model {
	m := Model{
		ctx:         context.Background(),
		service:     service,
		screen:      screenLoading,
		loadingText: "Loading repositories...",
		now:         func() time.Time { return time.Now().UTC() },
		// Default terminal size (design.md decision #8): overwritten by the
		// first real tea.WindowSizeMsg; stands as-is under the --snapshot
		// CLI path, which never runs the Bubble Tea program loop and so
		// never receives a resize event.
		viewport:    viewportSize{Width: defaultViewportWidth, Height: defaultViewportHeight},
		adminAuth:   adminAuthStateUnauthenticated,
		adminReturn: screenLoading,
		adminView:   newAdminViewState(),
		adminIntent: adminIntentOperator,
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
	if m.startupLogin {
		return func() tea.Msg { return startupLoginMsg{} }
	}
	return m.loadCatalogCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport = viewportSize{Width: msg.Width, Height: msg.Height}
		// Plain-list/text sections re-fit for free: View() recomputes
		// contentBudget() and renderSection() on every render (Phase 3).
		// Admin tables do not — their pageSize is baked into the
		// bubbletable.Model at rebuildAdminTables() time, so a resize must
		// explicitly rebuild them here too (Req: Live Terminal Resize
		// Refit). Safe/cheap when no admin tables are loaded yet: builds
		// harmlessly from empty adminView state.
		m.rebuildAdminTables(m.adminTablesLayout())
		// The Tags table needs the same explicit resize-rebuild for the same
		// reason (its pageSize is baked in at rebuildTagsTable() time, not
		// recomputed by View()); harmless/cheap when no tags are loaded yet.
		m.rebuildTagsTable(m.tagsTableLayout())
		// The Repositories table needs the same explicit resize-rebuild, for
		// the same reason.
		m.rebuildRepositoriesTable(m.repositoriesTableLayout())
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case catalogLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}
		m.repositories = RepositoriesModel{Items: append([]appregixtry.RepositorySummary(nil), msg.result...)}
		m.bodyScroll = 0
		if len(m.repositories.Items) == 0 {
			m.screen = screenEmpty
			return m, nil
		}
		m.screen = screenRepositories
		m.rebuildRepositoriesTable(m.repositoriesTableLayout())
		m.loadingText = ""
		return m, nil
	case startupLoginMsg:
		m.screen = screenAdminLogin
		m.loadingText = ""
		m.adminReturn = screenRepositories
		return m, nil
	case tagsLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}
		m.tags = TagsModel{Repository: msg.repository, Items: append([]appregixtry.TagDetails(nil), msg.result...)}
		m.lastRepository = msg.repository
		m.showMutationNotice = false
		m.status = ""
		m.bodyScroll = 0
		if len(m.tags.Items) == 0 {
			m.screen = screenEmpty
			m.empty = EmptyStateModel{Title: "Repository has no tags", Message: fmt.Sprintf("%s has no published tags yet.", msg.repository)}
			return m, nil
		}
		m.screen = screenTags
		m.rebuildTagsTable(m.tagsTableLayout())
		return m, nil
	case manifestLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}
		m.lastRepository = msg.repository
		m.lastTag = msg.tag
		m.manifest = ManifestModel{Details: msg.manifest, Signature: msg.signature}
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
		if m.adminIntent == adminIntentRepoGrants {
			// Consumed exactly once (design.md Decision 7): a repo-admin
			// delegate's successful login routes straight to their own
			// repository's grants instead of the global-admin users screen.
			repository := m.adminIntentRepository
			m.adminIntent = adminIntentOperator
			m.adminIntentRepository = ""
			m.adminView.RepoAdminRepository = repository
			m.adminView.RepoAdminGrantsAuthorized = false
			m.screen = screenRepoAdminGrants
			m.status = fmt.Sprintf("Loading grants for %s...", repository)
			return m, m.loadRepoAdminGrantsCmd(repository)
		}
		if m.startupLogin && len(m.repositories.Items) == 0 {
			m.status = ""
			m.screen = screenLoading
			m.loadingText = "Loading repositories..."
			m.startupLogin = false
			return m, m.loadCatalogCmd()
		}
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
	case adminFeaturesLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.applyLoadedFeatures(msg.features)
		m.rebuildAdminTables(m.adminTablesLayout())
		if len(m.adminView.Features) == 0 {
			m.status = "No built-in features found."
			return m, nil
		}
		m.status = fmt.Sprintf("Loading feature page for %s...", m.selectedFeatureName())
		return m, m.loadAdminFeaturePageCmd(m.selectedFeatureName())
	case adminFeaturePageLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.applyFeaturePage(msg.page)
		m.rebuildAdminTables(m.adminTablesLayout())
		if strings.TrimSpace(m.pendingAdminStatus) != "" {
			m.status = m.pendingAdminStatus
			m.pendingAdminStatus = ""
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		if msg.page.Summary.Name == trivyFeatureName {
			// The policy badge (renderTrivyTabs) needs ScanPolicy loaded
			// before it can render a real state; chained as a follow-up Cmd,
			// matching this Update loop's existing single-Cmd-return style.
			return m, m.loadScanPolicyCmd()
		}
		if msg.page.Summary.Name == signingFeatureName {
			// The signing badge (composed onto the Feature Page heading)
			// needs SigningPolicy loaded before it can render a real state,
			// mirroring the Trivy policy badge's own follow-up Cmd above
			// (design.md Decision 11 piece 1).
			return m, m.loadSigningPolicyCmd()
		}
		return m, nil
	case adminFeatureActionCompletedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{}
		m.pendingAdminStatus = strings.TrimSpace(msg.result.Message)
		m.status = "Loading built-in features..."
		m.screen = screenAdminFeatures
		return m, m.loadAdminFeaturesCmd()
	case adminFeatureConfiguredMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.TrivyConfigModal = trivyConfigModal{}
		m.adminView.GitleaksConfigModal = gitleaksConfigModal{}
		m.adminView.TrivyTab = trivyTabRuntime
		m.pendingAdminStatus = "Configuration saved."
		m.status = "Loading built-in features..."
		m.screen = screenAdminFeatures
		return m, m.loadAdminFeaturesCmd()
	case adminScanPolicyLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			// Best-effort, matching the badge/modal's fail-quiet posture:
			// leave ScanPolicy at its zero value rather than surfacing a
			// blocking status error over an otherwise-successful feature
			// page load.
			return m, nil
		}
		m.adminView.ScanPolicy = msg.settings
		return m, nil
	case adminScanPolicyUpdatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminView.ScanPolicyModal.Error = msg.err.Error()
			return m, nil
		}
		m.adminView.ScanPolicy = msg.settings
		m.adminView.ScanPolicyModal = scanPolicyModal{}
		m.status = "Vulnerability policy saved."
		return m, nil
	case adminSigningPolicyLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			// Best-effort, matching the ScanPolicy load's fail-quiet posture:
			// leave SigningPolicy at its zero value rather than surfacing a
			// blocking status error over an otherwise-successful feature
			// page load.
			return m, nil
		}
		m.adminView.SigningPolicy = msg.settings
		return m, nil
	case adminSigningPolicyUpdatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminView.SigningPolicyModal.Error = msg.err.Error()
			return m, nil
		}
		// Unlike scanPolicyModal, the modal stays open after a successful
		// save (design.md Decision 11 piece 1's growable key list): the
		// operator can keep adding keys, mirroring
		// applyRepositoryOverrideToModal's own "stays open" precedent.
		m.adminView.SigningPolicy = msg.settings
		m.adminView.SigningPolicyModal.Enabled = msg.settings.Enabled
		m.adminView.SigningPolicyModal.Fingerprints = signingKeyFingerprints(msg.settings.TrustedPublicKeys)
		m.adminView.SigningPolicyModal.AddKey = ""
		m.adminView.SigningPolicyModal.Error = ""
		m.status = "Signing policy saved."
		return m, nil
	case adminRepositoryOverrideLoadedMsg:
		if !m.adminView.RepositoryOverrideModal.Active() || msg.repository != m.adminView.RepositoryOverrideModal.Repository || msg.feature != m.adminView.RepositoryOverrideModal.Feature {
			// Stale response for a modal the operator has since closed or
			// switched away from.
			return m, nil
		}
		m.adminView.RepositoryOverrideModal.Loading = false
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminView.RepositoryOverrideModal.Error = msg.err.Error()
			return m, nil
		}
		m.applyRepositoryOverrideToModal(msg.override, msg.exists)
		return m, nil
	case adminRepositoryOverrideSavedMsg:
		if !m.adminView.RepositoryOverrideModal.Active() || msg.repository != m.adminView.RepositoryOverrideModal.Repository || msg.feature != m.adminView.RepositoryOverrideModal.Feature {
			return m, nil
		}
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminView.RepositoryOverrideModal.Error = msg.err.Error()
			return m, nil
		}
		m.adminView.RepositoryOverrideModal.Error = ""
		m.applyRepositoryOverrideToModal(msg.override, msg.exists)
		if msg.exists {
			m.status = "Repository override saved."
		} else {
			m.status = "Repository override cleared."
		}
		return m, nil
	case adminRepositoryOverridesListLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			// Best-effort, matching the ScanPolicy load's fail-quiet posture:
			// the Repository Alerts table just renders without the
			// "scanning disabled" annotation rather than surfacing a
			// blocking status error over an otherwise-successful alerts
			// load.
			return m, nil
		}
		if msg.feature == trivyFeatureName {
			m.adminView.TrivyOverrides = msg.overrides
			m.rebuildAdminTables(m.adminTablesLayout())
		}
		return m, nil
	case adminFeatureRuntimeMutatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.pendingAdminStatus = fmt.Sprintf("Managed runtime %s for %q at %s.", msg.action, msg.name, adminFirstNonEmpty(strings.TrimSpace(msg.state.ActiveVersion), adminFirstNonEmpty(strings.TrimSpace(string(msg.state.Status)), "unknown")))
		m.status = "Loading built-in features..."
		m.screen = screenAdminFeatures
		return m, m.loadAdminFeaturesCmd()
	case adminScanRunsLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		runs := append([]ports.ScanRun(nil), msg.runs...)
		sort.SliceStable(runs, func(i, j int) bool { return compareScanRuns(runs[i], runs[j]) < 0 })
		m.adminView.TrivyScanRuns = runs
		// TrivySummaries aggregates the same severity/fixability-ordered runs
		// into one row per repository (spec.md "Repository Alerts
		// Summarized Per Repository With Ordering And Freshness") —
		// summarizeScanRunsByRepository preserves first-seen order, so
		// feeding it the already-sorted runs keeps the summary
		// severity-ordered too.
		m.adminView.TrivySummaries = summarizeScanRunsByRepository(runs)
		m.adminView.TrivySelectedAlert = boundedIndex(0, len(m.adminView.TrivySummaries))
		m.adminView.TrivyAlertsLoaded = true
		m.rebuildAdminTables(m.adminTablesLayout())
		if len(m.adminView.TrivyScanRuns) == 0 {
			m.status = "No repository alerts found."
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		// The disabled-row annotation (buildAdminScanSummaryTable via
		// annotateDisabledSummaries) needs the override list loaded before it
		// can render; chained as a follow-up Cmd (not tea.Batch), matching
		// this Update loop's existing single-Cmd-return style.
		return m, m.loadRepositoryOverridesListCmd(trivyFeatureName)
	case adminScanRunDetailLoadedMsg:
		// loadAdminScanRunDetailCmd is only ever fired while the scan
		// history modal is active (Enter opens it, pageAdminScanHistory
		// re-fires it) — a response arriving after the operator has since
		// closed the modal (Esc) is stale and discarded.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			if m.adminView.ScanHistoryModal.Active() {
				m.adminView.ScanHistoryModal.Loading = false
				m.adminView.ScanHistoryModal.Error = msg.err.Error()
				m.rebuildAdminTables(m.adminTablesLayout())
			}
			return m, nil
		}
		// Digest-per-run correctness (design.md risk note): discard a stale
		// response for a run the operator has since paged away from, so the
		// Vulnerabilities/Leaks tabs never show a mix of two executions.
		if !m.adminView.ScanHistoryModal.Active() || !adminScanHistoryDetailMatchesCursor(m.adminView.ScanHistoryModal, msg.detail) {
			return m, nil
		}
		m.adminView.ScanHistoryModal.Detail = msg.detail
		m.adminView.ScanHistoryModal.Secrets = nil
		m.adminView.ScanHistoryModal.Error = ""
		m.rebuildAdminTables(m.adminTablesLayout())
		m.status = ""
		// Secret findings are surfaced alongside the vulnerability scan
		// detail just loaded above (spec.md "Operator reviews findings for
		// a selected image"), keyed by the same repository+digest both scan
		// legs share. Chained as a follow-up Cmd (not tea.Batch) so it
		// composes with this Update loop's existing single-Cmd-return style.
		return m, m.loadAdminSecretScanFindingsCmd(msg.detail.Run.Repository, msg.detail.Run.Digest)
	case adminSecretScanFindingsLoadedMsg:
		// Informational only (spec.md "Informational Findings Only"): a
		// failure to load secret findings (including "none persisted yet")
		// must never override the vulnerability detail already shown or
		// surface as a blocking error — it just renders as the clear empty
		// state below.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			if !m.adminView.ScanHistoryModal.Active() || !adminScanHistorySecretsMatchCursor(m.adminView.ScanHistoryModal, msg.repository, msg.digest) {
				return m, nil
			}
			m.adminView.ScanHistoryModal.Secrets = nil
			m.adminView.ScanHistoryModal.Loading = false
			m.rebuildAdminTables(m.adminTablesLayout())
			return m, nil
		}
		if !m.adminView.ScanHistoryModal.Active() || !adminScanHistorySecretsMatchCursor(m.adminView.ScanHistoryModal, msg.repository, msg.digest) {
			return m, nil
		}
		m.adminView.ScanHistoryModal.Secrets = msg.findings
		m.adminView.ScanHistoryModal.Loading = false
		m.rebuildAdminTables(m.adminTablesLayout())
		return m, nil
	case adminOpenURLCompletedMsg:
		// openSelectedAdminFindingLink's side effect (opening a finding's
		// advisory link) already ran inside its Cmd; only the resulting
		// transient status update happens here in Update, never the exec
		// itself (Bubble Tea convention).
		//
		// Any failure (not just a missing opener binary) falls back to
		// showing the URL itself rather than the raw Go exec error --
		// meaningless to a TUI operator on a headless server with no GUI
		// opener (e.g. `exec: "xdg-open": executable file not found in
		// $PATH`) -- so the operator can select/copy it from the terminal.
		if msg.err != nil {
			m.status = fmt.Sprintf("Could not open automatically — copy this link: %s", msg.url)
			return m, nil
		}
		m.status = ""
		return m, nil
	case adminScanHistoryLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			if m.adminView.ScanHistoryModal.Active() && msg.repository == m.adminView.ScanHistoryModal.Repository {
				m.adminView.ScanHistoryModal.Loading = false
				m.adminView.ScanHistoryModal.Error = msg.err.Error()
				m.rebuildAdminTables(m.adminTablesLayout())
			}
			return m, nil
		}
		if !m.adminView.ScanHistoryModal.Active() || msg.repository != m.adminView.ScanHistoryModal.Repository {
			// Stale response for a modal that has since closed or switched
			// to a different repository.
			return m, nil
		}
		m.adminView.ScanHistoryModal.Runs = msg.runs
		m.adminView.ScanHistoryModal.Cursor = 0
		m.adminView.ScanHistoryModal.Error = ""
		if len(msg.runs) == 0 {
			m.adminView.ScanHistoryModal.Loading = false
			m.rebuildAdminTables(m.adminTablesLayout())
			return m, nil
		}
		m.status = ""
		return m, m.loadAdminScanRunDetailCmd(msg.runs[0].ID)
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
	case adminRepoGrantsLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.adminView.RepoAdminGrantsAuthorized = false
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.RepoAdminRepository = msg.repository
		m.adminView.RepoAdminGrants = msg.grants
		m.adminView.RepoAdminGrantsAuthorized = true
		m.adminView.SelectedRepoAdminGrant = 0
		if len(msg.grants) == 0 {
			m.status = fmt.Sprintf("No grants for %q.", msg.repository)
		} else {
			m.status = ""
		}
		return m, nil
	case adminRepoGrantMutatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.RepoAdminGrantForm = adminRepositoryGrantForm{Role: domainauth.RepoRoleReader}
		m.adminView.ConfirmModal = adminConfirmModal{}
		if msg.deleted {
			m.status = fmt.Sprintf("Grant removed for %q. Refreshing grants...", msg.username)
		} else {
			m.status = fmt.Sprintf("Grant saved for %q. Refreshing grants...", msg.username)
		}
		m.screen = screenRepoAdminGrants
		return m, m.loadRepoAdminGrantsCmd(msg.repository)
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
	case adminRobotsLoadedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.Robots = append([]ports.AdminRobot(nil), msg.robots...)
		m.adminView.SelectedRobot = boundedIndex(m.adminView.SelectedRobot, len(m.adminView.Robots))
		if len(m.adminView.Robots) == 0 {
			m.status = "No robot accounts found."
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		return m, nil
	case adminRobotCreatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.CreateRobotForm = newAdminViewState().CreateRobotForm
		// The one-time secret (spec.md "Operator manages a robot account
		// end to end") is shown exactly once, here, on screenAdminCreateRobot
		// -- reusing RevealedTokenSecret/Accessor, the exact fields
		// renderAdminTokensScreen already reveals once for human admin
		// tokens (design.md Decision 6). The screen does NOT navigate away,
		// so the reveal survives this single render; every navigation away
		// from screenAdminCreateRobot (Esc) and every navigation into the
		// reused token screen (openAdminRobotTokens's "t") clears these
		// fields first, so the secret can never render a second time.
		m.adminView.RevealedTokenSecret = msg.created.Secret
		m.adminView.RevealedTokenAccessor = msg.created.Accessor
		m.adminView.RevealedTokenExpiresAt = msg.created.ExpiresAt
		m.status = fmt.Sprintf("Robot %q created.", msg.created.Robot.Username)
		m.screen = screenAdminCreateRobot
		return m, m.loadAdminRobotsCmd()
	case adminRobotEnabledMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{}
		verb := "disabled"
		if msg.enabled {
			verb = "enabled"
		}
		m.status = fmt.Sprintf("Robot %s. Refreshing robots...", verb)
		m.screen = screenAdminRobots
		return m, m.loadAdminRobotsCmd()
	}

	return m, nil
}

// Too-small guard message text per design.md's "Interfaces / Contracts"
// section: plain (not theme.section, which is a fixed 88-wide box).
const terminalTooSmallTemplate = "Terminal too small\nRegixtry needs at least %dx%d. Current: %dx%d.\nResize, or press q to quit."

func (m Model) View() string {
	// Guarded here (design decision #7) rather than in Update: View() is the
	// single funnel both the interactive program loop and the --snapshot
	// CLI path render through.
	if m.viewport.Width < minViewportWidth || m.viewport.Height < minViewportHeight {
		return fmt.Sprintf(terminalTooSmallTemplate, minViewportWidth, minViewportHeight, m.viewport.Width, m.viewport.Height)
	}

	switch m.screen {
	case screenLoading:
		help := "q: quit"
		layout := m.contentBudget("", help)
		return renderInspectionWorkspace("Loading", renderConsoleTextSection(m.loadingText, layout), "", help)
	case screenRepositories:
		status, help, _, _ := m.scrollableBodyContext()
		layout := m.contentBudget(status, help)
		return renderInspectionWorkspace(
			"Repositories",
			renderConsoleRepositoriesSection(m.repositories, layout),
			status,
			help,
		)
	case screenTags:
		status, help, _, _ := m.scrollableBodyContext()
		layout := m.contentBudget(status, help)
		return renderInspectionWorkspace(
			fmt.Sprintf("Repositories / %s / Tags", m.tags.Repository),
			renderConsoleTagsSection(m.tags, layout),
			status,
			help,
		)
	case screenManifest:
		status := ""
		if m.showMutationNotice {
			status = fmt.Sprintf("Delete unavailable in v1: %s", m.mutation.Reason)
		}
		help := "b: blobs | u: uploads | d: unsupported delete | Tab: admin | Esc: back | q: quit"
		layout := m.contentBudget(status, help)
		return renderInspectionWorkspace(
			fmt.Sprintf("Repositories / %s / %s / Manifest", m.manifest.Details.Repository, m.manifest.Details.Reference),
			renderConsoleTextSection(renderManifest(newAdminTheme(), m.manifest.Details, m.manifest.Signature), layout),
			status,
			help,
		)
	case screenBlobs:
		status := ""
		if m.showMutationNotice {
			status = fmt.Sprintf("Delete unavailable in v1: %s", m.mutation.Reason)
		}
		help := "Tab: admin | Esc: back | q: quit"
		layout := m.contentBudget(status, help)
		return renderInspectionWorkspace(
			fmt.Sprintf("Repositories / %s / %s / Blobs", m.lastRepository, m.lastTag),
			renderConsoleTextSection(renderBlobs(newAdminTheme(), m.blobs), layout),
			status,
			help,
		)
	case screenUploads:
		status := ""
		if m.showMutationNotice {
			status = fmt.Sprintf("Delete unavailable in v1: %s", m.mutation.Reason)
		}
		help := "Tab: admin | Esc: back | q: quit"
		layout := m.contentBudget(status, help)
		return renderInspectionWorkspace(
			fmt.Sprintf("Repositories / %s / %s / Uploads", m.lastRepository, m.lastTag),
			renderConsoleTextSection(renderUploads(newAdminTheme(), m.uploads), layout),
			status,
			help,
		)
	case screenEmpty:
		help := "Tab: admin | Esc: back | q: quit"
		layout := m.contentBudget("", help)
		return renderInspectionWorkspace(
			"Empty",
			renderConsoleTextSection(strings.Join([]string{m.empty.Title, "", m.empty.Message}, "\n"), layout),
			"",
			help,
		)
	case screenError:
		errText := "Unknown error"
		if m.err != nil {
			errText = m.err.Error()
		}
		help := "q: quit"
		layout := m.contentBudget(errText, help)
		theme := newAdminTheme()
		// Bypasses renderInspectionWorkspace (which defaults to
		// statusKindAuto) and calls renderConsoleWorkspace directly with an
		// explicit statusKindError: a fatal error must always render with
		// theme.error, regardless of whether its message happens to contain
		// "expired"/"invalid"/"error" (design.md Decision 2, spec.md "Fatal
		// error renders in error styling regardless of wording").
		return renderConsoleWorkspace("Regixtry Console", "Error",
			renderConsoleTextSection(theme.error.Render(errText), layout),
			errText, help, statusKindError)
	case screenAdminLogin:
		return renderInspectionWorkspace("Sign In", renderAdminLogin(newAdminTheme(), m.adminLogin), m.status, "Enter: sign in | Tab: switch field | Esc: back | q: quit")
	case screenAdminAuthenticating:
		help := "q: quit"
		layout := m.contentBudget("", help)
		return renderInspectionWorkspace("Sign In", renderConsoleTextSection(m.loadingText, layout), "", help)
	case screenAdminUsers, screenAdminFeatures, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens, screenAdminCreateToken, screenRepoAdminGrants, screenRepoAdminAddGrant, screenAdminRobots, screenAdminCreateRobot:
		layout := m.contentBudget(m.status, adminScreenHelp(m.screen, m.adminView))
		return renderAdminWorkspace(m.screen, m.adminSession, m.adminView, m.repositories.Names(), m.status, layout, m.now())
	}

	help := "q: quit"
	return renderInspectionWorkspace("Regixtry", renderConsoleTextSection("Ready.", m.contentBudget("", help)), "", help)
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
	case isPgUpKey(msg), isPgDnKey(msg), isHomeKey(msg), isEndKey(msg):
		if m.applyBodyPageKey(msg) {
			return m, nil
		}
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
		m.bodyScroll = 0
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
	case isRuneKey(msg, 'g'):
		if m.screen == screenRepositories {
			return m.openRepoAdminGrants()
		}
		return m, nil
	}

	return m, nil
}

func (m Model) updateAdminKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.adminView.TrivyConfigModal.Active() {
		return m.updateTrivyConfigModalKey(msg)
	}

	if m.adminView.GitleaksConfigModal.Active() {
		return m.updateGitleaksConfigModalKey(msg)
	}

	if m.adminView.ScanPolicyModal.Active() {
		return m.updateScanPolicyModalKey(msg)
	}

	if m.adminView.SigningPolicyModal.Active() {
		return m.updateSigningPolicyModalKey(msg)
	}

	if m.adminView.RepositoryOverrideModal.Active() {
		return m.updateRepositoryOverrideModalKey(msg)
	}

	if m.adminView.ScanHistoryModal.Active() {
		return m.updateAdminScanHistoryModalKey(msg)
	}

	if isRuneKey(msg, 'l') && m.canLogoutAdminFromCurrentScreen() {
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
	case screenAdminFeatures:
		return m.updateAdminFeaturesKey(msg)
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
	case screenRepoAdminGrants:
		return m.updateRepoAdminGrantsKey(msg)
	case screenRepoAdminAddGrant:
		return m.updateRepoAdminAddGrantKey(msg)
	case screenAdminRobots:
		return m.updateAdminRobotsKey(msg)
	case screenAdminCreateRobot:
		return m.updateCreateRobotFormKey(msg)
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
	case isRuneKey(msg, 'f'):
		return m.openAdminFeatures()
	case isRuneKey(msg, 'b'):
		return m.openAdminRobots()
	}

	return m, nil
}

// updateAdminRobotsKey handles screenAdminRobots, a sibling of
// updateAdminUsersKey (design.md Decision 7): list/select, "n" to create,
// "e"/"x" to enable/disable via the confirm modal, "t" to reuse the existing
// token screens for the selected robot, "r" to refresh.
func (m Model) updateAdminRobotsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.clearRevealedAdminToken()
		m.screen = screenAdminUsers
		m.status = ""
		return m, nil
	case isMoveUpKey(msg):
		m.adminView.SelectedRobot = boundedIndex(m.adminView.SelectedRobot-1, len(m.adminView.Robots))
		return m, nil
	case isMoveDownKey(msg):
		m.adminView.SelectedRobot = boundedIndex(m.adminView.SelectedRobot+1, len(m.adminView.Robots))
		return m, nil
	case isRuneKey(msg, 'n'):
		// Defensive: a lingering RevealedTokenSecret from a PRIOR creation
		// must never bleed into a fresh create-robot form (the same
		// second-write-path shape the "t" guard below protects against).
		m.clearRevealedAdminToken()
		m.adminView.CreateRobotForm = newAdminViewState().CreateRobotForm
		m.screen = screenAdminCreateRobot
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'r'):
		m.status = "Loading robots..."
		return m, m.loadAdminRobotsCmd()
	case isRuneKey(msg, 't'):
		return m.openAdminRobotTokens()
	case isRuneKey(msg, 'e', 'x'):
		robot, ok := selectedAdminRobot(m.adminView)
		if !ok {
			m.status = "No robot selected."
			return m, nil
		}
		if isRuneKey(msg, 'e') && robot.Enabled {
			return m, nil
		}
		if isRuneKey(msg, 'x') && !robot.Enabled {
			return m, nil
		}
		kind := adminConfirmDisableRobot
		verb := "disable"
		if isRuneKey(msg, 'e') {
			kind = adminConfirmEnableRobot
			verb = "enable"
		}
		m.adminView.ConfirmModal = adminConfirmModal{
			Kind:        kind,
			Title:       fmt.Sprintf("Confirm %s", strings.Title(verb)),
			Message:     fmt.Sprintf("Confirm %s robot %q?", verb, robot.Username),
			ConfirmText: verb,
			UserID:      robot.ID,
			Username:    robot.Username,
		}
		m.status = ""
		return m, nil
	}

	return m, nil
}

// updateCreateRobotFormKey handles screenAdminCreateRobot's free-text form.
// Role cycles with Space (mirroring updateGrantFormKey's adminGrantFieldRole
// convention); unlike the repo-admin delegate's adminRepositoryGrantForm,
// this form is global-admin-only, so Role is not restricted away from
// repo-admin.
func (m Model) updateCreateRobotFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		// Esc is the ordinary "cancel/back" exit from the create screen,
		// including immediately after a creation while the one-time secret
		// is still shown: it MUST be cleared here so it can never render
		// again on this screen after the operator has moved on.
		m.clearRevealedAdminToken()
		m.screen = screenAdminRobots
		m.status = ""
		return m, nil
	case isTabKey(msg):
		m.adminView.CreateRobotForm.Focus = nextCreateRobotField(m.adminView.CreateRobotForm.Focus)
		return m, nil
	case isBackspaceKey(msg):
		m.deleteCreateRobotRune()
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRole {
			m.adminView.CreateRobotForm.Role = nextGrantRole(m.adminView.CreateRobotForm.Role)
			return m, nil
		}
	case isEnterKey(msg):
		name := strings.TrimSpace(m.adminView.CreateRobotForm.Name)
		repository := strings.TrimSpace(m.adminView.CreateRobotForm.Repository)
		if name == "" || repository == "" {
			m.status = "Name and repository are required."
			return m, nil
		}
		input := ports.AdminCreateRobotInput{Name: name, Repository: repository, Role: m.adminView.CreateRobotForm.Role}
		if ttl := strings.TrimSpace(m.adminView.CreateRobotForm.TTLSeconds); ttl != "" {
			seconds, err := strconv.ParseInt(ttl, 10, 64)
			if err != nil {
				m.status = "TTL seconds must be a whole number."
				return m, nil
			}
			input.TTL = time.Duration(seconds) * time.Second
		}
		m.status = fmt.Sprintf("Creating robot %s...", name)
		return m, m.createAdminRobotCmd(input)
	}
	if msg.Type == tea.KeyRunes {
		switch m.adminView.CreateRobotForm.Focus {
		case adminCreateRobotFieldName:
			m.adminView.CreateRobotForm.Name += string(msg.Runes)
		case adminCreateRobotFieldRepository:
			m.adminView.CreateRobotForm.Repository += string(msg.Runes)
		case adminCreateRobotFieldTTL:
			for _, r := range msg.Runes {
				if !unicode.IsDigit(r) {
					return m, nil
				}
			}
			m.adminView.CreateRobotForm.TTLSeconds += string(msg.Runes)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateAdminFeaturesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenAdminUsers
		m.status = ""
		return m, nil
	case m.isSelectedTrivyFeature() && isTabKey(msg):
		return m.toggleTrivyTab()
	case m.isSelectedTrivyFeature() && isRuneKey(msg, 'p'):
		// Available on both Trivy tabs, matching the policy badge composed
		// into renderTrivyTabs, which is likewise visible on both
		// (design.md Decision 6; resolves the design's open question in
		// favor of both tabs rather than Runtime only).
		threshold := m.adminView.ScanPolicy.SeverityThreshold
		if strings.TrimSpace(threshold) == "" {
			threshold = ports.ScanPolicyThresholdCritical
		}
		m.adminView.ScanPolicyModal = scanPolicyModal{Open: true, Focus: scanPolicyFieldEnabled, Enabled: m.adminView.ScanPolicy.Enabled, SeverityThreshold: threshold}
		m.status = ""
		return m, nil
	case m.isSelectedSigningFeature() && isRuneKey(msg, 'p'):
		// No key collision with the trivy 'p' case above: this branch is
		// guarded by isSelectedSigningFeature(), the trivy branch by
		// isSelectedTrivyFeature() -- the two are mutually exclusive
		// (design.md Decision 11 piece 2).
		m.adminView.SigningPolicyModal = signingPolicyModal{
			Open:         true,
			Focus:        signingPolicyFieldEnabled,
			Enabled:      m.adminView.SigningPolicy.Enabled,
			Fingerprints: signingKeyFingerprints(m.adminView.SigningPolicy.TrustedPublicKeys),
		}
		m.status = ""
		return m, nil
	case m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRepositoryAlerts && isMoveUpKey(msg):
		if len(m.adminView.TrivySummaries) == 0 {
			return m, nil
		}
		m.adminView.TrivySelectedAlert = boundedIndex(m.adminView.TrivySelectedAlert-1, len(m.adminView.TrivySummaries))
		m.syncAdminTableHighlights()
		return m, nil
	case m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRepositoryAlerts && isMoveDownKey(msg):
		if len(m.adminView.TrivySummaries) == 0 {
			return m, nil
		}
		m.adminView.TrivySelectedAlert = boundedIndex(m.adminView.TrivySelectedAlert+1, len(m.adminView.TrivySummaries))
		m.syncAdminTableHighlights()
		return m, nil
	case isMoveUpKey(msg):
		return m.moveAdminFeatureSelection(-1)
	case isMoveDownKey(msg):
		return m.moveAdminFeatureSelection(1)
	case m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRuntime && isRuneKey(msg, 'c'):
		modal, ok := trivyConfigModalFromPage(m.adminView.FeaturePage)
		if !ok {
			m.status = "Current Trivy configuration is unavailable."
			return m, nil
		}
		m.adminView.TrivyConfigModal = modal
		m.status = ""
		return m, nil
	case m.isSelectedGitleaksFeature() && isRuneKey(msg, 's'):
		// gitleaks has no tabs (unlike Trivy), so this opener is scoped only
		// to "gitleaks is the highlighted feature row" -- no tab check.
		modal, ok := gitleaksConfigModalFromPage(m.adminView.FeaturePage)
		if !ok {
			m.status = "Current gitleaks configuration is unavailable."
			return m, nil
		}
		m.adminView.GitleaksConfigModal = modal
		m.status = ""
		return m, nil
	case m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRepositoryAlerts && isRuneKey(msg, 'o'):
		// design.md Decision 8: opens repositoryOverrideModal bound to the
		// highlighted Repository Alerts row's repository, always in the
		// context of trivyFeatureName -- the only feature with a Repository
		// Alerts row today. This case sits inside updateAdminFeaturesKey's
		// switch (ends at :1257 below the featureActionForKey fallback), so
		// 'o' cannot be stolen by a feature action, and cannot steal one
		// either.
		summary, ok := selectedScanSummary(m.adminView)
		if !ok {
			return m, nil
		}
		m.adminView.RepositoryOverrideModal = repositoryOverrideModal{
			Open: true, Repository: summary.Repository,
			Feature: trivyFeatureName, Loading: true,
		}
		m.status = ""
		return m, m.loadRepositoryOverrideCmd(summary.Repository, trivyFeatureName)
	case m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRepositoryAlerts && isEnterKey(msg):
		// spec.md "Repository Alert Drill-Down Opens History Modal": Enter
		// opens the scan history modal, never the old inline detail — the
		// two never fire together.
		if len(m.adminView.TrivySummaries) == 0 {
			return m, nil
		}
		summary, _ := selectedScanSummary(m.adminView)
		m.adminView.ScanHistoryModal = adminScanHistoryModal{
			Open:       true,
			Repository: summary.Repository,
			Tabs:       newAdminScanHistoryTabs(),
			Loading:    true,
		}
		m.status = fmt.Sprintf("Loading scan history for %s...", adminFirstNonEmpty(summary.Repository, "repository"))
		m.rebuildAdminTables(m.adminTablesLayout())
		return m, m.loadAdminScanHistoryCmd(summary.Repository)
	case isEnterKey(msg), isRuneKey(msg, 'r'):
		if m.isSelectedTrivyFeature() && m.adminView.TrivyTab == trivyTabRepositoryAlerts {
			m.status = "Loading repository alerts..."
			return m, m.loadAdminScanRunsCmd("", 25)
		}
		if strings.TrimSpace(m.selectedFeatureName()) == "" {
			m.status = "No feature selected."
			return m, nil
		}
		m.status = fmt.Sprintf("Loading feature page for %s...", m.selectedFeatureName())
		return m, m.loadAdminFeaturePageCmd(m.selectedFeatureName())
	}

	if action, ok := featureActionForKey(msg, m.adminView.FeaturePage); ok {
		if strings.TrimSpace(action.ConfirmMessage) != "" {
			kind := adminConfirmKind(action.ID)
			switch action.ID {
			case "enable":
				kind = adminConfirmEnableFeature
			case "disable":
				kind = adminConfirmDisableFeature
			}
			m.adminView.ConfirmModal = adminConfirmModal{
				Kind:        kind,
				Title:       adminFirstNonEmpty(action.ConfirmTitle, action.Label),
				Message:     action.ConfirmMessage,
				ConfirmText: strings.ToLower(strings.TrimSpace(action.Label)),
				FeatureName: m.selectedFeatureName(),
			}
			m.status = ""
			return m, nil
		}
		m.status = fmt.Sprintf("Running %s for %s...", strings.ToLower(strings.TrimSpace(action.Label)), m.selectedFeatureName())
		return m, m.executeFeatureActionCmd(m.selectedFeatureName(), action.ID)
	}

	return m, nil
}

func (m Model) updateTrivyConfigModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.TrivyConfigModal = trivyConfigModal{}
		m.status = ""
		return m, nil
	case isTabKey(msg):
		m.adminView.TrivyConfigModal.Focus = nextTrivyConfigField(m.adminView.TrivyConfigModal.Focus)
		m.adminView.TrivyConfigModal.Error = ""
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.TrivyConfigModal.Focus == trivyConfigFieldScheduleEnabled {
			m.adminView.TrivyConfigModal.ScheduleEnabled = !m.adminView.TrivyConfigModal.ScheduleEnabled
			m.adminView.TrivyConfigModal.Error = ""
		}
		return m, nil
	case isBackspaceKey(msg):
		m.deleteTrivyConfigModalRune()
		m.adminView.TrivyConfigModal.Error = ""
		return m, nil
	case isEnterKey(msg):
		input, err := m.trivyConfigInputFromModal()
		if err != nil {
			m.adminView.TrivyConfigModal.Error = err.Error()
			return m, nil
		}
		m.status = "Submitting Trivy configuration..."
		return m, m.configureFeatureCmd(trivyFeatureName, input)
	}
	if msg.Type == tea.KeyRunes {
		m.appendTrivyConfigModalRunes(string(msg.Runes))
		m.adminView.TrivyConfigModal.Error = ""
		return m, nil
	}
	return m, nil
}

// updateGitleaksConfigModalKey mirrors updateTrivyConfigModalKey's dedicated-
// handler pattern at gitleaks' narrower 3-field scope: Tab cycles fields
// (wrapping, via nextGitleaksConfigField), Space toggles Enabled when it has
// focus, Backspace/rune keys edit the focused text field (Timeout,
// MaxConcurrency), Enter saves, Esc cancels without persisting.
func (m Model) updateGitleaksConfigModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.GitleaksConfigModal = gitleaksConfigModal{}
		m.status = ""
		return m, nil
	case isTabKey(msg):
		m.adminView.GitleaksConfigModal.Focus = nextGitleaksConfigField(m.adminView.GitleaksConfigModal.Focus)
		m.adminView.GitleaksConfigModal.Error = ""
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.GitleaksConfigModal.Focus == gitleaksConfigFieldEnabled {
			m.adminView.GitleaksConfigModal.Enabled = !m.adminView.GitleaksConfigModal.Enabled
			m.adminView.GitleaksConfigModal.Error = ""
		}
		return m, nil
	case isBackspaceKey(msg):
		m.deleteGitleaksConfigModalRune()
		m.adminView.GitleaksConfigModal.Error = ""
		return m, nil
	case isEnterKey(msg):
		input, err := m.gitleaksConfigInputFromModal()
		if err != nil {
			m.adminView.GitleaksConfigModal.Error = err.Error()
			return m, nil
		}
		m.status = "Submitting gitleaks configuration..."
		return m, m.configureFeatureCmd(gitleaksFeatureName, input)
	}
	if msg.Type == tea.KeyRunes {
		m.appendGitleaksConfigModalRunes(string(msg.Runes))
		m.adminView.GitleaksConfigModal.Error = ""
		return m, nil
	}
	return m, nil
}

// updateScanPolicyModalKey handles keys while the vulnerability policy
// modal is open, mirroring updateTrivyConfigModalKey's dedicated-handler
// pattern: Tab cycles the 2 fields (wrapping, via nextScanPolicyField),
// Space toggles Enabled when it has focus or cycles SeverityThreshold
// between CRITICAL/CRITICAL+HIGH when Threshold has focus, Enter saves,
// Esc cancels without persisting.
func (m Model) updateScanPolicyModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.ScanPolicyModal = scanPolicyModal{}
		m.status = ""
		return m, nil
	case isTabKey(msg):
		m.adminView.ScanPolicyModal.Focus = nextScanPolicyField(m.adminView.ScanPolicyModal.Focus)
		m.adminView.ScanPolicyModal.Error = ""
		return m, nil
	case isRuneKey(msg, ' '):
		switch m.adminView.ScanPolicyModal.Focus {
		case scanPolicyFieldEnabled:
			m.adminView.ScanPolicyModal.Enabled = !m.adminView.ScanPolicyModal.Enabled
		case scanPolicyFieldThreshold:
			m.adminView.ScanPolicyModal.SeverityThreshold = nextScanPolicyThreshold(m.adminView.ScanPolicyModal.SeverityThreshold)
		}
		m.adminView.ScanPolicyModal.Error = ""
		return m, nil
	case isEnterKey(msg):
		m.status = "Saving vulnerability policy..."
		return m, m.updateScanPolicyCmd(ports.ScanPolicySettings{Enabled: m.adminView.ScanPolicyModal.Enabled, SeverityThreshold: m.adminView.ScanPolicyModal.SeverityThreshold})
	}
	return m, nil
}

// nextScanPolicyThreshold cycles the 2-value severity threshold, toggled
// with Space per design.md Decision 6.
func nextScanPolicyThreshold(threshold string) string {
	if threshold == ports.ScanPolicyThresholdCriticalHigh {
		return ports.ScanPolicyThresholdCritical
	}
	return ports.ScanPolicyThresholdCriticalHigh
}

// updateSigningPolicyModalKey handles keys while the signing policy modal is
// open, mirroring updateScanPolicyModalKey's dedicated-handler pattern: Tab
// cycles the 3 fields (wrapping, via nextSigningPolicyField), Space toggles
// Enabled when it has focus, rune keys append to AddKey when it has focus,
// Backspace trims AddKey when it has focus, Esc cancels without persisting.
// Enter's behavior depends on Focus (design.md Decision 11 piece 1):
//   - ClearKeys focus: submits with an empty trusted-key list.
//   - Any other focus: submits the currently stored keys, plus AddKey
//     appended when it is non-empty (the AddKey field is never itself
//     validated client-side -- the admin API's existing
//     signing.NormalizePublicKeyPEM validation is the single source of
//     truth, surfaced back into modal.Error on rejection).
func (m Model) updateSigningPolicyModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.SigningPolicyModal = signingPolicyModal{}
		m.status = ""
		return m, nil
	case isTabKey(msg):
		m.adminView.SigningPolicyModal.Focus = nextSigningPolicyField(m.adminView.SigningPolicyModal.Focus)
		m.adminView.SigningPolicyModal.Error = ""
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.SigningPolicyModal.Focus == signingPolicyFieldEnabled {
			m.adminView.SigningPolicyModal.Enabled = !m.adminView.SigningPolicyModal.Enabled
		}
		m.adminView.SigningPolicyModal.Error = ""
		return m, nil
	case isBackspaceKey(msg):
		if m.adminView.SigningPolicyModal.Focus == signingPolicyFieldAddKey {
			m.adminView.SigningPolicyModal.AddKey = trimLastRune(m.adminView.SigningPolicyModal.AddKey)
		}
		m.adminView.SigningPolicyModal.Error = ""
		return m, nil
	case isEnterKey(msg):
		modal := m.adminView.SigningPolicyModal
		var keys []string
		if modal.Focus != signingPolicyFieldClearKeys {
			keys = append(keys, m.adminView.SigningPolicy.TrustedPublicKeys...)
			if strings.TrimSpace(modal.AddKey) != "" {
				keys = append(keys, modal.AddKey)
			}
		}
		m.status = "Saving signing policy..."
		return m, m.updateSigningPolicyCmd(ports.SigningPolicySettings{Enabled: modal.Enabled, TrustedPublicKeys: keys})
	}
	if msg.Type == tea.KeyRunes && m.adminView.SigningPolicyModal.Focus == signingPolicyFieldAddKey {
		m.adminView.SigningPolicyModal.AddKey += string(msg.Runes)
		m.adminView.SigningPolicyModal.Error = ""
		return m, nil
	}
	return m, nil
}

// signingKeyFingerprints derives a read-only SHA-256/12 fingerprint for each
// stored trusted key, so signingPolicyModal/renderSigningPolicyModal never
// has to hold or render raw PEM key material (design.md Decision 11 piece 1
// -- "the modal never has to display multi-line text either").
func signingKeyFingerprints(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	fingerprints := make([]string, 0, len(keys))
	for _, key := range keys {
		sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
		fingerprints = append(fingerprints, hex.EncodeToString(sum[:])[:12])
	}
	return fingerprints
}

// applyRepositoryOverrideToModal reflects a loaded/saved/cleared override
// back onto repositoryOverrideModal (spec.md "both actions MUST round-trip
// through the admin API and be reflected back in the modal"). Unlike
// scanPolicyModal, the modal stays open after a successful save/clear so the
// operator can see the reflected state and immediately clear or re-edit --
// exists=false zeroes the editable fields to show the repository is back to
// inheriting global settings.
func (m *Model) applyRepositoryOverrideToModal(override ports.RepositoryOverrideDetails, exists bool) {
	m.adminView.RepositoryOverrideModal.Exists = exists
	if !exists {
		m.adminView.RepositoryOverrideModal.Enabled = false
		m.adminView.RepositoryOverrideModal.PathPrimary = ""
		m.adminView.RepositoryOverrideModal.PathSecondary = ""
		return
	}
	m.adminView.RepositoryOverrideModal.Enabled = override.Enabled
	switch m.adminView.RepositoryOverrideModal.Feature {
	case gitleaksFeatureName:
		m.adminView.RepositoryOverrideModal.PathPrimary = override.ConfigPath
	case signingFeatureName:
		// The override modal edits a single trusted key via PathPrimary
		// (design.md Decision 11 piece 3) -- unlike signingPolicyModal's
		// growable list, only the first stored key is shown/edited here.
		m.adminView.RepositoryOverrideModal.PathPrimary = firstRepositoryOverrideTrustedKey(override.TrustedPublicKeys)
	default:
		m.adminView.RepositoryOverrideModal.PathPrimary = override.IgnoreFilePath
		m.adminView.RepositoryOverrideModal.PathSecondary = override.IgnorePolicyPath
	}
}

// firstRepositoryOverrideTrustedKey returns the first stored trusted key, or
// "" when none are stored -- repositoryOverrideModal's PathPrimary field
// edits at most one key per repository override (design.md Decision 11
// piece 3's single-field reuse, distinct from signingPolicyModal's list).
func firstRepositoryOverrideTrustedKey(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// updateRepositoryOverrideModalKey handles keys while repositoryOverrideModal
// is open (design.md Decision 8 piece 2): Esc clears, Tab cycles fields,
// Space toggles Enabled when Enabled has focus or cycles Feature when
// Feature has focus, runes append to the focused path field
// (appendTrivyConfigModalRunes pattern), Enter means save on every focus
// except FieldClear, where it means clear -- inert (no DELETE) when the
// modal is not currently backed by a stored override.
func (m Model) updateRepositoryOverrideModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.RepositoryOverrideModal = repositoryOverrideModal{}
		m.status = ""
		return m, nil
	case isTabKey(msg):
		m.adminView.RepositoryOverrideModal.Focus = nextRepositoryOverrideField(m.adminView.RepositoryOverrideModal.Focus, m.adminView.RepositoryOverrideModal.Feature)
		m.adminView.RepositoryOverrideModal.Error = ""
		return m, nil
	case isRuneKey(msg, ' '):
		switch m.adminView.RepositoryOverrideModal.Focus {
		case repositoryOverrideFieldEnabled:
			m.adminView.RepositoryOverrideModal.Enabled = !m.adminView.RepositoryOverrideModal.Enabled
		case repositoryOverrideFieldFeature:
			m.adminView.RepositoryOverrideModal.Feature = nextRepositoryOverrideFeatureName(m.adminView.RepositoryOverrideModal.Feature)
		}
		m.adminView.RepositoryOverrideModal.Error = ""
		return m, nil
	case isBackspaceKey(msg):
		m.deleteRepositoryOverrideModalRune()
		m.adminView.RepositoryOverrideModal.Error = ""
		return m, nil
	case isEnterKey(msg):
		modal := m.adminView.RepositoryOverrideModal
		if modal.Focus == repositoryOverrideFieldClear {
			if !modal.Exists {
				m.status = "Already inheriting global settings."
				return m, nil
			}
			m.status = "Clearing repository override..."
			return m, m.clearRepositoryOverrideCmd(modal.Repository, modal.Feature)
		}
		input := ports.RepositoryOverrideDetails{Enabled: modal.Enabled}
		switch modal.Feature {
		case gitleaksFeatureName:
			input.ConfigPath = modal.PathPrimary
		case signingFeatureName:
			if strings.TrimSpace(modal.PathPrimary) != "" {
				input.TrustedPublicKeys = []string{modal.PathPrimary}
			}
		default:
			input.IgnoreFilePath = modal.PathPrimary
			input.IgnorePolicyPath = modal.PathSecondary
		}
		m.status = "Saving repository override..."
		return m, m.saveRepositoryOverrideCmd(modal.Repository, modal.Feature, input)
	}
	if msg.Type == tea.KeyRunes {
		m.appendRepositoryOverrideModalRunes(string(msg.Runes))
		m.adminView.RepositoryOverrideModal.Error = ""
		return m, nil
	}
	return m, nil
}

// repositoryOverrideFeatureCycle is the modal's Feature field cycle order
// (design.md Decision 11 piece 3). A fourth feature is one more entry -- no
// restructuring. This is the one shipped TUI behavior this change
// deliberately alters: the cycle grows from trivy -> gitleaks -> trivy to
// trivy -> gitleaks -> signing -> trivy.
var repositoryOverrideFeatureCycle = []string{trivyFeatureName, gitleaksFeatureName, signingFeatureName}

// nextRepositoryOverrideFeatureName cycles the modal's Feature field through
// repositoryOverrideFeatureCycle, wrapping back to the first entry.
func nextRepositoryOverrideFeatureName(feature string) string {
	for index, candidate := range repositoryOverrideFeatureCycle {
		if candidate == feature {
			return repositoryOverrideFeatureCycle[(index+1)%len(repositoryOverrideFeatureCycle)]
		}
	}
	return repositoryOverrideFeatureCycle[0]
}

func (m *Model) deleteRepositoryOverrideModalRune() {
	switch m.adminView.RepositoryOverrideModal.Focus {
	case repositoryOverrideFieldPathPrimary:
		m.adminView.RepositoryOverrideModal.PathPrimary = trimLastRune(m.adminView.RepositoryOverrideModal.PathPrimary)
	case repositoryOverrideFieldPathSecondary:
		m.adminView.RepositoryOverrideModal.PathSecondary = trimLastRune(m.adminView.RepositoryOverrideModal.PathSecondary)
	}
}

func (m *Model) appendRepositoryOverrideModalRunes(value string) {
	if value == "" {
		return
	}
	switch m.adminView.RepositoryOverrideModal.Focus {
	case repositoryOverrideFieldPathPrimary:
		m.adminView.RepositoryOverrideModal.PathPrimary += value
	case repositoryOverrideFieldPathSecondary:
		m.adminView.RepositoryOverrideModal.PathSecondary += value
	}
}

// updateAdminScanHistoryModalKey handles keys while the scan history modal
// is open (design.md "Keys ... gated in updateAdminKey before
// updateAdminFeaturesKey"), following updateTrivyConfigModalKey's dedicated-
// handler pattern: Tab/Shift+Tab cycle tabs (wrapping, via cycleIndex),
// Left/Right page through the repository's chronological history (clamping,
// via boundedIndex — "paging", unlike the wrapping tab cursor), Esc closes
// and restores the row-list focus with no residual detail.
func (m Model) updateAdminScanHistoryModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.adminView.ScanHistoryModal = adminScanHistoryModal{}
		m.status = ""
		m.rebuildAdminTables(m.adminTablesLayout())
		return m, nil
	case isShiftTabKey(msg):
		m.adminView.ScanHistoryModal.ActiveTab = cycleIndex(m.adminView.ScanHistoryModal.ActiveTab-1, len(m.adminView.ScanHistoryModal.Tabs))
		m.adminView.ScanHistoryModal.FindingCursor = 0
		m.rebuildAdminTables(m.adminTablesLayout())
		return m, nil
	case isTabKey(msg):
		m.adminView.ScanHistoryModal.ActiveTab = cycleIndex(m.adminView.ScanHistoryModal.ActiveTab+1, len(m.adminView.ScanHistoryModal.Tabs))
		m.adminView.ScanHistoryModal.FindingCursor = 0
		m.rebuildAdminTables(m.adminTablesLayout())
		return m, nil
	case isMoveLeftKey(msg):
		return m.pageAdminScanHistory(-1)
	case isMoveRightKey(msg):
		return m.pageAdminScanHistory(1)
	case isMoveUpKey(msg):
		return m.moveAdminFindingCursor(-1)
	case isMoveDownKey(msg):
		return m.moveAdminFindingCursor(1)
	case isEnterKey(msg):
		return m.openSelectedAdminFindingLink()
	}
	return m, nil
}

// moveAdminFindingCursor moves the scan history modal's FindingCursor by
// delta, bounded (boundedIndex, never wrapping) within the CURRENTLY ACTIVE
// tab's own list length -- len(Detail.Findings) for Vulnerabilities,
// len(Secrets) for Leaks -- distinct from Cursor (Left/Right, which
// execution), the same "clamp, don't wrap" navigation pageAdminScanHistory
// already uses.
func (m Model) moveAdminFindingCursor(delta int) (tea.Model, tea.Cmd) {
	modal := m.adminView.ScanHistoryModal
	if len(modal.Tabs) == 0 {
		return m, nil
	}
	active := modal.Tabs[boundedIndex(modal.ActiveTab, len(modal.Tabs))]
	length := len(modal.Detail.Findings)
	if active.Kind == adminScanHistoryTabLeaks {
		length = len(modal.Secrets)
	}
	if length == 0 {
		return m, nil
	}
	newCursor := boundedIndex(modal.FindingCursor+delta, length)
	if newCursor == modal.FindingCursor {
		return m, nil
	}
	m.adminView.ScanHistoryModal.FindingCursor = newCursor
	m.rebuildAdminTables(m.adminTablesLayout())
	return m, nil
}

// openSelectedAdminFindingLink resolves the finding at FindingCursor (only
// on the Vulnerabilities tab -- secret findings carry no comparable external
// link) and returns the tea.Cmd that opens its advisory link in the OS
// default browser: ports.ScanRunFinding.PrimaryURL when set, else the
// constructed NVD URL from VulnerabilityID. The side effect lives in the
// returned Cmd (openAdminURLCmd), never here in the key handler itself
// (Bubble Tea convention).
func (m Model) openSelectedAdminFindingLink() (tea.Model, tea.Cmd) {
	modal := m.adminView.ScanHistoryModal
	if len(modal.Tabs) == 0 {
		return m, nil
	}
	active := modal.Tabs[boundedIndex(modal.ActiveTab, len(modal.Tabs))]
	if active.Kind != adminScanHistoryTabVulnerabilities {
		return m, nil
	}
	findings := modal.Detail.Findings
	if len(findings) == 0 {
		return m, nil
	}
	finding := findings[boundedIndex(modal.FindingCursor, len(findings))]
	link := adminFindingLink(finding)
	if link == "" {
		return m, nil
	}
	return m, openAdminURLCmd(link)
}

// pageAdminScanHistory moves the scan history modal's cursor by delta,
// clamped (boundedIndex, not cycleIndex — paging stops at the ends rather
// than wrapping), and re-fires the loadAdminScanRunDetailCmd/
// loadAdminSecretScanFindingsCmd chain for the newly selected run so both
// the Vulnerabilities and Leaks tabs stay scoped to whichever run is
// currently navigated (spec.md "Scan Execution History Navigation").
func (m Model) pageAdminScanHistory(delta int) (tea.Model, tea.Cmd) {
	modal := m.adminView.ScanHistoryModal
	if len(modal.Runs) == 0 {
		return m, nil
	}
	newCursor := boundedIndex(modal.Cursor+delta, len(modal.Runs))
	if newCursor == modal.Cursor {
		return m, nil
	}
	m.adminView.ScanHistoryModal.Cursor = newCursor
	m.adminView.ScanHistoryModal.Detail = ports.ScanRunDetail{}
	m.adminView.ScanHistoryModal.Secrets = nil
	m.adminView.ScanHistoryModal.Loading = true
	m.adminView.ScanHistoryModal.Error = ""
	m.adminView.ScanHistoryModal.FindingCursor = 0
	run := modal.Runs[newCursor]
	m.rebuildAdminTables(m.adminTablesLayout())
	return m, m.loadAdminScanRunDetailCmd(run.ID)
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
		case adminConfirmEnableFeature:
			m.status = fmt.Sprintf("Submitting enable for %s...", modal.FeatureName)
			return m, m.executeFeatureActionCmd(modal.FeatureName, "enable")
		case adminConfirmDisableFeature:
			m.status = fmt.Sprintf("Submitting disable for %s...", modal.FeatureName)
			return m, m.executeFeatureActionCmd(modal.FeatureName, "disable")
		case adminConfirmDeleteGrant:
			m.status = fmt.Sprintf("Removing grant %q from %s...", modal.Repository, modal.Username)
			return m, m.deleteAdminGrantCmd(modal.UserID, modal.Username, modal.Repository)
		case adminConfirmDeleteRepoGrant:
			m.status = fmt.Sprintf("Removing grant for %q from %q...", modal.Username, modal.Repository)
			return m, m.deleteRepoAdminGrantCmd(modal.Repository, modal.Username)
		case adminConfirmRevokeToken:
			m.status = fmt.Sprintf("Revoking token %q for %s...", modal.Accessor, modal.Username)
			return m, m.revokeAdminTokenCmd(modal.UserID, modal.Username, modal.Accessor)
		case adminConfirmEnableRobot:
			m.status = fmt.Sprintf("Submitting enable for %s...", modal.Username)
			return m, m.enableDisableRobotCmd(modal.UserID, true)
		case adminConfirmDisableRobot:
			m.status = fmt.Sprintf("Submitting disable for %s...", modal.Username)
			return m, m.enableDisableRobotCmd(modal.UserID, false)
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
			Username:   strings.TrimSpace(m.adminView.CreateUserForm.Username),
			Password:   m.adminView.CreateUserForm.Password,
			IsAdmin:    m.adminView.CreateUserForm.IsAdmin,
			IsReadOnly: m.adminView.CreateUserForm.IsReadOnly,
			Enabled:    m.adminView.CreateUserForm.Enabled,
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
	case isMoveUpKey(msg):
		if m.adminView.GrantForm.Focus == adminGrantFieldRepository {
			suggestions := grantRepositorySuggestions(m.adminView.GrantForm, m.repositories.Names())
			m.adminView.GrantForm.RepositorySuggestion = boundedIndex(m.adminView.GrantForm.RepositorySuggestion-1, len(suggestions))
			return m, nil
		}
	case isMoveDownKey(msg):
		if m.adminView.GrantForm.Focus == adminGrantFieldRepository {
			suggestions := grantRepositorySuggestions(m.adminView.GrantForm, m.repositories.Names())
			m.adminView.GrantForm.RepositorySuggestion = boundedIndex(m.adminView.GrantForm.RepositorySuggestion+1, len(suggestions))
			return m, nil
		}
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
			m.adminView.GrantForm.RepositorySuggestion = 0
		}
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.GrantForm.Focus == adminGrantFieldRole {
			m.adminView.GrantForm.Role = nextGrantRole(m.adminView.GrantForm.Role)
		}
		return m, nil
	case isEnterKey(msg):
		if m.adminView.GrantForm.Focus == adminGrantFieldRepository {
			suggestions := grantRepositorySuggestions(m.adminView.GrantForm, m.repositories.Names())
			if len(suggestions) > 0 {
				m.adminView.GrantForm.Repository = suggestions[boundedIndex(m.adminView.GrantForm.RepositorySuggestion, len(suggestions))]
			}
			if strings.TrimSpace(m.adminView.GrantForm.Repository) == "" {
				m.status = "Repository is required."
				return m, nil
			}
			m.adminView.GrantForm.Focus = adminGrantFieldRole
			m.adminView.GrantForm.RepositorySuggestion = 0
			return m, nil
		}
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
		m.adminView.GrantForm.RepositorySuggestion = 0
		return m, nil
	}
	return m, nil
}

// updateRepoAdminGrantsKey handles screenRepoAdminGrants, a sibling of
// updateAdminGrantsKey scoped to RepoAdminRepository instead of
// SelectedUserID -- there is no user-selection guard on "n" (Add Grant),
// unlike the user-centric screen, because the repository context is already
// fixed by the time this screen is reachable at all.
func (m Model) updateRepoAdminGrantsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		return m.returnToInspection(), nil
	case isRuneKey(msg, 'n'):
		if !m.adminView.RepoAdminGrantsAuthorized {
			m.status = "You don't have repo-admin access to this repository."
			return m, nil
		}
		m.adminView.RepoAdminGrantForm = adminRepositoryGrantForm{Role: domainauth.RepoRoleReader}
		m.screen = screenRepoAdminAddGrant
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'e'):
		grant, ok := selectedRepoAdminGrantForView(m.adminView)
		if !ok {
			m.status = "No grant selected to edit."
			return m, nil
		}
		// A delegate's grants list is scoped to their own repository but not
		// filtered by role (service.go ListRepositoryGrants), so it can
		// legitimately contain a peer's repo-admin grant. Editing it through
		// this form is refused outright, rather than clamped to a different
		// role, so the form never displays or silently rewrites a row it
		// didn't create -- mirrors the existing "Already inheriting global
		// settings." refusal precedent for an action that doesn't apply to
		// the current row/state.
		if grant.Role == domainauth.RepoRoleAdmin {
			m.status = "repo-admin grants cannot be edited here; ask a global admin."
			return m, nil
		}
		m.adminView.RepoAdminGrantForm = adminRepositoryGrantForm{Username: grant.Username, Role: grant.Role, Focus: adminRepoGrantFieldUsername}
		m.screen = screenRepoAdminAddGrant
		m.status = ""
		return m, nil
	case isMoveUpKey(msg):
		m.adminView.SelectedRepoAdminGrant = boundedIndex(m.adminView.SelectedRepoAdminGrant-1, len(m.adminView.RepoAdminGrants))
		return m, nil
	case isMoveDownKey(msg):
		m.adminView.SelectedRepoAdminGrant = boundedIndex(m.adminView.SelectedRepoAdminGrant+1, len(m.adminView.RepoAdminGrants))
		return m, nil
	case isRuneKey(msg, 'x'):
		grant, ok := selectedRepoAdminGrantForView(m.adminView)
		if !ok {
			m.status = "No grant selected to remove."
			return m, nil
		}
		m.adminView.ConfirmModal = adminConfirmModal{
			Kind:        adminConfirmDeleteRepoGrant,
			Title:       "Confirm Grant Removal",
			Message:     fmt.Sprintf("Remove grant for %q from %q?", grant.Username, m.adminView.RepoAdminRepository),
			ConfirmText: "remove",
			Repository:  m.adminView.RepoAdminRepository,
			Username:    grant.Username,
		}
		m.status = ""
		return m, nil
	}
	return m, nil
}

// updateRepoAdminAddGrantKey handles screenRepoAdminAddGrant. Unlike
// updateGrantFormKey's repository text field with autosuggest, this form has
// no repository field at all (RepoAdminRepository is fixed context), and its
// Role field is cycled only by nextDelegateGrantRole -- the structural
// guarantee that repo-admin can never be offered here.
func (m Model) updateRepoAdminAddGrantKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		m.screen = screenRepoAdminGrants
		return m, nil
	case isTabKey(msg):
		if m.adminView.RepoAdminGrantForm.Focus == adminRepoGrantFieldUsername {
			m.adminView.RepoAdminGrantForm.Focus = adminRepoGrantFieldRole
		} else {
			m.adminView.RepoAdminGrantForm.Focus = adminRepoGrantFieldUsername
		}
		return m, nil
	case isBackspaceKey(msg):
		if m.adminView.RepoAdminGrantForm.Focus == adminRepoGrantFieldUsername {
			m.adminView.RepoAdminGrantForm.Username = trimLastRune(m.adminView.RepoAdminGrantForm.Username)
		}
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.RepoAdminGrantForm.Focus == adminRepoGrantFieldRole {
			m.adminView.RepoAdminGrantForm.Role = nextDelegateGrantRole(m.adminView.RepoAdminGrantForm.Role)
		}
		return m, nil
	case isEnterKey(msg):
		username := strings.TrimSpace(m.adminView.RepoAdminGrantForm.Username)
		if username == "" {
			m.status = "Username is required."
			return m, nil
		}
		repository := strings.TrimSpace(m.adminView.RepoAdminRepository)
		if repository == "" {
			m.status = "Select a repository before managing grants."
			return m, nil
		}
		input := ports.AdminPutRepositoryGrantInput{Repository: repository, Username: username, Role: m.adminView.RepoAdminGrantForm.Role}
		m.status = fmt.Sprintf("Saving grant for %s...", username)
		return m, m.putRepoAdminGrantCmd(input)
	}
	if msg.Type == tea.KeyRunes && m.adminView.RepoAdminGrantForm.Focus == adminRepoGrantFieldUsername {
		m.adminView.RepoAdminGrantForm.Username += string(msg.Runes)
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

func isShiftTabKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab"
}

// isMoveLeftKey/isMoveRightKey back the scan history modal's Left/Right
// history paging (spec.md "Scan Execution History Navigation"). Deliberately
// no 'h'/'l' vim-style rune aliases (unlike isMoveUpKey/isMoveDownKey's 'k'/
// 'j'): 'l' is already the admin logout key.
func isMoveLeftKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyLeft || msg.String() == "left"
}

func isMoveRightKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRight || msg.String() == "right"
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

func isPgUpKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyPgUp || msg.String() == "pgup"
}

func isPgDnKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyPgDown || msg.String() == "pgdown"
}

func isHomeKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyHome || msg.String() == "home"
}

func isEndKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyEnd || msg.String() == "end"
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

// scrollableBodyContext returns the status/help text and total line count
// for screens whose body scrolls via bodyScroll/renderSection (repository
// catalog and tags — design.md decision #3). ok is false elsewhere (table
// screens gain paging in Phase 4). Shared by View() and applyBodyPageKey so
// the measured status/help text can never drift from what is rendered.
func (m Model) scrollableBodyContext() (status, help string, total int, ok bool) {
	switch m.screen {
	case screenRepositories:
		return m.notice, "Enter: open tags | Tab: admin | q: quit | g: repo grants", len(m.repositories.Items) + 1, true
	case screenTags:
		return "", "Enter: inspect manifest | Tab: admin | Esc: back | q: quit", len(m.tags.Items) + 1, true
	}
	return "", "", 0, false
}

// applyBodyPageKey drives bodyScroll from PgUp/PgDn/Home/End, clamping to
// [0, maxScroll] with the same visible-row math fitLines uses at render
// time. Returns false (no-op) when the screen has no scrollable body.
func (m *Model) applyBodyPageKey(msg tea.KeyMsg) bool {
	status, help, total, ok := m.scrollableBodyContext()
	if !ok {
		return false
	}

	layout := m.contentBudget(status, help)
	visible := layout.SectionRows - 1
	if visible < 1 {
		visible = 1
	}
	maxScroll := total - visible
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch {
	case isHomeKey(msg):
		m.bodyScroll = 0
	case isEndKey(msg):
		m.bodyScroll = maxScroll
	case isPgUpKey(msg):
		m.bodyScroll -= visible
	case isPgDnKey(msg):
		m.bodyScroll += visible
	}
	if m.bodyScroll < 0 {
		m.bodyScroll = 0
	}
	if m.bodyScroll > maxScroll {
		m.bodyScroll = maxScroll
	}
	return true
}

func (m *Model) moveSelection(delta int) {
	switch m.screen {
	case screenRepositories:
		m.repositories.Selected = boundedIndex(m.repositories.Selected+delta, len(m.repositories.Items))
		// WithHighlightedRow auto-pages the live bubble-table to keep the
		// highlighted row visible -- mirrors the Tags table's own mechanism
		// below, one level up.
		if m.repositories.Table.TotalRows() > 0 {
			m.repositories.Table = m.repositories.Table.WithHighlightedRow(m.repositories.Selected)
		}
	case screenTags:
		m.tags.Selected = boundedIndex(m.tags.Selected+delta, len(m.tags.Items))
		// WithHighlightedRow auto-pages the live bubble-table to keep the
		// highlighted row visible (design.md decision #5's own mechanism,
		// mirrored from syncAdminTableHighlights) -- no PgUp/PgDn forwarding
		// into table.Update, which is never called for this table.
		if m.tags.Table.TotalRows() > 0 {
			m.tags.Table = m.tags.Table.WithHighlightedRow(m.tags.Selected)
		}
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
	return m.repositories.Items[m.repositories.Selected].Name, true
}

func (m Model) selectedTag() (string, bool) {
	if len(m.tags.Items) == 0 {
		return "", false
	}
	return m.tags.Items[m.tags.Selected].Name, true
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
		result, err := m.service.RepositorySummaries(m.ctx, 100, "")
		return catalogLoadedMsg{result: result, err: err}
	}
}

func (m Model) loadTagsCmd(repository string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.TagDetails(m.ctx, repository, 100, "")
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
		// A fresh SignatureStatus call here, rather than threading the
		// Console Tags table's already-resolved SignatureStatusResult
		// through tagsLoadedMsg/TagsModel: this screen's ResolveManifest
		// call above is already gated on ActionPull, the same
		// authorization SignatureStatus itself requires, while the Tags
		// list (TagDetails) is gated on the weaker ActionInspect -- its
		// resolved status was never computed under this screen's own
		// access check, so reusing it here would blur that boundary.
		signature, signatureErr := m.service.SignatureStatus(m.ctx, repository, tag)
		resultErr := uploadsErr
		if resultErr == nil {
			resultErr = signatureErr
		}
		return manifestLoadedMsg{repository: repository, tag: tag, manifest: manifest, uploads: uploads, signature: signature, err: resultErr}
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

func (m Model) loadAdminFeaturesCmd() tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeaturesLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		features, err := m.adminClient.ListFeatures(m.ctx, m.adminSession)
		return adminFeaturesLoadedMsg{features: features, err: err}
	}
}

func (m Model) loadAdminFeaturePageCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeaturePageLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		page, err := m.adminClient.GetFeaturePage(m.ctx, m.adminSession, name)
		return adminFeaturePageLoadedMsg{page: page, err: err}
	}
}

func (m Model) loadAdminScanRunsCmd(repository string, limit int) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminScanRunsLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		runs, err := m.adminClient.ListScanRuns(m.ctx, m.adminSession, repository, limit)
		return adminScanRunsLoadedMsg{runs: runs, err: err}
	}
}

func (m Model) loadAdminScanRunDetailCmd(runID string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminScanRunDetailLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		detail, err := m.adminClient.GetScanRunDetail(m.ctx, m.adminSession, runID)
		return adminScanRunDetailLoadedMsg{detail: detail, err: err}
	}
}

func (m Model) loadAdminSecretScanFindingsCmd(repository string, digest string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminSecretScanFindingsLoadedMsg{repository: repository, digest: digest, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		detail, err := m.adminClient.GetSecretScanFindings(m.ctx, m.adminSession, repository, digest)
		if err != nil {
			return adminSecretScanFindingsLoadedMsg{repository: repository, digest: digest, err: err}
		}
		return adminSecretScanFindingsLoadedMsg{repository: repository, digest: digest, findings: detail.Findings}
	}
}

// loadAdminScanHistoryCmd fetches one repository's scan-run history
// (design.md "Chronological history by client-side re-sort": ListScanRuns's
// own severity/fixability ordering is presentation-specific to the summary
// table, so the modal re-sorts the same bounded window chronologically
// itself instead of a new port/query) and re-sorts it newest-first for the
// scan history modal.
func (m Model) loadAdminScanHistoryCmd(repository string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminScanHistoryLoadedMsg{repository: repository, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		runs, err := m.adminClient.ListScanRuns(m.ctx, m.adminSession, repository, adminScanHistoryWindowLimit)
		if err != nil {
			return adminScanHistoryLoadedMsg{repository: repository, err: err}
		}
		return adminScanHistoryLoadedMsg{repository: repository, runs: sortScanRunsChronologically(runs)}
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

func (m Model) loadRepoAdminGrantsCmd(repository string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepoGrantsLoadedMsg{repository: repository, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		grants, err := m.adminClient.ListRepositoryGrants(m.ctx, m.adminSession, repository)
		return adminRepoGrantsLoadedMsg{repository: repository, grants: grants, err: err}
	}
}

func (m Model) putRepoAdminGrantCmd(input ports.AdminPutRepositoryGrantInput) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepoGrantMutatedMsg{repository: input.Repository, username: input.Username, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		_, err := m.adminClient.PutRepositoryGrant(m.ctx, m.adminSession, input)
		return adminRepoGrantMutatedMsg{repository: input.Repository, username: input.Username, err: err}
	}
}

func (m Model) deleteRepoAdminGrantCmd(repository string, username string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepoGrantMutatedMsg{repository: repository, username: username, deleted: true, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		err := m.adminClient.DeleteRepositoryGrant(m.ctx, m.adminSession, repository, username)
		return adminRepoGrantMutatedMsg{repository: repository, username: username, deleted: true, err: err}
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

func (m Model) loadAdminRobotsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRobotsLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		robots, err := m.adminClient.ListRobots(m.ctx, m.adminSession)
		return adminRobotsLoadedMsg{robots: robots, err: err}
	}
}

func (m Model) createAdminRobotCmd(input ports.AdminCreateRobotInput) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRobotCreatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		created, err := m.adminClient.CreateRobot(m.ctx, m.adminSession, input)
		return adminRobotCreatedMsg{created: created, err: err}
	}
}

// enableDisableRobotCmd reuses AdminClient.EnableUser/DisableUser with the
// robot's user ID (design.md Decision 6) -- no dedicated robot mutation
// route exists or is needed.
func (m Model) enableDisableRobotCmd(robotID string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRobotEnabledMsg{robotID: robotID, enabled: enabled, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		var err error
		if enabled {
			_, err = m.adminClient.EnableUser(m.ctx, m.adminSession, robotID)
		} else {
			_, err = m.adminClient.DisableUser(m.ctx, m.adminSession, robotID)
		}
		return adminRobotEnabledMsg{robotID: robotID, enabled: enabled, err: err}
	}
}

func (m Model) executeFeatureActionCmd(name string, actionID string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeatureActionCompletedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		result, err := m.adminClient.ExecuteFeatureAction(m.ctx, m.adminSession, name, actionID)
		return adminFeatureActionCompletedMsg{result: result, err: err}
	}
}

func (m Model) configureFeatureCmd(name string, input ports.FeatureConfigureInput) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeatureConfiguredMsg{name: name, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		_, err := m.adminClient.ConfigureFeature(m.ctx, m.adminSession, name, input)
		return adminFeatureConfiguredMsg{name: name, err: err}
	}
}

func (m Model) loadScanPolicyCmd() tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminScanPolicyLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		settings, err := m.adminClient.GetScanPolicy(m.ctx, m.adminSession)
		return adminScanPolicyLoadedMsg{settings: settings, err: err}
	}
}

func (m Model) updateScanPolicyCmd(input ports.ScanPolicySettings) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminScanPolicyUpdatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		settings, err := m.adminClient.UpdateScanPolicy(m.ctx, m.adminSession, input)
		return adminScanPolicyUpdatedMsg{settings: settings, err: err}
	}
}

func (m Model) loadSigningPolicyCmd() tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminSigningPolicyLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		settings, err := m.adminClient.GetSigningPolicy(m.ctx, m.adminSession)
		return adminSigningPolicyLoadedMsg{settings: settings, err: err}
	}
}

func (m Model) updateSigningPolicyCmd(input ports.SigningPolicySettings) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminSigningPolicyUpdatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		settings, err := m.adminClient.UpdateSigningPolicy(m.ctx, m.adminSession, input)
		return adminSigningPolicyUpdatedMsg{settings: settings, err: err}
	}
}

// loadRepositoryOverrideCmd fetches one repository's stored override to
// populate repositoryOverrideModal on open (design.md Decision 8 piece 2).
func (m Model) loadRepositoryOverrideCmd(repository string, feature string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepositoryOverrideLoadedMsg{repository: repository, feature: feature, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		override, exists, err := m.adminClient.GetRepositoryOverride(m.ctx, m.adminSession, repository, feature)
		return adminRepositoryOverrideLoadedMsg{repository: repository, feature: feature, override: override, exists: exists, err: err}
	}
}

// saveRepositoryOverrideCmd submits repositoryOverrideModal's edited fields
// as a full-row-replace PUT.
func (m Model) saveRepositoryOverrideCmd(repository string, feature string, input ports.RepositoryOverrideDetails) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepositoryOverrideSavedMsg{repository: repository, feature: feature, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		override, err := m.adminClient.SetRepositoryOverride(m.ctx, m.adminSession, repository, feature, input)
		return adminRepositoryOverrideSavedMsg{repository: repository, feature: feature, override: override, exists: true, err: err}
	}
}

// clearRepositoryOverrideCmd deletes the stored override for repositoryOverrideModal's Clear row.
func (m Model) clearRepositoryOverrideCmd(repository string, feature string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepositoryOverrideSavedMsg{repository: repository, feature: feature, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		err := m.adminClient.ClearRepositoryOverride(m.ctx, m.adminSession, repository, feature)
		return adminRepositoryOverrideSavedMsg{repository: repository, feature: feature, exists: false, err: err}
	}
}

// loadRepositoryOverridesListCmd fetches every stored override row for one
// feature, chained after adminScanRunsLoadedMsg so the Repository Alerts
// table can annotate disabled repositories (design.md Decision 7's "List
// (TUI annotation)" row).
func (m Model) loadRepositoryOverridesListCmd(feature string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepositoryOverridesListLoadedMsg{feature: feature, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		overrides, err := m.adminClient.ListRepositoryOverrides(m.ctx, m.adminSession, feature)
		return adminRepositoryOverridesListLoadedMsg{feature: feature, overrides: overrides, err: err}
	}
}

func (m Model) installFeatureRuntimeCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeatureRuntimeMutatedMsg{name: name, action: "installed", err: fmt.Errorf("admin API is unavailable for this session")}
		}
		state, err := m.adminClient.InstallFeatureRuntime(m.ctx, m.adminSession, name, "")
		return adminFeatureRuntimeMutatedMsg{name: name, action: "installed", state: state, err: err}
	}
}

func (m Model) upgradeFeatureRuntimeCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeatureRuntimeMutatedMsg{name: name, action: "upgraded", err: fmt.Errorf("admin API is unavailable for this session")}
		}
		state, err := m.adminClient.UpgradeFeatureRuntime(m.ctx, m.adminSession, name, "")
		return adminFeatureRuntimeMutatedMsg{name: name, action: "upgraded", state: state, err: err}
	}
}

func (m Model) rollbackFeatureRuntimeCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeatureRuntimeMutatedMsg{name: name, action: "rolled back", err: fmt.Errorf("admin API is unavailable for this session")}
		}
		state, err := m.adminClient.RollbackFeatureRuntime(m.ctx, m.adminSession, name)
		return adminFeatureRuntimeMutatedMsg{name: name, action: "rolled back", state: state, err: err}
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

// openRepoAdminGrants is the repo-admin delegate's own entry point
// (design.md Decision 7), reached from the Console Repositories screen's
// grant action instead of "tab"'s global-admin openAdmin. It mirrors
// openAdmin's already-authenticated/expired/unauthenticated branches, but
// carries adminIntent/adminIntentRepository through screenAdminLogin so the
// shared login screen can route to screenRepoAdminGrants on success.
func (m Model) openRepoAdminGrants() (tea.Model, tea.Cmd) {
	if m.adminClient == nil {
		m.status = "Admin API is unavailable for this session."
		return m, nil
	}
	repository, ok := m.selectedRepository()
	if !ok {
		m.status = "Select a repository to manage grants."
		return m, nil
	}
	m.adminIntent = adminIntentRepoGrants
	m.adminIntentRepository = repository
	m.adminReturn = m.screen
	m.showMutationNotice = false
	m.err = nil
	if m.adminSession.IsAuthenticated() {
		if m.adminSession.IsExpired(m.now()) {
			return m.expireAdminSession(AdminSessionExpiredReasonExpired), nil
		}
		m.adminAuth = adminAuthStateAuthenticated
		m.adminIntent = adminIntentOperator
		m.adminIntentRepository = ""
		m.adminView.RepoAdminRepository = repository
		m.adminView.RepoAdminGrantsAuthorized = false
		m.screen = screenRepoAdminGrants
		m.status = fmt.Sprintf("Loading grants for %s...", repository)
		return m, m.loadRepoAdminGrantsCmd(repository)
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

func (m Model) openAdminFeatures() (tea.Model, tea.Cmd) {
	m.screen = screenAdminFeatures
	m.adminView.UserSearchActive = false
	m.status = "Loading built-in features..."
	return m, m.loadAdminFeaturesCmd()
}

// openAdminRobots opens screenAdminRobots from screenAdminUsers (design.md
// Decision 7), mirroring openAdminFeatures. Defensively clears any lingering
// RevealedTokenSecret on entry (see updateAdminRobotsKey's "n" case for the
// leak scenario this guards against).
func (m Model) openAdminRobots() (tea.Model, tea.Cmd) {
	m.screen = screenAdminRobots
	m.adminView.UserSearchActive = false
	m.clearRevealedAdminToken()
	m.status = "Loading robots..."
	return m, m.loadAdminRobotsCmd()
}

// openAdminRobotTokens opens the existing screenAdminEditUserTokens screen
// for the selected robot, reusing SelectedUserID/SelectedUsername exactly
// like a human user (design.md Decision 6: enable/disable/token issuance
// reuse the existing user routes/screens unchanged). clearRevealedAdminToken
// MUST run first: RevealedTokenSecret/Accessor are the same fields the
// robot-creation reveal (screenAdminCreateRobot) populates, and
// renderAdminTokensScreen renders them unconditionally when non-empty --
// without this clear, a secret from an earlier robot creation would render
// a second time here, for a different robot, with no fresh token issued.
func (m Model) openAdminRobotTokens() (tea.Model, tea.Cmd) {
	robot, ok := selectedAdminRobot(m.adminView)
	if !ok {
		m.status = "Select a robot to manage tokens."
		return m, nil
	}
	m.clearRevealedAdminToken()
	m.adminView.SelectedUserID = robot.ID
	m.adminView.SelectedUsername = robot.Username
	m.screen = screenAdminEditUserTokens
	m.status = fmt.Sprintf("Loading admin tokens for %s...", robot.Username)
	return m, m.loadAdminTokensCmd(robot.ID, robot.Username)
}

// selectedAdminRobot returns the robot under AdminViewState.SelectedRobot,
// mirroring selectedAdminUser's bounds-checked convention.
func selectedAdminRobot(view AdminViewState) (ports.AdminRobot, bool) {
	if view.SelectedRobot < 0 || view.SelectedRobot >= len(view.Robots) {
		return ports.AdminRobot{}, false
	}
	return view.Robots[view.SelectedRobot], true
}

func (m Model) logoutAdmin() Model {
	m.adminSession, m.adminView = LogoutAdminState()
	m.adminAuth = adminAuthStateUnauthenticated
	m.adminLogin.Password = ""
	m.adminLogin.Focus = loginFieldUsername
	m.screen = screenAdminLogin
	m.loadingText = ""
	m.status = "Logged out."
	// Defensive reset: a stale repo-grants intent must never survive a
	// logout into a fresh operator login (adminIntent is one-shot per
	// design.md Decision 7).
	m.adminIntent = adminIntentOperator
	m.adminIntentRepository = ""
	return m
}

func (m Model) returnToInspection() Model {
	m.screen = m.adminReturn
	m.loadingText = ""
	m.status = ""
	m.adminView.ConfirmModal = adminConfirmModal{}
	m.adminView.UserSearchActive = false
	m.clearRevealedAdminToken()
	// Defensive reset: leaving the login screen (e.g. Esc) without
	// completing auth must not leave a stale repo-grants intent for a
	// later, unrelated login (adminIntent is one-shot per design.md
	// Decision 7).
	m.adminIntent = adminIntentOperator
	m.adminIntentRepository = ""
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

func (m *Model) deleteCreateRobotRune() {
	switch m.adminView.CreateRobotForm.Focus {
	case adminCreateRobotFieldName:
		m.adminView.CreateRobotForm.Name = trimLastRune(m.adminView.CreateRobotForm.Name)
	case adminCreateRobotFieldRepository:
		m.adminView.CreateRobotForm.Repository = trimLastRune(m.adminView.CreateRobotForm.Repository)
	case adminCreateRobotFieldTTL:
		m.adminView.CreateRobotForm.TTLSeconds = trimLastRune(m.adminView.CreateRobotForm.TTLSeconds)
	}
}

func (m *Model) toggleCreateUserField() {
	switch m.adminView.CreateUserForm.Focus {
	case adminCreateUserFieldIsAdmin:
		m.adminView.CreateUserForm.IsAdmin = !m.adminView.CreateUserForm.IsAdmin
	case adminCreateUserFieldIsReadOnly:
		m.adminView.CreateUserForm.IsReadOnly = !m.adminView.CreateUserForm.IsReadOnly
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

// nextCreateRobotField cycles screenAdminCreateRobot's 4 fields with a
// wrapping cursor: Name -> Repository -> Role -> TTL -> Name.
func nextCreateRobotField(field adminCreateRobotField) adminCreateRobotField {
	if field >= adminCreateRobotFieldTTL {
		return adminCreateRobotFieldName
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

// nextDelegateGrantRole cycles screenRepoAdminAddGrant's role field between
// exactly RepoRoleReader and RepoRoleWriter (design.md's escalation bounds:
// a delegate must never be offered repo-admin, whether for a new grant or
// self-assignment). Unlike nextGrantRole's 3-value cycle, this is a
// structural guarantee, not a UI convention: every branch, including the
// otherwise-unreachable RepoRoleAdmin starting value, resolves to Reader or
// Writer, so repo-admin can never appear regardless of the current state.
func nextDelegateGrantRole(current domainauth.RepoRole) domainauth.RepoRole {
	if current == domainauth.RepoRoleReader {
		return domainauth.RepoRoleWriter
	}
	return domainauth.RepoRoleReader
}

func isAdminScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminAuthenticating, screenAdminUsers, screenAdminFeatures, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens, screenAdminCreateToken, screenRepoAdminGrants, screenRepoAdminAddGrant, screenAdminRobots, screenAdminCreateRobot:
		return true
	default:
		return false
	}
}

// isAdminPrincipalScreen deliberately excludes screenRepoAdminAddGrant
// (a free-text username form), mirroring the existing screenAdminAddGrant
// exclusion: this gates the bare 'q' quit key, and including a free-text
// form here would make typing "q" as part of a username quit the program.
func isAdminPrincipalScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminUsers, screenAdminFeatures, screenAdminCreateUser, screenAdminEditUser, screenAdminEditUserGrants, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots:
		return true
	default:
		return false
	}
}

func (m Model) canLogoutAdminFromCurrentScreen() bool {
	if m.adminAuth != adminAuthStateAuthenticated || m.adminView.ConfirmModal.Active() {
		return false
	}

	switch m.screen {
	case screenAdminUsers:
		return !m.adminView.UserSearchActive
	case screenAdminFeatures, screenAdminEditUser, screenAdminEditUserGrants, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots:
		return true
	default:
		return false
	}
}

func grantRepositorySuggestions(form adminGrantForm, repositories []string) []string {
	query := strings.ToLower(strings.TrimSpace(form.Repository))
	seen := make(map[string]struct{}, len(repositories))
	suggestions := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		repository = strings.TrimSpace(repository)
		if repository == "" {
			continue
		}
		if _, ok := seen[repository]; ok {
			continue
		}
		seen[repository] = struct{}{}
		if query != "" && !strings.Contains(strings.ToLower(repository), query) {
			continue
		}
		suggestions = append(suggestions, repository)
	}
	return suggestions
}

func featureActionForKey(msg tea.KeyMsg, page ports.FeaturePage) (ports.FeatureAction, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return ports.FeatureAction{}, false
	}
	lookup := map[rune]string{
		'e': "enable",
		'x': "disable",
		'i': "install-runtime",
		'u': "upgrade-runtime",
		'b': "rollback-runtime",
	}
	actionID, ok := lookup[unicode.ToLower(msg.Runes[0])]
	if !ok {
		return ports.FeatureAction{}, false
	}
	for _, action := range page.Actions {
		if action.ID == actionID {
			return action, true
		}
	}
	return ports.FeatureAction{}, false
}

func featureActionHelp(page ports.FeaturePage) string {
	parts := []string{"Enter/r: refresh page"}
	for _, action := range page.Actions {
		switch action.ID {
		case "enable":
			parts = append(parts, "e: enable")
		case "disable":
			parts = append(parts, "x: disable")
		case "install-runtime":
			parts = append(parts, "i: install runtime")
		case "upgrade-runtime":
			parts = append(parts, "u: upgrade runtime")
		case "rollback-runtime":
			parts = append(parts, "b: rollback runtime")
		}
	}
	parts = append(parts, "Esc: back", "q: quit")
	return strings.Join(parts, " | ")
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

func renderConsoleWorkspace(title string, context string, body string, status string, help string, kind statusKind) string {
	theme := newAdminTheme()
	sections := []string{
		theme.title.Render(title),
		theme.context.Render(context),
		body,
	}
	if strings.TrimSpace(status) != "" {
		sections = append(sections, renderAdminStatus(theme, status, kind))
	}
	if strings.TrimSpace(help) != "" {
		sections = append(sections, theme.help.Render(help))
	}
	return theme.app.Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

// renderInspectionWorkspace's signature stays unchanged (design.md Decision
// 2: 0 of ~11 callers touched) — it always passes statusKindAuto, preserving
// today's substring-classification behavior for every screen that does not
// need an explicit kind.
func renderInspectionWorkspace(context string, body string, status string, help string) string {
	return renderConsoleWorkspace("Regixtry Console", context, body, status, help, statusKindAuto)
}

// renderConsoleTextSection wraps content in the themed bordered section,
// clipped to layout's row budget (design.md decision #3/#4).
func renderConsoleTextSection(content string, layout consoleLayout) string {
	return renderSection(newAdminTheme(), content, layout)
}

// renderManifest, renderBlobs, and renderUploads apply the same admin theme
// used everywhere else in the TUI (subheading for the section title, text
// for primary values, muted for secondary detail, selected/accent for the
// highlighted row) so the plain "Regixtry Console" screens (repositories,
// tags, manifest, blobs, uploads) read consistently with the admin screens
// instead of falling back to unstyled plain text.
func renderManifest(theme adminTheme, manifest appregixtry.ManifestDetails, signature appregixtry.SignatureStatusResult) string {
	lines := []string{
		theme.subheading.Render(fmt.Sprintf("Manifest · %s:%s", manifest.Repository, manifest.Reference)),
		fmt.Sprintf("%s %s", theme.muted.Render("Digest:"), theme.text.Render(manifest.Digest)),
		fmt.Sprintf("%s %s", theme.muted.Render("Media Type:"), theme.text.Render(manifest.MediaType)),
		fmt.Sprintf("%s %s", theme.muted.Render("Size:"), theme.text.Render(fmt.Sprintf("%d bytes", manifest.Size))),
		fmt.Sprintf("%s %s", theme.muted.Render("Blobs:"), theme.text.Render(fmt.Sprintf("%d", len(manifest.Blobs)))),
	}
	if len(manifest.Annotations) > 0 {
		keys := make([]string, 0, len(manifest.Annotations))
		for key := range manifest.Annotations {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("%s %s", theme.muted.Render("Annotation "+key+"="), theme.text.Render(manifest.Annotations[key])))
		}
	}
	lines = append(lines, renderSignatureLines(theme, signature)...)
	return strings.Join(lines, "\n")
}

// renderSignatureLines renders the manifest inspection view's Signature
// section from the SAME SignatureStatusResult/SignatureStatusDetail types
// Service.SignatureStatus already returns for the Console Tags table's
// Signed column -- no new fields. It renders only the fixed-vocabulary
// State, the `.sig` tag name, and the signature count; it never renders
// SignatureStatusDetail.Reason, key material, raw signature bytes, or trust
// configuration detail, mirroring the no-key-leakage discipline already
// enforced for signature-status/signing-policy-violation responses
// elsewhere in this codebase (internal/protocol/http/signature_status_test.go).
func renderSignatureLines(theme adminTheme, signature appregixtry.SignatureStatusResult) []string {
	if signature.Signature == nil {
		return []string{fmt.Sprintf("%s %s", theme.muted.Render("Signature:"), theme.text.Render("not signed"))}
	}

	lines := []string{
		fmt.Sprintf("%s %s", theme.muted.Render("Signature state:"), theme.text.Render(signature.State)),
	}
	if signature.Signature.Tag != "" {
		lines = append(lines, fmt.Sprintf("%s %s", theme.muted.Render("Signature tag:"), theme.text.Render(signature.Signature.Tag)))
	}
	lines = append(lines, fmt.Sprintf("%s %s", theme.muted.Render("Signature count:"), theme.text.Render(fmt.Sprintf("%d", signature.Signature.SignatureCount))))
	return lines
}

func renderBlobs(theme adminTheme, blobs BlobsModel) string {
	lines := []string{theme.subheading.Render("Blobs")}
	if len(blobs.Items) == 0 {
		return strings.Join(append(lines, theme.muted.Render("No blobs are linked to the selected manifest.")), "\n")
	}
	for index, blob := range blobs.Items {
		row := fmt.Sprintf("%s (%d bytes) [%s]", blob.Digest, blob.Size, blob.MediaType)
		if index == blobs.Selected {
			lines = append(lines, theme.selected.Render(row))
			continue
		}
		lines = append(lines, theme.text.Render(row))
	}
	return strings.Join(lines, "\n")
}

func renderUploads(theme adminTheme, uploads UploadsModel) string {
	lines := []string{theme.subheading.Render(fmt.Sprintf("Uploads · %s", uploads.Repository))}
	if len(uploads.Items) == 0 {
		return strings.Join(append(lines, theme.muted.Render("No in-progress uploads for this repository.")), "\n")
	}
	for _, upload := range uploads.Items {
		lines = append(lines, theme.text.Render(fmt.Sprintf("%s · %s · %d bytes", upload.ID, upload.Status, upload.Size)))
	}
	return strings.Join(lines, "\n")
}

func renderAdminLogin(theme adminTheme, form adminLoginForm) string {
	return theme.section.Render(strings.Join([]string{
		theme.subheading.Render("Operator Login"),
		renderTextField(theme, "Username", form.Username, form.Focus == loginFieldUsername),
		renderSecretField(theme, "Password", form.Password, form.Focus == loginFieldPassword),
	}, "\n"))
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
	m.adminView.Features = nil
	m.adminView.FeaturePage = ports.FeaturePage{}
	m.adminView.TrivyTab = trivyTabRuntime
	m.adminView.TrivyConfigModal = trivyConfigModal{}
	m.adminView.GitleaksConfigModal = gitleaksConfigModal{}
	m.adminView.TrivyScanRuns = nil
	m.adminView.TrivySelectedAlert = 0
	m.adminView.TrivyAlertsLoaded = false
	m.adminView.TrivySummaries = nil
	m.adminView.ScanHistoryModal = adminScanHistoryModal{}
	m.adminView.RepositoryOverrideModal = repositoryOverrideModal{}
	m.adminView.TrivyOverrides = nil
	m.adminView.SelectedGrant = 0
	m.adminView.SelectedFeature = 0
	m.adminView.SelectedToken = 0
	m.adminView.ResetPasswordForm = adminResetPasswordForm{}
	m.adminView.GrantForm = newAdminViewState().GrantForm
	m.adminView.TokenForm = adminTokenForm{}
	m.adminView.RevealedTokenSecret = ""
	m.adminView.RevealedTokenAccessor = ""
	m.adminView.RevealedTokenExpiresAt = time.Time{}
	m.adminView.Tables = newAdminViewState().Tables
}

func (m *Model) applyLoadedFeatures(features []ports.FeatureSummary) {
	preferredName := m.selectedFeatureName()
	m.adminView.Features = append([]ports.FeatureSummary(nil), features...)
	if len(m.adminView.Features) == 0 {
		m.adminView.SelectedFeature = 0
		m.adminView.FeaturePage = ports.FeaturePage{}
		return
	}
	selected := 0
	if preferredName != "" {
		for index, feature := range m.adminView.Features {
			if feature.Name == preferredName {
				selected = index
				break
			}
		}
	}
	m.adminView.SelectedFeature = boundedIndex(selected, len(m.adminView.Features))
	m.adminView.FeaturePage = ports.FeaturePage{}
	m.syncAdminTableHighlights()
}

func (m *Model) applyFeaturePage(page ports.FeaturePage) {
	m.adminView.FeaturePage = page
	m.adminView.TrivyConfigModal = trivyConfigModal{}
	m.adminView.GitleaksConfigModal = gitleaksConfigModal{}
	m.adminView.TrivyScanRuns = nil
	m.adminView.TrivySelectedAlert = 0
	m.adminView.TrivyAlertsLoaded = false
	m.adminView.TrivySummaries = nil
	m.adminView.ScanHistoryModal = adminScanHistoryModal{}
	m.adminView.RepositoryOverrideModal = repositoryOverrideModal{}
	m.adminView.TrivyOverrides = nil
	if page.Summary.Name == trivyFeatureName {
		m.adminView.TrivyTab = trivyTabRuntime
	}
	for index, feature := range m.adminView.Features {
		if feature.Name == page.Summary.Name {
			m.adminView.Features[index] = page.Summary
			m.adminView.SelectedFeature = index
			m.syncAdminTableHighlights()
			return
		}
	}
	m.syncAdminTableHighlights()
}

func (m Model) selectedFeatureName() string {
	if len(m.adminView.Features) == 0 {
		return ""
	}
	index := boundedIndex(m.adminView.SelectedFeature, len(m.adminView.Features))
	return m.adminView.Features[index].Name
}

func (m Model) isSelectedTrivyFeature() bool {
	name := strings.TrimSpace(m.adminView.FeaturePage.Summary.Name)
	if name == "" {
		name = strings.TrimSpace(m.selectedFeatureName())
	}
	return name == trivyFeatureName
}

// isSelectedGitleaksFeature mirrors isSelectedTrivyFeature for the gitleaks
// config modal's own opener, scoped to "gitleaks is the highlighted Built-in
// Features row" the same way Trivy's `c` is scoped to "trivy is highlighted".
func (m Model) isSelectedGitleaksFeature() bool {
	name := strings.TrimSpace(m.adminView.FeaturePage.Summary.Name)
	if name == "" {
		name = strings.TrimSpace(m.selectedFeatureName())
	}
	return name == gitleaksFeatureName
}

// isSelectedSigningFeature mirrors isSelectedTrivyFeature/
// isSelectedGitleaksFeature for signingPolicyModal's own opener, scoped to
// "signing is the highlighted Built-in Features row" (design.md
// Decision 11 piece 2).
func (m Model) isSelectedSigningFeature() bool {
	name := strings.TrimSpace(m.adminView.FeaturePage.Summary.Name)
	if name == "" {
		name = strings.TrimSpace(m.selectedFeatureName())
	}
	return name == signingFeatureName
}

func (m Model) toggleTrivyTab() (tea.Model, tea.Cmd) {
	if m.adminView.TrivyTab == trivyTabRepositoryAlerts {
		m.adminView.TrivyTab = trivyTabRuntime
		m.status = ""
		m.syncAdminTableHighlights()
		return m, nil
	}
	m.adminView.TrivyTab = trivyTabRepositoryAlerts
	m.rebuildAdminTables(m.adminTablesLayout())
	m.status = "Loading repository alerts..."
	return m, m.loadAdminScanRunsCmd("", 25)
}

// selectedScanSummary returns the Repository Alerts summary row the
// operator has highlighted (TrivySelectedAlert, shared with the Up/Down
// navigation in updateAdminFeaturesKey), used by Enter to determine which
// repository's scan history modal to open.
func selectedScanSummary(view AdminViewState) (repositorySummary, bool) {
	if len(view.TrivySummaries) == 0 {
		return repositorySummary{}, false
	}
	index := boundedIndex(view.TrivySelectedAlert, len(view.TrivySummaries))
	return view.TrivySummaries[index], true
}

func compareScanRuns(left ports.ScanRun, right ports.ScanRun) int {
	leftSeverity := highestSeverityRank(left)
	rightSeverity := highestSeverityRank(right)
	if leftSeverity != rightSeverity {
		return rightSeverity - leftSeverity
	}
	leftFixable := scanRunHasFixable(left)
	rightFixable := scanRunHasFixable(right)
	if leftFixable != rightFixable {
		if leftFixable {
			return -1
		}
		return 1
	}
	if left.CreatedAt.Equal(right.CreatedAt) {
		return strings.Compare(left.ID, right.ID)
	}
	if left.CreatedAt.After(right.CreatedAt) {
		return -1
	}
	return 1
}

func highestSeverityRank(run ports.ScanRun) int {
	if run.Critical > 0 {
		return 4
	}
	if run.High > 0 {
		return 3
	}
	if run.Medium > 0 {
		return 2
	}
	if run.Low > 0 {
		return 1
	}
	return 0
}

func scanRunHasFixable(run ports.ScanRun) bool {
	return run.HasFixable
}

func trivyConfigModalFromPage(page ports.FeaturePage) (trivyConfigModal, bool) {
	if page.Summary.Name != trivyFeatureName {
		return trivyConfigModal{}, false
	}
	modal := trivyConfigModal{Open: true, Focus: trivyConfigFieldScheduleEnabled}
	for _, section := range page.Sections {
		if section.ID != "config" {
			continue
		}
		for _, field := range section.Fields {
			switch field.Label {
			case "Schedule Enabled":
				modal.ScheduleEnabled, _ = strconv.ParseBool(strings.TrimSpace(field.Value))
			case "Interval":
				modal.Interval = strings.TrimSpace(field.Value)
			case "Timeout":
				modal.Timeout = strings.TrimSpace(field.Value)
			case "Registry Reachable URL":
				modal.RegistryReachableURL = strings.TrimSpace(field.Value)
			case "Max Concurrency":
				modal.MaxConcurrency = strings.TrimSpace(field.Value)
			}
		}
	}
	if modal.Interval == "" || modal.Timeout == "" || modal.MaxConcurrency == "" {
		return trivyConfigModal{}, false
	}
	return modal, true
}

func nextTrivyConfigField(field trivyConfigField) trivyConfigField {
	if field >= trivyConfigFieldMaxConcurrency {
		return trivyConfigFieldScheduleEnabled
	}
	return field + 1
}

// gitleaksConfigModalFromPage mirrors trivyConfigModalFromPage at gitleaks'
// narrower 3-field scope: Enabled comes from the page Header (every
// feature's Header carries it, per buildFeaturePage), Timeout and
// MaxConcurrency come from the generic "config" section's Fields (shared
// with Trivy's page shape; ScheduleEnabled/Interval/RegistryReachableURL are
// present in that same section but deliberately unused here).
func gitleaksConfigModalFromPage(page ports.FeaturePage) (gitleaksConfigModal, bool) {
	if page.Summary.Name != gitleaksFeatureName {
		return gitleaksConfigModal{}, false
	}
	modal := gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldEnabled}
	for _, field := range page.Header {
		if field.Label == "Enabled" {
			modal.Enabled, _ = strconv.ParseBool(strings.TrimSpace(field.Value))
		}
	}
	for _, section := range page.Sections {
		if section.ID != "config" {
			continue
		}
		for _, field := range section.Fields {
			switch field.Label {
			case "Timeout":
				modal.Timeout = strings.TrimSpace(field.Value)
			case "Max Concurrency":
				modal.MaxConcurrency = strings.TrimSpace(field.Value)
			}
		}
	}
	if modal.Timeout == "" || modal.MaxConcurrency == "" {
		return gitleaksConfigModal{}, false
	}
	return modal, true
}

func nextGitleaksConfigField(field gitleaksConfigField) gitleaksConfigField {
	if field >= gitleaksConfigFieldMaxConcurrency {
		return gitleaksConfigFieldEnabled
	}
	return field + 1
}

func (m *Model) deleteTrivyConfigModalRune() {
	switch m.adminView.TrivyConfigModal.Focus {
	case trivyConfigFieldInterval:
		m.adminView.TrivyConfigModal.Interval = trimLastRune(m.adminView.TrivyConfigModal.Interval)
	case trivyConfigFieldTimeout:
		m.adminView.TrivyConfigModal.Timeout = trimLastRune(m.adminView.TrivyConfigModal.Timeout)
	case trivyConfigFieldRegistryReachableURL:
		m.adminView.TrivyConfigModal.RegistryReachableURL = trimLastRune(m.adminView.TrivyConfigModal.RegistryReachableURL)
	case trivyConfigFieldMaxConcurrency:
		m.adminView.TrivyConfigModal.MaxConcurrency = trimLastRune(m.adminView.TrivyConfigModal.MaxConcurrency)
	}
}

func (m *Model) appendTrivyConfigModalRunes(value string) {
	if value == "" {
		return
	}
	switch m.adminView.TrivyConfigModal.Focus {
	case trivyConfigFieldInterval:
		m.adminView.TrivyConfigModal.Interval += value
	case trivyConfigFieldTimeout:
		m.adminView.TrivyConfigModal.Timeout += value
	case trivyConfigFieldRegistryReachableURL:
		m.adminView.TrivyConfigModal.RegistryReachableURL += value
	case trivyConfigFieldMaxConcurrency:
		m.adminView.TrivyConfigModal.MaxConcurrency += value
	}
}

func (m *Model) deleteGitleaksConfigModalRune() {
	switch m.adminView.GitleaksConfigModal.Focus {
	case gitleaksConfigFieldTimeout:
		m.adminView.GitleaksConfigModal.Timeout = trimLastRune(m.adminView.GitleaksConfigModal.Timeout)
	case gitleaksConfigFieldMaxConcurrency:
		m.adminView.GitleaksConfigModal.MaxConcurrency = trimLastRune(m.adminView.GitleaksConfigModal.MaxConcurrency)
	}
}

func (m *Model) appendGitleaksConfigModalRunes(value string) {
	if value == "" {
		return
	}
	switch m.adminView.GitleaksConfigModal.Focus {
	case gitleaksConfigFieldTimeout:
		m.adminView.GitleaksConfigModal.Timeout += value
	case gitleaksConfigFieldMaxConcurrency:
		m.adminView.GitleaksConfigModal.MaxConcurrency += value
	}
}

// gitleaksConfigInputFromModal mirrors trivyConfigInputFromModal at
// gitleaks' narrower 3-field scope: only Enabled/Timeout/MaxConcurrency are
// set on the returned FeatureConfigureInput, so mergeFeatureSettings leaves
// ScheduleEnabled/Interval/RegistryReachableURL untouched on the stored row
// (design.md's nil-pointer-is-a-no-op merge semantics).
func (m Model) gitleaksConfigInputFromModal() (ports.FeatureConfigureInput, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(m.adminView.GitleaksConfigModal.Timeout))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid timeout: %w", err)
	}
	maxConcurrency, err := strconv.Atoi(strings.TrimSpace(m.adminView.GitleaksConfigModal.MaxConcurrency))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid max concurrency: %w", err)
	}
	enabled := m.adminView.GitleaksConfigModal.Enabled
	return ports.FeatureConfigureInput{
		Enabled:        &enabled,
		Timeout:        &timeout,
		MaxConcurrency: &maxConcurrency,
	}, nil
}

func (m Model) trivyConfigInputFromModal() (ports.FeatureConfigureInput, error) {
	interval, err := time.ParseDuration(strings.TrimSpace(m.adminView.TrivyConfigModal.Interval))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid interval: %w", err)
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(m.adminView.TrivyConfigModal.Timeout))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid timeout: %w", err)
	}
	maxConcurrency, err := strconv.Atoi(strings.TrimSpace(m.adminView.TrivyConfigModal.MaxConcurrency))
	if err != nil {
		return ports.FeatureConfigureInput{}, fmt.Errorf("invalid max concurrency: %w", err)
	}
	registryURL := strings.TrimSpace(m.adminView.TrivyConfigModal.RegistryReachableURL)
	scheduleEnabled := m.adminView.TrivyConfigModal.ScheduleEnabled
	return ports.FeatureConfigureInput{
		ScheduleEnabled:      &scheduleEnabled,
		Interval:             &interval,
		Timeout:              &timeout,
		RegistryReachableURL: &registryURL,
		MaxConcurrency:       &maxConcurrency,
	}, nil
}

func (m Model) moveAdminFeatureSelection(delta int) (tea.Model, tea.Cmd) {
	if len(m.adminView.Features) == 0 {
		m.adminView.SelectedFeature = 0
		m.adminView.FeaturePage = ports.FeaturePage{}
		return m, nil
	}
	m.adminView.SelectedFeature = boundedIndex(m.adminView.SelectedFeature+delta, len(m.adminView.Features))
	m.adminView.FeaturePage = ports.FeaturePage{}
	m.syncAdminTableHighlights()
	m.status = fmt.Sprintf("Loading feature page for %s...", m.selectedFeatureName())
	return m, m.loadAdminFeaturePageCmd(m.selectedFeatureName())
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

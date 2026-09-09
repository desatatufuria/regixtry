package tui

import (
	"context"
	"fmt"
	stdhttp "net/http"
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
	"regixtry/internal/domain/signing"
	"regixtry/internal/infra/release"
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

// WithCurrentVersion sets the running binary's own version (main.buildVersion
// in cmd/regixtry, which internal/tui cannot import directly), used to
// decide whether a background update check finds a genuinely newer release.
// An empty value (the caller's choice, e.g. an unbuilt dev binary) leaves
// the check permanently disabled -- checkForUpdateCmd never fires a
// request with nothing to compare against.
func WithCurrentVersion(version string) Option {
	return func(m *Model) {
		m.currentVersion = strings.TrimSpace(version)
	}
}

type QueryService interface {
	RepositorySummaries(ctx context.Context, limit int, after string) ([]appregixtry.RepositorySummary, error)
	TagDetails(ctx context.Context, repositoryName string, limit int, after string) ([]appregixtry.TagDetails, error)
	ResolveManifest(ctx context.Context, repositoryName string, reference string) (appregixtry.ManifestDetails, error)
	Uploads(ctx context.Context, repositoryName string) ([]appregixtry.UploadDetails, error)
	SignatureStatus(ctx context.Context, repositoryName string, reference string) (appregixtry.SignatureStatusResult, error)
	// DeleteManifest backs the Tags screen's delete-tag "d" key/confirm flow
	// (blob-garbage-collection change), calling the already-tested
	// (*appregixtry.Service).DeleteManifest directly in-process -- not
	// through AdminClient/HTTP, since delete-enablement is Service's own
	// deleteEnabled gate, not an admin-session concern.
	DeleteManifest(ctx context.Context, repositoryName string, reference string) (appregixtry.DeletionDetails, error)
	// GetUpdateChannel backs checkForUpdateCmd's channel resolution, called
	// directly in-process exactly like DeleteManifest above -- QueryService
	// is always a real, local *appregixtry.Service regardless of whether
	// -api-base-url is set for the separate admin-panel HTTP client, so this
	// never needs the AdminClient/HTTP path the write side
	// (AdminClient.SetUpdateChannel) uses.
	GetUpdateChannel(ctx context.Context) (string, error)
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
	// Confirm holds the delete-tag pending confirm opened by the "d" key
	// (design.md Decision E/D7): the same confirmPrompt primitive
	// AdminViewState.Confirm uses, composed here identically -- this is the
	// retirement of TagsModel's own former PendingDelete string field, one
	// of the two confirm patterns D7 requires collapse to exactly one.
	Confirm confirmPrompt
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
	// screenSecurityGitleaksRepos/screenSecuritySigningRepos are Gitleaks'
	// and Signing's own dedicated per-repository override list screens
	// (design.md D8/D3), symmetric to screenSecurityTrivyRepos below, each
	// reachable from their own feature's config screen via 'o' without ever
	// entering Trivy's screen.
	screenSecurityGitleaksRepos screen = "security-gitleaks-repos"
	screenSecuritySigningRepos  screen = "security-signing-repos"
	// screenSecurityTrivy/screenSecurityTrivyRepos are Trivy's own peer
	// screens (Phase 11, design.md Decision I): trivyConfigScreen (Runtime
	// tab equivalent: config, scan policy, enable/disable/install/upgrade
	// actions) and trivyReposScreen (Repository Alerts tab equivalent),
	// reached from securityMenuScreen and from each other via Tab.
	screenSecurityTrivy      screen = "security-trivy"
	screenSecurityTrivyRepos screen = "security-trivy-repos"
	// screenSecurityGitleaksConfig/screenSecuritySigningConfig are
	// Gitleaks'/Signing's own config screens (Phase 11 resolved-gap
	// addendum), promoting gitleaksConfigScreen/signingConfigScreen from
	// overlays on the legacy screenAdminFeatures (Slice 1/Phase 12.3) to
	// properly addressable top-level screens, symmetric to
	// screenSecurityTrivy.
	screenSecurityGitleaksConfig screen = "security-gitleaks-config"
	screenSecuritySigningConfig  screen = "security-signing-config"
	// screenAdminMenu is the post-login domain-menu landing screen (Phase
	// 18, design.md Decision I/D8): 4 domain rows (Browse, Security &
	// Compliance, Identity & Access, Operations). It replaces
	// screenAdminUsers as the admin panel's own root.
	screenAdminMenu screen = "admin-menu"
	// screenAdminOperations is the Operations domain (Phase 18, D9): 2 rows
	// (Scan Runs, Secret Scan Findings).
	screenAdminOperations screen = "admin-operations"
	// screenAdminScanRuns/screenAdminSecretFindings are the two
	// scanRunsScreen-backed repository pickers Operations' own rows
	// navigate to (Phase 19, D9): the only difference between them is which
	// scanHistoryScreen tab Enter defaults to.
	screenAdminScanRuns       screen = "admin-scan-runs"
	screenAdminSecretFindings screen = "admin-secret-findings"
	// screenAdminUpdateChannel is Operations' third row (tui-update-check
	// feature): the admin-only screen that sets the server-side update
	// channel (GET /update-channel is public and pre-login; PUT
	// /admin/v1/update-channel is the only write path, admin-gated -- this
	// screen is the sole caller of the write side).
	screenAdminUpdateChannel screen = "admin-update-channel"
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

	// adminScreens holds every migrated screen sub-model (design.md
	// Decision B), keyed by screenSlot. It is an array of an interface
	// type, never a slice or map, so Model's ordinary value-copy semantics
	// hold for it exactly like every other Model field.
	adminScreens adminScreenSet

	// currentVersion/updateAvailable back the background update-check
	// banner. currentVersion empty means "no version info available" (an
	// unbuilt dev binary, or the option simply not passed) --
	// checkForUpdateCmd never fires a request in that case. The channel
	// itself is no longer client-side state (it moved to a server-side
	// setting, Service.GetUpdateChannel/AdminClient.SetUpdateChannel) --
	// checkForUpdateCmd resolves it fresh on every check. updateAvailable
	// is the newer version's tag once a check confirms one exists; empty
	// means "nothing to show," the default, always-safe state.
	currentVersion  string
	updateAvailable string
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
// screenEnv builds the read-only view of the outside world a migrated
// sub-model or a legacy model's embedded confirmPrompt may read
// (design.md's screenEnv). Layout uses adminTablesLayout's own admin-screen
// budget since, in Slice 1, only admin-side screens/overlays consume it.
func (m Model) screenEnv() screenEnv {
	return screenEnv{
		Client:            m.adminClient,
		Session:           m.adminSession,
		Layout:            m.adminTablesLayout(),
		KnownRepositories: m.repositories.Names(),
		Now:               m.now,
	}
}

func (m Model) contentBudget(status, help string) consoleLayout {
	layout := contentBudget(m.viewport.Width, m.viewport.Height, status, help)
	layout.Scroll = m.bodyScroll
	return layout
}

// adminTablesLayout computes the consoleLayout used to size admin tables at
// rebuild time (design.md decision #6). It uses securityMenuScreen's own
// help text as a representative stand-in (Phase 11: adminFeatureHelp,
// screenAdminFeatures' pre-migration help text, is retired) — callers of
// rebuildAdminTables/screenEnv() run from Update handlers, not View(), so no
// screen-specific help string is otherwise available; this is the same
// approximation the pre-change code already made for every OTHER migrated
// screen's own screenEnv().Layout, just now keyed to a static var instead of
// a dynamic view read.
func (m Model) adminTablesLayout() consoleLayout {
	return m.contentBudget(m.status, shortHelpView(newAdminTheme(), securityMenuKeys))
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

// tagDeletedMsg carries the repository/tag pair a deleteTagCmd targeted,
// mirroring adminRobotDeletedMsg's own shape: after a successful delete the
// screen reloads screenTags' own list (loadTagsCmd) rather than reusing a
// mutation response body.
type tagDeletedMsg struct {
	repository string
	tag        string
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

// adminFeaturePageLoadedMsg carries the feature name it was requested for
// (stamped by loadAdminFeaturePageCmd) so a per-feature screen (Phase 11:
// trivyConfigScreen/gitleaksConfigScreen/signingConfigScreen, each fixed to
// its own feature) can reliably filter a broadcast result even on error,
// when page.Summary.Name itself is unavailable (design.md Decision H).
type adminFeaturePageLoadedMsg struct {
	feature string
	page    ports.FeaturePage
	err     error
}

// adminFeatureActionCompletedMsg mirrors adminFeaturePageLoadedMsg's own
// feature-stamping reasoning: enable/disable/install/upgrade/rollback is
// dispatched by three different feature screens now, and result carries no
// feature identity of its own.
type adminFeatureActionCompletedMsg struct {
	feature string
	result  ports.FeatureActionResult
	err     error
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

// adminUpdateChannelLoadedMsg/adminUpdateChannelUpdatedMsg back
// updateChannelScreen (tui-update-check feature), mirroring
// adminScanPolicyLoadedMsg/adminScanPolicyUpdatedMsg's own shape.
type adminUpdateChannelLoadedMsg struct {
	channel string
	err     error
}

type adminUpdateChannelUpdatedMsg struct {
	channel string
	err     error
}

type adminSigningPolicyLoadedMsg struct {
	settings ports.SigningPolicySettings
	err      error
}

type adminSigningPolicyUpdatedMsg struct {
	settings ports.SigningPolicySettings
	err      error
}

// adminSigningKeyUsageLoadedMsg carries the result of a trustedKeyList
// delete-key usage-count lookup (CountSigningKeyUsage). It carries no
// requester identity (unlike adminRepositoryOverrideLoadedMsg's
// repository/feature): only one trustedKeyList is ever mid-delete at a
// time (signingConfigScreen's modal and overrideEditor's editor are
// mutually exclusive overlays), and trustedKeyList.applyUsageLoaded itself
// discards a response that arrives when it is not expecting one
// (usageLoading already false).
type adminSigningKeyUsageLoadedMsg struct {
	count  int
	capped bool
	err    error
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

// adminRepositoryScanSummariesLoadedMsg carries the result of loading the
// Repository Alerts table: one row per repository (ports.RepositoryScanSummary),
// collapsed server-side before limit so no repository can be crowded out by
// another's rescans (replaces the old raw-scan_runs adminScanRunsLoadedMsg).
type adminRepositoryScanSummariesLoadedMsg struct {
	summaries []ports.RepositoryScanSummary
	err       error
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

// adminRobotDeletedMsg carries only the robot's ID/username, the same
// shape adminRobotEnabledMsg uses: after a successful delete the screen
// reloads screenAdminRobots' own list rather than reusing a mutation
// response body.
type adminRobotDeletedMsg struct {
	robotID  string
	username string
	err      error
}

type startupLoginMsg struct{}

// updateCheckCompletedMsg is checkForUpdateCmd's result: a best-effort,
// silent background check, never surfaced to the operator as an error --
// see the Update handler for why err is intentionally not folded into
// model.err.
type updateCheckCompletedMsg struct {
	latestVersion string
	err           error
}

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
		// Migrated screens with their own baked-in bubbletable.Model
		// (securityMenuScreen/trivyReposScreen) need the same explicit
		// resize-rebuild, broadcast like every other non-key message
		// (design.md Decision H).
		m.adminScreens, _ = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case catalogLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, m.checkForUpdateCmd()
		}
		m.repositories = RepositoriesModel{Items: append([]appregixtry.RepositorySummary(nil), msg.result...)}
		m.bodyScroll = 0
		if len(m.repositories.Items) == 0 {
			m.screen = screenEmpty
			return m, m.checkForUpdateCmd()
		}
		m.screen = screenRepositories
		m.rebuildRepositoriesTable(m.repositoriesTableLayout())
		m.loadingText = ""
		return m, m.checkForUpdateCmd()
	case startupLoginMsg:
		m.screen = screenAdminLogin
		m.loadingText = ""
		m.adminReturn = screenRepositories
		return m, m.checkForUpdateCmd()
	case updateCheckCompletedMsg:
		// Best-effort and silent by design: a fetch error, an unparseable
		// candidate, or "already up to date" all leave updateAvailable empty
		// -- never surfaced as model.err, never a banner. Only a confirmed
		// genuinely newer version (release.IsNewerVersion) populates it.
		if msg.err != nil {
			return m, nil
		}
		if newer, err := release.IsNewerVersion(msg.latestVersion, m.currentVersion); err == nil && newer {
			m.updateAvailable = msg.latestVersion
		}
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
	case tagDeletedMsg:
		// Not an AdminClient/HTTP call (QueryService.DeleteManifest runs
		// in-process against *appregixtry.Service directly), so there is no
		// admin session to expire here -- just surface the error and clear
		// the pending state so the operator is never stuck on a failed
		// confirm.
		if msg.err != nil {
			m.tags.Confirm = confirmPrompt{}
			m.status = msg.err.Error()
			return m, nil
		}
		m.tags.Confirm = confirmPrompt{}
		m.status = fmt.Sprintf("Tag %q deleted. Refreshing tags...", msg.tag)
		return m, m.loadTagsCmd(msg.repository)
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
		// Phase 18 (design.md Decision I): the operator lands on the
		// domain-grouped menu, not directly on screenAdminUsers (Identity &
		// Access is now one domain among four, reached via its own row).
		// The Users list still loads immediately in the background exactly
		// as before (design.md's "narrow scope" precedent): no screen along
		// the legacy screenAdminUsers/screenAdminEditUser/securityMenuScreen
		// Esc-back chain ever re-fires loadAdminUsersCmd itself, so this
		// single post-login load remains the only source, just no longer
		// paired with landing directly on the screen it feeds.
		m.status = ""
		m.screen = screenAdminMenu
		m.adminScreens[slotAdminMenu] = newAdminMenuScreen()
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
		// Phase 11 (design.md Decision I / the resolved-gap addendum):
		// securityMenuScreen owns Features/SelectedFeature/Tables.Features
		// now; the central handler only sets global status and broadcasts
		// (design.md Decision H) -- it no longer chain-loads a FeaturePage,
		// since each feature's own screen loads its own page independently
		// once navigated into.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		if len(msg.features) == 0 {
			m.status = "No built-in features found."
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminFeaturePageLoadedMsg:
		// Broadcast only (design.md Decision H): whichever of
		// trivyConfigScreen/gitleaksConfigScreen/signingConfigScreen this
		// was requested for (msg.feature) applies it to its own page.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminFeatureActionCompletedMsg:
		// Broadcast only: the requesting screen (msg.feature) clears its own
		// confirm and reloads its own page; this handler only sets the
		// global status line (design.md Decision H).
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		m.status = strings.TrimSpace(msg.result.Message)
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminFeatureConfiguredMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		m.status = "Configuration saved."
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminScanPolicyLoadedMsg:
		// Best-effort, matching the badge/modal's fail-quiet posture: a
		// load failure just broadcasts (each screen leaves its own policy
		// copy at its zero value) rather than surfacing a blocking status
		// error over an otherwise-successful feature page load.
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case adminScanPolicyUpdatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		m.status = "Vulnerability policy saved."
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminUpdateChannelLoadedMsg:
		// Mirrors adminScanPolicyLoadedMsg's own fail-quiet posture: a load
		// failure just broadcasts (updateChannelScreen shows its own error,
		// never a blocking status line) rather than being surfaced here.
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case adminUpdateChannelUpdatedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		m.status = "Update channel saved."
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminSigningPolicyLoadedMsg:
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case adminSigningPolicyUpdatedMsg:
		// signingConfigScreen's own policy/cfg-refresh handling is reached
		// via the routeAdminMsg broadcast below (design.md Decision H) --
		// SigningPolicy fully migrated onto signingConfigScreen in Phase 11
		// (closing the Phase 12.3 disclosed deviation: the badge that used
		// to be its second reader no longer exists).
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			var cmd tea.Cmd
			m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, cmd
		}
		m.status = "Signing policy saved."
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminSigningKeyUsageLoadedMsg:
		// Broadcast like adminSigningPolicyLoadedMsg above (design.md
		// Decision H): whichever screen holds a trustedKeyList mid-delete
		// (signingConfigScreen's modal or an open signing overrideEditor)
		// reflects the usage count and opens its confirm; every other
		// occupied slot ignores it via its own type switch /
		// trustedKeyList.applyUsageLoaded's own usageLoading guard.
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case adminRepositoryOverrideLoadedMsg:
		// The uniform overrideEditor (design.md Decision F) is broadcast to
		// (design.md Decision H): whichever screen is holding an open
		// editor for this exact repository+feature reflects it; every other
		// occupied slot ignores it via its own type switch.
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminRepositoryOverrideSavedMsg:
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		if msg.err == nil {
			if msg.exists {
				m.status = "Repository override saved."
			} else {
				m.status = "Repository override cleared."
			}
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case featureOverridesLoadedMsg:
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case navigateMsg:
		// The router's own navigation channel (design.md screen.go
		// "navigate"): a migrated screen asks to switch the active
		// top-level screen (e.g. Esc on a repos screen back to
		// screenAdminFeatures, Tab between Trivy's peer screens, Enter from
		// securityMenuScreen into a feature's own screen) without ever
		// writing m.screen itself. Lazily mounts+Inits the target the first
		// time it is visited (newAdminScreenFor, screen.go) -- an
		// already-mounted target's slot is left exactly as it was, so
		// navigating back to it never discards its state.
		m.screen = msg.To
		m.status = ""
		if slot, ok := slotFor(msg.To); ok && m.adminScreens[slot] == nil {
			next := newAdminScreenFor(msg.To)
			m.adminScreens[slot] = next
			if next != nil {
				return m, next.Init(m.screenEnv())
			}
		}
		return m, nil
	case openScanHistoryMsg:
		// scanHistoryScreen is never m.screen-addressed (screen.go's
		// slotScanHistory doc comment) -- mounted directly at its own
		// dedicated slot, m.screen left untouched (design.md Decision B: a
		// migrated screen cannot write to state it does not own, mirrored
		// here for the parent constructing a fresh sub-model value).
		next := scanHistoryScreen{
			returnTo: msg.returnTo,
			modal: adminScanHistoryModal{
				Open: true, Repository: msg.repository, Tabs: newAdminScanHistoryTabs(),
				ActiveTab: msg.activeTab, Loading: true,
			},
		}
		m.adminScreens[slotScanHistory] = next
		m.status = fmt.Sprintf("Loading scan history for %s...", adminFirstNonEmpty(msg.repository, "repository"))
		return m, next.Init(m.screenEnv())
	case returnToInspectionMsg:
		return m.returnToInspection(), nil
	case adminRepositoryOverridesListLoadedMsg:
		// Best-effort, matching the ScanPolicy load's fail-quiet posture: a
		// load failure just broadcasts (trivyReposScreen's table renders
		// without the "scanning disabled" annotation) rather than surfacing
		// a blocking status error over an otherwise-successful alerts load.
		// trivyReposScreen filters this itself (typed.feature ==
		// trivyFeatureName), mirroring the pre-change central handler's own
		// filter (design.md Decision H).
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case adminFeatureRuntimeMutatedMsg:
		// Broadcast only (design.md Decision H): the requesting screen
		// (msg.name) reloads its own page; this handler only sets the
		// global status line. Phase 11 deviation from the pre-change
		// central handler: no longer forces m.screen back to
		// screenAdminFeatures -- each feature's own screen stays active and
		// refreshes itself in place, consistent with
		// adminFeatureActionCompletedMsg/adminFeatureConfiguredMsg above.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.status = fmt.Sprintf("Managed runtime %s for %q at %s.", msg.action, msg.name, adminFirstNonEmpty(strings.TrimSpace(msg.state.ActiveVersion), adminFirstNonEmpty(strings.TrimSpace(string(msg.state.Status)), "unknown")))
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminRepositoryScanSummariesLoadedMsg:
		// Broadcast only (design.md Decision H): trivyReposScreen owns
		// TrivySummaries/TrivyScanRuns/TrivySelectedAlert/TrivyAlertsLoaded
		// now (Phase 11) and chains its own loadTrivyRepositoryOverridesListCmd
		// follow-up from inside its own Update.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		if len(msg.summaries) == 0 {
			m.status = "No repository alerts found."
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
		// rebuildAdminTables no longer builds this screen's own table (Phase
		// 11), but still keeps AdminViewState.Layout's Primary/Compact split
		// fresh -- a handful of tests read it as a proxy for "the row budget
		// this screen was sized against", mirroring the pre-change
		// unconditional call here.
		m.rebuildAdminTables(m.adminTablesLayout())
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminScanRunDetailLoadedMsg:
		// Broadcast only (design.md Decision H): scanHistoryScreen owns the
		// stale-response/digest-per-run-correctness filtering itself now
		// (screen_scan_history.go), mirroring adminRepositoryScanSummariesLoadedMsg's
		// own broadcast-only shape above.
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case adminSecretScanFindingsLoadedMsg:
		// Broadcast only (design.md Decision H): informational only (spec.md
		// "Informational Findings Only"), scanHistoryScreen itself never
		// treats a load failure as a blocking error (screen_scan_history.go).
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
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
		// Broadcast only (design.md Decision H): scanHistoryScreen owns the
		// stale-response/repository-match filtering and its own
		// loadScanRunDetailCmd chain-follow-up itself now
		// (screen_scan_history.go).
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		if msg.err == nil {
			m.status = ""
		}
		return m, cmd
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
		m.adminView.Confirm = confirmPrompt{}
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
		m.adminView.Confirm = confirmPrompt{}
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
		m.adminView.Confirm = confirmPrompt{}
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
		m.adminView.Confirm = confirmPrompt{}
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
		m.adminView.Confirm = confirmPrompt{}
		verb := "disabled"
		if msg.enabled {
			verb = "enabled"
		}
		m.status = fmt.Sprintf("Robot %s. Refreshing robots...", verb)
		m.screen = screenAdminRobots
		return m, m.loadAdminRobotsCmd()
	case adminRobotDeletedMsg:
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminView.Confirm = confirmPrompt{}
		m.status = fmt.Sprintf("Robot %q deleted. Refreshing robots...", msg.username)
		m.screen = screenAdminRobots
		return m, m.loadAdminRobotsCmd()
	}

	return m, nil
}

// Too-small guard message text per design.md's "Interfaces / Contracts"
// section: plain (not theme.section, which is a fixed 88-wide box).
const terminalTooSmallTemplate = "Terminal too small\nRegixtry needs at least %dx%d. Current: %dx%d.\nResize, or press q to quit."

// View renders the too-small guard as-is (a bespoke plain string, never
// bannered), then wraps every real screen's render in withUpdateBanner --
// chrome that must appear regardless of which screen is active, without
// threading a banner parameter through renderConsoleWorkspace/
// renderInspectionWorkspace's ~11 existing call sites (design.md Decision 2:
// "0 of ~11 callers touched" -- this wrapper keeps that true for the banner
// too).
func (m Model) View() string {
	// Guarded here (design decision #7) rather than in Update: View() is the
	// single funnel both the interactive program loop and the --snapshot
	// CLI path render through.
	if m.viewport.Width < minViewportWidth || m.viewport.Height < minViewportHeight {
		return fmt.Sprintf(terminalTooSmallTemplate, minViewportWidth, minViewportHeight, m.viewport.Width, m.viewport.Height)
	}
	return m.withUpdateBanner(m.viewScreen())
}

// withUpdateBanner prepends a version line above body -- either "a newer
// release is available" (when checkForUpdateCmd has confirmed one exists,
// already naming the current version via its own "(you're on vA.B.C)"
// suffix so the two facts never need two separate lines) or, absent that, a
// standalone baseline "regixtry vX.Y.Z" line whenever the current version
// is known at all -- an operator should never have to guess which build
// they're running just because nothing newer happens to exist yet. Neither
// line renders when currentVersion is empty (an unbuilt dev binary): never
// fabricate a version string.
func (m Model) withUpdateBanner(body string) string {
	if m.currentVersion == "" {
		return body
	}
	theme := newAdminTheme()
	var line string
	if m.updateAvailable != "" {
		line = fmt.Sprintf("A newer regixtry release is available: %s (you're on %s)", m.updateAvailable, m.currentVersion)
	} else {
		line = fmt.Sprintf("regixtry %s", m.currentVersion)
	}
	// Deliberately theme.muted, not theme.success, and appended below body
	// rather than prepended above it: this is an ambient status line, not
	// an alert -- it must never compete with the screen's own title/content
	// for the operator's attention.
	return lipgloss.JoinVertical(lipgloss.Left, body, theme.muted.Render(line))
}

func (m Model) viewScreen() string {
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
	case screenAdminUsers, screenAdminFeatures, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens, screenAdminCreateToken, screenRepoAdminGrants, screenRepoAdminAddGrant, screenAdminRobots, screenAdminCreateRobot, screenSecurityGitleaksRepos, screenSecuritySigningRepos, screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminMenu, screenAdminOperations, screenAdminScanRuns, screenAdminSecretFindings, screenAdminUpdateChannel:
		help := adminScreenHelp(m.screen, m.adminView)
		if slot, ok := slotFor(m.screen); ok && m.adminScreens[slot] != nil {
			// Migrated top-level screen: help is derived from the screen's
			// own Keys(), never a separately hand-written string (spec.md
			// "Keymap-Derived Help Cannot Drift").
			help = shortHelpView(newAdminTheme(), m.adminScreens[slot].Keys())
		}
		layout := m.contentBudget(m.status, help)
		return renderAdminWorkspace(m.screen, m.adminSession, m.adminView, m.repositories.Names(), m.status, layout, m.now(), m.adminScreens)
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
	// The Tags screen's delete-tag pending-confirm check runs first, ahead
	// of every other case below: while a delete is pending, Enter/Esc must
	// mean "confirm"/"cancel this confirm", not the screen's ordinary
	// inspect-manifest/back-to-Repositories behavior those same keys
	// otherwise trigger further down this switch.
	case m.screen == screenTags && m.tags.Confirm.Active() && isEnterKey(msg):
		next, cmd, _ := m.tags.Confirm.update(m.screenEnv(), msg)
		m.tags.Confirm = next
		return m, cmd
	case m.screen == screenTags && m.tags.Confirm.Active() && isEscKey(msg):
		next, _, _ := m.tags.Confirm.update(m.screenEnv(), msg)
		m.tags.Confirm = next
		m.status = ""
		return m, nil
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
	case isRuneKey(msg, 'd') && m.screen == screenTags:
		// Only "d" (not "d"/"x", matching robot-delete's own single-key
		// precedent in updateAdminRobotsKey) -- placed ahead of the
		// screenManifest/Blobs/Uploads case below so it never falls through
		// to that unrelated v1 placeholder.
		if tag, ok := m.selectedTag(); ok {
			repository := m.tags.Repository
			service := m.service
			ctx := m.ctx
			// onConfirm ignores env and closes over service/ctx/repository/tag
			// directly instead: QueryService.DeleteManifest runs in-process
			// (not AdminClient-backed), so there is no session token to go
			// stale the way design.md's "receive env at confirm time" guard
			// protects against -- capturing these here is exactly as safe as
			// capturing them at open time already was in the pre-change code.
			m.tags.Confirm = newConfirmPrompt("", "", "delete", "", func(screenEnv) tea.Cmd {
				return func() tea.Msg {
					_, err := service.DeleteManifest(ctx, repository, tag)
					return tagDeletedMsg{repository: repository, tag: tag, err: err}
				}
			})
			m.status = fmt.Sprintf("Delete tag %q from %q? This action cannot be undone. (Enter: delete | Esc: cancel)", tag, m.tags.Repository)
		} else {
			m.status = "No tag selected to delete."
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
	// Trivy's config/scan-policy modals, gitleaks' config modal, and
	// signing's policy modal are no longer overlays pre-dispatched here
	// (Phase 11 promotes trivyConfigScreen/gitleaksConfigScreen/
	// signingConfigScreen to top-level screens, addressed via slotFor):
	// each now handles its own modal state inside its own Update, reached
	// through the ordinary routeAdminKey path below. Trivy's own override
	// editor is likewise now embedded in trivyReposScreen directly (mirrors
	// featureOverridesScreen), so the former slotTrivyOverride pre-dispatch
	// branch is also gone.
	// scanHistoryScreen (Phase 19) is never m.screen-addressed (screen.go's
	// slotScanHistory doc comment), so it cannot be reached through
	// routeAdminKey's ordinary slotFor(m.screen) dispatch below -- this
	// mirrors the pre-migration ScanHistoryModal.Active() precedence check
	// exactly (same position: before 'l' logout, before Confirm, before
	// 'q' quit), just re-pointed at its own dedicated slot. Esc is handled
	// here directly (un-mount) rather than inside scanHistoryScreen.Update,
	// mirroring Confirm.Active()'s own existing wrapper shape immediately
	// below.
	if scan, ok := m.adminScreens[slotScanHistory].(scanHistoryScreen); ok {
		if isEscKey(msg) {
			m.adminScreens[slotScanHistory] = nil
			m.status = ""
			return m, nil
		}
		next, cmd, _ := scan.Update(m.screenEnv(), msg)
		m.adminScreens[slotScanHistory] = next
		return m, cmd
	}

	if isRuneKey(msg, 'l') && m.canLogoutAdminFromCurrentScreen() {
		return m.logoutAdmin(), nil
	}

	if m.adminView.Confirm.Active() {
		before := m.adminView.Confirm
		next, cmd, _ := before.update(m.screenEnv(), msg)
		m.adminView.Confirm = next
		switch {
		case cmd != nil:
			m.status = before.submitting
		case isEscKey(msg):
			m.status = ""
		}
		return m, cmd
	}

	if isRuneKey(msg, 'q') && isAdminPrincipalScreen(m.screen) && !m.migratedScreenCapturesTextInput() {
		return m, tea.Quit
	}

	return routeAdminKey(m, msg)
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
		// Phase 18: screenAdminMenu is now the admin panel's own root, not
		// screenAdminUsers (design.md Decision I) -- Esc here goes back up
		// to the domain menu; returnToInspection() moved onto
		// screenAdminMenu's own Esc (screen_admin_menu.go).
		return m, navigate(screenAdminMenu)
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
// "e"/"x" to enable/disable via the confirm modal, "d" to delete (genuinely
// irreversible -- distinct from "x"/disable, and distinct from every other
// admin screen in this codebase where "x" itself means delete), "t" to
// reuse the existing token screens for the selected robot, "r" to refresh.
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
		enable := isRuneKey(msg, 'e')
		verb := "disable"
		if enable {
			verb = "enable"
		}
		robotID := robot.ID
		m.adminView.Confirm = newConfirmPrompt(
			fmt.Sprintf("Confirm %s", strings.Title(verb)),
			fmt.Sprintf("Confirm %s robot %q?", verb, robot.Username),
			verb,
			fmt.Sprintf("Submitting %s for %s...", verb, robot.Username),
			func(env screenEnv) tea.Cmd { return env.asModel().enableDisableRobotCmd(robotID, enable) },
		)
		m.status = ""
		return m, nil
	case isRuneKey(msg, 'd'):
		robot, ok := selectedAdminRobot(m.adminView)
		if !ok {
			m.status = "No robot selected."
			return m, nil
		}
		robotID, robotUsername := robot.ID, robot.Username
		m.adminView.Confirm = newConfirmPrompt(
			"Confirm Delete",
			fmt.Sprintf("Delete robot %q? This action cannot be undone.", robot.Username),
			"delete",
			fmt.Sprintf("Deleting robot %q...", robot.Username),
			func(env screenEnv) tea.Cmd { return env.asModel().deleteRobotCmd(robotID, robotUsername) },
		)
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
	case isMoveUpKey(msg):
		if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRepository {
			suggestions := robotRepositorySuggestions(m.adminView.CreateRobotForm, m.repositories.Names())
			m.adminView.CreateRobotForm.RepositorySuggestion = boundedIndex(m.adminView.CreateRobotForm.RepositorySuggestion-1, len(suggestions))
			return m, nil
		}
	case isMoveDownKey(msg):
		if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRepository {
			suggestions := robotRepositorySuggestions(m.adminView.CreateRobotForm, m.repositories.Names())
			m.adminView.CreateRobotForm.RepositorySuggestion = boundedIndex(m.adminView.CreateRobotForm.RepositorySuggestion+1, len(suggestions))
			return m, nil
		}
	case isTabKey(msg):
		m.adminView.CreateRobotForm.Focus = nextCreateRobotField(m.adminView.CreateRobotForm.Focus)
		return m, nil
	case isBackspaceKey(msg):
		m.deleteCreateRobotRune()
		if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRepository {
			m.adminView.CreateRobotForm.RepositorySuggestion = 0
		}
		return m, nil
	case isRuneKey(msg, ' '):
		if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRole {
			m.adminView.CreateRobotForm.Role = nextGrantRole(m.adminView.CreateRobotForm.Role)
			return m, nil
		}
	case isEnterKey(msg):
		if m.adminView.CreateRobotForm.Focus == adminCreateRobotFieldRepository {
			suggestions := robotRepositorySuggestions(m.adminView.CreateRobotForm, m.repositories.Names())
			if len(suggestions) > 0 {
				m.adminView.CreateRobotForm.Repository = suggestions[boundedIndex(m.adminView.CreateRobotForm.RepositorySuggestion, len(suggestions))]
			}
			if strings.TrimSpace(m.adminView.CreateRobotForm.Repository) == "" {
				m.status = "Repository is required."
				return m, nil
			}
			m.adminView.CreateRobotForm.Focus = adminCreateRobotFieldRole
			m.adminView.CreateRobotForm.RepositorySuggestion = 0
			return m, nil
		}
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
			m.adminView.CreateRobotForm.RepositorySuggestion = 0
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

// updateAdminFeaturesKey/updateTrivyConfigModalKey/updateScanPolicyModalKey
// moved to securityMenuScreen/trivyConfigScreen's own Update methods
// (screen_security_menu.go, screen_trivy_config.go, Phase 11 resolved-gap
// addendum): screenAdminFeatures is repurposed as securityMenuScreen, and
// Trivy's own config/scan-policy modals move onto trivyConfigScreen exactly
// like gitleaksConfigScreen's own config modal (Slice 1) and
// signingConfigScreen's own policy modal (Phase 12.3).

// nextScanPolicyThreshold cycles the 2-value severity threshold, toggled
// with Space per design.md Decision 6.
func nextScanPolicyThreshold(threshold string) string {
	if threshold == ports.ScanPolicyThresholdCriticalHigh {
		return ports.ScanPolicyThresholdCritical
	}
	return ports.ScanPolicyThresholdCriticalHigh
}

// updateSigningPolicyModalKey moved to signingConfigScreen.updateKey
// (screen_signing_config.go, Phase 12.3).

// signingKeyFingerprints derives a read-only fingerprint for each stored
// trusted key via signing.Fingerprint (the ONE canonical implementation of
// that algorithm), so signingPolicyModal/renderSigningPolicyModal never has
// to hold or render raw PEM key material (design.md Decision 11 piece 1 --
// "the modal never has to display multi-line text either").
func signingKeyFingerprints(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	fingerprints := make([]string, 0, len(keys))
	for _, key := range keys {
		fingerprints = append(fingerprints, signing.Fingerprint(key))
	}
	return fingerprints
}

// updateAdminScanHistoryModalKey/moveAdminFindingCursor/
// openSelectedAdminFindingLink/pageAdminScanHistory moved onto
// scanHistoryScreen (Phase 19, screen_scan_history.go): ScanHistoryModal
// migrates off AdminViewState per design.md's State Migration table.

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
		enable := isRuneKey(msg, 'e')
		verb := "disable"
		if enable {
			verb = "enable"
		}
		userID := user.ID
		m.adminView.Confirm = newConfirmPrompt(
			fmt.Sprintf("Confirm %s", strings.Title(verb)),
			fmt.Sprintf("Confirm %s user %q?", verb, user.Username),
			verb,
			fmt.Sprintf("Submitting %s for %s...", verb, user.Username),
			func(env screenEnv) tea.Cmd { return env.asModel().enableDisableUserCmd(userID, enable) },
		)
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
		userID, username, repository := m.adminView.SelectedUserID, m.adminView.SelectedUsername, grant.Repository.String()
		m.adminView.Confirm = newConfirmPrompt(
			"Confirm Grant Removal",
			fmt.Sprintf("Remove grant %q from %q?", repository, username),
			"remove",
			fmt.Sprintf("Removing grant %q from %s...", repository, username),
			func(env screenEnv) tea.Cmd { return env.asModel().deleteAdminGrantCmd(userID, username, repository) },
		)
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
		userID, username, accessor := m.adminView.SelectedUserID, m.adminView.SelectedUsername, token.Accessor
		m.adminView.Confirm = newConfirmPrompt(
			"Confirm Token Revocation",
			fmt.Sprintf("Revoke admin token %q for %q?", accessor, username),
			"revoke",
			fmt.Sprintf("Revoking token %q for %s...", accessor, username),
			func(env screenEnv) tea.Cmd { return env.asModel().revokeAdminTokenCmd(userID, username, accessor) },
		)
		m.status = ""
		return m, nil
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
		repository, username := m.adminView.RepoAdminRepository, grant.Username
		m.adminView.Confirm = newConfirmPrompt(
			"Confirm Grant Removal",
			fmt.Sprintf("Remove grant for %q from %q?", username, repository),
			"remove",
			fmt.Sprintf("Removing grant for %q from %q...", username, repository),
			func(env screenEnv) tea.Cmd { return env.asModel().deleteRepoAdminGrantCmd(repository, username) },
		)
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
		// Unlike the hardcoded "" every other branch here used before it,
		// this returns m.status: the delete-tag pending-confirm message,
		// its success/error follow-up, and the ordinary "" baseline all
		// flow through the same field, so View() and the row-budget math
		// below can never drift apart on what's actually shown.
		return m.status, "Enter: inspect manifest | d: delete tag | Tab: admin | Esc: back | q: quit", len(m.tags.Items) + 1, true
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

// regixtryReleasesAPI is this repo's own GitHub releases feed -- the same
// endpoint internal/infra/install/releases.defaultReleasesAPIURL points at
// for regixtry upgrade, duplicated here rather than imported: that package
// also pulls in archive extraction (tar/gzip) and systemd-oriented install
// orchestration entirely irrelevant to a background version-check banner.
const regixtryReleasesAPI = "https://api.github.com/repos/desatatufuria/regixtry/releases"

// updateCheckTimeout bounds the background update check's own HTTP client,
// mirroring admin_client.go's defaultAdminClientTimeout precedent for a
// TUI-adjacent outbound call that must never leave an indefinitely
// outstanding goroutine.
const updateCheckTimeout = 10 * time.Second

// checkForUpdateCmd is the background, non-blocking version check: fired as
// a follow-up Cmd chained from whichever startup message resolves first
// (catalogLoadedMsg or startupLoginMsg), never batched into Init() itself
// (design.md-established convention: tea.Batch would break --snapshot
// mode's single Update() call, which cannot unpack a tea.BatchMsg). Returns
// nil -- fires no request at all -- when there is no current version to
// compare against, so a dev build (main.buildVersion's "dev" sentinel,
// never passed to WithCurrentVersion by runTUI) can never trigger a network
// call.
//
// The channel is resolved fresh INSIDE the returned closure via
// m.service.GetUpdateChannel -- a real server-side setting now
// (PUT /admin/v1/update-channel is the only write path, admin-gated), not
// client-side Model state -- so a stale/never-updated local value can never
// drift from what an admin actually configured. GetUpdateChannel is called
// directly in-process, not over HTTP: QueryService (m.service) is always a
// real, local *appregixtry.Service regardless of -api-base-url (that flag
// only affects the separate admin-panel HTTP client). Any error here (a
// fresh install with the metadata store still initializing, or any other
// infrastructure fault) is reported through the same updateCheckCompletedMsg
// error path as a GitHub-fetch failure -- both are silent, best-effort, and
// never surfaced to the operator (see the Update handler).
func (m Model) checkForUpdateCmd() tea.Cmd {
	currentVersion := m.currentVersion
	if currentVersion == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, updateCheckTimeout)
		defer cancel()
		channelValue, err := m.service.GetUpdateChannel(ctx)
		if err != nil {
			return updateCheckCompletedMsg{err: err}
		}
		channel := release.Channel(channelValue)
		if !release.ValidChannel(string(channel)) {
			return updateCheckCompletedMsg{err: fmt.Errorf("release: unknown update channel %q", channelValue)}
		}
		httpClient := &stdhttp.Client{Timeout: updateCheckTimeout}
		latest, err := release.LatestForChannel(ctx, httpClient, regixtryReleasesAPI, channel)
		return updateCheckCompletedMsg{latestVersion: latest, err: err}
	}
}

func (m Model) loadTagsCmd(repository string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.TagDetails(m.ctx, repository, 100, "")
		return tagsLoadedMsg{repository: repository, result: result, err: err}
	}
}

// deleteTagCmd fires the Tags screen's pending-delete confirm (Enter),
// calling QueryService.DeleteManifest with the tag name as reference --
// per newDeletionDetailsForTag this untags only, leaving the manifest and
// any other tags intact. Always returns a tagDeletedMsg carrying err, the
// same always-returns-a-Msg-never-a-bare-error convention deleteRobotCmd
// uses.
func (m Model) deleteTagCmd(repository string, tag string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.DeleteManifest(m.ctx, repository, tag)
		return tagDeletedMsg{repository: repository, tag: tag, err: err}
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
			return adminFeaturePageLoadedMsg{feature: name, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		page, err := m.adminClient.GetFeaturePage(m.ctx, m.adminSession, name)
		return adminFeaturePageLoadedMsg{feature: name, page: page, err: err}
	}
}

// loadFeaturePageCmd is the screenEnv-scoped equivalent of
// Model.loadAdminFeaturePageCmd, needed because a sub-model has no Model to
// call a method on (mirrors screen_gitleaks_config.go's configureFeatureCmd
// wrapper).
func loadFeaturePageCmd(env screenEnv, name string) tea.Cmd {
	return env.asModel().loadAdminFeaturePageCmd(name)
}

// loadAdminRepositoryScanSummariesCmd fetches the Repository Alerts table's
// data: one row per repository, collapsed server-side before limit
// (ListRepositoryScanSummaries/ports.RepositoryScanSummary) so a few
// heavily-rescanned repositories can never crowd every other repository out
// of the window (the bug loadAdminScanRunsCmd's raw-ListScanRuns had).
func (m Model) loadAdminRepositoryScanSummariesCmd(limit int) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRepositoryScanSummariesLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		summaries, err := m.adminClient.ListRepositoryScanSummaries(m.ctx, m.adminSession, limit)
		return adminRepositoryScanSummariesLoadedMsg{summaries: summaries, err: err}
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

// deleteRobotCmd backs the "d" key on screenAdminRobots (a genuinely
// destructive, irreversible action, unlike enableDisableRobotCmd above).
// It calls AdminClient.DeleteRobot, already wired through to the backend's
// DELETE /admin/v1/robots/{id} route added in PR 4.
func (m Model) deleteRobotCmd(robotID string, username string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminRobotDeletedMsg{robotID: robotID, username: username, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		err := m.adminClient.DeleteRobot(m.ctx, m.adminSession, robotID)
		return adminRobotDeletedMsg{robotID: robotID, username: username, err: err}
	}
}

func (m Model) executeFeatureActionCmd(name string, actionID string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminFeatureActionCompletedMsg{feature: name, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		result, err := m.adminClient.ExecuteFeatureAction(m.ctx, m.adminSession, name, actionID)
		return adminFeatureActionCompletedMsg{feature: name, result: result, err: err}
	}
}

// executeFeatureActionCmd is the screenEnv-scoped equivalent of Model's own
// executeFeatureActionCmd method (mirrors configureFeatureCmd's wrapper).
func executeFeatureActionCmd(env screenEnv, name string, actionID string) tea.Cmd {
	return env.asModel().executeFeatureActionCmd(name, actionID)
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

func (m Model) loadUpdateChannelCmd() tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUpdateChannelLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		channel, err := m.adminClient.GetUpdateChannel(m.ctx, m.adminSession)
		return adminUpdateChannelLoadedMsg{channel: channel, err: err}
	}
}

func (m Model) updateUpdateChannelCmd(channel string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminUpdateChannelUpdatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		persisted, err := m.adminClient.SetUpdateChannel(m.ctx, m.adminSession, channel)
		return adminUpdateChannelUpdatedMsg{channel: persisted, err: err}
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

// loadScanPolicyCmd/updateScanPolicyCmd/loadSigningPolicyCmd are the
// screenEnv-scoped equivalents of Model's own methods above, needed because
// a sub-model has no Model to call a method on (mirror
// screen_gitleaks_config.go's configureFeatureCmd wrapper).
func loadScanPolicyCmd(env screenEnv) tea.Cmd { return env.asModel().loadScanPolicyCmd() }

func updateScanPolicyCmd(env screenEnv, input ports.ScanPolicySettings) tea.Cmd {
	return env.asModel().updateScanPolicyCmd(input)
}

// loadUpdateChannelCmd/updateUpdateChannelCmd are updateChannelScreen's own
// screenEnv-scoped wrappers, mirroring loadScanPolicyCmd/updateScanPolicyCmd
// exactly.
func loadUpdateChannelCmd(env screenEnv) tea.Cmd { return env.asModel().loadUpdateChannelCmd() }

func updateUpdateChannelCmd(env screenEnv, channel string) tea.Cmd {
	return env.asModel().updateUpdateChannelCmd(channel)
}

func loadSigningPolicyCmd(env screenEnv) tea.Cmd { return env.asModel().loadSigningPolicyCmd() }

func (m Model) updateSigningPolicyCmd(input ports.SigningPolicySettings) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminSigningPolicyUpdatedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		settings, err := m.adminClient.UpdateSigningPolicy(m.ctx, m.adminSession, input)
		return adminSigningPolicyUpdatedMsg{settings: settings, err: err}
	}
}

// signingKeyUsageCmd fetches trustedKeyList's delete-key usage-count
// advisory (CountSigningKeyUsage), the screenEnv-scoped equivalent of
// Model's own method below, needed because a sub-model has no Model to call
// a method on (mirrors loadSigningPolicyCmd's identical wrapper above).
func signingKeyUsageCmd(env screenEnv, repository string, keyPEM string) tea.Cmd {
	return env.asModel().signingKeyUsageCmd(repository, keyPEM)
}

func (m Model) signingKeyUsageCmd(repository string, keyPEM string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminSigningKeyUsageLoadedMsg{err: fmt.Errorf("admin API is unavailable for this session")}
		}
		count, capped, err := m.adminClient.CountSigningKeyUsage(m.ctx, m.adminSession, repository, keyPEM)
		return adminSigningKeyUsageLoadedMsg{count: count, capped: capped, err: err}
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
// feature, chained after adminRepositoryScanSummariesLoadedMsg so the
// Repository Alerts table can annotate disabled repositories (design.md
// Decision 7's "List (TUI annotation)" row).
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
		// Phase 18 (design.md Decision I): re-entering an already-
		// authenticated session must land on the same domain-menu root a
		// fresh login lands on (model.go's adminLoginCompletedMsg success
		// handler) -- this branch was the one re-entry path Phase 18 left
		// pointed at screenAdminUsers directly, confirmed live by an
		// operator whose deployment forces startup-login (so a fresh
		// login's own success handler never lands on screenAdminMenu
		// either -- it routes to the repository catalog instead), making
		// this branch the ONLY reachable path into the admin panel.
		m.screen = screenAdminMenu
		m.adminScreens[slotAdminMenu] = newAdminMenuScreen()
		if len(m.adminView.Users) == 0 {
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
	// screenAdminFeatures is repurposed as securityMenuScreen (design.md
	// Decision I): mount+Init it here exactly like Gitleaks'/Signing's own
	// 'o' openers do (mirrors newAdminScreenFor's own construction,
	// screen.go), rather than relying on navigateMsg's lazy-mount path,
	// since this is the very first entry point into the admin section's
	// Security & Compliance domain.
	screen := newSecurityMenuScreen()
	cmd := screen.Init(m.screenEnv())
	m.adminScreens[slotSecurityMenu] = screen
	m.screen = screenAdminFeatures
	m.adminView.UserSearchActive = false
	m.status = "Loading built-in features..."
	return m, cmd
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
	m.adminView.Confirm = confirmPrompt{}
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
	case screenAdminLogin, screenAdminAuthenticating, screenAdminUsers, screenAdminFeatures, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens, screenAdminCreateToken, screenRepoAdminGrants, screenRepoAdminAddGrant, screenAdminRobots, screenAdminCreateRobot, screenSecurityGitleaksRepos, screenSecuritySigningRepos, screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminMenu, screenAdminOperations, screenAdminScanRuns, screenAdminSecretFindings, screenAdminUpdateChannel:
		return true
	default:
		return false
	}
}

// isAdminPrincipalScreen deliberately excludes screenRepoAdminAddGrant
// (a free-text username form), mirroring the existing screenAdminAddGrant
// exclusion: this gates the bare 'q' quit key, and including a free-text
// form here would make typing "q" as part of a username quit the program.
//
// screenSecurityTrivy/screenSecurityGitleaksConfig/screenSecuritySigningConfig
// join this set because each advertises "q: quit" in its own Keys()-derived
// footer (trivyConfigScreen.Keys/gitleaksConfigScreen.Keys/
// signingConfigScreen.Keys). Their sibling repos screens
// (screenSecurityTrivyRepos/screenSecurityGitleaksRepos/
// screenSecuritySigningRepos) do not advertise "q: quit" and are
// deliberately excluded, mirroring the free-text-form exclusion above.
// screenAdminUpdateChannel (tui-update-check feature) joins the same way:
// updateChannelKeys advertises "q: quit" exactly like the three config
// screens above.
func isAdminPrincipalScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminUsers, screenAdminFeatures, screenAdminCreateUser, screenAdminEditUser, screenAdminEditUserGrants, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots, screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminMenu, screenAdminOperations, screenAdminUpdateChannel:
		return true
	default:
		return false
	}
}

// migratedScreenCapturesTextInput reports whether the currently mounted
// migrated top-level screen (design.md Decision A/I, resolved via slotFor)
// has its own local overlay open that captures free-text or key input -- a
// confirm prompt, a config/policy modal, or the shared overrideEditor. None
// of these local overlays are visible to the legacy
// m.adminView.Confirm.Active() guard (design.md Decision B: each migrated
// screen owns its own state), so the bare 'l' logout / 'q' quit global keys
// must not intercept keystrokes meant for one of these overlays -- mirrors
// the existing Confirm.Active() top-of-function guard, scoped to the
// currently mounted migrated screen's own equivalent state.
func (m Model) migratedScreenCapturesTextInput() bool {
	slot, ok := slotFor(m.screen)
	if !ok {
		return false
	}
	switch s := m.adminScreens[slot].(type) {
	case trivyConfigScreen:
		return s.confirm.Active() || s.cfg.Active() || s.policyModal.Active()
	case trivyReposScreen:
		return s.editor.Active()
	case gitleaksConfigScreen:
		return s.confirm.Active() || s.cfg.Active()
	case signingConfigScreen:
		return s.confirm.Active() || s.cfg.Active()
	case featureOverridesScreen:
		return s.editor.Active()
	default:
		return false
	}
}

func (m Model) canLogoutAdminFromCurrentScreen() bool {
	if m.adminAuth != adminAuthStateAuthenticated || m.adminView.Confirm.Active() || m.migratedScreenCapturesTextInput() {
		return false
	}

	switch m.screen {
	case screenAdminUsers:
		return !m.adminView.UserSearchActive
	case screenAdminFeatures, screenAdminEditUser, screenAdminEditUserGrants, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots,
		screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenSecurityGitleaksRepos, screenSecuritySigningRepos,
		screenAdminMenu, screenAdminOperations, screenAdminScanRuns, screenAdminSecretFindings, screenAdminUpdateChannel:
		return true
	default:
		return false
	}
}

// repositorySuggestionsMatching is the core repository-autosuggest filter:
// case-insensitive substring match against query, deduplicated, order
// preserved. It takes a plain query string rather than a specific form type
// so every form with a Repository autosuggest field (adminGrantForm today,
// adminCreateRobotForm below) can share this exact filtering/dedupe logic
// instead of each re-implementing it -- unlike the deliberate "sibling, not
// shared" pattern used for authorization-scoped structs such as
// adminGrantForm/adminRepositoryGrantForm, this is pure display filtering
// with no authorization semantics, so sharing is correct here.
func repositorySuggestionsMatching(query string, repositories []string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
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

func grantRepositorySuggestions(form adminGrantForm, repositories []string) []string {
	return repositorySuggestionsMatching(form.Repository, repositories)
}

// robotRepositorySuggestions is screenAdminCreateRobot's counterpart to
// grantRepositorySuggestions, reusing the same core filter so both forms'
// Repository field behave identically (manual RC feedback: Create Robot's
// Repository field had no autocomplete while Add Grant's already did).
func robotRepositorySuggestions(form adminCreateRobotForm, repositories []string) []string {
	return repositorySuggestionsMatching(form.Repository, repositories)
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

// featureActionHelp (the formatted-string help builder) is retired (Phase
// 11): featureActionKeyBindings (admin_keys.go) is its replacement,
// composed via shortHelpView so a migrated screen's help can never drift
// from what a key actually does (design.md Decision A).

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
// Signed column. It renders the fixed-vocabulary State, the `.sig` tag name,
// the signature count, and -- only when a signature actually verified -- the
// short fingerprint of the trusted key that verified it
// (SignatureStatusDetail.VerifiedKeyFingerprint, itself already fingerprint-
// only per queries.go's no-key-leakage discipline). It never renders
// SignatureStatusDetail.Reason, raw key material, raw signature bytes, or
// trust configuration detail, mirroring the no-key-leakage discipline
// already enforced for signature-status/signing-policy-violation responses
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
	if signature.Signature.VerifiedKeyFingerprint != "" {
		lines = append(lines, fmt.Sprintf("%s %s", theme.muted.Render("Signed with:"), theme.text.Render(signature.Signature.VerifiedKeyFingerprint)))
	}
	// VerifiedIdentity (signing-keyless-verification) is mutually exclusive
	// with VerifiedKeyFingerprint (queries.go's own discipline: a signature
	// verifies via exactly one anchor kind, never both), so this renders on
	// its own distinct line, never folded into "Signed with:" (spec:
	// "Identity match reports SAN and issuer separately").
	if signature.Signature.VerifiedIdentity != "" {
		lines = append(lines, fmt.Sprintf("%s %s", theme.muted.Render("Verified identity:"), theme.text.Render(signature.Signature.VerifiedIdentity)))
	}
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
	// Features/FeaturePage/TrivyTab/TrivyConfigModal/TrivyScanRuns/
	// TrivySelectedAlert/TrivyAlertsLoaded/TrivySummaries/TrivyOverrides all
	// migrated off AdminViewState (Phase 11); the Security & Compliance
	// domain's screens are simply un-mounted here instead, so the operator
	// gets a fresh load the next time they are navigated into.
	m.adminScreens[slotSecurityMenu] = nil
	m.adminScreens[slotTrivyConfig] = nil
	m.adminScreens[slotTrivyRepos] = nil
	m.adminScreens[slotGitleaksConfig] = nil
	m.adminScreens[slotSigningConfig] = nil
	// scanHistoryScreen (Phase 19) migrated off AdminViewState.ScanHistoryModal
	// onto its own dedicated slot (design.md's State Migration table);
	// un-mounted here exactly like the Security & Compliance screens above.
	m.adminScreens[slotScanHistory] = nil
	m.adminView.SelectedGrant = 0
	m.adminView.SelectedToken = 0
	m.adminView.ResetPasswordForm = adminResetPasswordForm{}
	m.adminView.GrantForm = newAdminViewState().GrantForm
	m.adminView.TokenForm = adminTokenForm{}
	m.adminView.RevealedTokenSecret = ""
	m.adminView.RevealedTokenAccessor = ""
	m.adminView.RevealedTokenExpiresAt = time.Time{}
}

// applyLoadedFeatures/applyFeaturePage/selectedFeatureName/
// isSelectedTrivyFeature/isSelectedGitleaksFeature/isSelectedSigningFeature/
// toggleTrivyTab/selectedScanSummary/trivyConfigModalFromPage/
// nextTrivyConfigField/gitleaksConfigModalFromPage/nextGitleaksConfigField/
// deleteTrivyConfigModalRune/appendTrivyConfigModalRunes/
// trivyConfigInputFromModal/moveAdminFeatureSelection all moved onto
// securityMenuScreen/trivyConfigScreen/gitleaksConfigScreen's own state and
// methods (Phase 11 resolved-gap addendum) -- each screen now independently
// owns the piece of this logic scoped to its own fixed feature, per
// design.md Decision D4 (no shared cross-screen state).

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

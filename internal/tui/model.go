package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appregixtry "regixtry/internal/app/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/infra/release"
	"regixtry/internal/ports"
)

type Option func(*Model)

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

// WithUpdateCheckCachePath sets the local JSON file checkForUpdateCmd uses
// to throttle its background GitHub release check
// (throttle-tui-update-check feature). A cache entry younger than its own
// TTL (updateCheckCacheTTLSuccess for a successful check,
// updateCheckCacheTTLFailure for a failed one) skips the real network call
// entirely and reuses the cached result, regardless of how many times the
// TUI is opened in between -- this is what fixes a confirmed live incident
// where GitHub's unauthenticated 60-requests/hour-per-IP limit was
// exhausted purely by this background check firing on every `regixtry tui`
// session start. An empty path (the default, and every pre-existing
// caller/test that does not pass this option) disables caching entirely:
// checkForUpdateCmd always performs a real check, exactly its pre-existing
// behavior.
func WithUpdateCheckCachePath(path string) Option {
	return func(m *Model) {
		m.updateCheckCachePath = strings.TrimSpace(path)
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
	// DeleteRepository backs the Repositories screen's delete-repository "d"
	// key/confirm flow (delete-entire-repository feature), mirroring
	// DeleteManifest's own in-process, not-AdminClient/HTTP shape exactly:
	// QueryService is always a real, local *appregixtry.Service, and
	// delete-enablement/authorization are Service's own concerns, not an
	// admin-session concern.
	DeleteRepository(ctx context.Context, repositoryName string) (appregixtry.RepositoryDeletionDetails, error)
	// GetUpdateChannel backs checkForUpdateCmd's channel resolution, called
	// directly in-process exactly like DeleteManifest above -- QueryService
	// is always a real, local *appregixtry.Service regardless of whether
	// -api-base-url is set for the separate admin-panel HTTP client, so this
	// never needs the AdminClient/HTTP path the write side
	// (AdminClient.SetUpdateChannel) uses.
	GetUpdateChannel(ctx context.Context) (string, error)
	// Referrers backs the Console manifest inspection screen's Referrers
	// section (console-manifest-enrichment change): manifests whose
	// subject.digest points at the inspected one -- signatures,
	// attestations, SBOMs. Called directly in-process against
	// (*appregixtry.Service).Referrers, exactly like ResolveManifest and
	// SignatureStatus above, never through AdminClient/HTTP. artifactType is
	// always "" from this caller -- loadManifestCmd wants every referrer,
	// never a filtered subset.
	Referrers(ctx context.Context, repositoryName string, subjectDigest string, artifactType string) (appregixtry.ReferrersIndex, error)
}

type ManifestModel struct {
	Details appregixtry.ManifestDetails
	// Signature is the SAME SignatureStatusResult shape the Console Tags
	// table's Signed column already resolves per tag, fetched fresh for
	// this reference (loadManifestCmd) rather than threaded through from
	// the Tags screen -- see loadManifestCmd's own comment for why.
	Signature appregixtry.SignatureStatusResult
	// Referrers is the manifest inspection screen's Referrers section
	// (console-manifest-enrichment change): manifests whose subject.digest
	// points at Details.Digest, fetched fresh via QueryService.Referrers in
	// the same loadManifestCmd round trip as Signature above.
	Referrers appregixtry.ReferrersIndex
}

type BlobsModel struct {
	Items    []appregixtry.BlobDetails
	Selected int
}

type UploadsModel struct {
	Repository string
	Items      []appregixtry.UploadDetails
}

type MutationUnavailableModel struct {
	Action string
	Reason string
}

type screen string

const (
	screenLoading      screen = "loading"
	screenRepositories screen = "repositories"
	// screenProjects is the path-based-project-grouping feature's Projects
	// screen: a Console-level screen (like screenRepositories/screenTags,
	// never behind the admin login/adminScreen sub-model system), reached
	// via "p" from screenRepositories and purely client-side (deriveProjects
	// over the already-fetched catalog -- no Init, no Service call).
	screenProjects            screen = "projects"
	screenTags                screen = "tags"
	screenManifest            screen = "manifest"
	screenBlobs               screen = "blobs"
	screenUploads             screen = "uploads"
	screenError               screen = "error"
	screenAdminLogin          screen = "admin-login"
	screenAdminAuthenticating screen = "admin-authenticating"
	screenAdminUsers          screen = "admin-users"
	screenAdminCreateUser     screen = "admin-create-user"
	screenAdminEditUser       screen = "admin-edit-user"
	screenAdminChangePassword screen = "admin-change-password"
	screenAdminEditUserTokens screen = "admin-edit-user-tokens"
	// screenRepoAdminGrants is the delegate-facing entry point (design.md
	// Decision 7): reached from the Console Repositories screen, not from the
	// global-admin user workspace, and scoped to exactly one repository via
	// adminIntent/adminIntentRepository. Its new grant form is part of the
	// screen, not a screen of its own.
	screenRepoAdminGrants screen = "repo-admin-grants"
	// screenAdminRobots/screenAdminCreateRobot are global-admin-only
	// screens (design.md Decision 7), reached from the Overview's sidebar.
	// there is no separate robot edit screen.
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
	// actions) and trivyReposScreen (the Repositories tab),
	// reached from the Overview's Image scanning item and from each other via Tab.
	screenSecurityTrivy      screen = "security-trivy"
	screenSecurityTrivyRepos screen = "security-trivy-repos"
	// screenSecurityGitleaksConfig/screenSecuritySigningConfig are
	// Gitleaks'/Signing's own config screens (Phase 11 resolved-gap
	// addendum): properly addressable top-level screens, symmetric to
	// screenSecurityTrivy.
	screenSecurityGitleaksConfig screen = "security-gitleaks-config"
	screenSecuritySigningConfig  screen = "security-signing-config"
	// screenAdminOverview is the post-login landing screen (tui-v2-redesign
	// S3): the application shell around the quick actions and "Needs
	// attention".
	screenAdminOverview screen = "admin-overview"
	// screenAdminScanRuns/screenAdminSecretFindings are the two
	// scanRunsScreen-backed repository pickers the Overview's Scan runs and
	// Secret findings items open (Phase 19, D9): the only difference between them is which
	// scanHistoryScreen tab Enter defaults to.
	screenAdminScanRuns       screen = "admin-scan-runs"
	screenAdminSecretFindings screen = "admin-secret-findings"
	// screenAdminUpdateChannel is Operations' third row (tui-update-check
	// feature): the admin-only screen that sets the server-side update
	// channel (GET /update-channel is public and pre-login; PUT
	// /admin/v1/update-channel is the only write path, admin-gated -- this
	// screen is the sole caller of the write side).
	screenAdminUpdateChannel screen = "admin-update-channel"
	// screenAdminGrantAccess is "Grant repository access" (tui-v2-redesign S4):
	// one screen to give a user or robot a role on several repositories.
	screenAdminGrantAccess screen = "admin-grant-access"
	// screenAdminRepositoryAccess is "Repository access": every grant, by
	// repository, with revoke.
	screenAdminRepositoryAccess screen = "admin-repository-access"
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
// constant of the same name and value.
const gitleaksFeatureName = "gitleaks"

// signingFeatureName mirrors internal/app/regixtry's own package-private
// constant of the same name and value (design.md Decision 11).
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
	// lateNotice is the one-line report of a create result that arrived after
	// its screen was left; queuedSecrets are created credentials waiting
	// behind a secret that is on screen. Both end with the session.
	lateNotice    string
	queuedSecrets []accessMsg
	ctx           context.Context
	service       QueryService
	screen        screen
	loadingText   string
	err           error
	now           func() time.Time

	// console is the console browser's delete bookkeeping, shared by every
	// console screen it mounts (see consoleShared).
	console  *consoleShared
	manifest ManifestModel
	blobs    BlobsModel
	uploads  UploadsModel

	adminClient  AdminClient
	adminSession AdminSession
	adminAuth    adminAuthState
	adminLogin   adminLoginForm
	// expiryNoteShown is true while adminSession.ExpiredReason holds the
	// "applied before it expired" note of a grant apply; Esc on the login
	// screen clears it.
	expiryNoteShown bool
	adminReturn     screen
	// adminIntent/adminIntentRepository carry the one-shot repo-admin-grants
	// destination (design.md Decision 7) from the Console Repositories
	// screen's grant action through screenAdminLogin to the post-login
	// routing decision. Both are reset once consumed.
	adminIntent           adminIntent
	adminIntentRepository string

	mutation MutationUnavailableModel
	status   string

	showMutationNotice bool
	lastRepository     string
	lastTag            string
	startupLogin       bool

	// viewport is the terminal size captured from the most recent
	// tea.WindowSizeMsg (or the default from NewModel before any resize
	// event, e.g. under the --snapshot CLI path — design.md decision #8).
	viewport viewportSize

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
	// updateCheckCachePath is checkForUpdateCmd's throttle cache file
	// (throttle-tui-update-check feature), set via WithUpdateCheckCachePath.
	// Empty (the zero value) disables caching entirely -- see that option's
	// doc comment.
	updateCheckCachePath string
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
// screen actually renders.
//
// Phase 3 deviation from design.md's zero-arg method: several screens
// render a status other than m.status plus their own help
// string. Measuring "" instead would under-count chrome and let the row
// budget exceed the terminal height once content is clipped.
// screenEnv builds the read-only view of the outside world a migrated
// sub-model or a model's embedded confirmPrompt may read
// (design.md's screenEnv). Layout uses adminTablesLayout's admin-screen
// budget, since only admin-side screens and overlays consume it.
func (m Model) screenEnv() screenEnv {
	return screenEnv{
		Client:            m.adminClient,
		Session:           m.adminSession,
		Layout:            m.adminTablesLayout(),
		Service:           m.service,
		KnownRepositories: m.knownRepositories(),
		Now:               m.now,
		CurrentVersion:    m.currentVersion,
		UpdateAvailable:   m.updateAvailable,
	}
}

func (m Model) contentBudget(status, help string) consoleLayout {
	layout := contentBudget(m.viewport.Width, m.viewport.Height, status, help)
	return layout
}

// adminTablesLayout computes the consoleLayout a screen is sized against in
// its Update. It uses the Overview's own help text as a representative
// stand-in: Update handlers run outside View(), so no screen-specific help
// string is available there.
func (m Model) adminTablesLayout() consoleLayout {
	return m.contentBudget(m.status, shortHelpView(newAdminTheme(), overviewKeys))
}

type catalogLoadedMsg struct {
	result []appregixtry.RepositorySummary
	err    error
}

type manifestLoadedMsg struct {
	repository string
	tag        string
	manifest   appregixtry.ManifestDetails
	uploads    []appregixtry.UploadDetails
	signature  appregixtry.SignatureStatusResult
	referrers  appregixtry.ReferrersIndex
	err        error
}

type adminLoginCompletedMsg struct {
	session AdminSession
	err     error
}

// adminSigningKeyUsageLoadedMsg carries the result of a trustedKeyList
// delete-key usage-count lookup (CountSigningKeyUsage). It carries no
// requester identity: only one trustedKeyList is ever mid-delete at a
// time, and trustedKeyList.applyUsageLoaded itself
// discards a response that arrives when it is not expecting one
// (usageLoading already false).
type adminSigningKeyUsageLoadedMsg struct {
	// key is the PEM the count was asked for: the list ignores an answer for
	// any other key.
	key    string
	count  int
	capped bool
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
		console:     newConsoleShared(),
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
		adminIntent: adminIntentOperator,
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
	next, cmd := m.updateModel(msg)
	if updated, ok := next.(Model); ok {
		updated.dropLeftRepositoryAccess()
		updated.dropLeftDelegate()
		next = updated
	}
	return next, cmd
}

// dropLeftRepositoryAccess un-mounts the Repository access screen once the
// operator is anywhere else: it holds a list built from many reads and keeps
// asking for the next batch of repositories while mounted, which must never
// go on behind another screen. Coming back mounts a fresh one.
func (m *Model) dropLeftRepositoryAccess() {
	if m.screen != screenAdminRepositoryAccess {
		m.adminScreens[slotRepositoryAccess] = nil
	}
}

func (m Model) updateModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport = viewportSize{Width: msg.Width, Height: msg.Height}
		// Migrated screens learn the new size through the broadcast like every
		// other non-key message (design.md Decision H).
		m.adminScreens, _ = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, nil
	case keyMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		// v1 delivered a paste as a runes key to the active screen only; keep that path.
		return m.handleKey(keyMsg{Text: msg.Content})
	case catalogLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, m.checkForUpdateCmd()
		}
		// The first read of the catalog mounts the console browser at its
		// root; an empty catalog is the Repositories screen's own empty state.
		repositories := newConsoleRepositoriesScreen(msg.result)
		repositories.shared = m.console
		m.adminScreens[slotConsoleRepositories] = repositories
		m.adminScreens[slotConsoleProjects] = nil
		m.adminScreens[slotConsoleTags] = nil
		m.screen = screenRepositories
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
	case consoleCatalogMsg, consoleTagsMsg, repositoryDeletedMsg, tagDeletedMsg, projectDeletedMsg:
		// Results of the console browser. Each screen drops what carries
		// another instance's generation; the deletes are facts the other
		// mounted screens apply too.
		m.console.applyResult(msg)
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case openProjectsMsg, openRepositoriesMsg, openTagsMsg, openManifestMsg:
		return m.openConsole(msg)
	case manifestLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}
		m.lastRepository = msg.repository
		m.lastTag = msg.tag
		m.manifest = ManifestModel{Details: msg.manifest, Signature: msg.signature, Referrers: msg.referrers}
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
			m.adminSession = LogoutAdminState()
			m.adminLogin.Password = ""
			m.screen = screenAdminLogin
			m.loadingText = ""
			m.status = msg.err.Error()
			return m, nil
		}
		m.adminSession = msg.session
		m.adminSession.ExpiredReason = ""
		m.expiryNoteShown = false
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
			return m.mountDelegate(repository)
		}
		if m.startupLogin && len(m.knownRepositories()) == 0 {
			m.status = ""
			m.screen = screenLoading
			m.loadingText = "Loading repositories..."
			m.startupLogin = false
			return m, m.loadCatalogCmd()
		}
		// The operator lands on the Overview; its own load fills "Needs
		// attention".
		m.status = ""
		m.screen = screenAdminOverview
		overview := newOverviewScreen()
		m.adminScreens[slotOverview] = overview
		return m, overview.Init(m.screenEnv())
	case securityMsg:
		// The Security configuration screens' results. Drop one from another
		// screen instance FIRST: a stale expiry result must never log the
		// operator out.
		if !securityScreenOwns(m.adminScreens, msg.securityGen()) {
			return m, nil
		}
		if reason, expired := msg.expiredReason(); expired {
			return m.expireAdminSession(reason), nil
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminUpdateChannelLoadedMsg, adminUpdateChannelUpdatedMsg:
		// Drop a message from another screen instance FIRST: a stale expiry
		// result must never log the operator out.
		if gen, ok := updateChannelMsgGen(msg); ok {
			if current, isChannel := m.adminScreens[slotUpdateChannel].(updateChannelScreen); !isChannel || current.gen != gen {
				return m, nil
			}
		}
		if expired, ok := msg.(interface{ expiredReason() (string, bool) }); ok {
			if reason, isExpired := expired.expiredReason(); isExpired {
				return m.expireAdminSession(reason), nil
			}
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminSigningKeyUsageLoadedMsg:
		// Broadcast (design.md Decision H): whichever screen holds a
		// trustedKeyList mid-delete reflects the usage count and opens its
		// confirm; every other occupied slot ignores it via its own type
		// switch / trustedKeyList.applyUsageLoaded's own usageLoading guard.
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		if msg.err != nil && IsAdminSessionExpired(msg.err) {
			return m.expireAdminSession(msg.err.Error()), nil
		}
		return m, cmd
	case navigateMsg:
		// The router's own navigation channel (design.md screen.go
		// "navigate"): a migrated screen asks to switch the active
		// top-level screen (e.g. Esc on a repos screen back to the Overview,
		// Tab between Trivy's peer screens, Enter on an Overview sidebar
		// item) without ever
		// writing m.screen itself. Lazily mounts+Inits the target the first
		// time it is visited (newAdminScreenFor, screen.go) -- an
		// already-mounted target's slot is left exactly as it was, so
		// navigating back to it never discards its state.
		m.screen = msg.To
		m.status = ""
		m.adminScreens[slotCreateUser] = nil
		m.adminScreens[slotChangePassword] = nil
		if msg.To == screenAdminRepositoryAccess {
			// Always a fresh read: grants change elsewhere (the grant-access
			// screen, a revoke) and the list is paged.
			next := newRepoAccessScreen()
			m.adminScreens[slotRepositoryAccess] = next
			return m, next.Init(m.screenEnv())
		}
		if msg.To == screenAdminOverview {
			// Coming back to the Overview refreshes "Needs attention": the
			// operator may just have granted access or fixed a robot.
			if overview, ok := m.adminScreens[slotOverview].(overviewScreen); ok {
				return m, overview.Init(m.screenEnv())
			}
		}
		switch msg.To {
		case screenAdminUsers:
			// The list is re-read on every visit: grants, tokens and accounts
			// change on the screens that lead back to it.
			selectID := ""
			if previous, ok := m.adminScreens[slotUsers].(usersScreen); ok {
				selectID = previous.selectedID
			}
			next := newUsersScreen(selectID)
			m.adminScreens[slotUsers] = next
			return m, next.Init(m.screenEnv())
		case screenAdminEditUser:
			// Back from Grant access: re-read the profile, or, if the screen
			// is gone (an expired session), fall back to the list.
			if edit, ok := m.adminScreens[slotEditUser].(editUserScreen); ok {
				return m, edit.Init(m.screenEnv())
			}
			return m.openUserScreen(openUserScreenMsg{to: screenAdminUsers})
		}
		switch msg.To {
		case screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig:
			// Always a fresh screen with a new generation: a result still in
			// flight for an earlier visit must not land on this one.
			slot, _ := slotFor(msg.To)
			next := newAdminScreenFor(msg.To)
			m.adminScreens[slot] = next
			return m, next.Init(m.screenEnv())
		}
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
	case overviewLoadedMsg:
		// A load of a replaced Overview is dropped FIRST: its stale expiry
		// must never log the operator out.
		if overview, ok := m.adminScreens[slotOverview].(overviewScreen); !ok || overview.gen != msg.gen {
			return m, nil
		}
		if reason, expired := msg.expiredReason(); expired {
			return m.expireAdminSession(reason), nil
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case overviewOpenMsg:
		return m.openFromOverview(msg.action)
	case openRobotsMsg:
		return m.mountRobots(msg.selectID)
	case openCreateRobotMsg:
		return m.mountCreateRobot()
	case openTokensMsg:
		return m.mountTokens(msg)
	case robotsLoadedMsg, robotsTokensLoadedMsg, robotEnabledMsg, robotDeletedMsg, robotCreatedMsg, tokensLoadedMsg, tokenCreatedMsg, tokenRevokedMsg:
		return m.routeAccessMsg(msg.(accessMsg))
	case openGrantAccessMsg:
		// An apply in flight owns the slot: never replace it (its result and
		// the grants it writes must stay with the screen that started it).
		if running, ok := m.adminScreens[slotGrantAccess].(grantAccessScreen); ok && running.applying {
			m.screen = screenAdminGrantAccess
			return m, nil
		}
		// Always a fresh screen: a previous visit's selection must not leak
		// into a new one, and the principal may differ (Users/Robots `g`).
		next := newGrantAccessScreen(msg.returnTo, msg.preselectID)
		m.adminScreens[slotGrantAccess] = next
		m.screen = screenAdminGrantAccess
		m.status = ""
		return m, next.Init(m.screenEnv())
	case grantAccessPrincipalsLoadedMsg, grantAccessGrantsLoadedMsg, grantAccessAppliedMsg:
		// Drop a message from another screen instance FIRST: a stale expiry
		// result must never log the operator out.
		if gen, ok := grantAccessMsgGen(msg); ok {
			if current, isGrant := m.adminScreens[slotGrantAccess].(grantAccessScreen); !isGrant || current.gen != gen {
				return m, nil
			}
		}
		if expired, ok := msg.(interface{ expiredReason() (string, bool) }); ok {
			if reason, isExpired := expired.expiredReason(); isExpired {
				if applied, isApplied := msg.(grantAccessAppliedMsg); isApplied {
					if note, hasNote := applied.expiryNote(); hasNote {
						m = m.expireAdminSession(note)
						m.expiryNoteShown = true
						return m, nil
					}
				}
				return m.expireAdminSession(reason), nil
			}
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case repoAccessGrantsLoadedMsg, repoAccessRevokedMsg:
		// Drop a message from another screen instance FIRST: a stale expiry
		// result must never log the operator out.
		if gen, ok := repoAccessMsgGen(msg); ok {
			if current, isRepoAccess := m.adminScreens[slotRepositoryAccess].(repoAccessScreen); !isRepoAccess || current.gen != gen {
				return m, nil
			}
		}
		if expired, ok := msg.(interface{ expiredReason() (string, bool) }); ok {
			if reason, isExpired := expired.expiredReason(); isExpired {
				return m.expireAdminSession(reason), nil
			}
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case repoAccessOpenPrincipalMsg:
		return m.openRepoAccessPrincipal(msg.principal)
	case returnToInspectionMsg:
		return m.returnToInspection(), nil
	case openUserScreenMsg:
		return m.openUserScreen(msg)
	case userScreenMsg:
		// Drop a result from another screen instance FIRST: a stale expiry
		// result must never log the operator out.
		if !m.userScreenOwns(msg.screenGen()) {
			return m, nil
		}
		if reason, expired := msg.expiredReason(); expired {
			return m.expireAdminSession(reason), nil
		}
		var cmd tea.Cmd
		m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
		return m, cmd
	case adminRepositoryScanSummariesLoadedMsg:
		// Broadcast only (design.md Decision H): the Scan runs and Secret
		// findings screens own what they show.
		if msg.err != nil {
			if IsAdminSessionExpired(msg.err) {
				return m.expireAdminSession(msg.err.Error()), nil
			}
			m.status = msg.err.Error()
			// The screens that asked for the summaries show the failure too.
			m.adminScreens, _ = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
			return m, nil
		}
		if len(msg.summaries) == 0 {
			m.status = "No repository alerts found."
		} else if strings.HasPrefix(strings.ToLower(m.status), "loading") {
			m.status = ""
		}
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
	case delegateLoadedMsg, delegateSavedMsg, delegateRemovedMsg:
		return m.routeDelegateMsg(msg)
	}

	return m, nil
}

// View renders the too-small notice as-is (never bannered), then wraps every real screen's render in withUpdateBanner --
// chrome that must appear regardless of which screen is active, without
// threading a banner parameter through renderConsoleWorkspace/
// renderInspectionWorkspace's ~11 existing call sites (design.md Decision 2:
// "0 of ~11 callers touched" -- this wrapper keeps that true for the banner
// too).
// View wraps viewContent in a tea.View that requests the alternate screen
// (the v2 replacement for tea.WithAltScreen()).
func (m Model) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	return view
}

func (m Model) viewContent() string {
	// Guarded here (design decision #7) rather than in Update: View() is the
	// single funnel both the interactive program loop and the --snapshot
	// CLI path render through.
	if m.tooSmall() {
		return renderTooSmall(newAdminTheme(), tooSmallSpec{
			Width: m.viewport.Width, Height: m.viewport.Height,
			MinWidth: minViewportWidth, MinHeight: minViewportHeight,
		})
	}
	// The version line takes the last row of the terminal: the screen itself
	// is laid out one row shorter, so the two never exceed the height.
	screenModel := m
	if m.currentVersion != "" {
		screenModel.viewport.Height--
	}
	return m.withUpdateBanner(screenModel.viewScreen())
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
	case screenRepositories, screenProjects, screenTags:
		return m.viewConsole()
	case screenManifest:
		status := ""
		if m.showMutationNotice {
			status = fmt.Sprintf("Delete unavailable in v1: %s", m.mutation.Reason)
		}
		help := "b: blobs | u: uploads | d: unsupported delete | Tab: admin | Esc: back | q: quit"
		layout := m.contentBudget(status, help)
		return renderInspectionWorkspace(
			fmt.Sprintf("Repositories / %s / %s / Manifest", m.manifest.Details.Repository, m.manifest.Details.Reference),
			renderConsoleTextSection(renderManifest(newAdminTheme(), m.manifest.Details, m.manifest.Signature, m.manifest.Referrers), layout),
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
		view := loginView{form: m.adminLogin, status: m.status}
		if m.expiryNoteShown {
			view = loginView{form: m.adminLogin, note: m.status}
		}
		return renderAdminLogin(newAdminTheme(), view, m.viewport.Width, m.viewport.Height)
	case screenAdminAuthenticating:
		return renderAdminAuthenticating(newAdminTheme(), m.loadingText, m.viewport.Width, m.viewport.Height)
	case screenAdminUsers, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots, screenAdminCreateRobot, screenSecurityGitleaksRepos, screenSecuritySigningRepos, screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminOverview, screenAdminScanRuns, screenAdminSecretFindings, screenAdminUpdateChannel, screenAdminGrantAccess, screenAdminRepositoryAccess:
		help := "Esc: back | q: quit"
		if slot, ok := slotFor(m.screen); ok && m.adminScreens[slot] != nil {
			// Migrated top-level screen: help is derived from the screen's
			// own Keys(), never a separately hand-written string (spec.md
			// "Keymap-Derived Help Cannot Drift").
			help = shortHelpView(newAdminTheme(), m.adminScreens[slot].Keys())
		}
		layout := m.contentBudget(m.status, help)
		content := renderAdminWorkspace(m.screen, m.adminSession, m.knownRepositories(), m.status, layout, m.now(), m.adminScreens)
		return m.overlayLateNotice(content, layout)
	}

	help := "q: quit"
	return renderInspectionWorkspace("Regixtry", renderConsoleTextSection("Ready.", m.contentBudget("", help)), "", help)
}

func (m Model) updateKey(msg keyMsg) (tea.Model, tea.Cmd) {
	if m.tooSmall() {
		return m.updateTooSmallKey(msg)
	}
	switch keyString(msg) {
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

	if isConsoleScreen(m.screen) {
		return m.updateConsoleKey(msg)
	}

	if keyString(msg) == "tab" {
		return m.openAdmin()
	}

	switch {
	case isMoveUpKey(msg):
		m.moveSelection(-1)
		return m, nil
	case isMoveDownKey(msg):
		m.moveSelection(1)
		return m, nil
	case isBackKey(msg):
		m.showMutationNotice = false
		m.status = ""
		switch m.screen {
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

func (m Model) updateAdminKey(msg keyMsg) (tea.Model, tea.Cmd) {
	// Trivy's config/scan-policy modals, gitleaks' config modal, and
	// signing's policy modal are no longer overlays pre-dispatched here
	// (Phase 11 promotes trivyConfigScreen/gitleaksConfigScreen/
	// signingConfigScreen to top-level screens, addressed via slotFor):
	// each now handles its own modal state inside its own Update, reached
	// through the ordinary routeAdminKey path below.
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

	// The login form, the active user search and the Create User text fields
	// type q; Esc is the way out (Ctrl+C still quits in updateKey).
	if isRuneKey(msg, 'q') && isAdminPrincipalScreen(m.screen) && !m.migratedScreenCapturesTextInput() &&
		!m.adminTextInputFocused() {
		return m, tea.Quit
	}

	return routeAdminKey(m, msg)
}

// adminTextInputFocused reports whether a principal admin screen currently has
// a text input focused, so a bare q must be typed instead of quitting.
func (m Model) adminTextInputFocused() bool {
	switch m.screen {
	case screenAdminLogin:
		return true
	}
	return false
}

func (m Model) updateAdminLoginKey(msg keyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isEscKey(msg):
		if m.expiryNoteShown {
			m.adminSession.ExpiredReason = ""
			m.expiryNoteShown = false
		}
		return m.returnToInspection(), nil
	// Only Tab and the arrows toggle focus: k and j are typed text here.
	case isTabKey(msg), isArrowUpKey(msg), isArrowDownKey(msg):
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
	if isTextKey(msg) {
		m.appendLoginRunes(keyText(msg))
		return m, nil
	}
	return m, nil
}

// Trivy's config/scan-policy modals live on trivyConfigScreen, exactly like
// gitleaksConfigScreen's own config modal and signingConfigScreen's own
// policy modal.

// nextScanPolicyThreshold cycles the 2-value severity threshold, toggled
// with Space per design.md Decision 6.
func nextScanPolicyThreshold(threshold string) string {
	if threshold == ports.ScanPolicyThresholdCriticalHigh {
		return ports.ScanPolicyThresholdCritical
	}
	return ports.ScanPolicyThresholdCriticalHigh
}

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
// openSelectedAdminFindingLink/pageAdminScanHistory live on
// scanHistoryScreen (screen_scan_history.go).

func (m *Model) moveSelection(delta int) {
	if m.screen == screenBlobs {
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
	cachePath := m.updateCheckCachePath
	now := m.now
	return func() tea.Msg {
		if entry, ok := readUpdateCheckCache(cachePath); ok && entry.fresh(now()) {
			if entry.Failed {
				return updateCheckCompletedMsg{err: errUpdateCheckCached}
			}
			return updateCheckCompletedMsg{latestVersion: entry.LatestVersion}
		}

		ctx, cancel := context.WithTimeout(m.ctx, updateCheckTimeout)
		defer cancel()
		channelValue, err := m.service.GetUpdateChannel(ctx)
		if err != nil {
			writeUpdateCheckCache(cachePath, updateCheckCacheEntry{CheckedAt: now(), Failed: true})
			return updateCheckCompletedMsg{err: err}
		}
		channel := release.Channel(channelValue)
		if !release.ValidChannel(string(channel)) {
			writeUpdateCheckCache(cachePath, updateCheckCacheEntry{CheckedAt: now(), Failed: true})
			return updateCheckCompletedMsg{err: fmt.Errorf("release: unknown update channel %q", channelValue)}
		}
		httpClient := &stdhttp.Client{Timeout: updateCheckTimeout}
		latest, err := release.LatestForChannel(ctx, httpClient, regixtryReleasesAPI, channel)
		writeUpdateCheckCache(cachePath, updateCheckCacheEntry{CheckedAt: now(), LatestVersion: latest, Failed: err != nil})
		return updateCheckCompletedMsg{latestVersion: latest, err: err}
	}
}

// updateCheckCacheTTLSuccess is how long a successful background update
// check's result is reused before checkForUpdateCmd performs a real network
// call again, regardless of how many times the TUI is opened in between --
// the standard once-a-day interval tools like brew/npm/kubectl use for this
// same class of background version check, comfortably under GitHub's
// unauthenticated 60-requests/hour-per-IP limit even across many TUI
// sessions in a single day.
const updateCheckCacheTTLSuccess = 24 * time.Hour

// updateCheckCacheTTLFailure is deliberately much shorter than
// updateCheckCacheTTLSuccess: this is the fix for the confirmed incident
// where a failed/rate-limited check kept retrying on literally the next TUI
// open, piling up more requests during an active rate-limit window. One hour
// is short enough that a since-resolved transient failure (a brief network
// blip, a metadata store still initializing) is retried within the same
// working session, while still being long enough that a rate-limit or
// outage episode is not hammered again and again within that same hour.
const updateCheckCacheTTLFailure = 1 * time.Hour

// errUpdateCheckCached is the updateCheckCompletedMsg.err sentinel used when
// a cached failure is still within updateCheckCacheTTLFailure and the real
// network call is skipped entirely. The Update handler treats any non-nil
// err identically (silently skip, never surfaced) -- this value only exists
// so tests can distinguish "cache hit, still failing" from "genuinely
// unreachable."
var errUpdateCheckCached = errors.New("release: cached update check failure, skipping retry")

// updateCheckCacheEntry is the on-disk (JSON) shape of checkForUpdateCmd's
// throttle cache (throttle-tui-update-check feature). CheckedAt is always
// this Model's own injectable now() clock at write time, never time.Now()
// directly -- the same seam every other clock-driven behavior in this file
// already uses (e.g. screenEnv().Now, AdminSession.IsExpired), so tests
// control it deterministically. LatestVersion is empty on a failed check;
// Failed distinguishes "checked, nothing newer" from "the check itself
// failed" so each branch can be throttled by its own TTL (fresh/fresh
// helper below).
type updateCheckCacheEntry struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
	Failed        bool      `json:"failed,omitempty"`
}

// fresh reports whether e is still within its TTL as of now -- a failed
// entry uses the shorter updateCheckCacheTTLFailure, a successful one the
// longer updateCheckCacheTTLSuccess.
func (e updateCheckCacheEntry) fresh(now time.Time) bool {
	ttl := updateCheckCacheTTLSuccess
	if e.Failed {
		ttl = updateCheckCacheTTLFailure
	}
	return now.Sub(e.CheckedAt) < ttl
}

// readUpdateCheckCache loads path's cache entry, failing open on any error
// (no path configured, missing file, unreadable, malformed JSON, a
// zero-value CheckedAt) exactly like
// loadSetupManagedTUIConfig/readSetupManagedEnvFile's own posture for
// optional local state (cmd/regixtry/main.go): a missing or corrupt cache
// must never crash or block the TUI's background update check, only fall
// back to "no cache, perform a real check."
func readUpdateCheckCache(path string) (updateCheckCacheEntry, bool) {
	if strings.TrimSpace(path) == "" {
		return updateCheckCacheEntry{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return updateCheckCacheEntry{}, false
	}
	var entry updateCheckCacheEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return updateCheckCacheEntry{}, false
	}
	if entry.CheckedAt.IsZero() {
		return updateCheckCacheEntry{}, false
	}
	return entry, true
}

// writeUpdateCheckCache best-effort persists entry to path: a write failure
// (read-only filesystem, missing parent directory) must never surface to
// the operator or block the background check's own result -- it only means
// the next TUI open performs a real check again, the same fail-open posture
// readUpdateCheckCache uses for reads. A no-op when path is empty (caching
// disabled).
func writeUpdateCheckCache(path string, entry updateCheckCacheEntry) {
	if strings.TrimSpace(path) == "" {
		return
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, body, 0o644)
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
		// through the Tags screen: this screen's ResolveManifest
		// call above is already gated on ActionPull, the same
		// authorization SignatureStatus itself requires, while the Tags
		// list (TagDetails) is gated on the weaker ActionInspect -- its
		// resolved status was never computed under this screen's own
		// access check, so reusing it here would blur that boundary.
		signature, signatureErr := m.service.SignatureStatus(m.ctx, repository, tag)
		// subjectDigest is manifest.Digest (the resolved manifest's own
		// content-addressed digest), never tag/reference: referrers point at
		// a manifest by digest, and this must find the exact digest being
		// inspected regardless of which tag reference got us here
		// (console-manifest-enrichment change). artifactType "" means every
		// referrer, never a filtered subset.
		referrers, referrersErr := m.service.Referrers(m.ctx, repository, manifest.Digest, "")
		resultErr := uploadsErr
		if resultErr == nil {
			resultErr = signatureErr
		}
		if resultErr == nil {
			resultErr = referrersErr
		}
		return manifestLoadedMsg{repository: repository, tag: tag, manifest: manifest, uploads: uploads, signature: signature, referrers: referrers, err: resultErr}
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

// signingKeyUsageCmd fetches trustedKeyList's delete-key usage-count
// advisory (CountSigningKeyUsage), the screenEnv-scoped equivalent of
// Model's own method below, needed because a sub-model has no Model to call
// a method on.
func signingKeyUsageCmd(env screenEnv, repository string, keyPEM string) tea.Cmd {
	return env.asModel().signingKeyUsageCmd(repository, keyPEM)
}

func (m Model) signingKeyUsageCmd(repository string, keyPEM string) tea.Cmd {
	return func() tea.Msg {
		if m.adminClient == nil {
			return adminSigningKeyUsageLoadedMsg{key: keyPEM, err: fmt.Errorf("admin API is unavailable for this session")}
		}
		count, capped, err := m.adminClient.CountSigningKeyUsage(m.ctx, m.adminSession, repository, keyPEM)
		return adminSigningKeyUsageLoadedMsg{key: keyPEM, count: count, capped: capped, err: err}
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
		// Re-entering an already-authenticated session must land on the same
		// Overview a fresh login lands on (the adminLoginCompletedMsg
		// success handler): for a deployment that forces startup-login that
		// handler routes to the repository catalog instead, which makes
		// this branch the ONLY reachable path into the admin panel.
		m.screen = screenAdminOverview
		overview := newOverviewScreen()
		m.adminScreens[slotOverview] = overview
		return m, overview.Init(m.screenEnv())
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

// openFromOverview opens a destination the Overview asked for that needs
// Model state: the Robots list, a fresh Create robot form, or the Users list
// with a one-line pointer to the token key.
func (m Model) openFromOverview(action overviewOpenAction) (tea.Model, tea.Cmd) {
	switch action {
	case overviewOpenRobots:
		return m.openAdminRobots()
	case overviewOpenCreateRobot:
		return m.mountCreateRobot()
	case overviewOpenUsersForToken:
		return m.openUserScreen(openUserScreenMsg{to: screenAdminUsers, notice: overviewUsersTokenStatus, noticeTone: userNoticeInfo})
	}
	return m, nil
}

// clearUserScreens un-mounts the user-management screens: they hold a user
// list and, in the two forms, typed passwords, none of which may outlive the
// session.
func (m *Model) clearUserScreens() {
	m.adminScreens[slotUsers] = nil
	m.adminScreens[slotEditUser] = nil
	m.adminScreens[slotCreateUser] = nil
	m.adminScreens[slotChangePassword] = nil
}

// userScreenOwns reports whether a mounted user-management screen has the
// given generation, i.e. whether a result carrying it still has a screen to
// land on.
func (m Model) userScreenOwns(gen uint64) bool {
	for _, slot := range []screenSlot{slotUsers, slotEditUser, slotCreateUser, slotChangePassword} {
		switch s := m.adminScreens[slot].(type) {
		case usersScreen:
			if s.gen == gen {
				return true
			}
		case editUserScreen:
			if s.gen == gen {
				return true
			}
		case createUserScreen:
			if s.gen == gen {
				return true
			}
		case resetPasswordScreen:
			if s.gen == gen {
				return true
			}
		}
	}
	return false
}

// openUserScreen mounts a FRESH user-management screen and loads it, or opens
// the Tokens screen for the user. A fresh screen every
// time means a form never shows what was typed on an earlier visit.
func (m Model) openUserScreen(msg openUserScreenMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	// A form left behind (Esc, or another screen opening) must not keep what
	// was typed in it: only the form being opened survives.
	if msg.to != screenAdminCreateUser {
		m.adminScreens[slotCreateUser] = nil
	}
	if msg.to != screenAdminChangePassword {
		m.adminScreens[slotChangePassword] = nil
	}
	switch msg.to {
	case screenAdminEditUserTokens:
		return m.mountTokens(openTokensMsg{principalID: msg.userID, principalName: msg.username})
	case screenAdminEditUser:
		next := newEditUserScreen(msg.userID, msg.username).withNotice(msg.noticeTone, msg.notice)
		m.adminScreens[slotEditUser], m.screen = next, screenAdminEditUser
		return m, next.Init(m.screenEnv())
	case screenAdminCreateUser:
		next := newCreateUserScreen()
		m.adminScreens[slotCreateUser], m.screen = next, screenAdminCreateUser
		return m, next.Init(m.screenEnv())
	case screenAdminChangePassword:
		next := newResetPasswordScreen(msg.userID, msg.username)
		m.adminScreens[slotChangePassword], m.screen = next, screenAdminChangePassword
		return m, next.Init(m.screenEnv())
	}
	next := newUsersScreen(msg.userID).withNotice(msg.noticeTone, msg.notice)
	m.adminScreens[slotUsers], m.screen = next, screenAdminUsers
	return m, next.Init(m.screenEnv())
}

// openRepoAccessPrincipal opens the user or robot behind a grant on the
// repository-access screen: the user's page, or the Robots list for a robot.
func (m Model) openRepoAccessPrincipal(principal grantPrincipal) (tea.Model, tea.Cmd) {
	if principal.Kind == grantKindRobot {
		return m.openAdminRobots()
	}
	return m.openUserScreen(openUserScreenMsg{to: screenAdminEditUser, userID: principal.ID, username: principal.Name})
}

// openAdminRobots opens the Robots list from the Users list ("b") or the
// Overview: always a fresh screen, so the list is read again.
func (m Model) openAdminRobots() (tea.Model, tea.Cmd) { return m.mountRobots("") }

// mountRobots mounts a FRESH Robots screen (loaded on mount) with the robot
// of selectID highlighted once the list arrives.
func (m Model) mountRobots(selectID string) (tea.Model, tea.Cmd) {
	next := newRobotsScreen()
	next.selectID = selectID
	m.adminScreens[slotRobots] = next
	m.screen = screenAdminRobots
	m.status = ""
	return m, next.Init(m.screenEnv())
}

// mountCreateRobot mounts a FRESH Create robot form.
func (m Model) mountCreateRobot() (tea.Model, tea.Cmd) {
	next := newCreateRobotScreen()
	m.adminScreens[slotCreateRobot] = next
	m.screen = screenAdminCreateRobot
	m.status = ""
	return m, next.Init(m.screenEnv())
}

// mountTokens mounts a FRESH Tokens screen for one principal: nothing from an
// earlier visit, a one-time secret above all, can show again.
func (m Model) mountTokens(open openTokensMsg) (tea.Model, tea.Cmd) {
	next := newTokensScreen(open.principalID, open.principalName, open.robot)
	m.adminScreens[slotTokens] = next
	m.screen = screenAdminEditUserTokens
	m.status = ""
	return m, next.Init(m.screenEnv())
}

// routeAccessMsg delivers an async result of the Robots, Create robot or
// Tokens screens. A result of another screen instance is dropped FIRST: a
// stale expiry must never log the operator out. An expired admin session ends
// in the normal re-login; anything else goes to the screens.
func (m Model) routeAccessMsg(msg accessMsg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case robotCreatedMsg, tokenCreatedMsg:
		return m.routeCreatedCredential(msg)
	}
	current, ok := m.adminScreens[msg.accessSlot()].(interface{ generation() uint64 })
	if !ok || current.generation() != msg.accessGen() {
		return m, nil
	}
	if reason, expired := msg.expiredReason(); expired {
		return m.expireAdminSession(reason), nil
	}
	var cmd tea.Cmd
	m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
	return m, cmd
}

// dropAccessScreens un-mounts the Robots, Create robot and Tokens screens so
// the next visit starts fresh and no one-time secret outlives the session.
func (m *Model) dropAccessScreens() {
	m.queuedSecrets, m.lateNotice = nil, ""
	m.adminScreens[slotRobots] = nil
	m.adminScreens[slotCreateRobot] = nil
	m.adminScreens[slotTokens] = nil
}

func (m Model) logoutAdmin() Model {
	m.dropAccessScreens()
	m.clearUserScreens()
	m.adminSession = LogoutAdminState()
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
	m.dropAccessScreens()
	m.screen = m.adminReturn
	m = m.withMountedConsole()
	m.loadingText = ""
	m.status = ""
	// Defensive reset: leaving the login screen (e.g. Esc) without
	// completing auth must not leave a stale repo-grants intent for a
	// later, unrelated login (adminIntent is one-shot per design.md
	// Decision 7).
	m.adminIntent = adminIntentOperator
	m.adminIntentRepository = ""
	return m
}

func (m Model) expireAdminSession(reason string) Model {
	m.expiryNoteShown = false
	// An expired session ends any grant screen, including one stuck applying:
	// the next open must mount a fresh one.
	m.adminScreens[slotGrantAccess] = nil
	m.dropAccessScreens()
	m.clearUserScreens()
	m.adminSession = ExpireAdminState(reason)
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
	case screenAdminLogin, screenAdminAuthenticating, screenAdminUsers, screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots, screenAdminCreateRobot, screenSecurityGitleaksRepos, screenSecuritySigningRepos, screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenAdminOverview, screenAdminScanRuns, screenAdminSecretFindings, screenAdminUpdateChannel, screenAdminGrantAccess, screenAdminRepositoryAccess:
		return true
	default:
		return false
	}
}

// isAdminPrincipalScreen gates the bare 'q' quit key. screenRepoAdminGrants
// is in it, but its grant form types q: migratedScreenCapturesTextInput holds
// the quit key back while the form is open.
//
// The Security screens (configuration and repositories) join this set; the
// repositories screens hold the quit key back while their override editor is
// open (migratedScreenCapturesTextInput). screenAdminUpdateChannel
// (tui-update-check feature) joins the same way.
func isAdminPrincipalScreen(current screen) bool {
	switch current {
	case screenAdminLogin, screenAdminUsers, screenAdminEditUser, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots, screenSecurityTrivy, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenSecurityTrivyRepos, screenSecurityGitleaksRepos, screenSecuritySigningRepos, screenAdminOverview, screenAdminUpdateChannel, screenAdminRepositoryAccess:
		return true
	default:
		return false
	}
}

// migratedScreenCapturesTextInput reports whether the currently mounted
// migrated top-level screen (design.md Decision A/I, resolved via slotFor)
// has its own local overlay open that captures free-text or key input -- a
// confirm prompt, a config/policy modal, or the override editor. None
// of these local overlays is Model state (design.md Decision B: each
// migrated screen owns its own), so the bare 'l' logout / 'q' quit global
// keys must not intercept keystrokes meant for one of these overlays.
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
	case usersScreen:
		return s.searching || s.confirm.Active()
	case editUserScreen:
		return s.confirm.Active()
	case repoAccessScreen:
		return s.filtering || s.confirm.Active()
	case delegateScreen:
		return s.capturesInput()
	case robotsScreen:
		return s.confirm.Active()
	case tokensScreen:
		return s.capturesInput()
	default:
		return false
	}
}

func (m Model) canLogoutAdminFromCurrentScreen() bool {
	if m.adminAuth != adminAuthStateAuthenticated || m.migratedScreenCapturesTextInput() {
		return false
	}

	switch m.screen {
	case screenAdminUsers, screenAdminEditUser, screenAdminEditUserTokens, screenRepoAdminGrants, screenAdminRobots,
		screenSecurityTrivy, screenSecurityTrivyRepos, screenSecurityGitleaksConfig, screenSecuritySigningConfig, screenSecurityGitleaksRepos, screenSecuritySigningRepos,
		screenAdminOverview, screenAdminScanRuns, screenAdminSecretFindings, screenAdminUpdateChannel:
		return true
	default:
		return false
	}
}

func featureActionForKey(msg keyMsg, page ports.FeaturePage) (ports.FeatureAction, bool) {
	text := keyText(msg)
	if text == "" {
		return ports.FeatureAction{}, false
	}
	lookup := map[rune]string{
		'e': "enable",
		'x': "disable",
		'i': "install-runtime",
		'u': "upgrade-runtime",
		'b': "rollback-runtime",
	}
	actionID, ok := lookup[unicode.ToLower([]rune(text)[0])]
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
func renderManifest(theme adminTheme, manifest appregixtry.ManifestDetails, signature appregixtry.SignatureStatusResult, referrers appregixtry.ReferrersIndex) string {
	lines := []string{
		theme.subheading.Render(fmt.Sprintf("Manifest · %s:%s", manifest.Repository, manifest.Reference)),
		fmt.Sprintf("%s %s", theme.muted.Render("Digest:"), theme.text.Render(manifest.Digest)),
		fmt.Sprintf("%s %s", theme.muted.Render("Media Type:"), theme.text.Render(manifest.MediaType)),
		// Manifest Size (the manifest JSON document's own byte size) and
		// Total Image Size (the sum of every blob's size -- config + every
		// layer, i.e. what a pull actually transfers) are deliberately two
		// separate, clearly-labeled lines: they are different numbers and
		// both are meaningful (console-manifest-enrichment change). Total
		// Image Size is 0 for an index-shaped manifest, which has no Blobs
		// of its own -- see the Platforms section below instead.
		fmt.Sprintf("%s %s", theme.muted.Render("Manifest Size:"), theme.text.Render(fmt.Sprintf("%d bytes", manifest.Size))),
		fmt.Sprintf("%s %s", theme.muted.Render("Total Image Size:"), theme.text.Render(fmt.Sprintf("%d bytes", manifest.TotalBlobSize()))),
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
	lines = append(lines, renderManifestPlatformLines(theme, manifest.Platforms)...)
	lines = append(lines, renderManifestReferrerLines(theme, referrers)...)
	lines = append(lines, renderSignatureLines(theme, signature)...)
	return strings.Join(lines, "\n")
}

// renderManifestPlatformLines renders the multi-arch platform breakdown
// (image-index-platform-breakdown change) for an OCI Image Index / Docker
// Manifest List: each child manifest's platform (os/architecture[/variant])
// alongside its digest, size, and mediaType. Returns nil (no "Platforms:"
// header at all) for an ordinary single-image manifest, mirroring the
// Annotations block's own omit-when-empty convention above.
func renderManifestPlatformLines(theme adminTheme, platforms []appregixtry.PlatformDetails) []string {
	if len(platforms) == 0 {
		return nil
	}

	lines := []string{theme.muted.Render("Platforms:")}
	for _, platform := range platforms {
		// "unknown" mirrors how real-world OCI Image Index entries with no
		// platform object at all are conventionally surfaced (rather than a
		// bare "/" from two empty strings) -- distinct from a Platform whose
		// OS/Architecture are merely both absent from an otherwise-present
		// object.
		target := "unknown"
		if platform.OS != "" || platform.Architecture != "" {
			target = platform.OS + "/" + platform.Architecture
			if platform.Variant != "" {
				target += "/" + platform.Variant
			}
		}
		row := fmt.Sprintf("  %s  %s (%d bytes) [%s]", target, platform.Digest, platform.Size, platform.MediaType)
		lines = append(lines, theme.text.Render(row))
	}
	return lines
}

// renderManifestReferrerLines renders the Referrers section (console-
// manifest-enrichment change): manifests whose subject.digest points at the
// inspected one -- signatures, attestations, SBOMs -- fetched via
// QueryService.Referrers (loadManifestCmd). Each row shows the referrer's
// digest, its ArtifactType (falling back to MediaType when ArtifactType is
// empty, matching the OCI 1.1 fallback resolveArtifactType already applies
// server-side), and a one-line annotation summary when it has annotations.
// Returns nil (no "Referrers:" header) when there are none, mirroring the
// Annotations/Platforms blocks' own omit-when-empty convention.
func renderManifestReferrerLines(theme adminTheme, referrers appregixtry.ReferrersIndex) []string {
	if len(referrers.Manifests) == 0 {
		return nil
	}

	lines := []string{theme.muted.Render("Referrers:")}
	for _, referrer := range referrers.Manifests {
		kind := referrer.ArtifactType
		if kind == "" {
			kind = referrer.MediaType
		}
		row := fmt.Sprintf("  %s [%s]", referrer.Digest, kind)
		if summary := summarizeAnnotations(referrer.Annotations); summary != "" {
			row += "  " + summary
		}
		lines = append(lines, theme.text.Render(row))
	}
	return lines
}

// summarizeAnnotations joins a referrer's annotations into a single
// "key=value, key2=value2" line, sorted keys, mirroring renderManifest's own
// sorted-keys rendering for the manifest's own Annotations block above.
// Returns "" for no annotations, so callers can cheaply decide whether to
// append anything at all.
func summarizeAnnotations(annotations map[string]string) string {
	if len(annotations) == 0 {
		return ""
	}

	keys := make([]string, 0, len(annotations))
	for key := range annotations {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, annotations[key]))
	}
	return strings.Join(pairs, ", ")
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

// applyLoadedFeatures/applyFeaturePage/selectedFeatureName/
// isSelectedTrivyFeature/isSelectedGitleaksFeature/isSelectedSigningFeature/
// toggleTrivyTab/selectedScanSummary/trivyConfigModalFromPage/
// nextTrivyConfigField/gitleaksConfigModalFromPage/nextGitleaksConfigField/
// deleteTrivyConfigModalRune/appendTrivyConfigModalRunes/
// trivyConfigInputFromModal/moveAdminFeatureSelection all moved onto
// trivyConfigScreen/gitleaksConfigScreen's own state and methods (Phase 11
// resolved-gap addendum) -- each screen now independently
// owns the piece of this logic scoped to its own fixed feature, per
// design.md Decision D4 (no shared cross-screen state).

package tui

import (
	"errors"
	"strings"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

const AdminSessionExpiredReasonExpired = "Session expired. Log in again."

type adminCreateUserField int

const (
	adminCreateUserFieldUsername adminCreateUserField = iota
	adminCreateUserFieldPassword
	adminCreateUserFieldIsAdmin
	adminCreateUserFieldIsReadOnly
	adminCreateUserFieldEnabled
)

type adminResetPasswordField int

const (
	adminResetPasswordFieldPassword adminResetPasswordField = iota
)

type adminGrantField int

const (
	adminGrantFieldRepository adminGrantField = iota
	adminGrantFieldRole
)

// adminRepoGrantField identifies which of adminRepositoryGrantForm's 2
// fields has focus. A sibling of adminGrantField, not an extension: the
// repo-admin delegate's form has no Repository field (it is fixed by
// RepoAdminRepository, the operator's own repository), only Username+Role.
type adminRepoGrantField int

const (
	adminRepoGrantFieldUsername adminRepoGrantField = iota
	adminRepoGrantFieldRole
)

type adminTokenField int

const (
	adminTokenFieldName adminTokenField = iota
	adminTokenFieldTTL
)

// adminCreateRobotField identifies which of adminCreateRobotForm's 4 fields
// has focus (design.md Decision 7's robot screens): Name -> Repository ->
// Role -> TTL, a global-admin-only form so, unlike adminRepositoryGrantForm,
// Role is not restricted away from RepoRoleAdmin.
type adminCreateRobotField int

const (
	adminCreateRobotFieldName adminCreateRobotField = iota
	adminCreateRobotFieldRepository
	adminCreateRobotFieldRole
	adminCreateRobotFieldTTL
)

type TrivyTab string

const (
	trivyTabRuntime          TrivyTab = "runtime"
	trivyTabRepositoryAlerts TrivyTab = "repository-alerts"
)

type trivyConfigField int

const (
	trivyConfigFieldScheduleEnabled trivyConfigField = iota
	trivyConfigFieldInterval
	trivyConfigFieldTimeout
	trivyConfigFieldRegistryReachableURL
	trivyConfigFieldMaxConcurrency
)

type adminCreateUserForm struct {
	Username   string
	Password   string
	IsAdmin    bool
	IsReadOnly bool
	Enabled    bool
	Focus      adminCreateUserField
}

type adminResetPasswordForm struct {
	NewPassword string
	Focus       adminResetPasswordField
}

type adminGrantForm struct {
	Repository           string
	Role                 domainauth.RepoRole
	Focus                adminGrantField
	RepositorySuggestion int
}

// adminRepositoryGrantForm backs screenRepoAdminAddGrant (design.md
// Decision 7): a repo-admin delegate names a user by username (no user
// directory read) and picks a role. Role's zero value is set to
// domainauth.RepoRoleReader by newAdminViewState, and the field is cycled
// only by nextDelegateGrantRole, which can never produce RepoRoleAdmin.
type adminRepositoryGrantForm struct {
	Username string
	Role     domainauth.RepoRole
	Focus    adminRepoGrantField
}

type adminTokenForm struct {
	Name       string
	TTLSeconds string
	Focus      adminTokenField
}

// adminCreateRobotForm backs screenAdminCreateRobot (design.md Decision 7).
// Role's zero value is set to domainauth.RepoRoleReader by newAdminViewState
// (mirroring adminGrantForm/adminRepositoryGrantForm), and TTLSeconds follows
// adminTokenForm's convention: empty means the service's default TTL.
// RepositorySuggestion mirrors adminGrantForm.RepositorySuggestion: the
// screenAdminAddGrant repository autosuggest pattern (filter as you type,
// Up/Down cycle, Enter commits) reused here so Create Robot behaves
// consistently with Add Grant instead of being a plain free-text field.
type adminCreateRobotForm struct {
	Name                 string
	Repository           string
	Role                 domainauth.RepoRole
	TTLSeconds           string
	Focus                adminCreateRobotField
	RepositorySuggestion int
}

type trivyConfigModal struct {
	Open                 bool
	Focus                trivyConfigField
	ScheduleEnabled      bool
	Interval             string
	Timeout              string
	RegistryReachableURL string
	MaxConcurrency       string
	Error                string
}

func (m trivyConfigModal) Active() bool {
	return m.Open
}

// gitleaksConfigField identifies which of gitleaksConfigModal's 3 fields has
// focus. Unlike trivyConfigField, there is no ScheduleEnabled/Interval pair
// (gitleaks scans immutable content once and never needs periodic
// rescanning, unlike Trivy's CVE database) and no
// RegistryReachableURL/TLS fields (gitleaks scans locally-staged blobs via
// BlobStore, it never pulls from the registry over HTTP).
type gitleaksConfigField int

const (
	gitleaksConfigFieldEnabled gitleaksConfigField = iota
	gitleaksConfigFieldTimeout
	gitleaksConfigFieldMaxConcurrency
)

// gitleaksConfigModal is gitleaks' own global config editor, a sibling
// struct to trivyConfigModal (not an extension of it) mirroring its exact
// shape at a narrower 3-field scope.
type gitleaksConfigModal struct {
	Open           bool
	Focus          gitleaksConfigField
	Enabled        bool
	Timeout        string
	MaxConcurrency string
	Error          string
}

func (m gitleaksConfigModal) Active() bool {
	return m.Open
}

// scanPolicyField identifies which of scanPolicyModal's 2 fields has focus.
type scanPolicyField int

const (
	scanPolicyFieldEnabled scanPolicyField = iota
	scanPolicyFieldThreshold
)

// scanPolicyModal is the vulnerability policy gate's own admin modal
// (design.md Decision 6 — a sibling struct, deliberately NOT an extension
// of trivyConfigModal, so the two features stay separately configurable
// surfaces per spec's "separate surface" requirement).
type scanPolicyModal struct {
	Open              bool
	Focus             scanPolicyField
	Enabled           bool
	SeverityThreshold string
	Error             string
}

func (m scanPolicyModal) Active() bool {
	return m.Open
}

// nextScanPolicyField cycles between the modal's 2 fields with a wrapping
// cursor, mirroring nextTrivyConfigField.
func nextScanPolicyField(field scanPolicyField) scanPolicyField {
	if field >= scanPolicyFieldThreshold {
		return scanPolicyFieldEnabled
	}
	return field + 1
}

// signingPolicyField identifies which of signingPolicyModal's 3 fields has
// focus. Unlike scanPolicyField's 2-field shape, signing's global policy
// has a growable list of trusted keys, so the third field (ClearKeys) is an
// action row rather than an input, mirroring
// repositoryOverrideFieldClear's shape (design.md Decision 11 piece 1).
type signingPolicyField int

const (
	signingPolicyFieldEnabled signingPolicyField = iota
	signingPolicyFieldUnsignedSelfRead
	signingPolicyFieldAddKey
	signingPolicyFieldClearKeys // action row, not an input
)

// signingPolicyModal is the image-signing content-trust gate's own admin
// modal, a sibling of scanPolicyModal (design.md Decision 11 piece 1 — NOT
// an extension of it, and NOT an extension of repositoryOverrideModal
// either). Fingerprints is a read-only, derived projection of the currently
// stored trusted keys (SHA-256/12, never the raw PEM -- the modal never
// displays key material) so the operator can confirm what is configured
// without a multi-line PEM viewport. AddKey is one PEM, entered as a single
// line (any whitespace arrangement -- signing.NormalizePublicKeyPEM
// reconstructs canonical PEM either way), submitted with Enter to append it
// to the stored key set. UnsignedSelfRead mirrors
// ports.SigningPolicySettings.UnsignedSelfRead, always held here as one of
// unsignedSelfReadCycle's three canonical values (never ""), cycled with
// Space like repositoryOverrideModal.Feature.
type signingPolicyModal struct {
	Open             bool
	Focus            signingPolicyField
	Enabled          bool
	UnsignedSelfRead string   // "off" | "pusher" | "repo_push" -- see type doc comment
	AddKey           string   // one PEM, single line -- see type doc comment
	Fingerprints     []string // read-only SHA-256/12 of each stored key
	Loading          bool
	Error            string
}

func (m signingPolicyModal) Active() bool {
	return m.Open
}

// nextSigningPolicyField cycles between the modal's 4 fields with a
// wrapping cursor, mirroring nextScanPolicyField/nextRepositoryOverrideField.
func nextSigningPolicyField(field signingPolicyField) signingPolicyField {
	if field >= signingPolicyFieldClearKeys {
		return signingPolicyFieldEnabled
	}
	return field + 1
}

// unsignedSelfReadCycle is signingPolicyModal/repositoryOverrideModal's
// UnsignedSelfRead field cycle order, mirroring
// repositoryOverrideFeatureCycle's shape. "" (the wire/storage value for "no
// exemption") is never a cycle member -- normalizeUnsignedSelfRead maps it
// to "off" before it ever reaches the modal.
var unsignedSelfReadCycle = []string{"off", "pusher", "repo_push"}

// normalizeUnsignedSelfRead maps ports.SigningPolicySettings/SigningOverride's
// wire value "" (no exemption configured) onto the modal's canonical "off",
// so the modal field is always one of unsignedSelfReadCycle's three values.
func normalizeUnsignedSelfRead(value string) string {
	if value == "" {
		return "off"
	}
	return value
}

// nextUnsignedSelfReadValue cycles the UnsignedSelfRead field through
// unsignedSelfReadCycle, wrapping back to the first entry, normalizing ""
// first so cycling from an unseeded modal still lands on a real value.
func nextUnsignedSelfReadValue(value string) string {
	normalized := normalizeUnsignedSelfRead(value)
	for index, candidate := range unsignedSelfReadCycle {
		if candidate == normalized {
			return unsignedSelfReadCycle[(index+1)%len(unsignedSelfReadCycle)]
		}
	}
	return unsignedSelfReadCycle[0]
}

// repositoryOverrideModal and repositoryOverrideField were retired by
// tui-menu-architecture (design.md Decision F): the per-repository override
// editor is now overrideEditor (override_editor.go), whose Feature is
// unexported and constructor-only rather than a focusable, cycle-driven
// field. See operator-admin-tui's MODIFIED requirement "Repository-Scoped
// Override Modal On The Repository Alerts Row".

// adminScanHistoryTabKind identifies one feature's tab inside the scan
// history modal (design.md "Ordered tab slice with a wrapping cursor").
type adminScanHistoryTabKind string

const (
	adminScanHistoryTabVulnerabilities adminScanHistoryTabKind = "vulnerabilities"
	adminScanHistoryTabLeaks           adminScanHistoryTabKind = "leaks"
)

// adminScanHistoryTab is one entry in the scan history modal's ordered,
// extensible tab set. A third feature's tab is one more slice entry, no
// restructuring (spec.md "Per-Feature Modal Tabs").
type adminScanHistoryTab struct {
	Kind  adminScanHistoryTabKind
	Label string
}

// newAdminScanHistoryTabs returns the modal's default ordered tab set.
// Leaks is always present alongside Vulnerabilities (design.md interface
// comment on adminScanHistoryModal.Tabs).
func newAdminScanHistoryTabs() []adminScanHistoryTab {
	return []adminScanHistoryTab{
		{Kind: adminScanHistoryTabVulnerabilities, Label: "Vulnerabilities"},
		{Kind: adminScanHistoryTabLeaks, Label: "Leaks"},
	}
}

// adminScanHistoryModal is the state for the Repository Alerts drill-down
// modal, rendered as a true floating overlay via compositeOverlay
// (claude-handoff.md), opened by Enter on a Repository Alerts summary row
// (spec.md "Repository Alert Drill-Down Opens History Modal").
type adminScanHistoryModal struct {
	Open       bool
	Repository string
	Tabs       []adminScanHistoryTab
	ActiveTab  int
	Runs       []ports.ScanRun // newest first
	Cursor     int             // which execution in history (Left/Right)
	Detail     ports.ScanRunDetail
	Secrets    []ports.SecretFinding
	Loading    bool
	Error      string
	// FindingCursor is which row is selected WITHIN the currently active
	// tab's own table (Up/Down) -- distinct from Cursor, which navigates
	// between executions (Left/Right). Reset to 0 on every tab switch and
	// every execution page so it never points past the end of a freshly
	// loaded, possibly shorter list.
	FindingCursor int
}

func (m adminScanHistoryModal) Active() bool {
	return m.Open
}

type AdminSession struct {
	Username      string
	BearerToken   string
	ExpiresAt     time.Time
	ExpiredReason string
}

type AdminViewState struct {
	Users             []ports.AdminUser
	SelectedUser      int
	SelectedUserID    string
	SelectedUsername  string
	UserSearchQuery   string
	UserSearchActive  bool
	Grants            []ports.AdminRepoGrant
	SelectedGrant     int
	AdminTokens       []ports.AdminToken
	SelectedToken     int
	CreateUserForm    adminCreateUserForm
	ResetPasswordForm adminResetPasswordForm
	GrantForm         adminGrantForm
	TokenForm         adminTokenForm
	// Confirm is THE confirm-before-destructive-action primitive
	// (design.md Decision E, D7), replacing adminConfirmModal's Kind-union.
	// Still shared across every legacy admin screen; the Security &
	// Compliance domain's own screens (Phase 11) each own their own local
	// confirm field instead (design.md Decision B: a migrated screen cannot
	// write to AdminViewState directly).
	Confirm                confirmPrompt
	RevealedTokenSecret    string
	RevealedTokenAccessor  string
	RevealedTokenExpiresAt time.Time
	// ScanHistoryModal is retired (Phase 19, design.md's State Migration
	// table): its state now lives on scanHistoryScreen
	// (screen_scan_history.go), reached at its own dedicated slot
	// (screen.go's slotScanHistory), never as an AdminViewState field.
	// RepositoryOverrideModal is retired (tui-menu-architecture, design.md
	// Decision F): the per-repository override editor now lives per-screen
	// as overrideEditor, embedded directly in trivyReposScreen/
	// featureOverridesScreen.
	// RepoAdminRepository/RepoAdminGrants/SelectedRepoAdminGrant/
	// RepoAdminGrantForm back screenRepoAdminGrants/screenRepoAdminAddGrant
	// (design.md Decision 7): the repo-admin delegate's own repository-
	// centric grants view, a sibling of the global-admin
	// Grants/SelectedGrant/GrantForm fields above, not an extension of them
	// — a delegate's Grants view is keyed by repository, not by SelectedUserID.
	RepoAdminRepository string
	RepoAdminGrants     []ports.AdminRepositoryGrant
	// RepoAdminGrantsAuthorized is true only after the most recent
	// screenRepoAdminGrants load actually succeeded (adminRepoGrantsLoadedMsg
	// with a nil err), distinct from "loaded successfully with zero grants".
	// A 403 from GET .../grants (repository-administrator privileges
	// required) leaves this false. Every call site that dispatches
	// loadRepoAdminGrantsCmd must reset this to false first, so a stale
	// true from a PREVIOUS repository's successful load can never leak into
	// a NEW repository's screen before its own load response arrives.
	RepoAdminGrantsAuthorized bool
	SelectedRepoAdminGrant    int
	RepoAdminGrantForm        adminRepositoryGrantForm
	// Robots/SelectedRobot/CreateRobotForm back screenAdminRobots/
	// screenAdminCreateRobot (design.md Decision 7): global-admin-only
	// screens mirroring Users/SelectedUser/CreateUserForm, a sibling data
	// set rather than an extension -- a robot is never listed in Users
	// (store.go's ListUsers excludes is_robot rows) and has no
	// SelectedUserID-keyed edit screen of its own; enable/disable and token
	// issuance reuse the existing user routes/screens via SelectedUserID.
	Robots          []ports.AdminRobot
	SelectedRobot   int
	CreateRobotForm adminCreateRobotForm
	// Layout is the consoleLayout used the last time rebuildAdminTables ran,
	// including the primary/compact table pageSize split (design.md
	// decision #6). It is a snapshot for table construction, not the live
	// render-time budget — renderAdminWorkspace/renderAdminScreen receive a
	// freshly computed layout on every View() call instead.
	Layout consoleLayout
}

type AdminSessionExpiredError struct {
	Reason string
}

func NewAdminSessionExpiredError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = AdminSessionExpiredReasonExpired
	}
	return AdminSessionExpiredError{Reason: reason}
}

func IsAdminSessionExpired(err error) bool {
	var target AdminSessionExpiredError
	return errors.As(err, &target)
}

func (e AdminSessionExpiredError) Error() string {
	if strings.TrimSpace(e.Reason) == "" {
		return AdminSessionExpiredReasonExpired
	}
	return e.Reason
}

func (s AdminSession) IsAuthenticated() bool {
	return strings.TrimSpace(s.BearerToken) != ""
}

func (s AdminSession) IsExpired(now time.Time) bool {
	if !s.IsAuthenticated() || s.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(s.ExpiresAt)
}

func (s AdminSession) Remaining(now time.Time) time.Duration {
	if s.ExpiresAt.IsZero() {
		return 0
	}
	if s.IsExpired(now) {
		return 0
	}
	return s.ExpiresAt.Sub(now)
}

func newAdminViewState() AdminViewState {
	return AdminViewState{
		CreateUserForm: adminCreateUserForm{
			Enabled: true,
		},
		GrantForm: adminGrantForm{
			Role: domainauth.RepoRoleReader,
		},
		RepoAdminGrantForm: adminRepositoryGrantForm{
			Role: domainauth.RepoRoleReader,
		},
		CreateRobotForm: adminCreateRobotForm{
			Role: domainauth.RepoRoleReader,
		},
	}
}

func LogoutAdminState() (AdminSession, AdminViewState) {
	return AdminSession{}, newAdminViewState()
}

func ExpireAdminState(reason string) (AdminSession, AdminViewState) {
	return AdminSession{ExpiredReason: strings.TrimSpace(reason)}, newAdminViewState()
}

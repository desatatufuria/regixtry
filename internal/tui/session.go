package tui

import (
	"errors"
	"strings"
	"time"

	bubbletable "github.com/evertras/bubble-table/table"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

const AdminSessionExpiredReasonExpired = "Session expired. Log in again."

type adminCreateUserField int

const (
	adminCreateUserFieldUsername adminCreateUserField = iota
	adminCreateUserFieldPassword
	adminCreateUserFieldIsAdmin
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

type adminTokenField int

const (
	adminTokenFieldName adminTokenField = iota
	adminTokenFieldTTL
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

type adminConfirmKind string

const (
	adminConfirmNone           adminConfirmKind = ""
	adminConfirmEnableUser     adminConfirmKind = "enable-user"
	adminConfirmDisableUser    adminConfirmKind = "disable-user"
	adminConfirmEnableFeature  adminConfirmKind = "enable-feature"
	adminConfirmDisableFeature adminConfirmKind = "disable-feature"
	adminConfirmDeleteGrant    adminConfirmKind = "delete-grant"
	adminConfirmRevokeToken    adminConfirmKind = "revoke-token"
)

type adminCreateUserForm struct {
	Username string
	Password string
	IsAdmin  bool
	Enabled  bool
	Focus    adminCreateUserField
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

type adminTokenForm struct {
	Name       string
	TTLSeconds string
	Focus      adminTokenField
}

type adminConfirmModal struct {
	Kind        adminConfirmKind
	Title       string
	Message     string
	ConfirmText string
	UserID      string
	Username    string
	FeatureName string
	Repository  string
	Accessor    string
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

func (m adminConfirmModal) Active() bool {
	return m.Kind != adminConfirmNone
}

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
// modal (design.md "Nested budget by row split, not overlay, not
// stacking"), opened by Enter on a Repository Alerts summary row (spec.md
// "Repository Alert Drill-Down Opens History Modal").
type adminScanHistoryModal struct {
	Open       bool
	Repository string
	Tabs       []adminScanHistoryTab
	ActiveTab  int
	Runs       []ports.ScanRun // newest first
	Cursor     int
	Detail     ports.ScanRunDetail
	Secrets    []ports.SecretFinding
	Loading    bool
	Error      string
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

type adminTableSelection struct {
	FeatureName string
	FindingID   string
}

type adminTablesState struct {
	Features       bubbletable.Model
	FeatureRows    map[string]bubbletable.Model
	Findings       bubbletable.Model
	SecretFindings bubbletable.Model
	// ScanSummary is the per-repository Repository Alerts summary table
	// (buildAdminScanSummaryTable), rendered by renderAdminScanSummary — the
	// sole table backing the Repository Alerts tab.
	ScanSummary bubbletable.Model
	Selection   adminTableSelection
}

type AdminViewState struct {
	Users                  []ports.AdminUser
	Features               []ports.FeatureSummary
	SelectedUser           int
	SelectedFeature        int
	SelectedUserID         string
	SelectedUsername       string
	UserSearchQuery        string
	UserSearchActive       bool
	FeaturePage            ports.FeaturePage
	Grants                 []ports.AdminRepoGrant
	SelectedGrant          int
	AdminTokens            []ports.AdminToken
	SelectedToken          int
	CreateUserForm         adminCreateUserForm
	ResetPasswordForm      adminResetPasswordForm
	GrantForm              adminGrantForm
	TokenForm              adminTokenForm
	ConfirmModal           adminConfirmModal
	TrivyTab               TrivyTab
	TrivyConfigModal       trivyConfigModal
	TrivyScanRuns          []ports.ScanRun
	TrivySelectedAlert     int
	TrivyAlertsLoaded      bool
	RevealedTokenSecret    string
	RevealedTokenAccessor  string
	RevealedTokenExpiresAt time.Time
	// TrivySummaries is the per-repository aggregation
	// (summarizeScanRunsByRepository) backing the Repository Alerts summary
	// table (spec.md "Repository Alerts Summarized Per Repository With
	// Ordering And Freshness"), derived from TrivyScanRuns.
	TrivySummaries []repositorySummary
	// ScanHistoryModal is the Repository Alerts drill-down modal state
	// (spec.md "Repository Alert Drill-Down Opens History Modal"), opened by
	// Enter on a summary row (updateAdminFeaturesKey).
	ScanHistoryModal adminScanHistoryModal
	Tables           adminTablesState
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
		TrivyTab: trivyTabRuntime,
		Tables: adminTablesState{
			FeatureRows: make(map[string]bubbletable.Model),
		},
	}
}

func LogoutAdminState() (AdminSession, AdminViewState) {
	return AdminSession{}, newAdminViewState()
}

func ExpireAdminState(reason string) (AdminSession, AdminViewState) {
	return AdminSession{ExpiredReason: strings.TrimSpace(reason)}, newAdminViewState()
}

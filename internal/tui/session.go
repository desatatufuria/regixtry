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

type AdminSession struct {
	Username      string
	BearerToken   string
	ExpiresAt     time.Time
	ExpiredReason string
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
	TrivyAlertDetailOpen   bool
	TrivyAlertsLoaded      bool
	RevealedTokenSecret    string
	RevealedTokenAccessor  string
	RevealedTokenExpiresAt time.Time
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
	}
}

func LogoutAdminState() (AdminSession, AdminViewState) {
	return AdminSession{}, newAdminViewState()
}

func ExpireAdminState(reason string) (AdminSession, AdminViewState) {
	return AdminSession{ExpiredReason: strings.TrimSpace(reason)}, newAdminViewState()
}

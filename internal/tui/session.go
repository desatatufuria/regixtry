package tui

import (
	"errors"
	"strings"
	"time"

	"regixtry/internal/ports"
)

const AdminSessionExpiredReasonExpired = "Session expired. Log in again."

type AdminSession struct {
	Username      string
	BearerToken   string
	ExpiresAt     time.Time
	ExpiredReason string
}

type AdminViewState struct {
	Users            []ports.AdminUser
	SelectedUser     int
	SelectedUserID   string
	SelectedUsername string
	Grants           []ports.AdminRepoGrant
	AdminTokens      []ports.AdminToken
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

func LogoutAdminState() (AdminSession, AdminViewState) {
	return AdminSession{}, AdminViewState{}
}

func ExpireAdminState(reason string) (AdminSession, AdminViewState) {
	return AdminSession{ExpiredReason: strings.TrimSpace(reason)}, AdminViewState{}
}

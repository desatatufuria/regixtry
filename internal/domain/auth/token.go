package auth

import (
	"strings"
	"time"
)

type TokenKind string

const (
	TokenKindAccess          TokenKind = "access"
	TokenKindAdminCredential TokenKind = "admin-credential"

	AccessTokenTTL       = 15 * time.Minute
	DefaultAdminTokenTTL = 30 * 24 * time.Hour
)

type Token struct {
	ID         string
	UserID     string
	Kind       TokenKind
	Name       string
	Scope      string
	Accessor   string
	SecretHash string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

func (t Token) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return NewValidationError("token id is required")
	}

	if strings.TrimSpace(t.UserID) == "" {
		return NewValidationError("token user id is required")
	}

	if t.Kind != TokenKindAccess && t.Kind != TokenKindAdminCredential {
		return NewValidationError("token kind is invalid")
	}

	if strings.TrimSpace(t.Accessor) == "" {
		return NewValidationError("token accessor is required")
	}

	if strings.TrimSpace(t.SecretHash) == "" {
		return NewValidationError("token secret hash is required")
	}

	if t.CreatedAt.IsZero() {
		return NewValidationError("token created at is required")
	}

	if t.ExpiresAt.IsZero() {
		return NewValidationError("token expires at is required")
	}

	if !t.ExpiresAt.After(t.CreatedAt) {
		return NewValidationError("token expiry must be after creation")
	}

	if t.Kind == TokenKindAccess && t.Name != "" {
		return NewValidationError("access tokens must not have a display name")
	}

	if t.Kind == TokenKindAdminCredential && strings.TrimSpace(t.Scope) != "" {
		return NewValidationError("admin credential tokens must not store access scope")
	}

	if _, err := t.Scopes(); err != nil {
		return err
	}

	return nil
}

func (t Token) Scopes() ([]Scope, error) {
	trimmed := strings.TrimSpace(t.Scope)
	if trimmed == "" {
		return nil, nil
	}

	return ParseScopes([]string{trimmed})
}

func (t Token) IsExpired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

func (t Token) IsRevoked() bool {
	return t.RevokedAt != nil
}

func (t Token) IsActive(now time.Time) error {
	if t.IsRevoked() {
		return NewRevokedTokenError(t.Accessor)
	}

	if t.IsExpired(now) {
		return NewExpiredTokenError(t.Accessor)
	}

	return nil
}

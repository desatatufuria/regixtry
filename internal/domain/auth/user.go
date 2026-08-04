package auth

import (
	"regexp"
	"strings"
	"time"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	IsAdmin      bool
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (u User) Validate() error {
	username := strings.TrimSpace(u.Username)
	if username == "" {
		return NewValidationError("username is required")
	}

	if !usernamePattern.MatchString(username) {
		return NewValidationError("username must contain only letters, numbers, dot, dash, or underscore")
	}

	if strings.TrimSpace(u.ID) == "" {
		return NewValidationError("user id is required")
	}

	if strings.TrimSpace(u.PasswordHash) == "" {
		return NewValidationError("password hash is required")
	}

	if u.CreatedAt.IsZero() {
		return NewValidationError("created at is required")
	}

	if u.UpdatedAt.IsZero() {
		return NewValidationError("updated at is required")
	}

	return nil
}

func (u User) Active() bool {
	return u.Enabled
}

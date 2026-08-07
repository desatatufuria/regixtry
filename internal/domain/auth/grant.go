package auth

import (
	"time"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

type RepoRole string

const (
	RepoRoleReader RepoRole = "repo-reader"
	RepoRoleWriter RepoRole = "repo-writer"
	RepoRoleAdmin  RepoRole = "repo-admin"
)

type RepoGrant struct {
	UserID     string
	Repository regixtrydomain.RepositoryRef
	Role       RepoRole
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (r RepoRole) Validate() error {
	switch r {
	case RepoRoleReader, RepoRoleWriter, RepoRoleAdmin:
		return nil
	default:
		return NewValidationError("repository role is invalid")
	}
}

func (r RepoRole) AllowsRead() bool {
	return r == RepoRoleReader || r == RepoRoleWriter || r == RepoRoleAdmin
}

func (r RepoRole) AllowsWrite() bool {
	return r == RepoRoleWriter || r == RepoRoleAdmin
}

func (r RepoRole) AllowsAdmin() bool {
	return r == RepoRoleAdmin
}

func (g RepoGrant) Validate() error {
	if g.UserID == "" {
		return NewValidationError("grant user id is required")
	}

	if err := g.Repository.Validate(); err != nil {
		return err
	}

	if err := g.Role.Validate(); err != nil {
		return err
	}

	if g.CreatedAt.IsZero() {
		return NewValidationError("grant created at is required")
	}

	if g.UpdatedAt.IsZero() {
		return NewValidationError("grant updated at is required")
	}

	return nil
}

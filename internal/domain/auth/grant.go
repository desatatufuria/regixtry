package auth

import (
	"strings"
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

// ProjectGrant gives a user one role over every repository of a project: the
// repositories whose first path segment equals Project. It reuses RepoRole so
// the role vocabulary and its predicates stay in one place.
type ProjectGrant struct {
	UserID    string
	Project   string
	Role      RepoRole
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (g ProjectGrant) Validate() error {
	if g.UserID == "" {
		return NewValidationError("grant user id is required")
	}

	// A project name is one valid repository path segment: reuse the
	// repository parser so both stay in sync, and reject any slash.
	if strings.Contains(g.Project, "/") {
		return NewValidationError("project name must not contain a slash")
	}
	if _, err := regixtrydomain.ParseRepositoryRef(g.Project); err != nil {
		return NewValidationError("project name is invalid")
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

// roleRank orders the roles reader < writer < admin; an unknown role ranks 0.
func roleRank(role RepoRole) int {
	switch role {
	case RepoRoleReader:
		return 1
	case RepoRoleWriter:
		return 2
	case RepoRoleAdmin:
		return 3
	default:
		return 0
	}
}

// projectOf returns the project of a repository name: the segment before the
// first '/'. A flat name (no slash) belongs to no project. This is a local
// copy of the registry domain's rule (a test pins their equivalence); the
// comparison against a grant is always == on the whole segment, never a prefix.
func projectOf(repository string) (string, bool) {
	i := strings.IndexByte(repository, '/')
	if i <= 0 {
		return "", false
	}

	return repository[:i], true
}

// EffectiveRole returns the highest role the exact repository grant and the
// grant of the repository's project give. Grants only add: an exact grant
// can widen what the project gives, never narrow it.
func EffectiveRole(grants []RepoGrant, projectGrants []ProjectGrant, repository string) (RepoRole, bool) {
	var best RepoRole
	for _, grant := range grants {
		if grant.Repository.String() == repository && roleRank(grant.Role) > roleRank(best) {
			best = grant.Role
		}
	}
	if project, ok := projectOf(repository); ok {
		for _, grant := range projectGrants {
			if grant.Project == project && roleRank(grant.Role) > roleRank(best) {
				best = grant.Role
			}
		}
	}

	return best, roleRank(best) > 0
}

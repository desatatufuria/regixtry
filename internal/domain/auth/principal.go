package auth

import "time"

type Principal struct {
	Subject    string
	UserID     string
	Username   string
	IsAdmin    bool
	IsReadOnly bool
	Grants     []RepoGrant
	// ProjectGrants give a role over every repository of a project (first
	// path segment, compared with ==).
	ProjectGrants []ProjectGrant
	Scopes        []Scope
	ExpiresAt     time.Time
}

// IsUnrestricted reports whether the principal authenticated with a token that
// carries no scope limits (the zero-scope login the TUI and CLI use). A
// scope-limited registry access token (for example repository:x:pull) is not
// unrestricted and must not open the administrative API.
func (p Principal) IsUnrestricted() bool {
	return len(p.Scopes) == 0
}

func (p Principal) HasReadAccess(repository string) bool {
	return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsRead) && p.scopeAllowsRepository(repository, Scope.AllowsPull)
}

func (p Principal) HasWriteAccess(repository string) bool {
	return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsWrite) && p.scopeAllowsRepository(repository, Scope.AllowsPush)
}

// HasGrantedWriteAccess deliberately checks only the grant half of
// HasWriteAccess, never the token's current scope. Callers that need "is
// this principal generally authorized to push here" independent of what a
// particular in-flight request happened to be scoped for -- e.g. a
// pull-scoped request checking whether the same underlying identity also
// holds push authority -- must use this, not HasWriteAccess, for the exact
// reason HasRepoAdminAccess's callers already read Grants directly instead
// of coupling to a push-scoped token: a request whose whole point is a read
// will almost never itself carry push scope, even when the identity behind
// it unquestionably has standing push authorization.
func (p Principal) HasGrantedWriteAccess(repository string) bool {
	return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsWrite)
}

// HasDeleteAccess deliberately does NOT reuse HasWriteAccess: that predicate
// checks Scope.AllowsPush, which a pull,push-scoped token satisfies and
// would wrongly let it delete. The role half stays RepoRole.AllowsWrite
// (the bound repo-writer decision); only the scope half changes to
// Scope.AllowsDelete (design.md Decision 5).
func (p Principal) HasDeleteAccess(repository string) bool {
	return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsWrite) && p.scopeAllowsRepository(repository, Scope.AllowsDelete)
}

func (p Principal) HasRepoAdminAccess(repository string) bool {
	if !p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsAdmin) {
		return false
	}

	return p.scopeAllowsRepository(repository, Scope.AllowsPush)
}

// HasGrantedRepoAdminAccess reports whether the principal is a global admin or
// holds repo-admin on the repository, directly or through its project. Like
// HasGrantedWriteAccess it checks only the grant half, never the token scope:
// admin-session tokens carry zero scopes.
func (p Principal) HasGrantedRepoAdminAccess(repository string) bool {
	return p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsAdmin)
}

func (p Principal) HasCatalogAccess(repository string) bool {
	if !p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsRead) {
		return false
	}

	return p.scopeAllowsCatalog() || p.scopeAllowsRepository(repository, Scope.AllowsPull)
}

// HasUniversalCatalogAccess reports that HasCatalogAccess is true for every
// repository: a global admin or registry-wide reader whose token carries the
// catalog scope. Callers use it only to skip per-repository filtering; a
// scope-limited admin (repository scopes only) is not universal.
func (p Principal) HasUniversalCatalogAccess() bool {
	return (p.IsAdmin || p.IsReadOnly) && p.scopeAllowsCatalog()
}

func (p Principal) CanAccessCatalog() bool {
	if p.scopeAllowsCatalog() {
		return true
	}

	for _, scope := range p.Scopes {
		if scope.IsRepository() && scope.AllowsPull() {
			return true
		}
	}

	return false
}

func (p Principal) hasGrantedRepositoryAccess(repository string, allows func(RepoRole) bool) bool {
	if p.IsAdmin {
		return true
	}

	// Registry-wide read: probe the caller's own predicate with
	// RepoRoleReader instead of comparing strings. Of the three call sites'
	// predicates (grant.go:34-44) only AllowsRead is true for
	// RepoRoleReader, so this grants read everywhere and can never widen to
	// write or admin — even if a fourth predicate is added later.
	if p.IsReadOnly && allows(RepoRoleReader) {
		return true
	}

	// Effective access is the exact repository grant OR the grant of the
	// repository's project (first segment, ==). One loop per source, so all
	// six predicates built on this function inherit project grants.
	for _, grant := range p.Grants {
		if grant.Repository.String() == repository && allows(p.cappedRole(grant.Role)) {
			return true
		}
	}

	if project, ok := projectOf(repository); ok {
		for _, grant := range p.ProjectGrants {
			if grant.Project == project && allows(p.cappedRole(grant.Role)) {
				return true
			}
		}
	}

	return false
}

// cappedRole is the single read-only decision for grants: a global read-only
// principal is held at reader whatever role a grant row carries, so rows that
// predate the rule (a writer or admin grant on a user later made read-only)
// have no effect. Every decision built on grants goes through it.
func (p Principal) cappedRole(role RepoRole) RepoRole {
	if p.IsReadOnly {
		return RepoRoleReader
	}

	return role
}

func (p Principal) scopeAllowsRepository(repository string, allows func(Scope) bool) bool {
	for _, scope := range p.Scopes {
		if scope.IsRepository() && scope.Name == repository && allows(scope) {
			return true
		}
	}

	return false
}

func (p Principal) scopeAllowsCatalog() bool {
	for _, scope := range p.Scopes {
		if scope.IsRegixtryCatalog() {
			return true
		}
	}

	return false
}

// HasGrantedProjectAdminAccess reports whether the principal holds a project
// admin grant on exactly this project. It is the one place the project
// delegation decision lives; a read-only principal is capped at read and so
// never administers.
func (p Principal) HasGrantedProjectAdminAccess(project string) bool {
	for _, grant := range p.ProjectGrants {
		if grant.Project == project && p.cappedRole(grant.Role).AllowsAdmin() {
			return true
		}
	}

	return false
}

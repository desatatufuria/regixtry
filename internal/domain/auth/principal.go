package auth

import "time"

type Principal struct {
	Subject    string
	UserID     string
	Username   string
	IsAdmin    bool
	IsReadOnly bool
	Grants     []RepoGrant
	Scopes     []Scope
	ExpiresAt  time.Time
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

func (p Principal) HasCatalogAccess(repository string) bool {
	if !p.hasGrantedRepositoryAccess(repository, RepoRole.AllowsRead) {
		return false
	}

	return p.scopeAllowsCatalog() || p.scopeAllowsRepository(repository, Scope.AllowsPull)
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

	for _, grant := range p.Grants {
		if grant.Repository.String() == repository && allows(grant.Role) {
			return true
		}
	}

	return false
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

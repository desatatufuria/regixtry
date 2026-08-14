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

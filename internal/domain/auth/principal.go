package auth

import "time"

type Principal struct {
	Subject   string
	UserID    string
	Username  string
	IsAdmin   bool
	Grants    []RepoGrant
	ExpiresAt time.Time
}

func (p Principal) HasReadAccess(repository string) bool {
	if p.IsAdmin {
		return true
	}

	for _, grant := range p.Grants {
		if grant.Repository.String() == repository && grant.Role.AllowsRead() {
			return true
		}
	}

	return false
}

func (p Principal) HasWriteAccess(repository string) bool {
	if p.IsAdmin {
		return true
	}

	for _, grant := range p.Grants {
		if grant.Repository.String() == repository && grant.Role.AllowsWrite() {
			return true
		}
	}

	return false
}

func (p Principal) HasRepoAdminAccess(repository string) bool {
	if p.IsAdmin {
		return true
	}

	for _, grant := range p.Grants {
		if grant.Repository.String() == repository && grant.Role.AllowsAdmin() {
			return true
		}
	}

	return false
}

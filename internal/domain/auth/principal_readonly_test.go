package auth

import (
	"testing"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

func exactPrincipal(t *testing.T, repository string, role RepoRole) Principal {
	t.Helper()
	ref, err := regixtrydomain.ParseRepositoryRef(repository)
	if err != nil {
		t.Fatal(err)
	}
	return Principal{Grants: []RepoGrant{{UserID: "u1", Repository: ref, Role: role}}}
}

// TestReadOnlyPrincipalNeverExceedsReadFromAnyGrant pins S1: a global
// read-only user keeps registry-wide read, and no exact or project grant, of
// any role (including legacy rows that predate the rule), lifts it above read.
func TestReadOnlyPrincipalNeverExceedsReadFromAnyGrant(t *testing.T) {
	t.Parallel()

	const repository = "odabank/payments"
	for _, role := range []RepoRole{RepoRoleReader, RepoRoleWriter, RepoRoleAdmin} {
		for source, build := range map[string]func(*testing.T) Principal{
			"exact":   func(t *testing.T) Principal { return exactPrincipal(t, repository, role) },
			"project": func(*testing.T) Principal { return projectPrincipal("odabank", role) },
		} {
			t.Run(string(role)+"/"+source, func(t *testing.T) {
				t.Parallel()
				p := build(t)
				p.IsReadOnly = true
				p.Scopes = append(fullScopes(repository), Scope{Type: "registry", Name: "catalog", Actions: []string{"*"}})
				want := map[string]bool{"read": true, "write": false, "grantedWrite": false, "delete": false, "repoAdmin": false, "catalog": true}
				for name, predicate := range predicates() {
					if got := predicate(p, repository); got != want[name] {
						t.Errorf("%s = %t, want %t", name, got, want[name])
					}
				}
				if p.HasGrantedRepoAdminAccess(repository) {
					t.Error("HasGrantedRepoAdminAccess = true for a read-only principal")
				}
				if p.HasGrantedProjectAdminAccess("odabank") {
					t.Error("HasGrantedProjectAdminAccess = true for a read-only principal")
				}
			})
		}
	}
}

// TestNonReadOnlyGrantDecisionsAreUnchanged pins the existing decisions of
// ordinary users and of a global admin that is also flagged read-only.
func TestNonReadOnlyGrantDecisionsAreUnchanged(t *testing.T) {
	t.Parallel()

	const repository = "odabank/payments"
	for _, role := range []RepoRole{RepoRoleReader, RepoRoleWriter, RepoRoleAdmin} {
		p := projectPrincipal("odabank", role)
		p.Scopes = fullScopes(repository)
		if got := p.HasGrantedProjectAdminAccess("odabank"); got != role.AllowsAdmin() {
			t.Errorf("project %s: HasGrantedProjectAdminAccess = %t", role, got)
		}
		if p.HasGrantedProjectAdminAccess("odabank2") {
			t.Errorf("project %s: admin of another project", role)
		}
		if got := p.HasGrantedRepoAdminAccess(repository); got != role.AllowsAdmin() {
			t.Errorf("project %s: HasGrantedRepoAdminAccess = %t", role, got)
		}
		if got := p.HasWriteAccess(repository); got != role.AllowsWrite() {
			t.Errorf("project %s: HasWriteAccess = %t", role, got)
		}
		e := exactPrincipal(t, repository, role)
		if got := e.HasGrantedWriteAccess(repository); got != role.AllowsWrite() {
			t.Errorf("exact %s: HasGrantedWriteAccess = %t", role, got)
		}
	}

	admin := Principal{IsAdmin: true, IsReadOnly: true, Scopes: fullScopes(repository)}
	if !admin.HasGrantedRepoAdminAccess(repository) || !admin.HasDeleteAccess(repository) {
		t.Error("a global admin flagged read-only lost admin access")
	}
}

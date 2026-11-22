package auth

import (
	"strings"
	"testing"
	"time"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

func projectPrincipal(project string, role RepoRole) Principal {
	return Principal{ProjectGrants: []ProjectGrant{{UserID: "u1", Project: project, Role: role}}}
}

// predicates lists the six grant-based predicates so every matching test
// covers all of them with one table.
func predicates() map[string]func(Principal, string) bool {
	return map[string]func(Principal, string) bool{
		"read":         Principal.HasReadAccess,
		"write":        Principal.HasWriteAccess,
		"grantedWrite": Principal.HasGrantedWriteAccess,
		"delete":       Principal.HasDeleteAccess,
		"repoAdmin":    Principal.HasRepoAdminAccess,
		"catalog":      Principal.HasCatalogAccess,
	}
}

func fullScopes(repository string) []Scope {
	return []Scope{{Type: "repository", Name: repository, Actions: []string{"pull", "push", "delete"}}}
}

// TestProjectGrantMatchesFirstSegmentExactly pins S2: the project of a
// repository is its first segment compared with ==, never a prefix.
func TestProjectGrantMatchesFirstSegmentExactly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repository string
		covered    bool
	}{
		{"repository inside the project", "odabank/payments/strike", true},
		{"repository directly under the project", "odabank/x", true},
		{"similar project name is not covered", "odabank2/x", false},
		{"longer project name sharing the prefix is not covered", "odabankx/payments/strike", false},
		{"shorter project name is not covered", "odaban/x", false},
		{"flat repository with the project name is not covered", "odabank", false},
		{"other project", "other/x", false},
		{"project name only as a later segment", "other/odabank/x", false},
	}

	for name, predicate := range predicates() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				principal := projectPrincipal("odabank", RepoRoleAdmin)
				principal.Scopes = fullScopes(tt.repository)
				if got := predicate(principal, tt.repository); got != tt.covered {
					t.Fatalf("%s(%q) = %t, want %t", name, tt.repository, got, tt.covered)
				}
			})
		}
	}
}

func TestProjectGrantRoleMatrix(t *testing.T) {
	t.Parallel()

	const repository = "odabank/payments/strike"
	tests := []struct {
		role RepoRole
		want map[string]bool
	}{
		{RepoRoleReader, map[string]bool{"read": true, "write": false, "grantedWrite": false, "delete": false, "repoAdmin": false, "catalog": true}},
		{RepoRoleWriter, map[string]bool{"read": true, "write": true, "grantedWrite": true, "delete": true, "repoAdmin": false, "catalog": true}},
		{RepoRoleAdmin, map[string]bool{"read": true, "write": true, "grantedWrite": true, "delete": true, "repoAdmin": true, "catalog": true}},
	}

	for _, tt := range tests {
		for name, predicate := range predicates() {
			t.Run(string(tt.role)+"/"+name, func(t *testing.T) {
				t.Parallel()
				principal := projectPrincipal("odabank", tt.role)
				principal.Scopes = fullScopes(repository)
				if got := predicate(principal, repository); got != tt.want[name] {
					t.Fatalf("%s = %t, want %t", name, got, tt.want[name])
				}
			})
		}
	}
}

func TestProjectGrantStillRequiresTokenScope(t *testing.T) {
	t.Parallel()

	principal := projectPrincipal("odabank", RepoRoleAdmin)
	principal.Scopes = []Scope{{Type: "repository", Name: "odabank/payments/strike", Actions: []string{"pull"}}}
	if principal.HasWriteAccess("odabank/payments/strike") || principal.HasDeleteAccess("odabank/payments/strike") || principal.HasRepoAdminAccess("odabank/payments/strike") {
		t.Fatal("a pull-scoped token must not gain push, delete or admin from a project admin grant")
	}
	if principal.HasReadAccess("odabank/other") {
		t.Fatal("a token scoped to one repository must not read another repository of the project")
	}
}

func TestEffectiveRoleIsTheHighestOfProjectAndExactGrant(t *testing.T) {
	t.Parallel()

	repo := regixtrydomain.MustParseRepositoryRef("odabank/x")
	tests := []struct {
		name    string
		project RepoRole
		exact   RepoRole
		want    RepoRole
		ok      bool
	}{
		{"project reader plus repo writer", RepoRoleReader, RepoRoleWriter, RepoRoleWriter, true},
		{"project writer plus repo reader keeps writer", RepoRoleWriter, RepoRoleReader, RepoRoleWriter, true},
		{"project admin plus repo reader keeps admin", RepoRoleAdmin, RepoRoleReader, RepoRoleAdmin, true},
		{"project only", RepoRoleWriter, "", RepoRoleWriter, true},
		{"repo only", "", RepoRoleReader, RepoRoleReader, true},
		{"neither", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var principal Principal
			if tt.project != "" {
				principal.ProjectGrants = []ProjectGrant{{Project: "odabank", Role: tt.project}}
			}
			if tt.exact != "" {
				principal.Grants = []RepoGrant{{Repository: repo, Role: tt.exact}}
			}
			got, ok := EffectiveRole(principal.Grants, principal.ProjectGrants, repo.String())
			if got != tt.want || ok != tt.ok {
				t.Fatalf("EffectiveRole() = %q, %t, want %q, %t", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestProjectGrantDoesNotChangeGlobalSemantics(t *testing.T) {
	t.Parallel()

	admin := Principal{IsAdmin: true}
	if !admin.HasGrantedRepoAdminAccess("x/y") || !admin.HasGrantedWriteAccess("flat") {
		t.Fatal("global admin must keep admin everywhere")
	}
	readOnly := Principal{IsReadOnly: true, ProjectGrants: []ProjectGrant{{Project: "odabank", Role: RepoRoleReader}}}
	if readOnly.HasGrantedWriteAccess("odabank/x") || readOnly.HasGrantedRepoAdminAccess("odabank/x") {
		t.Fatal("read-only principal must not gain write or admin from a project grant")
	}
	if !readOnly.hasGrantedRepositoryAccess("flat", RepoRole.AllowsRead) {
		t.Fatal("read-only principal must keep registry-wide read")
	}
}

// TestProjectOfMatchesRegixtryRule pins the local helper to the rule stated
// by the registry domain: the first segment before the first '/'; flat names
// have no project. The auth domain keeps its own tiny copy so the matching
// rule stays reviewable next to the predicates.
func TestProjectOfMatchesRegixtryRule(t *testing.T) {
	t.Parallel()

	rule := func(name string) (string, bool) {
		i := strings.IndexByte(name, '/')
		if i <= 0 {
			return "", false
		}
		return name[:i], true
	}
	for _, name := range []string{"", "a", "a/b", "a/b/c", "/a", "a/", "odabank", "odabank2/x", "a//b", "A/b", "a b/c"} {
		gotProject, gotOK := projectOf(name)
		wantProject, wantOK := rule(name)
		if gotProject != wantProject || gotOK != wantOK {
			t.Fatalf("projectOf(%q) = %q, %t, want %q, %t", name, gotProject, gotOK, wantProject, wantOK)
		}
	}
}

func TestProjectGrantValidate(t *testing.T) {
	t.Parallel()

	now := time.Now()
	valid := ProjectGrant{UserID: "u1", Project: "odabank", Role: RepoRoleWriter, CreatedAt: now, UpdatedAt: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for name, mutate := range map[string]func(*ProjectGrant){
		"missing user":   func(g *ProjectGrant) { g.UserID = "" },
		"empty project":  func(g *ProjectGrant) { g.Project = "" },
		"slash":          func(g *ProjectGrant) { g.Project = "a/b" },
		"bad characters": func(g *ProjectGrant) { g.Project = "Bad Name" },
		"bad role":       func(g *ProjectGrant) { g.Role = "owner" },
		"no created":     func(g *ProjectGrant) { g.CreatedAt = time.Time{} },
		"no updated":     func(g *ProjectGrant) { g.UpdatedAt = time.Time{} },
	} {
		g := valid
		mutate(&g)
		if err := g.Validate(); err == nil {
			t.Fatalf("%s: Validate() = nil, want error", name)
		}
	}
}

func TestHasUniversalCatalogAccessImpliesCatalogAccessEverywhere(t *testing.T) {
	t.Parallel()

	catalog := Scope{Type: "regixtry", Name: "catalog", Actions: []string{"*"}}
	pullOne := Scope{Type: "repository", Name: "a/b", Actions: []string{"pull"}}
	tests := []struct {
		name      string
		principal Principal
		want      bool
	}{
		{"admin with catalog scope", Principal{IsAdmin: true, Scopes: []Scope{catalog}}, true},
		{"read-only with catalog scope", Principal{IsReadOnly: true, Scopes: []Scope{catalog}}, true},
		{"admin limited to one repository", Principal{IsAdmin: true, Scopes: []Scope{pullOne}}, false},
		{"admin without scopes", Principal{IsAdmin: true}, false},
		{"grant holder with catalog scope", Principal{Grants: []RepoGrant{{Role: RepoRoleReader}}, Scopes: []Scope{catalog}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.principal.HasUniversalCatalogAccess(); got != tt.want {
				t.Fatalf("HasUniversalCatalogAccess() = %t, want %t", got, tt.want)
			}
			if tt.want {
				for _, repo := range []string{"x/y", "flat", "a/b"} {
					if !tt.principal.HasCatalogAccess(repo) {
						t.Fatalf("universal principal lacks catalog access to %q", repo)
					}
				}
			}
		})
	}
}

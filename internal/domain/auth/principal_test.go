package auth

import (
	"testing"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

// TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe pins design.md
// Decision 5: a registry-wide IsReadOnly principal probes hasGrantedRepositoryAccess
// with RepoRoleReader instead of comparing against a stored grant, so it reads
// everywhere, never writes, never administers, and an unflagged principal's
// behavior is byte-identical to before this role existed.
func TestPrincipalHasGrantedRepositoryAccessReadOnlyProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		principal  Principal
		repository string
		allows     func(RepoRole) bool
		want       bool
	}{
		{
			name:       "read-only principal allows read on any repository, even without a grant",
			principal:  Principal{IsReadOnly: true},
			repository: "team/app",
			allows:     RepoRole.AllowsRead,
			want:       true,
		},
		{
			name:       "read-only principal allows read on a different, unrelated repository",
			principal:  Principal{IsReadOnly: true},
			repository: "another/repo",
			allows:     RepoRole.AllowsRead,
			want:       true,
		},
		{
			name:       "read-only principal is denied write access",
			principal:  Principal{IsReadOnly: true},
			repository: "team/app",
			allows:     RepoRole.AllowsWrite,
			want:       false,
		},
		{
			name:       "read-only principal is denied admin access",
			principal:  Principal{IsReadOnly: true},
			repository: "team/app",
			allows:     RepoRole.AllowsAdmin,
			want:       false,
		},
		{
			name:       "unflagged principal without a grant is denied read, matching pre-existing behavior",
			principal:  Principal{},
			repository: "team/app",
			allows:     RepoRole.AllowsRead,
			want:       false,
		},
		{
			name: "unflagged principal with an explicit grant is unaffected by the read-only branch",
			principal: Principal{
				Grants: []RepoGrant{{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: RepoRoleReader}},
			},
			repository: "team/app",
			allows:     RepoRole.AllowsRead,
			want:       true,
		},
		{
			name:       "admin principal still allows everything regardless of the read-only branch",
			principal:  Principal{IsAdmin: true},
			repository: "team/app",
			allows:     RepoRole.AllowsAdmin,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.principal.hasGrantedRepositoryAccess(tt.repository, tt.allows)
			if got != tt.want {
				t.Fatalf("hasGrantedRepositoryAccess(%q) = %v, want %v", tt.repository, got, tt.want)
			}
		})
	}
}

// TestPrincipalHasGrantedWriteAccessIgnoresTokenScope is the RED test for
// the signing UnsignedSelfRead "repo_push" mode bug found live in
// production: a request whose whole point is a read (e.g. cosign's own GET
// of the manifest it is about to sign) is issued with a pull-only scoped
// token even when the same identity has standing push authorization, so
// HasWriteAccess (which additionally requires Scope.AllowsPush on THIS
// token) always returns false for exactly that scenario. HasGrantedWriteAccess
// must return true from the grant alone, regardless of the token's scope.
func TestPrincipalHasGrantedWriteAccessIgnoresTokenScope(t *testing.T) {
	t.Parallel()

	writerGrant := RepoGrant{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: RepoRoleWriter}
	readerGrant := RepoGrant{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: RepoRoleReader}
	pullOnlyScope := Scope{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}

	tests := []struct {
		name       string
		principal  Principal
		repository string
		want       bool
	}{
		{
			name:       "writer grant with a pull-only scoped token still passes",
			principal:  Principal{Grants: []RepoGrant{writerGrant}, Scopes: []Scope{pullOnlyScope}},
			repository: "team/app",
			want:       true,
		},
		{
			name:       "writer grant with no scope at all still passes",
			principal:  Principal{Grants: []RepoGrant{writerGrant}},
			repository: "team/app",
			want:       true,
		},
		{
			name:       "reader grant fails regardless of scope",
			principal:  Principal{Grants: []RepoGrant{readerGrant}, Scopes: []Scope{pullOnlyScope}},
			repository: "team/app",
			want:       false,
		},
		{
			name:       "no grant at all fails",
			principal:  Principal{Scopes: []Scope{pullOnlyScope}},
			repository: "team/app",
			want:       false,
		},
		{
			name:       "admin passes without any explicit grant",
			principal:  Principal{IsAdmin: true},
			repository: "team/app",
			want:       true,
		},
		{
			name:       "read-only with no explicit grant fails",
			principal:  Principal{IsReadOnly: true, Scopes: []Scope{pullOnlyScope}},
			repository: "team/app",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.principal.HasGrantedWriteAccess(tt.repository)
			if got != tt.want {
				t.Fatalf("HasGrantedWriteAccess(%q) = %v, want %v", tt.repository, got, tt.want)
			}
		})
	}
}

// TestPrincipalHasDeleteAccess pins design.md Decision 5: HasDeleteAccess
// does NOT reuse HasWriteAccess (which checks Scope.AllowsPush and would
// wrongly let a pull,push-scoped token pass). It requires both a
// repo-writer-or-higher role grant AND a scope that explicitly carries the
// delete action (manifest-blob-delete tasks.md 1.7).
func TestPrincipalHasDeleteAccess(t *testing.T) {
	t.Parallel()

	writerGrant := RepoGrant{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: RepoRoleWriter}
	readerGrant := RepoGrant{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: RepoRoleReader}

	deleteScope := Scope{Type: "repository", Name: "team/app", Actions: []string{"pull", "push", "delete"}, Canonical: "repository:team/app:pull,push,delete"}
	pushOnlyScope := Scope{Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}

	tests := []struct {
		name       string
		principal  Principal
		repository string
		want       bool
	}{
		{
			name:       "writer with delete scope passes",
			principal:  Principal{Grants: []RepoGrant{writerGrant}, Scopes: []Scope{deleteScope}},
			repository: "team/app",
			want:       true,
		},
		{
			name:       "writer with pull,push scope only fails",
			principal:  Principal{Grants: []RepoGrant{writerGrant}, Scopes: []Scope{pushOnlyScope}},
			repository: "team/app",
			want:       false,
		},
		{
			name:       "reader with delete scope fails",
			principal:  Principal{Grants: []RepoGrant{readerGrant}, Scopes: []Scope{deleteScope}},
			repository: "team/app",
			want:       false,
		},
		{
			name:       "admin with delete scope passes",
			principal:  Principal{IsAdmin: true, Scopes: []Scope{deleteScope}},
			repository: "team/app",
			want:       true,
		},
		{
			name:       "read-only fails even with delete scope",
			principal:  Principal{IsReadOnly: true, Scopes: []Scope{deleteScope}},
			repository: "team/app",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.principal.HasDeleteAccess(tt.repository)
			if got != tt.want {
				t.Fatalf("HasDeleteAccess(%q) = %v, want %v", tt.repository, got, tt.want)
			}
		})
	}
}

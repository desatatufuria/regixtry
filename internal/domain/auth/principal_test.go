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

package auth

import "testing"

// TestParseScopeAcceptsPullPushDelete supersedes the 1.2 characterization
// test now that delete is a real scope action (design.md Decision 4):
// pull,push,delete is accepted and canonicalizes in pull,push,delete order
// regardless of input order, while an action outside the allow-list still
// fails (manifest-blob-delete tasks.md 1.3).
func TestParseScopeAcceptsPullPushDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		raw           string
		wantActions   []string
		wantCanonical string
		wantErr       bool
	}{
		{
			name:          "pull,push,delete accepted in order",
			raw:           "repository:x:pull,push,delete",
			wantActions:   []string{"pull", "push", "delete"},
			wantCanonical: "repository:x:pull,push,delete",
		},
		{
			name:          "delete,pull,push canonicalizes to pull,push,delete",
			raw:           "repository:x:delete,pull,push",
			wantActions:   []string{"pull", "push", "delete"},
			wantCanonical: "repository:x:pull,push,delete",
		},
		{
			name:          "delete alone canonicalizes on its own",
			raw:           "repository:x:delete",
			wantActions:   []string{"delete"},
			wantCanonical: "repository:x:delete",
		},
		{
			name:    "unknown action still fails",
			raw:     "repository:x:pull,unknown",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scope, err := ParseScope(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected ParseScope to reject the unknown action, got nil error")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseScope(%q) error = %v", tt.raw, err)
			}
			if len(scope.Actions) != len(tt.wantActions) {
				t.Fatalf("Actions = %#v, want %#v", scope.Actions, tt.wantActions)
			}
			for i, action := range tt.wantActions {
				if scope.Actions[i] != action {
					t.Fatalf("Actions = %#v, want %#v", scope.Actions, tt.wantActions)
				}
			}
			if scope.Canonical != tt.wantCanonical {
				t.Fatalf("Canonical = %q, want %q", scope.Canonical, tt.wantCanonical)
			}
		})
	}
}

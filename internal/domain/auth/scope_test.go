package auth

import "testing"

// TestParseScopeRejectsPullPushDeleteToday pins scope.go's current behavior
// (design.md Decision 4, mandatory ordering constraint): normalizeScopeActions
// rejects any repository scope action other than pull/push, so a client that
// requests delete today is refused, not silently downgraded. This MUST land
// and pass before any task changes that rejection (manifest-blob-delete
// tasks.md 1.2).
func TestParseScopeRejectsPullPushDeleteToday(t *testing.T) {
	t.Parallel()

	_, err := ParseScope("repository:x:pull,push,delete")
	if err == nil {
		t.Fatal("expected ParseScope to reject the delete action, got nil error")
	}

	const wantMessage = "VALIDATION_FAILED: repository scope actions must be pull and/or push"
	if got := err.Error(); got != wantMessage {
		t.Fatalf("error = %q, want %q", got, wantMessage)
	}
}

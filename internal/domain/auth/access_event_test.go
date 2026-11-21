package auth

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAccessActionIsKnown(t *testing.T) {
	t.Parallel()

	for _, action := range []AccessAction{AccessActionGrantPut, AccessActionGrantDelete, AccessActionTokenIssued, AccessActionTokenRevoked, AccessActionRobotCreated} {
		if !action.IsKnown() {
			t.Errorf("%q should be known", action)
		}
	}
	for _, action := range []AccessAction{"", "grant", "grant.PUT", "user.deleted"} {
		if action.IsKnown() {
			t.Errorf("%q should be unknown", action)
		}
	}
}

func TestAccessActionValues(t *testing.T) {
	t.Parallel()

	want := map[AccessAction]string{
		AccessActionGrantPut:     "grant.put",
		AccessActionGrantDelete:  "grant.delete",
		AccessActionTokenIssued:  "token.issued",
		AccessActionTokenRevoked: "token.revoked",
		AccessActionRobotCreated: "robot.created",
	}
	for action, value := range want {
		if string(action) != value {
			t.Errorf("action = %q, want %q", action, value)
		}
	}
}

func TestAccessEventValidate(t *testing.T) {
	t.Parallel()

	valid := AccessEvent{ID: "e1", OccurredAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Action: AccessActionGrantPut}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AccessEvent)
	}{
		{"missing id", func(e *AccessEvent) { e.ID = " " }},
		{"zero time", func(e *AccessEvent) { e.OccurredAt = time.Time{} }},
		{"unknown action", func(e *AccessEvent) { e.Action = "nope" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := valid
			tt.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want a validation error")
			}
		})
	}
}

func TestAccessEventNormalizedCapsFreeText(t *testing.T) {
	t.Parallel()

	runes := func(n int, r string) string { return strings.Repeat(r, n) }
	tests := []struct {
		name  string
		input string
		limit int
		same  bool
	}{
		{"empty", "", AccessEventTextLimit, true},
		{"exactly at the limit", runes(AccessEventTextLimit, "a"), AccessEventTextLimit, true},
		{"one over the limit", runes(AccessEventTextLimit+1, "a"), AccessEventTextLimit, false},
		{"one megabyte", runes(1<<20, "x"), AccessEventTextLimit, false},
		{"multibyte at the limit", runes(AccessEventTextLimit, "ñ"), AccessEventTextLimit, true},
		{"multibyte over the limit", runes(AccessEventTextLimit+5, "日"), AccessEventTextLimit, false},
		{"four byte runes over the limit", runes(AccessEventTextLimit+5, "😀"), AccessEventTextLimit, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			event := AccessEvent{
				ID: "e", OccurredAt: time.Now(), Action: AccessActionGrantPut,
				ActorUsername: tt.input, SubjectUsername: tt.input, TokenName: tt.input, Detail: tt.input, Role: tt.input,
			}
			got := event.Normalized()
			for _, value := range []string{got.ActorUsername, got.SubjectUsername, got.TokenName, got.Detail, got.Role} {
				if !utf8.ValidString(value) {
					t.Fatal("a cut must never split a multibyte rune")
				}
				if utf8.RuneCountInString(value) > tt.limit {
					t.Fatalf("length = %d runes, want at most %d", utf8.RuneCountInString(value), tt.limit)
				}
				if tt.same && value != tt.input {
					t.Fatal("a value within the limit must be kept as is")
				}
				if !tt.same && !strings.HasSuffix(value, "…") {
					t.Fatal("a cut value must end with an ellipsis")
				}
			}
			if again := got.Normalized(); again != got {
				t.Fatal("Normalized must be idempotent")
			}
		})
	}
}

func TestAccessEventNormalizedRepositoryHasItsOwnLimit(t *testing.T) {
	t.Parallel()

	at := AccessEvent{Repository: strings.Repeat("r", AccessEventRepositoryLimit)}
	if got := at.Normalized().Repository; got != at.Repository {
		t.Fatal("a repository at the limit must be kept")
	}
	over := AccessEvent{Repository: strings.Repeat("r", AccessEventRepositoryLimit+1)}
	got := over.Normalized().Repository
	if utf8.RuneCountInString(got) != AccessEventRepositoryLimit || !strings.HasSuffix(got, "…") {
		t.Fatalf("repository = %d runes", utf8.RuneCountInString(got))
	}
}

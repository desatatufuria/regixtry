package auth

import (
	"testing"
	"time"
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

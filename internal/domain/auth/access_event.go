package auth

import (
	"strings"
	"time"
)

// AccessAction names one kind of access change recorded in the audit trail.
type AccessAction string

const (
	// AccessActionGrantPut is a repository grant given or whose role changed.
	AccessActionGrantPut AccessAction = "grant.put"
	// AccessActionGrantDelete is a repository grant revoked.
	AccessActionGrantDelete AccessAction = "grant.delete"
	// AccessActionTokenIssued is an admin credential token issued.
	AccessActionTokenIssued AccessAction = "token.issued"
	// AccessActionTokenRevoked is an admin credential token revoked.
	AccessActionTokenRevoked AccessAction = "token.revoked"
	// AccessActionRobotCreated is a robot created together with its first
	// grant and token: one event for the whole operation.
	AccessActionRobotCreated AccessAction = "robot.created"
)

// IsKnown reports whether the action is one of the recorded kinds.
func (a AccessAction) IsKnown() bool {
	switch a {
	case AccessActionGrantPut, AccessActionGrantDelete, AccessActionTokenIssued, AccessActionTokenRevoked, AccessActionRobotCreated:
		return true
	}

	return false
}

// AccessEvent is one row of the access change history: who changed what and
// when. It carries names and short human text only, never a secret, hash,
// accessor or token id. Optional fields are empty when they do not apply.
type AccessEvent struct {
	ID              string
	OccurredAt      time.Time
	Action          AccessAction
	ActorUserID     string
	ActorUsername   string
	SubjectUserID   string
	SubjectUsername string
	Repository      string
	Role            string
	TokenName       string
	// Detail is short human text such as "Reader to Writer" or "30 days".
	Detail string
}

// Validate checks the fields the history cannot do without.
func (e AccessEvent) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return NewValidationError("access event id is required")
	}
	if e.OccurredAt.IsZero() {
		return NewValidationError("access event time is required")
	}
	if !e.Action.IsKnown() {
		return NewValidationError("access event action is not recognized")
	}

	return nil
}

package auth

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// AccessEventTextLimit is the most runes a name, role or detail keeps in
	// an access event, ellipsis included.
	AccessEventTextLimit = 200
	// AccessEventRepositoryLimit is the most runes a repository keeps.
	AccessEventRepositoryLimit = 255

	accessEventEllipsis = "…"
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

// Normalized returns the event with every free-text field capped, so a huge
// name can never be stored whole or served back whole. It is the one place
// that cap lives: both the service and the store apply it, and applying it
// twice changes nothing.
func (e AccessEvent) Normalized() AccessEvent {
	e.ActorUsername = capRunes(e.ActorUsername, AccessEventTextLimit)
	e.SubjectUsername = capRunes(e.SubjectUsername, AccessEventTextLimit)
	e.Repository = capRunes(e.Repository, AccessEventRepositoryLimit)
	e.Role = capRunes(e.Role, AccessEventTextLimit)
	e.TokenName = capRunes(e.TokenName, AccessEventTextLimit)
	e.Detail = capRunes(e.Detail, AccessEventTextLimit)

	return e
}

// capRunes keeps at most limit runes, ellipsis included, cutting on a rune
// boundary.
func capRunes(value string, limit int) string {
	// Every rune takes at least one byte, so a short string cannot be over.
	if len(value) <= limit || utf8.RuneCountInString(value) <= limit {
		return value
	}

	kept := 0
	for index := range value {
		if kept == limit-1 {
			return value[:index] + accessEventEllipsis
		}
		kept++
	}

	return value
}

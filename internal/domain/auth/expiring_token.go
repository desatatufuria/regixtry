package auth

import "time"

// ExpiringToken is the read model of an admin credential that is about to
// expire, joined with its owner. It deliberately has no secret hash.
type ExpiringToken struct {
	ID        string
	UserID    string
	Username  string
	IsRobot   bool
	Name      string
	Accessor  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

package auth

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestUserValidateRejectsRobotThatIsAdminOrReadOnly pins design.md
// Decision 1: a robot is a machine identity with no global role, so
// Validate must reject IsRobot combined with IsAdmin or IsReadOnly, while
// a plain robot (neither flag) and every pre-existing human combination
// stay valid.
func TestUserValidateRejectsRobotThatIsAdminOrReadOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := User{ID: "user-1", Username: "ci", PasswordHash: RobotPasswordHash, CreatedAt: now, UpdatedAt: now}

	tests := []struct {
		name    string
		user    User
		wantErr bool
	}{
		{
			name:    "robot that is admin is rejected",
			user:    func() User { u := base; u.IsRobot = true; u.IsAdmin = true; return u }(),
			wantErr: true,
		},
		{
			name:    "robot that is read-only is rejected",
			user:    func() User { u := base; u.IsRobot = true; u.IsReadOnly = true; return u }(),
			wantErr: true,
		},
		{
			name:    "robot that is admin and read-only is rejected",
			user:    func() User { u := base; u.IsRobot = true; u.IsAdmin = true; u.IsReadOnly = true; return u }(),
			wantErr: true,
		},
		{
			name:    "plain robot with neither flag is valid",
			user:    func() User { u := base; u.IsRobot = true; return u }(),
			wantErr: false,
		},
		{
			name:    "non-robot admin is unaffected",
			user:    func() User { u := base; u.PasswordHash = "hash"; u.IsAdmin = true; return u }(),
			wantErr: false,
		},
		{
			name:    "non-robot read-only is unaffected",
			user:    func() User { u := base; u.PasswordHash = "hash"; u.IsReadOnly = true; return u }(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.user.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

// TestRobotPasswordHashNeverSatisfiesBcryptComparison pins design.md
// Decision 1's second independent layer: RobotPasswordHash is deliberately
// not a valid bcrypt hash, so bcrypt.CompareHashAndPassword must reject it
// against any candidate password, proving the crypto layer alone blocks a
// robot's password login regardless of any IsRobot flag check.
func TestRobotPasswordHashNeverSatisfiesBcryptComparison(t *testing.T) {
	t.Parallel()

	candidates := []string{"", "password123", RobotPasswordHash, "robot:no-password-guess"}

	for _, candidate := range candidates {
		t.Run(candidate, func(t *testing.T) {
			t.Parallel()

			if err := bcrypt.CompareHashAndPassword([]byte(RobotPasswordHash), []byte(candidate)); err == nil {
				t.Fatalf("bcrypt.CompareHashAndPassword(RobotPasswordHash, %q) = nil, want error", candidate)
			}
		})
	}
}

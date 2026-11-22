package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestValidatePasswordPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		password string
		wantErr  string
	}{
		{"too short", "abc1234", "password must be at least 8 characters"},
		{"spaces do not count", "  abc   ", "password must be at least 8 characters"},
		{"minimum length is 8, not 15", "x9!Kd2#q", ""},
		{"exactly 8 unusual characters", "tq7#Lm2z", ""},
		{"common password", "password123", "password is too common"},
		{"common password, upper case", "PASSWORD123", "password is too common"},
		{"common password, mixed case", "PassWord123", "password is too common"},
		{"common password with surrounding spaces", "  qwertyuiop  ", "password is too common"},
		{"keyboard walk", "1q2w3e4r5t", "password is too common"},
		{"common password is not matched as a substring", "password123-and-more-words", ""},
		{"long passphrase", "correct horse battery staple", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePassword(tc.password)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validatePassword(%q) = %v, want nil", tc.password, err)
				}
				return
			}
			if err == nil || !domainauth.IsCode(err, domainauth.ErrorCodeValidation) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validatePassword(%q) = %v, want a validation error containing %q", tc.password, err, tc.wantErr)
			}
		})
	}
}

func TestCommonPasswordListIsWellFormed(t *testing.T) {
	t.Parallel()

	if len(commonPasswords) < 200 {
		t.Fatalf("blocklist has %d entries, want at least 200", len(commonPasswords))
	}
	for entry := range commonPasswords {
		if entry != strings.ToLower(strings.TrimSpace(entry)) || entry == "" || strings.HasPrefix(entry, "#") {
			t.Errorf("malformed entry %q", entry)
		}
	}
}

func TestBlocklistAppliesWhereAPasswordIsSet(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	service := NewService(store)
	ctx := context.Background()

	if _, err := service.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "root", Password: "password123"}); err == nil || !strings.Contains(err.Error(), "too common") {
		t.Fatalf("bootstrap with a common password = %v, want refusal", err)
	}
	boot, err := service.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "root", Password: "tq7#Lm2z-root"})
	if err != nil {
		t.Fatalf("bootstrap with a good password: %v", err)
	}
	actor := domainauth.Principal{UserID: boot.User.ID, Username: "root", IsAdmin: true}

	if _, err := service.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: "eve", Password: "Welcome123", Enabled: true}); err == nil || !strings.Contains(err.Error(), "too common") {
		t.Fatalf("create user with a common password = %v, want refusal", err)
	}
	user, err := service.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: "eve", Password: "Zk4!vNp9wq", Enabled: true})
	if err != nil {
		t.Fatalf("create user with a good password: %v", err)
	}
	if err := service.ResetAdminUserPassword(ctx, actor, ports.AdminResetPasswordInput{UserID: user.ID, NewPassword: "letmein123"}); err == nil || !strings.Contains(err.Error(), "too common") {
		t.Fatalf("reset to a common password = %v, want refusal", err)
	}
	if err := service.ResetAdminUserPassword(ctx, actor, ports.AdminResetPasswordInput{UserID: user.ID, NewPassword: "Mv8$trL2qa"}); err != nil {
		t.Fatalf("reset to a good password: %v", err)
	}
}

func TestExistingCommonPasswordsKeepWorking(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	user := domainauth.User{ID: "u1", Username: "legacy", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[user.ID] = user
	store.usersByUsername[user.Username] = user

	if _, err := NewService(store).LoginWithPassword(context.Background(), "legacy", "password123", nil); err != nil {
		t.Fatalf("an existing password on the blocklist must still log in: %v", err)
	}
}

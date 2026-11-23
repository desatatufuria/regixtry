package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

// Two keys whose short hashes collide must not be reset by a hash label: the
// request is refused (422) and neither counter is touched.
func TestResetByHashRefusesACollisionAndClearsNothing(t *testing.T) {
	previous := accountHash
	accountHash = func(string) string { return "deadbeef" }
	defer func() { accountHash = previous }()

	service := NewService(nil)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	service.ConfigureLogin(LoginPolicy{Backoff: &BackoffPolicy{Mode: BackoffEnforce, Threshold: 2, Base: time.Minute, Now: func() time.Time { return now }}})
	for _, key := range []string{"aaa", "bbb"} {
		service.backoff.fail(key)
		service.backoff.fail(key)
	}
	_, err := service.ResetAdminLoginBackoff(context.Background(), domainauth.Principal{IsAdmin: true}, "hash:deadbeef")
	var authErr *domainauth.Error
	if !errors.As(err, &authErr) || authErr.Code != domainauth.ErrorCodeValidation {
		t.Fatalf("error = %v, want a validation error", err)
	}
	if got := len(service.backoff.waiting()); got != 2 {
		t.Fatalf("waiting = %d, want both counters untouched", got)
	}
}

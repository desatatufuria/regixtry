package auth

import (
	"context"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

func TestListAdminExpiringTokensRequiresAdminAndUsesServiceClock(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	service := NewService(store)
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	_, err := service.ListAdminExpiringTokens(context.Background(), domainauth.Principal{UserID: "u", IsAdmin: false}, time.Hour, 10)
	if err == nil || !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("non-admin error = %v, want forbidden", err)
	}

	_ = store.UpsertUser(context.Background(), domainauth.User{ID: "u1", Username: "alice", Enabled: true})
	_ = store.CreateToken(context.Background(), domainauth.Token{ID: "a", UserID: "u1", Kind: domainauth.TokenKindAdminCredential, Accessor: "acc-a", SecretHash: "h-a", CreatedAt: now, ExpiresAt: now.Add(2 * time.Hour)})
	_ = store.CreateToken(context.Background(), domainauth.Token{ID: "b", UserID: "u1", Kind: domainauth.TokenKindAdminCredential, Accessor: "acc-b", SecretHash: "h-b", CreatedAt: now, ExpiresAt: now.Add(48 * time.Hour)})

	got, err := service.ListAdminExpiringTokens(context.Background(), domainauth.Principal{IsAdmin: true}, 24*time.Hour, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" || got[0].Username != "alice" || got[0].Accessor != "acc-a" {
		t.Fatalf("got = %#v, want only token a", got)
	}
}

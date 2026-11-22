package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

type expiringFixture struct {
	store *Store
	now   time.Time
}

func (f expiringFixture) user(t *testing.T, id string, robot, enabled bool) {
	t.Helper()
	u := domainauth.User{ID: id, Username: "name-" + id, PasswordHash: "ph-" + id, IsRobot: robot, Enabled: enabled, CreatedAt: f.now, UpdatedAt: f.now}
	if err := f.store.UpsertUser(context.Background(), u); err != nil {
		t.Fatalf("UpsertUser(%s) error = %v", id, err)
	}
}

func (f expiringFixture) token(t *testing.T, id, userID string, expires time.Time, revoked bool) {
	t.Helper()
	tok := domainauth.Token{ID: id, UserID: userID, Kind: domainauth.TokenKindAdminCredential, Name: "n-" + id, Accessor: "acc-" + id, SecretHash: "SECRET-HASH-" + id, CreatedAt: f.now.Add(-time.Hour), ExpiresAt: expires}
	if revoked {
		r := f.now
		tok.RevokedAt = &r
	}
	if err := f.store.CreateToken(context.Background(), tok); err != nil {
		t.Fatalf("CreateToken(%s) error = %v", id, err)
	}
}

func ids(tokens []domainauth.ExpiringToken) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		out = append(out, tok.ID)
	}
	return out
}

func TestListExpiringTokens(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	now := time.Date(2026, 3, 1, 10, 0, 0, 123456789, time.UTC)
	f := expiringFixture{store: store, now: now}
	day := 24 * time.Hour

	f.user(t, "human", false, true)
	f.user(t, "bot", true, true)
	f.user(t, "off", false, false)
	f.token(t, "t-1h", "human", now.Add(time.Hour), false)
	f.token(t, "t-13d", "bot", now.Add(13*day), false)
	f.token(t, "t-14d", "human", now.Add(14*day), false) // boundary, inclusive
	f.token(t, "t-14d+1ns", "human", now.Add(14*day+1), false)
	f.token(t, "t-15d", "human", now.Add(15*day), false)
	f.token(t, "t-400d", "human", now.Add(400*day), false)
	f.token(t, "t-expired", "human", now.Add(-time.Second), false)
	f.token(t, "t-now", "human", now, false) // not yet expired means strictly after now
	f.token(t, "t-revoked", "human", now.Add(2*time.Hour), true)
	f.token(t, "t-disabled", "off", now.Add(3*time.Hour), false)
	// whole-second expiry (fraction trimmed in storage) just after now
	f.token(t, "t-whole", "human", time.Date(2026, 3, 1, 10, 0, 1, 0, time.UTC), false)

	until := now.Add(14 * day)
	got, err := store.ListExpiringTokens(context.Background(), now, until, 50)
	if err != nil {
		t.Fatalf("ListExpiringTokens() error = %v", err)
	}
	want := []string{"t-whole", "t-1h", "t-13d", "t-14d"}
	if g := ids(got); strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v (ordered by expires_at)", g, want)
	}

	byID := map[string]domainauth.ExpiringToken{}
	for _, tok := range got {
		byID[tok.ID] = tok
	}
	if b := byID["t-13d"]; !b.IsRobot || b.Username != "name-bot" || b.UserID != "bot" || b.Accessor != "acc-t-13d" || b.Name != "n-t-13d" || !b.ExpiresAt.Equal(now.Add(13*day)) || !b.CreatedAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("robot row = %#v", b)
	}
	if b := byID["t-1h"]; b.IsRobot || b.Username != "name-human" {
		t.Fatalf("human row = %#v", b)
	}

	raw, _ := json.Marshal(got)
	if strings.Contains(strings.ToLower(string(raw)), "secret") || strings.Contains(string(raw), "ph-") {
		t.Fatalf("result leaks secret material: %s", raw)
	}

	t.Run("limit", func(t *testing.T) {
		limited, err := store.ListExpiringTokens(context.Background(), now, until, 2)
		if err != nil {
			t.Fatal(err)
		}
		if g := ids(limited); strings.Join(g, ",") != "t-whole,t-1h" {
			t.Fatalf("limit 2 ids = %v", g)
		}
	})
	t.Run("empty is non-nil", func(t *testing.T) {
		none, err := store.ListExpiringTokens(context.Background(), now.Add(1000*day), now.Add(1001*day), 50)
		if err != nil {
			t.Fatal(err)
		}
		if none == nil || len(none) != 0 {
			t.Fatalf("empty = %#v, want empty non-nil slice", none)
		}
		if b, _ := json.Marshal(none); string(b) != "[]" {
			t.Fatalf("empty JSON = %s, want []", b)
		}
	})
	t.Run("access tokens are not listed", func(t *testing.T) {
		acc := domainauth.Token{ID: "t-access", UserID: "human", Kind: domainauth.TokenKindAccess, Accessor: "acc-access", SecretHash: "h-access", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := store.CreateToken(context.Background(), acc); err != nil {
			t.Fatal(err)
		}
		again, err := store.ListExpiringTokens(context.Background(), now, until, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids(again) {
			if id == "t-access" {
				t.Fatal("access token listed")
			}
		}
	})
}

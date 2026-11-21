package auth

import (
	"context"
	"strings"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

func TestListAdminAllRepositoryGrants(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	if _, err := f.service.ListAdminAllRepositoryGrants(ctx, domainauth.Principal{UserID: "u-bob"}, 10, ""); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("non-admin error = %v, want forbidden", err)
	}

	empty, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, 10, "")
	if err != nil || empty.Grants == nil || len(empty.Grants) != 0 || empty.Next != "" {
		t.Fatalf("empty = %#v, %v; want a non-nil empty slice and no next", empty, err)
	}

	bot := domainauth.User{ID: "r-bot", Username: "ci-bot", PasswordHash: "h", IsRobot: true, Enabled: true, CreatedAt: f.now, UpdatedAt: f.now}
	_ = f.store.UpsertUser(ctx, bot)
	for _, g := range []struct {
		user domainauth.User
		repo string
		role domainauth.RepoRole
	}{
		{f.alice, "team/web", domainauth.RepoRoleWriter},
		{bot, "team/web", domainauth.RepoRoleReader},
		{f.alice, "alpha/api", domainauth.RepoRoleAdmin},
	} {
		if err := f.store.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: g.user.ID, Repository: regixtrydomain.MustParseRepositoryRef(g.repo), Role: g.role, CreatedAt: f.now, UpdatedAt: f.now}); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, 2, "")
	if err != nil || len(page1.Grants) != 2 || page1.Next == "" {
		t.Fatalf("page1 = %#v, %v; want 2 grants and a next cursor", page1, err)
	}
	first := page1.Grants[0]
	if first.Repository != "alpha/api" || first.Username != "alice" || first.UserID != "u-alice" || first.IsRobot || first.Role != domainauth.RepoRoleAdmin || !first.CreatedAt.Equal(f.now) {
		t.Fatalf("first = %#v", first)
	}
	if page1.Grants[1].Username != "alice" || page1.Grants[1].Repository != "team/web" {
		t.Fatalf("second = %#v", page1.Grants[1])
	}

	page2, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, 2, page1.Next)
	if err != nil || len(page2.Grants) != 1 || page2.Next != "" {
		t.Fatalf("page2 = %#v, %v; want the last grant and no next", page2, err)
	}
	if got := page2.Grants[0]; got.Username != "ci-bot" || !got.IsRobot || got.UserID != "r-bot" {
		t.Fatalf("page2 grant = %#v", got)
	}

	exact, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, 3, "")
	if err != nil || len(exact.Grants) != 3 || exact.Next != "" {
		t.Fatalf("exact fit = %#v, %v; want 3 grants and no next", exact, err)
	}
}

func TestListAdminAllRepositoryGrantsRejectsBadInput(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	valid := domainauth.GrantCursor{Repository: "a/b", Username: "x", UserID: "i"}.Encode()
	for name, after := range map[string]string{
		"garbage":        "!!!not-a-cursor",
		"base64 of junk": "anVuaw",
		"padded":         valid + "=",
		"truncated":      valid[:len(valid)-3],
		"huge":           strings.Repeat("A", 5000),
		"json array":     "W10",
		"unknown field":  "eyJyIjoiYSIsInUiOiJiIiwiaSI6ImMiLCJ4IjoxfQ",
		"empty fields":   "eyJyIjoiIiwidSI6IiIsImkiOiIifQ",
		"trailing data":  "eyJyIjoiYSIsInUiOiJiIiwiaSI6ImMifXt9",
	} {
		_, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, 10, after)
		if !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
			t.Errorf("%s: error = %v, want a validation error", name, err)
		}
	}
	for _, limit := range []int{0, -1, 201} {
		if _, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, limit, ""); !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
			t.Errorf("limit %d: error = %v, want a validation error", limit, err)
		}
	}
	if _, err := f.service.ListAdminAllRepositoryGrants(ctx, f.admin, 10, valid); err != nil {
		t.Errorf("valid cursor error = %v", err)
	}
}

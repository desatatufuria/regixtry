package auth

import (
	"context"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

// TestServiceUserCentricGrantMethodsCharacterizeAdminOnlyBehavior pins
// design.md's Testing Strategy: PutRepoGrant/DeleteRepoGrant/ListRepoGrants
// (service.go:509,543,405) had zero coverage before this change. This
// approval test captures today's behavior — reject a non-admin actor,
// succeed for an admin actor — as a baseline BEFORE task 2.4
// (requireAdminOrRepoAdmin) or any later delegation task touches this file
// (tasks.md Mandatory Ordering Constraint).
func TestServiceUserCentricGrantMethodsCharacterizeAdminOnlyBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	repo := regixtrydomain.MustParseRepositoryRef("team/app")

	newStoreWithTargetUser := func() (*memoryAuthStore, domainauth.User) {
		store := newMemoryAuthStore()
		target := domainauth.User{ID: "user-1", Username: "alice", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
		store.usersByID[target.ID] = target
		store.usersByUsername[target.Username] = target
		return store, target
	}

	adminActor := domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}
	nonAdminActor := domainauth.Principal{UserID: "user-2", Username: "bob", IsAdmin: false}

	t.Run("PutRepoGrant rejects a non-admin actor", func(t *testing.T) {
		t.Parallel()
		store, target := newStoreWithTargetUser()
		service := NewService(store)
		service.now = func() time.Time { return now }

		_, err := service.PutRepoGrant(context.Background(), nonAdminActor, target.ID, repo.String(), domainauth.RepoRoleWriter)
		if !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("PutRepoGrant(non-admin) error = %v, want forbidden", err)
		}
	})

	t.Run("PutRepoGrant succeeds for an admin actor", func(t *testing.T) {
		t.Parallel()
		store, target := newStoreWithTargetUser()
		service := NewService(store)
		service.now = func() time.Time { return now }

		grant, err := service.PutRepoGrant(context.Background(), adminActor, target.ID, repo.String(), domainauth.RepoRoleWriter)
		if err != nil {
			t.Fatalf("PutRepoGrant(admin) error = %v", err)
		}
		if grant.Repository.String() != repo.String() || grant.Role != domainauth.RepoRoleWriter {
			t.Fatalf("PutRepoGrant(admin) grant = %#v, want repository=%q role=%q", grant, repo.String(), domainauth.RepoRoleWriter)
		}
	})

	t.Run("DeleteRepoGrant rejects a non-admin actor", func(t *testing.T) {
		t.Parallel()
		store, target := newStoreWithTargetUser()
		service := NewService(store)
		service.now = func() time.Time { return now }

		err := service.DeleteRepoGrant(context.Background(), nonAdminActor, target.ID, repo.String())
		if !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("DeleteRepoGrant(non-admin) error = %v, want forbidden", err)
		}
	})

	t.Run("DeleteRepoGrant succeeds for an admin actor", func(t *testing.T) {
		t.Parallel()
		store, target := newStoreWithTargetUser()
		service := NewService(store)
		service.now = func() time.Time { return now }

		err := service.DeleteRepoGrant(context.Background(), adminActor, target.ID, repo.String())
		if err != nil {
			t.Fatalf("DeleteRepoGrant(admin) error = %v", err)
		}
	})

	t.Run("ListRepoGrants rejects a non-admin actor", func(t *testing.T) {
		t.Parallel()
		store, target := newStoreWithTargetUser()
		service := NewService(store)
		service.now = func() time.Time { return now }

		_, err := service.ListRepoGrants(context.Background(), nonAdminActor, target.ID)
		if !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("ListRepoGrants(non-admin) error = %v, want forbidden", err)
		}
	})

	t.Run("ListRepoGrants succeeds for an admin actor", func(t *testing.T) {
		t.Parallel()
		store, target := newStoreWithTargetUser()
		store.grants[target.ID] = []domainauth.RepoGrant{{UserID: target.ID, Repository: repo, Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}}
		service := NewService(store)
		service.now = func() time.Time { return now }

		grants, err := service.ListRepoGrants(context.Background(), adminActor, target.ID)
		if err != nil {
			t.Fatalf("ListRepoGrants(admin) error = %v", err)
		}
		if len(grants) != 1 || grants[0].Repository.String() != repo.String() {
			t.Fatalf("ListRepoGrants(admin) = %#v, want single grant for %q", grants, repo.String())
		}
	})
}

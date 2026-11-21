package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// failingTokenStore makes token persistence fail after the robot user and its
// grant were already written, to cover the store-error rollback path.
type failingTokenStore struct {
	*memoryAuthStore
	err error
}

func (s *failingTokenStore) CreateToken(context.Context, domainauth.Token) error { return s.err }

func assertNothingLeft(t *testing.T, store *memoryAuthStore) {
	t.Helper()
	robots, _ := store.ListRobots(context.Background())
	if len(robots) != 0 || len(store.usersByID) != 0 || len(store.grants) != 0 || len(store.tokensByHash) != 0 {
		t.Fatalf("leftovers: robots=%d users=%d grants=%d tokens=%d, want all 0", len(robots), len(store.usersByID), len(store.grants), len(store.tokensByHash))
	}
}

func TestCreateRobotLeavesNothingBehindWhenItFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	actor := domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}

	t.Run("ttl above the ceiling", func(t *testing.T) {
		t.Parallel()
		store := newMemoryAuthStore()
		service := NewService(store)
		service.now = func() time.Time { return now }

		_, err := service.CreateRobot(context.Background(), actor, ports.CreateRobotInput{
			Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, TTL: domainauth.MaxAdminTokenTTL + time.Second,
		})
		if !domainauth.IsCode(err, domainauth.ErrorCodeValidation) || err.Error() != "VALIDATION_FAILED: admin token ttl must be 2 years or shorter" {
			t.Fatalf("error = %v, want the ttl validation error", err)
		}
		assertNothingLeft(t, store)
	})

	t.Run("invalid role", func(t *testing.T) {
		t.Parallel()
		store := newMemoryAuthStore()
		service := NewService(store)

		_, err := service.CreateRobot(context.Background(), actor, ports.CreateRobotInput{
			Name: "ci", Repository: "team/app", Role: domainauth.RepoRole("nope"),
		})
		if !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
			t.Fatalf("error = %v, want validation", err)
		}
		assertNothingLeft(t, store)
	})

	t.Run("invalid repository", func(t *testing.T) {
		t.Parallel()
		store := newMemoryAuthStore()
		service := NewService(store)

		if _, err := service.CreateRobot(context.Background(), actor, ports.CreateRobotInput{
			Name: "ci", Repository: "Not A Repo!!", Role: domainauth.RepoRoleWriter,
		}); err == nil {
			t.Fatal("error = nil, want invalid repository")
		}
		assertNothingLeft(t, store)
	})

	t.Run("token store error rolls the robot back", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		mem := newMemoryAuthStore()
		service := NewService(&failingTokenStore{memoryAuthStore: mem, err: boom})
		service.now = func() time.Time { return now }

		_, err := service.CreateRobot(context.Background(), actor, ports.CreateRobotInput{
			Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleWriter,
		})
		if !errors.Is(err, boom) {
			t.Fatalf("error = %v, want boom", err)
		}
		assertNothingLeft(t, mem)
	})
}

func TestCreateRobotSuccessCreatesOneRobotAndOneToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	actor := domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}

	for name, tc := range map[string]struct {
		ttl  time.Duration
		want time.Duration
	}{
		"default ttl":  {0, domainauth.DefaultAdminTokenTTL},
		"explicit ttl": {time.Hour, time.Hour},
		"max ttl":      {domainauth.MaxAdminTokenTTL, domainauth.MaxAdminTokenTTL},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newMemoryAuthStore()
			service := NewService(store)
			service.now = func() time.Time { return now }

			created, err := service.CreateRobot(context.Background(), actor, ports.CreateRobotInput{
				Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, TTL: tc.ttl,
			})
			if err != nil {
				t.Fatalf("CreateRobot() error = %v", err)
			}
			robots, _ := store.ListRobots(context.Background())
			if len(robots) != 1 || len(store.tokensByHash) != 1 {
				t.Fatalf("robots=%d tokens=%d, want 1 and 1", len(robots), len(store.tokensByHash))
			}
			if want := now.Add(tc.want); !created.ExpiresAt.Equal(want) {
				t.Fatalf("ExpiresAt = %v, want %v", created.ExpiresAt, want)
			}
		})
	}
}

package auth

import (
	"context"
	"testing"
	"time"

	domainauth "registry/internal/domain/auth"
	registrydomain "registry/internal/domain/registry"
)

func TestServiceGrantsOnlyAllowedRequestedRepositoryScopes(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	user := domainauth.User{ID: "user-1", Username: "alice", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[user.ID] = user
	store.usersByUsername[user.Username] = user
	store.grants[user.ID] = []domainauth.RepoGrant{{UserID: user.ID, Repository: registrydomain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}}

	service := NewService(store)
	service.now = func() time.Time { return now }

	requestedScopes, err := domainauth.ParseScopes([]string{"repository:team/app:pull,push"})
	if err != nil {
		t.Fatalf("ParseScopes() error = %v", err)
	}

	result, err := service.LoginWithPassword(context.Background(), "alice", "password123", requestedScopes)
	if err != nil {
		t.Fatalf("LoginWithPassword() error = %v", err)
	}
	if result.Scope != "repository:team/app:pull" {
		t.Fatalf("result.Scope = %q, want repository:team/app:pull", result.Scope)
	}

	principal, err := service.VerifyAccessToken(context.Background(), result.BearerToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	if !principal.HasReadAccess("team/app") {
		t.Fatal("expected granted token to allow pull")
	}
	if principal.HasWriteAccess("team/app") {
		t.Fatal("expected granted token to reject push")
	}
}

func TestServiceRejectsRepositoryAccessWhenRequestedScopeHasNoAllowedActions(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	user := domainauth.User{ID: "user-1", Username: "alice", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[user.ID] = user
	store.usersByUsername[user.Username] = user

	service := NewService(store)
	service.now = func() time.Time { return now }

	requestedScopes, err := domainauth.ParseScopes([]string{"repository:team/app:pull,push"})
	if err != nil {
		t.Fatalf("ParseScopes() error = %v", err)
	}

	result, err := service.LoginWithPassword(context.Background(), "alice", "password123", requestedScopes)
	if err != nil {
		t.Fatalf("LoginWithPassword() error = %v", err)
	}
	if result.Scope != "" {
		t.Fatalf("result.Scope = %q, want empty scope", result.Scope)
	}

	principal, err := service.VerifyAccessToken(context.Background(), result.BearerToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	if principal.HasReadAccess("team/app") || principal.HasWriteAccess("team/app") {
		t.Fatal("expected token with no granted repository scope to reject repository access")
	}
}

type memoryAuthStore struct {
	usersByID       map[string]domainauth.User
	usersByUsername map[string]domainauth.User
	grants          map[string][]domainauth.RepoGrant
	tokensByHash    map[string]domainauth.Token
	tokensByUser    map[string][]domainauth.Token
}

func newMemoryAuthStore() *memoryAuthStore {
	return &memoryAuthStore{
		usersByID:       map[string]domainauth.User{},
		usersByUsername: map[string]domainauth.User{},
		grants:          map[string][]domainauth.RepoGrant{},
		tokensByHash:    map[string]domainauth.Token{},
		tokensByUser:    map[string][]domainauth.Token{},
	}
}

func (s *memoryAuthStore) Close() error { return nil }
func (s *memoryAuthStore) HasActiveGlobalAdmin(context.Context) (bool, error) {
	for _, user := range s.usersByID {
		if user.IsAdmin && user.Enabled {
			return true, nil
		}
	}
	return false, nil
}
func (s *memoryAuthStore) ListUsers(context.Context) ([]domainauth.User, error) { return nil, nil }
func (s *memoryAuthStore) GetUserByUsername(_ context.Context, username string) (domainauth.User, error) {
	user, ok := s.usersByUsername[username]
	if !ok {
		return domainauth.User{}, domainauth.NewNotFoundError("user", username)
	}
	return user, nil
}
func (s *memoryAuthStore) GetUserByID(_ context.Context, userID string) (domainauth.User, error) {
	user, ok := s.usersByID[userID]
	if !ok {
		return domainauth.User{}, domainauth.NewNotFoundError("user", userID)
	}
	return user, nil
}
func (s *memoryAuthStore) UpsertUser(context.Context, domainauth.User) error { return nil }
func (s *memoryAuthStore) DeleteUser(context.Context, string) error          { return nil }
func (s *memoryAuthStore) ListRepoGrants(_ context.Context, userID string) ([]domainauth.RepoGrant, error) {
	return append([]domainauth.RepoGrant(nil), s.grants[userID]...), nil
}
func (s *memoryAuthStore) PutRepoGrant(context.Context, domainauth.RepoGrant) error { return nil }
func (s *memoryAuthStore) DeleteRepoGrant(context.Context, string, registrydomain.RepositoryRef) error {
	return nil
}
func (s *memoryAuthStore) CreateToken(_ context.Context, token domainauth.Token) error {
	s.tokensByHash[token.SecretHash] = token
	s.tokensByUser[token.UserID] = append(s.tokensByUser[token.UserID], token)
	return nil
}
func (s *memoryAuthStore) GetTokenBySecretHash(_ context.Context, kind domainauth.TokenKind, secretHash string) (domainauth.Token, error) {
	token, ok := s.tokensByHash[secretHash]
	if !ok || token.Kind != kind {
		return domainauth.Token{}, domainauth.NewNotFoundError("token", secretHash)
	}
	return token, nil
}
func (s *memoryAuthStore) ListTokensByUser(_ context.Context, userID string, kind domainauth.TokenKind) ([]domainauth.Token, error) {
	tokens := make([]domainauth.Token, 0)
	for _, token := range s.tokensByUser[userID] {
		if token.Kind == kind {
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}
func (s *memoryAuthStore) RevokeTokenByAccessor(context.Context, string, time.Time) error { return nil }

func mustHashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	return hash
}

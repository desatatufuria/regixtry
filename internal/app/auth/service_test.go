package auth

import (
	"context"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func TestServiceGrantsOnlyAllowedRequestedRepositoryScopes(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	user := domainauth.User{ID: "user-1", Username: "alice", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[user.ID] = user
	store.usersByUsername[user.Username] = user
	store.grants[user.ID] = []domainauth.RepoGrant{{UserID: user.ID, Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}}

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

func TestServiceDisableUserRejectsLastActiveAdmin(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	admin := domainauth.User{ID: "admin-1", Username: "admin", PasswordHash: mustHashPassword(t, "password123"), IsAdmin: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[admin.ID] = admin
	store.usersByUsername[admin.Username] = admin

	service := NewService(store)
	service.now = func() time.Time { return now }

	_, err := service.DisableAdminUser(context.Background(), domainauth.Principal{UserID: admin.ID, Username: admin.Username, IsAdmin: true}, admin.ID)
	if !domainauth.IsCode(err, domainauth.ErrorCodeConflict) {
		t.Fatalf("DisableAdminUser() error = %v, want conflict", err)
	}
}

func TestServiceListAdminUserRepoGrantsRejectsMissingUser(t *testing.T) {
	t.Parallel()

	service := NewService(newMemoryAuthStore())

	_, err := service.ListAdminUserRepoGrants(context.Background(), domainauth.Principal{IsAdmin: true}, "missing-user")
	if !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("ListAdminUserRepoGrants() error = %v, want not found", err)
	}
}

func TestServiceCreateAdminUserTokenRejectsDisabledUserAndExcessiveTTL(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	disabled := domainauth.User{ID: "user-1", Username: "disabled", PasswordHash: mustHashPassword(t, "password123"), Enabled: false, CreatedAt: now, UpdatedAt: now}
	store.usersByID[disabled.ID] = disabled
	store.usersByUsername[disabled.Username] = disabled
	service := NewService(store)
	service.now = func() time.Time { return now }
	actor := domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}

	_, err := service.CreateAdminUserToken(context.Background(), actor, ports.AdminCreateTokenInput{UserID: disabled.ID, Name: "ci"})
	if !domainauth.IsCode(err, domainauth.ErrorCodeDisabledUser) {
		t.Fatalf("CreateAdminUserToken(disabled) error = %v, want disabled user", err)
	}

	enabled := disabled
	enabled.ID = "user-2"
	enabled.Username = "enabled"
	enabled.Enabled = true
	store.usersByID[enabled.ID] = enabled
	store.usersByUsername[enabled.Username] = enabled

	_, err = service.CreateAdminUserToken(context.Background(), actor, ports.AdminCreateTokenInput{UserID: enabled.ID, Name: "ci", TTL: domainauth.DefaultAdminTokenTTL + time.Second})
	if !domainauth.IsCode(err, domainauth.ErrorCodeValidation) {
		t.Fatalf("CreateAdminUserToken(ttl) error = %v, want validation", err)
	}
}

func TestServiceRevokeAdminUserTokenRejectsMismatchedOwner(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	owner := domainauth.User{ID: "user-1", Username: "owner", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	other := domainauth.User{ID: "user-2", Username: "other", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[owner.ID] = owner
	store.usersByUsername[owner.Username] = owner
	store.usersByID[other.ID] = other
	store.usersByUsername[other.Username] = other
	token := domainauth.Token{ID: "token-1", UserID: owner.ID, Kind: domainauth.TokenKindAdminCredential, Accessor: "act_1", SecretHash: "hash-1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	store.tokensByHash[token.SecretHash] = token
	store.tokensByAccessor[token.Accessor] = token
	store.tokensByUser[owner.ID] = []domainauth.Token{token}

	service := NewService(store)
	service.now = func() time.Time { return now }

	err := service.RevokeAdminUserToken(context.Background(), domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}, other.ID, "act_1")
	if !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("RevokeAdminUserToken() error = %v, want not found", err)
	}
}

type memoryAuthStore struct {
	usersByID        map[string]domainauth.User
	usersByUsername  map[string]domainauth.User
	grants           map[string][]domainauth.RepoGrant
	tokensByHash     map[string]domainauth.Token
	tokensByAccessor map[string]domainauth.Token
	tokensByUser     map[string][]domainauth.Token
}

func newMemoryAuthStore() *memoryAuthStore {
	return &memoryAuthStore{
		usersByID:        map[string]domainauth.User{},
		usersByUsername:  map[string]domainauth.User{},
		grants:           map[string][]domainauth.RepoGrant{},
		tokensByHash:     map[string]domainauth.Token{},
		tokensByAccessor: map[string]domainauth.Token{},
		tokensByUser:     map[string][]domainauth.Token{},
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
func (s *memoryAuthStore) ListUsers(context.Context) ([]domainauth.User, error) {
	users := make([]domainauth.User, 0, len(s.usersByID))
	for _, user := range s.usersByID {
		users = append(users, user)
	}
	return users, nil
}
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
func (s *memoryAuthStore) UpsertUser(_ context.Context, user domainauth.User) error {
	s.usersByID[user.ID] = user
	s.usersByUsername[user.Username] = user
	return nil
}
func (s *memoryAuthStore) DeleteUser(context.Context, string) error { return nil }
func (s *memoryAuthStore) ListRepoGrants(_ context.Context, userID string) ([]domainauth.RepoGrant, error) {
	return append([]domainauth.RepoGrant(nil), s.grants[userID]...), nil
}
func (s *memoryAuthStore) PutRepoGrant(context.Context, domainauth.RepoGrant) error { return nil }
func (s *memoryAuthStore) DeleteRepoGrant(context.Context, string, regixtrydomain.RepositoryRef) error {
	return nil
}
func (s *memoryAuthStore) CreateToken(_ context.Context, token domainauth.Token) error {
	s.tokensByHash[token.SecretHash] = token
	s.tokensByAccessor[token.Accessor] = token
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
func (s *memoryAuthStore) GetTokenByAccessor(_ context.Context, kind domainauth.TokenKind, accessor string) (domainauth.Token, error) {
	token, ok := s.tokensByAccessor[accessor]
	if !ok || token.Kind != kind {
		return domainauth.Token{}, domainauth.NewNotFoundError("token", accessor)
	}
	return token, nil
}
func (s *memoryAuthStore) RevokeTokenByAccessor(_ context.Context, accessor string, revokedAt time.Time) error {
	token, ok := s.tokensByAccessor[accessor]
	if !ok {
		return domainauth.NewNotFoundError("token", accessor)
	}
	token.RevokedAt = &revokedAt
	s.tokensByAccessor[accessor] = token
	s.tokensByHash[token.SecretHash] = token
	for i := range s.tokensByUser[token.UserID] {
		if s.tokensByUser[token.UserID][i].Accessor == accessor {
			s.tokensByUser[token.UserID][i] = token
		}
	}
	return nil
}

func mustHashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	return hash
}

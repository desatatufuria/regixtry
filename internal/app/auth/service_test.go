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

// TestIntersectRequestedActionsReadOnlyYieldsPullOnly pins design.md
// Decision 5: a read-only actor's requested repository scope is granted
// "pull" and never "push", regardless of any stored grant for that
// repository, while the admin and grant-based paths stay byte-identical.
func TestIntersectRequestedActionsReadOnlyYieldsPullOnly(t *testing.T) {
	t.Parallel()

	pullPush := mustParseTestScope(t, "repository:team/app:pull,push")
	writerGrant := []domainauth.RepoGrant{{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}}

	tests := []struct {
		name       string
		isAdmin    bool
		isReadOnly bool
		grants     []domainauth.RepoGrant
		requested  domainauth.Scope
		want       []string
	}{
		{
			name:       "read-only actor with no grant is granted pull only",
			isReadOnly: true,
			requested:  pullPush,
			want:       []string{"pull"},
		},
		{
			name:       "read-only actor with an existing writer grant is still granted pull only",
			isReadOnly: true,
			grants:     writerGrant,
			requested:  pullPush,
			want:       []string{"pull"},
		},
		{
			name:      "admin actor is byte-identical: granted both pull and push",
			isAdmin:   true,
			requested: pullPush,
			want:      []string{"pull", "push"},
		},
		{
			name:      "grant-based actor is byte-identical: writer grant yields pull and push",
			grants:    writerGrant,
			requested: pullPush,
			want:      []string{"pull", "push"},
		},
		{
			name:      "unflagged actor with no grant is granted nothing",
			requested: pullPush,
			want:      []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := intersectRequestedActions(tt.isAdmin, tt.isReadOnly, tt.grants, tt.requested)
			if !equalStringSlices(got, tt.want) {
				t.Fatalf("intersectRequestedActions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func equalStringSlices(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func mustParseTestScope(t *testing.T, raw string) domainauth.Scope {
	t.Helper()
	scopes, err := domainauth.ParseScopes([]string{raw})
	if err != nil {
		t.Fatalf("ParseScopes(%q) error = %v", raw, err)
	}
	if len(scopes) != 1 {
		t.Fatalf("ParseScopes(%q) = %d scopes, want 1", raw, len(scopes))
	}
	return scopes[0]
}

// TestServiceCreateAdminUserPersistsReadOnlyFlagAndListReflectsIt pins
// operator-user-administration's "Creating a user with the read-only flag
// persists it" scenario: AdminCreateUserInput.IsReadOnly must flow through
// CreateAdminUser into the stored User and back out through
// ListAdminUsers/AdminUser.
func TestServiceCreateAdminUserPersistsReadOnlyFlagAndListReflectsIt(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	service := NewService(store)
	service.now = func() time.Time { return now }
	actor := domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}

	created, err := service.CreateAdminUser(context.Background(), actor, ports.AdminCreateUserInput{
		Username:   "reader",
		Password:   "password123",
		Enabled:    true,
		IsReadOnly: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}
	if !created.IsReadOnly {
		t.Fatalf("CreateAdminUser() result IsReadOnly = false, want true")
	}

	listed, err := service.ListAdminUsers(context.Background(), actor)
	if err != nil {
		t.Fatalf("ListAdminUsers() error = %v", err)
	}
	found := false
	for _, user := range listed {
		if user.ID != created.ID {
			continue
		}
		found = true
		if !user.IsReadOnly {
			t.Fatalf("ListAdminUsers() entry IsReadOnly = false, want true")
		}
	}
	if !found {
		t.Fatalf("ListAdminUsers() = %#v, want to contain created user %q", listed, created.ID)
	}
}

// TestServiceUpdateUserSetsReadOnlyFlag pins operator-user-administration's
// "Updating the read-only flag takes effect" scenario: UpdateUserInput.IsReadOnly
// must flow through UpdateUser into the stored User.
func TestServiceUpdateUserSetsReadOnlyFlag(t *testing.T) {
	t.Parallel()

	store := newMemoryAuthStore()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	user := domainauth.User{ID: "user-1", Username: "alice", PasswordHash: mustHashPassword(t, "password123"), Enabled: true, CreatedAt: now, UpdatedAt: now}
	store.usersByID[user.ID] = user
	store.usersByUsername[user.Username] = user

	service := NewService(store)
	service.now = func() time.Time { return now }
	actor := domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}

	updated, err := service.UpdateUser(context.Background(), actor, ports.UpdateUserInput{
		UserID:     user.ID,
		Username:   user.Username,
		IsReadOnly: true,
	})
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	if !updated.IsReadOnly {
		t.Fatalf("UpdateUser() result IsReadOnly = false, want true")
	}

	reloaded, err := store.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if !reloaded.IsReadOnly {
		t.Fatalf("stored user IsReadOnly = false, want true")
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

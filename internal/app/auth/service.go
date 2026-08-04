package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	domainauth "registry/internal/domain/auth"
	registrydomain "registry/internal/domain/registry"
	"registry/internal/ports"
)

type Service struct {
	store ports.AuthStore
	now   func() time.Time
}

func NewService(store ports.AuthStore) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) EnsureBootstrapAdmin(ctx context.Context) error {
	hasAdmin, err := s.store.HasActiveGlobalAdmin(ctx)
	if err != nil {
		return err
	}
	if !hasAdmin {
		return domainauth.NewBootstrapRequiredError()
	}

	return nil
}

func (s *Service) ListUsers(ctx context.Context, actor domainauth.Principal) ([]domainauth.User, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}

	return s.store.ListUsers(ctx)
}

func (s *Service) BootstrapAdmin(ctx context.Context, input ports.BootstrapAdminInput) (ports.BootstrapAdminResult, error) {
	username := normalizeUsername(input.Username)
	if username == "" {
		return ports.BootstrapAdminResult{}, domainauth.NewValidationError("username is required")
	}
	if err := validatePassword(input.Password); err != nil {
		return ports.BootstrapAdminResult{}, err
	}

	now := s.now()
	user, err := s.store.GetUserByUsername(ctx, username)
	if err == nil {
		if !user.IsAdmin {
			hasOtherAdmin, checkErr := s.store.HasActiveGlobalAdmin(ctx)
			if checkErr != nil {
				return ports.BootstrapAdminResult{}, checkErr
			}
			if hasOtherAdmin {
				return ports.BootstrapAdminResult{}, domainauth.NewConflictError("a global admin already exists")
			}
		}

		updated := user
		updated.Username = username
		updated.IsAdmin = true
		updated.Enabled = true
		updated.UpdatedAt = now

		rotated := false
		if input.RotatePassword {
			hash, hashErr := hashPassword(input.Password)
			if hashErr != nil {
				return ports.BootstrapAdminResult{}, hashErr
			}
			updated.PasswordHash = hash
			rotated = true
		}

		if err := s.store.UpsertUser(ctx, updated); err != nil {
			return ports.BootstrapAdminResult{}, err
		}

		return ports.BootstrapAdminResult{User: updated, PasswordRotated: rotated}, nil
	}
	if !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		return ports.BootstrapAdminResult{}, err
	}

	hasAdmin, err := s.store.HasActiveGlobalAdmin(ctx)
	if err != nil {
		return ports.BootstrapAdminResult{}, err
	}
	if hasAdmin {
		return ports.BootstrapAdminResult{}, domainauth.NewConflictError("a global admin already exists")
	}

	hash, err := hashPassword(input.Password)
	if err != nil {
		return ports.BootstrapAdminResult{}, err
	}

	user = domainauth.User{ID: uuid.NewString(), Username: username, PasswordHash: hash, IsAdmin: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return ports.BootstrapAdminResult{}, err
	}

	return ports.BootstrapAdminResult{User: user, Created: true}, nil
}

func (s *Service) CreateUser(ctx context.Context, actor domainauth.Principal, input ports.CreateUserInput) (domainauth.User, error) {
	if err := requireAdmin(actor); err != nil {
		return domainauth.User{}, err
	}

	username := normalizeUsername(input.Username)
	if username == "" {
		return domainauth.User{}, domainauth.NewValidationError("username is required")
	}
	if err := validatePassword(input.Password); err != nil {
		return domainauth.User{}, err
	}
	if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
		return domainauth.User{}, domainauth.NewConflictError("username already exists")
	} else if !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		return domainauth.User{}, err
	}

	hash, err := hashPassword(input.Password)
	if err != nil {
		return domainauth.User{}, err
	}

	now := s.now()
	user := domainauth.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      input.IsAdmin,
		Enabled:      input.Enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return domainauth.User{}, err
	}

	return user, nil
}

func (s *Service) UpdateUser(ctx context.Context, actor domainauth.Principal, input ports.UpdateUserInput) (domainauth.User, error) {
	if err := requireAdmin(actor); err != nil {
		return domainauth.User{}, err
	}

	user, err := s.store.GetUserByID(ctx, input.UserID)
	if err != nil {
		return domainauth.User{}, err
	}

	nextUsername := normalizeUsername(input.Username)
	if nextUsername == "" {
		nextUsername = user.Username
	}
	if nextUsername != user.Username {
		other, lookupErr := s.store.GetUserByUsername(ctx, nextUsername)
		if lookupErr == nil && other.ID != user.ID {
			return domainauth.User{}, domainauth.NewConflictError("username already exists")
		}
		if lookupErr != nil && !domainauth.IsCode(lookupErr, domainauth.ErrorCodeNotFound) {
			return domainauth.User{}, lookupErr
		}
	}

	if user.IsAdmin && !input.IsAdmin {
		if err := s.ensureAnotherActiveAdmin(ctx, user.ID, user.Enabled); err != nil {
			return domainauth.User{}, err
		}
	}

	user.Username = nextUsername
	user.IsAdmin = input.IsAdmin
	user.UpdatedAt = s.now()
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return domainauth.User{}, err
	}

	return user, nil
}

func (s *Service) SetUserEnabled(ctx context.Context, actor domainauth.Principal, userID string, enabled bool) (domainauth.User, error) {
	if err := requireAdmin(actor); err != nil {
		return domainauth.User{}, err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return domainauth.User{}, err
	}
	if user.IsAdmin && user.Enabled && !enabled {
		if err := s.ensureAnotherActiveAdmin(ctx, user.ID, true); err != nil {
			return domainauth.User{}, err
		}
	}

	user.Enabled = enabled
	user.UpdatedAt = s.now()
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return domainauth.User{}, err
	}

	return user, nil
}

func (s *Service) DeleteUser(ctx context.Context, actor domainauth.Principal, userID string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.IsAdmin && user.Enabled {
		if err := s.ensureAnotherActiveAdmin(ctx, user.ID, true); err != nil {
			return err
		}
	}

	return s.store.DeleteUser(ctx, userID)
}

func (s *Service) LoginWithPassword(ctx context.Context, username string, password string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	user, err := s.getActiveUserByUsername(ctx, username)
	if err != nil {
		return ports.LoginResult{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return ports.LoginResult{}, domainauth.NewInvalidCredentialsError()
	}

	return s.issueAccessToken(ctx, user, requestedScopes)
}

func (s *Service) LoginWithPreissuedToken(ctx context.Context, username string, token string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	user, err := s.getActiveUserByUsername(ctx, username)
	if err != nil {
		return ports.LoginResult{}, err
	}

	storedToken, err := s.store.GetTokenBySecretHash(ctx, domainauth.TokenKindAdminCredential, hashSecret(token))
	if err != nil {
		if domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
			return ports.LoginResult{}, domainauth.NewInvalidCredentialsError()
		}
		return ports.LoginResult{}, err
	}
	if storedToken.UserID != user.ID {
		return ports.LoginResult{}, domainauth.NewInvalidCredentialsError()
	}
	if err := storedToken.IsActive(s.now()); err != nil {
		return ports.LoginResult{}, err
	}

	return s.issueAccessToken(ctx, user, requestedScopes)
}

func (s *Service) VerifyAccessToken(ctx context.Context, bearerToken string) (domainauth.Principal, error) {
	storedToken, err := s.store.GetTokenBySecretHash(ctx, domainauth.TokenKindAccess, hashSecret(bearerToken))
	if err != nil {
		if domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
			return domainauth.Principal{}, domainauth.NewInvalidCredentialsError()
		}
		return domainauth.Principal{}, err
	}
	if err := storedToken.IsActive(s.now()); err != nil {
		return domainauth.Principal{}, err
	}

	user, err := s.store.GetUserByID(ctx, storedToken.UserID)
	if err != nil {
		return domainauth.Principal{}, err
	}
	if !user.Enabled {
		return domainauth.Principal{}, domainauth.NewDisabledUserError(user.Username)
	}

	principal, err := s.buildPrincipal(ctx, user, storedToken)
	if err != nil {
		return domainauth.Principal{}, domainauth.NewInvalidCredentialsError()
	}

	return principal, nil
}

func (s *Service) CreateAdminToken(ctx context.Context, actor domainauth.Principal, input ports.CreateAdminTokenInput) (ports.CreatedAdminToken, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.CreatedAdminToken{}, err
	}

	user, err := s.store.GetUserByID(ctx, input.UserID)
	if err != nil {
		return ports.CreatedAdminToken{}, err
	}
	if !user.Enabled {
		return ports.CreatedAdminToken{}, domainauth.NewDisabledUserError(user.Username)
	}

	ttl := input.TTL
	if ttl <= 0 {
		ttl = domainauth.DefaultAdminTokenTTL
	}
	if ttl > domainauth.DefaultAdminTokenTTL {
		return ports.CreatedAdminToken{}, domainauth.NewValidationError("admin token ttl must be 30 days or shorter")
	}

	secret, err := randomSecret(32)
	if err != nil {
		return ports.CreatedAdminToken{}, err
	}

	now := s.now()
	token := domainauth.Token{ID: uuid.NewString(), UserID: user.ID, Kind: domainauth.TokenKindAdminCredential, Name: strings.TrimSpace(input.Name), Accessor: shortAccessor("act"), SecretHash: hashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(ttl)}
	if err := s.store.CreateToken(ctx, token); err != nil {
		return ports.CreatedAdminToken{}, err
	}

	return ports.CreatedAdminToken{Token: token, Secret: secret, Plaintext: secret, Accessor: token.Accessor, ExpiresAt: token.ExpiresAt, TargetUser: user}, nil
}

func (s *Service) ListRepoGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.RepoGrant, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}

	return s.store.ListRepoGrants(ctx, userID)
}

func (s *Service) ListAdminTokens(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.Token, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}

	return s.store.ListTokensByUser(ctx, userID, domainauth.TokenKindAdminCredential)
}

func (s *Service) RevokeAdminToken(ctx context.Context, actor domainauth.Principal, accessor string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}

	return s.store.RevokeTokenByAccessor(ctx, strings.TrimSpace(accessor), s.now())
}

func (s *Service) ResetPassword(ctx context.Context, actor domainauth.Principal, userID string, newPassword string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	user.UpdatedAt = s.now()
	return s.store.UpsertUser(ctx, user)
}

func (s *Service) PutRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string, role domainauth.RepoRole) (domainauth.RepoGrant, error) {
	if err := requireAdmin(actor); err != nil {
		return domainauth.RepoGrant{}, err
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return domainauth.RepoGrant{}, err
	}

	repo, err := registrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return domainauth.RepoGrant{}, err
	}
	if err := role.Validate(); err != nil {
		return domainauth.RepoGrant{}, err
	}

	now := s.now()
	grant := domainauth.RepoGrant{UserID: userID, Repository: repo, Role: role, CreatedAt: now, UpdatedAt: now}
	if err := s.store.PutRepoGrant(ctx, grant); err != nil {
		return domainauth.RepoGrant{}, err
	}

	return grant, nil
}

func (s *Service) DeleteRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}

	repo, err := registrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return err
	}

	return s.store.DeleteRepoGrant(ctx, userID, repo)
}

func (s *Service) ensureAnotherActiveAdmin(ctx context.Context, exceptUserID string, includeTarget bool) error {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return err
	}

	for _, user := range users {
		if user.ID == exceptUserID {
			continue
		}
		if user.IsAdmin && user.Enabled {
			return nil
		}
	}
	if includeTarget {
		return domainauth.NewConflictError("at least one active global admin must remain configured")
	}

	return nil
}

func (s *Service) issueAccessToken(ctx context.Context, user domainauth.User, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	secret, err := randomSecret(32)
	if err != nil {
		return ports.LoginResult{}, err
	}

	grantedScopes, err := s.grantedScopes(ctx, user, requestedScopes)
	if err != nil {
		return ports.LoginResult{}, err
	}
	grantedScope := domainauth.NormalizeScopes(grantedScopes)

	now := s.now()
	token := domainauth.Token{ID: uuid.NewString(), UserID: user.ID, Kind: domainauth.TokenKindAccess, Scope: grantedScope, Accessor: shortAccessor("atk"), SecretHash: hashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(domainauth.AccessTokenTTL)}
	if err := s.store.CreateToken(ctx, token); err != nil {
		return ports.LoginResult{}, err
	}

	principal, err := s.buildPrincipal(ctx, user, token)
	if err != nil {
		return ports.LoginResult{}, err
	}

	return ports.LoginResult{Principal: principal, BearerToken: secret, ExpiresAt: token.ExpiresAt, Accessor: token.Accessor, Scope: grantedScope}, nil
}

func (s *Service) getActiveUserByUsername(ctx context.Context, username string) (domainauth.User, error) {
	user, err := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil {
		if domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
			return domainauth.User{}, domainauth.NewInvalidCredentialsError()
		}
		return domainauth.User{}, err
	}
	if !user.Enabled {
		return domainauth.User{}, domainauth.NewDisabledUserError(user.Username)
	}

	return user, nil
}

func (s *Service) buildPrincipal(ctx context.Context, user domainauth.User, token domainauth.Token) (domainauth.Principal, error) {
	grants, err := s.store.ListRepoGrants(ctx, user.ID)
	if err != nil {
		grants = nil
	}
	scopes, err := token.Scopes()
	if err != nil {
		return domainauth.Principal{}, err
	}

	return domainauth.Principal{Subject: token.Accessor, UserID: user.ID, Username: user.Username, IsAdmin: user.IsAdmin, Grants: grants, Scopes: scopes, ExpiresAt: token.ExpiresAt}, nil
}

func (s *Service) grantedScopes(ctx context.Context, user domainauth.User, requestedScopes []domainauth.Scope) ([]domainauth.Scope, error) {
	if len(requestedScopes) == 0 {
		return nil, nil
	}

	grants, err := s.store.ListRepoGrants(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	granted := make([]domainauth.Scope, 0, len(requestedScopes))
	for _, requested := range requestedScopes {
		if requested.IsRegistryCatalog() {
			granted = append(granted, requested)
			continue
		}
		if !requested.IsRepository() {
			continue
		}

		actions := intersectRequestedActions(user.IsAdmin, grants, requested)
		if len(actions) == 0 {
			continue
		}

		granted = append(granted, domainauth.Scope{Type: "repository", Name: requested.Repository().String(), Actions: actions, Canonical: "repository:" + requested.Repository().String() + ":" + strings.Join(actions, ",")})
	}

	return granted, nil
}

func intersectRequestedActions(isAdmin bool, grants []domainauth.RepoGrant, requested domainauth.Scope) []string {
	allowPull := false
	allowPush := false

	if isAdmin {
		allowPull = requested.AllowsPull()
		allowPush = requested.AllowsPush()
	} else {
		for _, grant := range grants {
			if grant.Repository.String() != requested.Repository().String() {
				continue
			}
			if grant.Role.AllowsRead() && requested.AllowsPull() {
				allowPull = true
			}
			if grant.Role.AllowsWrite() && requested.AllowsPush() {
				allowPush = true
			}
			break
		}
	}

	actions := make([]string, 0, 2)
	if allowPull {
		actions = append(actions, "pull")
	}
	if allowPush {
		actions = append(actions, "push")
	}

	return actions
}

func requireAdmin(actor domainauth.Principal) error {
	if !actor.IsAdmin {
		return domainauth.NewForbiddenError("administrator privileges are required")
	}

	return nil
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func validatePassword(password string) error {
	if len(strings.TrimSpace(password)) < 8 {
		return domainauth.NewValidationError("password must be at least 8 characters")
	}

	return nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomSecret(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

func shortAccessor(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, strings.ReplaceAll(uuid.NewString(), "-", ""))
}

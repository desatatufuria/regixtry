package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

type Service struct {
	store ports.AuthStore
	now   func() time.Time
	// rollbackTimeout bounds the best-effort robot rollback.
	rollbackTimeout time.Duration
	// comparePassword is the bcrypt comparison every password login performs;
	// a seam so tests can assert each failure path reaches exactly one.
	comparePassword func(hash, password []byte) error
	// limiter bounds simultaneous bcrypt comparisons (see ConfigureLogin).
	limiter *loginLimiter
	backoff *loginBackoff
	logger  *log.Logger
	// projects answers whether a project exists (see SetProjectLookup); nil
	// until main installs it.
	projects ports.ProjectLookup
}

// robotRollbackTimeout is the default bound on the robot rollback delete.
const robotRollbackTimeout = 5 * time.Second

func NewService(store ports.AuthStore) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }, rollbackTimeout: robotRollbackTimeout, comparePassword: bcrypt.CompareHashAndPassword, limiter: newLoginLimiter(0, 0), backoff: newLoginBackoff(DefaultBackoffPolicy()), logger: log.Default()}
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

func (s *Service) ListAdminUsers(ctx context.Context, actor domainauth.Principal) ([]ports.AdminUser, error) {
	users, err := s.ListUsers(ctx, actor)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminUser, 0, len(users))
	for _, user := range users {
		result = append(result, toAdminUser(user))
	}

	return result, nil
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
		IsReadOnly:   input.IsReadOnly,
		Enabled:      input.Enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return domainauth.User{}, err
	}

	s.recordUserCreated(ctx, actor, user)

	return user, nil
}

func (s *Service) CreateAdminUser(ctx context.Context, actor domainauth.Principal, input ports.AdminCreateUserInput) (ports.AdminUser, error) {
	user, err := s.CreateUser(ctx, actor, ports.CreateUserInput{
		Username:   input.Username,
		Password:   input.Password,
		IsAdmin:    input.IsAdmin,
		IsReadOnly: input.IsReadOnly,
		Enabled:    input.Enabled,
	})
	if err != nil {
		return ports.AdminUser{}, err
	}

	return toAdminUser(user), nil
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

	before := user
	user.Username = nextUsername
	user.IsAdmin = input.IsAdmin
	user.IsReadOnly = input.IsReadOnly
	user.UpdatedAt = s.now()
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return domainauth.User{}, err
	}

	s.recordUserUpdated(ctx, actor, before, user)

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

	changed := user.Enabled != enabled
	user.Enabled = enabled
	user.UpdatedAt = s.now()
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return domainauth.User{}, err
	}

	// Enabling an enabled user (or disabling a disabled one) changes nothing,
	// so it leaves nothing in the history.
	if !changed {
		return user, nil
	}

	action := domainauth.AccessActionUserDisabled
	if enabled {
		action = domainauth.AccessActionUserEnabled
	}
	s.recordUserEvent(ctx, actor, action, user, "")

	return user, nil
}

func (s *Service) EnableAdminUser(ctx context.Context, actor domainauth.Principal, userID string) (ports.AdminUser, error) {
	user, err := s.SetUserEnabled(ctx, actor, userID, true)
	if err != nil {
		return ports.AdminUser{}, err
	}

	return toAdminUser(user), nil
}

func (s *Service) DisableAdminUser(ctx context.Context, actor domainauth.Principal, userID string) (ports.AdminUser, error) {
	user, err := s.SetUserEnabled(ctx, actor, userID, false)
	if err != nil {
		return ports.AdminUser{}, err
	}

	return toAdminUser(user), nil
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

	if err := s.store.DeleteUser(ctx, userID); err != nil {
		return err
	}

	action := domainauth.AccessActionUserDeleted
	if user.IsRobot {
		action = domainauth.AccessActionRobotDeleted
	}
	s.recordUserEvent(ctx, actor, action, user, "")

	return nil
}

// LoginWithPassword authenticates a human user. Every way of failing the
// credential check (unknown username, disabled user, robot, wrong password)
// returns the same invalid-credentials error and pays one bcrypt comparison,
// so an unauthenticated caller can neither read the account state from the
// response nor infer existence from the response time. The real reason travels
// only in the error's wrapped cause, for server-side logs.
func (s *Service) LoginWithPassword(ctx context.Context, username string, password string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	// The backoff key is the normalized username alone, decided before any
	// lookup, so an unknown and an existing account behave identically. A
	// token-shaped secret (64 hex) is never gated and never counted: a wrong
	// one must not let a stranger put someone else's account in backoff.
	key := normalizeUsername(username)
	accounted := s.backoff.enabled() && !ports.IsCredentialTokenShaped(password)
	var wouldBlock time.Duration
	if accounted {
		if wait := s.backoff.check(key); wait > 0 {
			if s.backoff.enforcing() {
				// No log here: the HTTP adapter writes the single "login failed"
				// line for a refusal, with the client IP.
				return ports.LoginResult{}, &ports.LoginThrottledError{RetryAfter: wait, Reason: "throttled: account backoff"}
			}
			wouldBlock = wait
		}
	}

	user, err := s.store.GetUserByUsername(ctx, key)
	found := err == nil
	if err != nil && !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		return ports.LoginResult{}, err
	}
	if wouldBlock > 0 {
		account := "account_hash=" + accountHash(key)
		if found {
			account = "account=" + accountLogName(key)
		}
		s.logger.Printf("login backoff would have blocked %s retry_after=%s mode=log", account, wouldBlock.Round(time.Second))
	}

	// design.md Decision 6: a robot never authenticates with a password. Its
	// RobotPasswordHash sentinel independently blocks bcrypt, but this
	// per-attempt guard is a second, permanent layer that does not depend on
	// the stored hash's shape. The guard stays out of the shared lookup because
	// LoginWithPreissuedToken is the robot's only working credential path.
	hash := dummyPasswordHash()
	if found && !user.IsRobot {
		hash = []byte(user.PasswordHash)
	}
	passwordMatches, err := s.compareLimited(ctx, hash, []byte(password))
	if err != nil {
		return ports.LoginResult{}, err
	}

	var failure error
	switch {
	case !found:
		failure = invalidCredentialsBecause("unknown user")
	case !user.Enabled:
		failure = invalidCredentialsBecause("user is disabled")
	case user.IsRobot:
		failure = invalidCredentialsBecause("robot accounts cannot use a password")
	case !passwordMatches:
		failure = invalidCredentialsBecause("wrong password")
	}
	if failure != nil {
		if accounted {
			s.backoff.fail(key)
		}
		return ports.LoginResult{}, failure
	}
	if accounted {
		s.backoff.success(key)
	}

	return s.issueAccessToken(ctx, user, requestedScopes)
}

// LoginWithPreissuedToken exchanges an admin credential token for an access
// token. The user and token lookups both always run, and every mismatch
// (unknown user, disabled user, unknown token, token of another user) yields
// the same invalid-credentials error as a wrong password.
func (s *Service) LoginWithPreissuedToken(ctx context.Context, username string, token string, requestedScopes []domainauth.Scope) (ports.LoginResult, error) {
	user, userErr := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if userErr != nil && !domainauth.IsCode(userErr, domainauth.ErrorCodeNotFound) {
		return ports.LoginResult{}, userErr
	}
	storedToken, tokenErr := s.store.GetTokenBySecretHash(ctx, domainauth.TokenKindAdminCredential, hashSecret(token))
	if tokenErr != nil && !domainauth.IsCode(tokenErr, domainauth.ErrorCodeNotFound) {
		return ports.LoginResult{}, tokenErr
	}

	switch {
	case userErr != nil:
		return ports.LoginResult{}, invalidCredentialsBecause("unknown user")
	case !user.Enabled:
		return ports.LoginResult{}, invalidCredentialsBecause("user is disabled")
	case tokenErr != nil:
		return ports.LoginResult{}, invalidCredentialsBecause("wrong password or unknown admin credential token")
	case storedToken.UserID != user.ID:
		return ports.LoginResult{}, invalidCredentialsBecause("admin credential token belongs to another user")
	}
	if err := storedToken.IsActive(s.now()); err != nil {
		return ports.LoginResult{}, err
	}

	return s.issueAccessToken(ctx, user, requestedScopes)
}

// invalidCredentialsBecause is the uniform client-facing error with the real
// reason attached as a wrapped cause. The client sees only Message.
func invalidCredentialsBecause(reason string) error {
	return &domainauth.Error{Code: domainauth.ErrorCodeInvalidCredentials, Message: "invalid credentials", Err: errors.New(reason)}
}

// passwordHashCost is the bcrypt cost of every newly written password hash and
// of the dummy hash used to equalize failed logins. A hash stored with another
// cost (for example a legacy one at cost 4 or 12 migrated in) keeps its own
// cost, so a login against it takes proportionally longer or shorter than the
// dummy comparison; the dummy cost deliberately tracks new hashes only and
// never adapts to whatever is stored. Re-hash such users to remove the gap.
const passwordHashCost = bcrypt.DefaultCost

var (
	dummyHashOnce  sync.Once
	dummyHashBytes []byte
)

// dummyPasswordHash returns a bcrypt hash of the same cost as real password
// hashes, compared against when no real hash applies so every failed login
// costs the same.
func dummyPasswordHash() []byte {
	dummyHashOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("regixtry-dummy-password"), passwordHashCost)
		if err != nil {
			panic(err)
		}
		dummyHashBytes = hash
	})

	return dummyHashBytes
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

// resolveAdminTokenTTL applies the default and the ceiling to a requested admin
// token lifetime. It is shared by CreateAdminToken and CreateRobot so the robot
// flow can reject a bad TTL before it writes anything.
func resolveAdminTokenTTL(requested time.Duration) (time.Duration, error) {
	ttl := requested
	if ttl <= 0 {
		ttl = domainauth.DefaultAdminTokenTTL
	}
	if ttl > domainauth.MaxAdminTokenTTL {
		return 0, domainauth.NewValidationError("admin token ttl must be 2 years or shorter")
	}

	return ttl, nil
}

func (s *Service) CreateAdminToken(ctx context.Context, actor domainauth.Principal, input ports.CreateAdminTokenInput) (ports.CreatedAdminToken, error) {
	created, ttl, err := s.issueAdminToken(ctx, actor, input)
	if err != nil {
		return ports.CreatedAdminToken{}, err
	}

	s.recordTokenIssued(ctx, actor, created.TargetUser, created.Token, ttl)

	return created, nil
}

// issueAdminToken issues an admin credential token without recording it in
// the access history, so CreateRobot can record one event for the whole
// operation. It returns the lifetime that was applied.
func (s *Service) issueAdminToken(ctx context.Context, actor domainauth.Principal, input ports.CreateAdminTokenInput) (ports.CreatedAdminToken, time.Duration, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.CreatedAdminToken{}, 0, err
	}

	user, err := s.store.GetUserByID(ctx, input.UserID)
	if err != nil {
		return ports.CreatedAdminToken{}, 0, err
	}
	if !user.Enabled {
		return ports.CreatedAdminToken{}, 0, domainauth.NewDisabledUserError(user.Username)
	}

	ttl, err := resolveAdminTokenTTL(input.TTL)
	if err != nil {
		return ports.CreatedAdminToken{}, 0, err
	}

	secret, err := randomSecret(32)
	if err != nil {
		return ports.CreatedAdminToken{}, 0, err
	}

	now := s.now()
	token := domainauth.Token{ID: uuid.NewString(), UserID: user.ID, Kind: domainauth.TokenKindAdminCredential, Name: strings.TrimSpace(input.Name), Accessor: shortAccessor("act"), SecretHash: hashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(ttl)}
	if err := s.store.CreateToken(ctx, token); err != nil {
		return ports.CreatedAdminToken{}, 0, err
	}

	return ports.CreatedAdminToken{Token: token, Secret: secret, Plaintext: secret, Accessor: token.Accessor, ExpiresAt: token.ExpiresAt, TargetUser: user}, ttl, nil
}

func (s *Service) CreateAdminUserToken(ctx context.Context, actor domainauth.Principal, input ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	created, err := s.CreateAdminToken(ctx, actor, ports.CreateAdminTokenInput{
		UserID: input.UserID,
		Name:   input.Name,
		TTL:    input.TTL,
	})
	if err != nil {
		return ports.AdminCreatedToken{}, err
	}

	return ports.AdminCreatedToken{
		Token:      toAdminToken(created.Token),
		Secret:     created.Secret,
		Accessor:   created.Accessor,
		ExpiresAt:  created.ExpiresAt,
		TargetUser: toAdminUser(created.TargetUser),
	}, nil
}

func (s *Service) ListRepoGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.RepoGrant, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}

	return s.store.ListRepoGrants(ctx, userID)
}

func (s *Service) ListAdminUserRepoGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]ports.AdminRepoGrant, error) {
	grants, err := s.ListRepoGrants(ctx, actor, userID)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminRepoGrant, 0, len(grants))
	for _, grant := range grants {
		result = append(result, toAdminRepoGrant(grant))
	}

	return result, nil
}

func (s *Service) ListAdminTokens(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.Token, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}

	return s.store.ListTokensByUser(ctx, userID, domainauth.TokenKindAdminCredential)
}

func (s *Service) ListAdminUserTokens(ctx context.Context, actor domainauth.Principal, userID string) ([]ports.AdminToken, error) {
	tokens, err := s.ListAdminTokens(ctx, actor, userID)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminToken, 0, len(tokens))
	for _, token := range tokens {
		result = append(result, toAdminToken(token))
	}

	return result, nil
}

// ListAdminExpiringTokens lists admin credentials that expire within the given
// window from the service clock, soonest first. Metadata only.
func (s *Service) ListAdminExpiringTokens(ctx context.Context, actor domainauth.Principal, within time.Duration, limit int) ([]ports.AdminExpiringToken, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}

	now := s.now()
	tokens, err := s.store.ListExpiringTokens(ctx, now, now.Add(within), limit)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminExpiringToken, 0, len(tokens))
	for _, token := range tokens {
		result = append(result, ports.AdminExpiringToken{
			ID:        token.ID,
			Accessor:  token.Accessor,
			Name:      token.Name,
			UserID:    token.UserID,
			Username:  token.Username,
			IsRobot:   token.IsRobot,
			CreatedAt: token.CreatedAt,
			ExpiresAt: token.ExpiresAt,
		})
	}

	return result, nil
}

func (s *Service) RevokeAdminToken(ctx context.Context, actor domainauth.Principal, accessor string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}

	accessor = strings.TrimSpace(accessor)
	// Resolved only to name the subject in the access history; a failed
	// lookup never changes the revocation itself.
	token, tokenErr := s.store.GetTokenByAccessor(ctx, domainauth.TokenKindAdminCredential, accessor)

	if err := s.store.RevokeTokenByAccessor(ctx, accessor, s.now()); err != nil {
		return err
	}

	if tokenErr == nil {
		if subject, err := s.store.GetUserByID(ctx, token.UserID); err == nil {
			s.recordTokenRevoked(ctx, actor, subject, token)
		}
	}

	return nil
}

func (s *Service) RevokeAdminUserToken(ctx context.Context, actor domainauth.Principal, userID string, accessor string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	token, err := s.store.GetTokenByAccessor(ctx, domainauth.TokenKindAdminCredential, accessor)
	if err != nil {
		return err
	}
	if token.UserID != user.ID {
		return domainauth.NewNotFoundError("token", strings.TrimSpace(accessor))
	}

	if err := s.store.RevokeTokenByAccessor(ctx, token.Accessor, s.now()); err != nil {
		return err
	}

	s.recordTokenRevoked(ctx, actor, user, token)

	return nil
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
	// A robot has no password: LoginWithPassword refuses robots and the
	// RobotPasswordHash sentinel must never be replaced by a real hash.
	if user.IsRobot {
		return domainauth.NewValidationError("target user is a robot account and has no password")
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	// The new hash and the revocation of the user's login sessions (access
	// tokens, which a password login minted) commit in one transaction, so a
	// session opened with the old password cannot outlive the reset. Admin
	// credential tokens intentionally survive: they are PAT-like, do not
	// derive from the password and are revoked explicitly by the operator
	// (Tokens screen). Resetting your own password closes your own session.
	user.PasswordHash = hash
	user.UpdatedAt = s.now()
	if err := s.store.ResetUserPassword(ctx, user, user.UpdatedAt); err != nil {
		return err
	}

	// Detail stays empty: neither the password nor anything derived from it
	// belongs in the history.
	s.recordUserEvent(ctx, actor, domainauth.AccessActionUserPasswordReset, user, "")

	return nil
}

func (s *Service) ResetAdminUserPassword(ctx context.Context, actor domainauth.Principal, input ports.AdminResetPasswordInput) error {
	return s.ResetPassword(ctx, actor, input.UserID, input.NewPassword)
}

func (s *Service) PutRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string, role domainauth.RepoRole) (domainauth.RepoGrant, error) {
	if err := requireAdmin(actor); err != nil {
		return domainauth.RepoGrant{}, err
	}
	subject, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return domainauth.RepoGrant{}, err
	}

	repo, err := regixtrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return domainauth.RepoGrant{}, err
	}
	if err := role.Validate(); err != nil {
		return domainauth.RepoGrant{}, err
	}
	if err := s.refuseRepoGrantOnProjectRobot(ctx, subject); err != nil {
		return domainauth.RepoGrant{}, err
	}

	// Read only to word the access history ("Reader to Writer"); a failed
	// read never blocks the grant.
	existing, listErr := s.store.ListRepoGrants(ctx, userID)
	previous, hadPrevious := grantRoleOn(existing, repo.String())

	now := s.now()
	grant := domainauth.RepoGrant{UserID: userID, Repository: repo, Role: role, CreatedAt: now, UpdatedAt: now}
	if err := s.store.PutRepoGrant(ctx, grant); err != nil {
		return domainauth.RepoGrant{}, err
	}

	if listErr == nil {
		s.recordGrantPut(ctx, actor, subject, grant, previous, hadPrevious)
	}

	return grant, nil
}

func (s *Service) PutAdminUserRepoGrant(ctx context.Context, actor domainauth.Principal, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error) {
	grant, err := s.PutRepoGrant(ctx, actor, input.UserID, input.Repository, input.Role)
	if err != nil {
		return ports.AdminRepoGrant{}, err
	}

	return toAdminRepoGrant(grant), nil
}

func (s *Service) DeleteRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	subject, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	repo, err := regixtrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return err
	}

	if err := s.store.DeleteRepoGrant(ctx, userID, repo); err != nil {
		return err
	}

	s.recordGrantDelete(ctx, actor, subject, repo.String())

	return nil
}

func (s *Service) DeleteAdminUserRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string) error {
	return s.DeleteRepoGrant(ctx, actor, userID, repository)
}

// ListRepositoryGrants lists every grant on one repository (design.md
// Decision 3's delegate route table). Authorization is
// requireAdminOrRepoAdmin, so a delegate is structurally scoped to their own
// repository: they can never pass the gate for a repository they do not
// administer, which is what keeps their listing scoped without any extra
// filtering (operator-access-administration's delegate-scoped-listing
// scenario).
func (s *Service) ListRepositoryGrants(ctx context.Context, actor domainauth.Principal, repository string) ([]domainauth.RepoGrant, error) {
	repo, err := regixtrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return nil, err
	}
	if err := requireAdminOrRepoAdmin(actor, repo.String()); err != nil {
		return nil, err
	}

	return s.store.ListRepoGrantsByRepository(ctx, repo)
}

func (s *Service) ListAdminRepositoryGrants(ctx context.Context, actor domainauth.Principal, repository string) ([]ports.AdminRepositoryGrant, error) {
	grants, err := s.ListRepositoryGrants(ctx, actor, repository)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminRepositoryGrant, 0, len(grants))
	for _, grant := range grants {
		user, err := s.store.GetUserByID(ctx, grant.UserID)
		if err != nil {
			return nil, err
		}
		result = append(result, ports.AdminRepositoryGrant{Username: user.Username, Role: grant.Role, CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt})
	}

	return result, nil
}

// PutRepositoryGrant is the delegate-eligible sibling of PutRepoGrant
// (design.md Decision 3): grants are addressed by username, not user ID, so
// a delegate never needs to read the user directory to name someone. For a
// non-global-admin actor (necessarily a repo-admin delegate, enforced by
// requireAdminOrRepoAdmin), design.md Decision 4's escalation bounds apply:
// the requested role must be repo-reader or repo-writer (repo-admin is
// rejected outright, which also covers self-assignment — no identity
// comparison is needed), and the target's existing grant on this repository
// must not already be repo-admin (a delegate cannot demote or replace a
// peer repository administrator).
func (s *Service) PutRepositoryGrant(ctx context.Context, actor domainauth.Principal, repository string, username string, role domainauth.RepoRole) (domainauth.RepoGrant, error) {
	repo, err := regixtrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return domainauth.RepoGrant{}, err
	}
	if err := requireAdminOrRepoAdmin(actor, repo.String()); err != nil {
		return domainauth.RepoGrant{}, err
	}
	if err := role.Validate(); err != nil {
		return domainauth.RepoGrant{}, err
	}
	if !actor.IsAdmin && role == domainauth.RepoRoleAdmin {
		return domainauth.RepoGrant{}, domainauth.NewForbiddenError("repository administrators cannot grant repo-admin")
	}

	target, err := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil {
		return domainauth.RepoGrant{}, err
	}
	if err := s.refuseRepoGrantOnProjectRobot(ctx, target); err != nil {
		return domainauth.RepoGrant{}, err
	}

	// The existing grants bound a delegate and word the access history
	// ("Reader to Writer"). For a global admin a failed read never blocks the
	// grant; for a delegate it still does.
	existing, listErr := s.store.ListRepoGrants(ctx, target.ID)
	if !actor.IsAdmin {
		if listErr != nil {
			return domainauth.RepoGrant{}, listErr
		}
		for _, grant := range existing {
			if grant.Repository.String() == repo.String() && grant.Role == domainauth.RepoRoleAdmin {
				return domainauth.RepoGrant{}, domainauth.NewForbiddenError("repository administrators cannot modify another repository administrator's grant")
			}
		}
	}
	previous, hadPrevious := grantRoleOn(existing, repo.String())

	now := s.now()
	grant := domainauth.RepoGrant{UserID: target.ID, Repository: repo, Role: role, CreatedAt: now, UpdatedAt: now}
	if err := s.store.PutRepoGrant(ctx, grant); err != nil {
		return domainauth.RepoGrant{}, err
	}

	if listErr == nil {
		s.recordGrantPut(ctx, actor, target, grant, previous, hadPrevious)
	}

	return grant, nil
}

func (s *Service) PutAdminRepositoryGrant(ctx context.Context, actor domainauth.Principal, input ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	grant, err := s.PutRepositoryGrant(ctx, actor, input.Repository, input.Username, input.Role)
	if err != nil {
		return ports.AdminRepositoryGrant{}, err
	}

	return ports.AdminRepositoryGrant{Username: normalizeUsername(input.Username), Role: grant.Role, CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt}, nil
}

// DeleteRepositoryGrant is the delegate-eligible sibling of DeleteRepoGrant.
// The same existing-role escalation bound as PutRepositoryGrant applies: a
// non-global-admin actor cannot remove another repository administrator's
// grant on this repository.
func (s *Service) DeleteRepositoryGrant(ctx context.Context, actor domainauth.Principal, repository string, username string) error {
	repo, err := regixtrydomain.ParseRepositoryRef(strings.TrimSpace(repository))
	if err != nil {
		return err
	}
	if err := requireAdminOrRepoAdmin(actor, repo.String()); err != nil {
		return err
	}

	target, err := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil {
		return err
	}

	if !actor.IsAdmin {
		existing, err := s.store.ListRepoGrants(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, grant := range existing {
			if grant.Repository.String() == repo.String() && grant.Role == domainauth.RepoRoleAdmin {
				return domainauth.NewForbiddenError("repository administrators cannot modify another repository administrator's grant")
			}
		}
	}

	if err := s.store.DeleteRepoGrant(ctx, target.ID, repo); err != nil {
		return err
	}

	s.recordGrantDelete(ctx, actor, target, repo.String())

	return nil
}

func (s *Service) DeleteAdminRepositoryGrant(ctx context.Context, actor domainauth.Principal, repository string, username string) error {
	return s.DeleteRepositoryGrant(ctx, actor, repository, username)
}

// CreateRobot creates a robot account, its single repository grant, and its
// issued admin-credential token (design.md Decision 2 and Decision 6). The
// TTL ceiling and revocation are inherited unchanged by delegating token
// issuance to CreateAdminToken.
func (s *Service) CreateRobot(ctx context.Context, actor domainauth.Principal, input ports.CreateRobotInput) (ports.CreatedRobot, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.CreatedRobot{}, err
	}

	username := normalizeUsername(input.Name)
	if username == "" {
		return ports.CreatedRobot{}, domainauth.NewValidationError("robot name is required")
	}
	if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
		return ports.CreatedRobot{}, domainauth.NewConflictError("username already exists")
	} else if !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		return ports.CreatedRobot{}, err
	}

	// A robot is bound to exactly one repository OR one project, never both.
	// Neither falls through to the repository path and its usual error.
	repositoryName := strings.TrimSpace(input.Repository)
	projectName := strings.TrimSpace(input.Project)
	if repositoryName != "" && projectName != "" {
		return ports.CreatedRobot{}, domainauth.NewValidationError("a robot takes either a repository or a project, not both")
	}

	var repo regixtrydomain.RepositoryRef
	if projectName != "" {
		var err error
		if projectName, err = regixtrydomain.ParseProjectName(projectName); err != nil {
			return ports.CreatedRobot{}, err
		}
		if err := input.Role.Validate(); err != nil {
			return ports.CreatedRobot{}, err
		}
		// A robot token is an admin credential: a project admin robot would
		// delegate project management, so only reader and writer exist.
		if input.Role == domainauth.RepoRoleAdmin {
			return ports.CreatedRobot{}, domainauth.NewValidationError("robots on a project can be reader or writer only")
		}
		if err := s.projectMustExist(ctx, projectName); err != nil {
			return ports.CreatedRobot{}, err
		}
	} else {
		var err error
		if repo, err = regixtrydomain.ParseRepositoryRef(repositoryName); err != nil {
			return ports.CreatedRobot{}, err
		}
		if err := input.Role.Validate(); err != nil {
			return ports.CreatedRobot{}, err
		}
	}

	// Validate everything CreateAdminToken will validate before the first
	// write, so a bad TTL cannot leave an orphan robot behind.
	ttl, err := resolveAdminTokenTTL(input.TTL)
	if err != nil {
		return ports.CreatedRobot{}, err
	}

	now := s.now()
	user := domainauth.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: domainauth.RobotPasswordHash,
		IsRobot:      true,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return ports.CreatedRobot{}, err
	}

	var grant domainauth.RepoGrant
	var projectGrant domainauth.ProjectGrant
	boundTo := projectName
	if projectName != "" {
		projectGrant = domainauth.ProjectGrant{UserID: user.ID, Project: projectName, Role: input.Role, CreatedAt: now, UpdatedAt: now}
		if err := s.store.PutProjectGrant(ctx, projectGrant); err != nil {
			s.rollbackRobot(ctx, user.ID)
			return ports.CreatedRobot{}, err
		}
		boundTo = domainauth.ProjectEventRepository(projectName)
	} else {
		grant = domainauth.RepoGrant{UserID: user.ID, Repository: repo, Role: input.Role, CreatedAt: now, UpdatedAt: now}
		if err := s.store.PutRepoGrant(ctx, grant); err != nil {
			s.rollbackRobot(ctx, user.ID)
			return ports.CreatedRobot{}, err
		}
		boundTo = repo.String()
	}

	created, _, err := s.issueAdminToken(ctx, actor, ports.CreateAdminTokenInput{UserID: user.ID, Name: strings.TrimSpace(input.Name), TTL: input.TTL})
	if err != nil {
		s.rollbackRobot(ctx, user.ID)
		return ports.CreatedRobot{}, err
	}

	s.recordRobotCreated(ctx, actor, user, boundTo, input.Role, created.Token.Name, ttl)

	return ports.CreatedRobot{User: user, Grant: grant, ProjectGrant: projectGrant, Secret: created.Secret, Accessor: created.Accessor, ExpiresAt: created.ExpiresAt}, nil
}

// rollbackRobot best-effort deletes the robot user CreateRobot just wrote
// (grants and tokens cascade with the user). The original failure is what the
// caller reports; a failed rollback cannot be surfaced without masking it.
func (s *Service) rollbackRobot(ctx context.Context, userID string) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.rollbackTimeout)
	defer cancel()
	_ = s.store.DeleteUser(rollbackCtx, userID)
}

// ListRobots lists every robot account (design.md Decision 6): a dedicated
// store query, separate from ListUsers, which now excludes robot rows.
func (s *Service) ListRobots(ctx context.Context, actor domainauth.Principal) ([]domainauth.User, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}

	return s.store.ListRobots(ctx)
}

func (s *Service) CreateAdminRobot(ctx context.Context, actor domainauth.Principal, input ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	created, err := s.CreateRobot(ctx, actor, ports.CreateRobotInput{
		Name:       input.Name,
		Repository: input.Repository,
		Project:    input.Project,
		Role:       input.Role,
		TTL:        input.TTL,
	})
	if err != nil {
		return ports.AdminCreatedRobot{}, err
	}

	return ports.AdminCreatedRobot{
		Robot:     toAdminRobot(created.User, created.Grant, created.ProjectGrant),
		Secret:    created.Secret,
		Accessor:  created.Accessor,
		ExpiresAt: created.ExpiresAt,
	}, nil
}

func (s *Service) ListAdminRobots(ctx context.Context, actor domainauth.Principal) ([]ports.AdminRobot, error) {
	robots, err := s.ListRobots(ctx, actor)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminRobot, 0, len(robots))
	for _, robot := range robots {
		grants, err := s.store.ListRepoGrants(ctx, robot.ID)
		if err != nil {
			return nil, err
		}

		var grant domainauth.RepoGrant
		if len(grants) > 0 {
			grant = grants[0]
		}
		var projectGrant domainauth.ProjectGrant
		if len(grants) == 0 {
			projectGrants, err := s.store.ListProjectGrants(ctx, robot.ID)
			if err != nil {
				return nil, err
			}
			if len(projectGrants) > 0 {
				projectGrant = projectGrants[0]
			}
		}
		result = append(result, toAdminRobot(robot, grant, projectGrant))
	}

	return result, nil
}

// DeleteRobot hard-deletes a robot account (design.md robot-accounts
// deletion follow-up): deliberately breaking from this codebase's
// no-hard-delete-for-humans precedent, because a robot's only purpose is to
// hold exactly one (repository, role) grant plus its tokens — nothing is
// orphaned or historically meaningful by removing it. The IsRobot guard
// below is the single most important check: it is the boundary that keeps
// this global-admin-only endpoint from becoming a backdoor around hard
// deletion for human users.
func (s *Service) DeleteRobot(ctx context.Context, actor domainauth.Principal, userID string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.IsRobot {
		return domainauth.NewValidationError("target user is not a robot account")
	}

	if err := s.store.DeleteUser(ctx, userID); err != nil {
		return err
	}

	s.recordUserEvent(ctx, actor, domainauth.AccessActionRobotDeleted, user, "")

	return nil
}

func (s *Service) DeleteAdminRobot(ctx context.Context, actor domainauth.Principal, userID string) error {
	return s.DeleteRobot(ctx, actor, userID)
}

func toAdminRobot(user domainauth.User, grant domainauth.RepoGrant, projectGrant domainauth.ProjectGrant) ports.AdminRobot {
	role := grant.Role
	if projectGrant.Project != "" {
		role = projectGrant.Role
	}

	return ports.AdminRobot{
		ID:         user.ID,
		Username:   user.Username,
		Repository: grant.Repository.String(),
		Project:    projectGrant.Project,
		Role:       role,
		Enabled:    user.Enabled,
		CreatedAt:  user.CreatedAt,
	}
}

func toAdminUser(user domainauth.User) ports.AdminUser {
	return ports.AdminUser{
		ID:         user.ID,
		Username:   user.Username,
		IsAdmin:    user.IsAdmin,
		IsReadOnly: user.IsReadOnly,
		Enabled:    user.Enabled,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	}
}

func toAdminRepoGrant(grant domainauth.RepoGrant) ports.AdminRepoGrant {
	return ports.AdminRepoGrant{
		UserID:     grant.UserID,
		Repository: grant.Repository,
		Role:       grant.Role,
		CreatedAt:  grant.CreatedAt,
		UpdatedAt:  grant.UpdatedAt,
	}
}

func toAdminToken(token domainauth.Token) ports.AdminToken {
	return ports.AdminToken{
		ID:        token.ID,
		UserID:    token.UserID,
		Kind:      token.Kind,
		Name:      token.Name,
		Accessor:  token.Accessor,
		ExpiresAt: token.ExpiresAt,
		CreatedAt: token.CreatedAt,
		RevokedAt: token.RevokedAt,
	}
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

	// A request that named scopes but granted none must not yield an
	// unrestricted token: persist the restricted marker, report no scope.
	storedScope := grantedScope
	if storedScope == "" && len(requestedScopes) > 0 {
		storedScope = domainauth.ScopeNone
	}

	now := s.now()
	token := domainauth.Token{ID: uuid.NewString(), UserID: user.ID, Kind: domainauth.TokenKindAccess, Scope: storedScope, Accessor: shortAccessor("atk"), SecretHash: hashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(domainauth.AccessTokenTTL)}
	if err := s.store.CreateToken(ctx, token); err != nil {
		return ports.LoginResult{}, err
	}

	principal, err := s.buildPrincipal(ctx, user, token)
	if err != nil {
		return ports.LoginResult{}, err
	}

	return ports.LoginResult{Principal: principal, BearerToken: secret, ExpiresAt: token.ExpiresAt, Accessor: token.Accessor, Scope: grantedScope}, nil
}

func (s *Service) buildPrincipal(ctx context.Context, user domainauth.User, token domainauth.Token) (domainauth.Principal, error) {
	grants, err := s.store.ListRepoGrants(ctx, user.ID)
	if err != nil {
		grants = nil
	}
	// Same fail-closed handling as the exact grants: a read error leaves the
	// principal without project access, never with more.
	projectGrants, err := s.store.ListProjectGrants(ctx, user.ID)
	if err != nil {
		projectGrants = nil
	}
	scopes, err := token.Scopes()
	if err != nil {
		return domainauth.Principal{}, err
	}

	return domainauth.Principal{Subject: token.Accessor, UserID: user.ID, Username: user.Username, IsAdmin: user.IsAdmin, IsReadOnly: user.IsReadOnly, Grants: grants, ProjectGrants: projectGrants, Scopes: scopes, ExpiresAt: token.ExpiresAt}, nil
}

func (s *Service) grantedScopes(ctx context.Context, user domainauth.User, requestedScopes []domainauth.Scope) ([]domainauth.Scope, error) {
	if len(requestedScopes) == 0 {
		return nil, nil
	}

	grants, err := s.store.ListRepoGrants(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	projectGrants, err := s.store.ListProjectGrants(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	granted := make([]domainauth.Scope, 0, len(requestedScopes))
	for _, requested := range requestedScopes {
		if requested.IsRegixtryCatalog() {
			granted = append(granted, requested)
			continue
		}
		if !requested.IsRepository() {
			continue
		}

		actions := intersectRequestedActionsWithProjects(user.IsAdmin, user.IsReadOnly, grants, projectGrants, requested)
		if len(actions) == 0 {
			continue
		}

		granted = append(granted, domainauth.Scope{Type: "repository", Name: requested.Repository().String(), Actions: actions, Canonical: "repository:" + requested.Repository().String() + ":" + strings.Join(actions, ",")})
	}

	return granted, nil
}

func intersectRequestedActions(isAdmin bool, isReadOnly bool, grants []domainauth.RepoGrant, requested domainauth.Scope) []string {
	return intersectRequestedActionsWithProjects(isAdmin, isReadOnly, grants, nil, requested)
}

// intersectRequestedActionsWithProjects decides which of the requested
// actions a token may carry. The requested scope always names one exact
// repository; project grants are expanded here, at issue time, into the
// actions on that exact repository (the token's scopes never name a project),
// so a request for a repository outside the project gets nothing.
func intersectRequestedActionsWithProjects(isAdmin bool, isReadOnly bool, grants []domainauth.RepoGrant, projectGrants []domainauth.ProjectGrant, requested domainauth.Scope) []string {
	allowPull := false
	allowPush := false
	allowDelete := false

	if isAdmin {
		allowPull = requested.AllowsPull()
		allowPush = requested.AllowsPush()
		allowDelete = requested.AllowsDelete()
	} else if isReadOnly {
		// Registry-wide read: the domain-level probe in
		// hasGrantedRepositoryAccess (design.md Decision 5) already grants
		// read on every repository, but the token itself carries zero
		// scopes unless this branch runs — otherwise every pull fails
		// despite the domain check passing. Never grants push, and now
		// never grants delete either: the absence of a line here is the
		// guarantee, not a check.
		allowPull = requested.AllowsPull()
	} else if role, ok := domainauth.EffectiveRole(grants, projectGrants, requested.Repository().String()); ok {
		// The role is the highest of the exact grant and the grant of the
		// repository's project (first segment, ==).
		if role.AllowsRead() && requested.AllowsPull() {
			allowPull = true
		}
		if role.AllowsWrite() && requested.AllowsPush() {
			allowPush = true
		}
		// Own statement, own requested.AllowsDelete() guard — never
		// folded into the push branch as
		// `AllowsPush() || AllowsDelete()`. A fifth verb must likewise
		// add its own line, so no future action can inherit writer
		// derivation by being forgotten (proposal Q4).
		if role.AllowsWrite() && requested.AllowsDelete() {
			allowDelete = true
		}
	}

	actions := make([]string, 0, 3)
	if allowPull {
		actions = append(actions, "pull")
	}
	if allowPush {
		actions = append(actions, "push")
	}
	if allowDelete {
		actions = append(actions, "delete")
	}

	return actions
}

func requireAdmin(actor domainauth.Principal) error {
	if !actor.IsAdmin {
		return domainauth.NewForbiddenError("administrator privileges are required")
	}
	if !actor.IsUnrestricted() {
		return domainauth.NewForbiddenError("a scope-limited token cannot be used for administration")
	}

	return nil
}

// requireAdminOrRepoAdmin authorizes the delegated repository-grant
// namespace (design.md Decision 4): a global admin always passes, and a
// repo-admin grant on the exact target repository also passes. This
// deliberately reads actor.Grants directly instead of reusing
// Principal.HasRepoAdminAccess, which additionally requires a push token
// scope — a TUI/admin-API login requests zero scopes, so that helper would
// make delegation permanently dead code on exactly the tokens it runs on.
func requireAdminOrRepoAdmin(actor domainauth.Principal, repository string) error {
	if !actor.IsUnrestricted() {
		return domainauth.NewForbiddenError("a scope-limited token cannot be used for administration")
	}
	if actor.IsAdmin {
		return nil
	}

	for _, grant := range actor.Grants {
		if grant.Repository.String() == repository && grant.Role.AllowsAdmin() {
			return nil
		}
	}

	// A project admin is a delegate for every repository inside the project,
	// with the same escalation bounds as an exact repository admin (the
	// callers enforce them). The project is the first path segment compared
	// with ==: "odabank" never reaches "odabank2/x" nor the flat "odabank".
	if project, ok := repositoryProject(repository); ok && isProjectAdminOf(actor, project) {
		return nil
	}

	return domainauth.NewForbiddenError("repository administrator privileges are required")
}

// repositoryProject is the project of a repository name: the segment before
// the first '/'. A flat name belongs to no project.
func repositoryProject(repository string) (string, bool) {
	i := strings.IndexByte(repository, '/')
	if i <= 0 {
		return "", false
	}

	return repository[:i], true
}

// isProjectAdminOf reports whether the actor holds a project admin grant on
// exactly this project. A read-only principal never delegates, whatever its
// grants say (project admin grants for read-only users are refused at grant
// time; this covers one that predates the rule).
func isProjectAdminOf(actor domainauth.Principal, project string) bool {
	if actor.IsReadOnly {
		return false
	}
	for _, grant := range actor.ProjectGrants {
		if grant.Project == project && grant.Role.AllowsAdmin() {
			return true
		}
	}

	return false
}

// requireAdminOrProjectAdmin authorizes the delegated project-member
// namespace: a global admin always passes, a project admin passes for their
// own project only. A scope-limited token never administers.
func requireAdminOrProjectAdmin(actor domainauth.Principal, project string) error {
	if !actor.IsUnrestricted() {
		return domainauth.NewForbiddenError("a scope-limited token cannot be used for administration")
	}
	if actor.IsAdmin {
		return nil
	}
	if isProjectAdminOf(actor, project) {
		return nil
	}

	return domainauth.NewForbiddenError("project administrator privileges are required")
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func validatePassword(password string) error {
	if len(strings.TrimSpace(password)) < 8 {
		return domainauth.NewValidationError("password must be at least 8 characters")
	}
	if isCommonPassword(password) {
		return domainauth.NewValidationError("password is too common; choose a less guessable one")
	}

	return nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
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

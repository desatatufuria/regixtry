package ports

import (
	"context"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

type AuthStore interface {
	Close() error
	HasActiveGlobalAdmin(ctx context.Context) (bool, error)
	ListUsers(ctx context.Context) ([]domainauth.User, error)
	GetUserByUsername(ctx context.Context, username string) (domainauth.User, error)
	GetUserByID(ctx context.Context, userID string) (domainauth.User, error)
	UpsertUser(ctx context.Context, user domainauth.User) error
	DeleteUser(ctx context.Context, userID string) error
	ListRobots(ctx context.Context) ([]domainauth.User, error)
	ListRepoGrants(ctx context.Context, userID string) ([]domainauth.RepoGrant, error)
	ListRepoGrantsByRepository(ctx context.Context, repository regixtrydomain.RepositoryRef) ([]domainauth.RepoGrant, error)
	PutRepoGrant(ctx context.Context, grant domainauth.RepoGrant) error
	DeleteRepoGrant(ctx context.Context, userID string, repository regixtrydomain.RepositoryRef) error
	CreateToken(ctx context.Context, token domainauth.Token) error
	GetTokenBySecretHash(ctx context.Context, kind domainauth.TokenKind, secretHash string) (domainauth.Token, error)
	GetTokenByAccessor(ctx context.Context, kind domainauth.TokenKind, accessor string) (domainauth.Token, error)
	ListTokensByUser(ctx context.Context, userID string, kind domainauth.TokenKind) ([]domainauth.Token, error)
	RevokeTokenByAccessor(ctx context.Context, accessor string, revokedAt time.Time) error
}

type LoginResult struct {
	Principal   domainauth.Principal
	BearerToken string
	ExpiresAt   time.Time
	Accessor    string
	Scope       string
}

type BootstrapAdminInput struct {
	Username       string
	Password       string
	RotatePassword bool
}

type BootstrapAdminResult struct {
	User            domainauth.User
	Created         bool
	PasswordRotated bool
}

type CreateAdminTokenInput struct {
	UserID string
	Name   string
	TTL    time.Duration
}

type CreateUserInput struct {
	Username   string
	Password   string
	IsAdmin    bool
	IsReadOnly bool
	Enabled    bool
}

type UpdateUserInput struct {
	UserID        string
	Username      string
	IsAdmin       bool
	IsReadOnly    bool
	PreserveAdmin bool
}

// CreateRobotInput is the service-level input for creating a robot account
// (design.md Decision 2): exactly one repository+role is bound at creation,
// enforced by the service, not the schema. TTL follows CreateAdminTokenInput's
// convention: zero means DefaultAdminTokenTTL, reused unchanged.
type CreateRobotInput struct {
	Name       string
	Repository string
	Role       domainauth.RepoRole
	TTL        time.Duration
}

// CreatedRobot is CreateRobot's result: the persisted robot user, its single
// grant, and its issued admin-credential token (secret shown once, matching
// CreateAdminToken's shape).
type CreatedRobot struct {
	User      domainauth.User
	Grant     domainauth.RepoGrant
	Secret    string
	Accessor  string
	ExpiresAt time.Time
}

type CreatedAdminToken struct {
	Token      domainauth.Token
	Secret     string
	Plaintext  string
	Accessor   string
	ExpiresAt  time.Time
	TargetUser domainauth.User
}

type AdminUser struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	IsAdmin    bool      `json:"is_admin"`
	IsReadOnly bool      `json:"is_read_only"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type AdminCreateUserInput struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	IsAdmin    bool   `json:"is_admin"`
	IsReadOnly bool   `json:"is_read_only"`
	Enabled    bool   `json:"enabled"`
}

type AdminResetPasswordInput struct {
	UserID      string `json:"-"`
	NewPassword string `json:"new_password"`
}

type AdminRepoGrant struct {
	UserID     string                       `json:"user_id"`
	Repository regixtrydomain.RepositoryRef `json:"repository"`
	Role       domainauth.RepoRole          `json:"role"`
	CreatedAt  time.Time                    `json:"created_at"`
	UpdatedAt  time.Time                    `json:"updated_at"`
}

type AdminPutRepoGrantInput struct {
	UserID     string              `json:"-"`
	Repository string              `json:"-"`
	Role       domainauth.RepoRole `json:"role"`
}

// AdminRepositoryGrant is the delegate-facing grant projection
// (design.md Decision 3's route table). It deliberately carries no user ID,
// password hash, admin/read-only flags, or any other repository's grants —
// a delegate must be able to name a user to grant without ever reading the
// user directory (threat matrix: identity disclosure).
type AdminRepositoryGrant struct {
	Username  string              `json:"username"`
	Role      domainauth.RepoRole `json:"role"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

type AdminPutRepositoryGrantInput struct {
	Repository string              `json:"-"`
	Username   string              `json:"-"`
	Role       domainauth.RepoRole `json:"role"`
}

type AdminToken struct {
	ID        string               `json:"id"`
	UserID    string               `json:"user_id"`
	Kind      domainauth.TokenKind `json:"kind"`
	Name      string               `json:"name,omitempty"`
	Accessor  string               `json:"accessor"`
	ExpiresAt time.Time            `json:"expires_at"`
	CreatedAt time.Time            `json:"created_at"`
	RevokedAt *time.Time           `json:"revoked_at,omitempty"`
}

type AdminCreateTokenInput struct {
	UserID string        `json:"-"`
	Name   string        `json:"name"`
	TTL    time.Duration `json:"-"`
}

type AdminCreatedToken struct {
	Token      AdminToken `json:"token"`
	Secret     string     `json:"secret"`
	Accessor   string     `json:"accessor"`
	ExpiresAt  time.Time  `json:"expires_at"`
	TargetUser AdminUser  `json:"target_user"`
}

type AdminHTTPService interface {
	ListAdminUsers(ctx context.Context, actor domainauth.Principal) ([]AdminUser, error)
	CreateAdminUser(ctx context.Context, actor domainauth.Principal, input AdminCreateUserInput) (AdminUser, error)
	EnableAdminUser(ctx context.Context, actor domainauth.Principal, userID string) (AdminUser, error)
	DisableAdminUser(ctx context.Context, actor domainauth.Principal, userID string) (AdminUser, error)
	ResetAdminUserPassword(ctx context.Context, actor domainauth.Principal, input AdminResetPasswordInput) error
	ListAdminUserRepoGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]AdminRepoGrant, error)
	PutAdminUserRepoGrant(ctx context.Context, actor domainauth.Principal, input AdminPutRepoGrantInput) (AdminRepoGrant, error)
	DeleteAdminUserRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string) error
	ListAdminRepositoryGrants(ctx context.Context, actor domainauth.Principal, repository string) ([]AdminRepositoryGrant, error)
	PutAdminRepositoryGrant(ctx context.Context, actor domainauth.Principal, input AdminPutRepositoryGrantInput) (AdminRepositoryGrant, error)
	DeleteAdminRepositoryGrant(ctx context.Context, actor domainauth.Principal, repository string, username string) error
	ListAdminUserTokens(ctx context.Context, actor domainauth.Principal, userID string) ([]AdminToken, error)
	CreateAdminUserToken(ctx context.Context, actor domainauth.Principal, input AdminCreateTokenInput) (AdminCreatedToken, error)
	RevokeAdminUserToken(ctx context.Context, actor domainauth.Principal, userID string, accessor string) error
}

type AuthService interface {
	EnsureBootstrapAdmin(ctx context.Context) error
	BootstrapAdmin(ctx context.Context, input BootstrapAdminInput) (BootstrapAdminResult, error)
	ListUsers(ctx context.Context, actor domainauth.Principal) ([]domainauth.User, error)
	CreateUser(ctx context.Context, actor domainauth.Principal, input CreateUserInput) (domainauth.User, error)
	UpdateUser(ctx context.Context, actor domainauth.Principal, input UpdateUserInput) (domainauth.User, error)
	SetUserEnabled(ctx context.Context, actor domainauth.Principal, userID string, enabled bool) (domainauth.User, error)
	DeleteUser(ctx context.Context, actor domainauth.Principal, userID string) error
	LoginWithPassword(ctx context.Context, username string, password string, requestedScopes []domainauth.Scope) (LoginResult, error)
	LoginWithPreissuedToken(ctx context.Context, username string, token string, requestedScopes []domainauth.Scope) (LoginResult, error)
	VerifyAccessToken(ctx context.Context, bearerToken string) (domainauth.Principal, error)
	CreateAdminToken(ctx context.Context, actor domainauth.Principal, input CreateAdminTokenInput) (CreatedAdminToken, error)
	ListRepoGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.RepoGrant, error)
	ListAdminTokens(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.Token, error)
	RevokeAdminToken(ctx context.Context, actor domainauth.Principal, accessor string) error
	ResetPassword(ctx context.Context, actor domainauth.Principal, userID string, newPassword string) error
	PutRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string, role domainauth.RepoRole) (domainauth.RepoGrant, error)
	DeleteRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string) error
	ListRepositoryGrants(ctx context.Context, actor domainauth.Principal, repository string) ([]domainauth.RepoGrant, error)
	PutRepositoryGrant(ctx context.Context, actor domainauth.Principal, repository string, username string, role domainauth.RepoRole) (domainauth.RepoGrant, error)
	DeleteRepositoryGrant(ctx context.Context, actor domainauth.Principal, repository string, username string) error
}

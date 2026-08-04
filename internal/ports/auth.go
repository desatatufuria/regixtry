package ports

import (
	"context"
	"time"

	domainauth "registry/internal/domain/auth"
	registrydomain "registry/internal/domain/registry"
)

type AuthStore interface {
	Close() error
	HasActiveGlobalAdmin(ctx context.Context) (bool, error)
	ListUsers(ctx context.Context) ([]domainauth.User, error)
	GetUserByUsername(ctx context.Context, username string) (domainauth.User, error)
	GetUserByID(ctx context.Context, userID string) (domainauth.User, error)
	UpsertUser(ctx context.Context, user domainauth.User) error
	DeleteUser(ctx context.Context, userID string) error
	ListRepoGrants(ctx context.Context, userID string) ([]domainauth.RepoGrant, error)
	PutRepoGrant(ctx context.Context, grant domainauth.RepoGrant) error
	DeleteRepoGrant(ctx context.Context, userID string, repository registrydomain.RepositoryRef) error
	CreateToken(ctx context.Context, token domainauth.Token) error
	GetTokenBySecretHash(ctx context.Context, kind domainauth.TokenKind, secretHash string) (domainauth.Token, error)
	ListTokensByUser(ctx context.Context, userID string, kind domainauth.TokenKind) ([]domainauth.Token, error)
	RevokeTokenByAccessor(ctx context.Context, accessor string, revokedAt time.Time) error
}

type LoginResult struct {
	Principal   domainauth.Principal
	BearerToken string
	ExpiresAt   time.Time
	Accessor    string
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
	Username string
	Password string
	IsAdmin  bool
	Enabled  bool
}

type UpdateUserInput struct {
	UserID        string
	Username      string
	IsAdmin       bool
	PreserveAdmin bool
}

type CreatedAdminToken struct {
	Token      domainauth.Token
	Secret     string
	Plaintext  string
	Accessor   string
	ExpiresAt  time.Time
	TargetUser domainauth.User
}

type AuthService interface {
	EnsureBootstrapAdmin(ctx context.Context) error
	BootstrapAdmin(ctx context.Context, input BootstrapAdminInput) (BootstrapAdminResult, error)
	ListUsers(ctx context.Context, actor domainauth.Principal) ([]domainauth.User, error)
	CreateUser(ctx context.Context, actor domainauth.Principal, input CreateUserInput) (domainauth.User, error)
	UpdateUser(ctx context.Context, actor domainauth.Principal, input UpdateUserInput) (domainauth.User, error)
	SetUserEnabled(ctx context.Context, actor domainauth.Principal, userID string, enabled bool) (domainauth.User, error)
	DeleteUser(ctx context.Context, actor domainauth.Principal, userID string) error
	LoginWithPassword(ctx context.Context, username string, password string) (LoginResult, error)
	LoginWithPreissuedToken(ctx context.Context, username string, token string) (LoginResult, error)
	VerifyAccessToken(ctx context.Context, bearerToken string) (domainauth.Principal, error)
	CreateAdminToken(ctx context.Context, actor domainauth.Principal, input CreateAdminTokenInput) (CreatedAdminToken, error)
	ListRepoGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.RepoGrant, error)
	ListAdminTokens(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.Token, error)
	RevokeAdminToken(ctx context.Context, actor domainauth.Principal, accessor string) error
	ResetPassword(ctx context.Context, actor domainauth.Principal, userID string, newPassword string) error
	PutRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string, role domainauth.RepoRole) (domainauth.RepoGrant, error)
	DeleteRepoGrant(ctx context.Context, actor domainauth.Principal, userID string, repository string) error
}

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	domainauth "registry/internal/domain/auth"
	registrydomain "registry/internal/domain/registry"
)

type Store struct {
	db *sql.DB
}

func New(dsn string) (*Store, error) {
	return NewWithDriver("pgx", dsn)
}

func NewWithDriver(driverName string, dsn string) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.bootstrap(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) HasActiveGlobalAdmin(ctx context.Context) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM auth_users WHERE is_admin = TRUE AND enabled = TRUE`)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]domainauth.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, is_admin, enabled, created_at, updated_at
		FROM auth_users
		ORDER BY username ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]domainauth.User, 0)
	for rows.Next() {
		user, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, rows.Err()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (domainauth.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, is_admin, enabled, created_at, updated_at
		FROM auth_users
		WHERE username = $1
	`, strings.ToLower(strings.TrimSpace(username))))
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (domainauth.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, is_admin, enabled, created_at, updated_at
		FROM auth_users
		WHERE id = $1
	`, strings.TrimSpace(userID)))
}

func (s *Store) UpsertUser(ctx context.Context, user domainauth.User) error {
	user.Username = strings.ToLower(strings.TrimSpace(user.Username))
	if err := user.Validate(); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_users (id, username, password_hash, is_admin, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			password_hash = excluded.password_hash,
			is_admin = excluded.is_admin,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, user.ID, user.Username, user.PasswordHash, user.IsAdmin, user.Enabled, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	return err
}

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	trimmedUserID := strings.TrimSpace(userID)
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_repo_grants WHERE user_id = $1`, trimmedUserID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = $1`, trimmedUserID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM auth_users WHERE id = $1`, trimmedUserID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domainauth.NewNotFoundError("user", trimmedUserID)
	}

	err = tx.Commit()
	return err
}

func (s *Store) ListRepoGrants(ctx context.Context, userID string) ([]domainauth.RepoGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT repository, role, created_at, updated_at
		FROM auth_repo_grants
		WHERE user_id = $1
		ORDER BY repository ASC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grants := make([]domainauth.RepoGrant, 0)
	for rows.Next() {
		var repositoryName string
		var roleValue string
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&repositoryName, &roleValue, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		repository, err := registrydomain.ParseRepositoryRef(repositoryName)
		if err != nil {
			return nil, err
		}
		grant := domainauth.RepoGrant{UserID: userID, Repository: repository, Role: domainauth.RepoRole(roleValue), CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt)}
		if err := grant.Validate(); err != nil {
			return nil, err
		}

		grants = append(grants, grant)
	}

	return grants, rows.Err()
}

func (s *Store) PutRepoGrant(ctx context.Context, grant domainauth.RepoGrant) error {
	if err := grant.Validate(); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_repo_grants (user_id, repository, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(user_id, repository) DO UPDATE SET
			role = excluded.role,
			updated_at = excluded.updated_at
	`, grant.UserID, grant.Repository.String(), string(grant.Role), formatTime(grant.CreatedAt), formatTime(grant.UpdatedAt))
	return err
}

func (s *Store) DeleteRepoGrant(ctx context.Context, userID string, repository registrydomain.RepositoryRef) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM auth_repo_grants WHERE user_id = $1 AND repository = $2`, strings.TrimSpace(userID), repository.String())
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domainauth.NewNotFoundError("grant", repository.String())
	}

	return nil
}

func (s *Store) CreateToken(ctx context.Context, token domainauth.Token) error {
	if err := token.Validate(); err != nil {
		return err
	}

	var revokedAt any
	if token.RevokedAt != nil {
		revokedAt = formatTime(*token.RevokedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_tokens (id, user_id, kind, name, accessor, secret_hash, expires_at, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, token.ID, token.UserID, string(token.Kind), token.Name, token.Accessor, token.SecretHash, formatTime(token.ExpiresAt), formatTime(token.CreatedAt), revokedAt)
	return err
}

func (s *Store) GetTokenBySecretHash(ctx context.Context, kind domainauth.TokenKind, secretHash string) (domainauth.Token, error) {
	return s.scanToken(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, kind, name, accessor, secret_hash, expires_at, created_at, revoked_at
		FROM auth_tokens
		WHERE kind = $1 AND secret_hash = $2
	`, string(kind), strings.TrimSpace(secretHash)))
}

func (s *Store) ListTokensByUser(ctx context.Context, userID string, kind domainauth.TokenKind) ([]domainauth.Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, kind, name, accessor, secret_hash, expires_at, created_at, revoked_at
		FROM auth_tokens
		WHERE user_id = $1 AND kind = $2
		ORDER BY created_at DESC
	`, strings.TrimSpace(userID), string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]domainauth.Token, 0)
	for rows.Next() {
		token, err := scanTokenFromRows(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}

	return tokens, rows.Err()
}

func (s *Store) RevokeTokenByAccessor(ctx context.Context, accessor string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE auth_tokens
		SET revoked_at = $2
		WHERE accessor = $1 AND revoked_at IS NULL
	`, strings.TrimSpace(accessor), formatTime(revokedAt))
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return domainauth.NewNotFoundError("token", accessor)
	}

	return nil
}

func (s *Store) bootstrap(ctx context.Context) error {
	return bootstrapSchema(ctx, s.db)
}

func (s *Store) scanUser(row *sql.Row) (domainauth.User, error) {
	user, err := scanUserRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return domainauth.User{}, domainauth.NewNotFoundError("user", "")
		}
		return domainauth.User{}, err
	}

	return user, nil
}

func scanUserRow(scanner rowScanner) (domainauth.User, error) {
	var user domainauth.User
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.Enabled, &createdAt, &updatedAt); err != nil {
		return domainauth.User{}, err
	}

	user.CreatedAt = parseTime(createdAt)
	user.UpdatedAt = parseTime(updatedAt)
	return user, user.Validate()
}

func (s *Store) scanToken(row *sql.Row) (domainauth.Token, error) {
	token, err := scanTokenRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return domainauth.Token{}, domainauth.NewNotFoundError("token", "")
		}
		return domainauth.Token{}, err
	}

	return token, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTokenRow(scanner rowScanner) (domainauth.Token, error) {
	var token domainauth.Token
	var expiresAt string
	var createdAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(&token.ID, &token.UserID, &token.Kind, &token.Name, &token.Accessor, &token.SecretHash, &expiresAt, &createdAt, &revokedAt); err != nil {
		return domainauth.Token{}, err
	}

	token.ExpiresAt = parseTime(expiresAt)
	token.CreatedAt = parseTime(createdAt)
	if revokedAt.Valid {
		revoked := parseTime(revokedAt.String)
		token.RevokedAt = &revoked
	}

	return token, token.Validate()
}

func scanTokenFromRows(rows *sql.Rows) (domainauth.Token, error) {
	return scanTokenRow(rows)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

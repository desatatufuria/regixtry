package postgres

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

func TestStoreBootstrapsAndPersistsAuthState(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	user := domainauth.User{ID: "user-1", Username: "admin", PasswordHash: "hash-1", IsAdmin: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(context.Background(), user); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}

	hasAdmin, err := store.HasActiveGlobalAdmin(context.Background())
	if err != nil {
		t.Fatalf("HasActiveGlobalAdmin() error = %v", err)
	}
	if !hasAdmin {
		t.Fatal("HasActiveGlobalAdmin() = false, want true")
	}

	loadedUser, err := store.GetUserByUsername(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if loadedUser.ID != user.ID || !loadedUser.IsAdmin {
		t.Fatalf("loadedUser = %#v, want id=%q admin=true", loadedUser, user.ID)
	}

	repo := regixtrydomain.MustParseRepositoryRef("team/app")
	grant := domainauth.RepoGrant{UserID: user.ID, Repository: repo, Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}
	if err := store.PutRepoGrant(context.Background(), grant); err != nil {
		t.Fatalf("PutRepoGrant() error = %v", err)
	}

	grants, err := store.ListRepoGrants(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("ListRepoGrants() error = %v", err)
	}
	if len(grants) != 1 || grants[0].Repository.String() != repo.String() || grants[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("grants = %#v, want single writer grant", grants)
	}

	token := domainauth.Token{ID: "token-1", UserID: user.ID, Kind: domainauth.TokenKindAdminCredential, Name: "ci", Accessor: "act_1", SecretHash: "secret-hash", CreatedAt: now, ExpiresAt: now.Add(domainauth.DefaultAdminTokenTTL)}
	if err := store.CreateToken(context.Background(), token); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	loadedToken, err := store.GetTokenBySecretHash(context.Background(), domainauth.TokenKindAdminCredential, token.SecretHash)
	if err != nil {
		t.Fatalf("GetTokenBySecretHash() error = %v", err)
	}
	if loadedToken.Accessor != token.Accessor || loadedToken.Name != token.Name {
		t.Fatalf("loadedToken = %#v, want accessor=%q name=%q", loadedToken, token.Accessor, token.Name)
	}

	loadedByAccessor, err := store.GetTokenByAccessor(context.Background(), domainauth.TokenKindAdminCredential, token.Accessor)
	if err != nil {
		t.Fatalf("GetTokenByAccessor() error = %v", err)
	}
	if loadedByAccessor.ID != token.ID || loadedByAccessor.UserID != user.ID {
		t.Fatalf("loadedByAccessor = %#v, want token id=%q user=%q", loadedByAccessor, token.ID, user.ID)
	}

	accessToken := domainauth.Token{ID: "token-2", UserID: user.ID, Kind: domainauth.TokenKindAccess, Scope: "repository:team/app:pull", Accessor: "atk_1", SecretHash: "access-secret-hash", CreatedAt: now, ExpiresAt: now.Add(domainauth.AccessTokenTTL)}
	if err := store.CreateToken(context.Background(), accessToken); err != nil {
		t.Fatalf("CreateToken(access) error = %v", err)
	}
	loadedAccessToken, err := store.GetTokenBySecretHash(context.Background(), domainauth.TokenKindAccess, accessToken.SecretHash)
	if err != nil {
		t.Fatalf("GetTokenBySecretHash(access) error = %v", err)
	}
	if loadedAccessToken.Scope != accessToken.Scope {
		t.Fatalf("loadedAccessToken.Scope = %q, want %q", loadedAccessToken.Scope, accessToken.Scope)
	}

	tokens, err := store.ListTokensByUser(context.Background(), user.ID, domainauth.TokenKindAdminCredential)
	if err != nil {
		t.Fatalf("ListTokensByUser() error = %v", err)
	}
	if len(tokens) != 1 || tokens[0].Accessor != token.Accessor {
		t.Fatalf("tokens = %#v, want single token %q", tokens, token.Accessor)
	}

	revokedAt := now.Add(time.Hour)
	if err := store.RevokeTokenByAccessor(context.Background(), token.Accessor, revokedAt); err != nil {
		t.Fatalf("RevokeTokenByAccessor() error = %v", err)
	}

	revokedToken, err := store.GetTokenBySecretHash(context.Background(), domainauth.TokenKindAdminCredential, token.SecretHash)
	if err != nil {
		t.Fatalf("GetTokenBySecretHash(revoked) error = %v", err)
	}
	if revokedToken.RevokedAt == nil || !revokedToken.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revokedToken.RevokedAt = %#v, want %v", revokedToken.RevokedAt, revokedAt)
	}

	if err := store.DeleteRepoGrant(context.Background(), user.ID, repo); err != nil {
		t.Fatalf("DeleteRepoGrant() error = %v", err)
	}

	grants, err = store.ListRepoGrants(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("ListRepoGrants(after delete) error = %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("len(grants) = %d, want 0", len(grants))
	}
}

func TestMigrationsCreateOnlyAuthTables(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()

	rows, err := store.db.QueryContext(context.Background(), `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE 'auth_%'
		ORDER BY name ASC
	`)
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		names = append(names, name)
	}

	want := []string{"auth_repo_grants", "auth_tokens", "auth_users"}
	if len(names) != len(want) {
		t.Fatalf("auth tables = %#v, want %#v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("auth tables = %#v, want %#v", names, want)
		}
	}

	var metadataCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name IN ('repositories', 'manifests', 'tags', 'uploads')`).Scan(&metadataCount); err != nil {
		t.Fatalf("metadata count query error = %v", err)
	}
	if metadataCount != 0 {
		t.Fatalf("metadataCount = %d, want 0", metadataCount)
	}
}

func newSQLiteBackedStore(t *testing.T) *Store {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store := &Store{db: db}
	if err := store.bootstrap(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("bootstrap() error = %v", err)
	}

	return store
}

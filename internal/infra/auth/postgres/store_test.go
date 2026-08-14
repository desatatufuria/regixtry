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

// TestStoreRoundTripsIsReadOnlyAcrossAuthUserQueries pins design.md
// Decision 5 and Decision 8: the is_read_only column must survive
// UpsertUser/ListUsers/GetUserByID/GetUserByUsername, and toggling it back
// off must persist just as reliably as toggling it on.
func TestStoreRoundTripsIsReadOnlyAcrossAuthUserQueries(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	user := domainauth.User{ID: "user-ro-1", Username: "reader", PasswordHash: "hash-1", IsReadOnly: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(context.Background(), user); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}

	byUsername, err := store.GetUserByUsername(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if !byUsername.IsReadOnly {
		t.Fatalf("GetUserByUsername().IsReadOnly = false, want true")
	}

	byID, err := store.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if !byID.IsReadOnly {
		t.Fatalf("GetUserByID().IsReadOnly = false, want true")
	}

	users, err := store.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	found := false
	for _, listed := range users {
		if listed.ID != user.ID {
			continue
		}
		found = true
		if !listed.IsReadOnly {
			t.Fatalf("ListUsers() entry IsReadOnly = false, want true")
		}
	}
	if !found {
		t.Fatalf("ListUsers() = %#v, want to contain user %q", users, user.ID)
	}

	user.IsReadOnly = false
	user.UpdatedAt = now.Add(time.Minute)
	if err := store.UpsertUser(context.Background(), user); err != nil {
		t.Fatalf("UpsertUser(clear) error = %v", err)
	}

	cleared, err := store.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetUserByID(after clear) error = %v", err)
	}
	if cleared.IsReadOnly {
		t.Fatalf("GetUserByID(after clear).IsReadOnly = true, want false")
	}
}

// TestStoreListRepoGrantsByRepositoryReturnsGrantsAcrossUsers pins
// design.md Decision 3's delegate route table: ListRepoGrantsByRepository
// must return every grant recorded for one repository across different
// users, and must not return grants for a different repository.
func TestStoreListRepoGrantsByRepositoryReturnsGrantsAcrossUsers(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	alice := domainauth.User{ID: "user-alice", Username: "alice", PasswordHash: "hash-1", Enabled: true, CreatedAt: now, UpdatedAt: now}
	bob := domainauth.User{ID: "user-bob", Username: "bob", PasswordHash: "hash-2", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(context.Background(), alice); err != nil {
		t.Fatalf("UpsertUser(alice) error = %v", err)
	}
	if err := store.UpsertUser(context.Background(), bob); err != nil {
		t.Fatalf("UpsertUser(bob) error = %v", err)
	}

	targetRepo := regixtrydomain.MustParseRepositoryRef("team/app")
	otherRepo := regixtrydomain.MustParseRepositoryRef("team/other")
	if err := store.PutRepoGrant(context.Background(), domainauth.RepoGrant{UserID: alice.ID, Repository: targetRepo, Role: domainauth.RepoRoleAdmin, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("PutRepoGrant(alice, team/app) error = %v", err)
	}
	if err := store.PutRepoGrant(context.Background(), domainauth.RepoGrant{UserID: bob.ID, Repository: targetRepo, Role: domainauth.RepoRoleReader, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("PutRepoGrant(bob, team/app) error = %v", err)
	}
	if err := store.PutRepoGrant(context.Background(), domainauth.RepoGrant{UserID: bob.ID, Repository: otherRepo, Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("PutRepoGrant(bob, team/other) error = %v", err)
	}

	grants, err := store.ListRepoGrantsByRepository(context.Background(), targetRepo)
	if err != nil {
		t.Fatalf("ListRepoGrantsByRepository() error = %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("len(grants) = %d, want 2: %#v", len(grants), grants)
	}

	byUserID := map[string]domainauth.RepoGrant{}
	for _, grant := range grants {
		if grant.Repository.String() != targetRepo.String() {
			t.Fatalf("grant.Repository = %q, want %q", grant.Repository.String(), targetRepo.String())
		}
		byUserID[grant.UserID] = grant
	}
	if byUserID[alice.ID].Role != domainauth.RepoRoleAdmin {
		t.Fatalf("alice's role = %q, want repo-admin", byUserID[alice.ID].Role)
	}
	if byUserID[bob.ID].Role != domainauth.RepoRoleReader {
		t.Fatalf("bob's role = %q, want repo-reader", byUserID[bob.ID].Role)
	}
}

// TestStoreListUsersExcludesRobotsAndListRobotsReturnsOnlyRobots pins
// design.md Decision 6: the default human user listing must exclude robot
// rows (WHERE is_robot = FALSE), and the new ListRobots must return only
// robot rows (WHERE is_robot = TRUE) — the two queries partition
// auth_users, and neither leaks the other kind.
func TestStoreListUsersExcludesRobotsAndListRobotsReturnsOnlyRobots(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	human := domainauth.User{ID: "user-human", Username: "alice", PasswordHash: "hash-1", Enabled: true, CreatedAt: now, UpdatedAt: now}
	robot := domainauth.User{ID: "user-robot", Username: "ci", PasswordHash: domainauth.RobotPasswordHash, IsRobot: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(context.Background(), human); err != nil {
		t.Fatalf("UpsertUser(human) error = %v", err)
	}
	if err := store.UpsertUser(context.Background(), robot); err != nil {
		t.Fatalf("UpsertUser(robot) error = %v", err)
	}

	users, err := store.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	for _, listed := range users {
		if listed.ID == robot.ID {
			t.Fatalf("ListUsers() = %#v, must not contain robot %q", users, robot.ID)
		}
	}
	foundHuman := false
	for _, listed := range users {
		if listed.ID == human.ID {
			foundHuman = true
		}
	}
	if !foundHuman {
		t.Fatalf("ListUsers() = %#v, want to contain human %q", users, human.ID)
	}

	robots, err := store.ListRobots(context.Background())
	if err != nil {
		t.Fatalf("ListRobots() error = %v", err)
	}
	if len(robots) != 1 || robots[0].ID != robot.ID || !robots[0].IsRobot {
		t.Fatalf("ListRobots() = %#v, want single robot %q", robots, robot.ID)
	}
}

// TestStoreDeleteUserRemovesUserGrantsAndTokens is a characterization test
// (registry-acl-v1 robot-deletion follow-up): DeleteUser predates this
// change and was never covered here. It pins today's behavior — the user
// row is gone, a subsequent lookup is not-found, and the grant/token rows
// are genuinely removed too, queried directly rather than trusted from the
// FK definition alone ("verify, don't trust" discipline used throughout
// registry-acl-v1).
func TestStoreDeleteUserRemovesUserGrantsAndTokens(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	user := domainauth.User{ID: "user-delete", Username: "ci-delete", PasswordHash: domainauth.RobotPasswordHash, IsRobot: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(context.Background(), user); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}

	repo := regixtrydomain.MustParseRepositoryRef("team/app")
	grant := domainauth.RepoGrant{UserID: user.ID, Repository: repo, Role: domainauth.RepoRoleWriter, CreatedAt: now, UpdatedAt: now}
	if err := store.PutRepoGrant(context.Background(), grant); err != nil {
		t.Fatalf("PutRepoGrant() error = %v", err)
	}

	token := domainauth.Token{ID: "token-delete", UserID: user.ID, Kind: domainauth.TokenKindAdminCredential, Name: "ci", Accessor: "act_delete", SecretHash: "secret-hash-delete", CreatedAt: now, ExpiresAt: now.Add(domainauth.DefaultAdminTokenTTL)}
	if err := store.CreateToken(context.Background(), token); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	if err := store.DeleteUser(context.Background(), user.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}

	if _, err := store.GetUserByID(context.Background(), user.ID); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("GetUserByID(after delete) error = %v, want not-found", err)
	}

	var grantCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM auth_repo_grants WHERE user_id = $1`, user.ID).Scan(&grantCount); err != nil {
		t.Fatalf("count auth_repo_grants query error = %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("auth_repo_grants rows for %q after delete = %d, want 0", user.ID, grantCount)
	}

	var tokenCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM auth_tokens WHERE user_id = $1`, user.ID).Scan(&tokenCount); err != nil {
		t.Fatalf("count auth_tokens query error = %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("auth_tokens rows for %q after delete = %d, want 0", user.ID, tokenCount)
	}

	if err := store.DeleteUser(context.Background(), "does-not-exist"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Fatalf("DeleteUser(unknown) error = %v, want not-found", err)
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

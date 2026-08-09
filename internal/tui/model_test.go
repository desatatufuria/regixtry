package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func TestModelShowsEmptyStateWhenCatalogIsEmpty(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated := runCmd(t, model, model.Init())

	if updated.screen != screenEmpty {
		t.Fatalf("screen = %q, want %q", updated.screen, screenEmpty)
	}
	view := updated.View()
	if !strings.Contains(view, "Regixtry is empty") {
		t.Fatalf("view = %q, want empty-state title", view)
	}
}

func TestModelNavigatesRepositoriesManifestBlobsAndUploads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 28, 23, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}},
		tags: map[string]appregixtry.TagsResult{
			"library/alpine": {Name: "library/alpine", Tags: []string{"latest"}},
		},
		manifests: map[string]appregixtry.ManifestDetails{
			"library/alpine:latest": {
				Repository: "library/alpine",
				Reference:  "latest",
				MediaType:  "application/vnd.oci.image.manifest.v1+json",
				Digest:     "sha256:manifest",
				Size:       512,
				Blobs: []appregixtry.BlobDetails{{
					Repository: "library/alpine",
					MediaType:  "application/vnd.oci.image.layer.v1.tar",
					Digest:     "sha256:layer",
					Size:       128,
				}},
			},
		},
		uploads: map[string][]appregixtry.UploadDetails{
			"library/alpine": {{Repository: "library/alpine", ID: "upload-1", Status: "active", Size: 42, StartedAt: now, UpdatedAt: now, Location: "/tmp/upload-1"}},
		},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "enter")

	if updated.screen != screenManifest {
		t.Fatalf("screen = %q, want %q", updated.screen, screenManifest)
	}
	if !strings.Contains(updated.View(), "Manifest · library/alpine:latest") {
		t.Fatalf("view = %q, want manifest header", updated.View())
	}

	updated = runKey(t, updated, "b")
	if updated.screen != screenBlobs {
		t.Fatalf("screen = %q, want %q", updated.screen, screenBlobs)
	}
	updated = runKey(t, updated, "esc")
	updated = runKey(t, updated, "u")
	if updated.screen != screenUploads {
		t.Fatalf("screen = %q, want %q", updated.screen, screenUploads)
	}
	if !strings.Contains(updated.View(), "upload-1") {
		t.Fatalf("view = %q, want upload id", updated.View())
	}
}

func TestModelBlocksAdminUntilLogin(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	updated := runKey(t, model, "tab")

	if updated.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminLogin)
	}
	if updated.adminAuth != adminAuthStateUnauthenticated {
		t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateUnauthenticated)
	}
	if !strings.Contains(updated.View(), "Admin Login") {
		t.Fatalf("view = %q, want login screen", updated.View())
	}
}

func TestModelSuccessfulLoginRendersPremiumWorkspace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(2 * time.Minute)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }

	updated := runAdminLogin(t, model, "operator", "secret-pass")
	view := updated.View()

	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
	if !strings.Contains(view, "Regixtry Admin") || !strings.Contains(view, "Username contains") || !strings.Contains(view, "alice [admin, enabled]") {
		t.Fatalf("view = %q, want users shell", view)
	}
	if strings.Contains(view, "Create as admin") || strings.Contains(view, "New password") {
		t.Fatalf("view = %q, users screen must not render edit forms", view)
	}
}

func TestModelInvalidCredentialsStayOnLoginScreen(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{loginErr: errors.New("login: invalid credentials")}
	model := newAdminReadyModel(t, adminClient)

	updated := runAdminLogin(t, model, "operator", "wrong-pass")

	if updated.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminLogin)
	}
	if got, want := updated.status, "login: invalid credentials"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if !strings.Contains(updated.View(), "login: invalid credentials") {
		t.Fatalf("view = %q, want login error", updated.View())
	}
}

func TestModelCreateAdminUserRefreshesUsers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 22, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		listUsersResults: [][]ports.AdminUser{
			{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
			{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}, {ID: "u-2", Username: "bob", IsAdmin: true, Enabled: true}},
		},
		createUser: ports.AdminUser{ID: "u-2", Username: "bob", IsAdmin: true, Enabled: true},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")

	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, "bob")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "secret-pass")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "enter")

	if adminClient.createUserCalls != 1 || adminClient.listUsersCalls != 2 {
		t.Fatalf("admin client calls = %#v, want one create and refresh", adminClient)
	}
	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
	if got, want := updated.adminView.SelectedUserID, "u-2"; got != want {
		t.Fatalf("SelectedUserID = %q, want %q", got, want)
	}
	if !strings.Contains(updated.View(), `User "bob" created. Refreshing users...`) && !strings.Contains(updated.View(), `User "bob" created.`) {
		t.Fatalf("view = %q, want create-user status", updated.View())
	}
}

func TestModelUsersScreenShowsOnlyListAndSearch(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 2, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	view := updated.View()

	if !strings.Contains(view, "Username contains") || !strings.Contains(view, "alice [admin, enabled]") {
		t.Fatalf("view = %q, want users list and search", view)
	}

	lowerView := strings.ToLower(view)
	for _, forbidden := range []string{"create as admin", "new password", "grant details", "ttl seconds", "delete user", "edit is_admin", "toggle is_admin", "make admin", "remove admin"} {
		if strings.Contains(lowerView, forbidden) {
			t.Fatalf("view = %q, must not expose unsupported control %q", view, forbidden)
		}
	}
}

func TestModelResetPasswordFailureKeepsFormVisible(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession:     AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 5, 0, 0, time.UTC)},
		users:            []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		resetPasswordErr: errors.New("mutate admin resource: password must be 12 characters or longer"),
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")

	updated = runKey(t, updated, "p")
	updated = runKey(t, updated, "short")
	updated = runKey(t, updated, "enter")

	if updated.screen != screenAdminChangePassword {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminChangePassword)
	}
	if !strings.Contains(updated.View(), "Change Password") || !strings.Contains(updated.View(), "password must be 12 characters or longer") {
		t.Fatalf("view = %q, want recoverable backend error", updated.View())
	}
}

func TestModelDisableUserSuccessRefreshesUsersAndPreservesSelectionByID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 22, 10, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		listUsersResults: [][]ports.AdminUser{
			{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}, {ID: "u-2", Username: "bob", IsAdmin: false, Enabled: true}},
			{{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: false}, {ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		},
		disableUser: ports.AdminUser{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: false},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "j")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "x")

	if !strings.Contains(updated.View(), `Confirm disable user "bob"?`) {
		t.Fatalf("view = %q, want disable confirmation", updated.View())
	}

	updated = runKey(t, updated, "enter")

	if adminClient.disableCalls != 1 || adminClient.listUsersCalls != 2 {
		t.Fatalf("admin client calls = %#v, want one disable and refresh", adminClient)
	}
	if got, want := updated.adminView.SelectedUserID, "u-2"; got != want {
		t.Fatalf("SelectedUserID = %q, want %q", got, want)
	}
	if updated.screen != screenAdminEditUser {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminEditUser)
	}
	if !strings.Contains(updated.View(), "Disabled") {
		t.Fatalf("view = %q, want refreshed disabled status", updated.View())
	}
}

func TestModelNoSelectedUserBlocksEditing(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 20, 0, 0, time.UTC)}}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")

	if !strings.Contains(updated.View(), "Select a user to edit.") {
		t.Fatalf("view = %q, want blocked edit state", updated.View())
	}
}

func TestModelGrantSaveAndRemoveStayContextualizedToSelectedUser(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 30, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		grants: map[string][]ports.AdminRepoGrant{
			"u-1": {{UserID: "u-1", Repository: regixtrydomain.MustParseRepositoryRef("library/alpine"), Role: domainauth.RepoRoleWriter}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "g")

	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, "team/demo")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "enter")

	if adminClient.putGrantCalls != 1 {
		t.Fatalf("putGrantCalls = %d, want 1", adminClient.putGrantCalls)
	}
	if got, want := adminClient.lastGrantInput.UserID, "u-1"; got != want {
		t.Fatalf("grant user ID = %q, want %q", got, want)
	}

	updated = runKey(t, updated, "x")
	if !strings.Contains(updated.View(), `Remove grant "library/alpine" from "alice"?`) {
		t.Fatalf("view = %q, want grant removal confirmation", updated.View())
	}
	updated = runKey(t, updated, "enter")
	if adminClient.deleteGrantCalls != 1 {
		t.Fatalf("deleteGrantCalls = %d, want 1", adminClient.deleteGrantCalls)
	}
	if got, want := adminClient.lastDeleteGrantUserID, "u-1"; got != want {
		t.Fatalf("delete user ID = %q, want %q", got, want)
	}
}

func TestModelTokenCreateRevealOnceAndRevokeConfirmation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 22, 40, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		tokens: map[string][]ports.AdminToken{
			"u-1": {{ID: "t-1", UserID: "u-1", Kind: domainauth.TokenKindAdminCredential, Accessor: "tok_abc", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}},
		},
		createToken: ports.AdminCreatedToken{
			Secret:     "super-secret",
			Accessor:   "tok_new",
			ExpiresAt:  now.Add(48 * time.Hour),
			TargetUser: ports.AdminUser{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "t")
	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, "console")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "3600")
	updated = runKey(t, updated, "enter")

	if adminClient.createTokenCalls != 1 {
		t.Fatalf("createTokenCalls = %d, want 1", adminClient.createTokenCalls)
	}
	if !strings.Contains(updated.View(), "One-time secret") || !strings.Contains(updated.View(), "super-secret") {
		t.Fatalf("view = %q, want one-time secret reveal", updated.View())
	}

	updated = runKey(t, updated, "x")
	if !strings.Contains(updated.View(), `Revoke admin token "tok_abc" for "alice"?`) {
		t.Fatalf("view = %q, want revoke confirmation", updated.View())
	}
	updated = runKey(t, updated, "enter")

	if adminClient.revokeTokenCalls != 1 {
		t.Fatalf("revokeTokenCalls = %d, want 1", adminClient.revokeTokenCalls)
	}
	updated = runKey(t, updated, "g")
	if strings.Contains(updated.View(), "super-secret") {
		t.Fatalf("view = %q, want token secret cleared after leaving tokens panel", updated.View())
	}
}

func TestModelExpiredSessionForcesRelogin(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 4, 23, 20, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		grantsErr:    NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired),
	}
	model := newAdminReadyModel(t, adminClient)

	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "g")

	if updated.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminLogin)
	}
	if updated.adminAuth != adminAuthStateExpired {
		t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateExpired)
	}
	if !strings.Contains(updated.View(), AdminSessionExpiredReasonExpired) {
		t.Fatalf("view = %q, want expiry-specific relogin message", updated.View())
	}
}

func TestModelEscWalksBackThroughEditFlow(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 7, 21, 0, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		grants:       map[string][]ports.AdminRepoGrant{"u-1": {{UserID: "u-1", Repository: regixtrydomain.MustParseRepositoryRef("library/alpine"), Role: domainauth.RepoRoleReader}}},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")

	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "g")
	updated = runKey(t, updated, "n")
	if updated.screen != screenAdminAddGrant {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminAddGrant)
	}
	updated = runKey(t, updated, "esc")
	if updated.screen != screenAdminEditUserGrants {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminEditUserGrants)
	}
	updated = runKey(t, updated, "esc")
	if updated.screen != screenAdminEditUser {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminEditUser)
	}
	updated = runKey(t, updated, "esc")
	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
}

func TestModelSearchPreservesSelectionByIDAndEnterOpensEditUser(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 7, 21, 5, 0, 0, time.UTC)},
		users: []ports.AdminUser{
			{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true},
			{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: true},
			{ID: "u-3", Username: "bobby", IsAdmin: false, Enabled: true},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "j")

	updated = runKey(t, updated, "/")
	updated = runKey(t, updated, "bo")
	updated = runKey(t, updated, "enter")

	if got, want := updated.adminView.SelectedUserID, "u-2"; got != want {
		t.Fatalf("SelectedUserID = %q, want %q", got, want)
	}
	view := updated.View()
	if !strings.Contains(view, "Username contains") || !strings.Contains(view, "bo") {
		t.Fatalf("view = %q, want visible search query", view)
	}
	if strings.Contains(view, "alice [admin, enabled]") {
		t.Fatalf("view = %q, want alice filtered out", view)
	}

	updated = runKey(t, updated, "/")
	updated = runKey(t, updated, "bb")
	updated = runKey(t, updated, "enter")

	if got, want := updated.adminView.SelectedUserID, "u-3"; got != want {
		t.Fatalf("SelectedUserID = %q, want %q after narrowing filter", got, want)
	}
	if strings.Contains(updated.View(), "bob [user, enabled]") {
		t.Fatalf("view = %q, want bob filtered out after narrowing", updated.View())
	}
	updated = runKey(t, updated, "enter")
	if updated.screen != screenAdminEditUser {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminEditUser)
	}
	if !strings.Contains(updated.View(), "bobby") {
		t.Fatalf("view = %q, want selected user context", updated.View())
	}
}

type fakeQueryService struct {
	catalog   appregixtry.CatalogResult
	tags      map[string]appregixtry.TagsResult
	manifests map[string]appregixtry.ManifestDetails
	uploads   map[string][]appregixtry.UploadDetails
	calls     struct {
		catalog  int
		tags     int
		manifest int
		uploads  int
	}
}

type fakeAdminClient struct {
	loginSession AdminSession
	loginErr     error

	users            []ports.AdminUser
	listUsersResults [][]ports.AdminUser
	usersErr         error

	grants    map[string][]ports.AdminRepoGrant
	grantsErr error

	tokens    map[string][]ports.AdminToken
	tokensErr error

	createUser       ports.AdminUser
	createUserErr    error
	resetPasswordErr error

	putGrantErr    error
	lastGrantInput ports.AdminPutRepoGrantInput

	deleteGrantErr        error
	lastDeleteGrantUserID string
	lastDeleteGrantRepo   string

	createToken    ports.AdminCreatedToken
	createTokenErr error

	revokeTokenErr        error
	lastRevokeTokenUserID string
	lastRevokeTokenID     string

	enableUser ports.AdminUser
	enableErr  error

	disableUser ports.AdminUser
	disableErr  error

	loginCalls         int
	listUsersCalls     int
	listGrantsCalls    int
	listTokensCalls    int
	createUserCalls    int
	resetPasswordCalls int
	putGrantCalls      int
	deleteGrantCalls   int
	createTokenCalls   int
	revokeTokenCalls   int
	enableCalls        int
	disableCalls       int
}

func (f *fakeAdminClient) Login(context.Context, string, string) (AdminSession, error) {
	f.loginCalls++
	if f.loginErr != nil {
		return AdminSession{}, f.loginErr
	}
	return f.loginSession, nil
}

func (f *fakeAdminClient) ListUsers(context.Context, AdminSession) ([]ports.AdminUser, error) {
	f.listUsersCalls++
	if f.usersErr != nil {
		return nil, f.usersErr
	}
	if len(f.listUsersResults) > 0 {
		index := f.listUsersCalls - 1
		if index >= len(f.listUsersResults) {
			index = len(f.listUsersResults) - 1
		}
		return append([]ports.AdminUser(nil), f.listUsersResults[index]...), nil
	}
	return append([]ports.AdminUser(nil), f.users...), nil
}

func (f *fakeAdminClient) CreateUser(context.Context, AdminSession, ports.AdminCreateUserInput) (ports.AdminUser, error) {
	f.createUserCalls++
	if f.createUserErr != nil {
		return ports.AdminUser{}, f.createUserErr
	}
	return f.createUser, nil
}

func (f *fakeAdminClient) ResetPassword(context.Context, AdminSession, ports.AdminResetPasswordInput) error {
	f.resetPasswordCalls++
	return f.resetPasswordErr
}

func (f *fakeAdminClient) ListUserGrants(_ context.Context, _ AdminSession, userID string) ([]ports.AdminRepoGrant, error) {
	f.listGrantsCalls++
	if f.grantsErr != nil {
		return nil, f.grantsErr
	}
	return append([]ports.AdminRepoGrant(nil), f.grants[userID]...), nil
}

func (f *fakeAdminClient) PutUserGrant(_ context.Context, _ AdminSession, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error) {
	f.putGrantCalls++
	f.lastGrantInput = input
	if f.putGrantErr != nil {
		return ports.AdminRepoGrant{}, f.putGrantErr
	}
	return ports.AdminRepoGrant{UserID: input.UserID, Repository: regixtrydomain.MustParseRepositoryRef(input.Repository), Role: input.Role}, nil
}

func (f *fakeAdminClient) DeleteUserGrant(_ context.Context, _ AdminSession, userID string, repository string) error {
	f.deleteGrantCalls++
	f.lastDeleteGrantUserID = userID
	f.lastDeleteGrantRepo = repository
	return f.deleteGrantErr
}

func (f *fakeAdminClient) ListUserAdminTokens(_ context.Context, _ AdminSession, userID string) ([]ports.AdminToken, error) {
	f.listTokensCalls++
	if f.tokensErr != nil {
		return nil, f.tokensErr
	}
	return append([]ports.AdminToken(nil), f.tokens[userID]...), nil
}

func (f *fakeAdminClient) CreateUserAdminToken(_ context.Context, _ AdminSession, _ ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	f.createTokenCalls++
	if f.createTokenErr != nil {
		return ports.AdminCreatedToken{}, f.createTokenErr
	}
	return f.createToken, nil
}

func (f *fakeAdminClient) RevokeUserAdminToken(_ context.Context, _ AdminSession, userID string, accessor string) error {
	f.revokeTokenCalls++
	f.lastRevokeTokenUserID = userID
	f.lastRevokeTokenID = accessor
	return f.revokeTokenErr
}

func (f *fakeAdminClient) EnableUser(_ context.Context, _ AdminSession, _ string) (ports.AdminUser, error) {
	f.enableCalls++
	if f.enableErr != nil {
		return ports.AdminUser{}, f.enableErr
	}
	return f.enableUser, nil
}

func (f *fakeAdminClient) DisableUser(_ context.Context, _ AdminSession, _ string) (ports.AdminUser, error) {
	f.disableCalls++
	if f.disableErr != nil {
		return ports.AdminUser{}, f.disableErr
	}
	return f.disableUser, nil
}

func (f *fakeQueryService) Catalog(context.Context, int, string) (appregixtry.CatalogResult, error) {
	f.calls.catalog++
	return f.catalog, nil
}

func (f *fakeQueryService) Tags(_ context.Context, repository string, _ int, _ string) (appregixtry.TagsResult, error) {
	f.calls.tags++
	if result, ok := f.tags[repository]; ok {
		return result, nil
	}
	return appregixtry.TagsResult{Name: repository}, nil
}

func (f *fakeQueryService) ResolveManifest(_ context.Context, repository string, reference string) (appregixtry.ManifestDetails, error) {
	f.calls.manifest++
	if result, ok := f.manifests[fmt.Sprintf("%s:%s", repository, reference)]; ok {
		return result, nil
	}
	return appregixtry.ManifestDetails{}, nil
}

func (f *fakeQueryService) Uploads(_ context.Context, repository string) ([]appregixtry.UploadDetails, error) {
	f.calls.uploads++
	return append([]appregixtry.UploadDetails(nil), f.uploads[repository]...), nil
}

func runCmd(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := cmd()
	updated, nextCmd := model.Update(msg)
	result := updated.(Model)
	if nextCmd != nil {
		return runCmd(t, result, nextCmd)
	}
	return result
}

func newAdminReadyModel(t *testing.T, adminClient AdminClient) Model {
	t.Helper()
	model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}, WithAdminClient(adminClient))
	return runCmd(t, model, model.Init())
}

func runAdminLogin(t *testing.T, model Model, username string, password string) Model {
	t.Helper()
	updated := runKey(t, model, "tab")
	updated = runKey(t, updated, username)
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, password)
	return runKey(t, updated, "enter")
}

func runKey(t *testing.T, model Model, key string) Model {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	}
	updated, cmd := model.Update(msg)
	result := updated.(Model)
	if cmd != nil {
		return runCmd(t, result, cmd)
	}
	return result
}

package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appregistry "registry/internal/app/registry"
	"registry/internal/domain/auth"
	"registry/internal/domain/registry"
	"registry/internal/ports"
)

func TestModelShowsEmptyStateWhenCatalogIsEmpty(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated := runCmd(t, model, model.Init())

	if updated.screen != screenEmpty {
		t.Fatalf("screen = %q, want %q", updated.screen, screenEmpty)
	}

	view := updated.View()
	if !strings.Contains(view, "Registry is empty") {
		t.Fatalf("view = %q, want empty-state title", view)
	}
	if !strings.Contains(view, "No repositories have been published yet.") {
		t.Fatalf("view = %q, want empty-state message", view)
	}
}

func TestModelNavigatesRepositoriesManifestBlobsAndUploads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 28, 23, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		catalog: appregistry.CatalogResult{Repositories: []string{"library/alpine"}},
		tags: map[string]appregistry.TagsResult{
			"library/alpine": {Name: "library/alpine", Tags: []string{"latest"}},
		},
		manifests: map[string]appregistry.ManifestDetails{
			"library/alpine:latest": {
				Repository: "library/alpine",
				Reference:  "latest",
				MediaType:  "application/vnd.oci.image.manifest.v1+json",
				Digest:     "sha256:manifest",
				Size:       512,
				Blobs: []appregistry.BlobDetails{{
					Repository: "library/alpine",
					MediaType:  "application/vnd.oci.image.layer.v1.tar",
					Digest:     "sha256:layer",
					Size:       128,
				}},
			},
		},
		uploads: map[string][]appregistry.UploadDetails{
			"library/alpine": {{Repository: "library/alpine", ID: "upload-1", Status: "active", Size: 42, StartedAt: now, UpdatedAt: now, Location: "/tmp/upload-1"}},
		},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	if updated.screen != screenRepositories {
		t.Fatalf("screen = %q, want %q", updated.screen, screenRepositories)
	}

	updated = runKey(t, updated, "enter")
	if updated.screen != screenTags {
		t.Fatalf("screen = %q, want %q", updated.screen, screenTags)
	}

	updated = runKey(t, updated, "enter")
	if updated.screen != screenManifest {
		t.Fatalf("screen = %q, want %q", updated.screen, screenManifest)
	}

	view := updated.View()
	if !strings.Contains(view, "Manifest · library/alpine:latest") {
		t.Fatalf("view = %q, want manifest header", view)
	}

	updated = runKey(t, updated, "b")
	if updated.screen != screenBlobs {
		t.Fatalf("screen = %q, want %q", updated.screen, screenBlobs)
	}
	if !strings.Contains(updated.View(), "sha256:layer") {
		t.Fatalf("view = %q, want blob digest", updated.View())
	}

	updated = runKey(t, updated, "esc")
	updated = runKey(t, updated, "u")
	if updated.screen != screenUploads {
		t.Fatalf("screen = %q, want %q", updated.screen, screenUploads)
	}
	if !strings.Contains(updated.View(), "upload-1") {
		t.Fatalf("view = %q, want upload id", updated.View())
	}

	if service.calls.catalog != 1 || service.calls.tags != 1 || service.calls.manifest != 1 || service.calls.uploads != 1 {
		t.Fatalf("service calls = %#v, want one query per screen load", service.calls)
	}
}

func TestModelShowsUnavailableMutationNotice(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{
		catalog:   appregistry.CatalogResult{Repositories: []string{"library/alpine"}},
		tags:      map[string]appregistry.TagsResult{"library/alpine": {Name: "library/alpine", Tags: []string{"latest"}}},
		manifests: map[string]appregistry.ManifestDetails{"library/alpine:latest": {Repository: "library/alpine", Reference: "latest", MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: "sha256:manifest", Size: 512}},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "d")

	if !updated.showMutationNotice {
		t.Fatal("expected unavailable mutation notice to be visible")
	}
	if !strings.Contains(updated.View(), "Unavailable in v1") {
		t.Fatalf("view = %q, want unavailable notice", updated.View())
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
	if strings.Contains(updated.View(), "Users") {
		t.Fatalf("view = %q, want admin users to stay blocked", updated.View())
	}
}

func TestModelSuccessfulLoginOpensAdminUsers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{
			Username:    "operator",
			BearerToken: "bearer-token",
			ExpiresAt:   now.Add(2 * time.Minute),
		},
		users: []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }

	updated := runAdminLogin(t, model, "operator", "secret-pass")

	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
	if updated.adminAuth != adminAuthStateAuthenticated {
		t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateAuthenticated)
	}
	if updated.adminSession.BearerToken != "bearer-token" {
		t.Fatalf("BearerToken = %q, want %q", updated.adminSession.BearerToken, "bearer-token")
	}
	if updated.adminLogin.Password != "" {
		t.Fatalf("Password = %q, want cleared after login", updated.adminLogin.Password)
	}
	if adminClient.loginCalls != 1 || adminClient.listUsersCalls != 1 {
		t.Fatalf("admin client calls = %#v, want one login and one user load", adminClient)
	}

	view := updated.View()
	if !strings.Contains(view, "Admin · operator") {
		t.Fatalf("view = %q, want authenticated admin header", view)
	}
	if !strings.Contains(view, "> alice [admin, enabled]") {
		t.Fatalf("view = %q, want loaded admin user", view)
	}
	if strings.Contains(view, "secret-pass") {
		t.Fatalf("view = %q, password leaked in UI", view)
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
	if updated.adminAuth != adminAuthStateUnauthenticated {
		t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateUnauthenticated)
	}
	if updated.adminSession.IsAuthenticated() {
		t.Fatalf("session = %#v, want unauthenticated session", updated.adminSession)
	}
	if updated.adminLogin.Password != "" {
		t.Fatalf("Password = %q, want cleared after failed login", updated.adminLogin.Password)
	}
	if got, want := updated.status, "login: invalid credentials"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if !strings.Contains(updated.View(), "login: invalid credentials") {
		t.Fatalf("view = %q, want login error", updated.View())
	}
}

func TestModelReadOnlyAdminBrowsing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 23, 10, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		grants: map[string][]ports.AdminRepoGrant{
			"u-1": {{UserID: "u-1", Repository: registry.MustParseRepositoryRef("library/alpine"), Role: auth.RepoRoleWriter}},
		},
		tokens: map[string][]ports.AdminToken{
			"u-1": {{ID: "tok-1", UserID: "u-1", Kind: auth.TokenKindAdminCredential, Accessor: "tok_abc", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }

	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")

	if updated.screen != screenAdminGrants {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminGrants)
	}
	grantsView := updated.View()
	if !strings.Contains(grantsView, "Repository Grants · alice") || !strings.Contains(grantsView, "- library/alpine · repo-writer") {
		t.Fatalf("view = %q, want read-only grants data", grantsView)
	}
	assertNoAdminMutations(t, grantsView)

	updated = runKey(t, updated, "t")
	if updated.screen != screenAdminTokens {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminTokens)
	}
	tokensView := updated.View()
	if !strings.Contains(tokensView, "Admin Tokens · alice") || !strings.Contains(tokensView, "tok_abc") {
		t.Fatalf("view = %q, want read-only admin tokens", tokensView)
	}
	assertNoAdminMutations(t, tokensView)

	if adminClient.listGrantsCalls != 1 || adminClient.listTokensCalls != 1 {
		t.Fatalf("admin client calls = %#v, want one grants load and one token load", adminClient)
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

	if updated.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminLogin)
	}
	if updated.adminAuth != adminAuthStateExpired {
		t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateExpired)
	}
	if updated.adminSession.IsAuthenticated() {
		t.Fatalf("session = %#v, want expired unauthenticated session", updated.adminSession)
	}
	if updated.adminSession.ExpiredReason != AdminSessionExpiredReasonExpired {
		t.Fatalf("ExpiredReason = %q, want %q", updated.adminSession.ExpiredReason, AdminSessionExpiredReasonExpired)
	}
	if len(updated.adminView.Users) != 0 || len(updated.adminView.Grants) != 0 || len(updated.adminView.AdminTokens) != 0 {
		t.Fatalf("adminView = %#v, want cleared view state", updated.adminView)
	}
	if !strings.Contains(updated.View(), AdminSessionExpiredReasonExpired) {
		t.Fatalf("view = %q, want expiry-specific relogin message", updated.View())
	}
}

func TestModelLogoutReturnsToLoginAndClearsAdminSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 23, 25, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }

	authenticated := runAdminLogin(t, model, "operator", "secret-pass")
	loggedOut := runKey(t, authenticated, "l")

	if loggedOut.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want %q", loggedOut.screen, screenAdminLogin)
	}
	if loggedOut.adminAuth != adminAuthStateUnauthenticated {
		t.Fatalf("adminAuth = %q, want %q", loggedOut.adminAuth, adminAuthStateUnauthenticated)
	}
	if loggedOut.adminSession.IsAuthenticated() {
		t.Fatalf("session = %#v, want unauthenticated session", loggedOut.adminSession)
	}
	if loggedOut.adminSession.ExpiredReason != "" {
		t.Fatalf("ExpiredReason = %q, want empty after logout", loggedOut.adminSession.ExpiredReason)
	}
	if len(loggedOut.adminView.Users) != 0 || len(loggedOut.adminView.Grants) != 0 || len(loggedOut.adminView.AdminTokens) != 0 {
		t.Fatalf("adminView = %#v, want cleared view state", loggedOut.adminView)
	}
	if got, want := loggedOut.status, "Logged out."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if !strings.Contains(loggedOut.View(), "Admin Login") {
		t.Fatalf("view = %q, want login screen after logout", loggedOut.View())
	}

	reopened := runKey(t, runKey(t, loggedOut, "esc"), "tab")
	if reopened.screen != screenAdminLogin {
		t.Fatalf("screen after reopening admin = %q, want %q", reopened.screen, screenAdminLogin)
	}
	if reopened.adminSession.IsAuthenticated() {
		t.Fatalf("session after reopening admin = %#v, want fresh login required", reopened.adminSession)
	}
	if adminClient.loginCalls != 1 || adminClient.listUsersCalls != 1 {
		t.Fatalf("admin client calls = %#v, want no extra admin access after logout", adminClient)
	}
}

type fakeQueryService struct {
	catalog   appregistry.CatalogResult
	tags      map[string]appregistry.TagsResult
	manifests map[string]appregistry.ManifestDetails
	uploads   map[string][]appregistry.UploadDetails
	calls     struct {
		catalog  int
		tags     int
		manifest int
		uploads  int
	}
}

type fakeAdminClient struct {
	loginSession    AdminSession
	loginErr        error
	users           []ports.AdminUser
	usersErr        error
	grants          map[string][]ports.AdminRepoGrant
	grantsErr       error
	tokens          map[string][]ports.AdminToken
	tokensErr       error
	loginCalls      int
	listUsersCalls  int
	listGrantsCalls int
	listTokensCalls int
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
	return append([]ports.AdminUser(nil), f.users...), nil
}

func (f *fakeAdminClient) ListUserGrants(_ context.Context, _ AdminSession, userID string) ([]ports.AdminRepoGrant, error) {
	f.listGrantsCalls++
	if f.grantsErr != nil {
		return nil, f.grantsErr
	}
	return append([]ports.AdminRepoGrant(nil), f.grants[userID]...), nil
}

func (f *fakeAdminClient) ListUserAdminTokens(_ context.Context, _ AdminSession, userID string) ([]ports.AdminToken, error) {
	f.listTokensCalls++
	if f.tokensErr != nil {
		return nil, f.tokensErr
	}
	return append([]ports.AdminToken(nil), f.tokens[userID]...), nil
}

func (f *fakeQueryService) Catalog(context.Context, int, string) (appregistry.CatalogResult, error) {
	f.calls.catalog++
	return f.catalog, nil
}

func (f *fakeQueryService) Tags(_ context.Context, repository string, _ int, _ string) (appregistry.TagsResult, error) {
	f.calls.tags++
	if result, ok := f.tags[repository]; ok {
		return result, nil
	}
	return appregistry.TagsResult{Name: repository}, nil
}

func (f *fakeQueryService) ResolveManifest(_ context.Context, repository string, reference string) (appregistry.ManifestDetails, error) {
	f.calls.manifest++
	if result, ok := f.manifests[fmt.Sprintf("%s:%s", repository, reference)]; ok {
		return result, nil
	}
	return appregistry.ManifestDetails{}, nil
}

func (f *fakeQueryService) Uploads(_ context.Context, repository string) ([]appregistry.UploadDetails, error) {
	f.calls.uploads++
	return append([]appregistry.UploadDetails(nil), f.uploads[repository]...), nil
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
	model := NewModel(&fakeQueryService{catalog: appregistry.CatalogResult{Repositories: []string{"library/alpine"}}}, WithAdminClient(adminClient))
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

func assertNoAdminMutations(t *testing.T, view string) {
	t.Helper()
	for _, forbidden := range []string{"create", "revoke", "enable", "disable", "reset"} {
		if strings.Contains(strings.ToLower(view), forbidden) {
			t.Fatalf("view = %q, want read-only admin browsing without %q action", view, forbidden)
		}
	}
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
	}
	updated, cmd := model.Update(msg)
	result := updated.(Model)
	if cmd != nil {
		return runCmd(t, result, cmd)
	}
	return result
}

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
	"regixtry/internal/domain/auth"
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
	if !strings.Contains(view, "No repositories have been published yet.") {
		t.Fatalf("view = %q, want empty-state message", view)
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

func TestModelMovesRepositorySelectionWithRuneAndArrowKeys(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{
		catalog: appregixtry.CatalogResult{Repositories: []string{"alpine", "team/demo"}},
		tags: map[string]appregixtry.TagsResult{
			"team/demo": {Name: "team/demo", Tags: []string{"latest"}},
		},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	if updated.screen != screenRepositories {
		t.Fatalf("screen = %q, want %q", updated.screen, screenRepositories)
	}

	updated = runKey(t, updated, "j")
	if updated.repositories.Selected != 1 {
		t.Fatalf("selected = %d, want 1 after j", updated.repositories.Selected)
	}

	updated = runKey(t, updated, "enter")
	if updated.screen != screenTags {
		t.Fatalf("screen = %q, want %q", updated.screen, screenTags)
	}
	if updated.tags.Repository != "team/demo" {
		t.Fatalf("repository = %q, want team/demo", updated.tags.Repository)
	}

	updated.screen = screenRepositories
	updated = runKey(t, updated, "up")
	if updated.repositories.Selected != 0 {
		t.Fatalf("selected = %d, want 0 after up", updated.repositories.Selected)
	}
}

func TestNavigationKeyHelpersRecognizeRunesAndArrows(t *testing.T) {
	t.Parallel()

	if !isMoveDownKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) {
		t.Fatal("expected j rune to be recognized as move-down")
	}
	if !isMoveUpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}) {
		t.Fatal("expected k rune to be recognized as move-up")
	}
	if !isMoveDownKey(tea.KeyMsg{Type: tea.KeyDown}) {
		t.Fatal("expected KeyDown to be recognized as move-down")
	}
	if !isMoveUpKey(tea.KeyMsg{Type: tea.KeyUp}) {
		t.Fatal("expected KeyUp to be recognized as move-up")
	}
}

func TestModelShowsUnavailableMutationNotice(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{
		catalog:   appregixtry.CatalogResult{Repositories: []string{"library/alpine"}},
		tags:      map[string]appregixtry.TagsResult{"library/alpine": {Name: "library/alpine", Tags: []string{"latest"}}},
		manifests: map[string]appregixtry.ManifestDetails{"library/alpine:latest": {Repository: "library/alpine", Reference: "latest", MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: "sha256:manifest", Size: 512}},
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

func TestModelMovesAdminUserSelectionWithRuneAndArrowKeys(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 23, 5, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{
			Username:    "operator",
			BearerToken: "bearer-token",
			ExpiresAt:   now.Add(2 * time.Minute),
		},
		users: []ports.AdminUser{
			{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true},
			{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: true},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }

	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "j")
	if updated.adminView.SelectedUser != 1 {
		t.Fatalf("selectedUser = %d, want 1 after j", updated.adminView.SelectedUser)
	}
	if updated.adminView.SelectedUsername != "bob" {
		t.Fatalf("selectedUsername = %q, want bob", updated.adminView.SelectedUsername)
	}

	updated = runKey(t, updated, "up")
	if updated.adminView.SelectedUser != 0 {
		t.Fatalf("selectedUser = %d, want 0 after up", updated.adminView.SelectedUser)
	}
	if updated.adminView.SelectedUsername != "alice" {
		t.Fatalf("selectedUsername = %q, want alice", updated.adminView.SelectedUsername)
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
			"u-1": {{UserID: "u-1", Repository: regixtrydomain.MustParseRepositoryRef("library/alpine"), Role: auth.RepoRoleWriter}},
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

func TestModelDisableUserSuccessRefreshesUsersAndPreservesSelectionByID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 22, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		listUsersResults: [][]ports.AdminUser{
			{
				{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true},
				{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: true},
			},
			{
				{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: false},
				{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true},
			},
		},
		disableUser: ports.AdminUser{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: false},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }

	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated.adminView.SelectedUser = 1
	updated.adminView.SelectedUserID = "u-2"
	updated.adminView.SelectedUsername = "bob"
	updated = runKey(t, updated, "d")

	confirmationView := updated.View()
	if !strings.Contains(confirmationView, "Confirm disable user \"bob\"?") {
		t.Fatalf("view = %q, want disable confirmation", confirmationView)
	}

	updated = runKey(t, updated, "enter")

	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
	if got, want := updated.status, `User "bob" disabled.`; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := updated.adminView.SelectedUserID, "u-2"; got != want {
		t.Fatalf("SelectedUserID = %q, want %q", got, want)
	}
	if got, want := updated.adminView.SelectedUser, 0; got != want {
		t.Fatalf("SelectedUser = %d, want %d after refresh reordered users", got, want)
	}
	if adminClient.disableCalls != 1 || adminClient.listUsersCalls != 2 {
		t.Fatalf("admin client calls = %#v, want one disable and two user loads", adminClient)
	}
	if got, want := adminClient.lastDisableUserID, "u-2"; got != want {
		t.Fatalf("lastDisableUserID = %q, want %q", got, want)
	}

	view := updated.View()
	if !strings.Contains(view, "> bob [user, disabled]") {
		t.Fatalf("view = %q, want refreshed disabled user selection", view)
	}
	if !strings.Contains(view, `User "bob" disabled.`) {
		t.Fatalf("view = %q, want success status", view)
	}
	if strings.Contains(view, "Confirm disable") {
		t.Fatalf("view = %q, want confirmation cleared after success", view)
	}
	if !strings.Contains(view, "e: enable") {
		t.Fatalf("view = %q, want enable action hint for disabled user", view)
	}
	if strings.Contains(view, "d: disable") {
		t.Fatalf("view = %q, want action hints to reflect refreshed user state", view)
	}
	if strings.Contains(view, "Submitting disable") {
		t.Fatalf("view = %q, want in-flight copy cleared after completion", view)
	}
	if updated.adminMutation.Active() {
		t.Fatalf("adminMutation = %#v, want cleared after success", updated.adminMutation)
	}
}

func TestModelAdminMutationCancelDismissSendsNoRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "esc", key: "esc"},
		{name: "n", key: "n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adminClient := &fakeAdminClient{
				loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 5, 0, 0, time.UTC)},
				users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
			}
			model := newAdminReadyModel(t, adminClient)
			updated := runAdminLogin(t, model, "operator", "secret-pass")
			updated = runKey(t, updated, "d")

			if !strings.Contains(updated.View(), "Confirm disable user \"alice\"?") {
				t.Fatalf("view = %q, want confirmation before cancel", updated.View())
			}

			updated = runKey(t, updated, tc.key)

			if updated.screen != screenAdminUsers {
				t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
			}
			if adminClient.disableCalls != 0 || adminClient.enableCalls != 0 {
				t.Fatalf("admin client calls = %#v, want no mutation request", adminClient)
			}
			view := updated.View()
			if strings.Contains(view, "Confirm disable") {
				t.Fatalf("view = %q, want confirmation dismissed", view)
			}
			if !strings.Contains(view, "d: disable") {
				t.Fatalf("view = %q, want normal users-screen action hints restored", view)
			}
			if updated.adminMutation.Active() {
				t.Fatalf("adminMutation = %#v, want cleared after cancel", updated.adminMutation)
			}
		})
	}
}

func TestModelAdminMutationInFlightBlocksRepeatSubmission(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 7, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		disableUser:  ports.AdminUser{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: false},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "d")

	midflightModel, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	midflight := midflightModel.(Model)
	if cmd == nil {
		t.Fatal("expected mutation command after confirmation")
	}
	if !midflight.adminMutation.InFlight {
		t.Fatalf("adminMutation = %#v, want in-flight state before command completion", midflight.adminMutation)
	}
	if got, want := midflight.status, "Submitting disable for alice..."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if !strings.Contains(midflight.View(), "Please wait until the current mutation completes.") {
		t.Fatalf("view = %q, want in-flight guidance", midflight.View())
	}

	blockedModel, blockedCmd := midflight.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	blocked := blockedModel.(Model)
	if blockedCmd != nil {
		t.Fatal("expected no second mutation command while first request is in flight")
	}
	if blocked.status != midflight.status {
		t.Fatalf("status = %q, want in-flight status unchanged", blocked.status)
	}
	if adminClient.disableCalls != 0 {
		t.Fatalf("disableCalls = %d before executing command, want 0", adminClient.disableCalls)
	}

	completed := runCmd(t, midflight, cmd)
	if adminClient.disableCalls != 1 {
		t.Fatalf("disableCalls = %d, want 1 after command execution", adminClient.disableCalls)
	}
	if got, want := completed.status, `User "alice" disabled.`; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestModelAdminMutationRecoverableFailuresStayOnUsersScreen(t *testing.T) {
	t.Parallel()

	baseSession := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 10, 0, 0, time.UTC)}
	for _, tc := range []struct {
		name       string
		openKey    string
		wantStatus string
		client     *fakeAdminClient
		wantCalls  func(*fakeAdminClient) int
	}{
		{
			name:       "conflict",
			openKey:    "d",
			wantStatus: "mutate admin resource: cannot disable the last active admin",
			client: &fakeAdminClient{
				loginSession: baseSession,
				users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
				disableErr:   errors.New("mutate admin resource: cannot disable the last active admin"),
			},
			wantCalls: func(client *fakeAdminClient) int { return client.disableCalls },
		},
		{
			name:       "validation",
			openKey:    "e",
			wantStatus: "mutate admin resource: user id is required",
			client: &fakeAdminClient{
				loginSession: baseSession,
				users:        []ports.AdminUser{{ID: "u-2", Username: "bob", IsAdmin: false, Enabled: false}},
				enableErr:    errors.New("mutate admin resource: user id is required"),
			},
			wantCalls: func(client *fakeAdminClient) int { return client.enableCalls },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newAdminReadyModel(t, tc.client)
			updated := runAdminLogin(t, model, "operator", "secret-pass")
			updated = runKey(t, updated, tc.openKey)
			updated = runKey(t, updated, "enter")

			if updated.screen != screenAdminUsers {
				t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
			}
			if updated.adminAuth != adminAuthStateAuthenticated {
				t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateAuthenticated)
			}
			if got, want := updated.status, tc.wantStatus; got != want {
				t.Fatalf("status = %q, want %q", got, want)
			}
			if tc.wantCalls(tc.client) != 1 {
				t.Fatalf("admin client calls = %#v, want exactly one mutation request", tc.client)
			}
			if tc.client.listUsersCalls != 1 {
				t.Fatalf("listUsersCalls = %d, want only initial login load without refresh", tc.client.listUsersCalls)
			}
			view := updated.View()
			if !strings.Contains(view, tc.wantStatus) {
				t.Fatalf("view = %q, want recoverable backend message", view)
			}
			if strings.Contains(view, "Confirm ") {
				t.Fatalf("view = %q, want confirmation cleared after failure", view)
			}
			if updated.adminMutation.Active() {
				t.Fatalf("adminMutation = %#v, want cleared after failure", updated.adminMutation)
			}
		})
	}
}

func TestModelAdminMutationExpiredSessionReturnsToLogin(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 15, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		disableErr:   NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired),
	}
	model := newAdminReadyModel(t, adminClient)

	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "d")
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
	if got, want := updated.adminSession.ExpiredReason, AdminSessionExpiredReasonExpired; got != want {
		t.Fatalf("ExpiredReason = %q, want %q", got, want)
	}
	if got, want := updated.status, AdminSessionExpiredReasonExpired; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if adminClient.disableCalls != 1 || adminClient.listUsersCalls != 1 {
		t.Fatalf("admin client calls = %#v, want one failed disable and no refresh", adminClient)
	}
	if updated.adminMutation.Active() {
		t.Fatalf("adminMutation = %#v, want cleared after expiry", updated.adminMutation)
	}
	if !strings.Contains(updated.View(), AdminSessionExpiredReasonExpired) {
		t.Fatalf("view = %q, want expiry-specific relogin message", updated.View())
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
	loginSession      AdminSession
	loginErr          error
	users             []ports.AdminUser
	listUsersResults  [][]ports.AdminUser
	usersErr          error
	grants            map[string][]ports.AdminRepoGrant
	grantsErr         error
	tokens            map[string][]ports.AdminToken
	tokensErr         error
	enableUser        ports.AdminUser
	enableErr         error
	disableUser       ports.AdminUser
	disableErr        error
	loginCalls        int
	listUsersCalls    int
	listGrantsCalls   int
	listTokensCalls   int
	enableCalls       int
	disableCalls      int
	lastEnableUserID  string
	lastDisableUserID string
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

func (f *fakeAdminClient) EnableUser(_ context.Context, _ AdminSession, userID string) (ports.AdminUser, error) {
	f.enableCalls++
	f.lastEnableUserID = userID
	if f.enableErr != nil {
		return ports.AdminUser{}, f.enableErr
	}
	if strings.TrimSpace(f.enableUser.ID) != "" {
		return f.enableUser, nil
	}
	return ports.AdminUser{ID: userID}, nil
}

func (f *fakeAdminClient) DisableUser(_ context.Context, _ AdminSession, userID string) (ports.AdminUser, error) {
	f.disableCalls++
	f.lastDisableUserID = userID
	if f.disableErr != nil {
		return ports.AdminUser{}, f.disableErr
	}
	if strings.TrimSpace(f.disableUser.ID) != "" {
		return f.disableUser, nil
	}
	return ports.AdminUser{ID: userID}, nil
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

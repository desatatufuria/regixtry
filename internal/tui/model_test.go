package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	bubbletable "github.com/evertras/bubble-table/table"
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

func TestModelStartupLoginDefersCatalogUntilLoginSucceeds(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}
	model := NewModel(service, WithAdminClient(&fakeAdminClient{loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2099, time.August, 9, 12, 0, 0, 0, time.UTC)}}), WithStartupLogin())

	model = runCmd(t, model, model.Init())
	if got, want := service.calls.catalog, 0; got != want {
		t.Fatalf("catalog calls = %d, want %d before login", got, want)
	}
	if got, want := model.screen, screenAdminLogin; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if !strings.Contains(model.View(), "Operator Login") {
		t.Fatalf("view = %q, want login shell", model.View())
	}

	updated := runKey(t, model, "operator")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "secret-pass")
	updated = runKey(t, updated, "enter")

	if got, want := service.calls.catalog, 1; got != want {
		t.Fatalf("catalog calls = %d, want %d after login", got, want)
	}
	if got, want := updated.screen, screenRepositories; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if strings.Contains(updated.View(), "alice [admin, enabled]") {
		t.Fatalf("view = %q, want repository flow instead of admin users", updated.View())
	}
	if !strings.Contains(updated.View(), "Repositories") || !strings.Contains(updated.View(), "library/alpine") {
		t.Fatalf("view = %q, want repositories shell after login", updated.View())
	}

	updated = runKey(t, updated, "tab")
	if got, want := updated.screen, screenAdminUsers; got != want {
		t.Fatalf("screen = %q, want %q after opening admin", got, want)
	}
}

func TestModelLocalStartupLoadsCatalogImmediately(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}
	model := NewModel(service, WithAdminClient(&fakeAdminClient{}))
	updated := runCmd(t, model, model.Init())

	if got, want := service.calls.catalog, 1; got != want {
		t.Fatalf("catalog calls = %d, want %d", got, want)
	}
	if got, want := updated.screen, screenRepositories; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	view := updated.View()
	if !strings.Contains(view, "Regixtry Console") || !strings.Contains(view, "Enter: open tags | Tab: admin | q: quit") {
		t.Fatalf("view = %q, want unified repository shell", view)
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
	if !strings.Contains(updated.View(), "Operator Login") {
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

func TestModelFeatureViewRendersGenericPageAndAllowsDeclaredAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}, {Label: "Configured", Value: "true"}},
			Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}},
		},
		actionResults: map[string]ports.FeatureActionResult{"disable": {Message: `Feature "trivy" disabled.`}},
		pageAfterAction: ports.FeaturePage{
			Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: false, Configured: true},
			Header:   []ports.FeatureField{{Label: "Enabled", Value: "false"}, {Label: "Configured", Value: "true"}},
			Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}}}},
			Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: `Confirm enable feature "trivy"?`}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "f")

	if updated.screen != screenAdminFeatures {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminFeatures)
	}
	if adminClient.listFeaturesCalls != 1 || adminClient.getFeaturePageCalls != 1 {
		t.Fatalf("feature client calls = %#v, want one feature list + one page read", adminClient)
	}
	view := updated.View()
	for _, want := range []string{"Built-in Features", "trivy", "Configuration", "Runtime", "Version: 0.57.1", "x: disable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}

	updated = runKey(t, updated, "x")
	if !strings.Contains(updated.View(), `Confirm disable feature "trivy"?`) {
		t.Fatalf("view = %q, want feature disable confirmation", updated.View())
	}

	updated = runKey(t, updated, "enter")
	if adminClient.executeFeatureActionCalls != 1 || adminClient.lastFeatureAction != "disable" {
		t.Fatalf("feature action calls = %#v, want one disable action", adminClient)
	}
	if updated.screen != screenAdminFeatures {
		t.Fatalf("screen = %q, want %q after disable", updated.screen, screenAdminFeatures)
	}
	if !strings.Contains(updated.View(), `Feature "trivy" disabled.`) || !strings.Contains(updated.View(), `Enabled: false`) {
		t.Fatalf("view = %q, want disabled feature status", updated.View())
	}
}

func TestModelFeatureViewKeepsMinimalPagesUsable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "f")

	view := updated.View()
	for _, want := range []string{"future-plugin", "Enabled: true", "No additional feature details.", "Enter/r: refresh page"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	if strings.Contains(view, "x: disable") || strings.Contains(view, "e: enable") {
		t.Fatalf("view = %q, want no undeclared actions in help", view)
	}
}

func TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, {Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: false, Configured: true}},
		featurePages: map[string]ports.FeaturePage{
			"trivy":         {Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}, Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}}},
			"future-plugin": {Summary: ports.FeatureSummary{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: false, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "false"}}, Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: `Confirm enable feature "future-plugin"?`}}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "down")

	if adminClient.getFeaturePageCalls != 2 {
		t.Fatalf("getFeaturePageCalls = %d, want page refresh on selection change", adminClient.getFeaturePageCalls)
	}
	view := updated.View()
	for _, want := range []string{"future-plugin", "e: enable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	if strings.Contains(view, "x: disable") {
		t.Fatalf("view = %q, want backend-authoritative action help for selected feature", view)
	}
}

func TestModelTrivyFeatureDefaultsToRuntimeTabAndKeepsNonTrivyUntabbed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	t.Run("trivy defaults to runtime tab", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage: ports.FeaturePage{
				Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
				Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")

		view := updated.View()
		for _, want := range []string{"Runtime", "Repository Alerts", "Configuration", "Version: 0.57.1"} {
			if !strings.Contains(view, want) {
				t.Fatalf("view = %q, want %q", view, want)
			}
		}
		if strings.Contains(view, "No repository alerts") {
			t.Fatalf("view = %q, want runtime tab as default landing content", view)
		}
	})

	t.Run("non-trivy keeps generic shell without tabs", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: true, Configured: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")

		view := updated.View()
		if strings.Contains(view, "Repository Alerts") || strings.Contains(view, "Tabs") {
			t.Fatalf("view = %q, want no trivy-only tab chrome", view)
		}
	})
}

func TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
			Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")

	updated = runKey(t, updated, "c")
	modalView := updated.View()
	for _, want := range []string{"Edit Trivy Configuration", "Schedule Enabled", "Interval", "Timeout", "Registry Reachable URL", "Max Concurrency"} {
		if !strings.Contains(modalView, want) {
			t.Fatalf("view = %q, want %q", modalView, want)
		}
	}
	for _, hidden := range []string{"Auth Token", "Cache Dir", "Binary Path", "TLS CA Cert Path"} {
		if strings.Contains(modalView, hidden) {
			t.Fatalf("view = %q, want unsupported field %q hidden", modalView, hidden)
		}
	}

	canceled := runKey(t, updated, "esc")
	if adminClient.configureFeatureCalls != 0 {
		t.Fatalf("configureFeatureCalls = %d, want cancel to keep modal client-idle", adminClient.configureFeatureCalls)
	}
	if strings.Contains(canceled.View(), "Edit Trivy Configuration") {
		t.Fatalf("view = %q, want modal closed after esc", canceled.View())
	}

	submitted := runKey(t, updated, "enter")
	if adminClient.configureFeatureCalls != 1 {
		t.Fatalf("configureFeatureCalls = %d, want one Trivy config submit", adminClient.configureFeatureCalls)
	}
	if adminClient.lastConfiguredFeature != "trivy" {
		t.Fatalf("lastConfiguredFeature = %q, want trivy", adminClient.lastConfiguredFeature)
	}
	if adminClient.lastConfigureInput.ScheduleEnabled == nil || adminClient.lastConfigureInput.Interval == nil || adminClient.lastConfigureInput.Timeout == nil || adminClient.lastConfigureInput.RegistryReachableURL == nil || adminClient.lastConfigureInput.MaxConcurrency == nil {
		t.Fatalf("lastConfigureInput = %#v, want current trivy settings payload", adminClient.lastConfigureInput)
	}
	if adminClient.lastConfigureInput.AuthToken != nil || adminClient.lastConfigureInput.CacheDir != nil || adminClient.lastConfigureInput.BinaryPath != nil || adminClient.lastConfigureInput.TLSCACertPath != nil || adminClient.lastConfigureInput.ServiceURL != nil {
		t.Fatalf("lastConfigureInput = %#v, want unsupported fields omitted", adminClient.lastConfigureInput)
	}
	if !strings.Contains(submitted.View(), "Configuration saved") {
		t.Fatalf("view = %q, want config feedback after submit", submitted.View())
	}
}

func TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	t.Run("loads scan runs and drills into selected alert", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage: ports.FeaturePage{
				Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			},
			scanRuns: []ports.ScanRun{
				{ID: "run-2", Repository: "team/api", RequestedRef: "1.0.0", Digest: "sha256:222", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, Medium: 0, Low: 0, HasFixable: true},
				{ID: "run-3", Repository: "library/base", RequestedRef: "stable", Digest: "sha256:333", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, Medium: 0, Low: 0, HasFixable: false},
				{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:111", Status: ports.ScanRunStatusCompleted, Critical: 0, High: 2, Medium: 3, Low: 4, HasFixable: true},
			},
			scanRunDetails: map[string]ports.ScanRunDetail{
				"run-2": {
					Run:                ports.ScanRun{ID: "run-2", Repository: "team/api", RequestedRef: "1.0.0", Digest: "sha256:222", Status: ports.ScanRunStatusCompleted, Critical: 1},
					Findings:           []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-0001", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}},
					DBFreshness:        ports.ScanRunDBFreshness{FreshnessState: ports.ScanRunDBFreshnessStateStale},
					ReferenceFreshness: ports.ScanReferenceFreshnessMoved,
				},
				"run-3": {
					Run:                ports.ScanRun{ID: "run-3", Repository: "library/base", RequestedRef: "stable", Digest: "sha256:333", Status: ports.ScanRunStatusCompleted, Critical: 1},
					Findings:           []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-0002", PackageName: "busybox", InstalledVersion: "1.0.0", Fixable: false}},
					DBFreshness:        ports.ScanRunDBFreshness{FreshnessState: ports.ScanRunDBFreshnessStateFresh},
					ReferenceFreshness: ports.ScanReferenceFreshnessCurrent,
				},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "tab")

		if adminClient.listScanRunsCalls != 1 {
			t.Fatalf("listScanRunsCalls = %d, want one repository-alert load", adminClient.listScanRunsCalls)
		}
		alertsView := updated.View()
		for _, want := range []string{"Repository Alerts", "Repository", "Reference", "team/api", "library/base", "library/alpine", "1.0.0", "stable", "latest"} {
			if !strings.Contains(alertsView, want) {
				t.Fatalf("view = %q, want %q", alertsView, want)
			}
		}
		if got, want := updated.adminView.Tables.ScanRuns.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
			t.Fatalf("highlighted scan-run metadata = %#v, want %q", got, want)
		}

		updated = runKey(t, updated, "enter")
		if adminClient.getScanRunDetailCalls != 1 {
			t.Fatalf("getScanRunDetailCalls = %d, want detail fetch on enter", adminClient.getScanRunDetailCalls)
		}
		detailView := updated.View()
		for _, want := range []string{"Selected Scan Run", "Repository: team/api", "Digest: sha256:222", "Reference freshness: moved", "DB freshness: stale", "CVE-2026-0001", "openssl"} {
			if !strings.Contains(detailView, want) {
				t.Fatalf("view = %q, want %q", detailView, want)
			}
		}

		updated = runKey(t, updated, "esc")
		if strings.Contains(updated.View(), "Selected Scan Run") {
			t.Fatalf("view = %q, want esc to close alert detail and return to list", updated.View())
		}
		if got, want := updated.adminView.TrivySelectedAlert, 0; got != want {
			t.Fatalf("TrivySelectedAlert = %d, want %d", got, want)
		}
	})

	t.Run("empty scan runs stay recoverable and runtime tab remains usable", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage: ports.FeaturePage{
				Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "tab")
		if !strings.Contains(updated.View(), "No repository alerts found.") {
			t.Fatalf("view = %q, want clear empty-state message", updated.View())
		}
		updated = runKey(t, updated, "tab")
		if !strings.Contains(updated.View(), "Version: 0.57.1") {
			t.Fatalf("view = %q, want runtime tab still usable after empty alerts", updated.View())
		}
	})
}

func TestModelFeatureTablesRenderAlignedRowsAndPreserveBackendValues(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features: []ports.FeatureSummary{
			{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true, CurrentVersion: "0.57.1", LatestVersion: "0.58.0", UpdateStatus: "available"},
			{Name: "future-plugin", Kind: ports.FeatureKindExternalService, Enabled: false, Configured: false, CurrentVersion: "n/a", LatestVersion: "1.0.0", UpdateStatus: "blocked"},
		},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true, CurrentVersion: "0.57.1", LatestVersion: "0.58.0", UpdateStatus: "available"},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
			Sections: []ports.FeatureSection{{
				ID:    "checks",
				Title: "Runtime Checks",
				Kind:  "rows",
				Rows: []ports.FeatureRow{{
					Title:  "DB freshness",
					Status: "stale",
					Detail: "older than 24h",
				}, {
					Title:  "Registry reachability",
					Status: "ready",
					Detail: "https://registry.internal:5443",
				}},
			}},
		},
	}

	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")

	if got, want := updated.adminView.Tables.Features.TotalRows(), 2; got != want {
		t.Fatalf("feature table rows = %d, want %d", got, want)
	}
	if got, want := updated.adminView.Tables.Features.HighlightedRow().Data[adminTableMetaFeatureName], "trivy"; got != want {
		t.Fatalf("highlighted feature metadata = %#v, want %q", got, want)
	}
	rowsTable, ok := updated.adminView.Tables.FeatureRows["checks"]
	if !ok {
		t.Fatal("expected rows table for checks section")
	}
	if got, want := rowsTable.TotalRows(), 2; got != want {
		t.Fatalf("rows table rows = %d, want %d", got, want)
	}
	view := updated.View()
	for _, want := range []string{"Name", "Kind", "Enabled", "Configured", "Current", "Latest", "Update", "Runtime Checks", "Title", "Status", "Detail", "DB freshness", "older than 24h", "Registry reachability", "https://registry.internal:5443", "0.57.1", "0.58.0", "available"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
}

func TestModelTrivyTablesPreserveEmptyStateAndBackendOrdering(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)
	t.Run("empty state stays explicit", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "tab")

		if got, want := updated.adminView.Tables.ScanRuns.TotalRows(), 0; got != want {
			t.Fatalf("scan-runs table rows = %d, want %d", got, want)
		}
		if !strings.Contains(updated.View(), "No repository alerts found.") {
			t.Fatalf("view = %q, want empty alerts message", updated.View())
		}
	})

	t.Run("scan runs keep sorted backend values in the table", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}},
			scanRuns: []ports.ScanRun{
				{ID: "run-2", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: true},
				{ID: "run-3", Repository: "library/base", RequestedRef: "stable", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: false},
				{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Status: ports.ScanRunStatusCompleted, Critical: 0, High: 2, HasFixable: true},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "tab")

		if got, want := updated.adminView.Tables.ScanRuns.TotalRows(), 3; got != want {
			t.Fatalf("scan-runs table rows = %d, want %d", got, want)
		}
		if got, want := updated.adminView.Tables.ScanRuns.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
			t.Fatalf("highlighted scan-run metadata = %#v, want %q", got, want)
		}
		view := updated.View()
		for _, want := range []string{"Repository", "Reference", "Status", "Critical", "High", "Fixable", "team/api", "library/base", "library/alpine", "1.0.0", "stable", "latest"} {
			if !strings.Contains(view, want) {
				t.Fatalf("view = %q, want %q", view, want)
			}
		}
	})
}

func TestAdminFindingSeverityStylingScopesOnlyVulnerabilityRows(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	findingsTable := buildAdminFindingsTable(theme, []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-0001", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}}, 0)
	severityCell, ok := findingsTable.HighlightedRow().Data[adminTableColumnFindingSeverity].(bubbletable.StyledCell)
	if !ok {
		t.Fatalf("finding severity cell type = %T, want bubble-table styled cell", findingsTable.HighlightedRow().Data[adminTableColumnFindingSeverity])
	}
	if got, want := severityCell.Data, "CRITICAL"; got != want {
		t.Fatalf("finding severity data = %#v, want %q", got, want)
	}

	featuresTable := buildAdminFeaturesTable(theme, []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}}, 0)
	if _, styled := featuresTable.HighlightedRow().Data[adminTableColumnFeatureEnabled].(bubbletable.StyledCell); styled {
		t.Fatal("feature summary cells must stay neutral")
	}

	rowsTable := buildAdminFeatureRowsTable(theme, ports.FeatureSection{ID: "checks", Title: "Checks", Kind: "rows", Rows: []ports.FeatureRow{{Title: "DB freshness", Status: "stale", Detail: "older than 24h"}}})
	if _, styled := rowsTable.HighlightedRow().Data[adminTableColumnRowStatus].(bubbletable.StyledCell); styled {
		t.Fatal("generic row status cells must stay neutral")
	}

	scanRunsTable := buildAdminScanRunsTable(theme, []ports.ScanRun{{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: true}}, 0)
	if _, styled := scanRunsTable.HighlightedRow().Data[adminTableColumnScanRunStatus].(bubbletable.StyledCell); styled {
		t.Fatal("scan-run cells must stay neutral")
	}
}

func TestModelTrivyFindingsMixedSeverityRenderPreservesLabelsAndCounts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 30, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}},
		scanRuns: []ports.ScanRun{{
			ID:           "run-1",
			Repository:   "team/api",
			RequestedRef: "1.0.0",
			Status:       ports.ScanRunStatusCompleted,
			Critical:     1,
			High:         2,
			HasFixable:   true,
		}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run: ports.ScanRun{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted},
				Findings: []ports.ScanRunFinding{
					{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-3000", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true},
					{Severity: "HIGH", VulnerabilityID: "CVE-2026-3001", PackageName: "glibc", InstalledVersion: "2.38", FixedVersion: "2.39", Fixable: true},
					{Severity: "LOW", VulnerabilityID: "CVE-2026-3002", PackageName: "ca-certificates", InstalledVersion: "1.0.0", FixedVersion: "1.0.1", Fixable: false},
					{Severity: "HIGH", VulnerabilityID: "CVE-2026-3003", PackageName: "busybox", InstalledVersion: "1.36.0", FixedVersion: "1.36.1", Fixable: true},
				},
			},
		},
	}

	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "enter")

	if got, want := updated.adminView.Tables.Findings.TotalRows(), 4; got != want {
		t.Fatalf("findings table rows = %d, want %d", got, want)
	}

	rows := updated.adminView.Tables.Findings.GetVisibleRows()
	if got, want := len(rows), 4; got != want {
		t.Fatalf("visible findings rows = %d, want %d", got, want)
	}

	theme := newAdminTheme()
	expectedSeverities := []struct {
		label string
		style lipgloss.Style
	}{
		{label: "CRITICAL", style: theme.severityCritical},
		{label: "HIGH", style: theme.severityHigh},
		{label: "LOW", style: theme.severityLow},
		{label: "HIGH", style: theme.severityHigh},
	}
	for i, want := range expectedSeverities {
		severityCell, ok := rows[i].Data[adminTableColumnFindingSeverity].(bubbletable.StyledCell)
		if !ok {
			t.Fatalf("row %d severity cell type = %T, want bubble-table styled cell", i, rows[i].Data[adminTableColumnFindingSeverity])
		}
		if got := severityCell.Data; got != want.label {
			t.Fatalf("row %d severity label = %#v, want %q", i, got, want.label)
		}
		if got, wantRendered := severityCell.Style.Render(want.label), want.style.Render(want.label); got != wantRendered {
			t.Fatalf("row %d severity style render = %q, want %q", i, got, wantRendered)
		}
	}

	view := updated.View()
	for label, want := range map[string]int{"CRITICAL": 1, "HIGH": 2, "LOW": 1} {
		if got := strings.Count(view, label); got != want {
			t.Fatalf("view %q count = %d, want %d", label, got, want)
		}
	}
	for _, want := range []string{"Findings", "CVE-2026-3000", "CVE-2026-3001", "CVE-2026-3002", "CVE-2026-3003", "openssl", "glibc", "ca-certificates", "busybox"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
}

func TestModelAdminFeatureTablesKeepScreenShortcutsAuthoritative(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
			Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}},
			Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}},
		},
		scanRuns: []ports.ScanRun{
			{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: true},
			{ID: "run-2", Repository: "library/base", RequestedRef: "stable", Status: ports.ScanRunStatusCompleted, Critical: 0, High: 2, HasFixable: false},
		},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-2": {Run: ports.ScanRun{ID: "run-2", Repository: "library/base", RequestedRef: "stable", Status: ports.ScanRunStatusCompleted}, Findings: []ports.ScanRunFinding{{Severity: "HIGH", VulnerabilityID: "CVE-2026-2000", PackageName: "busybox", InstalledVersion: "1.0.0", FixedVersion: "1.0.1", Fixable: true}}},
		},
	}

	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "down")

	if got, want := updated.adminView.Tables.ScanRuns.GetHighlightedRowIndex(), 1; got != want {
		t.Fatalf("scan-runs highlighted index = %d, want %d", got, want)
	}
	if got, want := updated.adminView.Tables.ScanRuns.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
		t.Fatalf("highlighted scan-run metadata = %#v, want %q", got, want)
	}

	updated = runKey(t, updated, "enter")
	if adminClient.getScanRunDetailCalls != 1 {
		t.Fatalf("getScanRunDetailCalls = %d, want 1", adminClient.getScanRunDetailCalls)
	}
	if !strings.Contains(updated.View(), "CVE-2026-2000") {
		t.Fatalf("view = %q, want loaded detail", updated.View())
	}

	updated = runKey(t, updated, "esc")
	if updated.screen != screenAdminFeatures {
		t.Fatalf("screen = %q, want %q after closing detail", updated.screen, screenAdminFeatures)
	}
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "c")
	if !updated.adminView.TrivyConfigModal.Active() {
		t.Fatal("expected config shortcut to stay authoritative on runtime tab")
	}

	updated = runKey(t, updated, "esc")
	updated = runKey(t, updated, "r")
	if adminClient.getFeaturePageCalls < 2 {
		t.Fatalf("getFeaturePageCalls = %d, want refresh after table interactions", adminClient.getFeaturePageCalls)
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

func TestModelTypingLInEditingInputsDoesNotLogout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	baseSession := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)}
	user := ports.AdminUser{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}
	grant := ports.AdminRepoGrant{UserID: "u-1", Repository: regixtrydomain.MustParseRepositoryRef("library/alpine"), Role: domainauth.RepoRoleReader}

	tests := []struct {
		name   string
		setup  func() Model
		assert func(t *testing.T, got Model)
	}{
		{
			name: "admin login username",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.screen = screenAdminLogin
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminLogin.Focus = loginFieldUsername
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.screen != screenAdminLogin {
					t.Fatalf("screen = %q, want %q", got.screen, screenAdminLogin)
				}
				if got.adminLogin.Username != "l" {
					t.Fatalf("username = %q, want %q", got.adminLogin.Username, "l")
				}
			},
		},
		{
			name: "create user username",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.screen = screenAdminCreateUser
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminView.CreateUserForm.Focus = adminCreateUserFieldUsername
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.adminView.CreateUserForm.Username != "l" {
					t.Fatalf("username = %q, want %q", got.adminView.CreateUserForm.Username, "l")
				}
			},
		},
		{
			name: "change password",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.screen = screenAdminChangePassword
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminView.SelectedUserID = user.ID
				model.adminView.SelectedUsername = user.Username
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.adminView.ResetPasswordForm.NewPassword != "l" {
					t.Fatalf("new password = %q, want %q", got.adminView.ResetPasswordForm.NewPassword, "l")
				}
			},
		},
		{
			name: "grant repository",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.screen = screenAdminAddGrant
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminView.SelectedUserID = user.ID
				model.adminView.SelectedUsername = user.Username
				model.adminView.GrantForm.Focus = adminGrantFieldRepository
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.adminView.GrantForm.Repository != "l" {
					t.Fatalf("repository = %q, want %q", got.adminView.GrantForm.Repository, "l")
				}
			},
		},
		{
			name: "token name",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.screen = screenAdminCreateToken
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminView.SelectedUserID = user.ID
				model.adminView.SelectedUsername = user.Username
				model.adminView.TokenForm.Focus = adminTokenFieldName
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.adminView.TokenForm.Name != "l" {
					t.Fatalf("name = %q, want %q", got.adminView.TokenForm.Name, "l")
				}
			},
		},
		{
			name: "user search",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{users: []ports.AdminUser{user}})
				model.screen = screenAdminUsers
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminView.Users = []ports.AdminUser{user}
				model.adminView.UserSearchActive = true
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.adminView.UserSearchQuery != "l" {
					t.Fatalf("query = %q, want %q", got.adminView.UserSearchQuery, "l")
				}
				if !got.adminView.UserSearchActive {
					t.Fatal("expected search to stay active")
				}
			},
		},
		{
			name: "grant edit repository",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.screen = screenAdminAddGrant
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				model.adminView.SelectedUserID = user.ID
				model.adminView.SelectedUsername = user.Username
				model.adminView.Grants = []ports.AdminRepoGrant{grant}
				model.adminView.GrantForm.Repository = grant.Repository.String()
				model.adminView.GrantForm.Focus = adminGrantFieldRepository
				return model
			},
			assert: func(t *testing.T, got Model) {
				if got.adminView.GrantForm.Repository != "library/alpinel" {
					t.Fatalf("repository = %q, want appended input", got.adminView.GrantForm.Repository)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			updated := runKey(t, tt.setup(), "l")
			if updated.screen == screenAdminLogin && updated.status == "Logged out." {
				t.Fatalf("unexpected logout: %+v", updated)
			}
			if got, want := updated.adminSession.BearerToken, baseSession.BearerToken; got != want {
				t.Fatalf("bearer token = %q, want %q", got, want)
			}
			tt.assert(t, updated)
		})
	}
}

func TestModelGrantRepositorySuggestionsFilterAndSelect(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 8, 13, 0, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModelWithCatalog(t, []string{"library/alpine", "team/demo", "team/backend", "ops/console"}, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "g")
	updated = runKey(t, updated, "n")

	initialView := updated.View()
	if !strings.Contains(initialView, "Known Repositories") || !strings.Contains(initialView, "team/demo") || !strings.Contains(initialView, "team/backend") {
		t.Fatalf("view = %q, want known repository suggestions", initialView)
	}

	updated = runKey(t, updated, "tea")
	filteredView := updated.View()
	if strings.Contains(filteredView, "library/alpine") {
		t.Fatalf("view = %q, want non-matching repository hidden", filteredView)
	}
	if !strings.Contains(filteredView, "team/demo") || !strings.Contains(filteredView, "team/backend") {
		t.Fatalf("view = %q, want filtered suggestions", filteredView)
	}

	updated = runKey(t, updated, "down")
	updated = runKey(t, updated, "enter")
	if got, want := updated.adminView.GrantForm.Repository, "team/backend"; got != want {
		t.Fatalf("repository = %q, want %q", got, want)
	}
	if got, want := updated.adminView.GrantForm.Focus, adminGrantFieldRole; got != want {
		t.Fatalf("focus = %v, want %v", got, want)
	}

	updated = runKey(t, updated, "enter")
	if adminClient.putGrantCalls != 1 {
		t.Fatalf("putGrantCalls = %d, want 1", adminClient.putGrantCalls)
	}
	if got, want := adminClient.lastGrantInput.Repository, "team/backend"; got != want {
		t.Fatalf("grant repository = %q, want %q", got, want)
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

	features              []ports.FeatureSummary
	feature               ports.FeatureDetails
	featureStatus         ports.FeatureDetails
	featurePage           ports.FeaturePage
	featurePages          map[string]ports.FeaturePage
	pageAfterAction       ports.FeaturePage
	scanRuns              []ports.ScanRun
	scanRunDetails        map[string]ports.ScanRunDetail
	featureAfterConfigure ports.FeatureDetails
	installRuntime        ports.FeatureRuntimeState
	upgradeRuntime        ports.FeatureRuntimeState
	rollbackRuntime       ports.FeatureRuntimeState
	enableFeature         ports.FeatureDetails
	disableFeature        ports.FeatureDetails
	actionResult          ports.FeatureActionResult
	actionResults         map[string]ports.FeatureActionResult
	featureErr            error

	users            []ports.AdminUser
	listUsersResults [][]ports.AdminUser
	usersErr         error

	grants    map[string][]ports.AdminRepoGrant
	grantsErr error

	tokens    map[string][]ports.AdminToken
	tokensErr error

	createUser            ports.AdminUser
	createUserErr         error
	resetPasswordErr      error
	lastConfiguredFeature string
	lastConfigureInput    ports.FeatureConfigureInput

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

	loginCalls                int
	listFeaturesCalls         int
	getFeatureCalls           int
	getFeatureStatusCalls     int
	getFeaturePageCalls       int
	listScanRunsCalls         int
	getScanRunDetailCalls     int
	executeFeatureActionCalls int
	lastFeatureAction         string
	installRuntimeCalls       int
	upgradeRuntimeCalls       int
	rollbackRuntimeCalls      int
	enableFeatureCalls        int
	disableFeatureCalls       int
	listUsersCalls            int
	listGrantsCalls           int
	listTokensCalls           int
	createUserCalls           int
	resetPasswordCalls        int
	configureFeatureCalls     int
	putGrantCalls             int
	deleteGrantCalls          int
	createTokenCalls          int
	revokeTokenCalls          int
	enableCalls               int
	disableCalls              int
}

func (f *fakeAdminClient) Login(context.Context, string, string) (AdminSession, error) {
	f.loginCalls++
	if f.loginErr != nil {
		return AdminSession{}, f.loginErr
	}
	return f.loginSession, nil
}

func (f *fakeAdminClient) ListFeatures(context.Context, AdminSession) ([]ports.FeatureSummary, error) {
	f.listFeaturesCalls++
	if f.featureErr != nil {
		return nil, f.featureErr
	}
	return append([]ports.FeatureSummary(nil), f.features...), nil
}

func (f *fakeAdminClient) GetFeature(context.Context, AdminSession, string) (ports.FeatureDetails, error) {
	f.getFeatureCalls++
	if f.featureErr != nil {
		return ports.FeatureDetails{}, f.featureErr
	}
	return f.feature, nil
}

func (f *fakeAdminClient) GetFeatureStatus(context.Context, AdminSession, string) (ports.FeatureDetails, error) {
	f.getFeatureStatusCalls++
	if f.featureErr != nil {
		return ports.FeatureDetails{}, f.featureErr
	}
	return f.featureStatus, nil
}

func (f *fakeAdminClient) GetFeaturePage(_ context.Context, _ AdminSession, name string) (ports.FeaturePage, error) {
	f.getFeaturePageCalls++
	if f.featureErr != nil {
		return ports.FeaturePage{}, f.featureErr
	}
	if page, ok := f.featurePages[name]; ok {
		return page, nil
	}
	return f.featurePage, nil
}

func (f *fakeAdminClient) ListScanRuns(context.Context, AdminSession, string, int) ([]ports.ScanRun, error) {
	f.listScanRunsCalls++
	if f.featureErr != nil {
		return nil, f.featureErr
	}
	return append([]ports.ScanRun(nil), f.scanRuns...), nil
}

func (f *fakeAdminClient) GetScanRunDetail(_ context.Context, _ AdminSession, runID string) (ports.ScanRunDetail, error) {
	f.getScanRunDetailCalls++
	if f.featureErr != nil {
		return ports.ScanRunDetail{}, f.featureErr
	}
	if detail, ok := f.scanRunDetails[runID]; ok {
		return detail, nil
	}
	return ports.ScanRunDetail{}, nil
}

func (f *fakeAdminClient) ExecuteFeatureAction(_ context.Context, _ AdminSession, name string, actionID string) (ports.FeatureActionResult, error) {
	f.executeFeatureActionCalls++
	f.lastFeatureAction = actionID
	if f.featureErr != nil {
		return ports.FeatureActionResult{}, f.featureErr
	}
	if result, ok := f.actionResults[actionID]; ok {
		if page, ok := f.featurePages[name]; ok && f.pageAfterAction.Summary.Name == "" {
			f.featurePages[name] = page
		}
		if f.pageAfterAction.Summary.Name != "" {
			f.featurePage = f.pageAfterAction
			if f.featurePages != nil {
				f.featurePages[name] = f.pageAfterAction
			}
			if len(f.features) > 0 {
				for index := range f.features {
					if f.features[index].Name == name {
						f.features[index].Enabled = f.pageAfterAction.Summary.Enabled
						f.features[index].Configured = f.pageAfterAction.Summary.Configured
					}
				}
			}
		}
		return result, nil
	}
	return f.actionResult, nil
}

func (f *fakeAdminClient) InstallFeatureRuntime(context.Context, AdminSession, string, string) (ports.FeatureRuntimeState, error) {
	f.installRuntimeCalls++
	if f.featureErr != nil {
		return ports.FeatureRuntimeState{}, f.featureErr
	}
	if f.installRuntime.ActiveVersion != "" {
		f.featureStatus.Runtime.Status = string(f.installRuntime.Status)
		f.featureStatus.Runtime.Health = string(f.installRuntime.Status)
		f.featureStatus.Runtime.Version = f.installRuntime.ActiveVersion
		f.featureStatus.Runtime.UpdateStatus = "up-to-date"
		if len(f.features) > 0 {
			f.features[0].CurrentVersion = f.installRuntime.ActiveVersion
			f.features[0].UpdateStatus = "up-to-date"
		}
		return f.installRuntime, nil
	}
	return ports.FeatureRuntimeState{}, nil
}

func (f *fakeAdminClient) UpgradeFeatureRuntime(context.Context, AdminSession, string, string) (ports.FeatureRuntimeState, error) {
	f.upgradeRuntimeCalls++
	if f.featureErr != nil {
		return ports.FeatureRuntimeState{}, f.featureErr
	}
	if f.upgradeRuntime.ActiveVersion != "" {
		f.featureStatus.Runtime.Status = string(f.upgradeRuntime.Status)
		f.featureStatus.Runtime.Health = string(f.upgradeRuntime.Status)
		f.featureStatus.Runtime.Version = f.upgradeRuntime.ActiveVersion
		f.featureStatus.Runtime.UpdateStatus = "up-to-date"
		f.featureStatus.Runtime.RollbackAvailable = strings.TrimSpace(f.upgradeRuntime.PreviousVersion) != ""
		if len(f.features) > 0 {
			f.features[0].CurrentVersion = f.upgradeRuntime.ActiveVersion
			f.features[0].UpdateStatus = "up-to-date"
		}
	}
	return f.upgradeRuntime, nil
}

func (f *fakeAdminClient) RollbackFeatureRuntime(context.Context, AdminSession, string) (ports.FeatureRuntimeState, error) {
	f.rollbackRuntimeCalls++
	if f.featureErr != nil {
		return ports.FeatureRuntimeState{}, f.featureErr
	}
	return f.rollbackRuntime, nil
}

func (f *fakeAdminClient) ConfigureFeature(_ context.Context, _ AdminSession, name string, input ports.FeatureConfigureInput) (ports.FeatureDetails, error) {
	f.configureFeatureCalls++
	if f.featureErr != nil {
		return ports.FeatureDetails{}, f.featureErr
	}
	f.lastConfiguredFeature = name
	f.lastConfigureInput = input
	if f.featureAfterConfigure.Name != "" {
		f.feature = f.featureAfterConfigure
		f.featureStatus = f.featureAfterConfigure
		return f.featureAfterConfigure, nil
	}
	return f.feature, nil
}

func (f *fakeAdminClient) EnableFeature(context.Context, AdminSession, string) (ports.FeatureDetails, error) {
	f.enableFeatureCalls++
	if f.featureErr != nil {
		return ports.FeatureDetails{}, f.featureErr
	}
	if f.enableFeature.Name != "" {
		f.feature = f.enableFeature
		f.featureStatus = f.enableFeature
		if len(f.features) > 0 {
			f.features[0].Enabled = f.enableFeature.Enabled
			f.features[0].Configured = f.enableFeature.Configured
		}
		return f.enableFeature, nil
	}
	return f.feature, nil
}

func (f *fakeAdminClient) DisableFeature(context.Context, AdminSession, string) (ports.FeatureDetails, error) {
	f.disableFeatureCalls++
	if f.featureErr != nil {
		return ports.FeatureDetails{}, f.featureErr
	}
	if f.disableFeature.Name != "" {
		f.feature = f.disableFeature
		f.featureStatus = f.disableFeature
		if len(f.features) > 0 {
			f.features[0].Enabled = f.disableFeature.Enabled
			f.features[0].Configured = f.disableFeature.Configured
		}
		return f.disableFeature, nil
	}
	return f.feature, nil
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

func newAdminReadyModelWithCatalog(t *testing.T, repositories []string, adminClient AdminClient) Model {
	t.Helper()
	model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: append([]string(nil), repositories...)}}, WithAdminClient(adminClient))
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

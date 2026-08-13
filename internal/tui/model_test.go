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
	"github.com/muesli/termenv"
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

func TestModelUpdateWindowSizeMsgSetsViewport(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 120, Height: 45})
	result := updated.(Model)

	if result.viewport.Width != 120 || result.viewport.Height != 45 {
		t.Fatalf("viewport = %+v, want 120x45", result.viewport)
	}
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
}

func TestModelNewModelDefaultsViewportTo100x40(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})

	if model.viewport.Width != defaultViewportWidth || model.viewport.Height != defaultViewportHeight {
		t.Fatalf("viewport = %+v, want %dx%d (design.md decision #8 default/--snapshot fallback)", model.viewport, defaultViewportWidth, defaultViewportHeight)
	}
}

func TestModelViewBelowMinimumSizeShowsTerminalTooSmall(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	view := updated.(Model).View()

	if !strings.Contains(view, "Terminal too small") {
		t.Fatalf("view = %q, want too-small message", view)
	}
	if want := fmt.Sprintf("Regixtry needs at least %dx%d. Current: 60x20.", minViewportWidth, minViewportHeight); !strings.Contains(view, want) {
		t.Fatalf("view = %q, want current-size detail %q", view, want)
	}
	if strings.Contains(view, "Regixtry is empty") || strings.Contains(view, model.loadingText) {
		t.Fatalf("view = %q, want no screen content below minimum size", view)
	}
}

func TestModelViewResizeAboveMinimumRestoresRendering(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	tooSmall, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if !strings.Contains(tooSmall.(Model).View(), "Terminal too small") {
		t.Fatalf("view = %q, want too-small message before resize", tooSmall.(Model).View())
	}

	restored, _ := tooSmall.(Model).Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight})
	view := restored.(Model).View()

	if strings.Contains(view, "Terminal too small") {
		t.Fatalf("view = %q, want normal rendering restored after resize above minimum", view)
	}
	if !strings.Contains(view, model.loadingText) {
		t.Fatalf("view = %q, want loading screen content restored", view)
	}
}

// TestModelResizeTallerRebuildsAdminTablesToShowMoreRowsWithoutRestart is the
// Phase 5 task 5.5 RED test, first scenario (spec.md "Live Terminal Resize
// Refit", "Growing the terminal shows more rows"): resizing taller while an
// admin table screen is displayed must recompute the table pageSize and show
// more rows without the operator restarting or re-navigating.
func TestModelResizeTallerRebuildsAdminTablesToShowMoreRowsWithoutRestart(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight})
	result := updated.(Model)
	result.screen = screenAdminFeatures
	result.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
	result.adminView.Features = make([]ports.FeatureSummary, 40)
	for i := range result.adminView.Features {
		result.adminView.Features[i] = ports.FeatureSummary{Name: fmt.Sprintf("feature-%d", i)}
	}
	result.rebuildAdminTables(result.adminTablesLayout())
	beforePageSize := result.adminView.Layout.Primary

	grown, _ := result.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight + 20})
	afterModel := grown.(Model)

	if got := afterModel.adminView.Layout.Primary; got <= beforePageSize {
		t.Fatalf("primary pageSize after taller resize = %d, want > %d (before resize) — resize must recompute the table budget without restart", got, beforePageSize)
	}
	wantHeight := afterModel.adminView.Layout.Primary + tableChromeRows
	if got := lipgloss.Height(afterModel.adminView.Tables.Features.View()); got != wantHeight {
		t.Fatalf("features table height after resize = %d, want pageSize(%d)+tableChromeRows = %d", got, afterModel.adminView.Layout.Primary, wantHeight)
	}
}

// TestModelResizeShorterRebuildsAdminTablesWithoutExceedingViewport is the
// Phase 5 task 5.5 RED test, second scenario (spec.md "Live Terminal Resize
// Refit", "Shrinking the terminal re-bounds the screen"): resizing shorter
// while an admin table screen is fully visible must re-fit the table to the
// new height and must not exceed the new viewport.
func TestModelResizeShorterRebuildsAdminTablesWithoutExceedingViewport(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: adminTestViewportHeight})
	result := updated.(Model)
	result.screen = screenAdminFeatures
	result.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
	result.adminView.Features = make([]ports.FeatureSummary, 40)
	for i := range result.adminView.Features {
		result.adminView.Features[i] = ports.FeatureSummary{Name: fmt.Sprintf("feature-%d", i)}
	}
	result.rebuildAdminTables(result.adminTablesLayout())
	beforePageSize := result.adminView.Layout.Primary

	shrunk, _ := result.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight})
	afterModel := shrunk.(Model)

	if got := afterModel.adminView.Layout.Primary; got >= beforePageSize {
		t.Fatalf("primary pageSize after shorter resize = %d, want < %d (before resize) — resize must re-fit the table to the smaller budget", got, beforePageSize)
	}
	if got := lipgloss.Height(afterModel.View()); got > minViewportHeight {
		t.Fatalf("view height after shorter resize = %d, want <= %d (new viewport)", got, minViewportHeight)
	}
}

func TestModelContentBudgetWrapsPackageLevelContentBudgetUsingViewportAndGivenStatusHelp(t *testing.T) {
	t.Parallel()

	// Phase 3 deviation from design.md's originally specified zero-arg
	// method: status/help must be the exact strings the caller is about to
	// render (some screens show m.notice or a computed status, not
	// m.status), otherwise the row budget and the actual chrome height
	// diverge and the viewport invariant breaks. See contentBudget's doc
	// comment in model.go.
	model := NewModel(&fakeQueryService{})
	model.status = "Loading admin users..."
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	result := updated.(Model)

	status := "A different status than m.status"
	help := "q: quit"
	got := result.contentBudget(status, help)
	want := contentBudget(result.viewport.Width, result.viewport.Height, status, help)
	want.Scroll = result.bodyScroll

	if got != want {
		t.Fatalf("contentBudget(%q, %q) = %+v, want %+v (Model.contentBudget must wrap the package-level pure function with the exact given status/help and bodyScroll)", status, help, got, want)
	}
}

func TestModelCatalogAndTagsListScreensFitViewportHeight(t *testing.T) {
	t.Parallel()

	items := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		items = append(items, fmt.Sprintf("library/repo-%02d", i))
	}

	for _, height := range []int{24, 30, 50} {
		height := height
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			t.Parallel()

			model := NewModel(&fakeQueryService{})
			updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: height})
			result := updated.(Model)

			result.screen = screenRepositories
			result.repositories = RepositoriesModel{Items: items}
			view := result.View()
			if got := lipgloss.Height(view); got > height {
				t.Fatalf("repositories view height = %d, want <= %d\nview:\n%s", got, height, view)
			}

			result.screen = screenTags
			result.tags = TagsModel{Repository: "library/alpine", Items: items}
			view = result.View()
			if got := lipgloss.Height(view); got > height {
				t.Fatalf("tags view height = %d, want <= %d\nview:\n%s", got, height, view)
			}
		})
	}
}

// Phase 4 disposition note (tasks.md 5.2): the old-architecture regression
// tests TestModelTrivyRepositoryAlertsScreenFitsViewportHeight and
// TestModelTrivyRepositoryAlertsDetailShowsFindingsTableOnReasonablyTallTerminal
// were removed here, not superseded in place, because they constructed
// TrivyAlertDetailOpen/TrivyScanRunDetail/SecretFindings state directly —
// fields this phase deletes along with the inline-detail render path
// (renderTrivyRepositoryAlerts). Their coverage is superseded by:
//   - TestModelScanHistoryModalRendersWithinViewportAcrossHeights (heights
//     24/30/50, including the 24-row floor, with a 40-row findings AND a
//     40-row secrets page open via the real key-press flow) for the
//     viewport-fit guarantee the first test proved.
//   - TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator
//     for the "Findings content actually survives the render, not just its
//     heading" guarantee the second test proved (it asserts the specific
//     CVE ID is present in the rendered view).
//   - TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite
//     (admin_views_test.go) for the structural fix that makes the second
//     test's original bug class (an orphaned indicator with no table above
//     it) impossible under the new architecture: every bordered block is
//     clipped exactly once, by its own owner, never by a second fitLines
//     pass over an already-composed block.

// TestModelScreenErrorRendersWithThemeErrorRegardlessOfMessageWording is the
// Phase 4 task 4.3 RED test (design.md Decision 2 / spec.md "Fatal error
// renders in error styling regardless of wording"): screenError currently
// passes errText as both body and status to renderInspectionWorkspace, which
// classifies style from a substring match — an error message containing
// none of "expired"/"invalid"/"error" (e.g. "connection refused") falls
// through to theme.muted instead of theme.error. Once screenError passes an
// explicit statusKindError, both the body and status line must render with
// theme.error's hex regardless of wording.
func TestModelScreenErrorRendersWithThemeErrorRegardlessOfMessageWording(t *testing.T) {
	// Not t.Parallel(): forces the global lipgloss color profile so
	// theme.error actually emits an ANSI sequence to assert on (go test runs
	// with no tty, so lipgloss otherwise auto-detects "no color").
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: 30})
	result := updated.(Model)
	result.screen = screenError
	result.err = errors.New("connection refused")

	view := result.View()

	theme := newAdminTheme()
	errorHex := theme.error.Render("connection refused")
	// theme.error.Render on the raw message proves the SGR sequence itself
	// (extracted below) is what theme.error actually emits, rather than
	// hardcoding an ANSI escape string.
	sgrPrefix := errorHex[:strings.Index(errorHex, "connection refused")]
	if sgrPrefix == "" {
		t.Fatalf("test setup invalid: theme.error.Render() produced no ANSI prefix: %q", errorHex)
	}

	bodyIdx := strings.Index(view, "connection refused")
	if bodyIdx == -1 {
		t.Fatalf("view = %q, want the error message present", view)
	}
	if !strings.Contains(view, sgrPrefix) {
		t.Fatalf("view = %q, want theme.error's ANSI styling (%q) present on the body/status text — got no error-styled occurrence of %q", view, sgrPrefix, "connection refused")
	}
	if got := strings.Count(view, sgrPrefix); got < 2 {
		t.Fatalf("view contains theme.error's ANSI prefix %d times, want >= 2 (both body and status line styled with theme.error)\n%s", got, view)
	}
}

func TestModelPageKeysScrollAndClampCatalogList(t *testing.T) {
	t.Parallel()

	items := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		items = append(items, fmt.Sprintf("library/repo-%02d", i))
	}

	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: 24})
	result := updated.(Model)
	result.screen = screenRepositories
	result.repositories = RepositoriesModel{Items: items}

	if !strings.Contains(result.View(), "repo-00") {
		t.Fatalf("initial view = %q, want first item visible", result.View())
	}

	scrolled := runKey(t, result, "pgdown")
	if scrolled.bodyScroll <= result.bodyScroll {
		t.Fatalf("bodyScroll = %d, want increase after pgdown (was %d)", scrolled.bodyScroll, result.bodyScroll)
	}
	if strings.Contains(scrolled.View(), "repo-00") {
		t.Fatalf("view = %q, want repo-00 scrolled out of view after pgdown", scrolled.View())
	}

	pastEnd := scrolled
	for i := 0; i < 30; i++ {
		pastEnd = runKey(t, pastEnd, "pgdown")
	}
	if !strings.Contains(pastEnd.View(), "repo-59") {
		t.Fatalf("view = %q, want last item visible after paging past the end", pastEnd.View())
	}

	home := runKey(t, pastEnd, "home")
	if home.bodyScroll != 0 {
		t.Fatalf("bodyScroll = %d, want 0 after home", home.bodyScroll)
	}
	if !strings.Contains(home.View(), "repo-00") {
		t.Fatalf("view = %q, want repo-00 visible after home", home.View())
	}

	endJump := runKey(t, home, "end")
	if !strings.Contains(endJump.View(), "repo-59") {
		t.Fatalf("view = %q, want repo-59 visible after end", endJump.View())
	}

	pastTop := endJump
	for i := 0; i < 30; i++ {
		pastTop = runKey(t, pastTop, "pgup")
	}
	if pastTop.bodyScroll != 0 {
		t.Fatalf("bodyScroll = %d, want 0 after paging up past the top", pastTop.bodyScroll)
	}
	if !strings.Contains(pastTop.View(), "repo-00") {
		t.Fatalf("view = %q, want repo-00 visible after paging up past the top", pastTop.View())
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

func TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
		scanPolicy: ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")

	if got, want := adminClient.getScanPolicyCalls, 1; got != want {
		t.Fatalf("getScanPolicyCalls = %d, want %d", got, want)
	}
	if got, want := updated.adminView.ScanPolicy, (ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}); got != want {
		t.Fatalf("adminView.ScanPolicy = %#v, want %#v", got, want)
	}

	updated = runKey(t, updated, "p")
	modalView := updated.View()
	for _, want := range []string{"Enabled", "Severity Threshold", "CRITICAL"} {
		if !strings.Contains(modalView, want) {
			t.Fatalf("view = %q, want %q", modalView, want)
		}
	}

	// Toggle Enabled off, then Tab to Threshold and cycle it to
	// CRITICAL+HIGH before submitting.
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, " ")

	submitted := runKey(t, updated, "enter")

	if got, want := adminClient.updateScanPolicyCalls, 1; got != want {
		t.Fatalf("updateScanPolicyCalls = %d, want %d", got, want)
	}
	if got, want := adminClient.lastScanPolicyInput, (ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}); got != want {
		t.Fatalf("lastScanPolicyInput = %#v, want %#v", got, want)
	}
	if got, want := submitted.adminView.ScanPolicy, (ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}); got != want {
		t.Fatalf("adminView.ScanPolicy = %#v, want %#v", got, want)
	}
	if submitted.adminView.ScanPolicyModal.Active() {
		t.Fatalf("ScanPolicyModal = %#v, want closed after submit", submitted.adminView.ScanPolicyModal)
	}
	if !strings.Contains(submitted.View(), "Vulnerability policy saved") {
		t.Fatalf("view = %q, want policy feedback after submit", submitted.View())
	}
}

// TestModelRepositoryOverrideModalOpenerKeyIsScopedToRepositoryAlertsRow is
// the Phase 8 task 8.4/8.5 RED test (operator-admin-tui spec's "The override
// key is scoped to the Repository Alerts row only" scenario, design.md
// Decision 8 piece 2): `o` on a highlighted Repository Alerts row opens
// repositoryOverrideModal bound to that repository and trivyFeatureName and
// fires a load; `o` on the Runtime tab, or on Repository Alerts with no rows
// loaded, must not open it and must not collide with featureActionForKey's
// fallback (the case sits before the `model.go:1257` fallback in the
// switch).
func TestModelRepositoryOverrideModalOpenerKeyIsScopedToRepositoryAlertsRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	baseFeaturePage := ports.FeaturePage{
		Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
	}

	t.Run("o opens the modal on a highlighted Repository Alerts row", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  baseFeaturePage,
			scanRuns: []ports.ScanRun{
				{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1},
			},
			repositoryOverrides: map[string]ports.RepositoryOverrideDetails{
				"trivy/team/api": {Repository: "team/api", Feature: "trivy", Enabled: false, IgnoreFilePath: "/etc/trivy/ignore"},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "tab") // switch to Repository Alerts

		updated = runKey(t, updated, "o")

		if !updated.adminView.RepositoryOverrideModal.Open {
			t.Fatal("RepositoryOverrideModal.Open = false, want true after 'o'")
		}
		if got, want := updated.adminView.RepositoryOverrideModal.Repository, "team/api"; got != want {
			t.Fatalf("RepositoryOverrideModal.Repository = %q, want %q", got, want)
		}
		if got, want := updated.adminView.RepositoryOverrideModal.Feature, trivyFeatureName; got != want {
			t.Fatalf("RepositoryOverrideModal.Feature = %q, want %q", got, want)
		}
		if adminClient.getRepositoryOverrideCalls != 1 {
			t.Fatalf("getRepositoryOverrideCalls = %d, want 1", adminClient.getRepositoryOverrideCalls)
		}
		if updated.adminView.RepositoryOverrideModal.Loading {
			t.Fatal("RepositoryOverrideModal.Loading = true, want false once the load Cmd has resolved")
		}
		if !updated.adminView.RepositoryOverrideModal.Exists || !strings.Contains(updated.View(), "team/api") {
			t.Fatalf("RepositoryOverrideModal = %#v, want the stored override reflected", updated.adminView.RepositoryOverrideModal)
		}

		closed := runKey(t, updated, "esc")
		if closed.adminView.RepositoryOverrideModal.Open {
			t.Fatal("RepositoryOverrideModal.Open = true, want false after esc")
		}
	})

	t.Run("o on the Runtime tab does not open the modal or collide with feature actions", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  baseFeaturePage,
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")

		updated = runKey(t, updated, "o")

		if updated.adminView.RepositoryOverrideModal.Open {
			t.Fatal("RepositoryOverrideModal.Open = true, want false on the Runtime tab")
		}
		if adminClient.getRepositoryOverrideCalls != 0 {
			t.Fatalf("getRepositoryOverrideCalls = %d, want 0 (no load fired)", adminClient.getRepositoryOverrideCalls)
		}
		if adminClient.executeFeatureActionCalls != 0 {
			t.Fatalf("executeFeatureActionCalls = %d, want 0 ('o' must not fall through to featureActionForKey)", adminClient.executeFeatureActionCalls)
		}
	})

	t.Run("o with no Repository Alerts row highlighted does not open the modal", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  baseFeaturePage,
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "tab") // Repository Alerts, no scan runs loaded

		updated = runKey(t, updated, "o")

		if updated.adminView.RepositoryOverrideModal.Open {
			t.Fatal("RepositoryOverrideModal.Open = true, want false with no highlighted row")
		}
	})
}

// TestModelRepositoryOverrideModalSetAndClearRoundTripReflectsInModal is the
// Phase 8 tasks 8.6/8.7 RED test (operator-admin-tui spec's "Operator sets
// an override from the modal" / "Operator clears an override from the
// modal" scenarios): submitting new values persists through the admin API
// and reflects the new values back in the modal (still open, unlike
// scanPolicyModal); clearing deletes it and reflects the repository using
// global settings.
func TestModelRepositoryOverrideModalSetAndClearRoundTripReflectsInModal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
		scanRuns: []ports.ScanRun{
			{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "o")

	if !updated.adminView.RepositoryOverrideModal.Open || updated.adminView.RepositoryOverrideModal.Exists {
		t.Fatalf("RepositoryOverrideModal = %#v, want open with no existing override", updated.adminView.RepositoryOverrideModal)
	}

	// Tab past Feature to Enabled, toggle it on, Tab to PathPrimary, type a
	// path, then Enter to submit.
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "tab")
	for _, r := range "/etc/trivy/ignore" {
		updated = runKey(t, updated, string(r))
	}
	submitted := runKey(t, updated, "enter")

	if adminClient.setRepositoryOverrideCalls != 1 {
		t.Fatalf("setRepositoryOverrideCalls = %d, want 1", adminClient.setRepositoryOverrideCalls)
	}
	if !adminClient.lastSetRepositoryOverride.Enabled || adminClient.lastSetRepositoryOverride.IgnoreFilePath != "/etc/trivy/ignore" {
		t.Fatalf("lastSetRepositoryOverride = %#v, want Enabled true with the typed ignore file path", adminClient.lastSetRepositoryOverride)
	}
	if !submitted.adminView.RepositoryOverrideModal.Open {
		t.Fatal("RepositoryOverrideModal.Open = false, want the modal to stay open after a successful save")
	}
	if !submitted.adminView.RepositoryOverrideModal.Exists || submitted.adminView.RepositoryOverrideModal.PathPrimary != "/etc/trivy/ignore" {
		t.Fatalf("RepositoryOverrideModal = %#v, want the saved override reflected", submitted.adminView.RepositoryOverrideModal)
	}
	if !strings.Contains(submitted.View(), "Repository override saved") {
		t.Fatalf("view = %q, want save feedback", submitted.View())
	}

	// Tab to the Clear row and press Enter to clear it.
	cleared := submitted
	for cleared.adminView.RepositoryOverrideModal.Focus != repositoryOverrideFieldClear {
		cleared = runKey(t, cleared, "tab")
	}
	cleared = runKey(t, cleared, "enter")

	if adminClient.clearRepositoryOverrideCalls != 1 {
		t.Fatalf("clearRepositoryOverrideCalls = %d, want 1", adminClient.clearRepositoryOverrideCalls)
	}
	if cleared.adminView.RepositoryOverrideModal.Exists {
		t.Fatal("RepositoryOverrideModal.Exists = true, want false after clear")
	}
	if !strings.Contains(cleared.View(), "inheriting global settings") {
		t.Fatalf("view = %q, want the modal to show the repository inheriting global settings after clear", cleared.View())
	}

	// Pressing Enter on the Clear row again (already inheriting global) must
	// not issue another DELETE.
	inert := runKey(t, cleared, "enter")
	if adminClient.clearRepositoryOverrideCalls != 1 {
		t.Fatalf("clearRepositoryOverrideCalls = %d, want still 1 (inert when already inheriting global)", adminClient.clearRepositoryOverrideCalls)
	}
	if !strings.Contains(inert.status, "Already inheriting global") {
		t.Fatalf("status = %q, want the inert-clear message", inert.status)
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
		if got, want := updated.adminView.Tables.ScanSummary.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
			t.Fatalf("highlighted scan-summary metadata = %#v, want %q", got, want)
		}

		// Enter opens the scan history modal instead of the old inline
		// detail (spec.md "Repository Alert Drill-Down Opens History
		// Modal"), scoped to the highlighted summary row's repository.
		updated = runKey(t, updated, "enter")
		if adminClient.getScanRunDetailCalls != 1 {
			t.Fatalf("getScanRunDetailCalls = %d, want detail fetch on enter", adminClient.getScanRunDetailCalls)
		}
		if !updated.adminView.ScanHistoryModal.Open {
			t.Fatal("ScanHistoryModal.Open = false, want true after enter")
		}
		if got, want := updated.adminView.ScanHistoryModal.Repository, "team/api"; got != want {
			t.Fatalf("ScanHistoryModal.Repository = %q, want %q", got, want)
		}
		detailView := updated.View()
		for _, want := range []string{"Scan History — team/api", "CVE-2026-0001", "openssl"} {
			if !strings.Contains(detailView, want) {
				t.Fatalf("view = %q, want %q", detailView, want)
			}
		}

		updated = runKey(t, updated, "esc")
		if updated.adminView.ScanHistoryModal.Open {
			t.Fatal("ScanHistoryModal.Open = true, want false after esc")
		}
		if strings.Contains(updated.View(), "Scan History —") {
			t.Fatalf("view = %q, want esc to close the scan history modal with no residual detail", updated.View())
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

// TestModelRepositoryAlertsScreenShowsOneRowPerRepositoryNotPerScanRun is the
// Phase 4 removal-sweep RED/GREEN test: after renderFeaturePageBody's call
// site swap (renderTrivyRepositoryAlerts -> renderAdminScanSummary), the
// Repository Alerts screen an operator actually sees through a real
// Model.Update()/View() key flow must show exactly one row per repository,
// with a Runs count reflecting every underlying scan run for that
// repository — not one row per scan run (spec.md "Repository Alerts
// Summarized Per Repository With Ordering And Freshness").
func TestModelRepositoryAlertsScreenShowsOneRowPerRepositoryNotPerScanRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		// team/api has three underlying scan runs; library/base has one.
		// Four scan runs total, but only two repositories.
		scanRuns: []ports.ScanRun{
			{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, CreatedAt: now.Add(-3 * time.Hour), Critical: 0, High: 1},
			{ID: "run-2", Repository: "team/api", RequestedRef: "1.0.1", Status: ports.ScanRunStatusCompleted, CreatedAt: now.Add(-2 * time.Hour), Critical: 1, High: 0},
			{ID: "run-3", Repository: "team/api", RequestedRef: "1.0.2", Status: ports.ScanRunStatusCompleted, CreatedAt: now.Add(-1 * time.Hour), Critical: 2, High: 0, HasFixable: true},
			{ID: "run-4", Repository: "library/base", RequestedRef: "stable", Status: ports.ScanRunStatusCompleted, CreatedAt: now.Add(-30 * time.Minute), Critical: 0, High: 0},
		},
	}

	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")

	summaryTable := updated.adminView.Tables.ScanSummary
	if got, want := summaryTable.TotalRows(), 2; got != want {
		t.Fatalf("ScanSummary TotalRows() = %d, want %d (one row per repository, not %d per scan run)", got, want, len(adminClient.scanRuns))
	}

	view := updated.View()
	if !strings.Contains(view, "Repository Alerts") {
		t.Fatalf("view = %q, want the Repository Alerts summary table on screen", view)
	}
	// The old per-scan-run rendering path is gone: only one row per
	// repository is ever shown, so "team/api" appears exactly once even
	// though it backs three scan runs.
	if got, want := strings.Count(view, "team/api"), 1; got != want {
		t.Fatalf("view contains %q %d times, want exactly %d (one summary row, not one per underlying scan run)", "team/api", got, want)
	}
	if !strings.Contains(view, "library/base") {
		t.Fatalf("view = %q, want %q's summary row visible", view, "library/base")
	}
	// The Runs column on team/api's single summary row must reflect all
	// three underlying scan runs.
	rows := summaryTable.GetVisibleRows()
	found := false
	for _, row := range rows {
		if repo, _ := row.Data[adminTableColumnScanSummaryRepository].(string); repo == "team/api" {
			found = true
			if got, want := row.Data[adminTableColumnScanSummaryRuns], 3; got != want {
				t.Fatalf("team/api Runs column = %#v, want %d", got, want)
			}
		}
	}
	if !found {
		t.Fatal("team/api summary row not found in ScanSummary table")
	}
}

// newScanHistoryModalReadyModel logs in, opens the Trivy Repository Alerts
// tab, and presses Enter to open the scan history modal — a real
// Model.Update()/View() key-press flow (design.md "Keys ... gated in
// updateAdminKey"), not direct AdminViewState construction, shared by the
// Phase 3 wiring tests below.
func newScanHistoryModalReadyModel(t *testing.T, adminClient *fakeAdminClient) Model {
	t.Helper()
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")
	return runKey(t, updated, "enter")
}

// TestModelScanHistoryModalTabCyclesForwardAndBackwardWrapping is the Phase
// 3 task 3.2 RED test (spec.md "Operator cycles between tabs"): Tab/Shift+Tab
// cycle the modal's ordered tab set via cycleIndex, wrapping past either end
// instead of clamping — distinct from every other boundedIndex-based
// selection in this screen.
func TestModelScanHistoryModalTabCyclesForwardAndBackwardWrapping(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", RequestedRef: "latest", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted, Critical: 1, HasFixable: true}},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	if got, want := opened.adminView.ScanHistoryModal.ActiveTab, 0; got != want {
		t.Fatalf("ActiveTab = %d, want %d (Vulnerabilities is the default tab)", got, want)
	}

	next := runKey(t, opened, "tab")
	if got, want := next.adminView.ScanHistoryModal.ActiveTab, 1; got != want {
		t.Fatalf("ActiveTab after tab = %d, want %d (Leaks)", got, want)
	}

	wrapped := runKey(t, next, "tab")
	if got, want := wrapped.adminView.ScanHistoryModal.ActiveTab, 0; got != want {
		t.Fatalf("ActiveTab after tab past the last = %d, want %d (wraps to Vulnerabilities)", got, want)
	}

	back := runKey(t, wrapped, "shift+tab")
	if got, want := back.adminView.ScanHistoryModal.ActiveTab, 1; got != want {
		t.Fatalf("ActiveTab after shift+tab before the first = %d, want %d (wraps to Leaks)", got, want)
	}
}

// TestModelScanHistoryModalFindingCursorMovesBoundedWithinActiveTabList is
// the RED test for the findings-row cursor: Up/Down move FindingCursor by
// ±1, bounded (never wrapping, unlike the tab cursor) within the ACTIVE
// tab's own list length.
func TestModelScanHistoryModalFindingCursorMovesBoundedWithinActiveTabList(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run: ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa"},
				Findings: []ports.ScanRunFinding{
					{VulnerabilityID: "CVE-1"},
					{VulnerabilityID: "CVE-2"},
					{VulnerabilityID: "CVE-3"},
				},
			},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	if got, want := opened.adminView.ScanHistoryModal.FindingCursor, 0; got != want {
		t.Fatalf("initial FindingCursor = %d, want %d", got, want)
	}

	down := runKey(t, opened, "down")
	if got, want := down.adminView.ScanHistoryModal.FindingCursor, 1; got != want {
		t.Fatalf("FindingCursor after down = %d, want %d", got, want)
	}

	downPastEnd := down
	for i := 0; i < 5; i++ {
		downPastEnd = runKey(t, downPastEnd, "down")
	}
	if got, want := downPastEnd.adminView.ScanHistoryModal.FindingCursor, 2; got != want {
		t.Fatalf("FindingCursor after paging past the end = %d, want %d (clamped, not wrapping)", got, want)
	}

	up := runKey(t, downPastEnd, "up")
	if got, want := up.adminView.ScanHistoryModal.FindingCursor, 1; got != want {
		t.Fatalf("FindingCursor after up = %d, want %d", got, want)
	}

	upPastStart := up
	for i := 0; i < 5; i++ {
		upPastStart = runKey(t, upPastStart, "up")
	}
	if got, want := upPastStart.adminView.ScanHistoryModal.FindingCursor, 0; got != want {
		t.Fatalf("FindingCursor after paging past the start = %d, want %d (clamped, not wrapping)", got, want)
	}

	if got, want := upPastStart.adminView.Tables.Findings.HighlightedRow().Data[adminTableMetaFindingID], "CVE-1"; got != want {
		t.Fatalf("highlighted finding = %v, want %q (FindingCursor wired into buildAdminFindingsTable's highlighted arg)", got, want)
	}
}

// TestModelScanHistoryModalFindingCursorResetsOnTabSwitchAndHistoryPaging is
// the RED test guarding FindingCursor's two reset triggers: switching the
// active tab (Tab/Shift+Tab) and navigating to a different execution
// (Left/Right) — both must reset FindingCursor to 0 so it never points past
// the end of a freshly loaded, possibly shorter list.
func TestModelScanHistoryModalFindingCursorResetsOnTabSwitchAndHistoryPaging(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	older := now.Add(-24 * time.Hour)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns: []ports.ScanRun{
			{ID: "run-new", Repository: "acme/api", Digest: "sha256:new", FinishedAt: &now, CreatedAt: now},
			{ID: "run-old", Repository: "acme/api", Digest: "sha256:old", FinishedAt: &older, CreatedAt: older},
		},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-new": {Run: ports.ScanRun{ID: "run-new", Repository: "acme/api", Digest: "sha256:new"}, Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}, {VulnerabilityID: "CVE-2"}}},
			"run-old": {Run: ports.ScanRun{ID: "run-old", Repository: "acme/api", Digest: "sha256:old"}, Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-3"}}},
		},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			"acme/api@sha256:new": {Findings: []ports.SecretFinding{{RuleID: "rule-1"}}},
			"acme/api@sha256:old": {Findings: []ports.SecretFinding{{RuleID: "rule-2"}}},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	movedDown := runKey(t, opened, "down")
	if got, want := movedDown.adminView.ScanHistoryModal.FindingCursor, 1; got != want {
		t.Fatalf("test setup invalid: FindingCursor = %d, want %d", got, want)
	}

	switchedTab := runKey(t, movedDown, "tab")
	if got, want := switchedTab.adminView.ScanHistoryModal.FindingCursor, 0; got != want {
		t.Fatalf("FindingCursor after switching tab = %d, want %d (reset)", got, want)
	}

	movedDownAgain := runKey(t, movedDown, "down") // FindingCursor now at the last valid index (1)
	paged := runKey(t, movedDownAgain, "right")
	if got, want := paged.adminView.ScanHistoryModal.FindingCursor, 0; got != want {
		t.Fatalf("FindingCursor after paging history = %d, want %d (reset)", got, want)
	}
}

// TestModelScanHistoryModalEnterOpensSelectedFindingLink is the RED test for
// Enter on the Vulnerabilities tab: it resolves the finding at FindingCursor
// and opens its link (PrimaryURL when set) via the swappable
// openURLInBrowser, never a real browser exec in this test.
func TestModelScanHistoryModalEnterOpensSelectedFindingLink(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()
	var openedURLs []string
	openURLInBrowser = func(rawURL string) error {
		openedURLs = append(openedURLs, rawURL)
		return nil
	}

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run: ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa"},
				Findings: []ports.ScanRunFinding{
					{VulnerabilityID: "CVE-WITH-URL", PrimaryURL: "https://example.com/advisory/1"},
					{VulnerabilityID: "CVE-NO-URL"},
				},
			},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)

	withURL := runKey(t, opened, "enter")
	_ = withURL
	if len(openedURLs) != 1 || openedURLs[0] != "https://example.com/advisory/1" {
		t.Fatalf("openedURLs = %v, want exactly [%q] (PrimaryURL for the finding at FindingCursor 0)", openedURLs, "https://example.com/advisory/1")
	}

	movedDown := runKey(t, opened, "down")
	runKey(t, movedDown, "enter")
	if len(openedURLs) != 2 || openedURLs[1] != nvdVulnerabilityURL("CVE-NO-URL") {
		t.Fatalf("openedURLs = %v, want the second entry to be the constructed NVD URL for CVE-NO-URL (%q)", openedURLs, nvdVulnerabilityURL("CVE-NO-URL"))
	}
}

// TestModelScanHistoryModalEnterFailureShowsCopyableURLStatus is the RED
// test for the headless-server fallback: when openURLInBrowser fails (e.g.
// no GUI opener installed, such as xdg-open missing on a headless Linux
// server), the status must never surface the raw Go exec error text --
// meaningless to a TUI operator -- but instead show the resolved URL itself
// so the operator can select/copy it from the terminal. Any failure falls
// back this way, not just a missing-binary one (no error-type sniffing).
func TestModelScanHistoryModalEnterFailureShowsCopyableURLStatus(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()
	openURLInBrowser = func(rawURL string) error {
		return fmt.Errorf(`exec: "xdg-open": executable file not found in $PATH`)
	}

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run:      ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa"},
				Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-WITH-URL", PrimaryURL: "https://example.com/advisory/1"}},
			},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	updated := runKey(t, opened, "enter")

	wantStatus := "Could not open automatically — copy this link: https://example.com/advisory/1"
	if got := updated.status; got != wantStatus {
		t.Fatalf("status = %q, want %q", got, wantStatus)
	}
	if strings.Contains(updated.status, "xdg-open") {
		t.Fatalf("status = %q, must never leak the raw exec error text", updated.status)
	}
}

// TestModelScanHistoryModalEnterOnLeaksTabDoesNotOpenAnyLink is the RED test
// guarding the Vulnerabilities-only scope: Enter on the Leaks tab must never
// attempt to open a link (secret findings carry no comparable field).
func TestModelScanHistoryModalEnterOnLeaksTabDoesNotOpenAnyLink(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()
	var openedURLs []string
	openURLInBrowser = func(rawURL string) error {
		openedURLs = append(openedURLs, rawURL)
		return nil
	}

	now := time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {Run: ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa"}, Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1", PrimaryURL: "https://example.com/1"}}},
		},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			"acme/api@sha256:aaa": {Findings: []ports.SecretFinding{{RuleID: "rule-1"}}},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	leaksTab := runKey(t, opened, "tab")
	if got, want := leaksTab.adminView.ScanHistoryModal.ActiveTab, 1; got != want {
		t.Fatalf("test setup invalid: ActiveTab = %d, want %d (Leaks)", got, want)
	}

	runKey(t, leaksTab, "enter")
	if len(openedURLs) != 0 {
		t.Fatalf("openedURLs = %v, want none opened on the Leaks tab", openedURLs)
	}
}

// TestModelScanHistoryModalHistoryNavigationRefetchesDetailAndSecretsPerCursor
// is the Phase 3 task 3.3 RED test (spec.md "Position indicator shows one
// run's own findings"): Left/Right paging must re-fire the
// loadAdminScanRunDetailCmd/loadAdminSecretScanFindingsCmd chain for the
// newly selected run, so both the Vulnerabilities and Leaks tabs reflect
// whichever run is currently navigated, not the run the modal opened on.
func TestModelScanHistoryModalHistoryNavigationRefetchesDetailAndSecretsPerCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 9, 30, 0, 0, time.UTC)
	older := now.Add(-48 * time.Hour)
	newer := now.Add(-1 * time.Hour)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns: []ports.ScanRun{
			{ID: "run-old", Repository: "acme/api", RequestedRef: "1.0.0", Digest: "sha256:old", Status: ports.ScanRunStatusCompleted, FinishedAt: &older, CreatedAt: older, Critical: 1, HasFixable: true},
			{ID: "run-new", Repository: "acme/api", RequestedRef: "2.0.0", Digest: "sha256:new", Status: ports.ScanRunStatusCompleted, FinishedAt: &newer, CreatedAt: newer, Critical: 0, HasFixable: false},
		},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-old": {Run: ports.ScanRun{ID: "run-old", Repository: "acme/api", Digest: "sha256:old"}, Findings: []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-OLD", PackageName: "openssl"}}},
			"run-new": {Run: ports.ScanRun{ID: "run-new", Repository: "acme/api", Digest: "sha256:new"}, Findings: []ports.ScanRunFinding{{Severity: "LOW", VulnerabilityID: "CVE-NEW", PackageName: "curl"}}},
		},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			"acme/api@sha256:old": {Findings: []ports.SecretFinding{{RuleID: "rule-old", Path: "old.json"}}},
			"acme/api@sha256:new": {Findings: []ports.SecretFinding{{RuleID: "rule-new", Path: "new.json"}}},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	if got, want := opened.adminView.ScanHistoryModal.Detail.Run.ID, "run-new"; got != want {
		t.Fatalf("initial cursor Detail.Run.ID = %q, want %q (newest run first)", got, want)
	}
	if adminClient.getScanRunDetailCalls != 1 || adminClient.getSecretScanFindingsCalls != 1 {
		t.Fatalf("calls after open = detail:%d secrets:%d, want 1/1", adminClient.getScanRunDetailCalls, adminClient.getSecretScanFindingsCalls)
	}

	paged := runKey(t, opened, "right")
	if got, want := paged.adminView.ScanHistoryModal.Cursor, 1; got != want {
		t.Fatalf("Cursor after right = %d, want %d", got, want)
	}
	if adminClient.getScanRunDetailCalls != 2 {
		t.Fatalf("getScanRunDetailCalls after paging = %d, want 2 (re-fired for the new cursor)", adminClient.getScanRunDetailCalls)
	}
	if adminClient.getSecretScanFindingsCalls != 2 {
		t.Fatalf("getSecretScanFindingsCalls after paging = %d, want 2 (re-fired for the new cursor)", adminClient.getSecretScanFindingsCalls)
	}
	if got, want := paged.adminView.ScanHistoryModal.Detail.Run.ID, "run-old"; got != want {
		t.Fatalf("Detail.Run.ID after paging = %q, want %q", got, want)
	}

	view := paged.View()
	if !strings.Contains(view, "CVE-OLD") || strings.Contains(view, "CVE-NEW") {
		t.Fatalf("view = %q, want only the navigated run's (run-old) findings on the Vulnerabilities tab", view)
	}
	if !strings.Contains(view, "Execution 2/2") {
		t.Fatalf("view = %q, want the position indicator to report 2/2 after paging to the oldest (last, newest-first) run", view)
	}

	leaksView := runKey(t, paged, "tab").View()
	if !strings.Contains(leaksView, "rule-old") || strings.Contains(leaksView, "rule-new") {
		t.Fatalf("view = %q, want only run-old's secret finding on the Leaks tab", leaksView)
	}

	back := runKey(t, paged, "left")
	if got, want := back.adminView.ScanHistoryModal.Detail.Run.ID, "run-new"; got != want {
		t.Fatalf("Detail.Run.ID after paging back = %q, want %q", got, want)
	}
}

// TestModelScanHistoryModalDigestNeverLeaksAcrossRunsWhenPagingHistory is the
// Phase 3 task 3.4/3.5 RED test for design.md's flagged risk ("Digest-per-run
// correctness"): a stale async detail response for a run the operator has
// already paged away from must be discarded, never overwriting the
// currently-navigated run's Detail/Leaks state. Constructed with manual,
// out-of-order Update() calls (not runKey's auto-run-to-completion helper)
// to reproduce the exact race a real async admin API client can hit.
func TestModelScanHistoryModalDigestNeverLeaksAcrossRunsWhenPagingHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	older := now.Add(-48 * time.Hour)
	newer := now.Add(-1 * time.Hour)
	runA := ports.ScanRun{ID: "run-a", Repository: "acme/api", Digest: "sha256:aaa", FinishedAt: &newer, CreatedAt: newer}
	runB := ports.ScanRun{ID: "run-b", Repository: "acme/api", Digest: "sha256:bbb", FinishedAt: &older, CreatedAt: older}
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{runA, runB},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-a": {Run: runA, Findings: []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-A"}}},
			"run-b": {Run: runB, Findings: []ports.ScanRunFinding{{Severity: "LOW", VulnerabilityID: "CVE-B"}}},
		},
	}

	model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
	model = runCmd(t, model, model.Init())
	model = runAdminLogin(t, model, "operator", "secret-pass")
	model = runKey(t, model, "f")
	model = runKey(t, model, "tab")

	// Open the modal (cursor 0 = run-a, newest first) but capture the
	// history-load Cmd instead of letting runKey auto-run its whole chain,
	// so the detail fetch it triggers can be interleaved manually below.
	updatedRaw, historyCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	afterEnter := updatedRaw.(Model)
	if historyCmd == nil {
		t.Fatal("expected loadAdminScanHistoryCmd after enter")
	}
	historyLoaded := historyCmd()
	updatedRaw, detailCmdForA := afterEnter.Update(historyLoaded)
	afterHistory := updatedRaw.(Model)
	if got, want := afterHistory.adminView.ScanHistoryModal.Runs[0].ID, "run-a"; got != want {
		t.Fatalf("cursor 0 run ID = %q, want %q (newest first)", got, want)
	}
	if detailCmdForA == nil {
		t.Fatal("expected loadAdminScanRunDetailCmd(run-a) after history loads")
	}

	// Before resolving run-a's in-flight detail Cmd, the operator pages to
	// run-b — this fires a SECOND (newer) detail Cmd for run-b.
	updatedRaw, detailCmdForB := afterHistory.Update(tea.KeyMsg{Type: tea.KeyRight})
	afterPage := updatedRaw.(Model)
	if got, want := afterPage.adminView.ScanHistoryModal.Cursor, 1; got != want {
		t.Fatalf("Cursor after right = %d, want %d", got, want)
	}
	if detailCmdForB == nil {
		t.Fatal("expected loadAdminScanRunDetailCmd(run-b) after paging")
	}

	// The STALE run-a response now arrives late (out of order) — it must be
	// discarded, not overwrite run-b's in-progress navigation.
	staleMsg := detailCmdForA()
	updatedRaw, staleFollowUp := afterPage.Update(staleMsg)
	afterStale := updatedRaw.(Model)
	if staleFollowUp != nil {
		t.Fatal("a discarded stale detail response must not chain into loadAdminSecretScanFindingsCmd")
	}
	if got := afterStale.adminView.ScanHistoryModal.Detail.Run.ID; got == "run-a" {
		t.Fatalf("Detail.Run.ID = %q, want the stale run-a response to be discarded (cursor has moved to run-b)", got)
	}

	// The genuine run-b response arrives next and IS applied.
	correctMsg := detailCmdForB()
	updatedRaw, _ = afterStale.Update(correctMsg)
	final := updatedRaw.(Model)
	if got, want := final.adminView.ScanHistoryModal.Detail.Run.ID, "run-b"; got != want {
		t.Fatalf("Detail.Run.ID = %q, want %q", got, want)
	}

	view := final.View()
	if !strings.Contains(view, "CVE-B") || strings.Contains(view, "CVE-A") {
		t.Fatalf("view = %q, want only run-b's findings, never a leaked run-a finding", view)
	}
}

// TestModelScanHistoryModalRendersWithinViewportAcrossHeights is the Phase 3
// task 3.7 RED test (spec.md "Modal fits at minimum height with full
// content"): the FIRST test that drives the modal open through the real
// Model.Update()/View() key-press flow (login -> f -> tab -> enter) with a
// full findings page, and asserts the resulting composite view never exceeds
// the terminal height — proving the actual wiring respects the viewport, not
// just renderAdminScanHistoryModal in isolation (Phase 2's tests).
func TestModelScanHistoryModalRendersWithinViewportAcrossHeights(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	findings := make([]ports.ScanRunFinding, 0, 40)
	for i := 0; i < 40; i++ {
		findings = append(findings, ports.ScanRunFinding{Severity: "HIGH", VulnerabilityID: fmt.Sprintf("CVE-2026-%04d", i), PackageName: "openssl"})
	}
	secrets := make([]ports.SecretFinding, 0, 40)
	for i := 0; i < 40; i++ {
		secrets = append(secrets, ports.SecretFinding{RuleID: fmt.Sprintf("rule-%d", i), Path: "config.json", StartLine: i + 1})
	}

	for _, height := range []int{24, 30, 50} {
		height := height
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			t.Parallel()

			adminClient := &fakeAdminClient{
				loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
				features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
				featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
				scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", RequestedRef: "latest", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted, Critical: 1, HasFixable: true}},
				scanRunDetails: map[string]ports.ScanRunDetail{
					"run-1": {Run: ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa"}, Findings: findings},
				},
				secretScanFindings: map[string]ports.SecretScanRunDetail{
					"acme/api@sha256:aaa": {Findings: secrets},
				},
			}

			model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}, WithAdminClient(adminClient))
			updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: height})
			result := updated.(Model)
			result = runCmd(t, result, result.Init())
			result = runAdminLogin(t, result, "operator", "secret-pass")
			result = runKey(t, result, "f")
			result = runKey(t, result, "tab")
			result = runKey(t, result, "enter")

			if !result.adminView.ScanHistoryModal.Open {
				t.Fatal("ScanHistoryModal.Open = false, want true after the real key-press flow opened it")
			}

			view := result.View()
			if got := lipgloss.Height(view); got > height {
				t.Fatalf("view height = %d, want <= %d (modal open with a full findings page)\nview:\n%s", got, height, view)
			}

			leaksView := runKey(t, result, "tab").View()
			if got := lipgloss.Height(leaksView); got > height {
				t.Fatalf("Leaks tab view height = %d, want <= %d\nview:\n%s", got, height, leaksView)
			}
		})
	}
}

// newModelForModalOverlayTest builds a minimally-populated, logged-in-shaped
// admin Users screen at the given viewport height, for the Phase 3 modal
// overlay unification tests below. Users (not Features) so the Confirm and
// Trivy modal cases share one small, realistic base screen.
func newModelForModalOverlayTest(t *testing.T, height int) Model {
	t.Helper()
	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: height})
	result := updated.(Model)
	result.screen = screenAdminUsers
	result.adminSession = AdminSession{Username: "operator", ExpiresAt: time.Now().Add(time.Hour)}
	result.adminView.Users = []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}}
	return result
}

// TestModelConfirmAndTrivyConfigModalOverlayFitsViewportAndDoesNotGrowPageHeight
// is the Phase 3 task 3.2 RED test (design.md Decision 1): under the
// pre-Decision-1 architecture, renderAdminWorkspace appends the Confirm/Trivy
// modal BELOW the already-full-size base body via lipgloss.JoinVertical, so
// opening either modal grows total page height past the viewport. Once
// composited via compositeOverlay, the result is always clamped to exactly
// the canvas (layout.Width x layout.Height) — the same invariant
// TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen
// already established for the scan history modal, extended here to
// Confirm/Trivy.
func TestModelConfirmAndTrivyConfigModalOverlayFitsViewportAndDoesNotGrowPageHeight(t *testing.T) {
	t.Parallel()

	for _, height := range []int{24, 30, 40, 50, 60} {
		height := height

		t.Run(fmt.Sprintf("height=%d/confirm", height), func(t *testing.T) {
			t.Parallel()

			model := newModelForModalOverlayTest(t, height)
			model.adminView.ConfirmModal = adminConfirmModal{
				Kind: adminConfirmEnableUser, Title: "Enable User", Message: "Enable alice?", ConfirmText: "enable",
			}
			openView := model.View()
			if got := lipgloss.Height(openView); got > height {
				t.Fatalf("view height with Confirm modal open = %d, want <= %d (viewport height)\n%s", got, height, openView)
			}
			if got := lipgloss.Height(openView); got != height {
				t.Fatalf("view height with Confirm modal open = %d, want exactly %d (compositeOverlay clamps to the canvas — no unbudgeted page-height growth)", got, height)
			}
		})

		t.Run(fmt.Sprintf("height=%d/trivy", height), func(t *testing.T) {
			t.Parallel()

			model := newModelForModalOverlayTest(t, height)
			model.adminView.TrivyConfigModal = trivyConfigModal{
				Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
				RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
			}
			openView := model.View()
			if got := lipgloss.Height(openView); got > height {
				t.Fatalf("view height with Trivy config modal open = %d, want <= %d (viewport height)\n%s", got, height, openView)
			}
			if got := lipgloss.Height(openView); got != height {
				t.Fatalf("view height with Trivy config modal open = %d, want exactly %d (compositeOverlay clamps to the canvas — no unbudgeted page-height growth)", got, height)
			}
		})
	}
}

// TestModelTrivyConfigModalRendersFullBottomBorderAndHelpLineAtViewportFloor
// is the Phase 3 task 3.3 RED test: at the 24-row viewport floor, the
// composited Trivy config modal must still show its own closing border and
// its "Enter: save | ... | Esc: cancel" help line — proof the modal is never
// silently clipped by the viewport-height guard now that it is a floating
// overlay rather than appended body content.
func TestModelTrivyConfigModalRendersFullBottomBorderAndHelpLineAtViewportFloor(t *testing.T) {
	t.Parallel()

	model := newModelForModalOverlayTest(t, minViewportHeight)
	model.adminView.TrivyConfigModal = trivyConfigModal{
		Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
		RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
	}

	view := model.View()
	if got := lipgloss.Height(view); got > minViewportHeight {
		t.Fatalf("view height = %d, want <= %d (viewport floor)\n%s", got, minViewportHeight, view)
	}
	if !strings.Contains(view, "╰") {
		t.Fatalf("view = %q, want the Trivy config modal's own closing border rune present at the viewport floor", view)
	}
	if !strings.Contains(view, "Enter: save | Tab: next field | Space: toggle | Esc: cancel") {
		t.Fatalf("view = %q, want the modal's help line present at the viewport floor", view)
	}
}

// assertNoLineExceedsWidthOrCorruptsBorders is the shared width-boundary
// assertion (mirrors admin_overlay_test.go's
// TestCompositeOverlayIsANSISafeAcrossSplicedStyledBlocks and
// admin_tables_test.go's declaredWidth pattern): an ANSI-corrupted line can
// measure wider than the canvas it was composited into, so asserting every
// line's lipgloss.Width stays within the terminal width is this codebase's
// established way of catching corrupted/mismatched border runes, not just
// literal overflow.
func assertNoLineExceedsWidthOrCorruptsBorders(t *testing.T, view string, width int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d width = %d, want <= %d (terminal width) -- an ANSI-corrupted or overflowing line:\n%s", i, w, width, view)
		}
	}
}

// TestModelScanHistoryModalRendersWithinViewportAcrossWidths is the
// executions-side-panel RED test mirroring
// TestModelScanHistoryModalRendersWithinViewportAcrossHeights but varying
// WIDTH instead of height: with adminScanHistoryWindowLimit-scale history (20
// runs) and a full findings/secrets page loaded through the real
// Model.Update()/View() key-press flow, the composited view's every line
// must stay within the terminal width at minViewportWidth (the floor, raised
// for this side panel) and comfortably above it, on both tabs.
func TestModelScanHistoryModalRendersWithinViewportAcrossWidths(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	findings := make([]ports.ScanRunFinding, 0, 20)
	for i := 0; i < 20; i++ {
		findings = append(findings, ports.ScanRunFinding{Severity: "HIGH", VulnerabilityID: fmt.Sprintf("CVE-2026-%04d", i), PackageName: "openssl", InstalledVersion: "1.1.1", FixedVersion: "1.1.2"})
	}
	secrets := make([]ports.SecretFinding, 0, 20)
	for i := 0; i < 20; i++ {
		secrets = append(secrets, ports.SecretFinding{
			RuleID:      fmt.Sprintf("rule-%d", i),
			Path:        "config.json",
			StartLine:   i + 1,
			Description: "A leaked credential was detected in this file",
			Tags:        []string{"critical", "credentials"},
		})
	}
	runs := make([]ports.ScanRun, 0, 20)
	for i := 0; i < 20; i++ {
		finished := now.Add(-time.Duration(i) * 24 * time.Hour)
		runs = append(runs, ports.ScanRun{ID: fmt.Sprintf("run-%d", i), Repository: "acme/api", RequestedRef: "latest", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted, Critical: 1, HasFixable: true, FinishedAt: &finished, CreatedAt: finished})
	}

	for _, width := range []int{minViewportWidth, 160, 200} {
		width := width
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			t.Parallel()

			adminClient := &fakeAdminClient{
				loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
				features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
				featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
				scanRuns:     runs,
				scanRunDetails: map[string]ports.ScanRunDetail{
					"run-0": {Run: ports.ScanRun{ID: "run-0", Repository: "acme/api", Digest: "sha256:aaa"}, Findings: findings},
				},
				secretScanFindings: map[string]ports.SecretScanRunDetail{
					"acme/api@sha256:aaa": {Findings: secrets},
				},
			}

			model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}, WithAdminClient(adminClient))
			updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: defaultViewportHeight})
			result := updated.(Model)
			result = runCmd(t, result, result.Init())
			result = runAdminLogin(t, result, "operator", "secret-pass")
			result = runKey(t, result, "f")
			result = runKey(t, result, "tab")
			result = runKey(t, result, "enter")

			if !result.adminView.ScanHistoryModal.Open {
				t.Fatal("ScanHistoryModal.Open = false, want true after the real key-press flow opened it")
			}
			if got, want := len(result.adminView.ScanHistoryModal.Runs), 20; got != want {
				t.Fatalf("len(Runs) = %d, want %d (adminScanHistoryWindowLimit-scale history loaded)", got, want)
			}

			view := result.View()
			assertNoLineExceedsWidthOrCorruptsBorders(t, view, width)
			if !strings.Contains(view, "Executions") {
				t.Fatalf("view = %q, want the executions rail present on the Vulnerabilities tab", view)
			}

			leaksView := runKey(t, result, "tab").View()
			assertNoLineExceedsWidthOrCorruptsBorders(t, leaksView, width)
			if !strings.Contains(leaksView, "Executions") {
				t.Fatalf("leaksView = %q, want the executions rail present on the Leaks tab too", leaksView)
			}
		})
	}
}

// TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator
// is the Phase 6 RED test (tasks.md 6.5, spec.md "Operator reviews findings
// for a selected image"): opening a Trivy scan run's detail must also load
// and render that same image's redacted secret-scan findings (keyed by the
// same repository+digest both scan legs share), and the secret findings
// table must never carry a severity or gating column/styling — the
// resolved product decision (proposal.md) is informational-only.
func TestModelSecretFindingsSurfaceAlongsideVulnerabilityResultsWithoutSeverityOrGatingIndicator(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 11, 16, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
		scanRuns: []ports.ScanRun{
			{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:111", Status: ports.ScanRunStatusCompleted, Critical: 1, HasFixable: true},
		},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run:      ports.ScanRun{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Digest: "sha256:111", Status: ports.ScanRunStatusCompleted, Critical: 1},
				Findings: []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-0099", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}},
			},
		},
		secretScanFindings: map[string]ports.SecretScanRunDetail{
			"library/alpine@sha256:111": {
				Run:      ports.SecretScanRun{ID: "secret-run-1", Repository: "library/alpine", Digest: "sha256:111", Status: ports.SecretScanRunStatusCompleted},
				Findings: []ports.SecretFinding{{RuleID: "aws-access-token", Path: "config.json", StartLine: 4, EndLine: 4}},
			},
		},
	}

	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "enter")

	if adminClient.getSecretScanFindingsCalls != 1 {
		t.Fatalf("getSecretScanFindingsCalls = %d, want secret findings loaded alongside the vulnerability detail", adminClient.getSecretScanFindingsCalls)
	}
	if got, want := adminClient.lastSecretScanFindingsQuery, "library/alpine@sha256:111"; got != want {
		t.Fatalf("secret findings query = %q, want %q (same repository+digest as the vulnerability scan run)", got, want)
	}

	// Enter now opens the scan history modal (spec.md "Repository Alert
	// Drill-Down Opens History Modal"), defaulting to the Vulnerabilities
	// tab; the secret findings surface on the Leaks tab, reachable via Tab.
	view := updated.View()
	for _, want := range []string{"CVE-2026-0099", "Vulnerabilities", "Leaks"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q on the modal's default Vulnerabilities tab", view, want)
		}
	}
	if strings.Contains(view, "aws-access-token") {
		t.Fatalf("view = %q, want secret findings hidden while Vulnerabilities is the active tab", view)
	}

	leaksTab := runKey(t, updated, "tab")
	leaksView := leaksTab.View()
	for _, want := range []string{"aws-access-token", "config.json:4"} {
		if !strings.Contains(leaksView, want) {
			t.Fatalf("view = %q, want %q on the Leaks tab", leaksView, want)
		}
	}

	// No severity or gating indicator for the secret finding: the secret
	// findings table only declares rule/location columns, never the
	// severity/fixable columns the vulnerability findings table has.
	secretFindingsTable := updated.adminView.Tables.SecretFindings
	if got, want := secretFindingsTable.TotalRows(), 1; got != want {
		t.Fatalf("secret findings table rows = %d, want %d", got, want)
	}
	row := secretFindingsTable.HighlightedRow().Data
	for _, bannedKey := range []string{adminTableColumnFindingSeverity, adminTableColumnFindingFixable, "severity", "fixable", "gate", "blocking"} {
		if _, ok := row[bannedKey]; ok {
			t.Fatalf("secret finding row = %#v, must not carry a severity/gating column %q", row, bannedKey)
		}
	}
	if _, styled := row[adminTableColumnSecretFindingRule].(bubbletable.StyledCell); styled {
		t.Fatal("secret finding rule cell must stay neutral text, never severity-derived styling")
	}
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

		if got, want := updated.adminView.Tables.ScanSummary.TotalRows(), 0; got != want {
			t.Fatalf("scan-summary table rows = %d, want %d", got, want)
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

		if got, want := updated.adminView.Tables.ScanSummary.TotalRows(), 3; got != want {
			t.Fatalf("scan-summary table rows = %d, want %d", got, want)
		}
		if got, want := updated.adminView.Tables.ScanSummary.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
			t.Fatalf("highlighted scan-summary metadata = %#v, want %q", got, want)
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
	findingsTable := buildAdminFindingsTable(theme, []ports.ScanRunFinding{{Severity: "CRITICAL", VulnerabilityID: "CVE-2026-0001", PackageName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Fixable: true}}, 0, compactTableRows)
	severityCell, ok := findingsTable.HighlightedRow().Data[adminTableColumnFindingSeverity].(bubbletable.StyledCell)
	if !ok {
		t.Fatalf("finding severity cell type = %T, want bubble-table styled cell", findingsTable.HighlightedRow().Data[adminTableColumnFindingSeverity])
	}
	if got, want := severityCell.Data, "CRITICAL"; got != want {
		t.Fatalf("finding severity data = %#v, want %q", got, want)
	}

	featuresTable := buildAdminFeaturesTable(theme, []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}}, 0, defaultViewportHeight)
	if _, styled := featuresTable.HighlightedRow().Data[adminTableColumnFeatureEnabled].(bubbletable.StyledCell); styled {
		t.Fatal("feature summary cells must stay neutral")
	}

	rowsTable := buildAdminFeatureRowsTable(theme, ports.FeatureSection{ID: "checks", Title: "Checks", Kind: "rows", Rows: []ports.FeatureRow{{Title: "DB freshness", Status: "stale", Detail: "older than 24h"}}}, compactTableRows)
	if _, styled := rowsTable.HighlightedRow().Data[adminTableColumnRowStatus].(bubbletable.StyledCell); styled {
		t.Fatal("generic row status cells must stay neutral")
	}

	scanSummaryTable := buildAdminScanSummaryTable(theme, []repositorySummary{{Repository: "team/api", LatestRun: ports.ScanRun{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: true}, RunCount: 1}}, 0, defaultViewportHeight)
	if _, styled := scanSummaryTable.HighlightedRow().Data[adminTableColumnScanSummaryStatus].(bubbletable.StyledCell); styled {
		t.Fatal("scan-summary cells must stay neutral")
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
	// "Findings" (the old inline-detail heading) is deliberately not
	// asserted here: Enter now opens the scan history modal (spec.md
	// "Repository Alert Drill-Down Opens History Modal"), whose
	// Vulnerabilities tab renders the findings table directly without that
	// heading — the CVE/package assertions below still prove the same
	// severity-styled rows render, just inside the modal.
	for _, want := range []string{"Scan History", "CVE-2026-3000", "CVE-2026-3001", "CVE-2026-3002", "CVE-2026-3003", "openssl", "glibc", "ca-certificates", "busybox"} {
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

	if got, want := updated.adminView.Tables.ScanSummary.GetHighlightedRowIndex(), 1; got != want {
		t.Fatalf("scan-summary highlighted index = %d, want %d", got, want)
	}
	if got, want := updated.adminView.Tables.ScanSummary.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
		t.Fatalf("highlighted scan-summary metadata = %#v, want %q", got, want)
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

// TestModelAdminScanRunsTablePagesOnArrowKeyNavigationPastPageBoundary closes
// sdd-verify's CRITICAL-01 finding for the tui-table-viewport-fixed-size
// change: spec.md "Internal Table and List Scrolling", scenario "Long table
// pages internally" had zero covering tests. The existing arrow-key admin
// table test above uses only a 2-row ScanSummary table, which can never cross
// a page boundary. This test builds a ScanSummary table with more rows than its
// computed pageSize, presses "down" past the first page boundary, and
// verifies the mechanism design.md decision #5 describes:
// syncAdminTableHighlights()'s WithHighlightedRow(...) call auto-pages the
// live bubble-table (no PgUp/PgDn forwarding into table.Update, which is
// never called), keeping the highlighted row visible and the surrounding
// screen chrome (title, table header, help) unchanged.
func TestModelAdminScanRunsTablePagesOnArrowKeyNavigationPastPageBoundary(t *testing.T) {
	t.Parallel()

	const totalRuns = 30
	scanRuns := make([]ports.ScanRun, 0, totalRuns)
	for i := 0; i < totalRuns; i++ {
		scanRuns = append(scanRuns, ports.ScanRun{
			ID:           fmt.Sprintf("run-%02d", i),
			Repository:   fmt.Sprintf("team/service-%02d", i),
			RequestedRef: "latest",
			Status:       ports.ScanRunStatusCompleted,
		})
	}

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)},
		features:     []ports.FeatureSummary{{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: trivyFeatureName, Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
		scanRuns: scanRuns,
	}

	// A small, realistic viewport (the spec's minimum supported size) keeps
	// the computed pageSize well below totalRuns so pagination genuinely
	// activates, instead of a contrived pageSize.
	model := NewModel(&fakeQueryService{}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: minViewportWidth, Height: minViewportHeight}
	ready := runCmd(t, model, model.Init())

	updated := runAdminLogin(t, ready, "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "tab")

	if updated.status != "" {
		t.Fatalf("status = %q, want the repository-alerts load settled (empty) before navigating", updated.status)
	}

	primaryPageSize := updated.adminView.Layout.Primary
	if primaryPageSize <= 0 || primaryPageSize >= totalRuns {
		t.Fatalf("primary pageSize = %d, want a positive size smaller than %d rows so pagination genuinely activates", primaryPageSize, totalRuns)
	}

	beforeTable := updated.adminView.Tables.ScanSummary
	if got, want := beforeTable.CurrentPage(), 1; got != want {
		t.Fatalf("scan-summary table CurrentPage() before navigation = %d, want %d", got, want)
	}
	firstPageOnlyRepository, ok := beforeTable.HighlightedRow().Data[adminTableColumnScanSummaryRepository].(string)
	if !ok || firstPageOnlyRepository == "" {
		t.Fatalf("highlighted row repository before navigation = %#v, want a non-empty string", beforeTable.HighlightedRow().Data[adminTableColumnScanSummaryRepository])
	}
	beforeView := updated.View()
	beforeTableView := beforeTable.View()
	wantBeforeIndicator := fmt.Sprintf("%d/%d", 1, beforeTable.MaxPages())
	if !strings.Contains(beforeTableView, wantBeforeIndicator) {
		t.Fatalf("scan-summary table view before navigation = %q, want position indicator %q", beforeTableView, wantBeforeIndicator)
	}

	// Press "down" exactly pageSize times: this moves the highlighted row
	// from index 0 to index pageSize, the first row of the second page.
	for i := 0; i < primaryPageSize; i++ {
		updated = runKey(t, updated, "down")
	}

	afterTable := updated.adminView.Tables.ScanSummary
	if got, want := afterTable.GetHighlightedRowIndex(), primaryPageSize; got != want {
		t.Fatalf("highlighted row index after %d downs = %d, want %d", primaryPageSize, got, want)
	}
	if got, want := afterTable.CurrentPage(), 2; got != want {
		t.Fatalf("scan-summary table CurrentPage() after paging past the first page boundary = %d, want %d (design.md decision #5: WithHighlightedRow must auto-page the table)", got, want)
	}

	afterView := updated.View()
	afterTableView := afterTable.View()
	wantAfterIndicator := fmt.Sprintf("%d/%d", 2, afterTable.MaxPages())
	if !strings.Contains(afterTableView, wantAfterIndicator) {
		t.Fatalf("scan-summary table view after paging = %q, want position indicator %q reflecting the new page", afterTableView, wantAfterIndicator)
	}

	highlightedRepository, ok := afterTable.HighlightedRow().Data[adminTableColumnScanSummaryRepository].(string)
	if !ok || highlightedRepository == "" {
		t.Fatalf("highlighted row repository after paging = %#v, want a non-empty string", afterTable.HighlightedRow().Data[adminTableColumnScanSummaryRepository])
	}
	if !strings.Contains(afterTableView, highlightedRepository) {
		t.Fatalf("scan-summary table view after paging = %q, want the newly highlighted row %q to be genuinely visible on-screen", afterTableView, highlightedRepository)
	}
	if strings.Contains(afterTableView, firstPageOnlyRepository) {
		t.Fatalf("scan-summary table view after paging = %q, want first-page-only row %q to have scrolled off, not still be visible", afterTableView, firstPageOnlyRepository)
	}

	// The table header and surrounding screen chrome (title, help) must
	// remain visible and unchanged: same overall height, same title, same
	// help text, same table column headers, before and after paging.
	if got, want := lipgloss.Height(afterView), lipgloss.Height(beforeView); got != want {
		t.Fatalf("full screen height after paging = %d, want unchanged %d", got, want)
	}
	help := adminFeatureHelp(updated.adminView)
	if !strings.Contains(beforeView, help) || !strings.Contains(afterView, help) {
		t.Fatalf("help text %q must remain present and unchanged before/after paging\nbefore: %q\nafter: %q", help, beforeView, afterView)
	}
	if !strings.Contains(beforeView, "Regixtry Admin") || !strings.Contains(afterView, "Regixtry Admin") {
		t.Fatalf("screen title must remain visible and unchanged before/after paging\nbefore: %q\nafter: %q", beforeView, afterView)
	}
	if !strings.Contains(beforeTableView, "Repository") || !strings.Contains(afterTableView, "Repository") {
		t.Fatalf("table header must remain visible before/after paging\nbefore: %q\nafter: %q", beforeTableView, afterTableView)
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
	secretScanFindings    map[string]ports.SecretScanRunDetail
	secretScanFindingsErr error
	featureAfterConfigure ports.FeatureDetails
	scanPolicy            ports.ScanPolicySettings
	scanPolicyErr         error
	scanPolicyUpdateErr   error
	getScanPolicyCalls    int
	updateScanPolicyCalls int
	lastScanPolicyInput   ports.ScanPolicySettings
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

	loginCalls                  int
	listFeaturesCalls           int
	getFeatureCalls             int
	getFeatureStatusCalls       int
	getFeaturePageCalls         int
	listScanRunsCalls           int
	getScanRunDetailCalls       int
	getSecretScanFindingsCalls  int
	lastSecretScanFindingsQuery string
	executeFeatureActionCalls   int
	lastFeatureAction           string
	installRuntimeCalls         int
	upgradeRuntimeCalls         int
	rollbackRuntimeCalls        int
	enableFeatureCalls          int
	disableFeatureCalls         int
	listUsersCalls              int
	listGrantsCalls             int
	listTokensCalls             int
	createUserCalls             int
	resetPasswordCalls          int
	configureFeatureCalls       int
	putGrantCalls               int
	deleteGrantCalls            int
	createTokenCalls            int
	revokeTokenCalls            int
	enableCalls                 int
	disableCalls                int

	repositoryOverrides           map[string]ports.RepositoryOverrideDetails // key: feature+"/"+repository
	repositoryOverrideList        map[string][]ports.RepositoryOverrideDetails
	getRepositoryOverrideErr      error
	setRepositoryOverrideErr      error
	clearRepositoryOverrideErr    error
	listRepositoryOverridesErr    error
	getRepositoryOverrideCalls    int
	listRepositoryOverridesCalls  int
	setRepositoryOverrideCalls    int
	clearRepositoryOverrideCalls  int
	lastSetRepositoryOverride     ports.RepositoryOverrideDetails
	lastRepositoryOverrideRepo    string
	lastRepositoryOverrideFeature string
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

// ListScanRuns filters by repository when one is given, mirroring the real
// backend's ListScanRuns/store.go behavior (`AND repository = ?` only when
// repository is non-empty) — needed so loadAdminScanHistoryCmd's per-
// repository history window returns only that repository's runs in tests,
// same as it would against the real store.
func (f *fakeAdminClient) ListScanRuns(_ context.Context, _ AdminSession, repository string, _ int) ([]ports.ScanRun, error) {
	f.listScanRunsCalls++
	if f.featureErr != nil {
		return nil, f.featureErr
	}
	if strings.TrimSpace(repository) == "" {
		return append([]ports.ScanRun(nil), f.scanRuns...), nil
	}
	filtered := make([]ports.ScanRun, 0, len(f.scanRuns))
	for _, run := range f.scanRuns {
		if run.Repository == repository {
			filtered = append(filtered, run)
		}
	}
	return filtered, nil
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

func (f *fakeAdminClient) GetSecretScanFindings(_ context.Context, _ AdminSession, repository string, digest string) (ports.SecretScanRunDetail, error) {
	f.getSecretScanFindingsCalls++
	f.lastSecretScanFindingsQuery = repository + "@" + digest
	if f.secretScanFindingsErr != nil {
		return ports.SecretScanRunDetail{}, f.secretScanFindingsErr
	}
	if detail, ok := f.secretScanFindings[f.lastSecretScanFindingsQuery]; ok {
		return detail, nil
	}
	return ports.SecretScanRunDetail{}, nil
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

func (f *fakeAdminClient) GetScanPolicy(context.Context, AdminSession) (ports.ScanPolicySettings, error) {
	f.getScanPolicyCalls++
	if f.scanPolicyErr != nil {
		return ports.ScanPolicySettings{}, f.scanPolicyErr
	}
	return f.scanPolicy, nil
}

func (f *fakeAdminClient) UpdateScanPolicy(_ context.Context, _ AdminSession, input ports.ScanPolicySettings) (ports.ScanPolicySettings, error) {
	f.updateScanPolicyCalls++
	f.lastScanPolicyInput = input
	if f.scanPolicyUpdateErr != nil {
		return ports.ScanPolicySettings{}, f.scanPolicyUpdateErr
	}
	f.scanPolicy = input
	return f.scanPolicy, nil
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

func (f *fakeAdminClient) GetRepositoryOverride(_ context.Context, _ AdminSession, repository string, feature string) (ports.RepositoryOverrideDetails, bool, error) {
	f.getRepositoryOverrideCalls++
	f.lastRepositoryOverrideRepo = repository
	f.lastRepositoryOverrideFeature = feature
	if f.getRepositoryOverrideErr != nil {
		return ports.RepositoryOverrideDetails{}, false, f.getRepositoryOverrideErr
	}
	if f.repositoryOverrides == nil {
		return ports.RepositoryOverrideDetails{}, false, nil
	}
	details, ok := f.repositoryOverrides[feature+"/"+repository]
	return details, ok, nil
}

func (f *fakeAdminClient) ListRepositoryOverrides(_ context.Context, _ AdminSession, feature string) ([]ports.RepositoryOverrideDetails, error) {
	f.listRepositoryOverridesCalls++
	if f.listRepositoryOverridesErr != nil {
		return nil, f.listRepositoryOverridesErr
	}
	return append([]ports.RepositoryOverrideDetails(nil), f.repositoryOverrideList[feature]...), nil
}

func (f *fakeAdminClient) SetRepositoryOverride(_ context.Context, _ AdminSession, repository string, feature string, input ports.RepositoryOverrideDetails) (ports.RepositoryOverrideDetails, error) {
	f.setRepositoryOverrideCalls++
	f.lastSetRepositoryOverride = input
	f.lastRepositoryOverrideRepo = repository
	f.lastRepositoryOverrideFeature = feature
	if f.setRepositoryOverrideErr != nil {
		return ports.RepositoryOverrideDetails{}, f.setRepositoryOverrideErr
	}
	input.Repository = repository
	input.Feature = feature
	if f.repositoryOverrides == nil {
		f.repositoryOverrides = map[string]ports.RepositoryOverrideDetails{}
	}
	f.repositoryOverrides[feature+"/"+repository] = input
	return input, nil
}

func (f *fakeAdminClient) ClearRepositoryOverride(_ context.Context, _ AdminSession, repository string, feature string) error {
	f.clearRepositoryOverrideCalls++
	f.lastRepositoryOverrideRepo = repository
	f.lastRepositoryOverrideFeature = feature
	if f.clearRepositoryOverrideErr != nil {
		return f.clearRepositoryOverrideErr
	}
	delete(f.repositoryOverrides, feature+"/"+repository)
	return nil
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

// adminTestViewportHeight is deliberately generous (well above the
// defaultViewportHeight used by --snapshot/NewModel): most admin tests below
// assert on deeply-nested content (Trivy scan detail, secret findings) that
// is orthogonal to viewport-bounding — Phase 4's own containment tests
// (e.g. TestModelTrivyRepositoryAlertsScreenFitsViewportHeight) set their
// own small viewport explicitly instead of using this helper.
const adminTestViewportHeight = 200

func newAdminReadyModel(t *testing.T, adminClient AdminClient) Model {
	t.Helper()
	model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: []string{"library/alpine"}}}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
	return runCmd(t, model, model.Init())
}

func newAdminReadyModelWithCatalog(t *testing.T, repositories []string, adminClient AdminClient) Model {
	t.Helper()
	model := NewModel(&fakeQueryService{catalog: appregixtry.CatalogResult{Repositories: append([]string(nil), repositories...)}}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
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
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		msg = tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		msg = tea.KeyMsg{Type: tea.KeyEnd}
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

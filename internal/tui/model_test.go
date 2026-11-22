package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	bubbletable "github.com/evertras/bubble-table/table"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

func TestModelShowsEmptyStateWhenCatalogIsEmpty(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated := runCmd(t, model, model.Init())

	if updated.screen != screenEmpty {
		t.Fatalf("screen = %q, want %q", updated.screen, screenEmpty)
	}
	view := updated.View().Content
	if !strings.Contains(view, "Regixtry is empty") {
		t.Fatalf("view = %q, want empty-state title", view)
	}
}

func TestModelStartupLoginDefersCatalogUntilLoginSucceeds(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}
	model := NewModel(service, WithAdminClient(&fakeAdminClient{loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2099, time.August, 9, 12, 0, 0, 0, time.UTC)}}), WithStartupLogin())

	model = runCmd(t, model, model.Init())
	if got, want := service.calls.catalog, 0; got != want {
		t.Fatalf("catalog calls = %d, want %d before login", got, want)
	}
	if got, want := model.screen, screenAdminLogin; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "operator sign in") {
		t.Fatalf("view = %q, want login shell", model.View().Content)
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
	if strings.Contains(updated.View().Content, "alice [admin, enabled]") {
		t.Fatalf("view = %q, want repository flow instead of admin users", updated.View().Content)
	}
	if !strings.Contains(updated.View().Content, "Repositories") || !strings.Contains(updated.View().Content, "library/alpine") {
		t.Fatalf("view = %q, want repositories shell after login", updated.View().Content)
	}

	updated = runKey(t, updated, "tab")
	if got, want := updated.screen, screenAdminOverview; got != want {
		t.Fatalf("screen = %q, want %q after opening admin", got, want)
	}
}

func TestModelLocalStartupLoadsCatalogImmediately(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}
	model := NewModel(service, WithAdminClient(&fakeAdminClient{}))
	updated := runCmd(t, model, model.Init())

	if got, want := service.calls.catalog, 1; got != want {
		t.Fatalf("catalog calls = %d, want %d", got, want)
	}
	if got, want := updated.screen, screenRepositories; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	view := updated.View().Content
	if !strings.Contains(view, "Regixtry Console") || !strings.Contains(view, "Enter: open tags | d: delete repository | p: projects | Tab: admin | g: repo grants | q: quit") {
		t.Fatalf("view = %q, want unified repository shell", view)
	}
}

// TestModelChecksForUpdatesAfterStartupResolves guards the follow-up-Cmd
// chaining convention (not tea.Batch, to keep --snapshot mode's single
// Update call working, model.go's own established convention): once
// catalogLoadedMsg resolves the initial screen, Update must also return a
// non-nil Cmd for the background version check, on every branch (error,
// empty, and normal), so the check fires regardless of what the catalog
// contained. Deliberately does NOT invoke the returned Cmd (that would
// perform a real network call) -- only checkForUpdateCmd's own nilness is
// under test here.
func TestModelChecksForUpdatesAfterStartupResolves(t *testing.T) {
	t.Parallel()

	t.Run("normal catalog", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}, WithCurrentVersion("v0.2.0"))
		_, cmd := model.Update(catalogLoadedMsg{result: []appregixtry.RepositorySummary{{Name: "library/alpine"}}})
		if cmd == nil {
			t.Fatal("Update(catalogLoadedMsg) returned a nil Cmd, want the chained update-check Cmd")
		}
	})

	t.Run("empty catalog", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		_, cmd := model.Update(catalogLoadedMsg{})
		if cmd == nil {
			t.Fatal("Update(catalogLoadedMsg{empty}) returned a nil Cmd, want the chained update-check Cmd")
		}
	})

	t.Run("catalog load error", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		_, cmd := model.Update(catalogLoadedMsg{err: errors.New("boom")})
		if cmd == nil {
			t.Fatal("Update(catalogLoadedMsg{err}) returned a nil Cmd, want the chained update-check Cmd")
		}
	})

	t.Run("startup login path", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"), WithStartupLogin())
		_, cmd := model.Update(startupLoginMsg{})
		if cmd == nil {
			t.Fatal("Update(startupLoginMsg) returned a nil Cmd, want the chained update-check Cmd")
		}
	})

	t.Run("no current version known: never fires a request", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}})
		_, cmd := model.Update(catalogLoadedMsg{result: []appregixtry.RepositorySummary{{Name: "library/alpine"}}})
		if cmd != nil {
			t.Fatal("Update(catalogLoadedMsg) returned a non-nil Cmd with no current version set, want nil -- nothing to compare against")
		}
	})
}

// TestModelUpdateCheckCompletedMsgUpdatesBannerState covers
// updateCheckCompletedMsg's handler directly (no real I/O): a genuinely
// newer version populates the banner text in View(); an error, an
// unparseable candidate, or an already-up-to-date result must never show a
// banner or surface an error to the operator.
func TestModelUpdateCheckCompletedMsgUpdatesBannerState(t *testing.T) {
	t.Parallel()

	t.Run("newer version shows the banner on every screen", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		updated, cmd := model.Update(updateCheckCompletedMsg{latestVersion: "v0.3.0"})
		result := updated.(Model)
		if cmd != nil {
			t.Fatalf("Update(updateCheckCompletedMsg) returned a non-nil Cmd, want nil (terminal, no further chaining)")
		}
		view := result.View().Content
		if !strings.Contains(view, "v0.3.0") {
			t.Fatalf("view = %q, want it to mention the newer version v0.3.0", view)
		}
	})

	t.Run("fetch error never shows a banner or surfaces an error", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		updated, _ := model.Update(updateCheckCompletedMsg{err: errors.New("network unreachable")})
		result := updated.(Model)
		if result.err != nil {
			t.Fatalf("model.err = %v, want nil -- a failed update check must never surface as a fatal error", result.err)
		}
		if strings.Contains(result.View().Content, "network unreachable") {
			t.Fatalf("view leaked the update-check error, want it silently swallowed")
		}
	})

	t.Run("already up to date shows no banner", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		updated, _ := model.Update(updateCheckCompletedMsg{latestVersion: "v0.2.0"})
		result := updated.(Model)
		if result.updateAvailable != "" {
			t.Fatalf("updateAvailable = %q, want empty -- already on the latest version", result.updateAvailable)
		}
	})

	t.Run("an unparseable candidate never shows a banner", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		updated, _ := model.Update(updateCheckCompletedMsg{latestVersion: "not-a-version"})
		result := updated.(Model)
		if result.updateAvailable != "" {
			t.Fatalf("updateAvailable = %q, want empty for an unparseable candidate", result.updateAvailable)
		}
	})
}

// TestModelWithUpdateBannerShowsBaselineVersion guards the tui-update-check
// feature's baseline version line (user-confirmed requirement: an operator
// should never have to guess which build they're running just because
// nothing newer happens to exist yet). Renders on every screen (View's
// funnel through withUpdateBanner), never fabricated when currentVersion is
// unknown.
func TestModelWithUpdateBannerShowsBaselineVersion(t *testing.T) {
	t.Parallel()

	t.Run("shows a standalone baseline line when no update is available", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		view := model.View().Content
		if !strings.Contains(view, "regixtry v0.2.0") {
			t.Fatalf("view = %q, want a baseline \"regixtry v0.2.0\" line", view)
		}
		if strings.Contains(view, "newer") {
			t.Fatalf("view = %q, want no \"newer version\" wording with nothing newer known", view)
		}
	})

	t.Run("folds the current version into the update-available banner instead of a second line", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{}, WithCurrentVersion("v0.2.0"))
		updated, _ := model.Update(updateCheckCompletedMsg{latestVersion: "v0.3.0"})
		result := updated.(Model)
		view := result.View().Content
		if !strings.Contains(view, "you're on v0.2.0") {
			t.Fatalf("view = %q, want the current version folded into the update-available banner", view)
		}
		if strings.Count(view, "v0.2.0") != 1 {
			t.Fatalf("view = %q, want v0.2.0 mentioned exactly once (no separate baseline line once an update is known)", view)
		}
	})

	t.Run("shows nothing when the current version is unknown", func(t *testing.T) {
		t.Parallel()
		model := NewModel(&fakeQueryService{})
		view := model.View().Content
		if strings.Contains(view, "regixtry v") {
			t.Fatalf("view = %q, want no fabricated version line when currentVersion is empty", view)
		}
	})
}

// TestCheckForUpdateCmdResolvesChannelFromServiceBeforeFetchingReleases
// guards the tui-update-check feature's channel resolution: the channel is
// now a real server-side setting (PUT /admin/v1/update-channel is the only
// write path, admin-gated), never client-side Model state, so
// checkForUpdateCmd must resolve it fresh via QueryService.GetUpdateChannel
// -- called directly in-process, not over HTTP -- before it can even
// attempt the GitHub fetch. Forcing GetUpdateChannel to fail lets this be
// verified WITHOUT a real network call: the closure must short-circuit on
// that error before ever reaching release.LatestForChannel.
func TestCheckForUpdateCmdResolvesChannelFromServiceBeforeFetchingReleases(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{updateChannelErr: errors.New("metadata store unavailable")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"))
	cmd := model.checkForUpdateCmd()
	if cmd == nil {
		t.Fatal("checkForUpdateCmd() = nil, want a non-nil Cmd when a current version is known")
	}

	msg, ok := cmd().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 1 {
		t.Fatalf("GetUpdateChannel call count = %d, want exactly 1 -- checkForUpdateCmd must resolve the channel from the service before anything else", service.calls.updateChannel)
	}
	if msg.err == nil {
		t.Fatal("updateCheckCompletedMsg.err = nil, want the GetUpdateChannel error to short-circuit the check before any GitHub fetch")
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

func TestModelViewResizeAboveMinimumRestoresRendering(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	tooSmall, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if !strings.Contains(tooSmall.(Model).View().Content, "Terminal too small") {
		t.Fatalf("view = %q, want too-small message before resize", tooSmall.(Model).View().Content)
	}

	restored, _ := tooSmall.(Model).Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight})
	view := restored.(Model).View().Content

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
	result.screen = screenSecurityTrivyRepos
	result.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
	scanRuns := make([]ports.ScanRun, 40)
	for i := range scanRuns {
		scanRuns[i] = ports.ScanRun{ID: fmt.Sprintf("run-%d", i), Repository: fmt.Sprintf("team/service-%d", i)}
	}
	// Repository Alerts' table lives on trivyReposScreen (design.md's State
	// Migration table) -- mounted here like production (navigateMsg).
	repos := trivyReposScreen{summaries: summarizeScanRunsByRepository(scanRuns), loaded: true}
	repos.rebuildTable(result.screenEnv())
	result.adminScreens[slotTrivyRepos] = repos
	result.rebuildAdminTables(result.adminTablesLayout())
	beforePageSize := result.adminView.Layout.Primary

	grown, _ := result.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight + 20})
	afterModel := grown.(Model)

	if got := afterModel.adminView.Layout.Primary; got <= beforePageSize {
		t.Fatalf("primary pageSize after taller resize = %d, want > %d (before resize) — resize must recompute the table budget without restart", got, beforePageSize)
	}
	afterRepos, ok := afterModel.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyRepos] not a trivyReposScreen after resize")
	}
	wantHeight := afterModel.adminView.Layout.Primary + tableChromeRows
	if got := lipgloss.Height(afterRepos.table.View()); got != wantHeight {
		t.Fatalf("repository alerts table height after resize = %d, want pageSize(%d)+tableChromeRows = %d", got, afterModel.adminView.Layout.Primary, wantHeight)
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
	result.screen = screenAdminUsers
	result.adminSession = AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Now().Add(time.Hour)}
	result.rebuildAdminTables(result.adminTablesLayout())
	beforePageSize := result.adminView.Layout.Primary

	shrunk, _ := result.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight})
	afterModel := shrunk.(Model)

	if got := afterModel.adminView.Layout.Primary; got >= beforePageSize {
		t.Fatalf("primary pageSize after shorter resize = %d, want < %d (before resize) — resize must re-fit the table to the smaller budget", got, beforePageSize)
	}
	if got := lipgloss.Height(afterModel.View().Content); got > minViewportHeight {
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

	repoItems := make([]appregixtry.RepositorySummary, 0, 60)
	tagItems := make([]appregixtry.TagDetails, 0, 60)
	for i := 0; i < 60; i++ {
		repoItems = append(repoItems, appregixtry.RepositorySummary{Name: fmt.Sprintf("library/repo-%02d", i), TagCount: i})
		tagItems = append(tagItems, appregixtry.TagDetails{Name: fmt.Sprintf("v1.0.%02d", i), SignatureState: appregixtry.SignatureStatusUnsigned})
	}

	for _, height := range []int{24, 30, 50} {
		height := height
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			t.Parallel()

			model := NewModel(&fakeQueryService{})
			updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: height})
			result := updated.(Model)

			result.screen = screenRepositories
			result.repositories = RepositoriesModel{Items: repoItems}
			result.rebuildRepositoriesTable(result.repositoriesTableLayout())
			view := result.View().Content
			if got := lipgloss.Height(view); got > height {
				t.Fatalf("repositories view height = %d, want <= %d\nview:\n%s", got, height, view)
			}

			result.screen = screenTags
			result.tags = TagsModel{Repository: "library/alpine", Items: tagItems}
			result.rebuildTagsTable(result.tagsTableLayout())
			view = result.View().Content
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
	// lipgloss v2 always renders full-fidelity ANSI, so no color profile needs
	// forcing for the styled output below to be distinguishable from plain text.

	model := NewModel(&fakeQueryService{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: 30})
	result := updated.(Model)
	result.screen = screenError
	result.err = errors.New("connection refused")

	view := result.View().Content

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

// Disposition note (console-repositories-table change): the old
// TestModelPageKeysScrollAndClampCatalogList regression test was removed
// here, not superseded in place -- it drove the Repositories screen's
// PgUp/PgDn/Home/End scroll-clamping behavior (applyBodyPageKey /
// m.bodyScroll), which only ever applied to plain-list screens
// (design.md decision #3). The Repositories screen is now table-backed
// (buildConsoleRepositoriesTable), same as the Tags screen already was
// before this change, so it pages via WithHighlightedRow auto-paging on
// Up/Down instead -- m.bodyScroll is no longer consulted by
// renderConsoleRepositoriesSection. Its coverage is superseded by
// TestModelRepositoriesTableNavigatesUpDownAcrossPagesAndEntersSelectedRepository
// below, mirroring how the Tags screen's own equivalent migration
// (TestModelTagsTableNavigatesUpDownAcrossPagesAndEntersSelectedManifest)
// was covered.

// TestModelRepositoriesTableNavigatesUpDownAcrossPagesAndEntersSelectedRepository
// is the console-repositories-table change's own navigation RED test,
// mirroring TestModelTagsTableNavigatesUpDownAcrossPagesAndEntersSelectedManifest
// one level up: the Repositories screen's table-backed model must keep
// Up/Down selection and Enter working (spec requirement: "Preserve existing
// keyboard navigation"), including auto-paging past the first page's
// boundary via WithHighlightedRow, and Enter must open the Tags screen for
// whichever repository is currently highlighted -- not always the first
// one.
func TestModelRepositoriesTableNavigatesUpDownAcrossPagesAndEntersSelectedRepository(t *testing.T) {
	t.Parallel()

	const totalRepositories = 30
	summaries := make([]appregixtry.RepositorySummary, 0, totalRepositories)
	tagDetails := make(map[string][]appregixtry.TagDetails, totalRepositories)
	for i := 0; i < totalRepositories; i++ {
		name := fmt.Sprintf("library/repo-%02d", i)
		summaries = append(summaries, appregixtry.RepositorySummary{Name: name, TagCount: i})
		tagDetails[name] = []appregixtry.TagDetails{{Name: "latest", SignatureState: appregixtry.SignatureStatusUnsigned}}
	}

	service := &fakeQueryService{repositorySummaries: summaries, tagDetails: tagDetails}

	model := NewModel(service)
	model.viewport = viewportSize{Width: minViewportWidth, Height: minViewportHeight}
	updated := runCmd(t, model, model.Init())

	if updated.screen != screenRepositories {
		t.Fatalf("screen = %q, want %q", updated.screen, screenRepositories)
	}

	pageSize := consoleRepositoriesTablePageSize(updated.repositoriesTableLayout())
	if pageSize <= 0 || pageSize >= totalRepositories {
		t.Fatalf("repositories table pageSize = %d, want a positive size smaller than %d rows so pagination genuinely activates", pageSize, totalRepositories)
	}
	if got, want := updated.repositories.Table.CurrentPage(), 1; got != want {
		t.Fatalf("repositories table CurrentPage() before navigation = %d, want %d", got, want)
	}

	for i := 0; i < pageSize; i++ {
		updated = runKey(t, updated, "down")
	}

	if got, want := updated.repositories.Selected, pageSize; got != want {
		t.Fatalf("repositories.Selected after %d downs = %d, want %d", pageSize, got, want)
	}
	if got, want := updated.repositories.Table.CurrentPage(), 2; got != want {
		t.Fatalf("repositories table CurrentPage() after paging past the first page boundary = %d, want %d (WithHighlightedRow must auto-page the table)", got, want)
	}
	if !strings.Contains(updated.View().Content, summaries[pageSize].Name) {
		t.Fatalf("view after paging = %q, want the newly highlighted repository %q genuinely visible on-screen", updated.View().Content, summaries[pageSize].Name)
	}

	upOnce := runKey(t, updated, "up")
	if got, want := upOnce.repositories.Selected, pageSize-1; got != want {
		t.Fatalf("repositories.Selected after one up = %d, want %d", got, want)
	}

	entered := runKey(t, upOnce, "enter")
	if entered.screen != screenTags {
		t.Fatalf("screen = %q, want %q", entered.screen, screenTags)
	}
	wantRepository := summaries[pageSize-1].Name
	if entered.tags.Repository != wantRepository {
		t.Fatalf("tags.Repository = %q, want %q (Enter must open the currently highlighted repository, not always the first one)", entered.tags.Repository, wantRepository)
	}
}

func TestModelNavigatesRepositoriesManifestBlobsAndUploads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 28, 23, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails: map[string][]appregixtry.TagDetails{
			"library/alpine": {{Name: "latest", CreatedAt: now, SignatureState: appregixtry.SignatureStatusUnsigned, SigningEnabled: false}},
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
	if !strings.Contains(updated.View().Content, "Manifest · library/alpine:latest") {
		t.Fatalf("view = %q, want manifest header", updated.View().Content)
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
	if !strings.Contains(updated.View().Content, "upload-1") {
		t.Fatalf("view = %q, want upload id", updated.View().Content)
	}
}

// TestModelTagsTableNavigatesUpDownAcrossPagesAndEntersSelectedManifest is
// the console-tags-table change's own navigation RED test, mirroring
// TestModelAdminScanRunsTablePagesOnArrowKeyNavigationPastPageBoundary: the
// Tags screen's table-backed model must keep Up/Down selection and Enter
// working (spec requirement: "Preserve existing keyboard navigation"),
// including auto-paging past the first page's boundary via
// WithHighlightedRow, and Enter must open the manifest for whichever tag is
// currently highlighted -- not always the first one.
func TestModelTagsTableNavigatesUpDownAcrossPagesAndEntersSelectedManifest(t *testing.T) {
	t.Parallel()

	const totalTags = 30
	tags := make([]appregixtry.TagDetails, 0, totalTags)
	manifests := make(map[string]appregixtry.ManifestDetails, totalTags)
	for i := 0; i < totalTags; i++ {
		name := fmt.Sprintf("v1.0.%02d", i)
		tags = append(tags, appregixtry.TagDetails{Name: name, SignatureState: appregixtry.SignatureStatusUnsigned})
		manifests[fmt.Sprintf("library/alpine:%s", name)] = appregixtry.ManifestDetails{
			Repository: "library/alpine",
			Reference:  name,
			Digest:     "sha256:" + name,
		}
	}

	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails:          map[string][]appregixtry.TagDetails{"library/alpine": tags},
		manifests:           manifests,
	}

	model := NewModel(service)
	model.viewport = viewportSize{Width: minViewportWidth, Height: minViewportHeight}
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter") // open library/alpine's tags

	if updated.screen != screenTags {
		t.Fatalf("screen = %q, want %q", updated.screen, screenTags)
	}

	pageSize := consoleTagsTablePageSize(updated.tagsTableLayout())
	if pageSize <= 0 || pageSize >= totalTags {
		t.Fatalf("tags table pageSize = %d, want a positive size smaller than %d rows so pagination genuinely activates", pageSize, totalTags)
	}
	if got, want := updated.tags.Table.CurrentPage(), 1; got != want {
		t.Fatalf("tags table CurrentPage() before navigation = %d, want %d", got, want)
	}

	for i := 0; i < pageSize; i++ {
		updated = runKey(t, updated, "down")
	}

	if got, want := updated.tags.Selected, pageSize; got != want {
		t.Fatalf("tags.Selected after %d downs = %d, want %d", pageSize, got, want)
	}
	if got, want := updated.tags.Table.CurrentPage(), 2; got != want {
		t.Fatalf("tags table CurrentPage() after paging past the first page boundary = %d, want %d (WithHighlightedRow must auto-page the table)", got, want)
	}
	if !strings.Contains(updated.View().Content, tags[pageSize].Name) {
		t.Fatalf("view after paging = %q, want the newly highlighted tag %q genuinely visible on-screen", updated.View().Content, tags[pageSize].Name)
	}

	upOnce := runKey(t, updated, "up")
	if got, want := upOnce.tags.Selected, pageSize-1; got != want {
		t.Fatalf("tags.Selected after one up = %d, want %d", got, want)
	}

	entered := runKey(t, upOnce, "enter")
	if entered.screen != screenManifest {
		t.Fatalf("screen = %q, want %q", entered.screen, screenManifest)
	}
	wantTag := tags[pageSize-1].Name
	if entered.manifest.Details.Reference != wantTag {
		t.Fatalf("manifest.Details.Reference = %q, want %q (Enter must open the currently highlighted tag, not always the first one)", entered.manifest.Details.Reference, wantTag)
	}
}

// newTagsReadyModel builds a Model already on screenTags for a single
// repository/tag, mirroring the tag-setup slice other Tags tests above use,
// factored out since every tag-delete test below needs the same starting
// point.
func newTagsReadyModel(t *testing.T, service *fakeQueryService) Model {
	t.Helper()
	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter")
	if updated.screen != screenTags {
		t.Fatalf("test setup invalid: screen = %q, want %q", updated.screen, screenTags)
	}
	return updated
}

func tagsReadyFakeService() *fakeQueryService {
	return &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails: map[string][]appregixtry.TagDetails{
			"library/alpine": {{Name: "latest", SignatureState: appregixtry.SignatureStatusUnsigned}},
		},
	}
}

// TestModelTagsSortKeyCyclesNameAscendingToDateDescendingAndBack is the
// sortable-tags-and-projects feature's RED test for the Tags screen's "s"
// key: two tags whose Name-ascending and CreatedAt-descending orders
// disagree ("older" sorts first by name but last by date) prove the cycle
// actually re-sorts Items, entirely client-side, and cycles back on a
// second press.
func TestModelTagsSortKeyCyclesNameAscendingToDateDescendingAndBack(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails: map[string][]appregixtry.TagDetails{
			"library/alpine": {
				{Name: "older", CreatedAt: older},
				{Name: "zzz-newest", CreatedAt: newer},
			},
		},
	}
	ready := newTagsReadyModel(t, service)

	if got, want := ready.tags.Items[0].Name, "older"; got != want {
		t.Fatalf("initial Items[0].Name = %q, want %q (default name-ascending)", got, want)
	}

	sorted := runKey(t, ready, "s")
	if got, want := sorted.tags.Items[0].Name, "zzz-newest"; got != want {
		t.Fatalf("after 1st sort Items[0].Name = %q, want %q (date-descending: most recent first)", got, want)
	}
	if !strings.Contains(sorted.View().Content, "Tags (sort: date)") {
		t.Fatalf("view after 1st sort = %q, want the sort mode shown in the subheading", sorted.View().Content)
	}

	backToName := runKey(t, sorted, "s")
	if got, want := backToName.tags.Items[0].Name, "older"; got != want {
		t.Fatalf("after 2nd sort Items[0].Name = %q, want %q (cycled back to name-ascending)", got, want)
	}
	if !strings.Contains(backToName.View().Content, "Tags (sort: name)") {
		t.Fatalf("view after 2nd sort = %q, want the sort mode shown in the subheading", backToName.View().Content)
	}
}

// TestModelTagsSortModePersistsAcrossReload covers the tagsLoadedMsg refresh
// path (e.g. after a tag delete): a chosen sort mode must not silently
// revert to the default just because the list reloaded.
func TestModelTagsSortModePersistsAcrossReload(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails: map[string][]appregixtry.TagDetails{
			"library/alpine": {
				{Name: "older", CreatedAt: older},
				{Name: "zzz-newest", CreatedAt: newer},
			},
		},
	}
	ready := newTagsReadyModel(t, service)
	sorted := runKey(t, ready, "s")
	if sorted.tags.SortMode != sortByDateDesc {
		t.Fatalf("SortMode = %v, want sortByDateDesc", sorted.tags.SortMode)
	}

	reloaded := runCmd(t, sorted, sorted.loadTagsCmd("library/alpine"))
	if reloaded.tags.SortMode != sortByDateDesc {
		t.Fatalf("SortMode after reload = %v, want sortByDateDesc (must persist)", reloaded.tags.SortMode)
	}
	if got, want := reloaded.tags.Items[0].Name, "zzz-newest"; got != want {
		t.Fatalf("Items[0].Name after reload = %q, want %q (date-descending order preserved)", got, want)
	}
}

// TestModelTagsDeleteKeyShowsPendingConfirm is the blob-garbage-collection
// change's RED test for wiring the Tags screen's "d" key to
// QueryService.DeleteManifest (already fully tested at the Service level):
// pressing "d" with a tag selected must set the pending-delete confirm
// state and message, and must NOT call DeleteManifest yet -- mirroring
// TestModelAdminRobotDeleteKeyOpensConfirmModal's own confirm-before-action
// shape one level up (screenTags is not an admin screen, so it gets its own
// minimal TagsModel.PendingDelete field rather than adminView.ConfirmModal).
func TestModelTagsDeleteKeyShowsPendingConfirm(t *testing.T) {
	t.Parallel()

	service := tagsReadyFakeService()
	ready := newTagsReadyModel(t, service)

	pending := runKey(t, ready, "d")

	if !pending.tags.Confirm.Active() {
		t.Fatalf("tags.Confirm.Active() = false, want true (delete pending)")
	}
	want := `Delete tag "latest" from "library/alpine"? This action cannot be undone. (Enter: delete | Esc: cancel)`
	if got := pending.status; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if pending.screen != screenTags {
		t.Fatalf("screen = %q, want %q (must stay on Tags while pending)", pending.screen, screenTags)
	}
	if service.calls.deleteManifest != 0 {
		t.Fatalf("DeleteManifest called %d times, want 0 (must not fire before Enter confirms)", service.calls.deleteManifest)
	}
}

// TestModelTagsDeleteKeyWithNoTagSelectedIsNoop covers the empty-list case:
// pressing "d" on screenTags with no tag selected must not panic and must
// not open a pending confirm for a tag that does not exist.
func TestModelTagsDeleteKeyWithNoTagSelectedIsNoop(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{}
	model := NewModel(service)
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: defaultViewportHeight}
	model.screen = screenTags
	model.tags = TagsModel{Repository: "library/alpine"}

	updated := runKey(t, model, "d")

	if updated.tags.Confirm.Active() {
		t.Fatalf("tags.Confirm.Active() = true, want false (no tag selected)")
	}
	if service.calls.deleteManifest != 0 {
		t.Fatalf("DeleteManifest called %d times, want 0", service.calls.deleteManifest)
	}
	_ = updated.View() // must not panic
}

// TestModelTagsDeleteEscCancelsPendingWithoutNavigating covers Esc while a
// delete is pending: it must clear the pending state without also
// triggering screenTags' ordinary Esc-navigates-to-Repositories behavior --
// cancelling the confirm must feel like "never happened".
func TestModelTagsDeleteEscCancelsPendingWithoutNavigating(t *testing.T) {
	t.Parallel()

	service := tagsReadyFakeService()
	ready := newTagsReadyModel(t, service)
	pending := runKey(t, ready, "d")
	if !pending.tags.Confirm.Active() {
		t.Fatalf("test setup invalid: want a pending delete before Esc")
	}

	cancelled := runKey(t, pending, "esc")

	if cancelled.tags.Confirm.Active() {
		t.Fatalf("tags.Confirm.Active() = true, want false after Esc")
	}
	if cancelled.status != "" {
		t.Fatalf("status = %q, want empty after cancel", cancelled.status)
	}
	if cancelled.screen != screenTags {
		t.Fatalf("screen = %q, want %q (Esc while pending must not navigate back)", cancelled.screen, screenTags)
	}
	if service.calls.deleteManifest != 0 {
		t.Fatalf("DeleteManifest called %d times, want 0", service.calls.deleteManifest)
	}
}

// TestModelTagsDeleteEnterConfirmFlowSuccess drives Enter on the pending
// confirm and inspects each hop of the Update chain directly (rather than
// letting runKey auto-chain through the refresh), the same way this task's
// spec calls for: the delete msg handler must clear PendingDelete, set the
// success status, and return the Tags-list refresh command as a distinct
// tea.Cmd -- driving that cmd must actually reload the list via
// QueryService.TagDetails, not a hardcoded stub.
func TestModelTagsDeleteEnterConfirmFlowSuccess(t *testing.T) {
	t.Parallel()

	service := tagsReadyFakeService()
	ready := newTagsReadyModel(t, service)
	pending := runKey(t, ready, "d")
	tagsCallsBeforeEnter := service.calls.tags

	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	if deleteCmd == nil {
		t.Fatalf("Enter while pending returned a nil tea.Cmd, want the delete command")
	}
	if service.lastDeleteManifestRepository != "" || service.lastDeleteManifestReference != "" {
		t.Fatalf("DeleteManifest called synchronously by Update, want it deferred inside the returned tea.Cmd")
	}

	deleteMsg := deleteCmd()
	if service.lastDeleteManifestRepository != "library/alpine" || service.lastDeleteManifestReference != "latest" {
		t.Fatalf("DeleteManifest called with (%q, %q), want (%q, %q)", service.lastDeleteManifestRepository, service.lastDeleteManifestReference, "library/alpine", "latest")
	}

	afterDeleteUpdated, refreshCmd := firedModel.Update(deleteMsg)
	afterDelete := afterDeleteUpdated.(Model)

	if afterDelete.tags.Confirm.Active() {
		t.Fatalf("tags.Confirm.Active() = true, want false after delete completes")
	}
	wantStatus := `Tag "latest" deleted. Refreshing tags...`
	if got := afterDelete.status; got != wantStatus {
		t.Fatalf("status = %q, want %q", got, wantStatus)
	}
	if refreshCmd == nil {
		t.Fatalf("want a non-nil refresh cmd to re-fire the Tags list load")
	}

	final := runCmd(t, afterDelete, refreshCmd)
	if got, want := service.calls.tags, tagsCallsBeforeEnter+1; got != want {
		t.Fatalf("TagDetails called %d times after refresh, want %d", got, want)
	}
	if final.screen != screenTags {
		t.Fatalf("screen = %q, want %q after refresh", final.screen, screenTags)
	}
}

// TestModelTagsDeleteEnterConfirmFlowValidationError covers the
// delete-disabled backend response (Service.DeleteManifest's
// domain.ErrorCodeValidation "manifest deletion is not enabled" when
// REGISTRY_DELETE_ENABLED is off): the TUI must surface it in the status
// line, clear the pending state so the operator is not stuck, and must NOT
// refresh the list (nothing changed).
func TestModelTagsDeleteEnterConfirmFlowValidationError(t *testing.T) {
	t.Parallel()

	service := tagsReadyFakeService()
	service.deleteManifestErr = errors.New("manifest deletion is not enabled")
	ready := newTagsReadyModel(t, service)
	pending := runKey(t, ready, "d")
	tagsCallsBeforeEnter := service.calls.tags

	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	if deleteCmd == nil {
		t.Fatalf("Enter while pending returned a nil tea.Cmd, want the delete command")
	}
	deleteMsg := deleteCmd()

	afterDeleteUpdated, refreshCmd := firedModel.Update(deleteMsg)
	afterDelete := afterDeleteUpdated.(Model)

	if afterDelete.tags.Confirm.Active() {
		t.Fatalf("tags.Confirm.Active() = true, want false after a failed delete (operator must not be stuck)")
	}
	if got, want := afterDelete.status, "manifest deletion is not enabled"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if refreshCmd != nil {
		t.Fatalf("want a nil refresh cmd on error (list must not be refreshed)")
	}
	if got, want := service.calls.tags, tagsCallsBeforeEnter; got != want {
		t.Fatalf("TagDetails called %d times after a failed delete, want %d (unchanged)", got, want)
	}
}

// TestModelManifestBlobsUploadsDeleteKeyUnaffectedByTagsDeleteWiring is the
// regression guard for this task's explicit constraint: screenManifest's
// (and its screenBlobs/screenUploads siblings') existing "d"/"x" ->
// showMutationNotice "Delete unavailable in v1" placeholder must remain
// completely unchanged -- it must never call the real DeleteManifest.
func TestModelManifestBlobsUploadsDeleteKeyUnaffectedByTagsDeleteWiring(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 28, 23, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}},
		tagDetails: map[string][]appregixtry.TagDetails{
			"library/alpine": {{Name: "latest", CreatedAt: now, SignatureState: appregixtry.SignatureStatusUnsigned}},
		},
		manifests: map[string]appregixtry.ManifestDetails{
			"library/alpine:latest": {Repository: "library/alpine", Reference: "latest", Digest: "sha256:manifest"},
		},
	}
	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "enter")
	if updated.screen != screenManifest {
		t.Fatalf("test setup invalid: screen = %q, want %q", updated.screen, screenManifest)
	}

	manifestDeleted := runKey(t, updated, "d")
	if !manifestDeleted.showMutationNotice {
		t.Fatalf("showMutationNotice = false on screenManifest after 'd', want true (unchanged v1 placeholder)")
	}
	if !strings.Contains(manifestDeleted.View().Content, "Delete unavailable in v1") {
		t.Fatalf("view = %q, want the v1 placeholder message", manifestDeleted.View().Content)
	}

	blobsDeleted := runKey(t, runKey(t, updated, "b"), "x")
	if !blobsDeleted.showMutationNotice {
		t.Fatalf("showMutationNotice = false on screenBlobs after 'x', want true (unchanged v1 placeholder)")
	}

	uploadsDeleted := runKey(t, runKey(t, updated, "u"), "d")
	if !uploadsDeleted.showMutationNotice {
		t.Fatalf("showMutationNotice = false on screenUploads after 'd', want true (unchanged v1 placeholder)")
	}

	if service.calls.deleteManifest != 0 {
		t.Fatalf("DeleteManifest called %d times, want 0 (screenManifest/Blobs/Uploads must never call the real delete)", service.calls.deleteManifest)
	}
}

// TestModelTagsDeleteViewShowsConfirmAndHelpWhenPending is this task's
// rendering RED test, mirroring how viewport_test.go's inspection-help
// snapshot list already covers screenManifest's own help line: the pending
// confirm message must render in the status area and the Tags screen's help
// line must mention the new delete action.
func TestModelTagsDeleteViewShowsConfirmAndHelpWhenPending(t *testing.T) {
	t.Parallel()

	service := tagsReadyFakeService()
	ready := newTagsReadyModel(t, service)
	pending := runKey(t, ready, "d")

	view := pending.View().Content
	if !strings.Contains(view, `Delete tag "latest" from "library/alpine"? This action cannot be undone.`) {
		t.Fatalf("view = %q, want the pending-delete confirm message", view)
	}
	if !strings.Contains(view, "d: delete tag") {
		t.Fatalf("view = %q, want the updated Tags help line mentioning delete", view)
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
	if !strings.Contains(ansi.Strip(updated.View().Content), "operator sign in") {
		t.Fatalf("view = %q, want login screen", updated.View().Content)
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
	view := ansi.Strip(updated.View().Content)

	// Post-login lands on the Overview, not directly on screenAdminUsers.
	if updated.screen != screenAdminOverview {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminOverview)
	}
	for _, want := range []string{"regixtry admin", "ACCESS", "SECURITY", "OPERATIONS", "Overview", "signed in as operator"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want the Overview shell with %q", view, want)
		}
	}
	if strings.Contains(view, "Create as admin") || strings.Contains(view, "New password") {
		t.Fatalf("view = %q, the Overview must not render edit forms", view)
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
	if !strings.Contains(updated.View().Content, "login: invalid credentials") {
		t.Fatalf("view = %q, want login error", updated.View().Content)
	}
}

// TestModelAdminIntentRoutesPostLoginToUsersWhenNotSet triangulates the
// default (zero-value) adminIntent path: an ordinary operator login without
// any repo-grants intent keeps routing to screenAdminOverview, the admin
// panel's own root.
func TestModelAdminIntentRoutesPostLoginToUsersWhenNotSet(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)

	updated := runAdminLogin(t, model, "operator", "secret-pass")

	if got, want := updated.screen, screenAdminOverview; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := updated.adminIntent, adminIntentOperator; got != want {
		t.Fatalf("adminIntent = %q, want %q", got, want)
	}
}

// TestModelConsoleRepositoriesGrantActionSetsAdminIntentAndReachesLogin is
// the Phase 3 task 3.9 RED test (design.md Decision 7): pressing "g" on the
// Console Repositories screen, with a repository selected, sets
// adminIntent/adminIntentRepository and routes to screenAdminLogin exactly
// like the existing "tab" (operator) entry point, but carries repository
// context the operator entry point never needs.
func TestModelConsoleRepositoriesGrantActionSetsAdminIntentAndReachesLogin(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModelWithCatalog(t, []string{"team/app"}, &fakeAdminClient{})

	updated := runKey(t, model, "g")

	if got, want := updated.screen, screenAdminLogin; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := updated.adminIntent, adminIntentRepoGrants; got != want {
		t.Fatalf("adminIntent = %q, want %q", got, want)
	}
	if got, want := updated.adminIntentRepository, "team/app"; got != want {
		t.Fatalf("adminIntentRepository = %q, want %q", got, want)
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
	updated = openUsersFromOverview(t, updated)

	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, "bob")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "secret-pass")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "left")
	updated = runKey(t, updated, "left")
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "enter")

	if adminClient.createUserCalls != 1 || adminClient.listUsersCalls != 2 {
		t.Fatalf("admin client calls = %#v, want one create and refresh", adminClient)
	}
	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
	if user, _ := usersScreenOf(t, updated).current(); user.ID != "u-2" {
		t.Fatalf("selected user = %q, want the new user u-2", user.ID)
	}
	if view := ansi.Strip(updated.View().Content); !strings.Contains(view, `User "bob" created.`) {
		t.Fatalf("view = %q, want create-user status", view)
	}
	if got := adminClient.lastCreateUserInput; got.Username != "bob" || got.Password != "secret-pass" || !got.IsAdmin || got.IsReadOnly || !got.Enabled {
		t.Fatalf("create input = %+v, want admin bob, enabled", got)
	}
}

func TestAdminRobotScreensJoinAdminScreenSets(t *testing.T) {
	t.Parallel()

	if !isAdminScreen(screenAdminRobots) {
		t.Fatalf("isAdminScreen(screenAdminRobots) = false, want true")
	}
	if !isAdminScreen(screenAdminCreateRobot) {
		t.Fatalf("isAdminScreen(screenAdminCreateRobot) = false, want true")
	}
	if !isAdminPrincipalScreen(screenAdminRobots) {
		t.Fatalf("isAdminPrincipalScreen(screenAdminRobots) = false, want true")
	}
	if isAdminPrincipalScreen(screenAdminCreateRobot) {
		t.Fatalf("isAdminPrincipalScreen(screenAdminCreateRobot) = true, want false (free-text form)")
	}
}

func TestCanLogoutFromAdminRobotsScreen(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenAdminRobots

	if !model.canLogoutAdminFromCurrentScreen() {
		t.Fatalf("canLogoutAdminFromCurrentScreen() = false, want true on screenAdminRobots")
	}
}

// TestModelReenteringAdminWithActiveSessionLandsOnOverview is the
// user-reported regression test: pressing Tab while an admin session is
// already authenticated (e.g. after Esc-ing back to Console inspection, or
// under REGXITRY's startup-login mode where a fresh login's success handler
// routes to the repository catalog rather than screenAdminOverview directly)
// must still land on the Overview, not skip straight to screenAdminUsers --
// openAdmin()'s already-authenticated branch must agree with the
// fresh-login-success handler.
func TestModelReenteringAdminWithActiveSessionLandsOnOverview(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(15 * time.Minute)},
	}
	model := newAdminReadyModel(t, adminClient)
	model.now = func() time.Time { return now }
	loggedIn := runAdminLogin(t, model, "operator", "secret-pass")
	if loggedIn.screen != screenAdminOverview {
		t.Fatalf("test setup invalid: screen after login = %q, want %q", loggedIn.screen, screenAdminOverview)
	}

	returnedToInspection := runKey(t, loggedIn, "esc")
	if returnedToInspection.adminAuth != adminAuthStateAuthenticated {
		t.Fatalf("test setup invalid: adminAuth = %q, want authenticated (session must still be active)", returnedToInspection.adminAuth)
	}

	reentered := runKey(t, returnedToInspection, "tab")

	if got, want := reentered.screen, screenAdminOverview; got != want {
		t.Fatalf("screen after re-entering with an active session = %q, want %q", got, want)
	}
}

// TestAdminConfirmCharacterization is the tui-menu-architecture change's
// Phase 1 task 1.3 (T1.1) golden/characterization baseline, captured
// BEFORE confirm.go exists: for every one of the 10 adminConfirmKind values,
// opening the confirm and pressing Enter must dispatch the exact right
// admin API call, and Esc must cancel without dispatching anything. Every
// assertion is black-box (View() content, status text, and fakeAdminClient
// call counters) — deliberately never touching
// updated.adminView.ConfirmModal's fields directly, so this test keeps
// passing unchanged once Phase 5 retires adminConfirmModal onto
// confirmPrompt (design.md Decision E) and again once AdminViewState.Confirm
// itself becomes the only place the state lives.
func TestAdminConfirmCharacterization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)

	type tc struct {
		name           string
		setup          func(t *testing.T) (Model, *fakeAdminClient)
		openKey        string
		wantOpenSubstr string
		callCount      func(*fakeAdminClient) int
	}

	cases := []tc{
		{
			name: "enable-user",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					users:        []ports.AdminUser{{ID: "u-1", Username: "alice", Enabled: false}},
					enableUser:   ports.AdminUser{ID: "u-1", Username: "alice", Enabled: true},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openUsersFromOverview(t, m)
				return m, adminClient
			},
			openKey:        "d",
			wantOpenSubstr: "Enable user?",
			callCount:      func(f *fakeAdminClient) int { return f.enableCalls },
		},
		{
			name: "disable-user",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					users:        []ports.AdminUser{{ID: "u-1", Username: "alice", Enabled: true}},
					disableUser:  ports.AdminUser{ID: "u-1", Username: "alice", Enabled: false},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openUsersFromOverview(t, m)
				return m, adminClient
			},
			openKey:        "d",
			wantOpenSubstr: "Disable user?",
			callCount:      func(f *fakeAdminClient) int { return f.disableCalls },
		},
		{
			name: "enable-feature",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					featurePage: ports.FeaturePage{
						Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: false},
						Actions: []ports.FeatureAction{{ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: `Confirm enable feature "gitleaks"?`}},
					},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openSidebarItem(t, m, shellNavSecretScanning)
				return m, adminClient
			},
			openKey:        "e",
			wantOpenSubstr: `Confirm enable feature "gitleaks"?`,
			callCount:      func(f *fakeAdminClient) int { return f.executeFeatureActionCalls },
		},
		{
			name: "disable-feature",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					featurePage: ports.FeaturePage{
						Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true},
						Actions: []ports.FeatureAction{{ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "gitleaks"?`}},
					},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openSidebarItem(t, m, shellNavSecretScanning)
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Confirm disable feature "gitleaks"?`,
			callCount:      func(f *fakeAdminClient) int { return f.executeFeatureActionCalls },
		},
		{
			name: "revoke-token",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					users:        []ports.AdminUser{{ID: "u-1", Username: "alice", Enabled: true}},
					tokens:       map[string][]ports.AdminToken{"u-1": {{Accessor: "tok-1", ExpiresAt: now.Add(time.Hour)}}},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openUsersFromOverview(t, m)
				m = runKey(t, m, "t")
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Revoke token "unnamed" (tok-1) for "alice"?`,
			callCount:      func(f *fakeAdminClient) int { return f.revokeTokenCalls },
		},
		{
			name: "enable-robot",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					robots:       []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: false}},
					enableUser:   ports.AdminUser{ID: "u-2", Username: "robot$ci", Enabled: true},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openSidebarItem(t, m, shellNavRobots)
				return m, adminClient
			},
			openKey:        "e",
			wantOpenSubstr: `Confirm enable robot "robot$ci"?`,
			callCount:      func(f *fakeAdminClient) int { return f.enableCalls },
		},
		{
			name: "disable-robot",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					robots:       []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
					disableUser:  ports.AdminUser{ID: "u-2", Username: "robot$ci", Enabled: false},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = openSidebarItem(t, m, shellNavRobots)
				return m, adminClient
			},
			openKey:        "d",
			wantOpenSubstr: `Confirm disable robot "robot$ci"?`,
			callCount:      func(f *fakeAdminClient) int { return f.disableCalls },
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name+"/confirm", func(t *testing.T) {
			t.Parallel()
			m, client := c.setup(t)
			opened := runKey(t, m, c.openKey)
			if !strings.Contains(ansi.Strip(opened.View().Content), c.wantOpenSubstr) {
				t.Fatalf("view after %q = %q, want to contain %q", c.openKey, opened.View().Content, c.wantOpenSubstr)
			}
			before := c.callCount(client)
			confirmed := runKey(t, opened, "enter")
			_ = confirmed
			if got, want := c.callCount(client), before+1; got != want {
				t.Fatalf("call count after enter = %d, want %d", got, want)
			}
		})
		t.Run(c.name+"/cancel", func(t *testing.T) {
			t.Parallel()
			m, client := c.setup(t)
			opened := runKey(t, m, c.openKey)
			before := c.callCount(client)
			cancelled := runKey(t, opened, "esc")
			if strings.Contains(ansi.Strip(cancelled.View().Content), c.wantOpenSubstr) {
				t.Fatalf("view after esc = %q, want confirm closed", cancelled.View().Content)
			}
			if got := c.callCount(client); got != before {
				t.Fatalf("call count after esc = %d, want unchanged %d (cancel must not dispatch)", got, before)
			}
		})
	}
}

// TestDeleteTagConfirmCharacterization is the tui-menu-architecture change's
// Phase 1 task 1.4 (T1.2) golden/characterization baseline, captured BEFORE
// confirm.go exists: TagsModel's pending-delete Enter/Esc behavior, asserted
// black-box via View()/status/service call counters only — never touching
// tags.PendingDelete directly — so it survives unchanged once Phase 5 moves
// this state onto TagsModel.Confirm confirmPrompt.
func TestDeleteTagConfirmCharacterization(t *testing.T) {
	t.Parallel()

	service := tagsReadyFakeService()
	ready := newTagsReadyModel(t, service)

	pending := runKey(t, ready, "d")
	want := `Delete tag "latest" from "library/alpine"? This action cannot be undone. (Enter: delete | Esc: cancel)`
	if got := pending.status; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if service.calls.deleteManifest != 0 {
		t.Fatalf("DeleteManifest called %d times, want 0 before confirm", service.calls.deleteManifest)
	}

	cancelled := runKey(t, pending, "esc")
	if cancelled.status != "" {
		t.Fatalf("status after esc = %q, want empty", cancelled.status)
	}
	if service.calls.deleteManifest != 0 {
		t.Fatalf("DeleteManifest called %d times after esc, want 0", service.calls.deleteManifest)
	}

	// Manual (non-auto-chained) Update calls here, deliberately not runKey:
	// runKey auto-chains every returned tea.Cmd to completion, which would
	// also run the post-delete list refresh and clear m.status back to ""
	// before this assertion ever sees the intermediate "deleted" status —
	// mirroring TestModelTagsDeleteEnterConfirmFlowSuccess's own two-step
	// inspection of the Update chain.
	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	if deleteCmd == nil {
		t.Fatalf("Enter while pending returned a nil tea.Cmd, want the delete command")
	}
	afterDeleteUpdated, _ := firedModel.Update(deleteCmd())
	afterDelete := afterDeleteUpdated.(Model)
	if got, want := service.calls.deleteManifest, 1; got != want {
		t.Fatalf("DeleteManifest called %d times after enter, want %d", got, want)
	}
	if !strings.Contains(afterDelete.status, `"latest" deleted`) {
		t.Fatalf("status after enter = %q, want the deleted confirmation", afterDelete.status)
	}
}

// newRepositoriesReadyModel builds a Model already on screenRepositories,
// mirroring newTagsReadyModel's own factored-out setup one level up:
// screenRepositories is the natural landing screen once the catalog loads,
// so no extra key press is needed to get there.
func newRepositoriesReadyModel(t *testing.T, service *fakeQueryService) Model {
	t.Helper()
	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	if updated.screen != screenRepositories {
		t.Fatalf("test setup invalid: screen = %q, want %q", updated.screen, screenRepositories)
	}
	return updated
}

func repositoriesReadyFakeService() *fakeQueryService {
	return &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine", TagCount: 3}},
	}
}

// multiProjectFakeService seeds a catalog spanning two real projects plus
// one flat (no "/") repository, for the path-based-project-grouping
// feature's navigation/filter tests.
func multiProjectFakeService() *fakeQueryService {
	return &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{
			{Name: "team/app", TagCount: 2},
			{Name: "team/other", TagCount: 5},
			{Name: "smoke/keyless-live-verify", TagCount: 1},
			{Name: "govault-api", TagCount: 4},
		},
	}
}

// TestModelProjectsKeyNavigatesFromRepositoriesAndEscReturns is the
// path-based-project-grouping feature's RED test for the "p" key: it
// derives the Projects screen from the currently-loaded catalog, and Esc
// returns to Repositories unfiltered.
func TestModelProjectsKeyNavigatesFromRepositoriesAndEscReturns(t *testing.T) {
	t.Parallel()

	ready := newRepositoriesReadyModel(t, multiProjectFakeService())

	onProjects := runKey(t, ready, "p")
	if onProjects.screen != screenProjects {
		t.Fatalf("screen = %q, want %q", onProjects.screen, screenProjects)
	}
	if len(onProjects.projects.Items) != 3 {
		t.Fatalf("len(projects.Items) = %d, want 3 (team, smoke, ungrouped)", len(onProjects.projects.Items))
	}
	view := onProjects.View().Content
	for _, want := range []string{"team", "smoke", projectUngroupedLabel} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want it to contain %q", view, want)
		}
	}

	back := runKey(t, onProjects, "esc")
	if back.screen != screenRepositories {
		t.Fatalf("screen after esc = %q, want %q", back.screen, screenRepositories)
	}
	if back.repositories.Filter != nil {
		t.Fatalf("repositories.Filter = %#v, want nil (esc from Projects must not filter)", back.repositories.Filter)
	}
	if len(back.repositories.FilteredItems()) != 4 {
		t.Fatalf("len(FilteredItems()) = %d, want 4 (all repositories still reachable)", len(back.repositories.FilteredItems()))
	}
}

// TestModelProjectsEnterFiltersRepositoriesToSelectedProject covers the
// headline navigation requirement: selecting a project filters the
// Repositories screen to only that project's repositories, and Esc there
// clears the filter back to "all repositories".
func TestModelProjectsEnterFiltersRepositoriesToSelectedProject(t *testing.T) {
	t.Parallel()

	ready := newRepositoriesReadyModel(t, multiProjectFakeService())
	onProjects := runKey(t, ready, "p")

	// projects.Items is sorted name-ascending by default: "smoke", "team",
	// "(ungrouped)" -- '(' (0x28) sorts before letters, so "(ungrouped)"
	// is actually first; move down once to land on "smoke".
	filtered := runKey(t, onProjects, "down")
	filteredView := filtered.View().Content
	if !strings.Contains(filteredView, "smoke") {
		t.Fatalf("view after one down = %q, want the 'smoke' row highlighted next", filteredView)
	}

	entered := runKey(t, filtered, "enter")
	if entered.screen != screenRepositories {
		t.Fatalf("screen = %q, want %q", entered.screen, screenRepositories)
	}
	if entered.repositories.Filter == nil || entered.repositories.Filter.Project != "smoke" {
		t.Fatalf("repositories.Filter = %#v, want project %q", entered.repositories.Filter, "smoke")
	}
	items := entered.repositories.FilteredItems()
	if len(items) != 1 || items[0].Name != "smoke/keyless-live-verify" {
		t.Fatalf("FilteredItems() = %#v, want only smoke/keyless-live-verify", items)
	}
	if !strings.Contains(entered.View().Content, "Repositories (project: smoke)") {
		t.Fatalf("view = %q, want the active filter named in the heading", entered.View().Content)
	}

	cleared := runKey(t, entered, "esc")
	if cleared.repositories.Filter != nil {
		t.Fatalf("repositories.Filter = %#v after esc, want nil (show all)", cleared.repositories.Filter)
	}
	if len(cleared.repositories.FilteredItems()) != 4 {
		t.Fatalf("len(FilteredItems()) after esc = %d, want 4", len(cleared.repositories.FilteredItems()))
	}
}

// TestModelProjectsSortKeyCyclesNameAscendingToDateDescendingAndBack mirrors
// TestModelTagsSortKeyCyclesNameAscendingToDateDescendingAndBack exactly,
// one screen over.
func TestModelProjectsSortKeyCyclesNameAscendingToDateDescendingAndBack(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{
			{Name: "aaa/first", TagCount: 1, LastPushed: older},
			{Name: "zzz/last", TagCount: 1, LastPushed: newer},
		},
	}
	ready := newRepositoriesReadyModel(t, service)
	onProjects := runKey(t, ready, "p")

	if got, want := onProjects.projects.Items[0].Name, "aaa"; got != want {
		t.Fatalf("initial Items[0].Name = %q, want %q (default name-ascending)", got, want)
	}

	sorted := runKey(t, onProjects, "s")
	if got, want := sorted.projects.Items[0].Name, "zzz"; got != want {
		t.Fatalf("after sort Items[0].Name = %q, want %q (date-descending)", got, want)
	}
	if !strings.Contains(sorted.View().Content, "Projects (sort: date)") {
		t.Fatalf("view after sort = %q, want the sort mode shown", sorted.View().Content)
	}

	backToName := runKey(t, sorted, "s")
	if got, want := backToName.projects.Items[0].Name, "aaa"; got != want {
		t.Fatalf("after 2nd sort Items[0].Name = %q, want %q", got, want)
	}
}

// TestModelRepositoriesDeleteKeyShowsPendingConfirm is the
// delete-entire-repository feature's RED test for wiring the Repositories
// screen's "d" key to QueryService.DeleteRepository, mirroring
// TestModelTagsDeleteKeyShowsPendingConfirm one screen up: pressing "d" with
// a repository selected must set the pending-delete confirm state and a
// message naming BOTH the repository and its current tag count (so the
// operator is never confirming blind), and must NOT call DeleteRepository
// yet.
func TestModelRepositoriesDeleteKeyShowsPendingConfirm(t *testing.T) {
	t.Parallel()

	service := repositoriesReadyFakeService()
	ready := newRepositoriesReadyModel(t, service)

	pending := runKey(t, ready, "d")

	if !pending.repositories.Confirm.Active() {
		t.Fatalf("repositories.Confirm.Active() = false, want true (delete pending)")
	}
	want := `Delete repository "library/alpine" and all 3 tag(s)? This action cannot be undone. (Enter: delete | Esc: cancel)`
	if got := pending.status; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if pending.screen != screenRepositories {
		t.Fatalf("screen = %q, want %q (must stay on Repositories while pending)", pending.screen, screenRepositories)
	}
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times, want 0 before confirm", service.calls.deleteRepository)
	}
}

// TestDeleteRepositoryConfirmCharacterization mirrors
// TestDeleteTagConfirmCharacterization exactly, one screen up: Esc cancels
// without calling DeleteRepository, and Enter fires it, reporting success
// and refreshing the repository catalog.
func TestDeleteRepositoryConfirmCharacterization(t *testing.T) {
	t.Parallel()

	service := repositoriesReadyFakeService()
	ready := newRepositoriesReadyModel(t, service)

	pending := runKey(t, ready, "d")
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times, want 0 before confirm", service.calls.deleteRepository)
	}

	cancelled := runKey(t, pending, "esc")
	if cancelled.status != "" {
		t.Fatalf("status after esc = %q, want empty", cancelled.status)
	}
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times after esc, want 0", service.calls.deleteRepository)
	}
	if cancelled.repositories.Confirm.Active() {
		t.Fatal("repositories.Confirm.Active() = true after esc, want false")
	}

	// Manual (non-auto-chained) Update calls, deliberately not runKey:
	// runKey auto-chains every returned tea.Cmd to completion, which would
	// also run the post-delete catalog refresh and clear m.status back to
	// "" before this assertion ever sees the intermediate "deleted" status.
	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	if deleteCmd == nil {
		t.Fatalf("Enter while pending returned a nil tea.Cmd, want the delete command")
	}
	afterDeleteUpdated, _ := firedModel.Update(deleteCmd())
	afterDelete := afterDeleteUpdated.(Model)
	if got, want := service.calls.deleteRepository, 1; got != want {
		t.Fatalf("DeleteRepository called %d times after enter, want %d", got, want)
	}
	if got, want := service.lastDeleteRepositoryRepository, "library/alpine"; got != want {
		t.Fatalf("DeleteRepository called with repository %q, want %q", got, want)
	}
	if !strings.Contains(afterDelete.status, `"library/alpine" deleted`) {
		t.Fatalf("status after enter = %q, want the deleted confirmation", afterDelete.status)
	}
	// The catalog refresh command must be RepositorySummaries again
	// (loadCatalogCmd), mirroring the tag-delete flow's own reload-not-
	// local-mutation convention.
	refreshed := runCmd(t, afterDelete, afterDelete.loadCatalogCmd())
	if got, want := service.calls.catalog, 2; got != want {
		t.Fatalf("RepositorySummaries called %d times, want %d (initial load + post-delete refresh)", got, want)
	}
	_ = refreshed
}

// TestModelRepositoriesDeleteKeyWithNoRepositoriesShowsStatusOnly covers the
// no-selection guard, mirroring the Tags screen's own "No tag selected"
// posture -- an empty-catalog Repositories screen never actually renders
// (it falls to screenEmpty), so this exercises the defensive branch
// directly via selectedRepositorySummary's own empty-Items guard.
func TestModelRepositoriesDeleteKeyWithNoRepositoriesShowsStatusOnly(t *testing.T) {
	t.Parallel()

	service := repositoriesReadyFakeService()
	ready := newRepositoriesReadyModel(t, service)
	ready.repositories.Items = nil

	pending := runKey(t, ready, "d")
	if pending.repositories.Confirm.Active() {
		t.Fatal("repositories.Confirm.Active() = true, want false with no repository selected")
	}
	if got, want := pending.status, "No repository selected to delete."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times, want 0", service.calls.deleteRepository)
	}
}

// projectDeleteFakeService seeds a "team" project with THREE repositories
// (not two) plus one ungrouped repository, for the delete-entire-project
// feature's bulk-delete tests: three gives the mixed-authorization scenario
// (2 succeeded, 1 failed) a real minority/majority split to characterize,
// which a two-repository project could not.
func projectDeleteFakeService() *fakeQueryService {
	return &fakeQueryService{
		repositorySummaries: []appregixtry.RepositorySummary{
			{Name: "team/app", TagCount: 2},
			{Name: "team/api", TagCount: 1},
			{Name: "team/web", TagCount: 3},
			{Name: "solo-repo", TagCount: 4},
		},
	}
}

// teamProjectReadyModel drives newRepositoriesReadyModel through "p" (open
// Projects) then one "down" to land on "team" -- projects.Items sorts
// name-ascending by default, and "(ungrouped)" (leading '(' sorts before
// any letter) is always Items[0], so "team" is always Items[1].
func teamProjectReadyModel(t *testing.T, service *fakeQueryService) Model {
	t.Helper()
	ready := newRepositoriesReadyModel(t, service)
	onProjects := runKey(t, ready, "p")
	selected := runKey(t, onProjects, "down")
	if project, ok := selected.selectedProject(); !ok || project.Name != "team" {
		t.Fatalf("test setup invalid: selectedProject() = %#v, %v, want \"team\", true", project, ok)
	}
	return selected
}

// TestModelProjectsDeleteKeyShowsPendingConfirm is the delete-entire-project
// feature's RED test for wiring the Projects screen's "d" key to a
// best-effort bulk QueryService.DeleteRepository loop, mirroring
// TestModelRepositoriesDeleteKeyShowsPendingConfirm one screen up: pressing
// "d" with a project selected must set the pending-delete confirm state and
// a message naming the project, its repository count, AND its aggregate tag
// count (already known client-side from projectSummary) so the operator is
// never confirming blind, and must NOT call DeleteRepository yet.
func TestModelProjectsDeleteKeyShowsPendingConfirm(t *testing.T) {
	t.Parallel()

	service := projectDeleteFakeService()
	onTeam := teamProjectReadyModel(t, service)

	pending := runKey(t, onTeam, "d")

	if !pending.projects.Confirm.Active() {
		t.Fatalf("projects.Confirm.Active() = false, want true (delete pending)")
	}
	want := `Delete project "team" and all 3 repository(s) (6 tag(s) total)? This action cannot be undone. (Enter: delete | Esc: cancel)`
	if got := pending.status; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if pending.screen != screenProjects {
		t.Fatalf("screen = %q, want %q (must stay on Projects while pending)", pending.screen, screenProjects)
	}
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times, want 0 before confirm", service.calls.deleteRepository)
	}
}

// TestModelProjectsDeleteKeyWithNoProjectSelectedShowsStatusOnly mirrors
// TestModelRepositoriesDeleteKeyWithNoRepositoriesShowsStatusOnly one screen
// over: the defensive branch through selectedProject's own empty-Items
// guard.
func TestModelProjectsDeleteKeyWithNoProjectSelectedShowsStatusOnly(t *testing.T) {
	t.Parallel()

	service := projectDeleteFakeService()
	ready := newRepositoriesReadyModel(t, service)
	onProjects := runKey(t, ready, "p")
	onProjects.projects.Items = nil

	pending := runKey(t, onProjects, "d")
	if pending.projects.Confirm.Active() {
		t.Fatal("projects.Confirm.Active() = true, want false with no project selected")
	}
	if got, want := pending.status, "No project selected to delete."; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times, want 0", service.calls.deleteRepository)
	}
}

// TestDeleteProjectConfirmCharacterization mirrors
// TestDeleteRepositoryConfirmCharacterization exactly, one screen up: Esc
// cancels without calling DeleteRepository, and Enter fires the best-effort
// bulk loop over every repository in the project, reporting a clean success
// summary and refreshing the repository catalog (landing back on
// Repositories, mirroring the single-repository delete flow's own
// catalogLoadedMsg-driven navigation exactly -- no special-cased screen for
// this bulk variant).
func TestDeleteProjectConfirmCharacterization(t *testing.T) {
	t.Parallel()

	service := projectDeleteFakeService()
	onTeam := teamProjectReadyModel(t, service)

	pending := runKey(t, onTeam, "d")
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times, want 0 before confirm", service.calls.deleteRepository)
	}

	cancelled := runKey(t, pending, "esc")
	if cancelled.status != "" {
		t.Fatalf("status after esc = %q, want empty", cancelled.status)
	}
	if service.calls.deleteRepository != 0 {
		t.Fatalf("DeleteRepository called %d times after esc, want 0", service.calls.deleteRepository)
	}
	if cancelled.projects.Confirm.Active() {
		t.Fatal("projects.Confirm.Active() = true after esc, want false")
	}

	// Manual (non-auto-chained) Update calls, deliberately not runKey: runKey
	// auto-chains every returned tea.Cmd to completion, which would also run
	// the post-delete catalog refresh and clear m.status before this
	// assertion ever sees the intermediate "deleted" status -- mirroring
	// TestDeleteRepositoryConfirmCharacterization's own two-step inspection.
	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	if deleteCmd == nil {
		t.Fatalf("Enter while pending returned a nil tea.Cmd, want the delete command")
	}
	afterDeleteUpdated, refreshCmd := firedModel.Update(deleteCmd())
	afterDelete := afterDeleteUpdated.(Model)
	if got, want := service.calls.deleteRepository, 3; got != want {
		t.Fatalf("DeleteRepository called %d times after enter, want %d", got, want)
	}
	if !strings.Contains(afterDelete.status, `Project "team" deleted: 3 repository(s) removed`) {
		t.Fatalf("status after enter = %q, want the deleted confirmation", afterDelete.status)
	}
	if afterDelete.projects.Confirm.Active() {
		t.Fatal("projects.Confirm.Active() = true after enter, want false")
	}
	if refreshCmd == nil {
		t.Fatalf("Enter's delete command returned a nil follow-up tea.Cmd, want the catalog-refresh command")
	}
	refreshed := runCmd(t, afterDelete, refreshCmd)
	if got, want := service.calls.catalog, 2; got != want {
		t.Fatalf("RepositorySummaries called %d times, want %d (initial load + post-delete refresh)", got, want)
	}
	if refreshed.screen != screenRepositories {
		t.Fatalf("screen after refresh = %q, want %q", refreshed.screen, screenRepositories)
	}
}

// TestDeleteProjectConfirmMixedAuthorizationPartialFailure is this task's
// point-5 characterization: a mixed-authorization scenario where the caller
// has repo-admin rights on 2 of the 3 repositories in "team" (simulated here
// via the fake QueryService, mirroring how every other TUI delete test
// exercises authorization failure through a plain returned error rather than
// re-deriving Service.authorizeDeleteRepository's own already-tested logic).
// It must attempt every repository (best-effort, no early abort), never
// crash, and report the correct 2-succeeded/1-failed summary naming the
// failed repository and its reason -- never misreported as a clean success
// or a clean failure.
func TestDeleteProjectConfirmMixedAuthorizationPartialFailure(t *testing.T) {
	t.Parallel()

	service := projectDeleteFakeService()
	service.deleteRepositoryErrByRepo = map[string]error{
		"team/api": errors.New("forbidden: caller lacks repo-admin rights on team/api"),
	}
	onTeam := teamProjectReadyModel(t, service)
	pending := runKey(t, onTeam, "d")

	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	afterDeleteUpdated, _ := firedModel.Update(deleteCmd())
	afterDelete := afterDeleteUpdated.(Model)

	if got, want := service.calls.deleteRepository, 3; got != want {
		t.Fatalf("DeleteRepository called %d times, want %d (best-effort must attempt every repository)", got, want)
	}
	if !strings.Contains(afterDelete.status, `Project "team" partially deleted: 2 succeeded, 1 failed`) {
		t.Fatalf("status = %q, want the 2-succeeded/1-failed summary", afterDelete.status)
	}
	if !strings.Contains(afterDelete.status, "team/api") {
		t.Fatalf("status = %q, want it to name the failed repository", afterDelete.status)
	}
	if !strings.Contains(afterDelete.status, "forbidden") {
		t.Fatalf("status = %q, want it to name the failure reason", afterDelete.status)
	}
	if afterDelete.projects.Confirm.Active() {
		t.Fatal("projects.Confirm.Active() = true, want false after result")
	}
}

// TestDeleteProjectConfirmAllFail covers the full-failure path: every
// repository fails (e.g. -delete-enabled got disabled mid-operation), which
// must be reported honestly as a full failure -- never as a silent success.
func TestDeleteProjectConfirmAllFail(t *testing.T) {
	t.Parallel()

	service := projectDeleteFakeService()
	service.deleteRepositoryErr = errors.New("repository deletion is not enabled")
	onTeam := teamProjectReadyModel(t, service)
	pending := runKey(t, onTeam, "d")

	enterUpdated, deleteCmd := pending.Update(testKeyEnter())
	firedModel := enterUpdated.(Model)
	afterDeleteUpdated, _ := firedModel.Update(deleteCmd())
	afterDelete := afterDeleteUpdated.(Model)

	if got, want := service.calls.deleteRepository, 3; got != want {
		t.Fatalf("DeleteRepository called %d times, want %d (best-effort must attempt every repository)", got, want)
	}
	if !strings.Contains(afterDelete.status, `Project "team" delete failed for all 3 repository(s)`) {
		t.Fatalf("status = %q, want the all-failed summary", afterDelete.status)
	}
	if !strings.Contains(afterDelete.status, "repository deletion is not enabled") {
		t.Fatalf("status = %q, want it to name the failure reason", afterDelete.status)
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
	updated = openUsersFromOverview(t, updated)
	view := ansi.Strip(updated.View().Content)

	if !strings.Contains(view, "/ search") || !strings.Contains(view, "alice") || !strings.Contains(view, "admin") {
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
		resetPasswordErr: errors.New("mutate admin resource: password is too common"),
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = openUsersFromOverview(t, updated)

	updated = runKey(t, updated, "p")
	updated = runKey(t, updated, "long-enough-pass")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "long-enough-pass")
	updated = runKey(t, updated, "enter")

	if updated.screen != screenAdminChangePassword {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminChangePassword)
	}
	view := ansi.Strip(updated.View().Content)
	if !strings.Contains(view, "Reset password for alice") || !strings.Contains(view, "password is too common") {
		t.Fatalf("view = %q, want recoverable backend error", view)
	}
	if strings.Contains(view, "long-enough-pass") {
		t.Fatalf("view leaks the password:\n%s", view)
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
	updated = openUsersFromOverview(t, updated)
	updated = runKey(t, updated, "j")
	updated = runKey(t, updated, "d")

	if !strings.Contains(ansi.Strip(updated.View().Content), "Disable user?") {
		t.Fatalf("view = %q, want disable confirmation", updated.View().Content)
	}

	updated = runKey(t, updated, "enter")

	if adminClient.disableCalls != 1 || adminClient.listUsersCalls != 2 {
		t.Fatalf("admin client calls = %#v, want one disable and refresh", adminClient)
	}
	if user, _ := usersScreenOf(t, updated).current(); user.ID != "u-2" {
		t.Fatalf("selected user = %q, want u-2", user.ID)
	}
	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminUsers)
	}
	view := ansi.Strip(updated.View().Content)
	if !strings.Contains(view, `User "bob" disabled.`) || !strings.Contains(view, "disabled") {
		t.Fatalf("view = %q, want refreshed disabled status", view)
	}
}

func TestModelNoSelectedUserBlocksEditing(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 6, 22, 20, 0, 0, time.UTC)}}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = openUsersFromOverview(t, updated)
	updated = runKey(t, updated, "enter")

	if !strings.Contains(ansi.Strip(updated.View().Content), "Select a user first.") {
		t.Fatalf("view = %q, want blocked edit state", updated.View().Content)
	}
	if updated.screen != screenAdminUsers {
		t.Fatalf("screen = %q, want to stay on the Users list", updated.screen)
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
	updated = openUsersFromOverview(t, updated)
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "t")
	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, "console")
	updated = runKey(t, updated, "enter")

	if adminClient.createTokenCalls != 1 {
		t.Fatalf("createTokenCalls = %d, want 1", adminClient.createTokenCalls)
	}
	view := ansi.Strip(updated.View().Content)
	if !strings.Contains(view, "Shown once. Copy it before leaving") || !strings.Contains(view, "super-secret") {
		t.Fatalf("view = %q, want one-time secret reveal", view)
	}

	// Done drops the secret for good; only then can a token be revoked.
	updated = runKey(t, updated, "enter")
	if strings.Contains(ansi.Strip(updated.View().Content), "super-secret") {
		t.Fatalf("view = %q, want the secret gone after Done", updated.View().Content)
	}
	updated = runKey(t, updated, "x")
	if !strings.Contains(ansi.Strip(updated.View().Content), `Revoke token "unnamed" (tok_abc) for "alice"?`) {
		t.Fatalf("view = %q, want revoke confirmation", updated.View().Content)
	}
	updated = runKey(t, updated, "enter")

	if adminClient.revokeTokenCalls != 1 {
		t.Fatalf("revokeTokenCalls = %d, want 1", adminClient.revokeTokenCalls)
	}
	updated = runKey(t, updated, "esc")
	if updated.screen != screenAdminEditUser {
		t.Fatalf("Esc from a user's tokens went to %q, want the profile", updated.screen)
	}
	if strings.Contains(ansi.Strip(updated.View().Content), "super-secret") {
		t.Fatalf("view = %q, want token secret cleared after leaving tokens panel", updated.View().Content)
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
	updated = runKey(t, updated, "esc") // leave the admin panel for the Console
	updated = runKey(t, updated, "g")

	if updated.screen != screenAdminLogin {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminLogin)
	}
	if updated.adminAuth != adminAuthStateExpired {
		t.Fatalf("adminAuth = %q, want %q", updated.adminAuth, adminAuthStateExpired)
	}
	if !strings.Contains(updated.View().Content, AdminSessionExpiredReasonExpired) {
		t.Fatalf("view = %q, want expiry-specific relogin message", updated.View().Content)
	}
}

func TestModelFeatureViewRendersPageAndAllowsDeclaredAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
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
	updated = openSidebarItem(t, updated, shellNavImageScanning)
	if updated.screen != screenSecurityTrivy {
		t.Fatalf("screen = %q, want %q", updated.screen, screenSecurityTrivy)
	}
	if adminClient.getFeaturePageCalls != 1 {
		t.Fatalf("getFeaturePageCalls = %d, want one page read", adminClient.getFeaturePageCalls)
	}
	view := updated.View().Content
	for _, want := range []string{"TRIVY CONFIGURATION", "Runtime ready", "version 0.57.1", "x disable"} {
		if !strings.Contains(ansi.Strip(view), want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}

	updated = runKey(t, updated, "x")
	if !strings.Contains(updated.View().Content, `Confirm disable feature "trivy"?`) {
		t.Fatalf("view = %q, want feature disable confirmation", updated.View().Content)
	}

	updated = runKey(t, updated, "enter")
	if adminClient.executeFeatureActionCalls != 1 || adminClient.lastFeatureAction != "disable" {
		t.Fatalf("feature action calls = %#v, want one disable action", adminClient)
	}
	if updated.screen != screenSecurityTrivy {
		t.Fatalf("screen = %q, want %q after disable", updated.screen, screenSecurityTrivy)
	}
	if !strings.Contains(updated.View().Content, `Feature "trivy" disabled.`) {
		t.Fatalf("view = %q, want the action's own message", updated.View().Content)
	}
	// The message gives way to the feature's state on the next key.
	updated = runKey(t, updated, "r")
	if !strings.Contains(ansi.Strip(updated.View().Content), `Feature disabled`) || !strings.Contains(ansi.Strip(updated.View().Content), "e enable") {
		t.Fatalf("view = %q, want the disabled feature state and its enable action", ansi.Strip(updated.View().Content))
	}
}

func TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePages: map[string]ports.FeaturePage{
			"trivy":    {Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}, Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}}},
			"gitleaks": {Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: false, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "false"}}, Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: `Confirm enable feature "gitleaks"?`}}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSecretScanning)

	// Navigating into a feature's own screen is what triggers its
	// independent page load and backend-authoritative help.
	if updated.screen != screenSecurityGitleaksConfig {
		t.Fatalf("screen = %q, want %q", updated.screen, screenSecurityGitleaksConfig)
	}
	if adminClient.getFeaturePageCalls != 1 {
		t.Fatalf("getFeaturePageCalls = %d, want one page read for gitleaks", adminClient.getFeaturePageCalls)
	}
	view := updated.View().Content
	for _, want := range []string{"Secret scanning", "e enable"} {
		if !strings.Contains(ansi.Strip(view), want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	if strings.Contains(ansi.Strip(view), "x disable") {
		t.Fatalf("view = %q, want backend-authoritative action help for selected feature", view)
	}
}

func TestModelTrivyFeatureDefaultsToConfigTab(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	t.Run("trivy defaults to the config tab", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage: ports.FeaturePage{
				Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
				Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		// The sidebar's Image scanning item opens trivyConfigScreen, the
		// Runtime tab's successor screen -- this IS the default landing
		// content once trivy is entered (trivyReposScreen, the Repository
		// Alerts successor, is reached separately via Tab).
		updated = openSidebarItem(t, updated, shellNavImageScanning)

		view := updated.View().Content
		for _, want := range []string{"Config", "Repositories", "TRIVY CONFIGURATION", "version 0.57.1"} {
			if !strings.Contains(ansi.Strip(view), want) {
				t.Fatalf("view = %q, want %q", view, want)
			}
		}
		if strings.Contains(view, "No repository alerts") {
			t.Fatalf("view = %q, want runtime tab as default landing content", view)
		}
	})
}

func TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
			Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			Actions:  []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavImageScanning)

	updated = runKey(t, updated, "c")
	modalView := updated.View().Content
	for _, want := range []string{"Trivy configuration", "Scheduled scans", "Interval", "Timeout", "Registry Reachable URL", "Max Concurrency"} {
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
	if strings.Contains(canceled.View().Content, "Trivy configuration") {
		t.Fatalf("view = %q, want modal closed after esc", canceled.View().Content)
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
	if !strings.Contains(submitted.View().Content, "Configuration saved") {
		t.Fatalf("view = %q, want config feedback after submit", submitted.View().Content)
	}
}

// TestModelGitleaksConfigModalOpenCancelAndSubmitCurrentSettingsOnly mirrors
// TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly for the
// gitleaks global config modal, opened with `s` (not `c`, which stays
// Trivy-only) on a highlighted gitleaks feature row. Unlike Trivy's 5-field
// modal, gitleaks has exactly 3: Enabled, Timeout, MaxConcurrency —
// ScheduleEnabled/Interval/RegistryReachableURL never apply (gitleaks scans
// immutable content once and never pulls from the registry over HTTP).
func TestModelGitleaksConfigModalOpenCancelAndSubmitCurrentSettingsOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePages: map[string]ports.FeaturePage{
			"trivy": {
				Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}},
			},
			"gitleaks": {
				Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{
					{Label: "Schedule Enabled", Value: "false"},
					{Label: "Interval", Value: "24h0m0s"},
					{Label: "Timeout", Value: "5m0s"},
					{Label: "Registry Reachable URL", Value: ""},
					{Label: "Max Concurrency", Value: "1"},
				}}},
				Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "gitleaks"?`}},
			},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSecretScanning)

	if got, want := updated.screen, screenSecurityGitleaksConfig; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}

	updated = runKey(t, updated, "c")
	modalView := updated.View().Content
	for _, want := range []string{"Gitleaks configuration", "Enabled", "Timeout", "Max Concurrency"} {
		if !strings.Contains(modalView, want) {
			t.Fatalf("view = %q, want %q", modalView, want)
		}
	}
	// The Feature Page's own generic "Configuration" section legitimately
	// shows Schedule Enabled/Interval/Registry Reachable URL underneath the
	// modal (buildFeaturePage is shared by every builtin feature), so the
	// modal's own field separation is asserted directly on its render
	// output rather than the full composited view (covered exhaustively by
	// TestRenderGitleaksConfigModalIsASeparateSurfaceFromTrivyConfigModal).
	screen, ok := updated.adminScreens[slotGitleaksConfig].(gitleaksConfigScreen)
	if !ok {
		t.Fatalf("adminScreens[slotGitleaksConfig] = %#v, want a mounted gitleaksConfigScreen", updated.adminScreens[slotGitleaksConfig])
	}
	if got, want := screen.cfg, (gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldEnabled, Enabled: true, Timeout: "5m", MaxConcurrency: "1"}); got != want {
		t.Fatalf("adminScreens[slotGitleaksConfig].cfg = %#v, want %#v", got, want)
	}
	modalOnly := screen.View(newAdminTheme(), screenEnv{}).Overlay
	for _, hidden := range []string{"Schedule Enabled", "Interval", "Registry Reachable URL"} {
		if strings.Contains(modalOnly, hidden) {
			t.Fatalf("gitleaksConfigScreen.View() = %q, want unsupported field %q hidden", modalOnly, hidden)
		}
	}

	canceled := runKey(t, updated, "esc")
	if adminClient.configureFeatureCalls != 0 {
		t.Fatalf("configureFeatureCalls = %d, want cancel to keep modal client-idle", adminClient.configureFeatureCalls)
	}
	if strings.Contains(canceled.View().Content, "Gitleaks configuration") {
		t.Fatalf("view = %q, want modal closed after esc", canceled.View().Content)
	}

	submitted := runKey(t, updated, "enter")
	if adminClient.configureFeatureCalls != 1 {
		t.Fatalf("configureFeatureCalls = %d, want one gitleaks config submit", adminClient.configureFeatureCalls)
	}
	if adminClient.lastConfiguredFeature != "gitleaks" {
		t.Fatalf("lastConfiguredFeature = %q, want gitleaks", adminClient.lastConfiguredFeature)
	}
	if adminClient.lastConfigureInput.Enabled == nil || adminClient.lastConfigureInput.Timeout == nil || adminClient.lastConfigureInput.MaxConcurrency == nil {
		t.Fatalf("lastConfigureInput = %#v, want current gitleaks Enabled/Timeout/MaxConcurrency payload", adminClient.lastConfigureInput)
	}
	if adminClient.lastConfigureInput.ScheduleEnabled != nil || adminClient.lastConfigureInput.Interval != nil || adminClient.lastConfigureInput.RegistryReachableURL != nil {
		t.Fatalf("lastConfigureInput = %#v, want ScheduleEnabled/Interval/RegistryReachableURL omitted (rejected design decision, gitleaks-irrelevant fields)", adminClient.lastConfigureInput)
	}
	if !strings.Contains(submitted.View().Content, "Configuration saved") {
		t.Fatalf("view = %q, want config feedback after submit", submitted.View().Content)
	}
	// Phase 11 deviation from the pre-promotion assertion: gitleaksConfigScreen
	// is now a persistent top-level screen (screenSecurityGitleaksConfig),
	// not an ephemeral overlay -- it stays mounted after a successful
	// submit (its own page reloads in place), only its cfg modal closes.
	submittedScreen, ok := submitted.adminScreens[slotGitleaksConfig].(gitleaksConfigScreen)
	if !ok {
		t.Fatalf("adminScreens[slotGitleaksConfig] = %#v, want a mounted gitleaksConfigScreen", submitted.adminScreens[slotGitleaksConfig])
	}
	if submittedScreen.cfg.Active() {
		t.Fatalf("adminScreens[slotGitleaksConfig].cfg = %#v, want closed (Active()==false) after submit", submittedScreen.cfg)
	}
}

// TestModelGitleaksConfigModalValidationErrorSurfaced mirrors the Trivy
// modal's invalid-duration guard: an unparsable Timeout must surface the
// error in the modal and must not reach the admin API.
func TestModelGitleaksConfigModalValidationErrorSurfaced(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
			Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{
				{Label: "Timeout", Value: "5m0s"},
				{Label: "Max Concurrency", Value: "1"},
			}}},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSecretScanning)
	updated = runKey(t, updated, "c")

	// Focus starts on Enabled; Tab once lands on Timeout.
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "backspace")
	updated = runKey(t, updated, "backspace")
	updated = runKey(t, updated, "backspace")
	updated = runKey(t, updated, "backspace")
	updated = runKey(t, updated, "backspace")
	updated = runKey(t, updated, "x")

	submitted := runKey(t, updated, "enter")
	if adminClient.configureFeatureCalls != 0 {
		t.Fatalf("configureFeatureCalls = %d, want validation failure to block the submit", adminClient.configureFeatureCalls)
	}
	if !strings.Contains(submitted.View().Content, "invalid timeout") {
		t.Fatalf("view = %q, want invalid timeout error surfaced", submitted.View().Content)
	}
}

// TestGitleaksConfigModalCharacterization is the tui-menu-architecture
// change's Phase 1 task 1.2 (T1.0) golden/characterization baseline,
// captured BEFORE screen_gitleaks_config.go exists: table-driven over Esc,
// Tab x4 (the field-wrap cycle), Space, Backspace, Enter, plain rune input,
// and an unmapped key, every assertion black-box via View()/call-counter
// content only — never touching updated.adminView.GitleaksConfigModal's
// fields directly — so this test survives unchanged once Phase 6 moves this
// state into gitleaksConfigScreen and deletes the AdminViewState field.
func TestGitleaksConfigModalCharacterization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 14, 0, 0, 0, time.UTC)
	newReady := func(t *testing.T) (Model, *fakeAdminClient) {
		t.Helper()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{
					{Label: "Timeout", Value: "5m0s"},
					{Label: "Max Concurrency", Value: "1"},
				}}},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavSecretScanning)
		updated = runKey(t, updated, "c")
		if !strings.Contains(updated.View().Content, "Gitleaks configuration") {
			t.Fatalf("test setup invalid: view = %q, want the gitleaks modal open", updated.View().Content)
		}
		return updated, adminClient
	}

	t.Run("esc closes without submitting", func(t *testing.T) {
		t.Parallel()
		opened, client := newReady(t)
		closed := runKey(t, opened, "esc")
		if strings.Contains(closed.View().Content, "Gitleaks configuration") {
			t.Fatalf("view after esc = %q, want the modal closed", closed.View().Content)
		}
		if client.configureFeatureCalls != 0 {
			t.Fatalf("configureFeatureCalls = %d, want 0 after esc", client.configureFeatureCalls)
		}
	})

	t.Run("space toggles enabled while focus is on enabled", func(t *testing.T) {
		t.Parallel()
		// Round-trip identity, not exact-text matching: the modal renders
		// inside compositeOverlay's column-interleaved workspace, where a
		// literal "Enabled\non" substring search collides with unrelated
		// base-page content on the same physical row. One toggle must
		// change the view; two toggles must exactly restore it.
		opened, _ := newReady(t)
		toggledOnce := runKey(t, opened, " ")
		if toggledOnce.View().Content == opened.View().Content {
			t.Fatalf("view after one space = unchanged, want Enabled to visibly flip")
		}
		toggledTwice := runKey(t, toggledOnce, " ")
		if toggledTwice.View().Content != opened.View().Content {
			t.Fatalf("view after two spaces = %q, want it to exactly restore the original %q", toggledTwice.View().Content, opened.View().Content)
		}
	})

	t.Run("tab x4 wraps focus back to timeout", func(t *testing.T) {
		t.Parallel()
		opened, _ := newReady(t)
		afterFourTabs := opened
		for i := 0; i < 4; i++ {
			afterFourTabs = runKey(t, afterFourTabs, "tab")
		}
		typed := runKey(t, afterFourTabs, "9")
		if !strings.Contains(ansi.Strip(typed.View().Content), "5m9") {
			t.Fatalf("view after tab x4 + rune = %q, want the rune appended to Timeout (0:Enabled -> 1:Timeout -> 2:MaxConcurrency -> 0:Enabled -> 1:Timeout)", ansi.Strip(typed.View().Content))
		}
		if strings.Contains(ansi.Strip(typed.View().Content), "19") {
			t.Fatalf("view after tab x4 + rune = %q, want MaxConcurrency (\"1\") untouched", ansi.Strip(typed.View().Content))
		}
	})

	t.Run("backspace trims the focused field", func(t *testing.T) {
		t.Parallel()
		// Round-trip identity again (see the space test's comment above):
		// removing "m" from "5m" must change the view, and typing "m"
		// back must exactly restore it.
		opened, _ := newReady(t)
		onTimeout := runKey(t, opened, "tab")
		trimmed := runKey(t, onTimeout, "backspace")
		if trimmed.View().Content == onTimeout.View().Content {
			t.Fatalf("view after backspace = unchanged, want Timeout's trailing rune removed")
		}
		restored := runKey(t, trimmed, "m")
		if restored.View().Content != onTimeout.View().Content {
			t.Fatalf("view after backspace+\"m\" = %q, want it to exactly restore %q", restored.View().Content, onTimeout.View().Content)
		}
	})

	t.Run("enter submits with the current settings", func(t *testing.T) {
		t.Parallel()
		opened, client := newReady(t)
		submitted := runKey(t, opened, "enter")
		if client.configureFeatureCalls != 1 {
			t.Fatalf("configureFeatureCalls = %d, want 1 after enter with valid fields", client.configureFeatureCalls)
		}
		if client.lastConfiguredFeature != "gitleaks" {
			t.Fatalf("lastConfiguredFeature = %q, want gitleaks", client.lastConfiguredFeature)
		}
		if strings.Contains(submitted.View().Content, "Gitleaks configuration") {
			t.Fatalf("view after successful submit = %q, want the modal closed", submitted.View().Content)
		}
	})

	t.Run("plain rune input appends to the focused text field", func(t *testing.T) {
		t.Parallel()
		opened, _ := newReady(t)
		onTimeout := runKey(t, opened, "tab")
		typed := runKey(t, onTimeout, "9")
		if !strings.Contains(typed.View().Content, "5m9") {
			t.Fatalf("view after rune = %q, want \"9\" appended to Timeout", typed.View().Content)
		}
	})

	t.Run("unmapped key is a no-op", func(t *testing.T) {
		t.Parallel()
		opened, client := newReady(t)
		before := opened.View().Content
		updated, cmd := opened.Update(testKeyLeft())
		after := updated.(Model)
		if cmd != nil {
			t.Fatalf("Update(unmapped key) returned a non-nil cmd, want nil")
		}
		if after.View().Content != before {
			t.Fatalf("view after an unmapped key = %q, want unchanged %q", after.View().Content, before)
		}
		if client.configureFeatureCalls != 0 {
			t.Fatalf("configureFeatureCalls = %d, want 0", client.configureFeatureCalls)
		}
	})
}

func TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
		scanPolicy: ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	// ScanPolicy loads chained after trivyConfigScreen's OWN page load, once
	// navigated into via the sidebar's Image scanning item.
	updated = openSidebarItem(t, updated, shellNavImageScanning)

	if got, want := adminClient.getScanPolicyCalls, 1; got != want {
		t.Fatalf("getScanPolicyCalls = %d, want %d", got, want)
	}
	trivyScreen, ok := updated.adminScreens[slotTrivyConfig].(trivyConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyConfig] not a mounted trivyConfigScreen")
	}
	if got, want := trivyScreen.policy, (ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}); got != want {
		t.Fatalf("trivyConfigScreen.policy = %#v, want %#v", got, want)
	}

	updated = runKey(t, updated, "p")
	modalView := updated.View().Content
	for _, want := range []string{"Policy enabled", "Severity threshold", "CRITICAL"} {
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
	submittedScreen, ok := submitted.adminScreens[slotTrivyConfig].(trivyConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyConfig] not a mounted trivyConfigScreen after submit")
	}
	if got, want := submittedScreen.policy, (ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}); got != want {
		t.Fatalf("trivyConfigScreen.policy = %#v, want %#v", got, want)
	}
	if submittedScreen.policyModal.Active() {
		t.Fatalf("trivyConfigScreen.policyModal = %#v, want closed after submit", submittedScreen.policyModal)
	}
	if !strings.Contains(submitted.View().Content, "Vulnerability policy saved") {
		t.Fatalf("view = %q, want policy feedback after submit", submitted.View().Content)
	}
}

// TestModelSigningPolicyModalOpenerKeyIsScopedToSigningFeature is the Phase
// 9 task 9.9 RED test (design.md Decision 11 piece 2): `p` with the signing
// feature selected opens signingPolicyModal, and `p` with a different
// feature selected (trivy) still opens scanPolicyModal, not
// signingPolicyModal -- no key collision, guarded by
// isSelectedSigningFeature() vs. isSelectedTrivyFeature().
func TestModelSigningPolicyModalOpenerKeyIsScopedToSigningFeature(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 15, 0, 0, 0, time.UTC)

	t.Run("p opens signingPolicyModal when signing is selected", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
			},
			signingPolicy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"key-one"}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavSigningPolicy)

		if got, want := adminClient.getSigningPolicyCalls, 1; got != want {
			t.Fatalf("getSigningPolicyCalls = %d, want %d", got, want)
		}

		updated = runKey(t, updated, "p")
		signingScreen, ok := updated.adminScreens[slotSigningConfig].(signingConfigScreen)
		if !ok || !signingScreen.cfg.Active() {
			t.Fatal("adminScreens[slotSigningConfig] cfg.Active() = false, want true after 'p' on the signing feature")
		}
		if updated.adminScreens[slotTrivyConfig] != nil {
			t.Fatal("adminScreens[slotTrivyConfig] != nil, want unmounted -- 'p' on signing must not touch the trivy screen")
		}
		modalView := updated.View().Content
		for _, want := range []string{"Signing policy", "Pull-time gate enforced", "Trusted Keys"} {
			if !strings.Contains(modalView, want) {
				t.Fatalf("view = %q, want %q", modalView, want)
			}
		}
	})

	t.Run("p opens scanPolicyModal, not signingPolicyModal, when trivy is selected", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "p")

		trivyScreen, ok := updated.adminScreens[slotTrivyConfig].(trivyConfigScreen)
		if !ok || !trivyScreen.policyModal.Active() {
			t.Fatal("trivyConfigScreen.policyModal.Active() = false, want true after 'p' on the trivy feature")
		}
		if updated.adminScreens[slotSigningConfig] != nil {
			t.Fatal("adminScreens[slotSigningConfig] != nil, want unmounted -- 'p' on trivy must not open the signing screen")
		}
	})
}

// TestModelSigningPolicyModalOpenSeedsUnsignedSelfReadFromLoadedSettings is
// the RED test for the UnsignedSelfRead TUI surface: opening
// signingPolicyModal seeds UnsignedSelfRead from the already-loaded
// SigningPolicy rather than leaving it at its zero value, normalizing a
// stored "" to the modal's canonical "off".
func TestModelSigningPolicyModalOpenSeedsUnsignedSelfReadFromLoadedSettings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
		signingPolicy: ports.SigningPolicySettings{Enabled: true, UnsignedSelfRead: "repo_push"},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "p")

	signingScreen, ok := updated.adminScreens[slotSigningConfig].(signingConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotSigningConfig] not mounted after 'p'")
	}
	if got, want := signingScreen.cfg.UnsignedSelfRead, "repo_push"; got != want {
		t.Fatalf("cfg.UnsignedSelfRead = %q, want %q seeded from the loaded policy", got, want)
	}
}

// TestModelSigningPolicyModalSaveIncludesUnsignedSelfRead is the RED test
// for the bug found while scoping this change: saving the modal after
// changing an unrelated field (Enabled) used to silently wipe
// UnsignedSelfRead back to "" because the modal's Enter
// handler never included it in the save payload. Also verifies cycling the
// field itself with Space persists the new value.
func TestModelSigningPolicyModalSaveIncludesUnsignedSelfRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	newModel := func() (Model, *fakeAdminClient) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
			},
			signingPolicy: ports.SigningPolicySettings{Enabled: true, UnsignedSelfRead: "repo_push"},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavSigningPolicy)
		updated = runKey(t, updated, "p")
		return updated, adminClient
	}

	t.Run("saving after touching an unrelated field preserves the loaded value", func(t *testing.T) {
		t.Parallel()
		updated, adminClient := newModel()

		// Focus starts on Enabled; toggle it without touching UnsignedSelfRead.
		submitted := runKey(t, runKey(t, updated, " "), "enter")

		if got, want := adminClient.lastSigningPolicyInput.UnsignedSelfRead, "repo_push"; got != want {
			t.Fatalf("lastSigningPolicyInput.UnsignedSelfRead = %q, want %q preserved from the loaded policy", got, want)
		}
		signingScreen, ok := submitted.adminScreens[slotSigningConfig].(signingConfigScreen)
		if !ok {
			t.Fatal("adminScreens[slotSigningConfig] not mounted after save")
		}
		if got, want := signingScreen.cfg.UnsignedSelfRead, "repo_push"; got != want {
			t.Fatalf("cfg.UnsignedSelfRead = %q, want %q after save", got, want)
		}
	})

	t.Run("cycling the field with Space persists the new value on save", func(t *testing.T) {
		t.Parallel()
		updated, adminClient := newModel()

		// Tab from Enabled to UnsignedSelfRead, cycle repo_push -> off.
		updated = runKey(t, updated, "tab")
		updated = runKey(t, updated, " ")
		submitted := runKey(t, updated, "enter")

		if got, want := adminClient.lastSigningPolicyInput.UnsignedSelfRead, "off"; got != want {
			t.Fatalf("lastSigningPolicyInput.UnsignedSelfRead = %q, want %q after cycling", got, want)
		}
		signingScreen, ok := submitted.adminScreens[slotSigningConfig].(signingConfigScreen)
		if !ok {
			t.Fatal("adminScreens[slotSigningConfig] not mounted after save")
		}
		if got, want := signingScreen.cfg.UnsignedSelfRead, "off"; got != want {
			t.Fatalf("cfg.UnsignedSelfRead = %q, want %q after save", got, want)
		}
	})
}

// TestModelSigningPolicyModalAddKeySubmitPersistsAndReflectsCurrentSettings
// is the Phase 9 task 9.10 RED test (operator-admin-tui spec's "Operator
// saves a policy change" scenario), updated for the signing-key-management
// change: the retired single-line AddKey field is now trustedKeyList's own
// add flow ('n' to start, type the key, Enter to commit it into the LOCAL
// list, then Enter again -- now not consumed by the list, since it is back
// in idle navigation -- to submit the whole policy). Toggling Enabled and
// adding a key still persists through the admin API and is reflected back
// into the modal (cfg.Keys), the modal stays open (unlike scanPolicyModal)
// so the operator can keep adding keys.
func TestModelSigningPolicyModalAddKeySubmitPersistsAndReflectsCurrentSettings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 15, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: false},
		},
		signingPolicy: ports.SigningPolicySettings{Enabled: false},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "p")

	// Focus starts on Enabled; toggle it on, Tab past UnsignedSelfRead to
	// the Keys field, 'n' to start adding, type a real key, Enter to
	// commit it into the local list.
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, trustedKeyListTestPEM1)
	updated = runKey(t, updated, "enter")

	// A second Enter, now that the list is back in idle navigation (not
	// consumed by trustedKeyList), submits the whole policy.
	submitted := runKey(t, updated, "enter")

	wantKeyPEM, err := signing.NormalizePublicKeyPEM(trustedKeyListTestPEM1)
	if err != nil {
		t.Fatalf("signing.NormalizePublicKeyPEM(trustedKeyListTestPEM1) error = %v", err)
	}

	if got, want := adminClient.updateSigningPolicyCalls, 1; got != want {
		t.Fatalf("updateSigningPolicyCalls = %d, want %d", got, want)
	}
	if !adminClient.lastSigningPolicyInput.Enabled {
		t.Fatalf("lastSigningPolicyInput.Enabled = false, want true")
	}
	if got, want := adminClient.lastSigningPolicyInput.TrustedPublicKeys, []string{wantKeyPEM}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lastSigningPolicyInput.TrustedPublicKeys = %#v, want %#v", got, want)
	}
	submittedScreen, ok := submitted.adminScreens[slotSigningConfig].(signingConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotSigningConfig] not mounted after save")
	}
	if !submittedScreen.cfg.Active() {
		t.Fatal("cfg.Active() = false, want the screen to stay mounted/open after a successful save (unlike scanPolicyModal)")
	}
	if submittedScreen.cfg.Keys.adding {
		t.Fatal("cfg.Keys.adding = true, want cleared after a successful submit")
	}
	if len(submittedScreen.cfg.Keys.Keys()) != 1 {
		t.Fatalf("cfg.Keys.Keys() = %#v, want 1 key reflected from the saved policy", submittedScreen.cfg.Keys.Keys())
	}
	if !strings.Contains(submitted.View().Content, "Signing policy saved") {
		t.Fatalf("view = %q, want signing policy feedback after submit", submitted.View().Content)
	}

	// ClearKeys: Tab twice more (Keys -> Identities -> ClearKeys, the new
	// Identities field inserted by signing-keyless-verification between
	// them), Enter asks to clear every trusted key and a second Enter confirms.
	cleared := runKey(t, submitted, "tab")
	cleared = runKey(t, cleared, "tab")
	cleared = runKey(t, cleared, "enter")
	if got, want := adminClient.updateSigningPolicyCalls, 1; got != want {
		t.Fatalf("updateSigningPolicyCalls = %d, want %d before the clear is confirmed", got, want)
	}
	cleared = runKey(t, cleared, "enter")
	if got, want := adminClient.updateSigningPolicyCalls, 2; got != want {
		t.Fatalf("updateSigningPolicyCalls = %d, want %d after Clear", got, want)
	}
	if len(adminClient.lastSigningPolicyInput.TrustedPublicKeys) != 0 {
		t.Fatalf("lastSigningPolicyInput.TrustedPublicKeys = %#v, want empty after Clear", adminClient.lastSigningPolicyInput.TrustedPublicKeys)
	}
	clearedScreen, ok := cleared.adminScreens[slotSigningConfig].(signingConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotSigningConfig] not mounted after Clear")
	}
	if len(clearedScreen.cfg.Keys.Keys()) != 0 {
		t.Fatalf("cfg.Keys.Keys() = %#v, want empty after Clear", clearedScreen.cfg.Keys.Keys())
	}
}

// TestModelSigningPolicyModalDeleteKeyIsUsageInformedAndNeverBlocked mirrors
// TestFeatureOverridesScreenSigningDeleteKeyIsUsageInformedAndNeverBlocked
// for the GLOBAL signing policy modal (design requirement (d), global-scope
// leg): 'x' on a selected key fires the usage-count call scoped globally
// ("" repository), opens a confirm with the count folded in regardless of
// how high it is, and Enter removes exactly that key.
func TestModelSigningPolicyModalDeleteKeyIsUsageInformedAndNeverBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
		signingPolicy:         ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1}},
		signingKeyUsageCount:  7,
		signingKeyUsageCapped: false,
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "p") // opens signingPolicyModal
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "tab") // Enabled -> UnsignedSelfRead -> Keys

	deleted := runKey(t, updated, "x")

	if got, want := adminClient.signingKeyUsageCalls, 1; got != want {
		t.Fatalf("signingKeyUsageCalls = %d, want %d", got, want)
	}
	if got, want := adminClient.lastSigningKeyUsageRepo, ""; got != want {
		t.Fatalf("lastSigningKeyUsageRepo = %q, want %q (global scope)", got, want)
	}
	screenBeforeConfirm, _ := deleted.adminScreens[slotSigningConfig].(signingConfigScreen)
	if !screenBeforeConfirm.cfg.Keys.confirm.Active() {
		t.Fatal("cfg.Keys.confirm.Active() = false, want true")
	}

	confirmed := runKey(t, deleted, "enter")
	screenAfterConfirm, _ := confirmed.adminScreens[slotSigningConfig].(signingConfigScreen)
	if screenAfterConfirm.cfg.Keys.confirm.Active() {
		t.Fatal("confirm still active after Enter, want closed")
	}
	if len(screenAfterConfirm.cfg.Keys.Keys()) != 0 {
		t.Fatalf("cfg.Keys.Keys() = %#v, want empty -- confirming must remove the key regardless of the reported count", screenAfterConfirm.cfg.Keys.Keys())
	}
}

// TestModelTrivyOverrideEditorOpenerKeyIsScopedToRepositoryAlertsRow is the
// Slice 2 successor to the retired
// TestModelRepositoryOverrideModalOpenerKeyIsScopedToRepositoryAlertsRow
// (operator-admin-tui spec's "The override key is scoped to the opening
// screen's row only" scenario, design.md Decision F): `o` on a highlighted
// Repository Alerts row mounts the uniform overrideEditor bound to that
// repository and trivyFeatureName and fires a load; `o` on the Runtime tab,
// or on Repository Alerts with no rows loaded, must not open it and must
// not collide with featureActionForKey's fallback. 'o' keeps its exact
// current keybinding/meaning on Trivy's own Repository Alerts row
// (user-confirmed decision).
func TestModelTrivyOverrideEditorOpenerKeyIsScopedToRepositoryAlertsRow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	baseFeaturePage := ports.FeaturePage{
		Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
	}

	t.Run("o opens the editor on a highlighted Repository Alerts row", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage:  baseFeaturePage,
			scanRuns: []ports.ScanRun{
				{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1},
			},
			repositoryOverrides: map[string]ports.RepositoryOverrideDetails{
				"trivy/team/api": {Repository: "team/api", Feature: "trivy", Enabled: false, IgnoreFilePath: "/etc/trivy/ignore"},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "tab") // switch to trivyReposScreen (Repository Alerts)

		updated = runKey(t, updated, "o")

		// Phase 11: the uniform overrideEditor is now embedded directly in
		// trivyReposScreen (design.md's resolved-gap addendum), mirroring
		// featureOverridesScreen's own pattern -- the former slotTrivyOverride
		// overlay slot no longer exists.
		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok || !repos.editor.Active() {
			t.Fatalf("adminScreens[slotTrivyRepos].editor = %#v, want an active overrideEditor after 'o'", repos.editor)
		}
		editor := repos.editor
		if got, want := editor.repository, "team/api"; got != want {
			t.Fatalf("editor.repository = %q, want %q", got, want)
		}
		if got, want := editor.Feature(), trivyFeatureName; got != want {
			t.Fatalf("editor.Feature() = %q, want %q", got, want)
		}
		if adminClient.getRepositoryOverrideCalls != 1 {
			t.Fatalf("getRepositoryOverrideCalls = %d, want 1", adminClient.getRepositoryOverrideCalls)
		}
		if editor.loading {
			t.Fatal("editor.loading = true, want false once the load Cmd has resolved")
		}
		if !editor.exists || !strings.Contains(updated.View().Content, "team/api") {
			t.Fatalf("editor = %#v, want the stored override reflected", editor)
		}

		closed := runKey(t, updated, "esc")
		closedRepos, ok := closed.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok || closedRepos.editor.Active() {
			t.Fatalf("adminScreens[slotTrivyRepos].editor = %#v, want inactive after esc", closedRepos.editor)
		}
	})

	t.Run("o on trivyConfigScreen opens the repositories screen, not the editor, and does not collide with feature actions", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage:  baseFeaturePage,
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)

		updated = runKey(t, updated, "o")

		if updated.screen != screenSecurityTrivyRepos {
			t.Fatalf("screen = %q, want %q: 'o' on the Image scanning screen opens the repositories screen", updated.screen, screenSecurityTrivyRepos)
		}
		if repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen); !ok || repos.editor.Active() {
			t.Fatal("'o' on trivyConfigScreen must not open the override editor itself")
		}
		if adminClient.getRepositoryOverrideCalls != 0 {
			t.Fatalf("getRepositoryOverrideCalls = %d, want 0 (no editor load fired)", adminClient.getRepositoryOverrideCalls)
		}
		if adminClient.executeFeatureActionCalls != 0 {
			t.Fatalf("executeFeatureActionCalls = %d, want 0 ('o' must not fall through to featureActionForKey)", adminClient.executeFeatureActionCalls)
		}
	})

	t.Run("o with no Repository Alerts row highlighted does not open the editor", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage:  baseFeaturePage,
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "tab") // trivyReposScreen, no scan runs loaded

		updated = runKey(t, updated, "o")

		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok || repos.editor.Active() {
			t.Fatalf("adminScreens[slotTrivyRepos].editor = %#v, want inactive with no highlighted row", repos.editor)
		}
	})
}

// TestModelTrivyOverrideEditorSetAndClearRoundTripReflectsInModal is the
// Slice 2 successor to the retired
// TestModelRepositoryOverrideModalSetAndClearRoundTripReflectsInModal
// (operator-admin-tui spec's "Operator sets an override from the modal" /
// "Operator clears an override from the modal" scenarios): submitting new
// values persists through the admin API and reflects the new values back in
// the editor (still open, unlike scanPolicyModal); clearing deletes it and
// reflects the repository using global settings.
func TestModelTrivyOverrideEditorSetAndClearRoundTripReflectsInModal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
		scanRuns: []ports.ScanRun{
			{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavImageScanning)
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "o")

	// Phase 11: the uniform overrideEditor is now embedded directly in
	// trivyReposScreen (mirrors featureOverridesScreen's own pattern), not
	// a separate slotTrivyOverride slot.
	reposAfterOpen, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
	editorAfterOpen := reposAfterOpen.editor
	if !ok || !editorAfterOpen.Active() || editorAfterOpen.exists {
		t.Fatalf("editor = %#v, want open with no existing override", editorAfterOpen)
	}

	// Focus starts on Enabled (no Feature field, design.md Decision F):
	// toggle it on, Tab to PathPrimary, type a path, then Enter to submit.
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
	reposAfterSave, ok := submitted.adminScreens[slotTrivyRepos].(trivyReposScreen)
	editorAfterSave := reposAfterSave.editor
	if !ok || !editorAfterSave.Active() {
		t.Fatal("editor.Active() = false, want the editor to stay open after a successful save")
	}
	if !editorAfterSave.exists || editorAfterSave.pathPrimary != "/etc/trivy/ignore" {
		t.Fatalf("editor = %#v, want the saved override reflected", editorAfterSave)
	}
	if !strings.Contains(submitted.View().Content, "Repository override saved") {
		t.Fatalf("view = %q, want save feedback", submitted.View().Content)
	}

	// Tab to the Clear row and press Enter to clear it.
	cleared := submitted
	for {
		r, _ := cleared.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if r.editor.currentField() == overrideFieldClear {
			break
		}
		cleared = runKey(t, cleared, "tab")
	}
	cleared = runKey(t, cleared, "enter")

	if adminClient.clearRepositoryOverrideCalls != 1 {
		t.Fatalf("clearRepositoryOverrideCalls = %d, want 1", adminClient.clearRepositoryOverrideCalls)
	}
	reposAfterClear, _ := cleared.adminScreens[slotTrivyRepos].(trivyReposScreen)
	editorAfterClear := reposAfterClear.editor
	if editorAfterClear.exists {
		t.Fatal("editor.exists = true, want false after clear")
	}
	if !strings.Contains(cleared.View().Content, "inheriting global settings") {
		t.Fatalf("view = %q, want the editor to show the repository inheriting global settings after clear", cleared.View().Content)
	}

	// Pressing Enter on the Clear row again (already inheriting global) must
	// not issue another DELETE.
	inert := runKey(t, cleared, "enter")
	if adminClient.clearRepositoryOverrideCalls != 1 {
		t.Fatalf("clearRepositoryOverrideCalls = %d, want still 1 (inert when already inheriting global)", adminClient.clearRepositoryOverrideCalls)
	}
	inertRepos, _ := inert.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !strings.Contains(inertRepos.editor.err, "Already inheriting global") {
		t.Fatalf("editor.err = %q, want the inert-clear message", inertRepos.editor.err)
	}
}

// TestFeatureOverridesScreenSigningSaveIncludesUnsignedSelfRead is the Slice
// 2 successor to the retired
// TestModelRepositoryOverrideModalSigningSaveIncludesUnsignedSelfRead,
// re-targeted to Signing's own dedicated repository override screen (the
// only reachable way to edit a signing override now that the Feature cycle
// is gone): a first save establishes UnsignedSelfRead via the editor's own
// field (round-tripped back by applyOverride); a second save that only
// touches an unrelated field (Enabled) must not silently wipe
// UnsignedSelfRead back to "".
func TestFeatureOverridesScreenSigningSaveIncludesUnsignedSelfRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "o") // opens screenSecuritySigningRepos

	if got, want := updated.screen, screenSecuritySigningRepos; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	updated = runKey(t, updated, "o") // opens the editor on the highlighted (only) row

	// Focus starts on Enabled: Tab to the Keys field, 'n' to start adding,
	// type a real key, Enter to commit it into the local list, Tab twice
	// (Keys -> Identities -> UnsignedSelfRead, the new Identities field
	// inserted by signing-keyless-verification between them) and toggle
	// UnsignedSelfRead off -> pusher, then Enter (now not consumed by the
	// Keys field, back in idle navigation) to save.
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, trustedKeyListTestPEM1)
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, " ")
	firstSave := runKey(t, updated, "enter")

	if got, want := adminClient.lastSetRepositoryOverride.UnsignedSelfRead, "pusher"; got != want {
		t.Fatalf("lastSetRepositoryOverride.UnsignedSelfRead = %q, want %q", got, want)
	}
	screenAfterFirst, ok := firstSave.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if !ok {
		t.Fatalf("adminScreens[slotSigningRepos] = %#v, want featureOverridesScreen", firstSave.adminScreens[slotSigningRepos])
	}
	if got, want := screenAfterFirst.editor.unsignedSelfRead, "pusher"; got != want {
		t.Fatalf("editor.unsignedSelfRead = %q, want %q reflected after save", got, want)
	}

	// Navigate from UnsignedSelfRead -> Clear -> Enabled (2 Tabs), toggle
	// Enabled only, then save again without touching UnsignedSelfRead.
	navigated := firstSave
	for i := 0; i < 2; i++ {
		navigated = runKey(t, navigated, "tab")
	}
	navScreen, _ := navigated.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if got, want := navScreen.editor.currentField(), overrideFieldEnabled; got != want {
		t.Fatalf("currentField() = %v, want %v (Enabled) after 2 Tabs from UnsignedSelfRead", got, want)
	}
	navigated = runKey(t, navigated, " ")
	secondSave := runKey(t, navigated, "enter")

	if got, want := adminClient.setRepositoryOverrideCalls, 2; got != want {
		t.Fatalf("setRepositoryOverrideCalls = %d, want %d", got, want)
	}
	if got, want := adminClient.lastSetRepositoryOverride.UnsignedSelfRead, "pusher"; got != want {
		t.Fatalf("lastSetRepositoryOverride.UnsignedSelfRead = %q, want %q preserved from the first save", got, want)
	}
	screenAfterSecond, _ := secondSave.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if got, want := screenAfterSecond.editor.unsignedSelfRead, "pusher"; got != want {
		t.Fatalf("editor.unsignedSelfRead = %q, want %q after second save", got, want)
	}
}

// TestFeatureOverridesScreenSigningOpenPrefillsFromGlobalPolicyWhenNeverConfigured
// is the signing-key-management change's prefill requirement: opening a
// signing override for a repository with NO stored override row seeds the
// editor's key list with the CURRENT global signing policy's trusted keys,
// not an empty list.
func TestFeatureOverridesScreenSigningOpenPrefillsFromGlobalPolicyWhenNeverConfigured(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
		signingPolicy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2}},
		// repositoryOverrides deliberately left nil: no stored override row
		// for library/alpine -- GetRepositoryOverride reports (zero, false, nil).
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "o") // opens screenSecuritySigningRepos
	updated = runKey(t, updated, "o") // opens the editor on the highlighted (only) row

	screen, ok := updated.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if !ok {
		t.Fatal("adminScreens[slotSigningRepos] not mounted")
	}
	if screen.editor.exists {
		t.Fatal("editor.exists = true, want false (no stored override)")
	}
	if got, want := len(screen.editor.keys.Keys()), 2; got != want {
		t.Fatalf("editor.keys.Keys() = %#v, want %d keys prefilled from the global policy", screen.editor.keys.Keys(), want)
	}
}

// TestFeatureOverridesScreenSigningOpenDoesNotPrefillWhenOverrideAlreadyExists
// is the prefill requirement's negative case: an override that already
// exists uses its OWN stored keys, never the global policy's, even when
// they differ.
func TestFeatureOverridesScreenSigningOpenDoesNotPrefillWhenOverrideAlreadyExists(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
		signingPolicy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1, trustedKeyListTestPEM2}},
		repositoryOverrides: map[string]ports.RepositoryOverrideDetails{
			signingFeatureName + "/library/alpine": {Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1}},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "o")
	updated = runKey(t, updated, "o")

	screen, ok := updated.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if !ok {
		t.Fatal("adminScreens[slotSigningRepos] not mounted")
	}
	if !screen.editor.exists {
		t.Fatal("editor.exists = false, want true (a stored override exists)")
	}
	if got, want := len(screen.editor.keys.Keys()), 1; got != want {
		t.Fatalf("editor.keys.Keys() = %#v, want exactly the 1 stored override key, never the global policy's 2", screen.editor.keys.Keys())
	}
}

// TestFeatureOverridesScreenSigningDeleteKeyIsUsageInformedAndNeverBlocked
// is design requirement (d): deleting a key always shows a usage-count-
// informed confirm, and the count never blocks the deletion -- confirming
// removes the key regardless of how high the reported count is.
func TestFeatureOverridesScreenSigningDeleteKeyIsUsageInformedAndNeverBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
		repositoryOverrides: map[string]ports.RepositoryOverrideDetails{
			signingFeatureName + "/library/alpine": {Enabled: true, TrustedPublicKeys: []string{trustedKeyListTestPEM1}},
		},
		signingKeyUsageCount:  42,
		signingKeyUsageCapped: true,
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavSigningPolicy)
	updated = runKey(t, updated, "o")
	updated = runKey(t, updated, "o")
	updated = runKey(t, updated, "tab") // Enabled -> Keys field

	deleted := runKey(t, updated, "x")

	if got, want := adminClient.signingKeyUsageCalls, 1; got != want {
		t.Fatalf("signingKeyUsageCalls = %d, want %d (the delete key must fire the usage-count call)", got, want)
	}
	if got, want := adminClient.lastSigningKeyUsageRepo, "library/alpine"; got != want {
		t.Fatalf("lastSigningKeyUsageRepo = %q, want %q", got, want)
	}

	screenBeforeConfirm, _ := deleted.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if !screenBeforeConfirm.editor.keys.confirm.Active() {
		t.Fatal("editor.keys.confirm.Active() = false, want true -- a high, capped count must still open the confirm, never block silently")
	}
	if len(screenBeforeConfirm.editor.keys.Keys()) != 1 {
		t.Fatal("key removed before confirming -- deletion must wait for Enter")
	}

	confirmed := runKey(t, deleted, "enter")
	screenAfterConfirm, _ := confirmed.adminScreens[slotSigningRepos].(featureOverridesScreen)
	if screenAfterConfirm.editor.keys.confirm.Active() {
		t.Fatal("confirm still active after Enter, want closed")
	}
	if len(screenAfterConfirm.editor.keys.Keys()) != 0 {
		t.Fatalf("editor.keys.Keys() = %#v, want empty -- confirming must remove the key even though the count (42, capped) was high", screenAfterConfirm.editor.keys.Keys())
	}
}

func TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	t.Run("loads scan runs and drills into selected alert", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
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
		landingCalls := adminClient.listRepositoryScanSummariesCalls // the Overview's own read
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "tab")

		if got := adminClient.listRepositoryScanSummariesCalls - landingCalls; got != 1 {
			t.Fatalf("listRepositoryScanSummariesCalls grew by %d after opening Repository Alerts, want one repository-alert load", got)
		}
		alertsView := updated.View().Content
		for _, want := range []string{"Repository Alerts", "Repository", "Reference", "team/api", "library/base", "library/alpine", "1.0.0", "stable", "latest"} {
			if !strings.Contains(alertsView, want) {
				t.Fatalf("view = %q, want %q", alertsView, want)
			}
		}
		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok {
			t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
		}
		if got, want := repos.table.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
			t.Fatalf("highlighted scan-summary metadata = %#v, want %q", got, want)
		}

		// Enter opens the scan history modal instead of the old inline
		// detail (spec.md "Repository Alert Drill-Down Opens History
		// Modal"), scoped to the highlighted summary row's repository.
		updated = runKey(t, updated, "enter")
		if adminClient.getScanRunDetailCalls != 1 {
			t.Fatalf("getScanRunDetailCalls = %d, want detail fetch on enter", adminClient.getScanRunDetailCalls)
		}
		if !scanHistoryScreenOf(t, updated).modal.Open {
			t.Fatal("scanHistoryScreen.modal.Open = false, want true after enter")
		}
		if got, want := scanHistoryScreenOf(t, updated).modal.Repository, "team/api"; got != want {
			t.Fatalf("scanHistoryScreen.modal.Repository = %q, want %q", got, want)
		}
		detailView := updated.View().Content
		for _, want := range []string{"Run 1 of 1", "CVE-2026-0001", "openssl"} {
			if !strings.Contains(detailView, want) {
				t.Fatalf("view = %q, want %q", detailView, want)
			}
		}

		updated = runKey(t, updated, "esc")
		if updated.adminScreens[slotScanHistory] != nil {
			t.Fatal("adminScreens[slotScanHistory] still mounted, want un-mounted after esc")
		}
		if strings.Contains(updated.View().Content, "Run 1 of 1") {
			t.Fatalf("view = %q, want esc to close the scan history modal with no residual detail", updated.View().Content)
		}
		reposAfterEsc, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok {
			t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
		}
		if got, want := reposAfterEsc.selected, 0; got != want {
			t.Fatalf("trivyReposScreen.selected = %d, want %d", got, want)
		}
	})

	t.Run("empty scan runs stay recoverable and runtime tab remains usable", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage: ports.FeaturePage{
				Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "tab")
		if !strings.Contains(updated.View().Content, "No repository alerts found.") {
			t.Fatalf("view = %q, want clear empty-state message", updated.View().Content)
		}
		updated = runKey(t, updated, "tab")
		if !strings.Contains(ansi.Strip(updated.View().Content), "version 0.57.1") {
			t.Fatalf("view = %q, want the config tab (trivyConfigScreen) still usable after empty alerts", updated.View().Content)
		}
	})
}

// TestModelRepositoryAlertsScreenShowsOneRowPerRepositoryNotPerScanRun is the
// Phase 4 removal-sweep RED/GREEN test: after the call site swap
// (renderTrivyRepositoryAlerts -> renderAdminScanSummary), the
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
	updated = openSidebarItem(t, updated, shellNavImageScanning)
	updated = runKey(t, updated, "tab")

	repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
	}
	summaryTable := repos.table
	if got, want := summaryTable.TotalRows(), 2; got != want {
		t.Fatalf("ScanSummary TotalRows() = %d, want %d (one row per repository, not %d per scan run)", got, want, len(adminClient.scanRuns))
	}

	view := updated.View().Content
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
	updated = openSidebarItem(t, updated, shellNavImageScanning)
	updated = runKey(t, updated, "tab") // switch to trivyReposScreen
	return runKey(t, updated, "enter")  // open the scan history modal
}

// TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard is the Phase 20 task
// 20.1 RED test (T3.3, threat matrix "Subprocess invocation"): the findings
// Enter opener's isHTTPURL scheme guard and explicit-argv exec.Command
// construction (openurl.go) survive the move of this key path into
// scanHistoryScreen (screen_scan_history.go) unchanged -- the exact URL is
// opened for an http(s) PrimaryURL, and a non-http(s) PrimaryURL yields no
// exec at all (adminFindingLink's own defense-in-depth guard, unreachable
// through openURLInBrowser's own duplicate guard either, since openAdminURLCmd
// is never even dispatched).
func TestFindingLinkOpenUsesExplicitArgvAndHTTPSchemeGuard(t *testing.T) {
	original := openURLInBrowser
	defer func() { openURLInBrowser = original }()
	var openedURLs []string
	openURLInBrowser = func(rawURL string) error {
		openedURLs = append(openedURLs, rawURL)
		return nil
	}

	now := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa", Status: ports.ScanRunStatusCompleted}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run: ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:aaa"},
				Findings: []ports.ScanRunFinding{
					{VulnerabilityID: "CVE-HTTP", PrimaryURL: "https://example.com/advisory/http"},
				},
			},
		},
	}

	opened := newScanHistoryModalReadyModel(t, adminClient)
	afterHTTP := runKey(t, opened, "enter")
	if len(openedURLs) != 1 || openedURLs[0] != "https://example.com/advisory/http" {
		t.Fatalf("openedURLs = %v, want exactly [%q] -- the exact URL, unchanged by the move into scanHistoryScreen", openedURLs, "https://example.com/advisory/http")
	}
	_ = afterHTTP

	// A non-http(s) PrimaryURL must yield no exec at all: adminFindingLink's
	// own isHTTPURL guard resolves it to "", so openSelectedFindingLink
	// never returns a Cmd, and openURLInBrowser is never even invoked.
	nonHTTPClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", Digest: "sha256:bbb", Status: ports.ScanRunStatusCompleted}},
		scanRunDetails: map[string]ports.ScanRunDetail{
			"run-1": {
				Run:      ports.ScanRun{ID: "run-1", Repository: "acme/api", Digest: "sha256:bbb"},
				Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-NONHTTP", PrimaryURL: "javascript:alert(1)"}},
			},
		},
	}
	openedURLs = nil
	openedNonHTTP := newScanHistoryModalReadyModel(t, nonHTTPClient)
	runKey(t, openedNonHTTP, "enter")
	if len(openedURLs) != 0 {
		t.Fatalf("openedURLs = %v, want none opened for a non-http(s) PrimaryURL", openedURLs)
	}
}

// TestOverviewUpdateChannelItemReachesUpdateChannelScreen guards the
// tui-update-check feature: Enter on the sidebar's "Update channel" item
// navigates to screenAdminUpdateChannel, the sole admin-facing way to change
// the server-side channel PUT /admin/v1/update-channel writes to.
func TestOverviewUpdateChannelItemReachesUpdateChannelScreen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 26, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession:  AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		updateChannel: "stable",
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")

	updated = openSidebarItem(t, updated, shellNavUpdateChannel)

	if got, want := updated.screen, screenAdminUpdateChannel; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	// Asserting on the raw view text alone is a false-positive trap here:
	// the key-bindings footer itself reads "Space: toggle stable/insider"
	// regardless of whether the load ever actually completed, so a
	// strings.Contains(view, "stable") check would pass even when the
	// screen is stuck on "Loading update channel..." forever (the exact
	// bug this test must catch: Init's loadUpdateChannelCmd result,
	// adminUpdateChannelLoadedMsg, silently dropped by Model.Update if no
	// case routes it into m.adminScreens via routeAdminMsg). Assert on the
	// mounted screen's own loaded state instead.
	screen, ok := updated.adminScreens[slotUpdateChannel].(updateChannelScreen)
	if !ok {
		t.Fatalf("adminScreens[slotUpdateChannel] = %#v, want a mounted updateChannelScreen", updated.adminScreens[slotUpdateChannel])
	}
	if !screen.loaded {
		t.Fatalf("updateChannelScreen.loaded = false, want true -- adminUpdateChannelLoadedMsg never reached the screen")
	}
	if got, want := screen.current, "stable"; got != want {
		t.Fatalf("updateChannelScreen.current = %q, want %q", got, want)
	}
	view := updated.View().Content
	if strings.Contains(view, "Loading update channel...") {
		t.Fatalf("view = %q, want the loading message gone once the channel is loaded", view)
	}
}

// TestTrivyDrillDownStillReachesSecretFindings is the Phase 19 task 19.2 RED
// test (T3.1a, D9): the pre-existing Trivy Repository Alerts Enter drill-down
// to secret findings must still work, unchanged, alongside the new
// Operations entry point.
func TestTrivyDrillDownStillReachesSecretFindings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 9, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		scanRuns:     []ports.ScanRun{{ID: "run-1", Repository: "acme/api", RequestedRef: "latest", Status: ports.ScanRunStatusCompleted}},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = openSidebarItem(t, updated, shellNavImageScanning)
	updated = runKey(t, updated, "tab")   // trivyReposScreen
	updated = runKey(t, updated, "enter") // opens scanHistoryScreen

	scan, ok := updated.adminScreens[slotScanHistory].(scanHistoryScreen)
	if !ok || !scan.modal.Active() {
		t.Fatalf("adminScreens[slotScanHistory] = %#v, want an active scanHistoryScreen", updated.adminScreens[slotScanHistory])
	}
	if got, want := scan.returnTo, screenSecurityTrivyRepos; got != want {
		t.Fatalf("scanHistoryScreen.returnTo = %q, want %q", got, want)
	}
	if got, want := scan.modal.ActiveTab, scanHistoryTabIndexVulnerabilities; got != want {
		t.Fatalf("scanHistoryScreen.modal.ActiveTab = %d, want %d (Vulnerabilities-first from Trivy's drill-down)", got, want)
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
	result.adminScreens[slotUsers] = usersAt(&fakeAdminClient{users: []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}}}, "")
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
			c := newConfirmPrompt("Enable User", "Enable alice?", "enable", "", func(screenEnv) tea.Cmd { return nil })
			openView := confirmOver(model, c)
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
			// Phase 11: TrivyConfigModal moved off AdminViewState onto
			// trivyConfigScreen (screenSecurityTrivy), mounted here exactly
			// like a real navigation would.
			model.screen = screenSecurityTrivy
			model.adminScreens[slotTrivyConfig] = trivyConfigScreen{
				loaded: true,
				cfg: trivyConfigModal{
					Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
					RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
				},
			}
			openView := model.View().Content
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
// its "tab next field, space toggles" help line — proof the modal is never
// silently clipped by the viewport-height guard now that it is a floating
// overlay rather than appended body content.
func TestModelTrivyConfigModalRendersFullBottomBorderAndHelpLineAtViewportFloor(t *testing.T) {
	t.Parallel()

	model := newModelForModalOverlayTest(t, minViewportHeight)
	model.screen = screenSecurityTrivy
	model.adminScreens[slotTrivyConfig] = trivyConfigScreen{
		loaded: true,
		cfg: trivyConfigModal{
			Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
			RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
		},
	}

	view := model.View().Content
	if got := lipgloss.Height(view); got > minViewportHeight {
		t.Fatalf("view height = %d, want <= %d (viewport floor)\n%s", got, minViewportHeight, view)
	}
	if !strings.Contains(view, "╰") {
		t.Fatalf("view = %q, want the Trivy config modal's own closing border rune present at the viewport floor", view)
	}
	if !strings.Contains(ansi.Strip(view), "tab next field, space toggles") {
		t.Fatalf("view = %q, want the modal's help line present at the viewport floor", view)
	}
}

func TestModelTrivyTablesPreserveEmptyStateAndBackendOrdering(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 11, 0, 0, 0, time.UTC)
	t.Run("empty state stays explicit", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "tab")

		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok {
			t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
		}
		if got, want := repos.table.TotalRows(), 0; got != want {
			t.Fatalf("scan-summary table rows = %d, want %d", got, want)
		}
		if !strings.Contains(updated.View().Content, "No repository alerts found.") {
			t.Fatalf("view = %q, want empty alerts message", updated.View().Content)
		}
	})

	t.Run("scan runs keep sorted backend values in the table", func(t *testing.T) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			featurePage:  ports.FeaturePage{Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}},
			scanRuns: []ports.ScanRun{
				{ID: "run-2", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: true},
				{ID: "run-3", Repository: "library/base", RequestedRef: "stable", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: false},
				{ID: "run-1", Repository: "library/alpine", RequestedRef: "latest", Status: ports.ScanRunStatusCompleted, Critical: 0, High: 2, HasFixable: true},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = openSidebarItem(t, updated, shellNavImageScanning)
		updated = runKey(t, updated, "tab")

		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok {
			t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
		}
		if got, want := repos.table.TotalRows(), 3; got != want {
			t.Fatalf("scan-summary table rows = %d, want %d", got, want)
		}
		if got, want := repos.table.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
			t.Fatalf("highlighted scan-summary metadata = %#v, want %q", got, want)
		}
		view := updated.View().Content
		for _, want := range []string{"Repository", "Reference", "Status", "Critical", "High", "Fixable", "team/api", "library/base", "library/alpine", "1.0.0", "stable", "latest"} {
			if !strings.Contains(view, want) {
				t.Fatalf("view = %q, want %q", view, want)
			}
		}
	})
}

func TestAdminTableStatusCellsStayNeutral(t *testing.T) {
	t.Parallel()

	scanSummaryTable := buildAdminScanSummaryTable([]repositorySummary{{Repository: "team/api", LatestRun: ports.ScanRun{ID: "run-1", Repository: "team/api", RequestedRef: "1.0.0", Status: ports.ScanRunStatusCompleted, Critical: 1, High: 0, HasFixable: true}, RunCount: 1}}, 0, defaultViewportHeight)
	if _, styled := scanSummaryTable.HighlightedRow().Data[adminTableColumnScanSummaryStatus].(bubbletable.StyledCell); styled {
		t.Fatal("scan-summary cells must stay neutral")
	}
}

func TestModelAdminFeatureTablesKeepScreenShortcutsAuthoritative(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
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
	updated = openSidebarItem(t, updated, shellNavImageScanning)
	updated = runKey(t, updated, "tab") // trivyReposScreen (Repository Alerts tab's successor)
	updated = runKey(t, updated, "down")

	repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
	}
	if got, want := repos.table.GetHighlightedRowIndex(), 1; got != want {
		t.Fatalf("scan-summary highlighted index = %d, want %d", got, want)
	}
	if got, want := repos.table.HighlightedRow().Data[adminTableMetaScanRunID], "run-2"; got != want {
		t.Fatalf("highlighted scan-summary metadata = %#v, want %q", got, want)
	}

	updated = runKey(t, updated, "enter")
	if adminClient.getScanRunDetailCalls != 1 {
		t.Fatalf("getScanRunDetailCalls = %d, want 1", adminClient.getScanRunDetailCalls)
	}
	if !strings.Contains(updated.View().Content, "CVE-2026-2000") {
		t.Fatalf("view = %q, want loaded detail", updated.View().Content)
	}

	updated = runKey(t, updated, "esc")
	if updated.screen != screenSecurityTrivyRepos {
		t.Fatalf("screen = %q, want %q after closing detail", updated.screen, screenSecurityTrivyRepos)
	}
	updated = runKey(t, updated, "tab") // back to trivyConfigScreen
	updated = runKey(t, updated, "c")
	trivy, ok := updated.adminScreens[slotTrivyConfig].(trivyConfigScreen)
	if !ok || !trivy.cfg.Active() {
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
	updated = openUsersFromOverview(t, updated)

	updated = runKey(t, updated, "t")
	updated = runKey(t, updated, "g")
	if updated.screen != screenAdminGrantAccess {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminGrantAccess)
	}
	updated = runKey(t, updated, "esc")
	if updated.screen != screenAdminEditUserTokens {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminEditUserTokens)
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
	updated = openUsersFromOverview(t, updated)
	updated = runKey(t, updated, "j")

	updated = runKey(t, updated, "/")
	updated = runKey(t, updated, "bo")
	updated = runKey(t, updated, "enter")

	if user, _ := usersScreenOf(t, updated).current(); user.ID != "u-2" {
		t.Fatalf("selected user = %q, want u-2", user.ID)
	}
	view := ansi.Strip(updated.View().Content)
	if !strings.Contains(view, "/ search  bo") {
		t.Fatalf("view = %q, want visible search query", view)
	}
	if strings.Contains(view, "alice") {
		t.Fatalf("view = %q, want alice filtered out", view)
	}

	updated = runKey(t, updated, "/")
	updated = runKey(t, updated, "bb")
	updated = runKey(t, updated, "enter")

	if user, _ := usersScreenOf(t, updated).current(); user.ID != "u-3" {
		t.Fatalf("selected user = %q, want u-3 after narrowing filter", user.ID)
	}
	if strings.Contains(strings.ReplaceAll(ansi.Strip(updated.View().Content), "bobby", ""), "bob ") {
		t.Fatalf("view = %q, want bob filtered out after narrowing", updated.View().Content)
	}
	updated = runKey(t, updated, "enter")
	if updated.screen != screenAdminEditUser {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminEditUser)
	}
	if !strings.Contains(ansi.Strip(updated.View().Content), "bobby") {
		t.Fatalf("view = %q, want selected user context", updated.View().Content)
	}
}

func TestModelTypingLInEditingInputsDoesNotLogout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	baseSession := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)}
	user := ports.AdminUser{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}

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
				mountCreateUser(&model, createUserFieldUsername)
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				return model
			},
			assert: func(t *testing.T, got Model) {
				if createUserOf(got).username != "l" {
					t.Fatalf("username = %q, want %q", createUserOf(got).username, "l")
				}
			},
		},
		{
			name: "change password",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				mountResetPassword(&model)
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				return model
			},
			assert: func(t *testing.T, got Model) {
				if resetPasswordOf(got).password != "l" {
					t.Fatalf("new password = %q, want %q", resetPasswordOf(got).password, "l")
				}
			},
		},
		{
			name: "token name",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				mountTokensForTest(&model)
				return model
			},
			assert: func(t *testing.T, got Model) {
				if tokenNameOf(got) != "l" {
					t.Fatalf("name = %q, want %q", tokenNameOf(got), "l")
				}
			},
		},
		{
			name: "robot name",
			setup: func() Model {
				model := newAdminReadyModel(t, &fakeAdminClient{})
				model.adminAuth = adminAuthStateAuthenticated
				model.adminSession = baseSession
				mountCreateRobotForTest(&model)
				return model
			},
			assert: func(t *testing.T, got Model) {
				if createRobotNameOf(got) != "l" {
					t.Fatalf("name = %q, want %q", createRobotNameOf(got), "l")
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
				searching := usersAt(&fakeAdminClient{users: []ports.AdminUser{user}}, "")
				searching.searching = true
				model.adminScreens[slotUsers] = searching
				return model
			},
			assert: func(t *testing.T, got Model) {
				if s, _ := got.adminScreens[slotUsers].(usersScreen); s.query != "l" {
					t.Fatalf("query = %q, want %q", s.query, "l")
				} else if !s.searching {
					t.Fatal("expected search to stay active")
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

type fakeQueryService struct {
	repositorySummaries []appregixtry.RepositorySummary
	tagDetails          map[string][]appregixtry.TagDetails
	manifests           map[string]appregixtry.ManifestDetails
	uploads             map[string][]appregixtry.UploadDetails
	signatureStatus     map[string]appregixtry.SignatureStatusResult
	// deleteManifestErr backs the Tags screen's delete-tag "d" key/confirm
	// flow (blob-garbage-collection change), mirroring fakeAdminClient's own
	// deleteRobotErr control field.
	deleteManifestErr            error
	lastDeleteManifestRepository string
	lastDeleteManifestReference  string
	// deleteRepositoryErr backs the Repositories screen's delete-repository
	// "d" key/confirm flow (delete-entire-repository feature), mirroring
	// deleteManifestErr's own control-field pattern exactly.
	deleteRepositoryErr            error
	lastDeleteRepositoryRepository string
	// deleteRepositoryErrByRepo backs the delete-entire-project feature's
	// best-effort bulk-delete tests: a per-repository override checked
	// before the uniform deleteRepositoryErr above, so a single fake
	// service can simulate a mixed-authorization scenario (some
	// repositories succeed, others fail) within one project.
	deleteRepositoryErrByRepo map[string]error
	// updateChannel/updateChannelErr back checkForUpdateCmd's channel
	// resolution (tui-update-check feature), mirroring deleteManifestErr's
	// own control-field pattern.
	updateChannel    string
	updateChannelErr error
	// referrers/referrersErr back the manifest inspection screen's Referrers
	// section (console-manifest-enrichment change), keyed exactly like
	// signatureStatus above but on "repository:subjectDigest" since
	// Referrers is always looked up by digest, never by tag reference.
	referrers    map[string]appregixtry.ReferrersIndex
	referrersErr error
	calls        struct {
		catalog          int
		tags             int
		manifest         int
		uploads          int
		signature        int
		deleteManifest   int
		deleteRepository int
		updateChannel    int
		referrers        int
	}
}

type fakeAdminClient struct {
	loginSession AdminSession
	loginErr     error

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

	// updateChannel/updateChannelErr/updateChannelUpdateErr back the
	// Operations domain's Update Channel screen (tui-update-check feature),
	// mirroring scanPolicy's own control-field shape exactly.
	updateChannel            string
	updateChannelErr         error
	updateChannelUpdateErr   error
	getUpdateChannelCalls    int
	updateUpdateChannelCalls int
	lastUpdateChannelInput   string

	signingPolicy            ports.SigningPolicySettings
	signingPolicyErr         error
	signingPolicyUpdateErr   error
	getSigningPolicyCalls    int
	updateSigningPolicyCalls int
	lastSigningPolicyInput   ports.SigningPolicySettings

	signingKeyUsageCount      int
	signingKeyUsageCapped     bool
	signingKeyUsageErr        error
	signingKeyUsageCalls      int
	lastSigningKeyUsageRepo   string
	lastSigningKeyUsageKeyPEM string
	installRuntime            ports.FeatureRuntimeState
	upgradeRuntime            ports.FeatureRuntimeState
	rollbackRuntime           ports.FeatureRuntimeState
	enableFeature             ports.FeatureDetails
	disableFeature            ports.FeatureDetails
	actionResult              ports.FeatureActionResult
	actionResults             map[string]ports.FeatureActionResult
	featureErr                error

	users            []ports.AdminUser
	listUsersResults [][]ports.AdminUser
	usersErr         error

	grants    map[string][]ports.AdminRepoGrant
	grantsErr error

	tokens    map[string][]ports.AdminToken
	tokensErr error

	createUser             ports.AdminUser
	createUserErr          error
	lastCreateUserInput    ports.AdminCreateUserInput
	lastResetPasswordInput ports.AdminResetPasswordInput
	resetPasswordErr       error
	lastConfiguredFeature  string
	lastConfigureInput     ports.FeatureConfigureInput

	putGrantErr    error
	lastGrantInput ports.AdminPutRepoGrantInput

	repoGrants         map[string][]ports.AdminRepositoryGrant
	listRepoGrantsErr  error
	putRepoGrantErr    error
	lastRepoGrantInput ports.AdminPutRepositoryGrantInput

	deleteRepoGrantErr          error
	lastDeleteRepoGrantRepo     string
	lastDeleteRepoGrantUsername string

	createToken    ports.AdminCreatedToken
	createTokenErr error

	revokeTokenErr        error
	lastRevokeTokenUserID string
	lastRevokeTokenID     string

	enableUser ports.AdminUser
	enableErr  error

	expiringTokens     []ports.AdminExpiringToken
	expiringErr        error
	listExpiringCalls  int
	lastExpiringWithin time.Duration
	lastExpiringLimit  int

	accessEvents    []ports.AdminAccessEvent
	accessEventsErr error
	listEventsCalls int
	lastEventsLimit int

	// allGrants backs ListAllRepositoryGrants, served in pages with the
	// opaque cursor "c<offset>"; allGrantsErrs fails the read whose cursor is
	// the key ("" is the first page); allGrantsCursors records every cursor
	// asked for.
	allGrants        []ports.AdminGrantEntry
	allGrantsErrs    map[string]error
	allGrantsCursors []string

	disableUser ports.AdminUser
	disableErr  error

	robots               []ports.AdminRobot
	listRobotsErr        error
	listRobotsCalls      int
	createRobot          ports.AdminCreatedRobot
	createRobotErr       error
	createRobotCalls     int
	lastCreateRobotInput ports.AdminCreateRobotInput

	// deleteRobot* backs the registry-acl-v1 robot-deletion "x" key/confirm
	// flow on screenAdminRobots.
	deleteRobotErr        error
	deleteRobotCalls      int
	lastDeleteRobotUserID string

	loginCalls            int
	getFeatureCalls       int
	getFeatureStatusCalls int
	getFeaturePageCalls   int
	listScanRunsCalls     int
	// repositoryScanSummaries, when non-nil, is returned verbatim by
	// ListRepositoryScanSummaries (limit-truncated) -- lets a test simulate
	// exactly what the crowd-out-fixed backend would return. When nil, the
	// default derives summaries from scanRuns via the same
	// severity/fixability ordering and per-repository grouping the TUI
	// already used before this fix, so every pre-existing test that only
	// seeds scanRuns keeps passing unchanged.
	repositoryScanSummaries          []ports.RepositoryScanSummary
	listRepositoryScanSummariesCalls int
	getScanRunDetailCalls            int
	getSecretScanFindingsCalls       int
	lastSecretScanFindingsQuery      string
	executeFeatureActionCalls        int
	lastFeatureAction                string
	installRuntimeCalls              int
	upgradeRuntimeCalls              int
	rollbackRuntimeCalls             int
	enableFeatureCalls               int
	disableFeatureCalls              int
	listUsersCalls                   int
	listGrantsCalls                  int
	listTokensCalls                  int
	createUserCalls                  int
	resetPasswordCalls               int
	configureFeatureCalls            int
	putGrantCalls                    int
	listRepoGrantsCalls              int
	putRepoGrantCalls                int
	deleteRepoGrantCalls             int
	createTokenCalls                 int
	revokeTokenCalls                 int
	enableCalls                      int
	disableCalls                     int

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

// fakeScanRunSeverityRank/fakeScanRunSortsBefore mirror the severity-first,
// fixable-then-created_at-tiebroken ordering ListLatestScanRunPerRepository
// now applies server-side (store.go's ListLatestScanRunPerRepository SQL) --
// used only so fakeAdminClient's default ListRepositoryScanSummaries
// derivation (below) matches what the real backend would return for tests
// that seed raw scanRuns instead of an explicit repositoryScanSummaries
// override.
func fakeScanRunSeverityRank(run ports.ScanRun) int {
	switch {
	case run.Critical > 0:
		return 4
	case run.High > 0:
		return 3
	case run.Medium > 0:
		return 2
	case run.Low > 0:
		return 1
	default:
		return 0
	}
}

func fakeScanRunSortsBefore(left, right ports.ScanRun) bool {
	leftRank, rightRank := fakeScanRunSeverityRank(left), fakeScanRunSeverityRank(right)
	if leftRank != rightRank {
		return leftRank > rightRank
	}
	if left.HasFixable != right.HasFixable {
		return left.HasFixable
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.After(right.CreatedAt)
	}
	return left.ID < right.ID
}

func (f *fakeAdminClient) ListRepositoryScanSummaries(_ context.Context, _ AdminSession, limit int) ([]ports.RepositoryScanSummary, error) {
	f.listRepositoryScanSummariesCalls++
	if f.featureErr != nil {
		return nil, f.featureErr
	}
	if f.repositoryScanSummaries != nil {
		if limit > 0 && limit < len(f.repositoryScanSummaries) {
			return append([]ports.RepositoryScanSummary(nil), f.repositoryScanSummaries[:limit]...), nil
		}
		return append([]ports.RepositoryScanSummary(nil), f.repositoryScanSummaries...), nil
	}
	runs := append([]ports.ScanRun(nil), f.scanRuns...)
	sort.SliceStable(runs, func(i, j int) bool { return fakeScanRunSortsBefore(runs[i], runs[j]) })
	grouped := summarizeScanRunsByRepository(runs)
	summaries := make([]ports.RepositoryScanSummary, 0, len(grouped))
	for _, summary := range grouped {
		if limit > 0 && len(summaries) >= limit {
			break
		}
		summaries = append(summaries, ports.RepositoryScanSummary{Run: summary.LatestRun, RunCount: summary.RunCount})
	}
	return summaries, nil
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

func (f *fakeAdminClient) GetUpdateChannel(context.Context, AdminSession) (string, error) {
	f.getUpdateChannelCalls++
	if f.updateChannelErr != nil {
		return "", f.updateChannelErr
	}
	return f.updateChannel, nil
}

func (f *fakeAdminClient) SetUpdateChannel(_ context.Context, _ AdminSession, channel string) (string, error) {
	f.updateUpdateChannelCalls++
	f.lastUpdateChannelInput = channel
	if f.updateChannelUpdateErr != nil {
		return "", f.updateChannelUpdateErr
	}
	f.updateChannel = channel
	return f.updateChannel, nil
}

func (f *fakeAdminClient) GetSigningPolicy(context.Context, AdminSession) (ports.SigningPolicySettings, error) {
	f.getSigningPolicyCalls++
	if f.signingPolicyErr != nil {
		return ports.SigningPolicySettings{}, f.signingPolicyErr
	}
	return f.signingPolicy, nil
}

func (f *fakeAdminClient) UpdateSigningPolicy(_ context.Context, _ AdminSession, input ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	f.updateSigningPolicyCalls++
	f.lastSigningPolicyInput = input
	if f.signingPolicyUpdateErr != nil {
		return ports.SigningPolicySettings{}, f.signingPolicyUpdateErr
	}
	f.signingPolicy = input
	return f.signingPolicy, nil
}

func (f *fakeAdminClient) EnableFeature(context.Context, AdminSession, string) (ports.FeatureDetails, error) {
	f.enableFeatureCalls++
	if f.featureErr != nil {
		return ports.FeatureDetails{}, f.featureErr
	}
	if f.enableFeature.Name != "" {
		f.feature = f.enableFeature
		f.featureStatus = f.enableFeature
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

func (f *fakeAdminClient) CreateUser(_ context.Context, _ AdminSession, input ports.AdminCreateUserInput) (ports.AdminUser, error) {
	f.createUserCalls++
	f.lastCreateUserInput = input
	if f.createUserErr != nil {
		return ports.AdminUser{}, f.createUserErr
	}
	return f.createUser, nil
}

func (f *fakeAdminClient) ResetPassword(_ context.Context, _ AdminSession, input ports.AdminResetPasswordInput) error {
	f.resetPasswordCalls++
	f.lastResetPasswordInput = input
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

func (f *fakeAdminClient) DeleteRobot(_ context.Context, _ AdminSession, userID string) error {
	f.deleteRobotCalls++
	f.lastDeleteRobotUserID = userID
	if f.deleteRobotErr != nil {
		return f.deleteRobotErr
	}
	remaining := f.robots[:0:0]
	for _, robot := range f.robots {
		if robot.ID != userID {
			remaining = append(remaining, robot)
		}
	}
	f.robots = remaining
	return nil
}

func (f *fakeAdminClient) ListRepositoryGrants(_ context.Context, _ AdminSession, repository string) ([]ports.AdminRepositoryGrant, error) {
	f.listRepoGrantsCalls++
	if f.listRepoGrantsErr != nil {
		return nil, f.listRepoGrantsErr
	}
	return append([]ports.AdminRepositoryGrant(nil), f.repoGrants[repository]...), nil
}

func (f *fakeAdminClient) PutRepositoryGrant(_ context.Context, _ AdminSession, input ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	f.putRepoGrantCalls++
	f.lastRepoGrantInput = input
	if f.putRepoGrantErr != nil {
		return ports.AdminRepositoryGrant{}, f.putRepoGrantErr
	}
	return ports.AdminRepositoryGrant{Username: input.Username, Role: input.Role}, nil
}

func (f *fakeAdminClient) DeleteRepositoryGrant(_ context.Context, _ AdminSession, repository string, username string) error {
	f.deleteRepoGrantCalls++
	f.lastDeleteRepoGrantRepo = repository
	f.lastDeleteRepoGrantUsername = username
	return f.deleteRepoGrantErr
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

func (f *fakeAdminClient) CountSigningKeyUsage(_ context.Context, _ AdminSession, repository string, keyPEM string) (int, bool, error) {
	f.signingKeyUsageCalls++
	f.lastSigningKeyUsageRepo = repository
	f.lastSigningKeyUsageKeyPEM = keyPEM
	if f.signingKeyUsageErr != nil {
		return 0, false, f.signingKeyUsageErr
	}
	return f.signingKeyUsageCount, f.signingKeyUsageCapped, nil
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

func (f *fakeAdminClient) ListAccessEvents(_ context.Context, _ AdminSession, limit int) ([]ports.AdminAccessEvent, error) {
	f.listEventsCalls++
	f.lastEventsLimit = limit
	if f.accessEventsErr != nil {
		return nil, f.accessEventsErr
	}
	return append([]ports.AdminAccessEvent(nil), f.accessEvents...), nil
}

func (f *fakeAdminClient) ListExpiringTokens(_ context.Context, _ AdminSession, within time.Duration, limit int) ([]ports.AdminExpiringToken, error) {
	f.listExpiringCalls++
	f.lastExpiringWithin, f.lastExpiringLimit = within, limit
	if f.expiringErr != nil {
		return nil, f.expiringErr
	}
	return append([]ports.AdminExpiringToken(nil), f.expiringTokens...), nil
}

func (f *fakeAdminClient) ListAllRepositoryGrants(_ context.Context, _ AdminSession, limit int, after string) (ports.AdminGrantPage, error) {
	f.allGrantsCursors = append(f.allGrantsCursors, after)
	if err := f.allGrantsErrs[after]; err != nil {
		return ports.AdminGrantPage{}, err
	}
	offset := 0
	if after != "" {
		if _, err := fmt.Sscanf(after, "c%d", &offset); err != nil {
			return ports.AdminGrantPage{}, fmt.Errorf("invalid cursor %q", after)
		}
	}
	if limit <= 0 {
		limit = 100
	}
	end := min(offset+limit, len(f.allGrants))
	page := ports.AdminGrantPage{Grants: append([]ports.AdminGrantEntry(nil), f.allGrants[min(offset, end):end]...)}
	if end < len(f.allGrants) {
		page.Next = fmt.Sprintf("c%d", end)
	}
	return page, nil
}

func (f *fakeAdminClient) ListRobots(context.Context, AdminSession) ([]ports.AdminRobot, error) {
	f.listRobotsCalls++
	if f.listRobotsErr != nil {
		return nil, f.listRobotsErr
	}
	return append([]ports.AdminRobot(nil), f.robots...), nil
}

func (f *fakeAdminClient) CreateRobot(_ context.Context, _ AdminSession, input ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	f.createRobotCalls++
	f.lastCreateRobotInput = input
	if f.createRobotErr != nil {
		return ports.AdminCreatedRobot{}, f.createRobotErr
	}
	return f.createRobot, nil
}

func (f *fakeQueryService) RepositorySummaries(context.Context, int, string) ([]appregixtry.RepositorySummary, error) {
	f.calls.catalog++
	return append([]appregixtry.RepositorySummary(nil), f.repositorySummaries...), nil
}

func (f *fakeQueryService) TagDetails(_ context.Context, repository string, _ int, _ string) ([]appregixtry.TagDetails, error) {
	f.calls.tags++
	if result, ok := f.tagDetails[repository]; ok {
		return append([]appregixtry.TagDetails(nil), result...), nil
	}
	return nil, nil
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

func (f *fakeQueryService) SignatureStatus(_ context.Context, repository string, reference string) (appregixtry.SignatureStatusResult, error) {
	f.calls.signature++
	if result, ok := f.signatureStatus[fmt.Sprintf("%s:%s", repository, reference)]; ok {
		return result, nil
	}
	return appregixtry.SignatureStatusResult{State: appregixtry.SignatureStatusUnsigned}, nil
}

func (f *fakeQueryService) DeleteManifest(_ context.Context, repository string, reference string) (appregixtry.DeletionDetails, error) {
	f.calls.deleteManifest++
	f.lastDeleteManifestRepository = repository
	f.lastDeleteManifestReference = reference
	if f.deleteManifestErr != nil {
		return appregixtry.DeletionDetails{}, f.deleteManifestErr
	}
	return appregixtry.DeletionDetails{Repository: repository, Reference: reference, TagsRemoved: []string{reference}}, nil
}

func (f *fakeQueryService) DeleteRepository(_ context.Context, repository string) (appregixtry.RepositoryDeletionDetails, error) {
	f.calls.deleteRepository++
	f.lastDeleteRepositoryRepository = repository
	if err, ok := f.deleteRepositoryErrByRepo[repository]; ok {
		return appregixtry.RepositoryDeletionDetails{}, err
	}
	if f.deleteRepositoryErr != nil {
		return appregixtry.RepositoryDeletionDetails{}, f.deleteRepositoryErr
	}
	return appregixtry.RepositoryDeletionDetails{Repository: repository}, nil
}

func (f *fakeQueryService) GetUpdateChannel(context.Context) (string, error) {
	f.calls.updateChannel++
	if f.updateChannelErr != nil {
		return "", f.updateChannelErr
	}
	return f.updateChannel, nil
}

func (f *fakeQueryService) Referrers(_ context.Context, repository string, subjectDigest string, _ string) (appregixtry.ReferrersIndex, error) {
	f.calls.referrers++
	if f.referrersErr != nil {
		return appregixtry.ReferrersIndex{}, f.referrersErr
	}
	if result, ok := f.referrers[fmt.Sprintf("%s:%s", repository, subjectDigest)]; ok {
		return result, nil
	}
	return appregixtry.ReferrersIndex{}, nil
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

// scanHistoryScreenOf extracts the mounted scanHistoryScreen (Phase 19,
// design.md's State Migration table: ScanHistoryModal migrated off
// AdminViewState onto its own dedicated slot, screen.go's slotScanHistory).
// Fails the test if it is not mounted -- callers that need to assert it is
// CLOSED must check m.adminScreens[slotScanHistory] == nil directly instead
// (un-mounted, not a zero-value modal).
func scanHistoryScreenOf(t *testing.T, m Model) scanHistoryScreen {
	t.Helper()
	s, ok := m.adminScreens[slotScanHistory].(scanHistoryScreen)
	if !ok {
		t.Fatalf("adminScreens[slotScanHistory] = %#v, want a mounted scanHistoryScreen", m.adminScreens[slotScanHistory])
	}
	return s
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
	model := NewModel(&fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
	return runCmd(t, model, model.Init())
}

func newAdminReadyModelWithCatalog(t *testing.T, repositories []string, adminClient AdminClient) Model {
	t.Helper()
	summaries := make([]appregixtry.RepositorySummary, 0, len(repositories))
	for _, repository := range repositories {
		summaries = append(summaries, appregixtry.RepositorySummary{Name: repository})
	}
	model := NewModel(&fakeQueryService{repositorySummaries: summaries}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
	return runCmd(t, model, model.Init())
}

// openSidebarItem opens one sidebar item from the post-login Overview with the
// keys an operator presses: Down to the item's row, then Enter.
func openSidebarItem(t *testing.T, m Model, id shellNavID) Model {
	t.Helper()
	for index, item := range shellNavItems() {
		if item.ID != id {
			continue
		}
		for range index {
			m = runKey(t, m, "down")
		}
		return runKey(t, m, "enter")
	}
	t.Fatalf("no sidebar item %q", id)
	return m
}

// openUsersFromOverview opens the Users list from the post-login Overview.
func openUsersFromOverview(t *testing.T, m Model) Model {
	t.Helper()
	return openSidebarItem(t, m, shellNavUsers)
}

// TestRenderSignatureLinesShowsVerifiedIdentityDistinctlyFromKeyFingerprint
// is the Phase 8 RED test (signing-keyless-verification spec: "Identity
// match reports SAN and issuer separately"): the manifest inspection view's
// Signature section renders VerifiedIdentity on its own line, distinct from
// VerifiedKeyFingerprint's "Signed with:" line, when a signature verified
// via the identity path.
func TestRenderSignatureLinesShowsVerifiedIdentityDistinctlyFromKeyFingerprint(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	signature := appregixtry.SignatureStatusResult{
		State: "verified",
		Signature: &appregixtry.SignatureStatusDetail{
			SignatureCount:   1,
			VerifiedIdentity: "https://github.com/acme/repo/.github/workflows/release.yml@refs/heads/main (https://token.actions.githubusercontent.com)",
		},
	}

	lines := renderSignatureLines(theme, signature)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, signature.Signature.VerifiedIdentity) {
		t.Fatalf("renderSignatureLines() = %q, want the verified identity shown", joined)
	}
	if strings.Contains(joined, "Signed with:") {
		t.Fatalf("renderSignatureLines() = %q, must not render the key-fingerprint line when verified via identity", joined)
	}
}

// TestRenderSignatureLinesShowsKeyFingerprintNotIdentityOnKeyVerifiedPath is
// the companion mutual-exclusion case: the pre-existing key-verified path
// renders exactly as before, with no identity line at all.
func TestRenderSignatureLinesShowsKeyFingerprintNotIdentityOnKeyVerifiedPath(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	signature := appregixtry.SignatureStatusResult{
		State: "verified",
		Signature: &appregixtry.SignatureStatusDetail{
			SignatureCount:         1,
			VerifiedKeyFingerprint: "aa:bb:cc:dd",
		},
	}

	lines := renderSignatureLines(theme, signature)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "aa:bb:cc:dd") {
		t.Fatalf("renderSignatureLines() = %q, want the verified key fingerprint shown", joined)
	}
	if strings.Contains(joined, "Verified identity:") {
		t.Fatalf("renderSignatureLines() = %q, must not render an identity line on the key-verified path", joined)
	}
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
	msg := testKeyRunes(key)
	switch key {
	case "enter":
		msg = testKeyEnter()
	case "esc":
		msg = testKeyEsc()
	case "tab":
		msg = testKeyTab()
	case "shift+tab":
		msg = testKeyShiftTab()
	case "left":
		msg = testKeyLeft()
	case "right":
		msg = testKeyRight()
	case "down":
		msg = testKeyDown()
	case "up":
		msg = testKeyUp()
	case "backspace":
		msg = testKeyBackspace()
	case "pgup":
		msg = testKeyPgUp()
	case "pgdown":
		msg = testKeyPgDown()
	case "home":
		msg = testKeyHome()
	case "end":
		msg = testKeyEnd()
	case " ":
		msg = testKeySpace()
	}
	updated, cmd := model.Update(msg)
	result := updated.(Model)
	if cmd != nil {
		return runCmd(t, result, cmd)
	}
	return result
}

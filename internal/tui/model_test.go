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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

	service := &fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}
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

	service := &fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}
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
	features := make([]ports.FeatureSummary, 40)
	for i := range features {
		features[i] = ports.FeatureSummary{Name: fmt.Sprintf("feature-%d", i)}
	}
	// Phase 11: Features/Tables.Features moved off AdminViewState onto
	// securityMenuScreen (design.md's State Migration table) -- mounted
	// here exactly like production (openAdminFeatures).
	menu := securityMenuScreen{features: features, loaded: true}
	menu.rebuildTable(result.screenEnv())
	result.adminScreens[slotSecurityMenu] = menu
	result.rebuildAdminTables(result.adminTablesLayout())
	beforePageSize := result.adminView.Layout.Primary

	grown, _ := result.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: minViewportHeight + 20})
	afterModel := grown.(Model)

	if got := afterModel.adminView.Layout.Primary; got <= beforePageSize {
		t.Fatalf("primary pageSize after taller resize = %d, want > %d (before resize) — resize must recompute the table budget without restart", got, beforePageSize)
	}
	afterMenu, ok := afterModel.adminScreens[slotSecurityMenu].(securityMenuScreen)
	if !ok {
		t.Fatal("adminScreens[slotSecurityMenu] not a securityMenuScreen after resize")
	}
	wantHeight := afterModel.adminView.Layout.Primary + tableChromeRows
	if got := lipgloss.Height(afterMenu.table.View()); got != wantHeight {
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
			view := result.View()
			if got := lipgloss.Height(view); got > height {
				t.Fatalf("repositories view height = %d, want <= %d\nview:\n%s", got, height, view)
			}

			result.screen = screenTags
			result.tags = TagsModel{Repository: "library/alpine", Items: tagItems}
			result.rebuildTagsTable(result.tagsTableLayout())
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
	if !strings.Contains(updated.View(), summaries[pageSize].Name) {
		t.Fatalf("view after paging = %q, want the newly highlighted repository %q genuinely visible on-screen", updated.View(), summaries[pageSize].Name)
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
	if !strings.Contains(updated.View(), tags[pageSize].Name) {
		t.Fatalf("view after paging = %q, want the newly highlighted tag %q genuinely visible on-screen", updated.View(), tags[pageSize].Name)
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

	enterUpdated, deleteCmd := pending.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	enterUpdated, deleteCmd := pending.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	if !strings.Contains(manifestDeleted.View(), "Delete unavailable in v1") {
		t.Fatalf("view = %q, want the v1 placeholder message", manifestDeleted.View())
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

	view := pending.View()
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

// TestModelAdminIntentRoutesPostLoginToRepoAdminGrantsWhenSet is the Phase 3
// task 3.1 RED test (design.md Decision 7): a one-shot adminIntent field set
// before login routes a successful auth to screenRepoAdminGrants instead of
// the default screenAdminUsers, and the intent is consumed (reset) so a
// later plain login does not stick to the repo-grants destination.
func TestModelAdminIntentRoutesPostLoginToRepoAdminGrantsWhenSet(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "delegate", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)},
	}
	model := newAdminReadyModel(t, adminClient)
	model.adminIntent = adminIntentRepoGrants
	model.adminIntentRepository = "team/app"

	updated := runAdminLogin(t, model, "delegate", "secret-pass")

	if got, want := updated.screen, screenRepoAdminGrants; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := updated.adminIntent, adminIntentOperator; got != want {
		t.Fatalf("adminIntent = %q, want %q (must be consumed exactly once)", got, want)
	}
	if got, want := updated.adminView.RepoAdminRepository, "team/app"; got != want {
		t.Fatalf("RepoAdminRepository = %q, want %q", got, want)
	}
}

// TestModelAdminIntentRoutesPostLoginToUsersWhenNotSet triangulates the
// default (zero-value) adminIntent path: an ordinary operator login without
// any repo-grants intent keeps routing to screenAdminUsers, unchanged.
func TestModelAdminIntentRoutesPostLoginToUsersWhenNotSet(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)},
		users:        []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)

	updated := runAdminLogin(t, model, "operator", "secret-pass")

	if got, want := updated.screen, screenAdminUsers; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := updated.adminIntent, adminIntentOperator; got != want {
		t.Fatalf("adminIntent = %q, want %q", got, want)
	}
}

// TestRepoAdminGrantsScreensJoinAdminScreenSets is the Phase 3 task 3.3 RED
// test (design.md Decision 7): screenRepoAdminGrants/screenRepoAdminAddGrant
// must route through updateAdminKey (isAdminScreen) so the shipped
// session-expiry/logout plumbing covers them. Only screenRepoAdminGrants — a
// read/list screen, like its screenAdminEditUserGrants precedent — joins
// isAdminPrincipalScreen and canLogoutAdminFromCurrentScreen;
// screenRepoAdminAddGrant is a free-text username form, like its
// screenAdminAddGrant precedent, so it is deliberately excluded from both
// (isAdminPrincipalScreen gates the bare 'q' quit key — including it would
// make typing "q" as part of a username quit the whole program).
func TestRepoAdminGrantsScreensJoinAdminScreenSets(t *testing.T) {
	t.Parallel()

	if !isAdminScreen(screenRepoAdminGrants) {
		t.Fatalf("isAdminScreen(screenRepoAdminGrants) = false, want true")
	}
	if !isAdminScreen(screenRepoAdminAddGrant) {
		t.Fatalf("isAdminScreen(screenRepoAdminAddGrant) = false, want true")
	}
	if !isAdminPrincipalScreen(screenRepoAdminGrants) {
		t.Fatalf("isAdminPrincipalScreen(screenRepoAdminGrants) = false, want true")
	}
	if isAdminPrincipalScreen(screenRepoAdminAddGrant) {
		t.Fatalf("isAdminPrincipalScreen(screenRepoAdminAddGrant) = true, want false (free-text username form)")
	}
}

func TestCanLogoutFromRepoAdminGrantsScreen(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenRepoAdminGrants

	if !model.canLogoutAdminFromCurrentScreen() {
		t.Fatalf("canLogoutAdminFromCurrentScreen() = false, want true on screenRepoAdminGrants")
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

// TestModelRepoAdminGrantsLoadPutAndDeleteCommandsWired is the Phase 3
// task 3.9 RED test's second half: from the Console Repositories screen's
// grant action through login, grants load automatically, the add-grant form
// submits a PutRepositoryGrant with the default (never repo-admin) role, and
// the remove-grant confirmation issues a DeleteRepositoryGrant.
func TestModelRepoAdminGrantsLoadPutAndDeleteCommandsWired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 12, 10, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "delegate", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		repoGrants: map[string][]ports.AdminRepositoryGrant{
			"team/app": {{Username: "bob", Role: domainauth.RepoRoleWriter}},
		},
	}
	model := newAdminReadyModelWithCatalog(t, []string{"team/app"}, adminClient)
	model.now = func() time.Time { return now }

	updated := runKey(t, model, "g")
	updated = runKey(t, updated, "delegate")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "secret-pass")
	updated = runKey(t, updated, "enter")

	if got, want := updated.screen, screenRepoAdminGrants; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := adminClient.listRepoGrantsCalls, 1; got != want {
		t.Fatalf("listRepoGrantsCalls = %d, want %d", got, want)
	}
	if !strings.Contains(updated.View(), "bob") || !strings.Contains(updated.View(), string(domainauth.RepoRoleWriter)) {
		t.Fatalf("view = %q, want bob's grant listed", updated.View())
	}

	updated = runKey(t, updated, "n")
	updated = runKey(t, updated, "carol")
	updated = runKey(t, updated, "enter")

	if got, want := adminClient.putRepoGrantCalls, 1; got != want {
		t.Fatalf("putRepoGrantCalls = %d, want %d", got, want)
	}
	if got, want := adminClient.lastRepoGrantInput.Repository, "team/app"; got != want {
		t.Fatalf("input.Repository = %q, want %q", got, want)
	}
	if got, want := adminClient.lastRepoGrantInput.Username, "carol"; got != want {
		t.Fatalf("input.Username = %q, want %q", got, want)
	}
	if got, want := adminClient.lastRepoGrantInput.Role, domainauth.RepoRoleReader; got != want {
		t.Fatalf("input.Role = %q, want %q (default, never repo-admin)", got, want)
	}

	updated = runKey(t, updated, "x")
	if !strings.Contains(updated.View(), `Remove grant for "bob" from "team/app"?`) {
		t.Fatalf("view = %q, want grant removal confirmation", updated.View())
	}
	updated = runKey(t, updated, "enter")

	if got, want := adminClient.deleteRepoGrantCalls, 1; got != want {
		t.Fatalf("deleteRepoGrantCalls = %d, want %d", got, want)
	}
	if got, want := adminClient.lastDeleteRepoGrantRepo, "team/app"; got != want {
		t.Fatalf("delete repository = %q, want %q", got, want)
	}
	if got, want := adminClient.lastDeleteRepoGrantUsername, "bob"; got != want {
		t.Fatalf("delete username = %q, want %q", got, want)
	}
}

// TestUpdateRepoAdminGrantsKeyEditRefusesRepoAdminGrant is a CRITICAL-finding
// remediation RED test (sdd-verify, PR 3): ListRepositoryGrants returns every
// grant on a repository unfiltered by role (service.go:570-580), so a
// delegate's own grants list can legitimately contain a peer's repo-admin
// grant. Pressing "e" on that row must NOT populate screenRepoAdminAddGrant's
// Role field with domainauth.RepoRoleAdmin -- renderRepoAdminAddGrantScreen
// would then render the literal string "repo-admin", contradicting task
// 3.5's guarantee that the role field never offers repo-admin. This exercises
// the real "e" key handler (updateRepoAdminGrantsKey) through model.Update(),
// not just nextDelegateGrantRole in isolation (that function is already
// proven correct and is not where this bug lives).
func TestUpdateRepoAdminGrantsKeyEditRefusesRepoAdminGrant(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenRepoAdminGrants
	model.adminView.RepoAdminRepository = "team/app"
	model.adminView.RepoAdminGrants = []ports.AdminRepositoryGrant{
		{Username: "alice", Role: domainauth.RepoRoleAdmin},
	}
	model.adminView.SelectedRepoAdminGrant = 0

	updated := runKey(t, model, "e")

	if updated.screen == screenRepoAdminAddGrant {
		t.Fatalf("screen = %q after editing a repo-admin grant, want to stay on %q (edit refused)", updated.screen, screenRepoAdminGrants)
	}
	if updated.adminView.RepoAdminGrantForm.Role == domainauth.RepoRoleAdmin {
		t.Fatalf("RepoAdminGrantForm.Role = %q, want never repo-admin", updated.adminView.RepoAdminGrantForm.Role)
	}
	if updated.status == "" {
		t.Fatalf("status = empty, want a message explaining the edit was refused")
	}
}

// TestUpdateRepoAdminGrantsKeyEditAllowsNonRepoAdminGrant is the
// triangulation companion to TestUpdateRepoAdminGrantsKeyEditRefusesRepoAdminGrant:
// the refusal guard must be specific to domainauth.RepoRoleAdmin, not an
// over-broad block that disables editing altogether.
func TestUpdateRepoAdminGrantsKeyEditAllowsNonRepoAdminGrant(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenRepoAdminGrants
	model.adminView.RepoAdminRepository = "team/app"
	model.adminView.RepoAdminGrants = []ports.AdminRepositoryGrant{
		{Username: "bob", Role: domainauth.RepoRoleWriter},
	}
	model.adminView.SelectedRepoAdminGrant = 0

	updated := runKey(t, model, "e")

	if got, want := updated.screen, screenRepoAdminAddGrant; got != want {
		t.Fatalf("screen = %q, want %q (editing a non-repo-admin grant must proceed)", got, want)
	}
	if got, want := updated.adminView.RepoAdminGrantForm.Role, domainauth.RepoRoleWriter; got != want {
		t.Fatalf("RepoAdminGrantForm.Role = %q, want %q", got, want)
	}
	if got, want := updated.adminView.RepoAdminGrantForm.Username, "bob"; got != want {
		t.Fatalf("RepoAdminGrantForm.Username = %q, want %q", got, want)
	}
}

// TestUpdateRepoAdminGrantsKeyAddRefusesAfterUnauthorizedLoad is a manual-RC
// remediation RED test (PR 3): the backend correctly rejects
// GET .../grants with 403 when the caller lacks repo-admin on that
// repository (admin_handlers.go), which surfaces here as
// adminRepoGrantsLoadedMsg.err != nil. Pressing "n" in that state must
// refuse immediately with a clear status message instead of navigating to
// screenRepoAdminAddGrant -- letting an unauthorized user fill out an
// entire admin mutation form before the backend's eventual PUT rejection
// is poor UX, even though no privilege escalation occurs. This exercises
// the real load-failure branch of Update() and the real "n" key handler
// (updateRepoAdminGrantsKey) through model.Update(), not either in
// isolation.
func TestUpdateRepoAdminGrantsKeyAddRefusesAfterUnauthorizedLoad(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenRepoAdminGrants
	model.adminView.RepoAdminRepository = "team/app"

	loaded, _ := model.Update(adminRepoGrantsLoadedMsg{
		repository: "team/app",
		err:        errors.New("repository administrator privileges are required"),
	})
	model = loaded.(Model)

	updated := runKey(t, model, "n")

	if got, want := updated.screen, screenRepoAdminGrants; got != want {
		t.Fatalf("screen = %q after \"n\" following an unauthorized grants load, want to stay on %q (add refused)", got, want)
	}
	if updated.status == "" {
		t.Fatalf("status = empty, want a message explaining the add was refused")
	}
}

// TestUpdateRepoAdminGrantsKeyAddAllowsAfterAuthorizedLoad is the
// triangulation companion to
// TestUpdateRepoAdminGrantsKeyAddRefusesAfterUnauthorizedLoad: the refusal
// guard must be specific to a load that actually failed, not an over-broad
// block that disables "n" (Add Grant) altogether after any load.
func TestUpdateRepoAdminGrantsKeyAddAllowsAfterAuthorizedLoad(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenRepoAdminGrants
	model.adminView.RepoAdminRepository = "team/app"

	loaded, _ := model.Update(adminRepoGrantsLoadedMsg{
		repository: "team/app",
		grants:     []ports.AdminRepositoryGrant{{Username: "bob", Role: domainauth.RepoRoleWriter}},
	})
	model = loaded.(Model)

	updated := runKey(t, model, "n")

	if got, want := updated.screen, screenRepoAdminAddGrant; got != want {
		t.Fatalf("screen = %q after \"n\" following a successful grants load, want %q (add must still work)", got, want)
	}
}

// TestOpenRepoAdminGrantsResetsAuthorizedFlagBeforeNewLoad guards against
// the same class of "second write path" bug that caused the PR3 CRITICAL
// finding earlier in this change: a stale "authorized" state from a
// PREVIOUS repository's successful grants load must not leak into a NEW
// repository's screen before its own load response arrives. Without a
// reset, pressing "n" immediately after openRepoAdminGrants (already
// authenticated, before the fresh adminRepoGrantsLoadedMsg lands) would
// wrongly be allowed.
func TestOpenRepoAdminGrantsResetsAuthorizedFlagBeforeNewLoad(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 12, 10, 0, 0, time.UTC)
	model := newAdminReadyModelWithCatalog(t, []string{"team/app", "team/other"}, &fakeAdminClient{})
	model.now = func() time.Time { return now }
	model.adminAuth = adminAuthStateAuthenticated
	model.adminSession = AdminSession{Username: "delegate", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)}
	model.screen = screenRepoAdminGrants
	model.adminView.RepoAdminRepository = "team/app"

	loaded, _ := model.Update(adminRepoGrantsLoadedMsg{
		repository: "team/app",
		grants:     []ports.AdminRepositoryGrant{{Username: "bob", Role: domainauth.RepoRoleWriter}},
	})
	model = loaded.(Model)

	updated, _ := model.openRepoAdminGrants()
	fresh := updated.(Model)

	beforeResponse := runKey(t, fresh, "n")

	if got, want := beforeResponse.screen, screenRepoAdminGrants; got != want {
		t.Fatalf("screen = %q after \"n\" before the new repository's load response arrived, want to stay on %q (stale authorization must not leak)", got, want)
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

// TestAdminRobotScreensJoinAdminScreenSets is task 5.7's RED test (design.md
// Decision 7): screenAdminRobots/screenAdminCreateRobot must route through
// updateKey (isAdminScreen) so the shipped session-expiry/logout plumbing
// covers them. Only screenAdminRobots -- a list screen, like its
// screenAdminUsers precedent -- joins isAdminPrincipalScreen (gates the bare
// 'q' quit key); screenAdminCreateRobot is a free-text Name/Repository form,
// like screenAdminCreateUser, so it is deliberately excluded (typing "q"
// into a field must never quit the program).
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

// TestModelOpenAdminRobotsFromUsersLoadsRobotList is task 5.7's RED test:
// pressing "b" on screenAdminUsers opens screenAdminRobots and loads the
// robot list.
func TestModelOpenAdminRobotsFromUsersLoadsRobotList(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)},
		robots:       []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")

	updated = runKey(t, updated, "b")

	if got, want := updated.screen, screenAdminRobots; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if adminClient.listRobotsCalls != 1 {
		t.Fatalf("listRobotsCalls = %d, want 1", adminClient.listRobotsCalls)
	}
	if !strings.Contains(updated.View(), "robot$ci") || !strings.Contains(updated.View(), "team/app") {
		t.Fatalf("view = %q, want robot$ci's row listed", updated.View())
	}
}

// TestModelCreateAdminRobotShowsOneTimeSecretOnceOnCreateScreen is task 5.7's
// CRITICAL RED test (spec.md "Operator manages a robot account end to end"):
// submitting the create-robot form stays on screenAdminCreateRobot and
// displays the one-time secret exactly once, with the form cleared.
func TestModelCreateAdminRobotShowsOneTimeSecretOnceOnCreateScreen(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)},
		createRobot: ports.AdminCreatedRobot{
			Robot:    ports.AdminRobot{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true},
			Secret:   "robot-secret-value",
			Accessor: "tok_robot",
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")

	updated = runKey(t, updated, "b")
	updated = runKey(t, updated, "n")
	if got, want := updated.screen, screenAdminCreateRobot; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	updated = runKey(t, updated, "ci")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "team/app")
	// First Enter (while focused on Repository) commits the highlighted
	// suggestion and advances focus to Role -- it does not submit the form,
	// mirroring updateGrantFormKey's Add Grant behavior.
	updated = runKey(t, updated, "enter")
	if got, want := updated.adminView.CreateRobotForm.Focus, adminCreateRobotFieldRole; got != want {
		t.Fatalf("focus = %v, want %v after first Enter", got, want)
	}
	updated = runKey(t, updated, "enter")

	if adminClient.createRobotCalls != 1 {
		t.Fatalf("createRobotCalls = %d, want 1", adminClient.createRobotCalls)
	}
	if got, want := adminClient.lastCreateRobotInput.Name, "ci"; got != want {
		t.Fatalf("input.Name = %q, want %q", got, want)
	}
	if got, want := adminClient.lastCreateRobotInput.Repository, "team/app"; got != want {
		t.Fatalf("input.Repository = %q, want %q", got, want)
	}
	if got, want := updated.screen, screenAdminCreateRobot; got != want {
		t.Fatalf("screen = %q, want %q (stays to show the secret)", got, want)
	}
	if got, want := updated.adminView.RevealedTokenSecret, "robot-secret-value"; got != want {
		t.Fatalf("RevealedTokenSecret = %q, want %q", got, want)
	}
	if !strings.Contains(updated.View(), "robot-secret-value") || !strings.Contains(updated.View(), "tok_robot") {
		t.Fatalf("view = %q, want the one-time secret and accessor rendered", updated.View())
	}
	if updated.adminView.CreateRobotForm.Name != "" || updated.adminView.CreateRobotForm.Repository != "" {
		t.Fatalf("CreateRobotForm = %#v, want cleared after creation", updated.adminView.CreateRobotForm)
	}
}

// TestModelCreateRobotFormRepositorySuggestionsFilterAndSelect is the manual
// RC follow-up's RED test: screenAdminCreateRobot's Repository field must
// offer the same autosuggest behavior as screenAdminAddGrant's Repository
// field (updateGrantFormKey) -- typing filters known repositories, Up/Down
// cycle the highlighted suggestion, and Enter while focused on Repository
// commits the highlighted suggestion and advances focus to Role WITHOUT
// submitting the whole create-robot form.
func TestModelCreateRobotFormRepositorySuggestionsFilterAndSelect(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: time.Date(2026, time.August, 14, 12, 5, 0, 0, time.UTC)},
	}
	model := newAdminReadyModelWithCatalog(t, []string{"library/alpine", "team/demo", "team/backend", "ops/console"}, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")

	updated = runKey(t, updated, "b")
	updated = runKey(t, updated, "n")
	if got, want := updated.screen, screenAdminCreateRobot; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}

	updated = runKey(t, updated, "tab")
	if got, want := updated.adminView.CreateRobotForm.Focus, adminCreateRobotFieldRepository; got != want {
		t.Fatalf("focus = %v, want %v", got, want)
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
	if got, want := updated.adminView.CreateRobotForm.Repository, "team/backend"; got != want {
		t.Fatalf("repository = %q, want %q", got, want)
	}
	if got, want := updated.adminView.CreateRobotForm.Focus, adminCreateRobotFieldRole; got != want {
		t.Fatalf("focus = %v, want %v (Enter must advance focus, not submit)", got, want)
	}
	if adminClient.createRobotCalls != 0 {
		t.Fatalf("createRobotCalls = %d, want 0 (Enter on Repository must not submit the form)", adminClient.createRobotCalls)
	}
}

// TestModelAdminRobotTokensKeyClearsAnyPreviouslyRevealedSecretBeforeShowingTokenScreen
// is task 5.7's CRITICAL defense-in-depth RED test, mirroring PR 3's
// remediation lesson: RevealedTokenSecret/Accessor are the SAME fields both
// the robot-creation reveal (screenAdminCreateRobot) and the reused human
// admin-token reveal (screenAdminEditUserTokens, via "t" on a selected
// robot) read. If a secret from a just-created robot is still sitting in
// AdminViewState when the operator presses "t" on ANY robot row, the reused
// token screen would render that stale secret a second time -- without a
// fresh token ever being issued. openAdminRobotTokens (the "t" handler) MUST
// clear it before navigating, exactly like the existing openAdminEditTokens/
// openAdminEditGrants precedent already does for the human path.
func TestModelAdminRobotTokensKeyClearsAnyPreviouslyRevealedSecretBeforeShowingTokenScreen(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		tokens: map[string][]ports.AdminToken{"u-2": {{ID: "t-1", UserID: "u-2", Accessor: "tok_new"}}},
	}
	model := newAdminReadyModel(t, adminClient)
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenAdminRobots
	model.adminView.Robots = []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}}
	model.adminView.SelectedRobot = 0
	// Simulates the state immediately after a DIFFERENT prior creation whose
	// secret was never explicitly dismissed by the operator -- the exact
	// "second write path" shape PR 3's remediation caught.
	model.adminView.RevealedTokenSecret = "stale-secret-from-earlier-creation"
	model.adminView.RevealedTokenAccessor = "tok_old"

	updated := runKey(t, model, "t")

	if updated.adminView.RevealedTokenSecret != "" {
		t.Fatalf("RevealedTokenSecret = %q after opening robot tokens, want cleared (must never leak a stale secret onto the reused token screen)", updated.adminView.RevealedTokenSecret)
	}
	if updated.adminView.RevealedTokenAccessor != "" {
		t.Fatalf("RevealedTokenAccessor = %q after opening robot tokens, want cleared", updated.adminView.RevealedTokenAccessor)
	}
	if strings.Contains(updated.View(), "stale-secret-from-earlier-creation") {
		t.Fatalf("view = %q, must never render the stale secret", updated.View())
	}
	if got, want := updated.screen, screenAdminEditUserTokens; got != want {
		t.Fatalf("screen = %q, want %q (reuses the existing token screen)", got, want)
	}
	if got, want := updated.adminView.SelectedUserID, "u-2"; got != want {
		t.Fatalf("SelectedUserID = %q, want the robot's ID %q", got, want)
	}
}

// TestModelAdminCreateRobotEscClearsRevealedSecretBeforeReturningToList
// triangulates the guard above from the other exit path: leaving
// screenAdminCreateRobot via Esc after a secret was shown must also clear
// it, so a later "n" (reopening the create form) never renders it again.
func TestModelCreateAdminRobotEscClearsRevealedSecretBeforeReturningToList(t *testing.T) {
	t.Parallel()

	model := newAdminReadyModel(t, &fakeAdminClient{})
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenAdminCreateRobot
	model.adminView.RevealedTokenSecret = "just-shown-secret"
	model.adminView.RevealedTokenAccessor = "tok_just_shown"

	updated := runKey(t, model, "esc")

	if got, want := updated.screen, screenAdminRobots; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if updated.adminView.RevealedTokenSecret != "" {
		t.Fatalf("RevealedTokenSecret = %q after Esc, want cleared", updated.adminView.RevealedTokenSecret)
	}
}

// TestModelEnableDisableAdminRobotConfirmFlow is task 5.7's RED test for the
// enable/disable mutation, mirroring the existing human-user enable/disable
// confirm flow but scoped to screenAdminRobots and reusing
// EnableUser/DisableUser with the robot's user ID (design.md Decision 6).
func TestModelEnableDisableAdminRobotConfirmFlow(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		robots:      []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
		disableUser: ports.AdminUser{ID: "u-2", Username: "robot$ci", Enabled: false},
	}
	model := newAdminReadyModel(t, adminClient)
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenAdminRobots
	model.adminView.Robots = adminClient.robots
	model.adminView.SelectedRobot = 0

	updated := runKey(t, model, "x")
	if !strings.Contains(updated.View(), `Confirm disable robot "robot$ci"?`) {
		t.Fatalf("view = %q, want a disable-robot confirmation", updated.View())
	}

	updated = runKey(t, updated, "enter")

	if adminClient.disableCalls != 1 {
		t.Fatalf("disableCalls = %d, want 1", adminClient.disableCalls)
	}
	if adminClient.listRobotsCalls < 1 {
		t.Fatalf("listRobotsCalls = %d, want at least 1 (refreshed after mutation)", adminClient.listRobotsCalls)
	}
	if got, want := updated.screen, screenAdminRobots; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
}

// TestModelAdminRobotDeleteKeyOpensConfirmModal is this task's RED test for
// the "d" key on screenAdminRobots: unlike screenAdminUsers (no delete key
// at all) and unlike this same screen's "e"/"x" (enable/disable), "d" opens
// a destructive, irreversible confirmation -- distinct wording from the
// reversible disable confirm above, since a deleted robot cannot be
// recovered the way a disabled one can be re-enabled.
func TestModelAdminRobotDeleteKeyOpensConfirmModal(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		robots: []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenAdminRobots
	model.adminView.Robots = adminClient.robots
	model.adminView.SelectedRobot = 0

	updated := runKey(t, model, "d")

	if !updated.adminView.Confirm.Active() {
		t.Fatalf("adminView.Confirm.Active() = false, want true (delete opens a confirm)")
	}
	view := updated.View()
	if !strings.Contains(view, `robot$ci`) {
		t.Fatalf("view = %q, want the robot's username in the confirmation", view)
	}
	if !strings.Contains(strings.ToLower(view), "cannot be undone") {
		t.Fatalf("view = %q, want an irreversibility warning distinguishing this from disable", view)
	}
}

// TestModelDeleteAdminRobotConfirmFlow triangulates the RED test above by
// driving Enter on the opened modal: it must call AdminClient.DeleteRobot
// with the selected robot's ID and, on success, the robot must disappear
// from the refreshed list (proving the reload -- not a hardcoded stub --
// drives the list, the same pattern TestModelEnableDisableAdminRobotConfirmFlow
// already establishes for enable/disable).
func TestModelDeleteAdminRobotConfirmFlow(t *testing.T) {
	t.Parallel()

	adminClient := &fakeAdminClient{
		robots: []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
	}
	model := newAdminReadyModel(t, adminClient)
	model.adminAuth = adminAuthStateAuthenticated
	model.screen = screenAdminRobots
	model.adminView.Robots = adminClient.robots
	model.adminView.SelectedRobot = 0

	updated := runKey(t, model, "d")
	updated = runKey(t, updated, "enter")

	if adminClient.lastDeleteRobotUserID != "u-2" {
		t.Fatalf("lastDeleteRobotUserID = %q, want %q", adminClient.lastDeleteRobotUserID, "u-2")
	}
	if adminClient.listRobotsCalls < 1 {
		t.Fatalf("listRobotsCalls = %d, want at least 1 (refreshed after mutation)", adminClient.listRobotsCalls)
	}
	if got, want := updated.screen, screenAdminRobots; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if updated.adminView.Confirm.Active() {
		t.Fatalf("adminView.Confirm.Active() = true, want false (closed after success)")
	}
	for _, robot := range updated.adminView.Robots {
		if robot.ID == "u-2" {
			t.Fatalf("Robots = %+v, want %q removed after delete", updated.adminView.Robots, "u-2")
		}
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
				m = runKey(t, m, "enter")
				return m, adminClient
			},
			openKey:        "e",
			wantOpenSubstr: `Confirm enable user "alice"?`,
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
				m = runKey(t, m, "enter")
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Confirm disable user "alice"?`,
			callCount:      func(f *fakeAdminClient) int { return f.disableCalls },
		},
		{
			name: "enable-feature",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					features:     []ports.FeatureSummary{{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: false}},
					featurePage: ports.FeaturePage{
						Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: false},
						Actions: []ports.FeatureAction{{ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: `Confirm enable feature "gitleaks"?`}},
					},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = runKey(t, m, "f")
				m = runKey(t, m, "enter")
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
					features:     []ports.FeatureSummary{{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true}},
					featurePage: ports.FeaturePage{
						Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true},
						Actions: []ports.FeatureAction{{ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "gitleaks"?`}},
					},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = runKey(t, m, "f")
				m = runKey(t, m, "enter")
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Confirm disable feature "gitleaks"?`,
			callCount:      func(f *fakeAdminClient) int { return f.executeFeatureActionCalls },
		},
		{
			name: "delete-grant",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					users:        []ports.AdminUser{{ID: "u-1", Username: "alice", Enabled: true}},
					grants:       map[string][]ports.AdminRepoGrant{"u-1": {{Repository: regixtrydomain.MustParseRepositoryRef("team/app"), Role: domainauth.RepoRoleWriter}}},
				}
				m := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
				m = runKey(t, m, "enter")
				m = runKey(t, m, "g")
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Remove grant "team/app" from "alice"?`,
			callCount:      func(f *fakeAdminClient) int { return f.deleteGrantCalls },
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
				m = runKey(t, m, "enter")
				m = runKey(t, m, "t")
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Revoke admin token "tok-1" for "alice"?`,
			callCount:      func(f *fakeAdminClient) int { return f.revokeTokenCalls },
		},
		{
			name: "delete-repo-grant",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					loginSession: AdminSession{Username: "delegate", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
					repoGrants:   map[string][]ports.AdminRepositoryGrant{"team/app": {{Username: "bob", Role: domainauth.RepoRoleWriter}}},
				}
				m := newAdminReadyModel(t, adminClient)
				m.adminIntent = adminIntentRepoGrants
				m.adminIntentRepository = "team/app"
				m = runAdminLogin(t, m, "delegate", "secret-pass")
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Remove grant for "bob" from "team/app"?`,
			callCount:      func(f *fakeAdminClient) int { return f.deleteRepoGrantCalls },
		},
		{
			name: "enable-robot",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					robots:     []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: false}},
					enableUser: ports.AdminUser{ID: "u-2", Username: "robot$ci", Enabled: true},
				}
				m := newAdminReadyModel(t, adminClient)
				m.adminAuth = adminAuthStateAuthenticated
				m.screen = screenAdminRobots
				m.adminView.Robots = adminClient.robots
				m.adminView.SelectedRobot = 0
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
					robots:      []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
					disableUser: ports.AdminUser{ID: "u-2", Username: "robot$ci", Enabled: false},
				}
				m := newAdminReadyModel(t, adminClient)
				m.adminAuth = adminAuthStateAuthenticated
				m.screen = screenAdminRobots
				m.adminView.Robots = adminClient.robots
				m.adminView.SelectedRobot = 0
				return m, adminClient
			},
			openKey:        "x",
			wantOpenSubstr: `Confirm disable robot "robot$ci"?`,
			callCount:      func(f *fakeAdminClient) int { return f.disableCalls },
		},
		{
			name: "delete-robot",
			setup: func(t *testing.T) (Model, *fakeAdminClient) {
				adminClient := &fakeAdminClient{
					robots: []ports.AdminRobot{{ID: "u-2", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true}},
				}
				m := newAdminReadyModel(t, adminClient)
				m.adminAuth = adminAuthStateAuthenticated
				m.screen = screenAdminRobots
				m.adminView.Robots = adminClient.robots
				m.adminView.SelectedRobot = 0
				return m, adminClient
			},
			openKey:        "d",
			wantOpenSubstr: `Delete robot "robot$ci"? This action cannot be undone.`,
			callCount:      func(f *fakeAdminClient) int { return f.deleteRobotCalls },
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name+"/confirm", func(t *testing.T) {
			t.Parallel()
			m, client := c.setup(t)
			opened := runKey(t, m, c.openKey)
			if !strings.Contains(ansi.Strip(opened.View()), c.wantOpenSubstr) {
				t.Fatalf("view after %q = %q, want to contain %q", c.openKey, opened.View(), c.wantOpenSubstr)
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
			if strings.Contains(ansi.Strip(cancelled.View()), c.wantOpenSubstr) {
				t.Fatalf("view after esc = %q, want confirm closed", cancelled.View())
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
	enterUpdated, deleteCmd := pending.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

	// Phase 11 (design.md Decision I): 'f' lands on securityMenuScreen, the
	// bare Security & Compliance peer list -- no Feature Page detail until
	// Enter navigates into the highlighted feature's own screen.
	if updated.screen != screenAdminFeatures {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminFeatures)
	}
	if adminClient.listFeaturesCalls != 1 {
		t.Fatalf("listFeaturesCalls = %d, want 1", adminClient.listFeaturesCalls)
	}
	if !strings.Contains(updated.View(), "Built-in Features") || !strings.Contains(updated.View(), "trivy") {
		t.Fatalf("view = %q, want the Security & Compliance menu listing trivy", updated.View())
	}

	updated = runKey(t, updated, "enter")
	if updated.screen != screenSecurityTrivy {
		t.Fatalf("screen = %q, want %q", updated.screen, screenSecurityTrivy)
	}
	if adminClient.getFeaturePageCalls != 1 {
		t.Fatalf("getFeaturePageCalls = %d, want one page read", adminClient.getFeaturePageCalls)
	}
	view := updated.View()
	for _, want := range []string{"Configuration", "Runtime", "Version: 0.57.1", "x: disable"} {
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
	if updated.screen != screenSecurityTrivy {
		t.Fatalf("screen = %q, want %q after disable", updated.screen, screenSecurityTrivy)
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

	// Phase 11 (design.md Decision I): securityMenuScreen's own peer list
	// shows every backend-declared feature by name/state regardless of
	// kind, even one with no dedicated drill-down screen of its own (only
	// trivy/gitleaks/signing gain one) -- Enter on such a row is inert,
	// since Decision I only builds forward-navigation targets for the three
	// known built-in features.
	view := updated.View()
	for _, want := range []string{"future-plugin", "true"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	if strings.Contains(view, "x: disable") || strings.Contains(view, "e: enable") {
		t.Fatalf("view = %q, want no undeclared actions in help", view)
	}

	navigated := runKey(t, updated, "enter")
	if navigated.screen != screenAdminFeatures {
		t.Fatalf("screen = %q, want %q (Enter on a non-built-in feature is inert)", navigated.screen, screenAdminFeatures)
	}
}

func TestModelFeatureSelectionRefreshesPageAndHelpFromBackendActions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, {Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: false, Configured: true}},
		featurePages: map[string]ports.FeaturePage{
			"trivy":    {Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "true"}}, Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "disable", Label: "Disable", ConfirmTitle: "Confirm Disable", ConfirmMessage: `Confirm disable feature "trivy"?`}}},
			"gitleaks": {Summary: ports.FeatureSummary{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: false, Configured: true}, Header: []ports.FeatureField{{Label: "Enabled", Value: "false"}}, Actions: []ports.FeatureAction{{ID: "refresh", Label: "Refresh"}, {ID: "enable", Label: "Enable", ConfirmTitle: "Confirm Enable", ConfirmMessage: `Confirm enable feature "gitleaks"?`}}},
		},
	}
	model := newAdminReadyModel(t, adminClient)
	updated := runAdminLogin(t, model, "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "down")
	updated = runKey(t, updated, "enter")

	// Phase 11 (design.md Decision I): moving securityMenuScreen's own
	// selection no longer auto-refreshes any Feature Page (there is none on
	// that bare peer list) -- navigating into a DIFFERENT feature's own
	// screen is what triggers its independent page load and
	// backend-authoritative help.
	if updated.screen != screenSecurityGitleaksConfig {
		t.Fatalf("screen = %q, want %q", updated.screen, screenSecurityGitleaksConfig)
	}
	if adminClient.getFeaturePageCalls != 1 {
		t.Fatalf("getFeaturePageCalls = %d, want one page read for gitleaks", adminClient.getFeaturePageCalls)
	}
	view := updated.View()
	for _, want := range []string{"Gitleaks", "e: enable"} {
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
		// Phase 11 (design.md Decision I): Enter navigates from
		// securityMenuScreen into trivyConfigScreen, the Runtime tab's
		// successor screen -- this IS the default landing content once
		// trivy is entered (trivyReposScreen, the Repository Alerts
		// successor, is reached separately via Tab).
		updated = runKey(t, updated, "enter")

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

		// Phase 11: a non-built-in feature has no dedicated screen of its
		// own (only trivy/gitleaks/signing do), so it is only ever seen on
		// securityMenuScreen's own bare peer list -- which never shows
		// trivy-only tab chrome regardless of which feature is highlighted.
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
	updated = runKey(t, updated, "enter")

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
		features: []ports.FeatureSummary{
			{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
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
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "down")
	updated = runKey(t, updated, "enter")

	if got, want := updated.screen, screenSecurityGitleaksConfig; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}

	updated = runKey(t, updated, "s")
	modalView := updated.View()
	for _, want := range []string{"Edit Gitleaks Configuration", "Enabled", "Timeout", "Max Concurrency"} {
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
	if got, want := screen.cfg, (gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldEnabled, Enabled: true, Timeout: "5m0s", MaxConcurrency: "1"}); got != want {
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
	if strings.Contains(canceled.View(), "Edit Gitleaks Configuration") {
		t.Fatalf("view = %q, want modal closed after esc", canceled.View())
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
	if !strings.Contains(submitted.View(), "Configuration saved") {
		t.Fatalf("view = %q, want config feedback after submit", submitted.View())
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
		features: []ports.FeatureSummary{
			{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
		},
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
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "s")

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
	if !strings.Contains(submitted.View(), "invalid timeout") {
		t.Fatalf("view = %q, want invalid timeout error surfaced", submitted.View())
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
			features:     []ports.FeatureSummary{{Name: "gitleaks", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
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
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")
		updated = runKey(t, updated, "s")
		if !strings.Contains(updated.View(), "Edit Gitleaks Configuration") {
			t.Fatalf("test setup invalid: view = %q, want the gitleaks modal open", updated.View())
		}
		return updated, adminClient
	}

	t.Run("esc closes without submitting", func(t *testing.T) {
		t.Parallel()
		opened, client := newReady(t)
		closed := runKey(t, opened, "esc")
		if strings.Contains(closed.View(), "Edit Gitleaks Configuration") {
			t.Fatalf("view after esc = %q, want the modal closed", closed.View())
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
		if toggledOnce.View() == opened.View() {
			t.Fatalf("view after one space = unchanged, want Enabled to visibly flip")
		}
		toggledTwice := runKey(t, toggledOnce, " ")
		if toggledTwice.View() != opened.View() {
			t.Fatalf("view after two spaces = %q, want it to exactly restore the original %q", toggledTwice.View(), opened.View())
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
		if !strings.Contains(typed.View(), "5m0s9") {
			t.Fatalf("view after tab x4 + rune = %q, want the rune appended to Timeout (0:Enabled -> 1:Timeout -> 2:MaxConcurrency -> 0:Enabled -> 1:Timeout)", typed.View())
		}
		if strings.Contains(typed.View(), "19") {
			t.Fatalf("view after tab x4 + rune = %q, want MaxConcurrency (\"1\") untouched", typed.View())
		}
	})

	t.Run("backspace trims the focused field", func(t *testing.T) {
		t.Parallel()
		// Round-trip identity again (see the space test's comment above):
		// removing "s" from "5m0s" must change the view, and typing "s"
		// back must exactly restore it.
		opened, _ := newReady(t)
		onTimeout := runKey(t, opened, "tab")
		trimmed := runKey(t, onTimeout, "backspace")
		if trimmed.View() == onTimeout.View() {
			t.Fatalf("view after backspace = unchanged, want Timeout's trailing rune removed")
		}
		restored := runKey(t, trimmed, "s")
		if restored.View() != onTimeout.View() {
			t.Fatalf("view after backspace+\"s\" = %q, want it to exactly restore %q", restored.View(), onTimeout.View())
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
		if strings.Contains(submitted.View(), "Edit Gitleaks Configuration") {
			t.Fatalf("view after successful submit = %q, want the modal closed", submitted.View())
		}
	})

	t.Run("plain rune input appends to the focused text field", func(t *testing.T) {
		t.Parallel()
		opened, _ := newReady(t)
		onTimeout := runKey(t, opened, "tab")
		typed := runKey(t, onTimeout, "9")
		if !strings.Contains(typed.View(), "5m0s9") {
			t.Fatalf("view after rune = %q, want \"9\" appended to Timeout", typed.View())
		}
	})

	t.Run("unmapped key is a no-op", func(t *testing.T) {
		t.Parallel()
		opened, client := newReady(t)
		before := opened.View()
		updated, cmd := opened.Update(tea.KeyMsg{Type: tea.KeyLeft})
		after := updated.(Model)
		if cmd != nil {
			t.Fatalf("Update(unmapped key) returned a non-nil cmd, want nil")
		}
		if after.View() != before {
			t.Fatalf("view after an unmapped key = %q, want unchanged %q", after.View(), before)
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
		features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			Header:  []ports.FeatureField{{Label: "Enabled", Value: "true"}},
		},
		scanPolicy: ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	// Phase 11 (design.md Decision I): ScanPolicy now loads chained after
	// trivyConfigScreen's OWN page load, once navigated into via Enter --
	// not on 'f' alone (securityMenuScreen never loads any feature's page).
	updated = runKey(t, updated, "enter")

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
	if !strings.Contains(submitted.View(), "Vulnerability policy saved") {
		t.Fatalf("view = %q, want policy feedback after submit", submitted.View())
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
			features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
			},
			signingPolicy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"key-one"}},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")

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
		modalView := updated.View()
		for _, want := range []string{"Signing Policy", "Enabled", "Trusted Key (PEM)"} {
			if !strings.Contains(modalView, want) {
				t.Fatalf("view = %q, want %q", modalView, want)
			}
		}
	})

	t.Run("p opens scanPolicyModal, not signingPolicyModal, when trivy is selected", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")
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
		features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
		signingPolicy: ports.SigningPolicySettings{Enabled: true, UnsignedSelfRead: "repo_push"},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter")
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
// UnsignedSelfRead back to "" because updateSigningPolicyModalKey's Enter
// handler never included it in the save payload. Also verifies cycling the
// field itself with Space persists the new value.
func TestModelSigningPolicyModalSaveIncludesUnsignedSelfRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	newModel := func() (Model, *fakeAdminClient) {
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
			featurePage: ports.FeaturePage{
				Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
			},
			signingPolicy: ports.SigningPolicySettings{Enabled: true, UnsignedSelfRead: "repo_push"},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")
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
// saves a policy change" scenario): toggling Enabled and adding a key
// persists through the admin API and is reflected back into the modal
// (Fingerprints), the modal stays open (unlike scanPolicyModal) so the
// operator can keep adding keys, and AddKey is cleared after a successful
// submit.
func TestModelSigningPolicyModalAddKeySubmitPersistsAndReflectsCurrentSettings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 15, 0, 0, 0, time.UTC)
	adminClient := &fakeAdminClient{
		loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
		features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: false}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: false},
		},
		signingPolicy: ports.SigningPolicySettings{Enabled: false},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "p")

	// Focus starts on Enabled; toggle it on, Tab past UnsignedSelfRead to
	// AddKey, type a key.
	updated = runKey(t, updated, " ")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "tab")
	updated = runKey(t, updated, "-----BEGIN PUBLIC KEY-----fakekeydata-----END PUBLIC KEY-----")

	submitted := runKey(t, updated, "enter")

	if got, want := adminClient.updateSigningPolicyCalls, 1; got != want {
		t.Fatalf("updateSigningPolicyCalls = %d, want %d", got, want)
	}
	if !adminClient.lastSigningPolicyInput.Enabled {
		t.Fatalf("lastSigningPolicyInput.Enabled = false, want true")
	}
	if got, want := adminClient.lastSigningPolicyInput.TrustedPublicKeys, []string{"-----BEGIN PUBLIC KEY-----fakekeydata-----END PUBLIC KEY-----"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lastSigningPolicyInput.TrustedPublicKeys = %#v, want %#v", got, want)
	}
	submittedScreen, ok := submitted.adminScreens[slotSigningConfig].(signingConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotSigningConfig] not mounted after save")
	}
	if !submittedScreen.cfg.Active() {
		t.Fatal("cfg.Active() = false, want the screen to stay mounted/open after a successful save (unlike scanPolicyModal)")
	}
	if submittedScreen.cfg.AddKey != "" {
		t.Fatalf("cfg.AddKey = %q, want cleared after a successful submit", submittedScreen.cfg.AddKey)
	}
	if len(submittedScreen.cfg.Fingerprints) != 1 {
		t.Fatalf("cfg.Fingerprints = %#v, want 1 fingerprint reflected from the saved key", submittedScreen.cfg.Fingerprints)
	}
	if !strings.Contains(submitted.View(), "Signing policy saved") {
		t.Fatalf("view = %q, want signing policy feedback after submit", submitted.View())
	}

	// ClearKeys: Tab twice more (AddKey -> ClearKeys), Enter clears every
	// trusted key.
	cleared := runKey(t, submitted, "tab")
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
	if len(clearedScreen.cfg.Fingerprints) != 0 {
		t.Fatalf("cfg.Fingerprints = %#v, want empty after Clear", clearedScreen.cfg.Fingerprints)
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
		updated = runKey(t, updated, "enter") // navigate into trivyConfigScreen
		updated = runKey(t, updated, "tab")   // switch to trivyReposScreen (Repository Alerts)

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
		if !editor.exists || !strings.Contains(updated.View(), "team/api") {
			t.Fatalf("editor = %#v, want the stored override reflected", editor)
		}

		closed := runKey(t, updated, "esc")
		closedRepos, ok := closed.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok || closedRepos.editor.Active() {
			t.Fatalf("adminScreens[slotTrivyRepos].editor = %#v, want inactive after esc", closedRepos.editor)
		}
	})

	t.Run("o on trivyConfigScreen does not open the editor or collide with feature actions", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  baseFeaturePage,
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")

		updated = runKey(t, updated, "o")

		if updated.adminScreens[slotTrivyRepos] != nil {
			t.Fatal("adminScreens[slotTrivyRepos] != nil, want unmounted -- 'o' on trivyConfigScreen (the Runtime tab's successor) does nothing")
		}
		if adminClient.getRepositoryOverrideCalls != 0 {
			t.Fatalf("getRepositoryOverrideCalls = %d, want 0 (no load fired)", adminClient.getRepositoryOverrideCalls)
		}
		if adminClient.executeFeatureActionCalls != 0 {
			t.Fatalf("executeFeatureActionCalls = %d, want 0 ('o' must not fall through to featureActionForKey)", adminClient.executeFeatureActionCalls)
		}
	})

	t.Run("o with no Repository Alerts row highlighted does not open the editor", func(t *testing.T) {
		t.Parallel()
		adminClient := &fakeAdminClient{
			loginSession: AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: now.Add(5 * time.Minute)},
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage:  baseFeaturePage,
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")
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
	updated = runKey(t, updated, "enter")
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
	if !strings.Contains(submitted.View(), "Repository override saved") {
		t.Fatalf("view = %q, want save feedback", submitted.View())
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
	if !strings.Contains(cleared.View(), "inheriting global settings") {
		t.Fatalf("view = %q, want the editor to show the repository inheriting global settings after clear", cleared.View())
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
		features:     []ports.FeatureSummary{{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true}},
		featurePage: ports.FeaturePage{
			Summary: ports.FeatureSummary{Name: "signing", Kind: ports.FeatureKindBuiltin, Enabled: true},
		},
	}
	updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
	updated = runKey(t, updated, "f")
	updated = runKey(t, updated, "enter") // navigate into signingConfigScreen
	updated = runKey(t, updated, "o")     // opens screenSecuritySigningRepos

	if got, want := updated.screen, screenSecuritySigningRepos; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	updated = runKey(t, updated, "o") // opens the editor on the highlighted (only) row

	// Focus starts on Enabled: Tab to PathPrimary, type a key, Tab to
	// UnsignedSelfRead and toggle it off -> pusher, then Enter to save.
	updated = runKey(t, updated, "tab")
	for _, r := range "-----BEGIN PUBLIC KEY-----fakekeydata-----END PUBLIC KEY-----" {
		updated = runKey(t, updated, string(r))
	}
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
		updated = runKey(t, updated, "enter")
		updated = runKey(t, updated, "tab")

		if adminClient.listRepositoryScanSummariesCalls != 1 {
			t.Fatalf("listRepositoryScanSummariesCalls = %d, want one repository-alert load", adminClient.listRepositoryScanSummariesCalls)
		}
		alertsView := updated.View()
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
			features:     []ports.FeatureSummary{{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true}},
			featurePage: ports.FeaturePage{
				Summary:  ports.FeatureSummary{Name: "trivy", Kind: ports.FeatureKindBuiltin, Enabled: true, Configured: true},
				Header:   []ports.FeatureField{{Label: "Enabled", Value: "true"}},
				Sections: []ports.FeatureSection{{ID: "config", Title: "Configuration", Kind: "fields", Fields: []ports.FeatureField{{Label: "Schedule Enabled", Value: "true"}, {Label: "Interval", Value: "6h0m0s"}, {Label: "Timeout", Value: "10m0s"}, {Label: "Registry Reachable URL", Value: "https://registry.internal:5443"}, {Label: "Max Concurrency", Value: "2"}}}, {ID: "runtime", Title: "Runtime", Kind: "fields", Fields: []ports.FeatureField{{Label: "Status", Value: "ready"}, {Label: "Version", Value: "0.57.1"}}}},
			},
		}
		updated := runAdminLogin(t, newAdminReadyModel(t, adminClient), "operator", "secret-pass")
		updated = runKey(t, updated, "f")
		updated = runKey(t, updated, "enter")
		updated = runKey(t, updated, "tab")
		if !strings.Contains(updated.View(), "No repository alerts found.") {
			t.Fatalf("view = %q, want clear empty-state message", updated.View())
		}
		updated = runKey(t, updated, "tab")
		if !strings.Contains(updated.View(), "Version: 0.57.1") {
			t.Fatalf("view = %q, want runtime tab (trivyConfigScreen) still usable after empty alerts", updated.View())
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
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "tab")

	repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
	}
	summaryTable := repos.table
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
	updated = runKey(t, updated, "enter") // navigate into trivyConfigScreen
	updated = runKey(t, updated, "tab")   // switch to trivyReposScreen
	return runKey(t, updated, "enter")    // open the scan history modal
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

	model := NewModel(&fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}, WithAdminClient(adminClient))
	model.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
	model = runCmd(t, model, model.Init())
	model = runAdminLogin(t, model, "operator", "secret-pass")
	model = runKey(t, model, "f")
	model = runKey(t, model, "enter") // trivyConfigScreen
	model = runKey(t, model, "tab")   // trivyReposScreen

	// Open the modal (cursor 0 = run-a, newest first) but capture the
	// history-load Cmd instead of letting runKey auto-run its whole chain,
	// so the detail fetch it triggers can be interleaved manually below.
	// Phase 11: Enter on trivyReposScreen returns openAdminScanHistory (a
	// relay Cmd, design.md Decision B -- a migrated screen cannot write to
	// AdminViewState.ScanHistoryModal directly), one hop before the actual
	// history load Cmd the pre-change code returned directly.
	updatedRaw, openCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	afterOpenRequest := updatedRaw.(Model)
	if openCmd == nil {
		t.Fatal("expected openAdminScanHistory after enter")
	}
	updatedRaw, historyCmd := afterOpenRequest.Update(openCmd())
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

			model := NewModel(&fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}, WithAdminClient(adminClient))
			updated, _ := model.Update(tea.WindowSizeMsg{Width: minViewportWidth, Height: height})
			result := updated.(Model)
			result = runCmd(t, result, result.Init())
			result = runAdminLogin(t, result, "operator", "secret-pass")
			result = runKey(t, result, "f")
			result = runKey(t, result, "enter") // navigate into trivyConfigScreen
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
			model.adminView.Confirm = newConfirmPrompt("Enable User", "Enable alice?", "enable", "", func(screenEnv) tea.Cmd { return nil })
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
	model.screen = screenSecurityTrivy
	model.adminScreens[slotTrivyConfig] = trivyConfigScreen{
		loaded: true,
		cfg: trivyConfigModal{
			Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
			RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
		},
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

			model := NewModel(&fakeQueryService{repositorySummaries: []appregixtry.RepositorySummary{{Name: "library/alpine"}}}, WithAdminClient(adminClient))
			updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: defaultViewportHeight})
			result := updated.(Model)
			result = runCmd(t, result, result.Init())
			result = runAdminLogin(t, result, "operator", "secret-pass")
			result = runKey(t, result, "f")
			result = runKey(t, result, "enter") // navigate into trivyConfigScreen
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
	updated = runKey(t, updated, "enter") // trivyConfigScreen
	updated = runKey(t, updated, "tab")   // trivyReposScreen
	updated = runKey(t, updated, "enter") // opens the scan history modal

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

	// Phase 11: the Features table (the peer list itself) lives on
	// securityMenuScreen, available right after 'f'; the "checks" rows
	// table lives on trivyConfigScreen, reached only once navigated into.
	menu, ok := updated.adminScreens[slotSecurityMenu].(securityMenuScreen)
	if !ok {
		t.Fatal("adminScreens[slotSecurityMenu] not a mounted securityMenuScreen")
	}
	if got, want := menu.table.TotalRows(), 2; got != want {
		t.Fatalf("feature table rows = %d, want %d", got, want)
	}
	if got, want := menu.table.HighlightedRow().Data[adminTableMetaFeatureName], "trivy"; got != want {
		t.Fatalf("highlighted feature metadata = %#v, want %q", got, want)
	}
	menuView := updated.View()
	for _, want := range []string{"Name", "Kind", "Enabled", "Configured", "Current", "Latest", "Update", "0.57.1", "0.58.0", "available"} {
		if !strings.Contains(menuView, want) {
			t.Fatalf("view = %q, want %q", menuView, want)
		}
	}

	updated = runKey(t, updated, "enter")
	trivy, ok := updated.adminScreens[slotTrivyConfig].(trivyConfigScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyConfig] not a mounted trivyConfigScreen")
	}
	rowsTable, ok := trivy.rows["checks"]
	if !ok {
		t.Fatal("expected rows table for checks section")
	}
	if got, want := rowsTable.TotalRows(), 2; got != want {
		t.Fatalf("rows table rows = %d, want %d", got, want)
	}
	view := updated.View()
	for _, want := range []string{"Runtime Checks", "Title", "Status", "Detail", "DB freshness", "older than 24h", "Registry reachability", "https://registry.internal:5443"} {
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
		updated = runKey(t, updated, "enter")
		updated = runKey(t, updated, "tab")

		repos, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
		if !ok {
			t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
		}
		if got, want := repos.table.TotalRows(), 0; got != want {
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
		updated = runKey(t, updated, "enter")
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
	updated = runKey(t, updated, "enter") // trivyConfigScreen
	updated = runKey(t, updated, "tab")   // trivyReposScreen
	updated = runKey(t, updated, "enter") // opens the scan history modal

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
	updated = runKey(t, updated, "enter") // trivyConfigScreen (Runtime tab's successor)
	updated = runKey(t, updated, "tab")   // trivyReposScreen (Repository Alerts tab's successor)
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
	if !strings.Contains(updated.View(), "CVE-2026-2000") {
		t.Fatalf("view = %q, want loaded detail", updated.View())
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
	updated = runKey(t, updated, "enter") // trivyConfigScreen
	updated = runKey(t, updated, "tab")   // trivyReposScreen

	if updated.status != "" {
		t.Fatalf("status = %q, want the repository-alerts load settled (empty) before navigating", updated.status)
	}

	primaryPageSize := updated.adminView.Layout.Primary
	if primaryPageSize <= 0 || primaryPageSize >= totalRuns {
		t.Fatalf("primary pageSize = %d, want a positive size smaller than %d rows so pagination genuinely activates", primaryPageSize, totalRuns)
	}

	reposScreen, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen")
	}
	beforeTable := reposScreen.table
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

	afterReposScreen, ok := updated.adminScreens[slotTrivyRepos].(trivyReposScreen)
	if !ok {
		t.Fatal("adminScreens[slotTrivyRepos] not a mounted trivyReposScreen after paging")
	}
	afterTable := afterReposScreen.table
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
	help := shortHelpView(newAdminTheme(), afterReposScreen.Keys())
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
	calls                        struct {
		catalog        int
		tags           int
		manifest       int
		uploads        int
		signature      int
		deleteManifest int
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

	signingPolicy            ports.SigningPolicySettings
	signingPolicyErr         error
	signingPolicyUpdateErr   error
	getSigningPolicyCalls    int
	updateSigningPolicyCalls int
	lastSigningPolicyInput   ports.SigningPolicySettings
	installRuntime           ports.FeatureRuntimeState
	upgradeRuntime           ports.FeatureRuntimeState
	rollbackRuntime          ports.FeatureRuntimeState
	enableFeature            ports.FeatureDetails
	disableFeature           ports.FeatureDetails
	actionResult             ports.FeatureActionResult
	actionResults            map[string]ports.FeatureActionResult
	featureErr               error

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

	disableUser ports.AdminUser
	disableErr  error

	robots               []ports.AdminRobot
	listRobotsErr        error
	listRobotsCalls      int
	createRobot          ports.AdminCreatedRobot
	createRobotErr       error
	createRobotCalls     int
	lastCreateRobotInput ports.AdminCreateRobotInput

	// deleteRobot* backs the registry-acl-v1 robot-deletion "d" key/confirm
	// flow on screenAdminRobots.
	deleteRobotErr        error
	deleteRobotCalls      int
	lastDeleteRobotUserID string

	loginCalls            int
	listFeaturesCalls     int
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
	deleteGrantCalls                 int
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

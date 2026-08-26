package tui

import (
	"strings"
	"testing"

	"regixtry/internal/ports"
)

// TestFeatureOverrideRowsAreCatalogUnionStoredOverrides is the Phase 12 task
// 12.1 RED test (T2.7, design.md Decision J): the row set is the union of
// the Console catalog and stored override rows, including the empty-catalog
// case (stored-override rows plus an explicit empty state, never an empty
// screen with no explanation).
func TestFeatureOverrideRowsAreCatalogUnionStoredOverrides(t *testing.T) {
	t.Parallel()

	t.Run("catalog repository with no override shows inheriting global settings", func(t *testing.T) {
		t.Parallel()

		rows := mergeFeatureOverrideRows([]string{"library/alpine"}, nil)
		if len(rows) != 1 {
			t.Fatalf("rows = %#v, want exactly 1 row", rows)
		}
		if rows[0].Repository != "library/alpine" || rows[0].HasOverride {
			t.Fatalf("rows[0] = %#v, want library/alpine with no override", rows[0])
		}
	})

	t.Run("stored override merges onto its catalog row", func(t *testing.T) {
		t.Parallel()

		overrides := []ports.RepositoryOverrideDetails{{Repository: "library/alpine", Enabled: true, ConfigPath: "/etc/gitleaks/config.toml"}}
		rows := mergeFeatureOverrideRows([]string{"library/alpine", "team/api"}, overrides)
		if len(rows) != 2 {
			t.Fatalf("rows = %#v, want 2 rows (union, not duplicated)", rows)
		}
		if !rows[0].HasOverride || rows[0].Detail.ConfigPath != "/etc/gitleaks/config.toml" {
			t.Fatalf("rows[0] = %#v, want the stored override merged in", rows[0])
		}
		if rows[1].HasOverride {
			t.Fatalf("rows[1] = %#v, want team/api with no override", rows[1])
		}
	})

	t.Run("stored override for a repository no longer in the catalog still shows", func(t *testing.T) {
		t.Parallel()

		overrides := []ports.RepositoryOverrideDetails{{Repository: "removed/repo", Enabled: true}}
		rows := mergeFeatureOverrideRows([]string{"library/alpine"}, overrides)
		if len(rows) != 2 {
			t.Fatalf("rows = %#v, want the catalog row plus the orphaned override row", rows)
		}
		found := false
		for _, row := range rows {
			if row.Repository == "removed/repo" && row.HasOverride {
				found = true
			}
		}
		if !found {
			t.Fatalf("rows = %#v, want removed/repo present with HasOverride=true", rows)
		}
	})

	t.Run("empty catalog with no stored overrides yields zero rows", func(t *testing.T) {
		t.Parallel()

		rows := mergeFeatureOverrideRows(nil, nil)
		if len(rows) != 0 {
			t.Fatalf("rows = %#v, want empty -- the screen renders the explicit empty state, not a fabricated row", rows)
		}
	})
}

// TestFeatureOverridesScreenViewRendersABubbleTable guards the switch from a
// plain fmt.Sprintf line list to a real bordered bubble-table (mirroring
// trivyReposScreen's own table, "como siempre" -- the user's own words): the
// rendered body must show table chrome (a footer position marker) and every
// row's repository name, not the old bare "Repository — status" line format.
func TestFeatureOverridesScreenViewRendersABubbleTable(t *testing.T) {
	t.Parallel()

	env := screenEnv{Layout: consoleLayout{SectionRows: 20}}
	screen := featureOverridesScreen{
		feature: signingFeatureName,
		loaded:  true,
		rows: []featureOverrideRow{
			{Repository: "dtf-bookmarks-sync-admin-web", HasOverride: true, Detail: ports.RepositoryOverrideDetails{Repository: "dtf-bookmarks-sync-admin-web"}},
			{Repository: "alpine"},
		},
	}
	screen.rebuildTable(env)

	frame := screen.View(newAdminTheme(), env)
	if !strings.Contains(frame.Body, "dtf-bookmarks-sync-admin-web") {
		t.Fatalf("frame.Body = %q, want it to contain the repository name", frame.Body)
	}
	if !strings.Contains(frame.Body, "1/1") {
		t.Fatalf("frame.Body = %q, want a bubble-table footer position marker (e.g. %q), not the old bare line list", frame.Body, "1/1")
	}
}

// TestFeatureOverridesScreenViewStillHandlesErrorLoadingAndOverlayStates
// guards the three branches that must not regress when View's body-building
// switches from a manual line list to the table helper: an error message, a
// loading message, and the override editor overlay.
func TestFeatureOverridesScreenViewStillHandlesErrorLoadingAndOverlayStates(t *testing.T) {
	t.Parallel()

	env := screenEnv{Layout: consoleLayout{SectionRows: 20}}
	theme := newAdminTheme()

	errScreen := featureOverridesScreen{feature: signingFeatureName, err: "boom"}
	if frame := errScreen.View(theme, env); !strings.Contains(frame.Body, "boom") {
		t.Fatalf("error frame.Body = %q, want it to contain the error message", frame.Body)
	}

	loadingScreen := featureOverridesScreen{feature: signingFeatureName, loaded: false}
	if frame := loadingScreen.View(theme, env); !strings.Contains(frame.Body, "Loading repository overrides...") {
		t.Fatalf("loading frame.Body = %q, want the loading message", frame.Body)
	}

	overlayScreen := featureOverridesScreen{feature: signingFeatureName, loaded: true, editor: newOverrideEditor(signingFeatureName, "alpine")}
	if frame := overlayScreen.View(theme, env); frame.Overlay == "" {
		t.Fatalf("frame.Overlay is empty, want the override editor overlay rendered while the editor is active")
	}

	emptyScreen := featureOverridesScreen{feature: signingFeatureName, loaded: true}
	if frame := emptyScreen.View(theme, env); !strings.Contains(frame.Body, "No repositories available to override.") {
		t.Fatalf("empty frame.Body = %q, want the explicit empty-state message", frame.Body)
	}
}

// TestFeatureOverridesScreenRebuildsTableAfterSave guards against the table
// silently going stale after a successful save/clear: applySavedToRow
// updates s.rows in place, but the rendered s.table is a snapshot baked at
// the last rebuildTable call (load, resize, or Up/Down) -- without an
// explicit rebuild here, a repository's Detail column (e.g. "0 trusted
// key(s)") would keep showing the pre-save value until the operator moved
// the cursor or resized the terminal, even though the editor overlay
// (closed on Esc, still open right after save) hides the stale table only
// until then.
func TestFeatureOverridesScreenRebuildsTableAfterSave(t *testing.T) {
	t.Parallel()

	env := screenEnv{Layout: consoleLayout{SectionRows: 20}}
	screen := featureOverridesScreen{
		feature: signingFeatureName,
		loaded:  true,
		editor:  newOverrideEditor(signingFeatureName, "dtf-bookmarks-sync-admin-web"),
		rows: []featureOverrideRow{
			{Repository: "dtf-bookmarks-sync-admin-web", HasOverride: true, Detail: ports.RepositoryOverrideDetails{
				Repository: "dtf-bookmarks-sync-admin-web",
			}},
		},
	}
	screen.rebuildTable(env)

	saved := adminRepositoryOverrideSavedMsg{
		repository: "dtf-bookmarks-sync-admin-web",
		feature:    signingFeatureName,
		exists:     true,
		override: ports.RepositoryOverrideDetails{
			Repository:        "dtf-bookmarks-sync-admin-web",
			TrustedPublicKeys: []string{"key-one", "key-two"},
		},
	}
	updated, _, _ := screen.Update(env, saved)
	next, ok := updated.(featureOverridesScreen)
	if !ok {
		t.Fatalf("Update returned %T, want featureOverridesScreen", updated)
	}

	if got := next.table.View(); !strings.Contains(got, "2 trusted key(s)") {
		t.Fatalf("table.View() = %q, want it refreshed to show the just-saved key count (2 trusted key(s)), not the stale pre-save table", got)
	}
}

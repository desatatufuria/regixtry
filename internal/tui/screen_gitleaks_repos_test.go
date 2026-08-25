package tui

import (
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

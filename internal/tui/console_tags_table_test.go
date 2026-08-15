package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	appregixtry "regixtry/internal/app/regixtry"
)

// TestBuildConsoleTagsTableRendersTagCreatedPushedBySignedColumns is the RED
// test for the console-tags-table / console-tags-pushed-by changes: the
// Tags screen's table has exactly the four columns (Tag, Created, Pushed
// By, Signed), and each row's cells carry the tag name, a formatted
// created_at, the resolved pusher username (or "unknown" when unset), and
// the mapped Signed value.
func TestBuildConsoleTagsTableRendersTagCreatedPushedBySignedColumns(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	created := time.Date(2026, 8, 10, 9, 15, 0, 0, time.UTC)
	tags := []appregixtry.TagDetails{
		{Name: "latest", CreatedAt: created, SignatureState: appregixtry.SignatureStatusVerified, SigningEnabled: true, PushedBy: "operator"},
		{Name: "v1.2.3-rc1-alpine-slim", CreatedAt: created, SignatureState: appregixtry.SignatureStatusUnsigned, SigningEnabled: true, PushedBy: "az-deploy-ci"},
		{Name: "legacy", CreatedAt: created, SignatureState: appregixtry.SignatureStatusUnsigned, SigningEnabled: false},
	}

	table := buildConsoleTagsTable(theme, tags, 0, minTableRows)
	if got, want := table.TotalRows(), len(tags); got != want {
		t.Fatalf("buildConsoleTagsTable() TotalRows() = %d, want %d", got, want)
	}

	view := table.View()
	for _, want := range []string{"Tag", "Created", "Pushed By", "Signed", "latest", "v1.2.3-rc1-alpine-slim", "legacy", "2026-08-10 09:15", "operator", "az-deploy-ci", "unknown", "yes", "no", "n/a"} {
		if !strings.Contains(view, want) {
			t.Fatalf("table view = %q, want it to contain %q", view, want)
		}
	}
}

// TestTagSignedLabelMapsSignatureStateToYesNoOrNA is the RED test for the
// Signed column's text-only vocabulary (design decision: no icon/glyph
// vocabulary anywhere in this TUI, confirmed by scanPolicyBadge and
// signingPolicyBadge's own precedent): SigningEnabled=false collapses to
// "n/a" regardless of the computed state, "yes" only for the verified state,
// "no" for every other state while the policy is enabled.
func TestTagSignedLabelMapsSignatureStateToYesNoOrNA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  appregixtry.TagDetails
		want string
	}{
		{name: "verified and enabled is yes", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusVerified, SigningEnabled: true}, want: "yes"},
		{name: "unsigned and enabled is no", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusUnsigned, SigningEnabled: true}, want: "no"},
		{name: "untrusted and enabled is no", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusUntrusted, SigningEnabled: true}, want: "no"},
		{name: "mismatched and enabled is no", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusMismatched, SigningEnabled: true}, want: "no"},
		{name: "unverifiable and enabled is no", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusUnverifiable, SigningEnabled: true}, want: "no"},
		{name: "verified but policy disabled is n/a", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusVerified, SigningEnabled: false}, want: "n/a"},
		{name: "unsigned and policy disabled is n/a", tag: appregixtry.TagDetails{SignatureState: appregixtry.SignatureStatusUnsigned, SigningEnabled: false}, want: "n/a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tagSignedLabel(tc.tag)
			if got != tc.want {
				t.Fatalf("tagSignedLabel(%+v) = %q, want %q", tc.tag, got, tc.want)
			}
			for _, glyph := range []string{"✓", "✗", "×", "☓", "✔", "❌"} {
				if strings.Contains(got, glyph) {
					t.Fatalf("tagSignedLabel(%+v) = %q, must never contain a glyph/icon (%q)", tc.tag, got, glyph)
				}
			}
		})
	}
}

// TestFormatTagCreatedAtNeverBlank mirrors
// TestFormatScanSummaryLastExecutedNeverBlank's own zero-time fallback
// discipline.
func TestFormatTagCreatedAtNeverBlank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "non-zero time renders formatted date", when: time.Date(2026, 8, 10, 9, 15, 0, 0, time.UTC), want: "2026-08-10 09:15"},
		{name: "zero time still renders non-blank text", when: time.Time{}, want: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatTagCreatedAt(tc.when)
			if got != tc.want {
				t.Fatalf("formatTagCreatedAt() = %q, want %q", got, tc.want)
			}
			if strings.TrimSpace(got) == "" {
				t.Fatalf("formatTagCreatedAt() returned blank text, want never blank")
			}
		})
	}
}

// TestConsoleTagsTablePageSizeFloorsAtMinTableRows mirrors
// TestAdminScanHistoryModalTablePageSizeFloorsAtMinTableRows: a tiny/negative
// budget must never produce a degenerate table height.
func TestConsoleTagsTablePageSizeFloorsAtMinTableRows(t *testing.T) {
	t.Parallel()

	got := consoleTagsTablePageSize(consoleLayout{SectionRows: 0})
	if got != minTableRows {
		t.Fatalf("consoleTagsTablePageSize(SectionRows=0) = %d, want floor %d", got, minTableRows)
	}
}

// TestBuildConsoleTagsTableColumnsFitWithoutOverflow mirrors
// TestBuildAdminSecretFindingsTableColumnsFitWithoutOverflow: no rendered
// line may exceed the table's own declared column widths.
func TestBuildConsoleTagsTableColumnsFitWithoutOverflow(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	tags := []appregixtry.TagDetails{
		{Name: "sha256-d1f1ed21dad86b499c874cef225b6e61c4cf1a8684363810e99d723a306a75b4.sig", CreatedAt: time.Now(), SignatureState: appregixtry.SignatureStatusUnsigned, SigningEnabled: true, PushedBy: "a-very-long-username-example"},
	}
	table := buildConsoleTagsTable(theme, tags, 0, minTableRows)
	view := table.View()

	wantWidth := consoleTagsColumnNameWidth + consoleTagsColumnCreatedWidth + consoleTagsColumnPushedByWidth + consoleTagsColumnSignedWidth + 5 // +5: 4 columns' own border/separator characters (cols+1)
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > wantWidth {
			t.Fatalf("line %d width = %d, want <= %d (table overflowed its own declared column widths):\n%s", i, w, wantWidth, view)
		}
	}
}

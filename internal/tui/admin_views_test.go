package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestClassifyStatusTextPreservesEveryExistingSubstringCase is the Phase 4
// task 4.1 RED test (design.md Decision 2): classifyStatusText does not
// exist yet — it is the current renderAdminStatus substring switch moved
// out verbatim, so callers that do not carry an explicit kind still get
// today's behavior via statusKindAuto. Every case ported directly from the
// pre-Decision-2 switch in admin_views.go.
func TestClassifyStatusTextPreservesEveryExistingSubstringCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   statusKind
	}{
		{name: "expired", status: "Token expired", want: statusKindError},
		{name: "invalid", status: "Invalid credentials", want: statusKindError},
		{name: "error", status: "An error occurred", want: statusKindError},
		{name: "created", status: "User created", want: statusKindSuccess},
		{name: "enabled", status: "Feature enabled", want: statusKindSuccess},
		{name: "disabled", status: "Feature disabled", want: statusKindSuccess},
		{name: "reset", status: "Password reset", want: statusKindSuccess},
		{name: "saved", status: "Configuration saved", want: statusKindSuccess},
		{name: "revoked", status: "Token revoked", want: statusKindSuccess},
		{name: "loading", status: "Loading users...", want: statusKindWarning},
		{name: "refreshing", status: "Refreshing page...", want: statusKindWarning},
		{name: "submitting", status: "Submitting form...", want: statusKindWarning},
		{name: "no match falls to neutral", status: "Ready.", want: statusKindNeutral},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyStatusText(tc.status); got != tc.want {
				t.Fatalf("classifyStatusText(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// TestRenderAdminStatusSelectsStyleFromExplicitKindRegardlessOfText is the
// Phase 4 task 4.2 RED test (design.md Decision 2 / spec.md "Admin Status
// And Error Styling Uses An Explicit Status Kind"): an explicit kind other
// than statusKindAuto must select its style directly, bypassing substring
// classification entirely — including statusKindError for text containing
// none of "expired"/"invalid"/"error" (the exact bug the substring switch
// produced).
func TestRenderAdminStatusSelectsStyleFromExplicitKindRegardlessOfText(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name  string
		kind  statusKind
		style lipgloss.Style
	}{
		{name: "error kind on text with no error substring", kind: statusKindError, style: theme.error},
		{name: "success kind on text with no success substring", kind: statusKindSuccess, style: theme.success},
		{name: "warning kind on text with no warning substring", kind: statusKindWarning, style: theme.warning},
		{name: "neutral kind on text with no matching substring", kind: statusKindNeutral, style: theme.muted},
	}

	const text = "connection refused"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderAdminStatus(theme, text, tc.kind)
			// Bare styled line (design.md Decision 4: no theme.section wrap,
			// no "Status" subheading) — updated from Phase 4's boxed
			// expectation once Phase 5 de-boxed renderAdminStatus.
			want := tc.style.Render(text)
			if got != want {
				t.Fatalf("renderAdminStatus(%q, kind=%v) = %q, want %q (explicit kind must select style regardless of text)", text, tc.kind, got, want)
			}
		})
	}
}

// TestRenderAdminStatusReturnsSingleRowWithNoBorder is the Phase 5 task 5.1
// RED test (design.md Decision 4 / spec.md "Viewport-Bounded Screen
// Rendering"): renderAdminStatus currently wraps its content in
// theme.section (a RoundedBorder + Padding(1) box) plus a "Status"
// subheading, costing ~6 rows. It must render as a single bare line with no
// border rune, matching the help line's own bare decoration, for any status
// text and any explicit kind.
func TestRenderAdminStatusReturnsSingleRowWithNoBorder(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	kinds := []statusKind{statusKindAuto, statusKindNeutral, statusKindSuccess, statusKindWarning, statusKindError}
	statuses := []string{"ready", "User created", "connection refused"}

	for _, kind := range kinds {
		for _, status := range statuses {
			got := renderAdminStatus(theme, status, kind)
			if h := lipgloss.Height(got); h != 1 {
				t.Fatalf("renderAdminStatus(%q, kind=%v) height = %d, want exactly 1 (de-boxed, no Status subheading)", status, kind, h)
			}
			for _, borderRune := range []rune{'─', '│', '╭', '╮', '╰', '╯'} {
				if strings.ContainsRune(got, borderRune) {
					t.Fatalf("renderAdminStatus(%q, kind=%v) = %q, want no border rune %q", status, kind, got, borderRune)
				}
			}
			if strings.Contains(got, "Status") {
				t.Fatalf("renderAdminStatus(%q, kind=%v) = %q, want no \"Status\" subheading", status, kind, got)
			}
		}
	}
}

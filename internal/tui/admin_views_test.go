package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	bubbletable "github.com/evertras/bubble-table/table"
	"regixtry/internal/ports"
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

// TestRenderOverrideEditorFitsWithinRowBudget is the Slice 2 successor to
// the retired TestRenderRepositoryOverrideModalFitsWithinRowBudget: with the
// Feature row deleted (design.md Decision F) and the Clear row's own text
// wrapping to 2 rendered lines at this theme.section width, heights are 16
// (trivy, no error), 18 (trivy + error), 14 (gitleaks, no error), 16
// (gitleaks + error) -- comfortably within the 24-row minViewportHeight
// floor.
func TestRenderOverrideEditorFitsWithinRowBudget(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name       string
		editor     overrideEditor
		wantHeight int
	}{
		{
			name:       "trivy, no error",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, exists: true, enabled: true, pathPrimary: "/etc/trivy/ignore", pathSecondary: "/etc/trivy/policy.rego"},
			wantHeight: 16,
		},
		{
			name:       "trivy + error",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, exists: true, enabled: true, pathPrimary: "/etc/trivy/ignore", pathSecondary: "/etc/trivy/policy.rego", err: "ignore_file_path must be absolute"},
			wantHeight: 18,
		},
		{
			name:       "gitleaks, no error",
			editor:     overrideEditor{open: true, repository: "team/config", feature: gitleaksFeatureName, exists: false, pathPrimary: "/etc/gitleaks/config.toml"},
			wantHeight: 14,
		},
		{
			name:       "gitleaks + error",
			editor:     overrideEditor{open: true, repository: "team/config", feature: gitleaksFeatureName, exists: false, pathPrimary: "/etc/gitleaks/config.toml", err: "config_path must be absolute"},
			wantHeight: 16,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderOverrideEditor(theme, tc.editor)
			if h := lipgloss.Height(got); h != tc.wantHeight {
				t.Fatalf("renderOverrideEditor() height = %d, want %d\n%s", h, tc.wantHeight, got)
			}
		})
	}
}

// TestRenderOverrideEditorShowsRepositoryAndInheritanceState is the Slice 2
// successor to the retired TestRenderRepositoryOverrideModalShowsRepositoryAndInheritanceState
// (operator-admin-tui spec's "Opening the modal on a highlighted row shows
// the effective config" / "Opening the modal shows an existing override"
// scenarios): the heading carries the repository name, and the status line
// carries the inheritance state for all three cases -- Loading, override
// active, and inheriting global settings.
func TestRenderOverrideEditorShowsRepositoryAndInheritanceState(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name       string
		editor     overrideEditor
		wantStatus string
	}{
		{
			name:       "loading",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, loading: true},
			wantStatus: "Loading…",
		},
		{
			name:       "override active",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, exists: true, enabled: true, pathPrimary: "/etc/trivy/ignore"},
			wantStatus: "override active",
		},
		{
			name:       "inheriting global settings",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, exists: false},
			wantStatus: "inheriting global settings",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderOverrideEditor(theme, tc.editor)
			if !strings.Contains(got, "library/alpine") {
				t.Fatalf("renderOverrideEditor() = %q, want the repository name in the heading", got)
			}
			if !strings.Contains(got, tc.wantStatus) {
				t.Fatalf("renderOverrideEditor() = %q, want status line %q", got, tc.wantStatus)
			}
		})
	}
}

// TestRenderOverrideEditorIsASeparateSurfaceFromOtherAdminModals is the
// Slice 2 successor to the retired
// TestRenderRepositoryOverrideModalIsASeparateSurfaceFromOtherAdminModals
// (spec.md's "Out of Scope Note": the override editor is its own sibling
// surface, not an extension of scanPolicyModal/trivyConfigModal).
func TestRenderOverrideEditorIsASeparateSurfaceFromOtherAdminModals(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	overrideOutput := renderOverrideEditor(theme, overrideEditor{open: true, repository: "library/alpine", feature: trivyFeatureName, exists: true, enabled: true, pathPrimary: "/etc/trivy/ignore"})
	for _, forbidden := range []string{"Vulnerability Policy", "Severity Threshold", "Edit Trivy Configuration", "Registry Reachable URL"} {
		if strings.Contains(overrideOutput, forbidden) {
			t.Fatalf("renderOverrideEditor() output contains %q, want a separate surface\n%s", forbidden, overrideOutput)
		}
	}
}

// TestRenderOverrideEditorSigningShowsTrustedKeyLabel is the Slice 2
// successor to the retired TestRenderRepositoryOverrideModalSigningShowsTrustedKeyLabel,
// updated for the signing-key-management change: when feature is "signing",
// PathPrimary's position renders trustedKeyList's own "Trusted Keys (N)"
// heading, not the Trivy/gitleaks path label. Further updated by
// signing-keyless-verification for the new trustedIdentityList component
// (own heading row + "No trusted identities configured." row when empty,
// +2 rows versus the pre-identities budget since both cases below configure
// zero identities).
func TestRenderOverrideEditorSigningShowsTrustedKeyLabel(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name       string
		editor     overrideEditor
		wantHeight int
	}{
		{
			name:       "signing, no error",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: signingFeatureName, exists: true, enabled: true, keys: newTrustedKeyList("library/alpine", []string{"-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n"})},
			wantHeight: 18,
		},
		{
			name:       "signing + error",
			editor:     overrideEditor{open: true, repository: "library/alpine", feature: signingFeatureName, exists: true, enabled: true, keys: newTrustedKeyList("library/alpine", []string{"-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n"}), err: "trusted_public_keys[0] is invalid"},
			wantHeight: 20,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderOverrideEditor(theme, tc.editor)
			if h := lipgloss.Height(got); h != tc.wantHeight {
				t.Fatalf("renderOverrideEditor() height = %d, want %d\n%s", h, tc.wantHeight, got)
			}
			if !strings.Contains(got, "Trusted Keys") {
				t.Fatalf("renderOverrideEditor() = %q, want the signing-specific field label", got)
			}
			for _, forbidden := range []string{"Ignore File Path", "Ignore Policy Path", "Config Path"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("renderOverrideEditor() = %q, want no Trivy/gitleaks field labels for signing", got)
				}
			}
		})
	}
}

// TestRenderOverrideEditorShowsUnsignedSelfReadOnlyForSigning is the Slice 2
// successor to the retired TestRenderRepositoryOverrideModalShowsUnsignedSelfReadOnlyForSigning:
// the field's label and current value render for the signing feature
// (normalizing an unseeded "" to "off"), and never render for
// trivy/gitleaks.
func TestRenderOverrideEditorShowsUnsignedSelfReadOnlyForSigning(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	signing := renderOverrideEditor(theme, overrideEditor{open: true, repository: "team/az-deploy-demo", feature: signingFeatureName, exists: true, enabled: true, pathPrimary: "-----BEGIN PUBLIC KEY-----", unsignedSelfRead: "repo_push"})
	if !strings.Contains(signing, "Unsigned Self-Read") {
		t.Fatalf("renderOverrideEditor() = %q, want the Unsigned Self-Read label for signing", signing)
	}
	if !strings.Contains(signing, "repo_push") {
		t.Fatalf("renderOverrideEditor() = %q, want the current repo_push value shown", signing)
	}

	unseededSigning := renderOverrideEditor(theme, overrideEditor{open: true, repository: "team/az-deploy-demo", feature: signingFeatureName, exists: false})
	if !strings.Contains(unseededSigning, "off") {
		t.Fatalf(`renderOverrideEditor() = %q, want "" to normalize to "off"`, unseededSigning)
	}

	for _, feature := range []string{trivyFeatureName, gitleaksFeatureName} {
		got := renderOverrideEditor(theme, overrideEditor{open: true, repository: "team/az-deploy-demo", feature: feature, exists: true, enabled: true, pathPrimary: "/etc/config"})
		if strings.Contains(got, "Unsigned Self-Read") {
			t.Fatalf("renderOverrideEditor() feature %q = %q, want no Unsigned Self-Read row outside signing", feature, got)
		}
	}
}

// TestRenderTrivyTabsComposesPolicyBadgeAtZeroRowCost is the Phase 9 task
// 9.1 RED test (design.md Decision 6): renderTrivyTabs must still return
// exactly 2 rows (subheading + composed tab line) once the policy badge is
// composed onto the existing tab line, for both an enabled and a disabled
// policy state.
func TestRenderTrivyTabsComposesPolicyBadgeAtZeroRowCost(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name   string
		policy ports.ScanPolicySettings
	}{
		{name: "enabled policy", policy: ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}},
		{name: "disabled policy", policy: ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderTrivyTabs(theme, screenSecurityTrivy, tc.policy)
			if h := lipgloss.Height(got); h != 2 {
				t.Fatalf("renderTrivyTabs() height = %d, want exactly 2 (subheading + composed tab line, +0 rows for the badge)\n%s", h, got)
			}
		})
	}
}

// TestRenderTrivyTabsComposedWidthStaysWithinSectionWidthFloor is the Phase
// 9 task 9.2 RED test: the composed tab line must not wrap at the 150-
// column floor design.md measured (55 of 146 content columns).
func TestRenderTrivyTabsComposedWidthStaysWithinSectionWidthFloor(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	got := renderTrivyTabs(theme, screenSecurityTrivyRepos, ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh})

	budget := sectionWidth(consoleLayout{Width: 150, Height: 24})
	if w := lipgloss.Width(got); w > budget {
		t.Fatalf("renderTrivyTabs() width = %d, want <= %d (sectionWidth at the 150-column floor)\n%s", w, budget, got)
	}
}

// TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph is the
// Phase 9 task 9.3 RED test (spec.md "Badge uses text, not an icon or
// glyph"): badge text reflects enabled/threshold, table-driven, and the
// regression guard asserts every rune is printable ASCII, not just a
// string-equality check against known glyphs.
func TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name   string
		policy ports.ScanPolicySettings
		want   string
	}{
		{name: "enabled critical", policy: ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}, want: "Policy: ON (CRITICAL)"},
		{name: "enabled critical_high", policy: ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh}, want: "Policy: ON (CRITICAL+HIGH)"},
		{name: "disabled", policy: ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCritical}, want: "Policy: OFF"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderTrivyTabs(theme, screenSecurityTrivy, tc.policy)
			if !strings.Contains(ansi.Strip(got), tc.want) {
				t.Fatalf("renderTrivyTabs() = %q, want it to contain %q", ansi.Strip(got), tc.want)
			}

			for _, r := range ansi.Strip(got) {
				if r > 126 {
					t.Fatalf("renderTrivyTabs() contains non-ASCII rune %q (%U), want text only, no icon or glyph\n%s", r, r, ansi.Strip(got))
				}
			}
		})
	}
}

// TestRenderToggleFieldCompactedToTwoRows is the Phase 2 task 2.2
// characterization test: renderToggleField puts its on/off value inside the
// same bordered box as text/secret fields today (4 rows total: label + 3-row
// bordered value). Once theme.input/theme.inputFocus are flattened, its
// value line collapses to 1 row, so label+value is 2 rows total, identical
// whether focused or not.
func TestRenderToggleFieldCompactedToTwoRows(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	for _, focused := range []bool{false, true} {
		got := renderToggleField(theme, "Schedule Enabled", true, focused)
		if h := lipgloss.Height(got); h != 2 {
			t.Fatalf("renderToggleField(focused=%v) height = %d, want exactly 2 (label + flattened single-row value)", focused, h)
		}
	}
}

// TestRenderAdminScanSummaryShowsEmptyStateThenPopulatedTable is the Phase 2
// task 2.2/2.4 RED test: renderAdminScanSummary shows the same two empty
// states renderTrivyRepositoryAlerts already established (spec.md
// "Repository Alerts Summarized Per Repository With Ordering And
// Freshness"), then the built ScanSummary table once TrivySummaries and the
// table are populated.
func TestRenderAdminScanSummaryShowsEmptyStateThenPopulatedTable(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	got := strings.Join(renderAdminScanSummary(theme, nil, false, bubbletable.Model{}), "\n")
	if !strings.Contains(got, "Loading repository alerts...") {
		t.Fatalf("renderAdminScanSummary() = %q, want the not-yet-loaded message", got)
	}

	got = strings.Join(renderAdminScanSummary(theme, nil, true, bubbletable.Model{}), "\n")
	if !strings.Contains(got, "No repository alerts found.") {
		t.Fatalf("renderAdminScanSummary() = %q, want the loaded-but-empty message", got)
	}

	summaries := []repositorySummary{{Repository: "acme/api", RunCount: 1}}
	table := buildAdminScanSummaryTable(summaries, 0, minTableRows)
	got = strings.Join(renderAdminScanSummary(theme, summaries, true, table), "\n")
	if !strings.Contains(got, "acme/api") {
		t.Fatalf("renderAdminScanSummary() = %q, want the populated ScanSummary table containing %q", got, "acme/api")
	}
}


package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	bubbletable "github.com/evertras/bubble-table/table"
	"github.com/muesli/termenv"
	domainauth "regixtry/internal/domain/auth"
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

// TestRenderAdminModalRendersTitleMessageAndHelp is the Phase 3 task 3.1
// approval test (design.md Decision 1), updated for Phase 5's confirmPrompt
// retirement of adminConfirmModal (design.md Decision E): renderAdminModal
// moved onto confirmPrompt.view, called here directly instead of through
// renderAdminWorkspace's stacked composition. Captures its standalone
// rendered content and measured height (title+message+blank+help = 4 inner
// rows + 4 rows of theme.section chrome = 8, per design.md's measured-height
// table).
func TestRenderAdminModalRendersTitleMessageAndHelp(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := newConfirmPrompt("Enable User", "Enable alice?", "enable", "", func(screenEnv) tea.Cmd { return nil })

	got := modal.view(theme)

	if !strings.Contains(got, "Enable User") {
		t.Fatalf("confirmPrompt.view() = %q, want the title present", got)
	}
	if !strings.Contains(got, "Enable alice?") {
		t.Fatalf("confirmPrompt.view() = %q, want the message present", got)
	}
	if !strings.Contains(got, "Enter: enable | Esc: cancel") {
		t.Fatalf("confirmPrompt.view() = %q, want the confirm/cancel help line present", got)
	}
	if h := lipgloss.Height(got); h != 8 {
		t.Fatalf("confirmPrompt.view() height = %d, want 8 (title+message+blank+help = 4 inner rows + 4 rows theme.section chrome, design.md Decision 1)", h)
	}
}

// TestRenderTrivyConfigModalFitsWithinCompactedRowBudget is the Phase 2 task
// 2.2 RED test (design.md Decision 5): the modal is currently 27 rows
// (29 with an error), well past the 24-row viewport floor. Once
// theme.input/theme.inputFocus are flattened (Decision 5), it must fit
// within 20 rows both with and without an error message present.
func TestRenderTrivyConfigModalFitsWithinCompactedRowBudget(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name  string
		modal trivyConfigModal
	}{
		{
			name: "without error",
			modal: trivyConfigModal{
				Open: true, Focus: trivyConfigFieldScheduleEnabled,
				ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
				RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
			},
		},
		{
			name: "with error",
			modal: trivyConfigModal{
				Open: true, Focus: trivyConfigFieldInterval,
				ScheduleEnabled: true, Interval: "bad", Timeout: "30s",
				RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
				Error: "Interval must be a valid duration",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderTrivyConfigModal(theme, tc.modal)
			if h := lipgloss.Height(got); h > 20 {
				t.Fatalf("renderTrivyConfigModal() height = %d, want <= 20 (design.md Decision 5 compaction)\n%s", h, got)
			}
		})
	}
}

// TestRenderTrivyConfigModalHeightIsIdenticalAcrossEveryFocusPosition is the
// Phase 2 task 2.2 RED test: since theme.input/theme.inputFocus render the
// same number of rows regardless of focus (Decision 5's whole point — focus
// re-expressed in color, not border), the modal's total height must not
// change as focus tabs between fields.
func TestRenderTrivyConfigModalHeightIsIdenticalAcrossEveryFocusPosition(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	base := trivyConfigModal{
		Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
		RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
	}

	focusPositions := []trivyConfigField{
		trivyConfigFieldScheduleEnabled,
		trivyConfigFieldInterval,
		trivyConfigFieldTimeout,
		trivyConfigFieldRegistryReachableURL,
		trivyConfigFieldMaxConcurrency,
	}

	var wantHeight int
	for i, focus := range focusPositions {
		modal := base
		modal.Focus = focus
		got := lipgloss.Height(renderTrivyConfigModal(theme, modal))
		if i == 0 {
			wantHeight = got
			continue
		}
		if got != wantHeight {
			t.Fatalf("renderTrivyConfigModal() height at focus %d = %d, want %d (identical across every focus position, no reflow)", focus, got, wantHeight)
		}
	}
}

// TestRenderGitleaksConfigModalFitsWithinViewportFloor mirrors
// TestRenderTrivyConfigModalFitsWithinCompactedRowBudget for gitleaks' own
// 3-field modal (Enabled, Timeout, MaxConcurrency), which must fit
// comfortably under the 24-row minViewportHeight floor.
func TestRenderGitleaksConfigModalFitsWithinViewportFloor(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name  string
		modal gitleaksConfigModal
	}{
		{
			name:  "without error",
			modal: gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldEnabled, Enabled: true, Timeout: "5m", MaxConcurrency: "1"},
		},
		{
			name:  "with error",
			modal: gitleaksConfigModal{Open: true, Focus: gitleaksConfigFieldTimeout, Enabled: true, Timeout: "bad", MaxConcurrency: "1", Error: "invalid timeout: time: invalid duration \"bad\""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := gitleaksConfigScreen{cfg: tc.modal}.View(theme, screenEnv{}).Overlay
			if h := lipgloss.Height(got); h > 20 {
				t.Fatalf("gitleaksConfigScreen.View() height = %d, want <= 20\n%s", h, got)
			}
		})
	}
}

// TestRenderGitleaksConfigModalIsASeparateSurfaceFromTrivyConfigModal
// mirrors TestRenderScanPolicyModalIsASeparateSurfaceFromTrivyConfigModal:
// gitleaks' 3 fields must never appear in Trivy's modal (and vice versa),
// keeping the two configuration surfaces independently editable.
func TestRenderGitleaksConfigModalIsASeparateSurfaceFromTrivyConfigModal(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	trivyOutput := renderTrivyConfigModal(theme, trivyConfigModal{
		Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
		RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
	})
	if strings.Contains(trivyOutput, "Edit Gitleaks Configuration") {
		t.Fatalf("renderTrivyConfigModal() output contains %q, want the gitleaks modal to be a separate surface\n%s", "Edit Gitleaks Configuration", trivyOutput)
	}

	gitleaksOutput := gitleaksConfigScreen{cfg: gitleaksConfigModal{Open: true, Enabled: true, Timeout: "5m", MaxConcurrency: "1"}}.View(theme, screenEnv{}).Overlay
	for _, forbidden := range []string{"Edit Trivy Configuration", "Schedule Enabled", "Registry Reachable URL"} {
		if strings.Contains(gitleaksOutput, forbidden) {
			t.Fatalf("gitleaksConfigScreen.View() output contains %q, want a separate surface from Trivy's modal\n%s", forbidden, gitleaksOutput)
		}
	}
}

// TestRenderScanPolicyModalFitsWithinElevenRowBudget is the Phase 8 task 8.2
// RED test (design.md Decision 6): heading + 2 fields x 2 rows + blank/help
// = 7 inner rows + 4 rows theme.section chrome = 11 total, 13 with an error
// present (+1 blank +1 error line) — mirrors
// TestRenderTrivyConfigModalFitsWithinCompactedRowBudget's pattern at the
// 11/13-row budget instead of 17/20.
func TestRenderScanPolicyModalFitsWithinElevenRowBudget(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name       string
		modal      scanPolicyModal
		wantHeight int
	}{
		{
			name:       "without error",
			modal:      scanPolicyModal{Open: true, Focus: scanPolicyFieldEnabled, Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			wantHeight: 11,
		},
		{
			name:       "with error",
			modal:      scanPolicyModal{Open: true, Focus: scanPolicyFieldThreshold, Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh, Error: "severity_threshold must be critical or critical_high"},
			wantHeight: 13,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderScanPolicyModal(theme, tc.modal)
			if h := lipgloss.Height(got); h != tc.wantHeight {
				t.Fatalf("renderScanPolicyModal() height = %d, want %d (design.md Decision 6's 11/13-row budget)\n%s", h, tc.wantHeight, got)
			}
		})
	}
}

// TestRenderScanPolicyModalHeightIsIdenticalAcrossEveryFocusPosition mirrors
// TestRenderTrivyConfigModalHeightIsIdenticalAcrossEveryFocusPosition: the
// enabled-toggle and threshold-cycle fields cost the same 2 rows each, so
// total height must not change as focus tabs between them.
func TestRenderScanPolicyModalHeightIsIdenticalAcrossEveryFocusPosition(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	base := scanPolicyModal{Open: true, Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}

	focusPositions := []scanPolicyField{scanPolicyFieldEnabled, scanPolicyFieldThreshold}

	var wantHeight int
	for i, focus := range focusPositions {
		modal := base
		modal.Focus = focus
		got := lipgloss.Height(renderScanPolicyModal(theme, modal))
		if i == 0 {
			wantHeight = got
			continue
		}
		if got != wantHeight {
			t.Fatalf("renderScanPolicyModal() height at focus %d = %d, want %d (identical across every focus position, no reflow)", focus, got, wantHeight)
		}
	}
}

// TestRenderScanPolicyModalIsASeparateSurfaceFromTrivyConfigModal is the
// Phase 8 task 8.3 regression guard (spec's explicit "separate surface"
// scenario): the policy fields must not appear when trivyConfigModal is
// rendered standalone, and trivyConfigModal's fields must not appear in the
// policy modal's own output.
func TestRenderScanPolicyModalIsASeparateSurfaceFromTrivyConfigModal(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	trivyOutput := renderTrivyConfigModal(theme, trivyConfigModal{
		Open: true, ScheduleEnabled: true, Interval: "1h", Timeout: "30s",
		RegistryReachableURL: "https://registry.example.com", MaxConcurrency: "4",
	})
	for _, forbidden := range []string{"Vulnerability Policy", "Severity Threshold"} {
		if strings.Contains(trivyOutput, forbidden) {
			t.Fatalf("renderTrivyConfigModal() output contains %q, want the policy modal to be a separate surface\n%s", forbidden, trivyOutput)
		}
	}

	policyOutput := renderScanPolicyModal(theme, scanPolicyModal{Open: true, Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical})
	for _, forbidden := range []string{"Edit Trivy Configuration", "Registry Reachable URL", "Schedule Enabled"} {
		if strings.Contains(policyOutput, forbidden) {
			t.Fatalf("renderScanPolicyModal() output contains %q, want no Trivy config fields\n%s", forbidden, policyOutput)
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

	policyOutput := renderScanPolicyModal(theme, scanPolicyModal{Open: true, Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical})
	if strings.Contains(policyOutput, "Repository Override") {
		t.Fatalf("renderScanPolicyModal() output contains %q, want no override modal fields\n%s", "Repository Override", policyOutput)
	}
}

// TestRenderSigningPolicyModalFitsWithinRowBudget is the Phase 9 task 9.3
// RED test (design.md Decision 11 piece 1's row-budget table), updated for
// the signing-key-management change's trustedKeyList component (own
// heading row + one row per key + a nav-hint row when focused, replacing
// the retired single Trusted Key (PEM) input row + capped key-list rows),
// and further updated by signing-keyless-verification for the new
// trustedIdentityList component (own heading row + "No trusted identities
// configured." row when empty, +2 rows versus the pre-identities budget in
// every case below since all three cases configure zero identities).
func TestRenderSigningPolicyModalFitsWithinRowBudget(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name       string
		modal      signingPolicyModal
		wantHeight int
	}{
		{
			name:       "no error, 0 keys",
			modal:      signingPolicyModal{Open: true, Focus: signingPolicyFieldEnabled, Enabled: false, Keys: newTrustedKeyList("", nil)},
			wantHeight: 17,
		},
		{
			name:       "no error, 3 keys",
			modal:      signingPolicyModal{Open: true, Focus: signingPolicyFieldEnabled, Enabled: true, Keys: newTrustedKeyList("", []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"})},
			wantHeight: 19,
		},
		{
			name:       "error, 5 keys, focused (shows the add/delete hint row)",
			modal:      signingPolicyModal{Open: true, Focus: signingPolicyFieldAddKey, Enabled: true, Keys: newTrustedKeyList("", []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc", "dddddddddddd", "eeeeeeeeeeee"}), Error: "trusted_public_keys[0] is invalid"},
			wantHeight: 24,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderSigningPolicyModal(theme, tc.modal)
			if h := lipgloss.Height(got); h != tc.wantHeight {
				t.Fatalf("renderSigningPolicyModal() height = %d, want %d (design.md Decision 11's row budget)\n%s", h, tc.wantHeight, got)
			}
		})
	}
}

// TestRenderSigningPolicyModalNeverRendersRawPEM is the Phase 9 task 9.4 RED
// test: renderSigningPolicyModal only ever shows truncated SHA-256/12
// fingerprints for stored keys, never the raw PEM (design.md Decision 11
// piece 1: "the modal never has to display multi-line text either").
func TestRenderSigningPolicyModalNeverRendersRawPEM(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	rawPEM := "-----BEGIN PUBLIC KEY-----"
	storedKey := rawPEM + "\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAErSUZU4IUnh+IonVC8RB3NSN1lQLe\nxnF8AMhBjtuWIOnJjBgfpVuckOjRgdcXwUr/GtUnkVEeSGvmLC4F3sr/Cw==\n-----END PUBLIC KEY-----\n"
	wantFingerprint := signingKeyFingerprints([]string{storedKey})[0]
	modal := signingPolicyModal{Open: true, Enabled: true, Keys: newTrustedKeyList("", []string{storedKey})}

	got := renderSigningPolicyModal(theme, modal)
	if strings.Contains(got, rawPEM) {
		t.Fatalf("renderSigningPolicyModal() = %q, want no raw PEM markers, only fingerprints", got)
	}
	if !strings.Contains(got, wantFingerprint) {
		t.Fatalf("renderSigningPolicyModal() = %q, want the stored fingerprint %q shown", got, wantFingerprint)
	}
}

// TestRenderSigningPolicyModalShowsUnsignedSelfRead is the RED test for the
// UnsignedSelfRead TUI surface: the modal renders the field's label and its
// current cycled value, normalizing an unseeded "" to "off" rather than
// rendering a blank value row.
func TestRenderSigningPolicyModalShowsUnsignedSelfRead(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	unseeded := renderSigningPolicyModal(theme, signingPolicyModal{Open: true, Enabled: true})
	if !strings.Contains(unseeded, "Unsigned Self-Read") {
		t.Fatalf("renderSigningPolicyModal() = %q, want the Unsigned Self-Read label", unseeded)
	}
	if !strings.Contains(unseeded, "off") {
		t.Fatalf(`renderSigningPolicyModal() = %q, want "" to normalize to "off"`, unseeded)
	}

	seeded := renderSigningPolicyModal(theme, signingPolicyModal{Open: true, Enabled: true, UnsignedSelfRead: "repo_push"})
	if !strings.Contains(seeded, "repo_push") {
		t.Fatalf("renderSigningPolicyModal() = %q, want the current repo_push value shown", seeded)
	}
}

// TestRenderSigningPolicyModalIsASeparateSurfaceFromScanPolicyModal mirrors
// TestRenderRepositoryOverrideModalIsASeparateSurfaceFromOtherAdminModals:
// signingPolicyModal is its own sibling surface, not an extension of
// scanPolicyModal.
func TestRenderSigningPolicyModalIsASeparateSurfaceFromScanPolicyModal(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	signingOutput := renderSigningPolicyModal(theme, signingPolicyModal{Open: true, Enabled: true})
	for _, forbidden := range []string{"Vulnerability Policy", "Severity Threshold"} {
		if strings.Contains(signingOutput, forbidden) {
			t.Fatalf("renderSigningPolicyModal() output contains %q, want a separate surface\n%s", forbidden, signingOutput)
		}
	}

	policyOutput := renderScanPolicyModal(theme, scanPolicyModal{Open: true, Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical})
	if strings.Contains(policyOutput, "Signing Policy") {
		t.Fatalf("renderScanPolicyModal() output contains %q, want no signing modal fields\n%s", "Signing Policy", policyOutput)
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

// TestSigningPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph is the Phase 9
// task 9.6 RED test, mirroring
// TestRenderTrivyTabsPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph's
// shape: text-only, no icon/glyph, reflecting enabled/disabled and the
// trusted-key count.
func TestSigningPolicyBadgeTextReflectsStateAndUsesNoIconOrGlyph(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()

	tests := []struct {
		name   string
		policy ports.SigningPolicySettings
		want   string
	}{
		{name: "disabled", policy: ports.SigningPolicySettings{Enabled: false}, want: "Signing: OFF"},
		{name: "enabled, one key", policy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"key-one"}}, want: "Signing: REQUIRED (1 keys, 0 identities)"},
		{name: "enabled, two keys", policy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"key-one", "key-two"}}, want: "Signing: REQUIRED (2 keys, 0 identities)"},
		{name: "enabled, one key and one identity", policy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"key-one"}, TrustedIdentities: []ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}}, want: "Signing: REQUIRED (1 keys, 1 identities)"},
		{name: "enabled, identity-only", policy: ports.SigningPolicySettings{Enabled: true, TrustedIdentities: []ports.TrustedIdentity{{CertificateIdentityRegexp: "^valid$", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"}}}, want: "Signing: REQUIRED (0 keys, 1 identities)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := signingPolicyBadge(theme, tc.policy)
			if !strings.Contains(ansi.Strip(got), tc.want) {
				t.Fatalf("signingPolicyBadge() = %q, want it to contain %q", ansi.Strip(got), tc.want)
			}
			for _, r := range ansi.Strip(got) {
				if r > 126 {
					t.Fatalf("signingPolicyBadge() contains non-ASCII rune %q (%U), want text only, no icon or glyph\n%s", r, r, ansi.Strip(got))
				}
			}
		})
	}
}

// TestFeaturePageHeadingComposesSigningBadgeAtZeroRowCostForSigningOnly is
// the Phase 9 task 9.7 RED test (design.md Decision 11 piece 1's exact
// heading-composition snippet): the badge is composed onto the existing
// "Feature Page" heading line only when the selected feature is signing,
// and adds zero rows versus the heading without it.
func TestFeaturePageHeadingComposesSigningBadgeAtZeroRowCostForSigningOnly(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	env := screenEnv{Layout: contentBudget(150, 24, "", "")}

	// gitleaksConfigScreen is the fair "no badge" baseline (Phase 11: both
	// screens now render their own independent "Feature Page" heading, so
	// the only variable between them is the badge itself).
	baseline := gitleaksConfigScreen{loaded: true, page: ports.FeaturePage{Summary: ports.FeatureSummary{Name: gitleaksFeatureName}}}.View(theme, env).Body

	withBadge := signingConfigScreen{
		loaded: true,
		page:   ports.FeaturePage{Summary: ports.FeatureSummary{Name: signingFeatureName}},
		policy: ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{"key-one"}},
	}.View(theme, env).Body

	if !strings.Contains(ansi.Strip(withBadge), "Signing: REQUIRED (1 keys, 0 identities)") {
		t.Fatalf("signingConfigScreen.View() = %q, want the signing badge composed onto the heading", ansi.Strip(withBadge))
	}
	if lipgloss.Height(withBadge) != lipgloss.Height(baseline) {
		t.Fatalf("signingConfigScreen.View() height = %d, want %d (badge composed at zero row cost)\n%s", lipgloss.Height(withBadge), lipgloss.Height(baseline), withBadge)
	}

	nonSigning := gitleaksConfigScreen{
		loaded: true,
		page:   ports.FeaturePage{Summary: ports.FeatureSummary{Name: gitleaksFeatureName}},
	}.View(theme, env).Body
	if strings.Contains(nonSigning, "Signing:") {
		t.Fatalf("gitleaksConfigScreen.View() = %q, want no signing badge for a non-signing feature", nonSigning)
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

// TestAdminScanHistoryModalTabBarHighlightsActiveTabAndListsAllTabs is the
// Phase 2 task 2.4 RED test: the tab bar lists every tab in order and
// highlights only the active one (design.md "Ordered tab slice with a
// wrapping cursor").
func TestAdminScanHistoryModalTabBarHighlightsActiveTabAndListsAllTabs(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := adminScanHistoryModal{Tabs: newAdminScanHistoryTabs(), ActiveTab: 1}

	got := adminScanHistoryModalTabBar(theme, modal)
	if !strings.Contains(got, "Vulnerabilities") {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want it to contain %q", got, "Vulnerabilities")
	}
	if !strings.Contains(got, "Leaks") {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want it to contain %q", got, "Leaks")
	}

	activeLabel := theme.selected.Render("Leaks")
	if !strings.Contains(got, activeLabel) {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want the active tab (Leaks, index 1) styled with theme.selected", got)
	}
	inactiveLabel := theme.selected.Render("Vulnerabilities")
	if strings.Contains(got, inactiveLabel) {
		t.Fatalf("adminScanHistoryModalTabBar() = %q, want only the active tab styled with theme.selected", got)
	}
}

// TestAdminScanHistoryModalFooterReportsPositionDateAndInProgressMarker is
// the Phase 2 task 2.4 RED test (design.md "the TUI SHALL show 2/17, the
// run's date"): no runs shows a zero position, and a populated cursor shows
// 1-based position, total count, and the in-progress marker when applicable.
func TestAdminScanHistoryModalFooterReportsPositionDateAndInProgressMarker(t *testing.T) {
	t.Parallel()

	empty := adminScanHistoryModal{}
	if got, want := adminScanHistoryModalFooter(empty), "Execution 0/0"; got != want {
		t.Fatalf("adminScanHistoryModalFooter() = %q, want %q", got, want)
	}

	finished := time.Date(2026, 4, 1, 8, 15, 0, 0, time.UTC)
	created := time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)
	modal := adminScanHistoryModal{
		Runs: []ports.ScanRun{
			{ID: "run-1", FinishedAt: &finished, CreatedAt: finished},
			{ID: "run-2", FinishedAt: nil, CreatedAt: created},
			{ID: "run-3", FinishedAt: &finished, CreatedAt: finished},
		},
		Cursor: 1,
	}

	got := adminScanHistoryModalFooter(modal)
	want := fmt.Sprintf("Execution 2/3 — %s (in progress)", created.UTC().Format("2006-01-02 15:04"))
	if got != want {
		t.Fatalf("adminScanHistoryModalFooter() = %q, want %q", got, want)
	}
}

// TestAdminScanHistoryModalTableBodySubstitutesSingleLineWhenBudgetTooSmall
// is the Phase 2 task 2.3/2.4 RED test (design.md "the table is replaced by
// a single-line substitute, never sliced" — the fix for the historical
// orphaned "Showing x-y of N" bug): when tableBudget cannot hold a bordered
// table at all, the modal never returns a bordered table body, it returns
// one substitute line instead.
func TestAdminScanHistoryModalTableBodySubstitutesSingleLineWhenBudgetTooSmall(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := adminScanHistoryModal{
		Tabs:      newAdminScanHistoryTabs(),
		ActiveTab: 0,
		Detail:    ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	got := adminScanHistoryModalTableBody(theme, modal, bubbletable.Model{}, bubbletable.Model{}, adminScanHistoryModalMinTableBudget-1)
	if lipgloss.Height(got) != 1 {
		t.Fatalf("adminScanHistoryModalTableBody() height = %d, want exactly 1 (single-line substitute, never a sliced bordered table)", lipgloss.Height(got))
	}
	if !strings.Contains(got, "too small") {
		t.Fatalf("adminScanHistoryModalTableBody() = %q, want a message explaining the terminal is too small", got)
	}
}

// TestRenderAdminScanHistoryModalChromeLinesAreSingleLine is the Phase 2
// task 2.4 RED test (design.md "Modal chrome accounting": title, tab bar,
// footer, and help are each guarded single-row), mirroring
// TestViewportChromeInvariantTitleContextHelpAreSingleLine's convention for
// the base screen's chrome.
func TestRenderAdminScanHistoryModalChromeLinesAreSingleLine(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	created := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		ActiveTab:  1,
		Runs:       []ports.ScanRun{{ID: "run-1", CreatedAt: created}},
		Cursor:     0,
	}

	assertSingleLine := func(t *testing.T, label, rendered string) {
		t.Helper()
		if got := lipgloss.Height(rendered); got != 1 {
			t.Fatalf("%s height = %d, want exactly 1 (modal chrome invariant)", label, got)
		}
	}

	assertSingleLine(t, "title", theme.subheading.Render(fmt.Sprintf("Scan History — %s", adminFirstNonEmpty(modal.Repository, "unknown"))))
	assertSingleLine(t, "tab bar", adminScanHistoryModalTabBar(theme, modal))
	assertSingleLine(t, "footer", theme.muted.Render(adminScanHistoryModalFooter(modal)))
	assertSingleLine(t, "help", theme.help.Render("Tab/Shift+Tab: tabs | Left/Right: history | Up/Down: select | Enter/click: open | Esc: close"))
}

// TestRenderAdminScanHistoryModalRendersExecutionsColumnWithCursorHighlighted
// is the RED test for the executions side panel (executions side panel
// feature): the modal renders a left-hand executions rail alongside the
// existing right column, listing compact per-run labels with the currently
// navigated run (modal.Cursor) highlighted the same way
// adminScanHistoryModalTabBar highlights the active tab.
func TestRenderAdminScanHistoryModalRendersExecutionsColumnWithCursorHighlighted(t *testing.T) {
	// Not t.Parallel(): forces the global lipgloss color profile so
	// theme.selected actually emits an ANSI sequence to assert on, following
	// TestNewAdminBubbleTableAppliesThemeBorderForegroundColor's precedent
	// (go test runs with no tty, so lipgloss otherwise auto-detects "no
	// color" and theme.selected.Render would differ from plain text only by
	// its Padding(0,1), which is indistinguishable from this modal's own
	// unrelated column padding).
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	theme := newAdminTheme()
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	runs := make([]ports.ScanRun, 0, 5)
	for i := 0; i < 5; i++ {
		finished := base.AddDate(0, 0, i)
		runs = append(runs, ports.ScanRun{ID: fmt.Sprintf("run-%d", i), FinishedAt: &finished})
	}
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       runs,
		Cursor:     2,
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	findingsTable := buildAdminFindingsTable(modal.Detail.Findings, 0, minTableRows)

	got := renderAdminScanHistoryModal(theme, modal, findingsTable, bubbletable.Model{}, 30)

	if !strings.Contains(got, "Executions") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the executions rail heading present", got)
	}
	for i, run := range runs {
		label := adminScanHistoryModalExecutionRowLabel(i, run)
		if !strings.Contains(got, label) {
			t.Fatalf("renderAdminScanHistoryModal() = %q, want it to contain run label %q", got, label)
		}
	}

	cursorLabel := adminScanHistoryModalExecutionRowLabel(modal.Cursor, runs[modal.Cursor])
	highlighted := theme.selected.Render(cursorLabel)
	if !strings.Contains(got, highlighted) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the cursor run label %q styled with theme.selected", got, cursorLabel)
	}
	otherLabel := adminScanHistoryModalExecutionRowLabel(0, runs[0])
	otherHighlighted := theme.selected.Render(otherLabel)
	if strings.Contains(got, otherHighlighted) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want only the cursor run styled with theme.selected", got)
	}
}

// TestRenderAdminScanHistoryModalExecutionsColumnIsAScrollableWindowNotFullList
// is the RED test guarding against unconditionally rendering all
// adminScanHistoryWindowLimit (50) runs in the executions rail: with a tight
// modal row budget and the cursor near the start, a run label far past the
// visible window (near the end of 50) must not appear.
func TestRenderAdminScanHistoryModalExecutionsColumnIsAScrollableWindowNotFullList(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runs := make([]ports.ScanRun, 0, adminScanHistoryWindowLimit)
	for i := 0; i < adminScanHistoryWindowLimit; i++ {
		finished := base.AddDate(0, 0, i)
		runs = append(runs, ports.ScanRun{ID: fmt.Sprintf("run-%d", i), FinishedAt: &finished})
	}
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       runs,
		Cursor:     0,
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	findingsTable := buildAdminFindingsTable(modal.Detail.Findings, 0, minTableRows)

	got := renderAdminScanHistoryModal(theme, modal, findingsTable, bubbletable.Model{}, adminScanHistoryModalMinRows)

	lastLabel := adminScanHistoryModalExecutionRowLabel(adminScanHistoryWindowLimit-1, runs[adminScanHistoryWindowLimit-1])
	if strings.Contains(got, lastLabel) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the executions rail to be a bounded scrollable window, not the full %d-run list (last run's label %q should not be visible while cursor is at 0 with a tight row budget)", got, adminScanHistoryWindowLimit, lastLabel)
	}
	firstLabel := adminScanHistoryModalExecutionRowLabel(0, runs[0])
	if !strings.Contains(got, firstLabel) {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the first run's label %q visible since cursor is at 0", got, firstLabel)
	}
}

// TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite is the
// Phase 2 task 2.3 RED test (design.md "No fitLines over a composite
// containing a bordered block" — the bug that produced an orphaned "Showing
// x-y of N" line with no table above it): renderAdminScanHistoryModal never
// slices its composed content through fitLines/renderSection. The active
// tab's own bordered table keeps its own pagination footer untouched, and
// no outer "Showing " indicator ever appears in the output.
// TestAdminScanHistoryModalTableBodyShowsEmptyLeaksStateAndKeepsTabVisible is
// the sdd-verify CRITICAL-1 remediation test (spec.md "Secret Findings
// Surface" — "No secret scan for the navigated execution stays visible"):
// when the navigated execution's digest has no secret scan (modal.Secrets is
// empty) and the operator is on the Leaks tab, the modal MUST show an
// explicit empty-state message rather than a blank or crashing body, and the
// Leaks tab MUST stay present in the tab bar rather than being hidden or
// removed.
func TestAdminScanHistoryModalTableBodyShowsEmptyLeaksStateAndKeepsTabVisible(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		ActiveTab:  1, // Leaks tab
		Runs:       []ports.ScanRun{{ID: "run-1", CreatedAt: time.Now()}},
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
		Secrets:    nil, // no secret scan recorded for this execution's digest
	}
	body := adminScanHistoryModalTableBody(theme, modal, bubbletable.Model{}, bubbletable.Model{}, adminScanHistoryModalMinTableBudget)
	if !strings.Contains(body, "No secret findings recorded for this execution.") {
		t.Fatalf("adminScanHistoryModalTableBody() = %q, want the explicit empty-state message when modal.Secrets is empty", body)
	}

	got := renderAdminScanHistoryModal(theme, modal, bubbletable.Model{}, bubbletable.Model{}, 20)
	if !strings.Contains(got, "No secret findings recorded for this execution.") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the explicit empty-state message on the Leaks tab", got)
	}
	if !strings.Contains(got, "Leaks") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the Leaks tab to stay present in the tab bar, never hidden", got)
	}
	if !strings.Contains(got, "Vulnerabilities") {
		t.Fatalf("renderAdminScanHistoryModal() = %q, want the Vulnerabilities tab to remain in the tab bar alongside Leaks", got)
	}
}

// TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen is
// the RED test for the reported regression (claude-handoff.md: "The modal
// must be rendered above the Feature Page ... and must not be appended
// below the page content. The underlying Features page must remain visible
// behind the modal and must not reflow when it opens"). It proves two
// distinct properties the pre-existing suite did not cover:
//
//  1. The base page renders at its FULL, unshrunk size when the modal is
//     open -- identical to its own standalone render at the very same
//     layout, not a row-split-reduced one.
//  2. The modal is composited ON TOP of the base (result height ==
//     layout.Height, the canvas), never appended below it as a trailing
//     block (which would grow the result past the canvas height).
func TestRenderAdminWorkspaceKeepsBaseFullSizeAndLayersModalOnTopWhenOpen(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	view := AdminViewState{}
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       []ports.ScanRun{{ID: "run-1", Repository: "acme/api", CreatedAt: now}},
		Detail:     ports.ScanRunDetail{Findings: []ports.ScanRunFinding{{VulnerabilityID: "CVE-1"}}},
	}
	findingsTable := buildAdminFindingsTable(modal.Detail.Findings, 0, minTableRows)
	screens := adminScreenSet{}
	screens[slotScanHistory] = scanHistoryScreen{returnTo: screenAdminFeatures, modal: modal, findings: findingsTable}

	layout := contentBudget(defaultViewportWidth, defaultViewportHeight, "", adminScreenHelp(screenAdminFeatures, view))

	// Property 1: the base page (rendered independently, at the very same
	// unreduced layout the modal-open path is given) must be byte-identical
	// to what renderAdminWorkspace's modal-open branch composites as its
	// base -- proving it is never shrunk via a reduced SectionRows the way
	// the superseded row-split design did.
	standaloneContext, standaloneBody, standaloneHelp := renderAdminScreen(theme, screenAdminFeatures, session, view, nil, layout, now)
	wantBase := renderConsoleWorkspace("Regixtry Admin", standaloneContext, standaloneBody, "", standaloneHelp, statusKindAuto)

	got := renderAdminWorkspace(screenAdminFeatures, session, view, nil, "", layout, now, screens)

	// Property 2: layered on top, not appended below -- the composite must
	// fit exactly within the canvas (layout.Width x layout.Height), never
	// grow past it the way lipgloss.JoinVertical(base, modal) would once
	// base is rendered at its full size AND the modal is rendered on top of
	// it (base+modal stacked would be far taller than one canvas).
	if h := lipgloss.Height(got); h != layout.Height {
		t.Fatalf("renderAdminWorkspace() height = %d, want exactly layout.Height(%d) -- the modal must be composited on top of the base, never appended below it", h, layout.Height)
	}

	// The base's own bottom-of-screen marker (only present once the full,
	// unshrunk base is rendered) must survive in the composite.
	if !strings.Contains(wantBase, "Operator: operator") {
		t.Fatalf("test setup invalid: standalone base render = %q, want it to contain the Operator marker", wantBase)
	}
	if !strings.Contains(got, "Operator: operator") {
		t.Fatalf("renderAdminWorkspace() = %q, want the base page's full content (Operator marker) to survive unshrunk when the modal opens", got)
	}

	// The modal's own title must be present, proving it was actually
	// rendered and composited, not silently dropped.
	if !strings.Contains(got, "Scan History — acme/api") {
		t.Fatalf("renderAdminWorkspace() = %q, want the modal title present", got)
	}
}

// TestRenderAdminWorkspaceLeavesVisibleMarginAroundModalWhenOpen is the
// permanent, real-render replacement for the throwaway visual-debug test
// used to find this regression: it proves the actual end-to-end
// renderAdminWorkspace output -- not just compositeOverlay's synthetic unit
// tests -- has a real blank gap between the modal's own left/right border
// and whatever base content survives beside it, at the modal's own
// vertical midpoint. Without this, a modal that happens to sit beside a
// wide, unshrunk base table would read as corruption (a base border
// character fused directly against the modal's edge) rather than a
// floating dialog.
func TestRenderAdminWorkspaceLeavesVisibleMarginAroundModalWhenOpen(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	view := AdminViewState{}
	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "web-dvwa",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       []ports.ScanRun{{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}},
		Detail: ports.ScanRunDetail{Findings: []ports.ScanRunFinding{
			{VulnerabilityID: "CVE-2016-9841", Severity: "CRITICAL", PackageName: "rsync", Fixable: true},
			{VulnerabilityID: "CVE-2017-12424", Severity: "CRITICAL", PackageName: "login", Fixable: true},
		}},
	}
	findingsTable := buildAdminFindingsTable(modal.Detail.Findings, 0, minTableRows)

	layout := contentBudget(defaultViewportWidth, defaultViewportHeight, "", "")

	// Phase 11: the Repository Alerts summary table (the wide, dense base
	// content this test needs to stress-test overlay margins against) now
	// lives on trivyReposScreen, a migrated top-level screen -- mounted here
	// exactly like production (renderAdminWorkspace's slotFor branch), with
	// screenSecurityTrivyRepos as the current screen. Two rows (matching a
	// real captured repro), not one, so the table is actually dense enough
	// at the modal's own row range to make this a genuine regression guard:
	// a sparse single-row fixture leaves enough natural blank space that
	// the margin assertions below would pass even without the fix.
	repos := trivyReposScreen{
		loaded: true,
		summaries: []repositorySummary{
			{Repository: "web-dvwa", LatestRun: ports.ScanRun{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}, LastExecuted: now, RunCount: 13},
			{Repository: "alpine-vuln", LatestRun: ports.ScanRun{ID: "run-2", Repository: "alpine-vuln", CreatedAt: now}, LastExecuted: now, RunCount: 12},
		},
	}
	repos.rebuildTable(screenEnv{Layout: layout})
	screens := adminScreenSet{}
	screens[slotTrivyRepos] = repos
	screens[slotScanHistory] = scanHistoryScreen{returnTo: screenSecurityTrivyRepos, modal: modal, findings: findingsTable}

	// Compute the modal's own footprint the exact same way
	// renderAdminWorkspace does, so this test does not hardcode numbers that
	// would silently drift out of sync with the production sizing.
	standaloneFrame := repos.View(theme, screenEnv{Layout: layout})
	standaloneHelp := shortHelpView(theme, repos.Keys())
	standaloneBaseWorkspace := renderConsoleWorkspace("Regixtry Admin", standaloneFrame.Context, standaloneFrame.Body, "", standaloneHelp, statusKindAuto)
	modalView := renderAdminScanHistoryModal(theme, modal, findingsTable, bubbletable.Model{}, adminScanHistoryModalRows(layout, lipgloss.Height(standaloneFrame.Body)))
	overlayWidth := lipgloss.Width(modalView)
	overlayHeight := lipgloss.Height(modalView)
	x := (layout.Width - overlayWidth) / 2
	// y mirrors compositeOverlay's own centering formula exactly: anchored to
	// the base workspace's own actual rendered height, not the raw
	// layout.Height canvas (admin_overlay.go's compositeOverlay doc comment).
	baseHeight := lipgloss.Height(standaloneBaseWorkspace)
	if baseHeight > layout.Height {
		baseHeight = layout.Height
	}
	y := (baseHeight - overlayHeight) / 2
	if y < 0 {
		y = 0
	}

	got := renderAdminWorkspace(screenSecurityTrivyRepos, session, view, nil, "", layout, now, screens)
	lines := strings.Split(ansi.Strip(got), "\n")

	// Check every row of the modal's own footprint, not just the midpoint:
	// the real captured regression showed the fused-border defect on
	// several (not all) of the modal's rows, wherever the base's own
	// content happened to be dense at that exact row.
	for row := y; row < y+overlayHeight; row++ {
		if row < 0 || row >= len(lines) {
			t.Fatalf("test setup invalid: modal row %d out of range (got %d lines)", row, len(lines))
		}
		runes := []rune(lines[row])
		for m := 1; m <= overlayHorizontalMargin; m++ {
			if leftCol := x - m; leftCol >= 0 && leftCol < len(runes) {
				if runes[leftCol] != ' ' {
					t.Fatalf("renderAdminWorkspace() row %d col %d = %q, want a blank margin column left of the modal (base content directly touching the modal's own edge)", row, leftCol, string(runes[leftCol]))
				}
			}
			if rightCol := x + overlayWidth + m - 1; rightCol >= 0 && rightCol < len(runes) {
				if runes[rightCol] != ' ' {
					t.Fatalf("renderAdminWorkspace() row %d col %d = %q, want a blank margin column right of the modal", row, rightCol, string(runes[rightCol]))
				}
			}
		}
	}
}

// TestRenderAdminWorkspaceModalNeverExtendsPastBaseBodysOwnBottomBorder is the
// RED regression test for the height-budget bug found by real-render visual
// inspection at realistic terminal heights (claude-handoff.md follow-up).
// Two independent defects combined to produce it:
//
//  1. adminScanHistoryModalRows derived the modal's row budget purely from
//     l.Height, with no awareness of how tall the base Feature Page body
//     ACTUALLY renders. The base body is content-driven
//     (renderSection/fitLines only ever trims, never pads to fill
//     l.SectionRows), so it stays roughly constant height regardless of
//     terminal height, while the modal's budget kept scaling up with
//     l.Height.
//  2. Even with the modal's own SIZE correctly capped, compositeOverlay
//     centered it within the FULL raw terminal canvas rather than within the
//     base workspace's own actual rendered footprint -- which is
//     mathematically incapable of keeping ANY overlay (down to 1 row) inside
//     a ~29-row base footprint once the terminal exceeds roughly 60 rows, no
//     matter how tightly the budget is capped. This is why the assertion
//     below spans heights up to 80, not just the height (50) where the bug
//     was first confirmed by hand: a size-only fix passes at 24/30/40 by
//     coincidence but is provably unable to pass at 50/80.
//
// This asserts, at a realistic range of terminal heights including the one
// where the bug was confirmed absent-by-coincidence (24) and several where it
// reproduced (30, 40, 50, 80): the modal's own composited bottom row (its
// centered offset, anchored to the base workspace's own rendered height, plus
// its own rendered height) never lands past the base body's own composited
// bottom row (title + context + the base body's actual measured height) --
// computed via lipgloss.Height bookkeeping on the real production render
// functions, not guessed or hardcoded, and not string-scanned for border
// glyphs (fragile once ANSI styling is involved).
func TestRenderAdminWorkspaceModalNeverExtendsPastBaseBodysOwnBottomBorder(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)

	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "web-dvwa",
		Tabs:       newAdminScanHistoryTabs(),
		Runs:       []ports.ScanRun{{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}},
		Detail: ports.ScanRunDetail{Findings: []ports.ScanRunFinding{
			{VulnerabilityID: "CVE-2016-9841", Severity: "CRITICAL", PackageName: "rsync", Fixable: true},
			{VulnerabilityID: "CVE-2017-12424", Severity: "CRITICAL", PackageName: "login", Fixable: true},
		}},
	}
	findingsTable := buildAdminFindingsTable(modal.Detail.Findings, 0, minTableRows)

	// Phase 11: the Repository Alerts summary table (realistic content,
	// well under 30 rows total, so the base body renders at its natural,
	// short, content-driven height instead of being clipped/padded to fill
	// the terminal -- design.md: renderSection only ever TRIMS content
	// longer than the budget, never stretches shorter content to fill it)
	// now lives on trivyReposScreen, mounted exactly like production.
	summaries := []repositorySummary{
		{Repository: "web-dvwa", LatestRun: ports.ScanRun{ID: "run-1", Repository: "web-dvwa", CreatedAt: now}, LastExecuted: now, RunCount: 13},
		{Repository: "alpine-vuln", LatestRun: ports.ScanRun{ID: "run-2", Repository: "alpine-vuln", CreatedAt: now}, LastExecuted: now, RunCount: 12},
	}

	for _, height := range []int{24, 30, 40, 50, 80} {
		height := height
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			t.Parallel()

			layout := contentBudget(minViewportWidth, height, "", "")

			repos := trivyReposScreen{loaded: true, summaries: summaries}
			repos.rebuildTable(screenEnv{Layout: layout})

			// The base body's own bottom row within the composited canvas:
			// title (1 row, guarded invariant) + context (1 row) + the base
			// body's real measured height, ending at the body's own closing
			// border row.
			baseFrame := repos.View(theme, screenEnv{Layout: layout})
			baseHelp := shortHelpView(theme, repos.Keys())
			baseBody := baseFrame.Body
			baseBodyHeight := lipgloss.Height(baseBody)
			baseBottomRow := 2 + baseBodyHeight - 1
			baseWorkspace := renderConsoleWorkspace("Regixtry Admin", baseFrame.Context, baseBody, "", baseHelp, statusKindAuto)

			modalRows := adminScanHistoryModalRows(layout, baseBodyHeight)
			modalView := renderAdminScanHistoryModal(theme, modal, findingsTable, bubbletable.Model{}, modalRows)

			overlayHeight := lipgloss.Height(modalView)
			if overlayHeight > layout.Height {
				overlayHeight = layout.Height
			}
			// Mirrors compositeOverlay's own centering formula exactly (see
			// admin_overlay.go's compositeOverlay and this codebase's existing
			// admin_overlay_test.go idiom of recomputing it inline rather than
			// guessing a number): anchored to the base WORKSPACE's own actual
			// rendered height, not the raw layout.Height canvas.
			workspaceHeight := lipgloss.Height(baseWorkspace)
			if workspaceHeight > layout.Height {
				workspaceHeight = layout.Height
			}
			y := (workspaceHeight - overlayHeight) / 2
			if y < 0 {
				y = 0
			}
			modalBottomRow := y + overlayHeight - 1

			if modalBottomRow > baseBottomRow {
				t.Fatalf("height=%d: modal's composited bottom row = %d, want <= %d (the base Feature Page body's own bottom border row) -- the modal renders below where the base page's own bordered box closes, floating in blank canvas instead of over the base page (modalRows=%d, baseBodyHeight=%d, overlayHeight=%d, y=%d)",
					height, modalBottomRow, baseBottomRow, modalRows, baseBodyHeight, overlayHeight, y)
			}
		})
	}
}

func TestRenderAdminScanHistoryModalNeverAppliesFitLinesOverComposite(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	findings := make([]ports.ScanRunFinding, 0, 30)
	for i := 0; i < 30; i++ {
		findings = append(findings, ports.ScanRunFinding{Severity: "HIGH", VulnerabilityID: fmt.Sprintf("CVE-2026-%04d", i), PackageName: "openssl"})
	}
	const pageSize = 5 // 30 findings over pageSize 5 -> 6 pages, forces the table's own internal pagination

	findingsTable := buildAdminFindingsTable(findings, 0, pageSize)

	modal := adminScanHistoryModal{
		Open:       true,
		Repository: "acme/api",
		Tabs:       newAdminScanHistoryTabs(),
		ActiveTab:  0,
		Runs:       []ports.ScanRun{{ID: "run-1", CreatedAt: time.Now()}},
		Detail:     ports.ScanRunDetail{Findings: findings},
	}

	got := renderAdminScanHistoryModal(theme, modal, findingsTable, bubbletable.Model{}, 20)

	if strings.Contains(got, "Showing ") {
		t.Fatalf("renderAdminScanHistoryModal() output contains an outer fitLines indicator (\"Showing \" substring) — the modal must never apply fitLines over a composite containing a bordered table:\n%s", got)
	}
	if !strings.Contains(got, "1/6") {
		t.Fatalf("renderAdminScanHistoryModal() output = %q, want the Findings table's own pagination footer (1/6) present and untouched", got)
	}
}

// TestRenderRepoAdminGrantsScreenShowsOperatorsOwnRepositoryGrants is the
// Phase 3 task 3.5 RED test (design.md Decision 7 / spec.md "Delegate sees
// only their own repositories' grants"): the repo-admin delegate's grants
// screen renders exactly the repository it was opened for, and every
// username/role pair it was handed — never a user-directory-style listing.
func TestRenderRepoAdminGrantsScreenShowsOperatorsOwnRepositoryGrants(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	view := AdminViewState{
		RepoAdminRepository: "team/app",
		RepoAdminGrants: []ports.AdminRepositoryGrant{
			{Username: "bob", Role: domainauth.RepoRoleWriter},
			{Username: "carol", Role: domainauth.RepoRoleReader},
		},
	}

	got := renderRepoAdminGrantsScreen(theme, view)

	if !strings.Contains(got, "team/app") {
		t.Fatalf("renderRepoAdminGrantsScreen() = %q, want the repository heading", got)
	}
	if !strings.Contains(got, "bob") || !strings.Contains(got, string(domainauth.RepoRoleWriter)) {
		t.Fatalf("renderRepoAdminGrantsScreen() = %q, want bob's repo-writer grant listed", got)
	}
	if !strings.Contains(got, "carol") || !strings.Contains(got, string(domainauth.RepoRoleReader)) {
		t.Fatalf("renderRepoAdminGrantsScreen() = %q, want carol's repo-reader grant listed", got)
	}
}

// TestRenderRepoAdminGrantsScreenShowsEmptyStateWithNoGrants triangulates
// the populated-grants case above with an empty repository.
func TestRenderRepoAdminGrantsScreenShowsEmptyStateWithNoGrants(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	view := AdminViewState{RepoAdminRepository: "team/app"}

	got := renderRepoAdminGrantsScreen(theme, view)

	if !strings.Contains(got, "team/app") {
		t.Fatalf("renderRepoAdminGrantsScreen() = %q, want the repository heading even with no grants", got)
	}
	if !strings.Contains(got, "No") {
		t.Fatalf("renderRepoAdminGrantsScreen() = %q, want an explicit empty-grants message", got)
	}
}

// TestRenderRepoAdminAddGrantScreenRoleFieldNeverRendersRepoAdmin is the
// Phase 3 task 3.5 RED test's second half (spec.md "Delegate cannot select
// repo-admin in the grant role picker"): whatever role the form currently
// holds, the rendered field text never shows "repo-admin".
func TestRenderRepoAdminAddGrantScreenRoleFieldNeverRendersRepoAdmin(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	for _, role := range []domainauth.RepoRole{domainauth.RepoRoleReader, domainauth.RepoRoleWriter} {
		view := AdminViewState{RepoAdminRepository: "team/app", RepoAdminGrantForm: adminRepositoryGrantForm{Role: role}}
		got := renderRepoAdminAddGrantScreen(theme, view)
		if strings.Contains(got, string(domainauth.RepoRoleAdmin)) {
			t.Fatalf("renderRepoAdminAddGrantScreen() = %q, role field must never render %q", got, domainauth.RepoRoleAdmin)
		}
		if !strings.Contains(got, string(role)) {
			t.Fatalf("renderRepoAdminAddGrantScreen() = %q, want the current role %q rendered", got, role)
		}
	}
}

// TestNextDelegateGrantRoleNeverProducesRepoAdmin is the pure-function proof
// behind the UI constraint above: this is the ONLY function allowed to
// mutate RepoAdminGrantForm.Role from a keypress (Space), so it alone must
// guarantee repo-admin is structurally unreachable — including from the
// (otherwise impossible) RepoRoleAdmin starting state.
// TestRenderAdminRobotsScreenListsRobotsWithRepositoryRoleAndState is task
// 5.3's first RED test (design.md Decision 7): screenAdminRobots mirrors
// renderAdminUsersScreen's list shape -- one line per robot, the selected
// row highlighted, an Operator/session-remaining footer -- but keyed by
// repository/role/enabled state instead of admin/read-only flags.
func TestRenderAdminRobotsScreenListsRobotsWithRepositoryRoleAndState(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	session := AdminSession{Username: "operator"}
	view := AdminViewState{
		Robots: []ports.AdminRobot{
			{ID: "1", Username: "robot$ci", Repository: "team/app", Role: domainauth.RepoRoleWriter, Enabled: true},
			{ID: "2", Username: "robot$audit", Repository: "team/other", Role: domainauth.RepoRoleReader, Enabled: false},
		},
		SelectedRobot: 0,
	}
	layout := consoleLayout{Width: defaultViewportWidth, Height: defaultViewportHeight, SectionRows: 20}

	got := renderAdminRobotsScreen(theme, session, view, layout, time.Now())

	if !strings.Contains(got, "robot$ci") || !strings.Contains(got, "team/app") || !strings.Contains(got, string(domainauth.RepoRoleWriter)) || !strings.Contains(got, "enabled") {
		t.Fatalf("renderAdminRobotsScreen() = %q, want robot$ci's repository/role/enabled state listed", got)
	}
	if !strings.Contains(got, "robot$audit") || !strings.Contains(got, "team/other") || !strings.Contains(got, string(domainauth.RepoRoleReader)) || !strings.Contains(got, "disabled") {
		t.Fatalf("renderAdminRobotsScreen() = %q, want robot$audit's repository/role/disabled state listed", got)
	}
	if !strings.Contains(got, "operator") {
		t.Fatalf("renderAdminRobotsScreen() = %q, want the operator footer", got)
	}
}

// TestRenderAdminRobotsScreenShowsEmptyStateWithNoRobots triangulates the
// populated case above with an empty robot list.
func TestRenderAdminRobotsScreenShowsEmptyStateWithNoRobots(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	session := AdminSession{Username: "operator"}
	layout := consoleLayout{Width: defaultViewportWidth, Height: defaultViewportHeight, SectionRows: 20}

	got := renderAdminRobotsScreen(theme, session, AdminViewState{}, layout, time.Now())

	if !strings.Contains(got, "No robot") {
		t.Fatalf("renderAdminRobotsScreen() = %q, want an explicit empty-robots message", got)
	}
}

// TestRenderAdminCreateRobotScreenShowsFormFields is task 5.3's third RED
// test: screenAdminCreateRobot renders every form field's current value.
func TestRenderCreateAdminRobotScreenShowsFormFields(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	view := AdminViewState{
		CreateRobotForm: adminCreateRobotForm{
			Name:       "ci",
			Repository: "team/app",
			Role:       domainauth.RepoRoleWriter,
			TTLSeconds: "604800",
		},
	}

	got := renderAdminCreateRobotScreen(theme, view, nil)

	for _, want := range []string{"ci", "team/app", string(domainauth.RepoRoleWriter), "604800"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderAdminCreateRobotScreen() = %q, want it to contain %q", got, want)
		}
	}
}

// TestRenderAdminCreateRobotScreenShowsOneTimeSecretWhenRevealed is task
// 5.3's CRITICAL RED test (spec.md "Operator manages a robot account end to
// end": "the TUI ... shows the one-time token secret"): immediately after a
// successful creation, RevealedTokenSecret/Accessor are populated on
// AdminViewState (the exact same fields renderAdminTokensScreen already
// reveals once for human admin tokens -- design.md Decision 6 reuses the
// token machinery unchanged), and screenAdminCreateRobot must display them.
func TestRenderCreateAdminRobotScreenShowsOneTimeSecretWhenRevealed(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	view := AdminViewState{
		RevealedTokenSecret:   "s3cr3t-value",
		RevealedTokenAccessor: "accessor-123",
	}

	got := renderAdminCreateRobotScreen(theme, view, nil)

	if !strings.Contains(got, "s3cr3t-value") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, want the one-time secret rendered", got)
	}
	if !strings.Contains(got, "accessor-123") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, want the accessor rendered", got)
	}
}

// TestRenderAdminCreateRobotScreenNeverShowsSecretBlockWhenNotRevealed
// triangulates the reveal case above: with no secret populated (the normal
// state before a creation, and after clearRevealedAdminToken runs), no
// secret material or accessor placeholder is rendered at all -- proving the
// block is conditional, not merely blank-value formatting that would leave
// an empty label lying around.
func TestRenderCreateAdminRobotScreenNeverShowsSecretBlockWhenNotRevealed(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	got := renderAdminCreateRobotScreen(theme, AdminViewState{}, nil)

	if strings.Contains(got, "One-time secret") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, must not render the secret block when nothing was revealed", got)
	}
}

// TestRenderCreateAdminRobotScreenShowsRepositorySuggestionList is the manual
// RC follow-up's RED test: screenAdminCreateRobot must render a "Known
// Repositories" suggestion list, mirroring renderAdminAddGrantScreen's
// windowed, highlighted-selection rendering for the same filter/select
// behavior.
func TestRenderCreateAdminRobotScreenShowsRepositorySuggestionList(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	view := AdminViewState{
		CreateRobotForm: adminCreateRobotForm{
			Repository:           "tea",
			Focus:                adminCreateRobotFieldRepository,
			RepositorySuggestion: 1,
		},
	}
	knownRepositories := []string{"library/alpine", "team/demo", "team/backend", "ops/console"}

	got := renderAdminCreateRobotScreen(theme, view, knownRepositories)

	if !strings.Contains(got, "Known Repositories") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, want a known-repositories suggestion header", got)
	}
	if strings.Contains(got, "library/alpine") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, want non-matching repository hidden", got)
	}
	if !strings.Contains(got, "team/demo") || !strings.Contains(got, "team/backend") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, want filtered suggestions rendered", got)
	}
}

// TestRenderCreateAdminRobotScreenShowsEmptySuggestionMessage triangulates
// the suggestion-list case above: when no known repository matches the
// current filter, the screen shows an explicit empty-state message instead
// of an empty "Known Repositories" section.
func TestRenderCreateAdminRobotScreenShowsEmptySuggestionMessage(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	view := AdminViewState{
		CreateRobotForm: adminCreateRobotForm{Repository: "no-match", Focus: adminCreateRobotFieldRepository},
	}

	got := renderAdminCreateRobotScreen(theme, view, []string{"library/alpine", "team/demo"})

	if !strings.Contains(got, "No known repositories match the current filter.") {
		t.Fatalf("renderAdminCreateRobotScreen() = %q, want the empty-suggestions message", got)
	}
}

func TestNextDelegateGrantRoleNeverProducesRepoAdmin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current domainauth.RepoRole
		want    domainauth.RepoRole
	}{
		{"reader cycles to writer", domainauth.RepoRoleReader, domainauth.RepoRoleWriter},
		{"writer cycles to reader", domainauth.RepoRoleWriter, domainauth.RepoRoleReader},
		{"admin (structurally unreachable) falls back to reader", domainauth.RepoRoleAdmin, domainauth.RepoRoleReader},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := nextDelegateGrantRole(tt.current); got != tt.want {
				t.Fatalf("nextDelegateGrantRole(%q) = %q, want %q", tt.current, got, tt.want)
			}
			if got := nextDelegateGrantRole(tt.current); got == domainauth.RepoRoleAdmin {
				t.Fatalf("nextDelegateGrantRole(%q) = %q, must never be repo-admin", tt.current, got)
			}
		})
	}
}

// TestGeneratedFooterMatchesPreviousHandWrittenString is the
// tui-menu-architecture change's Phase 4 task 4.3 (T1.3) RED test
// (design.md Decision D's Rung-1 gate): shortHelpView(gitleaksConfigModalKeys)
// must equal admin_views.go's pre-change hand-written footer string
// byte-for-byte — the exact drift class this change exists to kill, proven
// by generating the same bytes instead of asserting they merely look
// similar.
func TestGeneratedFooterMatchesPreviousHandWrittenString(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	got := shortHelpView(theme, gitleaksConfigModalKeys)
	want := "Enter: save | Tab: next field | Space: toggle | Esc: cancel"
	if got != want {
		t.Fatalf("shortHelpView(gitleaksConfigModalKeys) = %q, want %q byte-for-byte", got, want)
	}
}

// TestNonMigratedScreensUnchanged is the tui-menu-architecture change's
// Phase 1 task 1.1 (T1.8) golden/characterization baseline, captured
// BEFORE screen.go/admin_router.go or any router code exists: for every one
// of the legacy admin screens dispatched by renderAdminScreen (the screens
// updateAdminKey's switch still resolves through legacyScreenHandlers),
// Model.View()'s composited output MUST always equal renderAdminScreen's
// own direct output for that exact screen/session/view/layout/now — the
// invariant design.md's Data Flow section states holds for every
// non-migrated screen. A routing regression that dispatches a legacy
// screen to the wrong renderer, drops its help text, or diverges its body
// would fail this test.
//
// Narrowed from 13 to 12 screens in Phase 11: screenAdminFeatures is no
// longer one of them (design.md Decision I repurposes it as
// securityMenuScreen, a migrated screen addressed via slotFor) — see
// TestEveryScreenRoutesExactlyOnce/TestModel* coverage for its own
// characterization now.
func TestNonMigratedScreensUnchanged(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	userView := AdminViewState{
		Users:            []ports.AdminUser{{ID: "u-1", Username: "alice", IsAdmin: true, Enabled: true}},
		SelectedUserID:   "u-1",
		SelectedUsername: "alice",
	}
	repoAdminView := AdminViewState{RepoAdminRepository: "team/app"}

	screens := []struct {
		name string
		view AdminViewState
	}{
		{"screenAdminUsers", AdminViewState{}},
		{"screenAdminCreateUser", AdminViewState{}},
		{"screenAdminEditUser", userView},
		{"screenAdminChangePassword", userView},
		{"screenAdminEditUserGrants", userView},
		{"screenAdminAddGrant", userView},
		{"screenAdminEditUserTokens", userView},
		{"screenAdminCreateToken", userView},
		{"screenRepoAdminGrants", repoAdminView},
		{"screenRepoAdminAddGrant", repoAdminView},
		{"screenAdminRobots", AdminViewState{}},
		{"screenAdminCreateRobot", AdminViewState{}},
	}
	ids := map[string]screen{
		"screenAdminUsers":          screenAdminUsers,
		"screenAdminCreateUser":     screenAdminCreateUser,
		"screenAdminEditUser":       screenAdminEditUser,
		"screenAdminChangePassword": screenAdminChangePassword,
		"screenAdminEditUserGrants": screenAdminEditUserGrants,
		"screenAdminAddGrant":       screenAdminAddGrant,
		"screenAdminEditUserTokens": screenAdminEditUserTokens,
		"screenAdminCreateToken":    screenAdminCreateToken,
		"screenRepoAdminGrants":     screenRepoAdminGrants,
		"screenRepoAdminAddGrant":   screenRepoAdminAddGrant,
		"screenAdminRobots":         screenAdminRobots,
		"screenAdminCreateRobot":    screenAdminCreateRobot,
	}

	if got, want := len(screens), 12; got != want {
		t.Fatalf("test setup invalid: %d screen fixtures, want %d (Phase 11 narrows design.md's 13 legacy screens by one)", got, want)
	}

	for _, tc := range screens {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			id := ids[tc.name]
			m := NewModel(&fakeQueryService{})
			m.viewport = viewportSize{Width: defaultViewportWidth, Height: adminTestViewportHeight}
			m.screen = id
			m.adminSession = session
			m.adminAuth = adminAuthStateAuthenticated
			m.adminView = tc.view
			m.now = func() time.Time { return now }

			layout := m.contentBudget(m.status, adminScreenHelp(id, tc.view))
			wantContext, wantBody, wantHelp := renderAdminScreen(theme, id, session, tc.view, m.repositories.Names(), layout, now)
			want := renderConsoleWorkspace("Regixtry Admin", wantContext, wantBody, m.status, wantHelp, statusKindAuto)

			got := m.View()
			if got != want {
				t.Fatalf("screen %q: Model.View() diverged from renderAdminScreen's direct output\ngot:\n%s\nwant:\n%s", id, got, want)
			}
		})
	}
}

// TestRenderPeerMenuRowsHighlightsCursorAndAlignsLabelColumn is the visual
// polish RED test the user asked for on the domain menu / Operations
// screens: the highlighted row must carry a leading "▸ " cursor (other rows
// get matching blank padding, not shifted text), and every row's label must
// be right-padded to the widest label's rendered width so each row's help
// text starts in the same column.
func TestRenderPeerMenuRowsHighlightsCursorAndAlignsLabelColumn(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	lines := renderPeerMenuRows(theme, []peerMenuRow{
		{Label: "Browse", Help: "Repositories, Tags, Manifests, Blobs & Uploads"},
		{Label: "Security & Compliance", Help: "Trivy, Gitleaks, Signing"},
	}, 1)

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if strings.Contains(lines[0], "▸") {
		t.Fatalf("non-highlighted row 0 = %q, must not carry the cursor", lines[0])
	}
	if !strings.Contains(lines[1], "▸") {
		t.Fatalf("highlighted row 1 = %q, must carry the cursor", lines[1])
	}

	labelColumnWidth := lipgloss.Width(strings.SplitN(lines[0], "Repositories", 2)[0])
	otherLabelColumnWidth := lipgloss.Width(strings.SplitN(lines[1], "Trivy", 2)[0])
	if labelColumnWidth != otherLabelColumnWidth {
		t.Fatalf("label column width = %d and %d, want equal (help text must align across rows)", labelColumnWidth, otherLabelColumnWidth)
	}
}

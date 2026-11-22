package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	bubbletable "github.com/evertras/bubble-table/table"
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
func TestNonMigratedScreensUnchanged(t *testing.T) {
	t.Parallel()

	theme := newAdminTheme()
	now := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", ExpiresAt: now.Add(10 * time.Minute)}

	repoAdminView := AdminViewState{RepoAdminRepository: "team/app"}

	screens := []struct {
		name string
		view AdminViewState
	}{
		{"screenRepoAdminGrants", repoAdminView},
		{"screenRepoAdminAddGrant", repoAdminView},
	}
	ids := map[string]screen{
		"screenRepoAdminGrants":   screenRepoAdminGrants,
		"screenRepoAdminAddGrant": screenRepoAdminAddGrant,
	}

	if got, want := len(screens), 2; got != want {
		t.Fatalf("test setup invalid: %d screen fixtures, want %d (the user-management screens left the old renderer)", got, want)
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

			got := m.View().Content
			if got != want {
				t.Fatalf("screen %q: Model.View() diverged from renderAdminScreen's direct output\ngot:\n%s\nwant:\n%s", id, got, want)
			}
		})
	}
}

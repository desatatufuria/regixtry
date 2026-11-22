package tui

import (
	"testing"

	"regixtry/internal/ports"
)

// TestTableRolesAssignsPrimaryAdaptiveAndCompactFloorAtThree is the Phase 4
// task 4.4 RED test: design.md decision #6 — primary tables get the whole
// per-table budget available (adaptive), compact tables are capped at
// compactTableRows(5) and floored at minTableRows(3) when the budget shrinks
// below that.
func TestTableRolesAssignsPrimaryAdaptiveAndCompactFloorAtThree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sectionRows int
		wantPrimary int
		wantCompact int
	}{
		{name: "generous budget: primary adaptive, compact capped at 5", sectionRows: 40, wantPrimary: 34, wantCompact: 5},
		{name: "tight budget: compact floors at 3", sectionRows: 7, wantPrimary: minTableRows, wantCompact: minTableRows},
		{name: "budget below minimum: both floor at 3", sectionRows: 2, wantPrimary: minTableRows, wantCompact: minTableRows},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			layout := consoleLayout{SectionRows: tc.sectionRows}
			primary, compact := tableRoles(layout)
			if primary != tc.wantPrimary {
				t.Errorf("primary = %d, want %d", primary, tc.wantPrimary)
			}
			if compact != tc.wantCompact {
				t.Errorf("compact = %d, want %d", compact, tc.wantCompact)
			}
		})
	}
}

// TestAdminFindingLinkResolution is the RED test for the shared link
// resolver (adminFindingLink), the single resolution both
// openSelectedAdminFindingLink (model.go, the Enter-key opener) uses:
// PrimaryURL first, else the constructed NVD URL from VulnerabilityID, and
// "" whenever nothing usable resolves -- including when the resolved value
// isn't a valid http(s) URL (isHTTPURL), the same defense-in-depth guard
// openURLInBrowser applies.
func TestAdminFindingLinkResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding ports.ScanRunFinding
		want    string
	}{
		{"PrimaryURL wins", ports.ScanRunFinding{VulnerabilityID: "CVE-1", PrimaryURL: "https://example.com/1"}, "https://example.com/1"},
		{"falls back to NVD", ports.ScanRunFinding{VulnerabilityID: "CVE-2"}, nvdVulnerabilityURL("CVE-2")},
		{"nothing resolves", ports.ScanRunFinding{}, ""},
		{"non-http PrimaryURL rejected", ports.ScanRunFinding{VulnerabilityID: "CVE-3", PrimaryURL: "javascript:alert(1)"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := adminFindingLink(tc.finding); got != tc.want {
				t.Fatalf("adminFindingLink(%+v) = %q, want %q", tc.finding, got, tc.want)
			}
		})
	}
}

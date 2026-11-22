package tui

import (
	"testing"

	"regixtry/internal/ports"
)

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

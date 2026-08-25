package tui

import "testing"

// TestGitleaksConfigScreenSatisfiesAdminScreenInterface is the
// tui-menu-architecture change's Phase 6 task 6.1 compile-level RED test
// (design.md Decision G): gitleaksConfigScreen must satisfy the adminScreen
// contract value-typed (Update returns a new adminScreen, never a pointer
// receiver — design.md Decision A).
func TestGitleaksConfigScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()

	var _ adminScreen = gitleaksConfigScreen{}
}

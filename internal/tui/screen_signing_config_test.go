package tui

import "testing"

// TestSigningConfigScreenSatisfiesAdminScreenInterface is the Phase 12.3 RED
// test (design.md's State Migration table: SigningPolicy/SigningPolicyModal
// -> signingConfigScreen), mirroring
// TestGitleaksConfigScreenSatisfiesAdminScreenInterface's compile-level
// assertion. signingConfigScreen must satisfy the adminScreen contract
// value-typed (Update returns a new adminScreen, never a pointer receiver --
// design.md Decision A).
func TestSigningConfigScreenSatisfiesAdminScreenInterface(t *testing.T) {
	t.Parallel()

	var _ adminScreen = signingConfigScreen{}
}

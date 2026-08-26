package ports

import "testing"

// TestValidUpdateChannel guards the exact two-value enum accepted by the
// update-channel setting -- must mirror release.ChannelStable/ChannelInsider
// byte-for-byte (internal/ports cannot import internal/infra/release without
// inverting this codebase's layering, so the literal values are duplicated
// here on purpose; keep them in sync).
func TestValidUpdateChannel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value string
		want  bool
	}{
		{"stable", true},
		{"insider", true},
		{"", false},
		{"Stable", false},
		{"INSIDER", false},
		{"rc", false},
		{"beta", false},
	}
	for _, tc := range cases {
		if got := ValidUpdateChannel(tc.value); got != tc.want {
			t.Errorf("ValidUpdateChannel(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

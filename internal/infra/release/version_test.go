package release

import "testing"

func TestParseVersionRejectsUnparseable(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "dev", "v1", "v1.2", "v1.2.3.4", "v1.2.x", "v1.2.3-beta1", "v1.2.3-rc"} {
		if _, err := parseVersion(raw); err == nil {
			t.Fatalf("parseVersion(%q) = nil error, want an error for an unparseable version", raw)
		}
	}
}

func TestParseVersionAcceptsStableAndRC(t *testing.T) {
	t.Parallel()

	stable, err := parseVersion("v0.2.0")
	if err != nil {
		t.Fatalf("parseVersion(v0.2.0) error = %v", err)
	}
	if stable.major != 0 || stable.minor != 2 || stable.patch != 0 || stable.hasRC {
		t.Fatalf("stable = %#v, want {0 2 0 false ...}", stable)
	}

	rc, err := parseVersion("v0.2.0-rc105")
	if err != nil {
		t.Fatalf("parseVersion(v0.2.0-rc105) error = %v", err)
	}
	if rc.major != 0 || rc.minor != 2 || rc.patch != 0 || !rc.hasRC || rc.rc != 105 {
		t.Fatalf("rc = %#v, want {0 2 0 true 105}", rc)
	}

	// Version with no leading "v" (goreleaser's {{ .Version }} template
	// omits it, matching release.Asset.Version's own v-stripped form).
	noPrefix, err := parseVersion("0.2.0")
	if err != nil {
		t.Fatalf("parseVersion(0.2.0) error = %v", err)
	}
	if noPrefix.major != 0 || noPrefix.minor != 2 || noPrefix.patch != 0 {
		t.Fatalf("noPrefix = %#v, want {0 2 0 ...}", noPrefix)
	}
}

func TestCompareVersionsOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    string
		b    string
		want int // sign of compare(a, b)
	}{
		{"patch bump", "v0.2.0", "v0.2.1", -1},
		{"minor bump", "v0.2.0", "v0.3.0", -1},
		{"major bump", "v0.2.0", "v1.0.0", -1},
		{"stable beats rc of the same version", "v0.2.0-rc105", "v0.2.0", -1},
		{"rc never beats stable of the same version, even a much later rc", "v0.2.0-rc999", "v0.2.0", -1},
		{"rc numeric ordering, not lexicographic: rc105 beats rc99", "v0.2.0-rc99", "v0.2.0-rc105", -1},
		{"rc numeric ordering across a digit-width boundary: rc9 vs rc10", "v0.2.0-rc9", "v0.2.0-rc10", -1},
		{"equal versions", "v0.2.0", "v0.2.0", 0},
		{"equal rc versions", "v0.2.0-rc105", "v0.2.0-rc105", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a, err := parseVersion(tc.a)
			if err != nil {
				t.Fatalf("parseVersion(%q) error = %v", tc.a, err)
			}
			b, err := parseVersion(tc.b)
			if err != nil {
				t.Fatalf("parseVersion(%q) error = %v", tc.b, err)
			}
			got := compareVersions(a, b)
			gotSign := sign(got)
			if gotSign != tc.want {
				t.Fatalf("compareVersions(%q, %q) sign = %d, want %d", tc.a, tc.b, gotSign, tc.want)
			}
			// compare must anti-commute: compare(b, a) has the opposite sign
			// (or is also zero when compare(a, b) is zero).
			if reverse := sign(compareVersions(b, a)); reverse != -tc.want {
				t.Fatalf("compareVersions(%q, %q) sign = %d, want %d (anti-commutative with the forward comparison)", tc.b, tc.a, reverse, -tc.want)
			}
		})
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	t.Run("reports a genuinely newer stable candidate", func(t *testing.T) {
		t.Parallel()
		newer, err := IsNewerVersion("v0.2.1", "v0.2.0")
		if err != nil || !newer {
			t.Fatalf("IsNewerVersion(v0.2.1, v0.2.0) = (%v, %v), want (true, nil)", newer, err)
		}
	})

	t.Run("does not flag an equal or older version as newer", func(t *testing.T) {
		t.Parallel()
		newer, err := IsNewerVersion("v0.2.0", "v0.2.0")
		if err != nil || newer {
			t.Fatalf("IsNewerVersion(v0.2.0, v0.2.0) = (%v, %v), want (false, nil)", newer, err)
		}
		newer, err = IsNewerVersion("v0.1.0", "v0.2.0")
		if err != nil || newer {
			t.Fatalf("IsNewerVersion(v0.1.0, v0.2.0) = (%v, %v), want (false, nil)", newer, err)
		}
	})

	t.Run("an unparseable current version (e.g. the dev-binary fallback) is an explicit error, never a wrong default", func(t *testing.T) {
		t.Parallel()
		if _, err := IsNewerVersion("v0.2.0", "dev"); err == nil {
			t.Fatalf("IsNewerVersion(v0.2.0, dev) error = nil, want an error so the caller can skip the check entirely")
		}
	})
}

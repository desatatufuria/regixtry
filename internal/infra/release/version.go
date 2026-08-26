package release

import (
	"fmt"
	"strconv"
	"strings"
)

// parsedVersion is this repo's own tag shape, "vMAJOR.MINOR.PATCH" or
// "vMAJOR.MINOR.PATCH-rcN" -- deliberately narrower than general semver
// (this repo never publishes any other prerelease/build-metadata shape), so
// a hand-rolled parser is simpler and more precise than pulling in a
// general-purpose semver dependency for one comparison.
type parsedVersion struct {
	major, minor, patch int
	hasRC               bool
	rc                  int
}

// parseVersion accepts an optional leading "v" (goreleaser's {{ .Version }}
// template omits it; a manually-typed tag includes it -- both must parse
// the same way) and requires exactly three dot-separated integers, with an
// optional "-rcN" suffix. Anything else is an explicit error: this must
// never panic and must never silently coerce an unparseable string (e.g.
// the dev-binary fallback "dev") into a comparable value.
func parseVersion(raw string) (parsedVersion, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if trimmed == "" {
		return parsedVersion{}, fmt.Errorf("release: version is empty")
	}

	core, rcSuffix, hasSuffix := strings.Cut(trimmed, "-")
	var version parsedVersion
	if hasSuffix {
		rcNumber, ok := strings.CutPrefix(rcSuffix, "rc")
		if !ok || rcNumber == "" {
			return parsedVersion{}, fmt.Errorf("release: version %q has an unsupported suffix %q, want \"rcN\"", raw, rcSuffix)
		}
		n, err := strconv.Atoi(rcNumber)
		if err != nil || n < 0 {
			return parsedVersion{}, fmt.Errorf("release: version %q has a non-numeric rc suffix %q", raw, rcSuffix)
		}
		version.hasRC = true
		version.rc = n
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return parsedVersion{}, fmt.Errorf("release: version %q must have exactly 3 dot-separated components (major.minor.patch), got %d", raw, len(parts))
	}
	values := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsedVersion{}, fmt.Errorf("release: version %q component %q is not a non-negative integer", raw, part)
		}
		values[i] = n
	}
	version.major, version.minor, version.patch = values[0], values[1], values[2]
	return version, nil
}

// compareVersions returns <0 if a is older than b, 0 if equal, >0 if a is
// newer than b. Ordering: major, then minor, then patch, as integers. When
// major/minor/patch are equal, a stable build (no rc suffix) is always
// newer than any rc build of the same major.minor.patch; between two rc
// builds of the same major.minor.patch, the rc number is compared as an
// integer -- NEVER lexicographically. A naive string comparison of the rc
// suffix is wrong once the rc number crosses a digit-count boundary ("rc105"
// sorts lexicographically BEFORE "rc99", since '1' < '9' at the first
// differing byte) -- this repo is already well past that boundary.
func compareVersions(a, b parsedVersion) int {
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	if a.patch != b.patch {
		return a.patch - b.patch
	}
	if a.hasRC != b.hasRC {
		if a.hasRC {
			return -1 // a is an rc of this version, b is stable: a is older
		}
		return 1 // a is stable, b is an rc of this version: a is newer
	}
	if !a.hasRC {
		return 0 // both stable, same major.minor.patch
	}
	return a.rc - b.rc
}

// IsNewerVersion reports whether candidate is a genuinely newer version than
// current, using this repo's own version ordering (compareVersions). Either
// version failing to parse (e.g. current being the dev-binary fallback
// "dev") is returned as an explicit error, never coerced into "not newer" or
// "always newer" -- the caller (the TUI's background update check) must
// treat a returned error as "cannot determine, skip the check entirely",
// never as a signal either way.
func IsNewerVersion(candidate, current string) (bool, error) {
	candidateParsed, err := parseVersion(candidate)
	if err != nil {
		return false, err
	}
	currentParsed, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	return compareVersions(candidateParsed, currentParsed) > 0, nil
}

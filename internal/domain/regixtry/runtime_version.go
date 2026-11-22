package regixtry

import (
	"fmt"
	"regexp"
)

// MaxRuntimeVersionLength bounds a managed-runtime version string.
const MaxRuntimeVersionLength = 64

// runtimeVersionPattern is `v?MAJOR.MINOR.PATCH[-prerelease]`: numeric core
// without leading zeros and dot-separated alphanumeric/hyphen prerelease
// identifiers. Nothing else (slashes, dots-dots, queries, fragments, percent
// escapes, whitespace, control characters) can match.
var runtimeVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// ValidateRuntimeVersion reports whether version is an acceptable
// managed-runtime release version. It does not trim: callers decide how to
// treat empty input ("latest") before calling.
func ValidateRuntimeVersion(version string) error {
	if len(version) > MaxRuntimeVersionLength {
		return NewValidationError(fmt.Sprintf("version must be at most %d characters", MaxRuntimeVersionLength))
	}
	if !runtimeVersionPattern.MatchString(version) {
		return NewValidationError("version must look like v1.2.3 or 1.2.3-rc.1")
	}
	return nil
}

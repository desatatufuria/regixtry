package regixtry

import (
	"errors"
	"fmt"
	"strings"
)

// MaxProjectNameLength bounds a project name. It is a regixtry decision, not
// an OCI rule.
const MaxProjectNameLength = 64

// reservedProjectNames are path markers that already steer /v2/<name>/... and
// /admin/v1/... routing; a project must never shadow one.
var reservedProjectNames = map[string]struct{}{
	"blobs":                {},
	"manifests":            {},
	"tags":                 {},
	"referrers":            {},
	"grants":               {},
	"repository-overrides": {},
}

// ErrProjectNotEmpty and ErrProjectExists are the sentinels wrapped by the
// project conflict errors so callers can use errors.Is next to IsCode.
var (
	ErrProjectNotEmpty = errors.New("project is not empty")
	ErrProjectExists   = errors.New("project already exists")
)

// ParseProjectName validates a project name: ONE repository path segment (so
// lowercase, no slash), at most MaxProjectNameLength bytes, not reserved.
func ParseProjectName(value string) (string, error) {
	if value == "" {
		return "", NewValidationError("project name is required")
	}
	if len(value) > MaxProjectNameLength {
		return "", NewValidationError(fmt.Sprintf("project name %q is longer than %d characters", value, MaxProjectNameLength))
	}
	if !repositorySegmentPattern.MatchString(value) {
		return "", NewValidationError(fmt.Sprintf("invalid project name %q: use lowercase letters, digits and the separators . _ - (no slash)", value))
	}
	if _, reserved := reservedProjectNames[value]; reserved {
		return "", NewValidationError(fmt.Sprintf("project name %q is reserved", value))
	}
	return value, nil
}

// ProjectOf returns the project of a repository: its FIRST path segment. A flat
// repository name (no slash) belongs to no project. Compare the result with
// ==, never with a string prefix: project "odabank" must not cover
// "odabank2/...".
func ProjectOf(repository RepositoryRef) (string, bool) {
	index := strings.IndexByte(repository.Name, '/')
	if index <= 0 {
		return "", false
	}
	return repository.Name[:index], true
}

// NewProjectExistsError reports a duplicate project (conflict).
func NewProjectExistsError(name string) error {
	return &Error{Code: ErrorCodeConflict, Message: fmt.Sprintf("project %q already exists", name), Err: ErrProjectExists}
}

// NewProjectNotEmptyError reports that a project cannot be deleted because it
// still holds repositories or open uploads (conflict).
func NewProjectNotEmptyError(name string, repositories int, openUploads int) error {
	var parts []string
	if repositories > 0 {
		parts = append(parts, plural(repositories, "repository", "repositories"))
	}
	if openUploads > 0 {
		parts = append(parts, plural(openUploads, "open upload", "open uploads"))
	}
	return &Error{
		Code:    ErrorCodeConflict,
		Message: fmt.Sprintf("project %q still has %s; delete or finish them first", name, strings.Join(parts, " and ")),
		Err:     ErrProjectNotEmpty,
	}
}

func plural(count int, one string, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, one)
	}
	return fmt.Sprintf("%d %s", count, many)
}

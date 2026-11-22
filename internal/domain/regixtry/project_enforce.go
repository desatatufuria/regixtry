package regixtry

import (
	"errors"
	"fmt"
)

// ErrProjectNotFound is wrapped by NewProjectNotFoundError so the HTTP layer
// can answer NAME_UNKNOWN (instead of the route's default code) when a push
// targets a project that does not exist.
var ErrProjectNotFound = errors.New("project does not exist")

// NewProjectNotFoundError reports that a push targets a missing project (not
// found). The message is what a docker client prints.
func NewProjectNotFoundError(name string) error {
	return &Error{
		Code:    ErrorCodeNotFound,
		Message: fmt.Sprintf("project %q does not exist; ask an administrator to create it", name),
		Err:     ErrProjectNotFound,
	}
}

// NewFlatRepositoryRefusedError reports that a NEW repository without a
// project segment cannot be created while project enforcement is on. It is an
// invalid repository name (OCI NAME_INVALID, HTTP 400).
func NewFlatRepositoryRefusedError(name string) error {
	return &Error{
		Code:    ErrorCodeInvalidRepository,
		Message: fmt.Sprintf("repository %q has no project; use <project>/%s (flat repository names are no longer created)", name, name),
	}
}

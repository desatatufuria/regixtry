package tui

import (
	"fmt"
	"regexp"
	"strings"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

// Live validation of a project name while it is typed. The server decides
// (domain.ParseProjectName); this mirrors it only to say WHICH rule fails
// before anything is sent, and project_name_test.go pins that both always
// agree on whether a name is valid. The first failing rule wins, in the
// order of the board: empty, length, slash, charset, reserved.
type projectNameIssue int

const (
	projectNameValid projectNameIssue = iota
	projectNameEmpty
	projectNameTooLong
	projectNameSlash
	projectNameCharset
	projectNameReserved
)

// projectNameSegment is domain's repository path segment: lowercase letters
// and digits, single . _ - separators between them.
var projectNameSegment = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// projectNameReservedWords are the path markers the registry API already
// uses (docs/api.md): a project may not shadow one.
var projectNameReservedWords = map[string]bool{
	"blobs": true, "manifests": true, "tags": true, "referrers": true, "grants": true, "repository-overrides": true,
}

func checkProjectName(name string) projectNameIssue {
	switch {
	case name == "":
		return projectNameEmpty
	case len(name) > regixtrydomain.MaxProjectNameLength:
		return projectNameTooLong
	case strings.Contains(name, "/"):
		return projectNameSlash
	case !projectNameSegment.MatchString(name):
		return projectNameCharset
	case projectNameReservedWords[name]:
		return projectNameReserved
	}
	return projectNameValid
}

// projectNameMessage is the text under the field for a name, "" when valid.
func projectNameMessage(name string) string {
	switch checkProjectName(name) {
	case projectNameEmpty:
		return "Enter a project name."
	case projectNameTooLong:
		return fmt.Sprintf("At most %d characters (%d now).", regixtrydomain.MaxProjectNameLength, len(name))
	case projectNameSlash:
		return "A project name has no slash. Repositories go inside the project."
	case projectNameCharset:
		return "Use lowercase letters, digits and . _ - only; start and end with a letter or digit."
	case projectNameReserved:
		return fmt.Sprintf("%s is reserved by the registry API. Pick another name.", name)
	}
	return ""
}

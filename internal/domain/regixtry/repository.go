package regixtry

import (
	"regexp"
	"strings"
)

var repositorySegmentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

type RepositoryRef struct {
	Name string
}

func ParseRepositoryRef(value string) (RepositoryRef, error) {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return RepositoryRef{}, NewInvalidRepositoryError(value)
	}

	segments := strings.Split(value, "/")
	for _, segment := range segments {
		if !repositorySegmentPattern.MatchString(segment) {
			return RepositoryRef{}, NewInvalidRepositoryError(value)
		}
	}

	return RepositoryRef{Name: value}, nil
}

func MustParseRepositoryRef(value string) RepositoryRef {
	reference, err := ParseRepositoryRef(value)
	if err != nil {
		panic(err)
	}

	return reference
}

func (r RepositoryRef) Validate() error {
	_, err := ParseRepositoryRef(r.Name)
	return err
}

func (r RepositoryRef) String() string {
	return r.Name
}

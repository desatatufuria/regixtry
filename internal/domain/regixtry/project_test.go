package regixtry

import (
	"errors"
	"strings"
	"testing"
)

func TestParseProjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple", input: "odabank"},
		{name: "digits", input: "odabank2"},
		{name: "separators", input: "team.a-b_c"},
		{name: "max length", input: strings.Repeat("a", 64)},
		{name: "empty", input: "", wantErr: true},
		{name: "uppercase", input: "Odabank", wantErr: true},
		{name: "slash inside", input: "odabank/payments", wantErr: true},
		{name: "leading slash", input: "/odabank", wantErr: true},
		{name: "trailing slash", input: "odabank/", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 65), wantErr: true},
		{name: "unicode", input: "café", wantErr: true},
		{name: "whitespace", input: " odabank", wantErr: true},
		{name: "leading dash", input: "-odabank", wantErr: true},
		{name: "reserved blobs", input: "blobs", wantErr: true},
		{name: "reserved manifests", input: "manifests", wantErr: true},
		{name: "reserved tags", input: "tags", wantErr: true},
		{name: "reserved referrers", input: "referrers", wantErr: true},
		{name: "reserved grants", input: "grants", wantErr: true},
		{name: "reserved repository-overrides", input: "repository-overrides", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseProjectName(tt.input)
			if tt.wantErr {
				if !IsCode(err, ErrorCodeValidation) {
					t.Fatalf("ParseProjectName(%q) error = %v, want a validation error", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseProjectName(%q) error = %v", tt.input, err)
			}
			if got != tt.input {
				t.Fatalf("ParseProjectName(%q) = %q, want the input unchanged", tt.input, got)
			}
		})
	}
}

func TestProjectOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repository  string
		wantProject string
		wantOK      bool
	}{
		{name: "two segments", repository: "odabank/payments", wantProject: "odabank", wantOK: true},
		{name: "nested", repository: "odabank/payments/strike", wantProject: "odabank", wantOK: true},
		{name: "similar prefix is another project", repository: "odabank2/payments", wantProject: "odabank2", wantOK: true},
		{name: "flat repository has no project", repository: "odabank", wantProject: "", wantOK: false},
		{name: "zero value", repository: "", wantProject: "", wantOK: false},
		{name: "leading slash", repository: "/odabank", wantProject: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project, ok := ProjectOf(RepositoryRef{Name: tt.repository})
			if project != tt.wantProject || ok != tt.wantOK {
				t.Fatalf("ProjectOf(%q) = (%q, %v), want (%q, %v)", tt.repository, project, ok, tt.wantProject, tt.wantOK)
			}
		})
	}
}

func TestNewProjectNotEmptyErrorIsTypedConflict(t *testing.T) {
	t.Parallel()

	err := NewProjectNotEmptyError("odabank", 2, 1)
	if !IsCode(err, ErrorCodeConflict) {
		t.Fatalf("error = %v, want a conflict error", err)
	}
	if !errors.Is(err, ErrProjectNotEmpty) {
		t.Fatalf("error = %v, want errors.Is(ErrProjectNotEmpty)", err)
	}
	for _, want := range []string{`"odabank"`, "2 repositories", "1 open upload"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err.Error(), want)
		}
	}
}

func TestNewProjectExistsErrorIsConflict(t *testing.T) {
	t.Parallel()

	err := NewProjectExistsError("odabank")
	if !IsCode(err, ErrorCodeConflict) || !errors.Is(err, ErrProjectExists) {
		t.Fatalf("error = %v, want a conflict wrapping ErrProjectExists", err)
	}
}

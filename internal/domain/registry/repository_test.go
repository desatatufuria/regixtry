package registry

import "testing"

func TestParseRepositoryRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "simple", value: "alpine"},
		{name: "namespaced", value: "library/alpine"},
		{name: "invalid uppercase", value: "Library/alpine", wantError: true},
		{name: "invalid separator", value: "library//alpine", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref, err := ParseRepositoryRef(tt.value)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error for %q", tt.value)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseRepositoryRef() error = %v", err)
			}

			if ref.Name != tt.value {
				t.Fatalf("ref.Name = %q, want %q", ref.Name, tt.value)
			}
		})
	}
}

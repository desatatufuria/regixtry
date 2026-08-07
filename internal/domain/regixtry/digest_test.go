package regixtry

import (
	"strings"
	"testing"
)

func TestParseDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "valid sha256", value: DigestFromBytes([]byte("hello")).String()},
		{name: "invalid algorithm", value: "sha512:abcd", wantError: true},
		{name: "invalid hex", value: "sha256:" + strings.Repeat("z", 64), wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			digest, err := ParseDigest(tt.value)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error for %q", tt.value)
				}

				if !IsCode(err, ErrorCodeInvalidDigest) {
					t.Fatalf("expected invalid digest error, got %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseDigest() error = %v", err)
			}

			if digest.String() != tt.value {
				t.Fatalf("digest = %q, want %q", digest, tt.value)
			}
		})
	}
}

func TestDigestFromBytes(t *testing.T) {
	t.Parallel()

	digest := DigestFromBytes([]byte("hello"))
	const want = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

	if digest.String() != want {
		t.Fatalf("digest = %q, want %q", digest, want)
	}
}

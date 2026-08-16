package regixtry

import "testing"

func TestNewPolicyViolationErrorProducesErrorCodePolicyViolation(t *testing.T) {
	t.Parallel()

	err := NewPolicyViolationError("blocked by scan policy")
	if !IsCode(err, ErrorCodePolicyViolation) {
		t.Fatalf("IsCode(err, ErrorCodePolicyViolation) = false, want true for err = %v", err)
	}
}

// TestNewUnsupportedErrorProducesErrorCodeUnsupported pins tasks.md 6.1
// (design.md D9): a new ErrorCodeUnsupported = "UNSUPPORTED", mapped in
// writeAdminError to 501 -- distinct from every existing code, since admin
// routes carry no OCI error code vocabulary and the message is the only
// disambiguator.
func TestNewUnsupportedErrorProducesErrorCodeUnsupported(t *testing.T) {
	t.Parallel()

	err := NewUnsupportedError("blob garbage collection delete is disabled (REGISTRY_GC_DELETE_ENABLED)")
	if !IsCode(err, ErrorCodeUnsupported) {
		t.Fatalf("IsCode(err, ErrorCodeUnsupported) = false, want true for err = %v", err)
	}
	if ErrorCodeUnsupported != "UNSUPPORTED" {
		t.Fatalf("ErrorCodeUnsupported = %q, want %q", ErrorCodeUnsupported, "UNSUPPORTED")
	}
}

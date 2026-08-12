package regixtry

import "testing"

func TestNewPolicyViolationErrorProducesErrorCodePolicyViolation(t *testing.T) {
	t.Parallel()

	err := NewPolicyViolationError("blocked by scan policy")
	if !IsCode(err, ErrorCodePolicyViolation) {
		t.Fatalf("IsCode(err, ErrorCodePolicyViolation) = false, want true for err = %v", err)
	}
}

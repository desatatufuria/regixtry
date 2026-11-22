package tui

import (
	"testing"
)

// TestCycleIndexWrapsForwardPastTheEnd is the Phase 1 task 1.5 RED test:
// unlike boundedIndex (which clamps), cycleIndex wraps past the last index
// back to 0.
func TestCycleIndexWrapsForwardPastTheEnd(t *testing.T) {
	t.Parallel()

	got := cycleIndex(2, 3) // last valid index (size-1)
	if got != 2 {
		t.Fatalf("cycleIndex(2, 3) = %d, want 2 (still in range)", got)
	}

	got = cycleIndex(3, 3) // one past the end
	if got != 0 {
		t.Fatalf("cycleIndex(3, 3) = %d, want 0 (wraps to first)", got)
	}
}

// TestCycleIndexWrapsBackwardBeforeTheStart triangulates task 1.5: moving
// before index 0 wraps to the last valid index, proving cycleIndex is
// distinct from the clamping boundedIndex in both directions.
func TestCycleIndexWrapsBackwardBeforeTheStart(t *testing.T) {
	t.Parallel()

	got := cycleIndex(-1, 3)
	if got != 2 {
		t.Fatalf("cycleIndex(-1, 3) = %d, want 2 (wraps to last)", got)
	}
}

// TestCycleIndexHandlesZeroSize guards the degenerate empty-slice case so
// callers never index out of range while a tab slice is still empty.
func TestCycleIndexHandlesZeroSize(t *testing.T) {
	t.Parallel()

	got := cycleIndex(5, 0)
	if got != 0 {
		t.Fatalf("cycleIndex(5, 0) = %d, want 0", got)
	}
}

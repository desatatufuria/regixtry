package tui

import "testing"

// TestSortModeNextCyclesNameAscendingToDateDescendingAndBack is the RED test
// for the sortable-tags-and-projects feature's shared sort-cycle primitive:
// exactly a 2-state cycle (name ascending <-> date descending, the exact
// pair the feature's own spec names), discoverable and used identically by
// both the Tags screen and the Projects screen.
func TestSortModeNextCyclesNameAscendingToDateDescendingAndBack(t *testing.T) {
	t.Parallel()

	mode := sortByNameAsc
	mode = mode.next()
	if mode != sortByDateDesc {
		t.Fatalf("sortByNameAsc.next() = %v, want sortByDateDesc", mode)
	}
	mode = mode.next()
	if mode != sortByNameAsc {
		t.Fatalf("sortByDateDesc.next() = %v, want sortByNameAsc", mode)
	}
}

func TestSortModeLabelIsDiscoverableTextOnly(t *testing.T) {
	t.Parallel()

	if got, want := sortByNameAsc.label(), "name"; got != want {
		t.Fatalf("sortByNameAsc.label() = %q, want %q", got, want)
	}
	if got, want := sortByDateDesc.label(), "date"; got != want {
		t.Fatalf("sortByDateDesc.label() = %q, want %q", got, want)
	}
}

package tui

import (
	"reflect"
	"testing"
)

// TestNoSecondConfirmPatternRemains is the tui-menu-architecture change's
// Phase 7 task 7.4 explicit checklist item ("No second confirm pattern
// remains", spec.md's tui-navigation-architecture requirement "One
// Confirm-Before-Destructive-Action Primitive"): AdminViewState and
// TagsModel must each hold exactly a confirmPrompt-typed field for their
// confirm, and no field named "PendingDelete" or "ConfirmModal" (the two
// retired patterns) may exist on either struct.
func TestNoSecondConfirmPatternRemains(t *testing.T) {
	t.Parallel()

	confirmPromptType := reflect.TypeOf(confirmPrompt{})

	for _, tc := range []struct {
		name      string
		structVal any
		wantField string
	}{
		{"AdminViewState", AdminViewState{}, "Confirm"},
		{"TagsModel", TagsModel{}, "Confirm"},
	} {
		typ := reflect.TypeOf(tc.structVal)
		field, ok := typ.FieldByName(tc.wantField)
		if !ok {
			t.Fatalf("%s has no %q field, want exactly one confirmPrompt-typed field", tc.name, tc.wantField)
		}
		if field.Type != confirmPromptType {
			t.Fatalf("%s.%s type = %v, want %v", tc.name, tc.wantField, field.Type, confirmPromptType)
		}
		for _, retired := range []string{"PendingDelete", "ConfirmModal"} {
			if _, ok := typ.FieldByName(retired); ok {
				t.Fatalf("%s still has a %q field, want it retired onto confirmPrompt", tc.name, retired)
			}
		}
	}
}

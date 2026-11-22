package tui

import (
	"reflect"
	"testing"
)

// TestAdminScreenSetHoldsOnlyInterfaceValues is the tui-menu-architecture
// change's Phase 2 task 2.1 (T1.6) RED test (design.md Decision B):
// Model.adminScreens must be a fixed-size array of an INTERFACE type, never
// a slice or map — arrays are values, so copying Model copies the screen
// set, and interface-typed elements make `m.adminScreens[slot].field = x`
// a compile error (you cannot address a field through an interface).
func TestAdminScreenSetHoldsOnlyInterfaceValues(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(Model{}.adminScreens)
	if typ.Kind() != reflect.Array {
		t.Fatalf("Model.adminScreens kind = %v, want %v (Decision B: array, not slice/map)", typ.Kind(), reflect.Array)
	}
	if got := typ.Elem().Kind(); got != reflect.Interface {
		t.Fatalf("Model.adminScreens element kind = %v, want %v", got, reflect.Interface)
	}
}

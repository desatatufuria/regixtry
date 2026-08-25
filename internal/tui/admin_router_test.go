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

// adminScreenIDs is every screen constant updateAdminKey's routing domain
// covers (isAdminScreen's exact 15), used by TestEveryScreenRoutesExactlyOnce
// below.
var adminScreenIDs = []screen{
	screenAdminLogin, screenAdminAuthenticating, screenAdminUsers, screenAdminFeatures,
	screenAdminCreateUser, screenAdminEditUser, screenAdminChangePassword,
	screenAdminEditUserGrants, screenAdminAddGrant, screenAdminEditUserTokens,
	screenAdminCreateToken, screenRepoAdminGrants, screenRepoAdminAddGrant,
	screenAdminRobots, screenAdminCreateRobot,
}

// TestEveryScreenRoutesExactlyOnce is the tui-menu-architecture change's
// Phase 3 task 3.1 (T1.7, first half) RED test (design.md Decision C): the
// union of slotFor's migrated slots and legacyScreenHandlers must cover
// every screen id updateAdminKey's switch used to dispatch (isAdminScreen's
// domain) exactly once — no id unrouted, no id double-routed.
func TestEveryScreenRoutesExactlyOnce(t *testing.T) {
	t.Parallel()

	for _, id := range adminScreenIDs {
		_, migrated := slotFor(id)
		_, legacy := legacyScreenHandlers[id]
		switch {
		case migrated && legacy:
			t.Fatalf("screen %q is routed by BOTH a migrated slot and legacyScreenHandlers, want exactly one", id)
		case !migrated && !legacy:
			t.Fatalf("screen %q is routed by NEITHER a migrated slot nor legacyScreenHandlers, want exactly one", id)
		}
	}
}

// TestLegacyAdapterHoldsNoState is the tui-menu-architecture change's
// Phase 3 task 3.2 (T1.7, second half) RED test (design.md Decision C):
// legacyHandler's underlying kind must be a func — the adapter is a
// package-level function table with zero data of its own, so it cannot
// become a god-object; AdminViewState stays exactly where it is on Model.
func TestLegacyAdapterHoldsNoState(t *testing.T) {
	t.Parallel()

	var zero legacyHandler
	if kind := reflect.TypeOf(zero).Kind(); kind != reflect.Func {
		t.Fatalf("legacyHandler kind = %v, want %v", kind, reflect.Func)
	}
	if got, want := len(legacyScreenHandlers), len(adminScreenIDs); got != want {
		t.Fatalf("len(legacyScreenHandlers) = %d, want %d (every admin screen routed through the adapter in Slice 1)", got, want)
	}
}

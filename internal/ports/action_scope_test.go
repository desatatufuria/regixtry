package ports

import (
	"testing"

	domainauth "regixtry/internal/domain/auth"
)

// TestActionCatalogScopeIsAcceptedByScopeValidator guards against a resource
// type mismatch: Action{Verb: ActionCatalog}.Scope() must emit a scope string
// that domainauth.ParseScope (the same validator token issuance/verification
// relies on) actually accepts. domainauth.scopeTypeRegixtry is "regixtry"
// (matching the module name), not "registry" -- so a catalog-scope token
// built from the emitted string must round-trip through ParseScope without
// error, and must resolve to the domainauth.Scope.IsRegixtryCatalog() shape.
func TestActionCatalogScopeIsAcceptedByScopeValidator(t *testing.T) {
	t.Parallel()

	action := Action{Verb: ActionCatalog}
	emitted := action.Scope()

	scope, err := domainauth.ParseScope(emitted)
	if err != nil {
		t.Fatalf("ParseScope(%q) error = %v, want the catalog scope Action.Scope() emits to be accepted", emitted, err)
	}
	if !scope.IsRegixtryCatalog() {
		t.Fatalf("ParseScope(%q).IsRegixtryCatalog() = false, want true", emitted)
	}
}

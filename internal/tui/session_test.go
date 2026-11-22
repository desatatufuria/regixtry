package tui

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminSessionIsExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 4, 22, 0, 0, 0, time.UTC)
	session := AdminSession{
		Username:    "operator",
		BearerToken: "token",
		ExpiresAt:   now.Add(30 * time.Second),
	}

	if session.IsExpired(now) {
		t.Fatal("IsExpired() = true, want false before expiry")
	}
	if !session.IsExpired(now.Add(30 * time.Second)) {
		t.Fatal("IsExpired() = false, want true at expiry")
	}
	if got := session.Remaining(now); got != 30*time.Second {
		t.Fatalf("Remaining() = %s, want %s", got, 30*time.Second)
	}
	if got := session.Remaining(now.Add(31 * time.Second)); got != 0 {
		t.Fatalf("Remaining() after expiry = %s, want 0", got)
	}
}

func TestLogoutAdminStateClearsSession(t *testing.T) {
	t.Parallel()

	session := LogoutAdminState()
	if session.IsAuthenticated() {
		t.Fatal("LogoutAdminState() returned authenticated session")
	}
	if session.ExpiredReason != "" {
		t.Fatalf("ExpiredReason = %q, want empty", session.ExpiredReason)
	}
}

func TestExpireAdminStateKeepsReason(t *testing.T) {
	t.Parallel()

	session := ExpireAdminState("Session expired. Log in again.")
	if session.IsAuthenticated() {
		t.Fatal("ExpireAdminState() returned authenticated session")
	}
	if session.ExpiredReason != "Session expired. Log in again." {
		t.Fatalf("ExpiredReason = %q, want session expiry message", session.ExpiredReason)
	}
}

// TestAdminScanHistoryModalActiveReflectsOpenField is the Phase 2 task 2.1
// RED test: adminScanHistoryModal follows the same Active()-gated pattern as
// trivyConfigModal/adminConfirmModal — Active() reports exactly the Open
// field, nothing else, so callers can gate rendering/key-routing on it
// (design.md "func (m adminScanHistoryModal) Active() bool { return
// m.Open }").
func TestAdminScanHistoryModalActiveReflectsOpenField(t *testing.T) {
	t.Parallel()

	closed := adminScanHistoryModal{}
	if closed.Active() {
		t.Fatal("adminScanHistoryModal{}.Active() = true, want false when Open is unset")
	}

	open := adminScanHistoryModal{Open: true, Repository: "acme/api"}
	if !open.Active() {
		t.Fatal("adminScanHistoryModal{Open: true}.Active() = false, want true")
	}
}

// TestScanPolicyModalActiveReflectsOpenField is the Phase 8 task 8.1 RED
// test: scanPolicyModal follows the same Active()-gated pattern as
// trivyConfigModal (design.md Decision 6 — a sibling struct, not an
// extension of trivyConfigModal).
func TestScanPolicyModalActiveReflectsOpenField(t *testing.T) {
	t.Parallel()

	closed := scanPolicyModal{}
	if closed.Active() {
		t.Fatal("scanPolicyModal{}.Active() = true, want false when Open is unset")
	}

	open := scanPolicyModal{Open: true}
	if !open.Active() {
		t.Fatal("scanPolicyModal{Open: true}.Active() = false, want true")
	}
}

// TestNextScanPolicyFieldCyclesBetweenTheTwoFields mirrors
// nextTrivyConfigField's wrapping-cursor pattern for the modal's 2 fields
// (enabled toggle, severity threshold cycle).
func TestNextScanPolicyFieldCyclesBetweenTheTwoFields(t *testing.T) {
	t.Parallel()

	if got := nextScanPolicyField(scanPolicyFieldEnabled); got != scanPolicyFieldThreshold {
		t.Fatalf("nextScanPolicyField(Enabled) = %v, want Threshold", got)
	}
	if got := nextScanPolicyField(scanPolicyFieldThreshold); got != scanPolicyFieldEnabled {
		t.Fatalf("nextScanPolicyField(Threshold) = %v, want Enabled (wraps)", got)
	}
}

// TestOverrideFieldsForFeatureSkipsPathSecondaryForGitleaks pins that
// overrideFieldsForFeature builds trivy's 4-field set (with PathSecondary),
// skips it for gitleaks (3 fields), and skips it for signing while
// including UnsignedSelfRead instead (4 fields) -- the concrete replacement
// for the retired nextRepositoryOverrideField's runtime skipping (design.md
// Decision F).
func TestOverrideFieldsForFeatureSkipsPathSecondaryForGitleaks(t *testing.T) {
	t.Parallel()

	t.Run("trivy includes PathSecondary", func(t *testing.T) {
		t.Parallel()

		want := []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldPathSecondary, overrideFieldClear}
		got := overrideFieldsForFeature(trivyFeatureName)
		if len(got) != len(want) {
			t.Fatalf("overrideFieldsForFeature(trivy) = %v, want %v", got, want)
		}
		for i, field := range want {
			if got[i] != field {
				t.Fatalf("overrideFieldsForFeature(trivy)[%d] = %v, want %v", i, got[i], field)
			}
		}
	})

	t.Run("gitleaks skips PathSecondary and UnsignedSelfRead", func(t *testing.T) {
		t.Parallel()

		got := overrideFieldsForFeature(gitleaksFeatureName)
		for _, forbidden := range []overrideField{overrideFieldPathSecondary, overrideFieldUnsignedSelfRead} {
			for _, field := range got {
				if field == forbidden {
					t.Fatalf("overrideFieldsForFeature(gitleaks) = %v, want no %v", got, forbidden)
				}
			}
		}
	})

	// Signing likewise has no second path field (design.md Decision 11
	// piece 3's condition, generalized), but includes its own
	// UnsignedSelfRead field, plus (signing-keyless-verification) its own
	// Identities field.
	t.Run("signing skips PathSecondary but includes UnsignedSelfRead", func(t *testing.T) {
		t.Parallel()

		got := overrideFieldsForFeature(signingFeatureName)
		want := []overrideField{overrideFieldEnabled, overrideFieldPathPrimary, overrideFieldIdentities, overrideFieldUnsignedSelfRead, overrideFieldClear}
		if len(got) != len(want) {
			t.Fatalf("overrideFieldsForFeature(signing) = %v, want %v", got, want)
		}
		for i, field := range want {
			if got[i] != field {
				t.Fatalf("overrideFieldsForFeature(signing)[%d] = %v, want %v", i, got[i], field)
			}
		}
	})

	t.Run("trivy and gitleaks never include UnsignedSelfRead", func(t *testing.T) {
		t.Parallel()

		for _, feature := range []string{trivyFeatureName, gitleaksFeatureName} {
			for _, field := range overrideFieldsForFeature(feature) {
				if field == overrideFieldUnsignedSelfRead {
					t.Fatalf("feature %q includes UnsignedSelfRead, want it absent outside signing", feature)
				}
			}
		}
	})
}

// TestNoFeatureCycleSymbolsRemain is the Phase 10 task 10.1 RED test (T2.3):
// a go/parser scan over every non-test .go file in internal/tui confirms
// repositoryOverrideFeatureCycle and nextRepositoryOverrideFeatureName are
// absent -- the modal's Feature field and its cycling mechanism are deleted
// entirely (design.md Decision F, spec.md's REMOVED requirement "Signing Is
// A Third Feature Cycle Option In The Override Modal").
func TestNoFeatureCycleSymbolsRemain(t *testing.T) {
	t.Parallel()

	forbidden := map[string]bool{
		"repositoryOverrideFeatureCycle":    true,
		"nextRepositoryOverrideFeatureName": true,
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("os.ReadDir(.) = %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(".", name)
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parser.ParseFile(%s) = %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if forbidden[ident.Name] {
				t.Fatalf("%s: found forbidden symbol %q -- the feature cycle must be fully deleted (design.md Decision F)", path, ident.Name)
			}
			return true
		})
	}
}

// TestSigningPolicyModalActiveReflectsOpenField is the Phase 9 task 9.1 RED
// test: signingPolicyModal follows the same Active()-gated pattern as
// scanPolicyModal/repositoryOverrideModal (design.md Decision 11 piece 1 —
// a sibling struct, not an extension of scanPolicyModal).
func TestSigningPolicyModalActiveReflectsOpenField(t *testing.T) {
	t.Parallel()

	closed := signingPolicyModal{}
	if closed.Active() {
		t.Fatal("signingPolicyModal{}.Active() = true, want false when Open is unset")
	}

	open := signingPolicyModal{Open: true}
	if !open.Active() {
		t.Fatal("signingPolicyModal{Open: true}.Active() = false, want true")
	}
}

// TestNextSigningPolicyFieldCyclesThroughAllFiveFields is the RED test for
// the UnsignedSelfRead TUI surface, extended by signing-keyless-verification
// for the new Identities field: nextSigningPolicyField wraps
// Enabled -> UnsignedSelfRead -> AddKey -> Identities -> ClearKeys -> Enabled.
func TestNextSigningPolicyFieldCyclesThroughAllFiveFields(t *testing.T) {
	t.Parallel()

	got := signingPolicyFieldEnabled
	want := []signingPolicyField{
		signingPolicyFieldUnsignedSelfRead,
		signingPolicyFieldAddKey,
		signingPolicyFieldIdentities,
		signingPolicyFieldClearKeys,
		signingPolicyFieldEnabled,
	}
	for i, expect := range want {
		got = nextSigningPolicyField(got)
		if got != expect {
			t.Fatalf("step %d: nextSigningPolicyField() = %v, want %v", i, got, expect)
		}
	}
}

// TestNextUnsignedSelfReadValueCyclesThroughAllModes is the RED test for the
// UnsignedSelfRead TUI surface: nextUnsignedSelfReadValue wraps
// off -> pusher -> repo_push -> off, and treats "" (the wire "no exemption"
// value) the same as "off" when cycling forward from an unseeded modal.
func TestNextUnsignedSelfReadValueCyclesThroughAllModes(t *testing.T) {
	t.Parallel()

	got := "off"
	want := []string{"pusher", "repo_push", "off"}
	for i, expect := range want {
		got = nextUnsignedSelfReadValue(got)
		if got != expect {
			t.Fatalf("step %d: nextUnsignedSelfReadValue() = %q, want %q", i, got, expect)
		}
	}

	if got := nextUnsignedSelfReadValue(""); got != "pusher" {
		t.Fatalf(`nextUnsignedSelfReadValue("") = %q, want "pusher" (treats "" as "off" before advancing)`, got)
	}
}

// TestNormalizeUnsignedSelfReadMapsEmptyToOff is the RED test for the
// UnsignedSelfRead TUI surface: the wire/storage "no exemption" value ""
// always displays as the modal's canonical "off".
func TestNormalizeUnsignedSelfReadMapsEmptyToOff(t *testing.T) {
	t.Parallel()

	if got := normalizeUnsignedSelfRead(""); got != "off" {
		t.Fatalf(`normalizeUnsignedSelfRead("") = %q, want "off"`, got)
	}
	for _, value := range []string{"off", "pusher", "repo_push"} {
		if got := normalizeUnsignedSelfRead(value); got != value {
			t.Fatalf("normalizeUnsignedSelfRead(%q) = %q, want unchanged", value, got)
		}
	}
}

func TestIsAdminSessionExpired(t *testing.T) {
	t.Parallel()

	err := NewAdminSessionExpiredError("")
	if !IsAdminSessionExpired(err) {
		t.Fatal("IsAdminSessionExpired() = false, want true")
	}
	if IsAdminSessionExpired(errors.New("different error")) {
		t.Fatal("IsAdminSessionExpired() = true, want false for unrelated error")
	}
}

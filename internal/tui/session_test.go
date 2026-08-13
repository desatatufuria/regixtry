package tui

import (
	"errors"
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

func TestLogoutAdminStateClearsSessionAndViewData(t *testing.T) {
	t.Parallel()

	session, view := LogoutAdminState()
	if session.IsAuthenticated() {
		t.Fatal("LogoutAdminState() returned authenticated session")
	}
	if session.ExpiredReason != "" {
		t.Fatalf("ExpiredReason = %q, want empty", session.ExpiredReason)
	}
	if len(view.Users) != 0 || view.SelectedUserID != "" || len(view.Grants) != 0 || len(view.AdminTokens) != 0 || len(view.Features) != 0 || view.SelectedFeature != 0 || view.FeaturePage.Summary.Name != "" {
		t.Fatalf("LogoutAdminState() returned populated view state: %#v", view)
	}
}

func TestExpireAdminStateClearsViewDataAndKeepsReason(t *testing.T) {
	t.Parallel()

	session, view := ExpireAdminState("Session expired. Log in again.")
	if session.IsAuthenticated() {
		t.Fatal("ExpireAdminState() returned authenticated session")
	}
	if session.ExpiredReason != "Session expired. Log in again." {
		t.Fatalf("ExpiredReason = %q, want session expiry message", session.ExpiredReason)
	}
	if len(view.Users) != 0 || len(view.Grants) != 0 || len(view.AdminTokens) != 0 || len(view.Features) != 0 || view.FeaturePage.Summary.Name != "" {
		t.Fatalf("ExpireAdminState() returned populated view state: %#v", view)
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

// TestRepositoryOverrideModalActiveReflectsOpenField is the Phase 8 task 8.1
// RED test: repositoryOverrideModal follows the same Active()-gated pattern
// as scanPolicyModal/trivyConfigModal (design.md Decision 8 piece 1).
func TestRepositoryOverrideModalActiveReflectsOpenField(t *testing.T) {
	t.Parallel()

	closed := repositoryOverrideModal{}
	if closed.Active() {
		t.Fatal("repositoryOverrideModal{}.Active() = true, want false when Open is unset")
	}

	open := repositoryOverrideModal{Open: true, Repository: "library/alpine"}
	if !open.Active() {
		t.Fatal("repositoryOverrideModal{Open: true}.Active() = false, want true")
	}
}

// TestNextRepositoryOverrideFieldSkipsPathSecondaryForGitleaks is the Phase 8
// task 8.1 RED test: nextRepositoryOverrideField wraps through all 5 fields
// for trivy, but skips repositoryOverrideFieldPathSecondary (which gitleaks
// has no use for -- gitleaks only has ConfigPath) when the modal's Feature is
// gitleaksFeatureName (design.md Decision 8 piece 1, mirrors
// nextScanPolicyField's wrapping-cursor pattern).
func TestNextRepositoryOverrideFieldSkipsPathSecondaryForGitleaks(t *testing.T) {
	t.Parallel()

	t.Run("trivy visits every field in order and wraps", func(t *testing.T) {
		t.Parallel()

		got := repositoryOverrideFieldFeature
		want := []repositoryOverrideField{
			repositoryOverrideFieldEnabled,
			repositoryOverrideFieldPathPrimary,
			repositoryOverrideFieldPathSecondary,
			repositoryOverrideFieldClear,
			repositoryOverrideFieldFeature,
		}
		for i, expect := range want {
			got = nextRepositoryOverrideField(got, trivyFeatureName)
			if got != expect {
				t.Fatalf("step %d: nextRepositoryOverrideField() = %v, want %v", i, got, expect)
			}
		}
	})

	t.Run("gitleaks skips PathSecondary", func(t *testing.T) {
		t.Parallel()

		got := nextRepositoryOverrideField(repositoryOverrideFieldPathPrimary, gitleaksFeatureName)
		if got != repositoryOverrideFieldClear {
			t.Fatalf("nextRepositoryOverrideField(PathPrimary, gitleaks) = %v, want Clear (PathSecondary skipped)", got)
		}
	})
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

package tui

import (
	"errors"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
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

	t.Run("gitleaks skips PathSecondary and UnsignedSelfRead", func(t *testing.T) {
		t.Parallel()

		got := nextRepositoryOverrideField(repositoryOverrideFieldPathPrimary, gitleaksFeatureName)
		if got != repositoryOverrideFieldClear {
			t.Fatalf("nextRepositoryOverrideField(PathPrimary, gitleaks) = %v, want Clear (PathSecondary+UnsignedSelfRead skipped)", got)
		}
	})

	// Phase 9 task 9.15 RED: signing likewise has no second path field, so
	// nextRepositoryOverrideField's condition generalizes from
	// "feature == gitleaksFeatureName" to "feature != trivyFeatureName"
	// (design.md Decision 11 piece 3) -- signing must skip PathSecondary too,
	// but (unlike gitleaks) visits its own UnsignedSelfRead field before Clear.
	t.Run("signing skips PathSecondary but visits UnsignedSelfRead", func(t *testing.T) {
		t.Parallel()

		got := nextRepositoryOverrideField(repositoryOverrideFieldPathPrimary, signingFeatureName)
		if got != repositoryOverrideFieldUnsignedSelfRead {
			t.Fatalf("nextRepositoryOverrideField(PathPrimary, signing) = %v, want UnsignedSelfRead (PathSecondary skipped)", got)
		}
		got = nextRepositoryOverrideField(got, signingFeatureName)
		if got != repositoryOverrideFieldClear {
			t.Fatalf("nextRepositoryOverrideField(UnsignedSelfRead, signing) = %v, want Clear", got)
		}
	})

	t.Run("trivy and gitleaks never reach UnsignedSelfRead", func(t *testing.T) {
		t.Parallel()

		for _, feature := range []string{trivyFeatureName, gitleaksFeatureName} {
			field := repositoryOverrideFieldFeature
			for i := 0; i < 10; i++ {
				field = nextRepositoryOverrideField(field, feature)
				if field == repositoryOverrideFieldUnsignedSelfRead {
					t.Fatalf("feature %q reached UnsignedSelfRead, want it unreachable outside signing", feature)
				}
			}
		}
	})
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

// TestNextSigningPolicyFieldCyclesThroughAllFourFields is the RED test for
// the UnsignedSelfRead TUI surface: nextSigningPolicyField wraps
// Enabled -> UnsignedSelfRead -> AddKey -> ClearKeys -> Enabled.
func TestNextSigningPolicyFieldCyclesThroughAllFourFields(t *testing.T) {
	t.Parallel()

	got := signingPolicyFieldEnabled
	want := []signingPolicyField{
		signingPolicyFieldUnsignedSelfRead,
		signingPolicyFieldAddKey,
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

// TestNextCreateUserFieldCyclesThroughReadOnlyIndependentlyOfAdmin pins
// design.md Decision 7's "Read-only toggle beside the existing Admin
// toggle": the create/edit user form gains a distinct
// adminCreateUserFieldIsReadOnly focus stop, between IsAdmin and Enabled,
// and nextCreateUserField's wrapping-cursor visits it on its own.
func TestNextCreateUserFieldCyclesThroughReadOnlyIndependentlyOfAdmin(t *testing.T) {
	t.Parallel()

	if got := nextCreateUserField(adminCreateUserFieldUsername); got != adminCreateUserFieldPassword {
		t.Fatalf("nextCreateUserField(Username) = %v, want Password", got)
	}
	if got := nextCreateUserField(adminCreateUserFieldPassword); got != adminCreateUserFieldIsAdmin {
		t.Fatalf("nextCreateUserField(Password) = %v, want IsAdmin", got)
	}
	if got := nextCreateUserField(adminCreateUserFieldIsAdmin); got != adminCreateUserFieldIsReadOnly {
		t.Fatalf("nextCreateUserField(IsAdmin) = %v, want IsReadOnly", got)
	}
	if got := nextCreateUserField(adminCreateUserFieldIsReadOnly); got != adminCreateUserFieldEnabled {
		t.Fatalf("nextCreateUserField(IsReadOnly) = %v, want Enabled", got)
	}
	if got := nextCreateUserField(adminCreateUserFieldEnabled); got != adminCreateUserFieldUsername {
		t.Fatalf("nextCreateUserField(Enabled) = %v, want Username (wraps)", got)
	}
}

// TestCreateUserFormDefaultsReadOnlyToFalseAndToggleIsIndependentOfAdmin pins
// the same requirement's default state and independence: a fresh form has
// IsReadOnly false, and toggling the Admin field never flips IsReadOnly (and
// vice versa) -- the two booleans are separate flags, not a shared role enum.
func TestCreateUserFormDefaultsReadOnlyToFalseAndToggleIsIndependentOfAdmin(t *testing.T) {
	t.Parallel()

	form := newAdminViewState().CreateUserForm
	if form.IsReadOnly {
		t.Fatal("newAdminViewState().CreateUserForm.IsReadOnly = true, want false")
	}

	model := Model{}
	model.adminView.CreateUserForm.Focus = adminCreateUserFieldIsAdmin
	model.toggleCreateUserField()
	if !model.adminView.CreateUserForm.IsAdmin {
		t.Fatal("toggleCreateUserField() on IsAdmin focus did not set IsAdmin")
	}
	if model.adminView.CreateUserForm.IsReadOnly {
		t.Fatal("toggleCreateUserField() on IsAdmin focus unexpectedly set IsReadOnly")
	}

	model.adminView.CreateUserForm.Focus = adminCreateUserFieldIsReadOnly
	model.toggleCreateUserField()
	if !model.adminView.CreateUserForm.IsReadOnly {
		t.Fatal("toggleCreateUserField() on IsReadOnly focus did not set IsReadOnly")
	}
	if !model.adminView.CreateUserForm.IsAdmin {
		t.Fatal("toggleCreateUserField() on IsReadOnly focus unexpectedly cleared IsAdmin")
	}
}

// TestNewAdminViewStateSeedsAdminRobotFormDefaultsAndEmptyList is task 5.1's
// RED test (design.md Decision 7's robot screens): a fresh AdminViewState
// seeds CreateRobotForm.Role to RepoRoleReader -- mirroring GrantForm/
// RepoAdminGrantForm's own zero-value convention, so the create-robot form
// never starts on the unrestricted repo-admin role by accident -- and starts
// with no robots listed or selected.
func TestNewAdminViewStateSeedsAdminRobotFormDefaultsAndEmptyList(t *testing.T) {
	t.Parallel()

	view := newAdminViewState()
	if view.CreateRobotForm.Role != domainauth.RepoRoleReader {
		t.Fatalf("CreateRobotForm.Role = %v, want RepoRoleReader", view.CreateRobotForm.Role)
	}
	if view.CreateRobotForm.Focus != adminCreateRobotFieldName {
		t.Fatalf("CreateRobotForm.Focus = %v, want adminCreateRobotFieldName", view.CreateRobotForm.Focus)
	}
	if len(view.Robots) != 0 || view.SelectedRobot != 0 {
		t.Fatalf("fresh AdminViewState has populated robot data: %#v", view)
	}
}

// TestNextAdminRobotFormFieldCyclesThroughAllFourFields pins the create-robot
// form's field order: Name -> Repository -> Role -> TTL, wrapping to Name.
func TestNextAdminRobotFormFieldCyclesThroughAllFourFields(t *testing.T) {
	t.Parallel()

	if got := nextCreateRobotField(adminCreateRobotFieldName); got != adminCreateRobotFieldRepository {
		t.Fatalf("nextCreateRobotField(Name) = %v, want Repository", got)
	}
	if got := nextCreateRobotField(adminCreateRobotFieldRepository); got != adminCreateRobotFieldRole {
		t.Fatalf("nextCreateRobotField(Repository) = %v, want Role", got)
	}
	if got := nextCreateRobotField(adminCreateRobotFieldRole); got != adminCreateRobotFieldTTL {
		t.Fatalf("nextCreateRobotField(Role) = %v, want TTL", got)
	}
	if got := nextCreateRobotField(adminCreateRobotFieldTTL); got != adminCreateRobotFieldName {
		t.Fatalf("nextCreateRobotField(TTL) = %v, want Name (wraps)", got)
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

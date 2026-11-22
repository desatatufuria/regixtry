package tui

import (
	"errors"
	"strings"
	"time"

	"regixtry/internal/ports"
)

const AdminSessionExpiredReasonExpired = "Session expired. Log in again."

type trivyConfigField int

const (
	trivyConfigFieldScheduleEnabled trivyConfigField = iota
	trivyConfigFieldInterval
	trivyConfigFieldTimeout
	trivyConfigFieldRegistryReachableURL
	trivyConfigFieldMaxConcurrency
)

type trivyConfigModal struct {
	Open                 bool
	Focus                trivyConfigField
	ScheduleEnabled      bool
	Interval             string
	Timeout              string
	RegistryReachableURL string
	MaxConcurrency       string
	Error                string
}

func (m trivyConfigModal) Active() bool {
	return m.Open
}

// gitleaksConfigField identifies which of gitleaksConfigModal's 3 fields has
// focus. Unlike trivyConfigField, there is no ScheduleEnabled/Interval pair
// (gitleaks scans immutable content once and never needs periodic
// rescanning, unlike Trivy's CVE database) and no
// RegistryReachableURL/TLS fields (gitleaks scans locally-staged blobs via
// BlobStore, it never pulls from the registry over HTTP).
type gitleaksConfigField int

const (
	gitleaksConfigFieldEnabled gitleaksConfigField = iota
	gitleaksConfigFieldTimeout
	gitleaksConfigFieldMaxConcurrency
)

// gitleaksConfigModal is gitleaks' own global config editor, a sibling
// struct to trivyConfigModal (not an extension of it) mirroring its exact
// shape at a narrower 3-field scope.
type gitleaksConfigModal struct {
	Open           bool
	Focus          gitleaksConfigField
	Enabled        bool
	Timeout        string
	MaxConcurrency string
	Error          string
}

func (m gitleaksConfigModal) Active() bool {
	return m.Open
}

// scanPolicyField identifies which of scanPolicyModal's 2 fields has focus.
type scanPolicyField int

const (
	scanPolicyFieldEnabled scanPolicyField = iota
	scanPolicyFieldThreshold
)

// scanPolicyModal is the vulnerability policy gate's own admin modal
// (design.md Decision 6 — a sibling struct, deliberately NOT an extension
// of trivyConfigModal, so the two features stay separately configurable
// surfaces per spec's "separate surface" requirement).
type scanPolicyModal struct {
	Open              bool
	Focus             scanPolicyField
	Enabled           bool
	SeverityThreshold string
	Error             string
}

func (m scanPolicyModal) Active() bool {
	return m.Open
}

// nextScanPolicyField cycles between the modal's 2 fields with a wrapping
// cursor, mirroring nextTrivyConfigField.
func nextScanPolicyField(field scanPolicyField) scanPolicyField {
	if field >= scanPolicyFieldThreshold {
		return scanPolicyFieldEnabled
	}
	return field + 1
}

// signingPolicyField identifies which of signingPolicyModal's 3 fields has
// focus. Unlike scanPolicyField's 2-field shape, signing's global policy
// has a growable list of trusted keys, so the third field (ClearKeys) is an
// action row rather than an input, mirroring
// repositoryOverrideFieldClear's shape (design.md Decision 11 piece 1).
type signingPolicyField int

const (
	signingPolicyFieldEnabled signingPolicyField = iota
	signingPolicyFieldUnsignedSelfRead
	signingPolicyFieldAddKey
	// signingPolicyFieldIdentities is the trusted-identity list's own focus
	// position (signing-keyless-verification), a sibling of AddKey rather
	// than a variant of it: an identity has its own add/select/delete
	// widget (trustedIdentityList, trusted_identity_list.go), not the
	// PEM-paste shape trustedKeyList uses.
	signingPolicyFieldIdentities
	signingPolicyFieldClearKeys // action row, not an input
)

// signingPolicyModal is the image-signing content-trust gate's own admin
// modal, a sibling of scanPolicyModal (design.md Decision 11 piece 1 — NOT
// an extension of it, and NOT an extension of repositoryOverrideModal
// either). Fingerprints is a read-only, derived projection of the currently
// stored trusted keys (SHA-256/12, never the raw PEM -- the modal never
// displays key material) so the operator can confirm what is configured
// without a multi-line PEM viewport. AddKey is one PEM, entered as a single
// line (any whitespace arrangement -- signing.NormalizePublicKeyPEM
// reconstructs canonical PEM either way), submitted with Enter to append it
// to the stored key set. UnsignedSelfRead mirrors
// ports.SigningPolicySettings.UnsignedSelfRead, always held here as one of
// unsignedSelfReadCycle's three canonical values (never ""), cycled with
// Space like repositoryOverrideModal.Feature.
type signingPolicyModal struct {
	Open             bool
	Focus            signingPolicyField
	Enabled          bool
	UnsignedSelfRead string // "off" | "pusher" | "repo_push" -- see type doc comment
	// Keys replaces the retired AddKey/Fingerprints pair (signing-key-
	// management change): trustedKeyList (trusted_key_list.go) owns
	// add/select/delete for the modal's trusted-key list directly, growing
	// or shrinking the list live rather than staging one pending AddKey
	// string that only ever appended.
	Keys trustedKeyList
	// Identities is Keys' sibling for keyless (Fulcio/OIDC) trust anchors
	// (signing-keyless-verification): trustedIdentityList owns add/select/
	// delete for the modal's trusted-identity list directly, mirroring Keys'
	// own growable-list shape.
	Identities trustedIdentityList
	Loading    bool
	Error      string
}

func (m signingPolicyModal) Active() bool {
	return m.Open
}

// nextSigningPolicyField cycles between the modal's 4 fields with a
// wrapping cursor, mirroring nextScanPolicyField/nextRepositoryOverrideField.
func nextSigningPolicyField(field signingPolicyField) signingPolicyField {
	if field >= signingPolicyFieldClearKeys {
		return signingPolicyFieldEnabled
	}
	return field + 1
}

// unsignedSelfReadCycle is signingPolicyModal/repositoryOverrideModal's
// UnsignedSelfRead field cycle order, mirroring
// repositoryOverrideFeatureCycle's shape. "" (the wire/storage value for "no
// exemption") is never a cycle member -- normalizeUnsignedSelfRead maps it
// to "off" before it ever reaches the modal.
var unsignedSelfReadCycle = []string{"off", "pusher", "repo_push"}

// normalizeUnsignedSelfRead maps ports.SigningPolicySettings/SigningOverride's
// wire value "" (no exemption configured) onto the modal's canonical "off",
// so the modal field is always one of unsignedSelfReadCycle's three values.
func normalizeUnsignedSelfRead(value string) string {
	if value == "" {
		return "off"
	}
	return value
}

// nextUnsignedSelfReadValue cycles the UnsignedSelfRead field through
// unsignedSelfReadCycle, wrapping back to the first entry, normalizing ""
// first so cycling from an unseeded modal still lands on a real value.
func nextUnsignedSelfReadValue(value string) string {
	normalized := normalizeUnsignedSelfRead(value)
	for index, candidate := range unsignedSelfReadCycle {
		if candidate == normalized {
			return unsignedSelfReadCycle[(index+1)%len(unsignedSelfReadCycle)]
		}
	}
	return unsignedSelfReadCycle[0]
}

// adminScanHistoryTabKind identifies one feature's tab inside the scan
// history modal (design.md "Ordered tab slice with a wrapping cursor").
type adminScanHistoryTabKind string

const (
	adminScanHistoryTabVulnerabilities adminScanHistoryTabKind = "vulnerabilities"
	adminScanHistoryTabLeaks           adminScanHistoryTabKind = "leaks"
)

// adminScanHistoryTab is one entry in the scan history modal's ordered,
// extensible tab set. A third feature's tab is one more slice entry, no
// restructuring (spec.md "Per-Feature Modal Tabs").
type adminScanHistoryTab struct {
	Kind  adminScanHistoryTabKind
	Label string
}

// newAdminScanHistoryTabs returns the modal's default ordered tab set.
// Leaks is always present alongside Vulnerabilities (design.md interface
// comment on adminScanHistoryModal.Tabs).
func newAdminScanHistoryTabs() []adminScanHistoryTab {
	return []adminScanHistoryTab{
		{Kind: adminScanHistoryTabVulnerabilities, Label: "Vulnerabilities"},
		{Kind: adminScanHistoryTabLeaks, Label: "Leaks"},
	}
}

// adminScanHistoryModal is the state for the Repository Alerts drill-down
// modal, rendered as a true floating overlay via compositeOverlay
// (claude-handoff.md), opened by Enter on a Repository Alerts summary row
// (spec.md "Repository Alert Drill-Down Opens History Modal").
type adminScanHistoryModal struct {
	Open       bool
	Repository string
	Tabs       []adminScanHistoryTab
	ActiveTab  int
	Runs       []ports.ScanRun // newest first
	Cursor     int             // which execution in history (Left/Right)
	Detail     ports.ScanRunDetail
	Secrets    []ports.SecretFinding
	Loading    bool
	Error      string
	// FindingCursor is which row is selected WITHIN the currently active
	// tab's own table (Up/Down) -- distinct from Cursor, which navigates
	// between executions (Left/Right). Reset to 0 on every tab switch and
	// every execution page so it never points past the end of a freshly
	// loaded, possibly shorter list.
	FindingCursor int
}

func (m adminScanHistoryModal) Active() bool {
	return m.Open
}

type AdminSession struct {
	Username      string
	BearerToken   string
	ExpiresAt     time.Time
	ExpiredReason string
}

type AdminSessionExpiredError struct {
	Reason string
}

func NewAdminSessionExpiredError(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = AdminSessionExpiredReasonExpired
	}
	return AdminSessionExpiredError{Reason: reason}
}

func IsAdminSessionExpired(err error) bool {
	var target AdminSessionExpiredError
	return errors.As(err, &target)
}

func (e AdminSessionExpiredError) Error() string {
	if strings.TrimSpace(e.Reason) == "" {
		return AdminSessionExpiredReasonExpired
	}
	return e.Reason
}

func (s AdminSession) IsAuthenticated() bool {
	return strings.TrimSpace(s.BearerToken) != ""
}

func (s AdminSession) IsExpired(now time.Time) bool {
	if !s.IsAuthenticated() || s.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(s.ExpiresAt)
}

func (s AdminSession) Remaining(now time.Time) time.Duration {
	if s.ExpiresAt.IsZero() {
		return 0
	}
	if s.IsExpired(now) {
		return 0
	}
	return s.ExpiresAt.Sub(now)
}

func LogoutAdminState() AdminSession {
	return AdminSession{}
}

func ExpireAdminState(reason string) AdminSession {
	return AdminSession{ExpiredReason: strings.TrimSpace(reason)}
}

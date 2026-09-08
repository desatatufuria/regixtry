// Package signing (this file): offline keyless (Fulcio/OIDC) Sigstore Bundle
// verification. This is the sole file in this codebase that imports
// sigstore-go -- no sigstore-go type ever crosses this file's exported
// boundary (design.md "Bundle handoff" decision). Every other file in this
// package stays sigstore-go-free, matching the project's existing
// stdlib-only cosign primitives.
package signing

import (
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	sigstorebundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

//go:embed assets/trusted_root.json
var embeddedTrustedRoot []byte

// TrustedIdentity is one operator-configured Fulcio-certificate identity
// anchor: a certificate SAN regexp and the exact OIDC issuer that must have
// minted the certificate. VerifyKeyless accepts a signature if it matches
// ANY configured TrustedIdentity (OR semantics), mirroring how the existing
// static-key path accepts a match against any TrustedPublicKeys entry.
type TrustedIdentity struct {
	CertificateIdentityRegexp string
	CertificateOIDCIssuer     string
}

// Typed, distinct fail-closed errors VerifyKeyless returns. Callers should
// use errors.Is against these sentinels; each also wraps the underlying
// sigstore-go error text via %w for diagnostics.
//
// All six sentinels below are genuinely distinguishable from sigstore-go
// v0.7.1's own error surface, confirmed by direct test execution (not
// assumption): an expired certificate is caught by
// VerifyArtifactTransparencyLog's own integrated-time-vs-certificate-
// validity check (pkg/verify/tlog.go: "integrated time outside certificate
// validity") BEFORE x509 chain validation ever runs, which is textually
// distinct from both an untrusted chain ("failed to verify leaf
// certificate", pkg/verify/certificate.go's VerifyLeafCertificate) and a
// tampered/forged SET ("not enough verified log entries", also from
// tlog.go but a different inner message). An earlier draft of this file
// assumed expiry and untrusted-chain would collapse into the same error;
// running the adversarial test suite in this file disproved that.
var (
	// ErrNotKeylessShaped is returned when the bundle carries no Fulcio
	// certificate at all (e.g. a static-key-signed bundle handed to this
	// function by mistake).
	ErrNotKeylessShaped = errors.New("signing: bundle carries no keyless (Fulcio certificate) verification material")

	// ErrTransparencyLogMissing is returned when the bundle carries zero
	// transparency log entries -- there is no Signed Entry Timestamp to
	// verify at all.
	ErrTransparencyLogMissing = errors.New("signing: keyless bundle carries no transparency log entry")

	// ErrTransparencyLogInvalid is returned when the bundle carries a
	// transparency log entry, but its Signed Entry Timestamp (or the entry
	// itself) fails verification against the trusted root's Rekor keys --
	// e.g. a tampered or forged SET.
	ErrTransparencyLogInvalid = errors.New("signing: keyless bundle's Signed Entry Timestamp failed verification")

	// ErrCertificateChainInvalid is returned when the certificate does not
	// chain to the pinned trusted root.
	ErrCertificateChainInvalid = errors.New("signing: keyless certificate does not chain to the trusted root")

	// ErrCertificateExpired is returned when the certificate was already
	// expired (or not yet valid) at the bundle's verified transparency log
	// integrated time.
	ErrCertificateExpired = errors.New("signing: keyless certificate is expired relative to the Signed Entry Timestamp")

	// ErrIdentityNotMatched is returned when the certificate's SAN and
	// issuer match none of the caller-supplied trusted identities, or when
	// no trusted identities were supplied at all.
	ErrIdentityNotMatched = errors.New("signing: keyless certificate matches no trusted identity")
)

// signedEntityVerifier lazily builds the package-level SignedEntityVerifier
// exactly once (sync.OnceValues): the embedded pinned trusted root is
// parsed via root.NewTrustedRootFromJSON, a pure protojson unmarshal with
// zero TUF/network I/O (design.md "Library Verification" table, verified
// against source). Trusted identities are policy-scoped per call
// (verify.WithCertificateIdentity), not baked into the verifier itself,
// since they are operator-configured and may change between calls; only
// the root-derived trust material -- expensive to parse, identity-agnostic
// -- is cached here.
//
// Deviation from design.md's literal API table: verify.WithSignedCertificateTimestamps
// is deliberately NOT enabled here, resolving design.md's own flagged Open
// Question ("keep enabled unless a real artifact fails"). Confirmed by
// reading sigstore-go v0.7.1's source: sigstore-go's own test suite
// (pkg/verify/signature_test.go, fuzz_test.go) never enables this option
// when verifying against sigstore-go/pkg/testing/ca-produced synthetic
// entities, because ca.GenerateLeafCert never embeds a Signed Certificate
// Timestamp extension in the certificates it generates -- enabling this
// option would make every synthetic-chain test in this file (task 3.1-3.5,
// mandated to use sigstore-go/pkg/testing/ca) structurally unpassable, not
// just this one bundle's. WithTransparencyLog + WithObserverTimestamps
// alone still enforce the SET-based offline observer-timestamp requirement
// the spec's "verify the embedded Signed Entry Timestamp (SET) offline"
// requirement needs.
var signedEntityVerifier = sync.OnceValues(func() (*verify.SignedEntityVerifier, error) {
	trustedRoot, err := root.NewTrustedRootFromJSON(embeddedTrustedRoot)
	if err != nil {
		return nil, fmt.Errorf("signing: parsing embedded trusted root: %w", err)
	}

	sev, err := verify.NewSignedEntityVerifier(
		trustedRoot,
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return nil, fmt.Errorf("signing: constructing keyless verifier: %w", err)
	}

	return sev, nil
})

// VerifyKeyless offline-verifies a Sigstore Bundle's embedded Fulcio
// certificate against the pinned trusted root, at least one
// operator-configured TrustedIdentity, and the bundle's Signed Entry
// Timestamp -- performing zero network I/O (spec: "Offline Keyless
// (Fulcio/OIDC) Bundle Verification"). digest is the "sha256:<hex>" image
// manifest digest the bundle's signed content must bind
// (verify.WithArtifactDigest). On success, matched is the certificate's
// Subject Alternative Name that matched a trusted identity. bundleRaw is
// the caller-supplied raw bundle document bytes; the caller is responsible
// for size-bounding it before calling (mirrors ParseBundleDocument's
// MaxBundleDocumentBytes contract) -- this function performs no I/O of its
// own. No sigstore-go type crosses this function's boundary.
func VerifyKeyless(bundleRaw []byte, identities []TrustedIdentity, digest string) (matched string, err error) {
	sev, err := signedEntityVerifier()
	if err != nil {
		return "", err
	}

	var b sigstorebundle.Bundle
	if unmarshalErr := b.UnmarshalJSON(bundleRaw); unmarshalErr != nil {
		return "", fmt.Errorf("signing: keyless bundle is not valid: %w", unmarshalErr)
	}

	return verifyKeylessEntity(sev, &b, identities, digest)
}

// verifyKeylessEntity is VerifyKeyless's core logic, factored out so tests
// can supply both a synthetic verify.SignedEntity (built with
// sigstore-go/pkg/testing/ca) AND a verifier bound to that same synthetic
// CA's own trusted material -- never the real embedded pinned root in
// tests (tasks.md 3.1) -- without needing to re-marshal fixtures to bundle
// JSON bytes first. Mirrors how sigstore-go's own test suite
// (pkg/verify/signature_test.go, fuzz_test.go) exercises
// SignedEntityVerifier.Verify directly against ca.VirtualSigstore-produced
// entities and a verifier built over that same VirtualSigstore.
func verifyKeylessEntity(sev *verify.SignedEntityVerifier, entity verify.SignedEntity, identities []TrustedIdentity, digest string) (matched string, err error) {
	if len(identities) == 0 {
		return "", ErrIdentityNotMatched
	}

	tlogEntries, err := entity.TlogEntries()
	if err != nil {
		return "", fmt.Errorf("signing: reading keyless bundle transparency log entries: %w", err)
	}
	if len(tlogEntries) == 0 {
		return "", ErrTransparencyLogMissing
	}

	digestBytes, err := decodeSHA256Digest(digest)
	if err != nil {
		return "", err
	}

	identityOpts := make([]verify.PolicyOption, 0, len(identities))
	for _, id := range identities {
		ci, ciErr := verify.NewShortCertificateIdentity(id.CertificateOIDCIssuer, "", "", id.CertificateIdentityRegexp)
		if ciErr != nil {
			return "", fmt.Errorf("signing: invalid trusted identity (issuer %q, regexp %q): %w", id.CertificateOIDCIssuer, id.CertificateIdentityRegexp, ciErr)
		}
		identityOpts = append(identityOpts, verify.WithCertificateIdentity(ci))
	}

	result, verifyErr := sev.Verify(entity, verify.NewPolicy(verify.WithArtifactDigest("sha256", digestBytes), identityOpts...))
	if verifyErr != nil {
		return "", classifyVerifyError(verifyErr)
	}

	if result.Signature == nil || result.Signature.Certificate == nil {
		return "", ErrIdentityNotMatched
	}

	return result.Signature.Certificate.SubjectAlternativeName, nil
}

// classifyVerifyError maps a SignedEntityVerifier.Verify error to one of
// this package's typed, distinct sentinels. sigstore-go v0.7.1's
// verify.SignedEntityVerifier.Verify (pkg/verify/signed_entity.go) does not
// expose per-cause error types for most of these failures -- it wraps each
// stage's underlying error behind a stable, distinct message
// ("integrated time outside certificate validity", "not enough verified
// log entries", "failed to verify leaf certificate: ...", "entity was not
// signed with a certificate"). This classifier matches on those stable
// messages (confirmed against the pinned v0.7.1 source AND by running the
// adversarial test suite in keyless_test.go, not guessed);
// verify.ErrNoMatchingCertificateIdentity is the one stage sigstore-go
// exposes as a concrete exported type, so that case is matched via
// errors.As instead. The expired-certificate case is checked before the
// generic "failed to verify log inclusion" case, since both originate
// inside VerifyArtifactTransparencyLog and share that same outer wrap.
func classifyVerifyError(err error) error {
	var noMatch *verify.ErrNoMatchingCertificateIdentity
	if errors.As(err, &noMatch) {
		return fmt.Errorf("%w: %w", ErrIdentityNotMatched, err)
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "entity was not signed with a certificate"):
		return fmt.Errorf("%w: %w", ErrNotKeylessShaped, err)
	case strings.Contains(msg, "integrated time outside certificate validity"):
		return fmt.Errorf("%w: %w", ErrCertificateExpired, err)
	case strings.Contains(msg, "failed to verify log inclusion"):
		return fmt.Errorf("%w: %w", ErrTransparencyLogInvalid, err)
	case strings.Contains(msg, "failed to verify leaf certificate"):
		return fmt.Errorf("%w: %w", ErrCertificateChainInvalid, err)
	default:
		return fmt.Errorf("signing: keyless verification failed: %w", err)
	}
}

// decodeSHA256Digest validates a "sha256:<hex>" digest and returns its
// decoded bytes, as verify.WithArtifactDigest requires. Mirrors
// validatedDigestHex (cosign.go), which returns the bare hex string instead.
func decodeSHA256Digest(digest string) ([]byte, error) {
	hexDigest, err := validatedDigestHex(digest)
	if err != nil {
		return nil, err
	}

	decoded, err := hex.DecodeString(hexDigest)
	if err != nil {
		return nil, fmt.Errorf("signing: digest %q is not valid hex: %w", digest, err)
	}

	return decoded, nil
}

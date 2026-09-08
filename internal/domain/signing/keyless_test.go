package signing

import (
	"errors"
	"go/build"
	"go/parser"
	"go/token"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testDigestHex   = "4a668fd22601acf91adb14ae57c2fe45a61010c709b3c48ff12b0b9187147640"
	testDigest      = "sha256:" + testDigestHex
	testIdentitySAN = "https://github.com/example/repo/.github/workflows/release.yml@refs/heads/main"
	testIssuer      = "https://token.actions.githubusercontent.com"
)

// testTrustedIdentity is the single trusted identity most tests configure:
// exact-match SAN regexp, matching issuer.
func testTrustedIdentity() []TrustedIdentity {
	return []TrustedIdentity{{
		CertificateIdentityRegexp: "^" + testIdentitySAN + "$",
		CertificateOIDCIssuer:     testIssuer,
	}}
}

// TestVerifyKeylessEntity_MatchingIdentityVerifiesOffline is tasks.md 3.1:
// a synthetic Fulcio-shaped chain, signed and logged by the same
// VirtualSigstore the verifier trusts, with a SAN+issuer matching a
// configured TrustedIdentity, verifies successfully.
func TestVerifyKeylessEntity_MatchingIdentityVerifiesOffline(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)
	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{})

	matched, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), testDigest)
	if err != nil {
		t.Fatalf("verifyKeylessEntity() error = %v, want nil", err)
	}
	if matched != testIdentitySAN {
		t.Fatalf("matched = %q, want %q", matched, testIdentitySAN)
	}
}

// TestVerifyKeylessEntity_UntrustedChainFailsClosed is tasks.md 3.2: a
// certificate issued by a completely different (untrusted) synthetic CA
// fails closed with ErrCertificateChainInvalid.
func TestVerifyKeylessEntity_UntrustedChainFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	untrustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)

	// Signed AND logged entirely by untrustedCA: its cert does not chain to
	// trustedCA's root, and (since VerifyArtifactTransparencyLog looks up
	// the tlog entry's LogKeyID in the verifier's own trusted material) its
	// tlog entry would not verify against trustedCA's Rekor key either --
	// but chain validation is checked first, so this exercises the
	// untrusted-chain path specifically.
	entity := buildKeylessEntity(t, untrustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{
		tlogEntriesFrom: trustedCA,
	})

	_, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), testDigest)
	if err == nil {
		t.Fatal("verifyKeylessEntity() error = nil, want ErrCertificateChainInvalid")
	}
	if !errors.Is(err, ErrCertificateChainInvalid) {
		t.Fatalf("verifyKeylessEntity() error = %v, want errors.Is(..., ErrCertificateChainInvalid)", err)
	}
}

// TestVerifyKeylessEntity_WrongIssuerFailsClosed is tasks.md 3.3: a
// matching SAN but a certificate minted by a different OIDC issuer than
// any configured TrustedIdentity fails closed with ErrIdentityNotMatched,
// distinctly from the chain-failure error.
func TestVerifyKeylessEntity_WrongIssuerFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)

	const wrongIssuer = "https://wrong-issuer.example"
	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, wrongIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{})

	_, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), testDigest)
	if err == nil {
		t.Fatal("verifyKeylessEntity() error = nil, want ErrIdentityNotMatched")
	}
	if !errors.Is(err, ErrIdentityNotMatched) {
		t.Fatalf("verifyKeylessEntity() error = %v, want errors.Is(..., ErrIdentityNotMatched)", err)
	}
	if errors.Is(err, ErrCertificateChainInvalid) {
		t.Fatalf("verifyKeylessEntity() error = %v, must NOT also match ErrCertificateChainInvalid (distinct failure)", err)
	}
}

// TestVerifyKeylessEntity_ExpiredCertificateFailsClosed is tasks.md 3.4: a
// certificate that has already expired relative to the tlog's (trusted)
// integrated timestamp fails closed with the dedicated ErrCertificateExpired
// sentinel -- genuinely distinct from ErrCertificateChainInvalid (confirmed
// by running this test: sigstore-go's own VerifyArtifactTransparencyLog
// catches this case via its own integrated-time-vs-certificate-validity
// check, before x509 chain validation ever runs).
func TestVerifyKeylessEntity_ExpiredCertificateFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)

	// The leaf cert is valid for exactly 10 minutes from its own generation
	// time (ca.GenerateLeafCert, sigstore-go source). An integratedTime an
	// hour past "now" makes the (already-generated, already-fixed-validity)
	// cert observably expired relative to the SET-derived observer
	// timestamp used for chain validation.
	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{
		integratedTime: time.Now().Add(time.Hour),
	})

	_, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), testDigest)
	if err == nil {
		t.Fatal("verifyKeylessEntity() error = nil, want ErrCertificateExpired")
	}
	if !errors.Is(err, ErrCertificateExpired) {
		t.Fatalf("verifyKeylessEntity() error = %v, want errors.Is(..., ErrCertificateExpired)", err)
	}
	if errors.Is(err, ErrCertificateChainInvalid) || errors.Is(err, ErrTransparencyLogInvalid) {
		t.Fatalf("verifyKeylessEntity() error = %v, must NOT also match chain/tlog-invalid sentinels (distinct failure)", err)
	}
}

// TestVerifyKeylessEntity_TamperedSETFailsClosed is tasks.md 3.5 (tampered
// half): a tlog entry IS present, but its Signed Entry Timestamp was
// produced by a Rekor key the trusted material does not recognize -- a
// real member of the "tampered/forged SET" failure class. Must fail
// closed with ErrTransparencyLogInvalid, distinctly from
// ErrTransparencyLogMissing (the "absent entirely" case) and from the
// chain/issuer failures above.
func TestVerifyKeylessEntity_TamperedSETFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	foreignRekorCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)

	// Cert legitimately chains to trustedCA, but the tlog entry (and its
	// SET) is generated by a DIFFERENT VirtualSigstore's Rekor key --
	// trustedCA's RekorLogs() map has no entry for that key, so
	// VerifyArtifactTransparencyLog's lookup skips it (sigstore-go source:
	// "skip entries the trust root cannot verify"), leaving zero verified
	// entries.
	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{
		tlogEntriesFrom: foreignRekorCA,
	})

	_, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), testDigest)
	if err == nil {
		t.Fatal("verifyKeylessEntity() error = nil, want ErrTransparencyLogInvalid")
	}
	if !errors.Is(err, ErrTransparencyLogInvalid) {
		t.Fatalf("verifyKeylessEntity() error = %v, want errors.Is(..., ErrTransparencyLogInvalid)", err)
	}
	if errors.Is(err, ErrTransparencyLogMissing) {
		t.Fatalf("verifyKeylessEntity() error = %v, must NOT also match ErrTransparencyLogMissing (distinct failure)", err)
	}
	if errors.Is(err, ErrCertificateChainInvalid) || errors.Is(err, ErrIdentityNotMatched) {
		t.Fatalf("verifyKeylessEntity() error = %v, must NOT also match chain/issuer sentinels (distinct failure)", err)
	}
}

// TestVerifyKeylessEntity_MissingSETFailsClosed is tasks.md 3.5 (missing
// half): a bundle carrying zero tlog entries at all fails closed with
// ErrTransparencyLogMissing, distinctly from ErrTransparencyLogInvalid.
func TestVerifyKeylessEntity_MissingSETFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)

	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{
		omitTlogEntries: true,
	})

	_, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), testDigest)
	if err == nil {
		t.Fatal("verifyKeylessEntity() error = nil, want ErrTransparencyLogMissing")
	}
	if !errors.Is(err, ErrTransparencyLogMissing) {
		t.Fatalf("verifyKeylessEntity() error = %v, want errors.Is(..., ErrTransparencyLogMissing)", err)
	}
	if errors.Is(err, ErrTransparencyLogInvalid) {
		t.Fatalf("verifyKeylessEntity() error = %v, must NOT also match ErrTransparencyLogInvalid (distinct failure)", err)
	}
}

// TestVerifyKeylessEntity_NoTrustedIdentitiesFailsClosed covers the
// zero-identities precondition: VerifyKeyless must never be called with no
// anchors to check against (the app layer's keys-OR-identities precondition
// is Phase 6's job), but this package still fails closed defensively rather
// than trivially "verifying" against an empty policy.
func TestVerifyKeylessEntity_NoTrustedIdentitiesFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)
	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{})

	_, err := verifyKeylessEntity(sev, entity, nil, testDigest)
	if !errors.Is(err, ErrIdentityNotMatched) {
		t.Fatalf("verifyKeylessEntity() error = %v, want errors.Is(..., ErrIdentityNotMatched)", err)
	}
}

// TestVerifyKeylessEntity_MismatchedDigestFailsClosed confirms the
// verified content is actually bound to the pulled digest, not just any
// digest: a bundle built for one digest must not verify against a
// different one.
func TestVerifyKeylessEntity_MismatchedDigestFailsClosed(t *testing.T) {
	t.Parallel()

	trustedCA := newSyntheticVirtualSigstore(t)
	sev := newSyntheticVerifier(t, trustedCA)
	entity := buildKeylessEntity(t, trustedCA, testIdentitySAN, testIssuer, inTotoStatementFor(testDigestHex), keylessFixtureOptions{})

	const otherDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	_, err := verifyKeylessEntity(sev, entity, testTrustedIdentity(), otherDigest)
	if err == nil {
		t.Fatal("verifyKeylessEntity() error = nil, want error (digest does not match the signed statement's subject)")
	}
}

// TestVerifyKeyless_UnmarshalsRawBytesAndDelegates confirms the public
// VerifyKeyless([]byte, ...) entrypoint's own boundary: malformed JSON is a
// hard error, distinct from any verification-policy failure, and never
// panics on a well-formed-but-not-keyless-shaped real bundle document
// (the real static-key fixture captured in testdata/README.md).
func TestVerifyKeyless_UnmarshalsRawBytesAndDelegates(t *testing.T) {
	t.Parallel()

	t.Run("malformed JSON is a hard error", func(t *testing.T) {
		t.Parallel()

		_, err := VerifyKeyless([]byte(`not json`), testTrustedIdentity(), testDigest)
		if err == nil {
			t.Fatal("VerifyKeyless(malformed) error = nil, want error")
		}
	})

	t.Run("real static-key fixture is not keyless-shaped", func(t *testing.T) {
		t.Parallel()

		raw := readTestdataFixture(t, "bundle-document.json")

		_, err := VerifyKeyless(raw, testTrustedIdentity(), realBundleImageDigest)
		if err == nil {
			t.Fatal("VerifyKeyless(real static-key fixture) error = nil, want error (no Fulcio certificate)")
		}
	})
}

// TestKeylessImports_NoNetHTTP is tasks.md 3.6: a structural,
// zero-network-I/O assertion. It checks the DIRECT (not transitive)
// imports of both this file's own package (keyless.go's home) and
// sigstore-go/pkg/verify (the package keyless.go actually calls into for
// verification) never include net/http. This deliberately does NOT check
// the full transitive import graph: design.md's "Library Verification"
// table already confirms TUF (pkg/tuf, reachable from sigstore-go/pkg/root)
// links net/http into the binary transitively but is never called --
// checking direct imports of the two packages actually exercised by
// VerifyKeyless's call path is the structural signal design.md describes.
func TestKeylessImports_NoNetHTTP(t *testing.T) {
	t.Parallel()

	assertNoDirectNetHTTPImport(t, "keyless.go's own file-level imports", directFileImports(t, "keyless.go"))
	assertNoDirectNetHTTPImport(t, "sigstore-go/pkg/verify's own direct imports", directPackageImports(t, "github.com/sigstore/sigstore-go/pkg/verify"))
}

func assertNoDirectNetHTTPImport(t *testing.T, label string, imports []string) {
	t.Helper()

	for _, imp := range imports {
		if imp == "net/http" {
			t.Fatalf("%s: imports net/http, want offline-only (zero network I/O)", label)
		}
	}
}

// directFileImports parses a single Go source file's own import list via
// go/parser -- deliberately file-scoped, not package-scoped, since keyless.go
// shares its package with bundle.go/cosign.go/keys.go, which this test must
// not conflate with keyless.go's own imports.
func directFileImports(t *testing.T, filename string) []string {
	t.Helper()

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, filename, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parser.ParseFile(%s) error = %v", filename, err)
	}

	imports := make([]string, 0, len(astFile.Imports))
	for _, imp := range astFile.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquoting import path %s: %v", imp.Path.Value, err)
		}
		imports = append(imports, path)
	}
	return imports
}

// directPackageImports reports pkgPath's own direct (non-transitive)
// imports. Prefers go/build.Import; falls back to `go list` (module-aware)
// if go/build's classic GOPATH-oriented resolution cannot find the package
// in module mode.
func directPackageImports(t *testing.T, pkgPath string) []string {
	t.Helper()

	pkg, err := build.Import(pkgPath, ".", 0)
	if err == nil {
		return pkg.Imports
	}

	out, listErr := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkgPath).Output()
	if listErr != nil {
		t.Fatalf("resolving %s imports: build.Import error = %v, go list error = %v", pkgPath, err, listErr)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

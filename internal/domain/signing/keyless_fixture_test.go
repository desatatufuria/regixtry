package signing

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"testing"
	"time"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
	sigdsse "github.com/sigstore/sigstore/pkg/signature/dsse"
)

// keylessTestEntity is a minimal, fully self-built verify.SignedEntity,
// mirroring sigstore-go/pkg/testing/ca's own (unexported) TestEntity shape.
// Building it here, one level below TestEntity, is what lets these tests
// swap in a deliberately corrupted or foreign tlog entry (see
// tlogEntriesFrom in buildKeylessEntity) for the tampered-SET case --
// TestEntity itself exposes no way to do that, since its fields are
// unexported.
type keylessTestEntity struct {
	certChain   []*x509.Certificate
	envelope    *dsse.Envelope
	tlogEntries []*tlog.Entry
}

func (e *keylessTestEntity) VerificationContent() (verify.VerificationContent, error) {
	return bundle.NewCertificate(e.certChain[0]), nil
}

func (e *keylessTestEntity) HasInclusionPromise() bool {
	return true
}

func (e *keylessTestEntity) HasInclusionProof() bool {
	for _, entry := range e.tlogEntries {
		if entry.HasInclusionProof() {
			return true
		}
	}
	return false
}

func (e *keylessTestEntity) SignatureContent() (verify.SignatureContent, error) {
	return &bundle.Envelope{Envelope: e.envelope}, nil
}

// Timestamps deliberately returns no RFC3161 timestamp authority response:
// these fixtures only exercise the transparency-log (SET) observer-timestamp
// path (verify.WithObserverTimestamps), never a TSA. VerifyTimestampAuthority
// (sigstore-go's pkg/verify/tsa.go, confirmed by reading its source) treats
// an empty Timestamps() slice as "zero TSA timestamps verified", not an
// error, so the SET-derived observer timestamp alone still satisfies
// WithObserverTimestamps(1).
func (e *keylessTestEntity) Timestamps() ([][]byte, error) {
	return nil, nil
}

func (e *keylessTestEntity) TlogEntries() ([]*tlog.Entry, error) {
	return e.tlogEntries, nil
}

var _ verify.SignedEntity = (*keylessTestEntity)(nil)

// newSyntheticVirtualSigstore builds a fresh sigstore-go/pkg/testing/ca
// VirtualSigstore -- a synthetic Fulcio root+intermediate, Rekor key, and
// (unused here) TSA -- never the real pinned trusted root
// (tasks.md 3.1: "never the real pinned root in tests").
func newSyntheticVirtualSigstore(t *testing.T) *ca.VirtualSigstore {
	t.Helper()

	virtualCA, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("ca.NewVirtualSigstore() error = %v", err)
	}
	return virtualCA
}

// newSyntheticVerifier builds a SignedEntityVerifier bound to trustedCA's
// own trusted material (ca.VirtualSigstore implements root.TrustedMaterial
// directly) -- the "own test trusted root" tasks.md 3.1 requires, and the
// same WithTransparencyLog+WithObserverTimestamps option set production's
// signedEntityVerifier uses (WithSignedCertificateTimestamps deliberately
// excluded; see keyless.go's doc comment on signedEntityVerifier).
func newSyntheticVerifier(t *testing.T, trustedCA *ca.VirtualSigstore) *verify.SignedEntityVerifier {
	t.Helper()

	sev, err := verify.NewSignedEntityVerifier(trustedCA, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		t.Fatalf("verify.NewSignedEntityVerifier() error = %v", err)
	}
	return sev
}

// keylessFixtureOptions configures buildKeylessEntity's deviations from the
// fully-valid happy path, one knob per adversarial case this file's tests
// need.
type keylessFixtureOptions struct {
	// integratedTime overrides the tlog entry's integrated (SET) time.
	// Zero value means "now". Used to simulate an expired certificate: a
	// leaf cert is only valid for 10 minutes from its own generation time
	// (ca.GenerateLeafCert, sigstore-go source), so an integratedTime far
	// enough in the future makes the cert observably expired relative to
	// the (trusted, SET-derived) observer timestamp used for chain
	// validation.
	integratedTime time.Time

	// tlogEntriesFrom, if non-nil, generates the tlog entry (and its Signed
	// Entry Timestamp) using a DIFFERENT VirtualSigstore's Rekor key than
	// the one whose certificate authority signed the leaf certificate.
	// Verifying against the leaf's own trusted material then finds a tlog
	// entry whose SET does not verify against any Rekor key that trusted
	// material recognizes -- a real member of the "tampered/forged SET"
	// failure class (tlog.VerifySET fails to match), not merely a
	// byte-flip simulation of it.
	tlogEntriesFrom *ca.VirtualSigstore

	// omitTlogEntries, if true, builds an entity with zero tlog entries at
	// all -- the "missing SET" case, distinct from "present but invalid".
	omitTlogEntries bool
}

// buildKeylessEntity builds a fully synthetic, real (non-mocked)
// verify.SignedEntity: a genuine Fulcio-shaped leaf certificate issued by
// signingCA's own intermediate, a genuine DSSE-signed in-toto statement
// envelope, and (unless overridden) a genuine Rekor tlog entry with a valid
// Signed Entry Timestamp -- exactly the shape a real keyless `cosign sign`
// bundle document's dsseEnvelope-shaped verificationMaterial carries (Phase
// 0 spike; apply-progress.md).
func buildKeylessEntity(t *testing.T, signingCA *ca.VirtualSigstore, identity, issuer string, statement []byte, opts keylessFixtureOptions) *keylessTestEntity {
	t.Helper()

	leafCert, leafKey, err := signingCA.GenerateLeafCert(identity, issuer)
	if err != nil {
		t.Fatalf("GenerateLeafCert() error = %v", err)
	}

	signer, err := signature.LoadECDSASignerVerifier(leafKey, crypto.SHA256)
	if err != nil {
		t.Fatalf("LoadECDSASignerVerifier() error = %v", err)
	}

	dsseSigner, err := dsse.NewEnvelopeSigner(&sigdsse.SignerAdapter{
		SignatureSigner: signer,
		Pub:             leafCert.PublicKey.(*ecdsa.PublicKey),
	})
	if err != nil {
		t.Fatalf("dsse.NewEnvelopeSigner() error = %v", err)
	}

	envelope, err := dsseSigner.SignPayload(context.Background(), "application/vnd.in-toto+json", statement)
	if err != nil {
		t.Fatalf("SignPayload() error = %v", err)
	}

	entity := &keylessTestEntity{
		certChain: []*x509.Certificate{leafCert},
		envelope:  envelope,
	}

	if opts.omitTlogEntries {
		return entity
	}

	sig, err := base64.StdEncoding.DecodeString(envelope.Signatures[0].Sig)
	if err != nil {
		t.Fatalf("base64-decoding envelope signature: %v", err)
	}

	integratedTime := opts.integratedTime
	if integratedTime.IsZero() {
		integratedTime = time.Now()
	}

	tlogCA := signingCA
	if opts.tlogEntriesFrom != nil {
		tlogCA = opts.tlogEntriesFrom
	}

	entry, err := tlogCA.GenerateTlogEntry(leafCert, envelope, sig, integratedTime.Unix(), false)
	if err != nil {
		t.Fatalf("GenerateTlogEntry() error = %v", err)
	}

	entity.tlogEntries = []*tlog.Entry{entry}

	return entity
}

// inTotoStatementFor builds the exact in-toto Statement JSON bytes a
// keyless `cosign sign` bundle's dsseEnvelope payload carries, binding the
// given sha256 hex digest -- mirrors the real captured shape documented in
// testdata/README.md's "Sigstore bundle fixtures" section (same
// predicateType, same subject/digest/sha256 shape), differing only in the
// digest value.
func inTotoStatementFor(digestHex string) []byte {
	return []byte(`{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{"sha256":"` + digestHex + `"},"annotations":{}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`)
}

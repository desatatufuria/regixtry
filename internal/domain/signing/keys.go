package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

const pemBlockType = "PUBLIC KEY"

// NormalizePublicKeyPEM accepts a PEM public key in any whitespace
// arrangement (real newlines from an API body, spaces from the TUI's
// single-line field), rejects anything that is not ECDSA P-256, and returns
// the canonical PEM that gets stored. Configuration-time, never pull-time.
func NormalizePublicKeyPEM(raw string) (string, error) {
	block, err := decodePEMBlock(raw)
	if err != nil {
		return "", err
	}

	if _, err := parseECDSAP256PublicKey(block.Bytes); err != nil {
		return "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: pemBlockType, Bytes: block.Bytes})), nil
}

// ParseTrustedKey decodes one canonical stored PEM into an ECDSA P-256
// public key.
func ParseTrustedKey(pemText string) (*ecdsa.PublicKey, error) {
	block, err := decodePEMBlock(pemText)
	if err != nil {
		return nil, err
	}

	return parseECDSAP256PublicKey(block.Bytes)
}

// Fingerprint derives a read-only, short display identifier for a trusted
// public key PEM: SHA-256 of the trimmed PEM text, truncated to its first 12
// hex characters. This is the ONE canonical implementation of that
// algorithm -- every caller that needs to display "which key" (the TUI's
// signing config screens, the manifest inspect screen's verified-key report)
// must call this instead of reimplementing the hash+truncate logic, so a
// fingerprint value can never drift between call sites.
func Fingerprint(pemText string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(pemText)))
	return hex.EncodeToString(sum[:])[:12]
}

// Verify returns nil only on a valid signature. It never returns a bool: a
// caller under a fail-closed gate must not be able to read "error" as
// "allowed". Errors carry a fixed vocabulary and never echo key or payload
// bytes.
func Verify(key *ecdsa.PublicKey, payload []byte, signatureBase64 string) error {
	if key == nil {
		return errors.New("signing: trusted key is required")
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return errors.New("signing: signature is not valid base64")
	}

	hash := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(key, hash[:], signature) {
		return errors.New("signing: signature verification failed")
	}

	return nil
}

// decodePEMBlock accepts a PEM public key in any whitespace arrangement.
// A PEM block is normally multi-line, but a caller may submit it as a
// single line with spaces in place of newlines (e.g. a TUI text field,
// where a literal newline submits the field instead of continuing it).
// Reconstructing the block from its BEGIN/END markers and the base64 body
// between them, independent of the original line breaks, handles both
// forms with a single code path.
func decodePEMBlock(raw string) (*pem.Block, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("signing: public key PEM is required")
	}

	beginMarker := "-----BEGIN " + pemBlockType + "-----"
	endMarker := "-----END " + pemBlockType + "-----"

	beginIndex := strings.Index(trimmed, beginMarker)
	endIndex := strings.Index(trimmed, endMarker)
	if beginIndex == -1 || endIndex == -1 || endIndex < beginIndex {
		return nil, fmt.Errorf("signing: public key PEM must have %q and %q markers", beginMarker, endMarker)
	}

	body := strings.Join(strings.Fields(trimmed[beginIndex+len(beginMarker):endIndex]), "")
	if body == "" {
		return nil, errors.New("signing: public key PEM body is empty")
	}

	var reconstructed strings.Builder
	reconstructed.WriteString(beginMarker)
	reconstructed.WriteByte('\n')
	for len(body) > 0 {
		lineLength := 64
		if lineLength > len(body) {
			lineLength = len(body)
		}
		reconstructed.WriteString(body[:lineLength])
		reconstructed.WriteByte('\n')
		body = body[lineLength:]
	}
	reconstructed.WriteString(endMarker)
	reconstructed.WriteByte('\n')

	block, rest := pem.Decode([]byte(reconstructed.String()))
	if block == nil {
		return nil, errors.New("signing: public key is not valid PEM")
	}
	if len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("signing: public key PEM has trailing data after the END marker")
	}
	if block.Type != pemBlockType {
		return nil, fmt.Errorf("signing: public key PEM block type must be %q, got %q", pemBlockType, block.Type)
	}

	return block, nil
}

// parseECDSAP256PublicKey decodes PKIX/SPKI DER bytes and rejects anything
// that is not an ECDSA P-256 key. A key that parses but is not ECDSA P-256
// is rejected here — at configuration time, never at pull time.
func parseECDSAP256PublicKey(der []byte) (*ecdsa.PublicKey, error) {
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("signing: public key is not a valid PKIX/SPKI key: %w", err)
	}

	ecdsaKey, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("signing: public key must be ECDSA (Ed25519 and RSA are not supported in this slice)")
	}

	if ecdsaKey.Curve != elliptic.P256() {
		return nil, errors.New("signing: public key must use curve P-256")
	}

	return ecdsaKey, nil
}

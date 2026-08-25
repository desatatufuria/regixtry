package signing

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

func TestNormalizePublicKeyPEM(t *testing.T) {
	t.Parallel()

	realNewlinePEM := string(readTestdataFixture(t, "cosign.pub"))
	spaceSeparatedPEM := strings.Join(strings.Fields(realNewlinePEM), " ")

	canonicalFromNewlines, err := NormalizePublicKeyPEM(realNewlinePEM)
	if err != nil {
		t.Fatalf("NormalizePublicKeyPEM(real-newline PEM) error = %v", err)
	}

	canonicalFromSpaces, err := NormalizePublicKeyPEM(spaceSeparatedPEM)
	if err != nil {
		t.Fatalf("NormalizePublicKeyPEM(space-separated PEM) error = %v", err)
	}

	if canonicalFromNewlines != canonicalFromSpaces {
		t.Fatalf("canonical PEM differs by input whitespace arrangement:\nnewlines: %q\nspaces:   %q", canonicalFromNewlines, canonicalFromSpaces)
	}

	tests := []struct {
		name string
		pem  string
	}{
		{name: "empty input", pem: ""},
		{name: "garbage input", pem: "not a pem at all"},
		{name: "ed25519 key is rejected", pem: generateEd25519PublicKeyPEM(t)},
		{name: "rsa key is rejected", pem: generateRSAPublicKeyPEM(t)},
		{name: "p-384 key is rejected", pem: generateECDSAPublicKeyPEM(t, elliptic.P384())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NormalizePublicKeyPEM(tt.pem); err == nil {
				t.Fatalf("NormalizePublicKeyPEM(%q) error = nil, want error", tt.name)
			}
		})
	}
}

// TestFingerprint is the RED test for the canonical fingerprint helper: it
// must reproduce, byte-for-byte, the exact algorithm the TUI's
// signingKeyFingerprints previously implemented directly (sha256 of the
// trimmed PEM text, first 12 hex characters) -- so this becomes the single
// source of truth without changing any fingerprint value already displayed
// anywhere in the TUI.
func TestFingerprint(t *testing.T) {
	t.Parallel()

	pemText := string(readTestdataFixture(t, "cosign.pub"))
	sum := sha256.Sum256([]byte(strings.TrimSpace(pemText)))
	want := hex.EncodeToString(sum[:])[:12]

	if got := Fingerprint(pemText); got != want {
		t.Fatalf("Fingerprint() = %q, want %q", got, want)
	}

	t.Run("trims surrounding whitespace before hashing", func(t *testing.T) {
		t.Parallel()

		padded := "\n  " + pemText + "  \n"
		if got := Fingerprint(padded); got != want {
			t.Fatalf("Fingerprint(padded) = %q, want %q (must trim before hashing)", got, want)
		}
	})

	t.Run("returns exactly 12 hex characters", func(t *testing.T) {
		t.Parallel()

		got := Fingerprint(pemText)
		if len(got) != 12 {
			t.Fatalf("len(Fingerprint()) = %d, want 12", len(got))
		}
	})
}

func TestParseTrustedKey(t *testing.T) {
	t.Parallel()

	canonical, err := NormalizePublicKeyPEM(string(readTestdataFixture(t, "cosign.pub")))
	if err != nil {
		t.Fatalf("NormalizePublicKeyPEM() error = %v", err)
	}

	key, err := ParseTrustedKey(canonical)
	if err != nil {
		t.Fatalf("ParseTrustedKey() error = %v", err)
	}

	if key == nil {
		t.Fatal("ParseTrustedKey() returned a nil key")
	}
	if key.Curve != elliptic.P256() {
		t.Fatalf("ParseTrustedKey().Curve = %v, want P-256", key.Curve)
	}
}

// TestVerify_SyntheticFixture exercises Verify against the Phase 0 synthetic
// fixture (testdata/README.md documents its provenance: hand-constructed
// offline, never captured from a real `cosign sign` invocation, because no
// cosign binary was available in this apply environment).
func TestVerify_SyntheticFixture(t *testing.T) {
	t.Parallel()

	payload := readTestdataFixture(t, "payload.json")
	pubPEM := readTestdataFixture(t, "cosign.pub")
	signature := signatureAnnotationFromFixture(t)

	key, err := ParseTrustedKey(string(pubPEM))
	if err != nil {
		t.Fatalf("ParseTrustedKey() error = %v", err)
	}

	if err := Verify(key, payload, signature); err != nil {
		t.Fatalf("Verify(valid fixture) error = %v, want nil", err)
	}

	t.Run("rejects a one-byte-mutated payload", func(t *testing.T) {
		t.Parallel()

		mutated := append([]byte(nil), payload...)
		mutated[0] = mutated[0] ^ 0xFF

		if err := Verify(key, mutated, signature); err == nil {
			t.Fatal("Verify(mutated payload) error = nil, want error")
		}
	})

	t.Run("rejects a mutated signature", func(t *testing.T) {
		t.Parallel()

		decoded, err := base64.StdEncoding.DecodeString(signature)
		if err != nil {
			t.Fatalf("base64 decode fixture signature: %v", err)
		}
		decoded[0] = decoded[0] ^ 0xFF
		mutatedSignature := base64.StdEncoding.EncodeToString(decoded)

		if err := Verify(key, payload, mutatedSignature); err == nil {
			t.Fatal("Verify(mutated signature) error = nil, want error")
		}
	})

	t.Run("rejects a wrong key", func(t *testing.T) {
		t.Parallel()

		wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate wrong key: %v", err)
		}

		if err := Verify(&wrongKey.PublicKey, payload, signature); err == nil {
			t.Fatal("Verify(wrong key) error = nil, want error")
		}
	})

	t.Run("rejects non-base64 signature", func(t *testing.T) {
		t.Parallel()

		if err := Verify(key, payload, "not-base64!!!"); err == nil {
			t.Fatal("Verify(non-base64 signature) error = nil, want error")
		}
	})
}

// TestVerify_RemarshallingPayloadBreaksVerification is the Decision 1a
// pinning test — the single most important correctness property in this
// package. SimpleSigning JSON is not canonical: decoding it into a Go
// struct and re-marshalling it can change key order and whitespace, which
// changes the exact bytes that were hashed and signed. If Verify (or any
// caller) ever hashed a re-marshalled struct instead of the verbatim stored
// bytes, every real cosign signature would silently fail to verify. This
// test proves the reverse holds for our fixture: the re-marshalled bytes
// differ from the original, round-trip as equivalent JSON, and Verify
// rejects the re-marshalled version even though it is semantically
// equivalent.
func TestVerify_RemarshallingPayloadBreaksVerification(t *testing.T) {
	t.Parallel()

	original := readTestdataFixture(t, "payload.json")
	pubPEM := readTestdataFixture(t, "cosign.pub")
	signature := signatureAnnotationFromFixture(t)

	key, err := ParseTrustedKey(string(pubPEM))
	if err != nil {
		t.Fatalf("ParseTrustedKey() error = %v", err)
	}

	// Sanity: the original, verbatim bytes verify.
	if err := Verify(key, original, signature); err != nil {
		t.Fatalf("Verify(original payload) error = %v, want nil", err)
	}

	// Decode into a generic map and re-marshal — Go's encoding/json sorts
	// map keys alphabetically, which is very likely to disagree with
	// SimpleSigning's field order ("critical" then "optional", with
	// "identity"/"image"/"type" inside "critical" in that order), and never
	// reproduces the original's exact byte-for-byte whitespace.
	var generic map[string]any
	if err := json.Unmarshal(original, &generic); err != nil {
		t.Fatalf("json.Unmarshal(original) error = %v", err)
	}

	remarshalled, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("json.Marshal(generic) error = %v", err)
	}

	if string(remarshalled) == string(original) {
		t.Fatalf("re-marshalled payload is byte-identical to the original — this test's premise (JSON is not canonical) does not hold for this fixture, the test needs a different payload shape")
	}

	// The re-marshalled bytes still decode to an equivalent structure —
	// round-trips as equivalent JSON.
	var remarshalledGeneric map[string]any
	if err := json.Unmarshal(remarshalled, &remarshalledGeneric); err != nil {
		t.Fatalf("json.Unmarshal(remarshalled) error = %v", err)
	}

	// The load-bearing assertion: verification must hash the verbatim
	// bytes, never a re-marshalled struct. The re-marshalled bytes hash to
	// a different SHA-256 sum than the original, and Verify rejects them.
	originalSum := sha256.Sum256(original)
	remarshalledSum := sha256.Sum256(remarshalled)
	if originalSum == remarshalledSum {
		t.Fatalf("SHA-256(original) == SHA-256(remarshalled), want different sums — re-marshalling must change the hashed bytes for this pinning test to be meaningful")
	}

	if err := Verify(key, remarshalled, signature); err == nil {
		t.Fatal("Verify(re-marshalled payload) error = nil, want error — hashing a re-marshalled struct instead of verbatim bytes must break verification")
	}
}

func TestCheckClaims(t *testing.T) {
	t.Parallel()

	payload := readTestdataFixture(t, "payload.json")

	var parsed struct {
		Critical struct {
			Image struct {
				DockerManifestDigest string `json:"docker-manifest-digest"`
			} `json:"image"`
		} `json:"critical"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("json.Unmarshal(fixture payload) error = %v", err)
	}
	boundDigest := parsed.Critical.Image.DockerManifestDigest
	if boundDigest == "" {
		t.Fatal("fixture payload has no docker-manifest-digest — fixture is malformed")
	}

	if err := CheckClaims(payload, boundDigest); err != nil {
		t.Fatalf("CheckClaims(matching digest) error = %v, want nil", err)
	}

	t.Run("rejects a payload binding a different digest", func(t *testing.T) {
		t.Parallel()

		if err := CheckClaims(payload, "sha256:0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
			t.Fatal("CheckClaims(different digest) error = nil, want error")
		}
	})

	t.Run("rejects a wrong critical.type", func(t *testing.T) {
		t.Parallel()

		wrongType := strings.Replace(string(payload), "cosign container image signature", "something else", 1)
		if err := CheckClaims([]byte(wrongType), boundDigest); err == nil {
			t.Fatal("CheckClaims(wrong critical.type) error = nil, want error")
		}
	})

	t.Run("accepts a differing docker-reference (documented, intentional)", func(t *testing.T) {
		t.Parallel()

		differentRef := strings.Replace(string(payload), "registry.example.com/library/synthetic-fixture:v1", "registry.example.com/other/repo:v9", 1)
		if err := CheckClaims([]byte(differentRef), boundDigest); err != nil {
			t.Fatalf("CheckClaims(differing docker-reference) error = %v, want nil — docker-reference is deliberately not enforced", err)
		}
	})
}

func signatureAnnotationFromFixture(t *testing.T) string {
	t.Helper()

	raw := readTestdataFixture(t, "signature-manifest.json")
	entries, err := ParseSignatureManifest(raw)
	if err != nil {
		t.Fatalf("ParseSignatureManifest(fixture) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	return entries[0].Signature
}

func generateEd25519PublicKeyPEM(t *testing.T) string {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal ed25519 public key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func generateRSAPublicKeyPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal rsa public key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func generateECDSAPublicKeyPEM(t *testing.T, curve elliptic.Curve) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key on curve %v: %v", curve, err)
	}

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal ecdsa public key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

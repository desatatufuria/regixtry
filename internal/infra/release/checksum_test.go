package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyChecksumAcceptsMatchAndRejectsMismatch(t *testing.T) {
	t.Parallel()

	archiveBody := []byte("archive-body")
	archivePath := filepath.Join(t.TempDir(), "example_1.0.0_Linux-64bit.tar.gz")
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}
	sum := sha256.Sum256(archiveBody)

	validChecksums := []byte(hex.EncodeToString(sum[:]) + "  " + filepath.Base(archivePath) + "\n")
	if err := VerifyChecksum(archivePath, filepath.Base(archivePath), validChecksums); err != nil {
		t.Fatalf("VerifyChecksum() error = %v", err)
	}

	withSidecarEntry := []byte(strings.Join([]string{
		strings.Repeat("f", 64) + "  " + filepath.Base(archivePath) + ".sigstore.json",
		hex.EncodeToString(sum[:]) + "  " + filepath.Base(archivePath),
	}, "\n") + "\n")
	if err := VerifyChecksum(archivePath, filepath.Base(archivePath), withSidecarEntry); err != nil {
		t.Fatalf("VerifyChecksum() with sidecar entry error = %v", err)
	}

	mismatched := []byte(strings.Repeat("0", 64) + "  " + filepath.Base(archivePath) + "\n")
	if err := VerifyChecksum(archivePath, filepath.Base(archivePath), mismatched); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("VerifyChecksum() error = %v, want checksum mismatch", err)
	}
}

func TestVerifyChecksumRejectsMissingEntry(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "example.tar.gz")
	if err := os.WriteFile(archivePath, []byte("body"), 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}
	if err := VerifyChecksum(archivePath, filepath.Base(archivePath), []byte("")); err == nil || !strings.Contains(err.Error(), "checksum not found") {
		t.Fatalf("VerifyChecksum() error = %v, want checksum not found", err)
	}
}

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// VerifyChecksum reads archivePath and confirms its SHA-256 digest matches
// the entry for archiveName inside checksumsBody — the contents of a
// sha256sum-style checksums file ("<hex-digest>  <filename>" per line,
// optionally prefixed with "*" for binary mode, and tolerant of extra
// sidecar entries such as ".sigstore.json" checksums).
func VerifyChecksum(archivePath string, archiveName string, checksumsBody []byte) error {
	body, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	want := strings.ToLower(hex.EncodeToString(sum[:]))
	for _, line := range strings.Split(string(checksumsBody), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != archiveName {
			continue
		}
		if strings.ToLower(strings.TrimSpace(fields[0])) != want {
			return fmt.Errorf("checksum mismatch for %s", archiveName)
		}
		return nil
	}
	return fmt.Errorf("checksum not found for %s", archiveName)
}

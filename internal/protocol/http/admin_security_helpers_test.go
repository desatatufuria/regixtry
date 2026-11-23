package regixtryhttp

import (
	"crypto/sha256"
	"encoding/hex"
)

func accountHashForTest(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:4])
}

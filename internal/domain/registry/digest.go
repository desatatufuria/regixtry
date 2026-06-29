package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const digestAlgorithmSHA256 = "sha256"

type Digest string

func ParseDigest(value string) (Digest, error) {
	algorithm, encoded, ok := strings.Cut(value, ":")
	if !ok || algorithm != digestAlgorithmSHA256 || len(encoded) != sha256.Size*2 {
		return "", NewInvalidDigestError(value)
	}

	if _, err := hex.DecodeString(encoded); err != nil {
		return "", NewInvalidDigestError(value)
	}

	return Digest(value), nil
}

func MustParseDigest(value string) Digest {
	digest, err := ParseDigest(value)
	if err != nil {
		panic(err)
	}

	return digest
}

func DigestFromBytes(payload []byte) Digest {
	sum := sha256.Sum256(payload)
	return Digest(fmt.Sprintf("%s:%s", digestAlgorithmSHA256, hex.EncodeToString(sum[:])))
}

func DigestFromReader(reader io.Reader) (Digest, int64, error) {
	hasher := sha256.New()
	size, err := io.Copy(hasher, reader)
	if err != nil {
		return "", 0, err
	}

	return Digest(fmt.Sprintf("%s:%s", digestAlgorithmSHA256, hex.EncodeToString(hasher.Sum(nil)))), size, nil
}

func (d Digest) Validate() error {
	_, err := ParseDigest(string(d))
	return err
}

func (d Digest) String() string {
	return string(d)
}

func (d Digest) Algorithm() string {
	algorithm, _, _ := strings.Cut(string(d), ":")
	return algorithm
}

func (d Digest) Encoded() string {
	_, encoded, _ := strings.Cut(string(d), ":")
	return encoded
}

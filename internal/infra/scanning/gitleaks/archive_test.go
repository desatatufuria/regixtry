package gitleaks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

// makeGitleaksArchive builds a gzip-compressed tar archive containing a
// single "gitleaks" binary entry, mirroring the shape of gitleaks' real
// release archives closely enough for internal/infra/release.ExtractBinary
// to resolve it.
func makeGitleaksArchive(t *testing.T, binaryBody string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gzw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: "gitleaks", Mode: 0o755, Size: int64(len(binaryBody))}); err != nil {
		t.Fatalf("WriteHeader(gitleaks) error = %v", err)
	}
	if _, err := tw.Write([]byte(binaryBody)); err != nil {
		t.Fatalf("Write(gitleaks) error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	return raw.Bytes()
}

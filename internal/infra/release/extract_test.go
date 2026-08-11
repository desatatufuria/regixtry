package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractBinaryMatchesEntryByBaseNameAmongOtherFiles(t *testing.T) {
	t.Parallel()

	archiveBody := makeReleaseArchive(t, map[string]string{
		"README.md":      "docs",
		"nested/example": "example-binary",
	})
	archivePath := filepath.Join(t.TempDir(), "example_1.0.0.tar.gz")
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}

	binaryPath, err := ExtractBinary(context.Background(), archivePath, filepath.Join(t.TempDir(), "extract"), "example")
	if err != nil {
		t.Fatalf("ExtractBinary() error = %v", err)
	}
	body, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("ReadFile(binary) error = %v", err)
	}
	if string(body) != "example-binary" {
		t.Fatalf("binary body = %q, want extracted payload", string(body))
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("Stat(binary) error = %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary mode = %v, want executable permissions", info.Mode())
	}
}

func TestExtractBinaryFailsWhenNameNotFound(t *testing.T) {
	t.Parallel()

	archiveBody := makeReleaseArchive(t, map[string]string{"README.md": "docs"})
	archivePath := filepath.Join(t.TempDir(), "example.tar.gz")
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}
	if _, err := ExtractBinary(context.Background(), archivePath, filepath.Join(t.TempDir(), "extract"), "example"); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("ExtractBinary() error = %v, want ErrBinaryNotFound", err)
	}
}

func makeReleaseArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gzw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gzw)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("Write(%s) error = %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	return raw.Bytes()
}
